package scale_down

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	strings2 "strings"
	"time"

	"github.com/weka/go-cloud-lib/connectors"
	"github.com/weka/go-cloud-lib/lib/jrpc"
	"github.com/weka/go-cloud-lib/lib/math"
	"github.com/weka/go-cloud-lib/lib/strings"
	"github.com/weka/go-cloud-lib/lib/types"
	"github.com/weka/go-cloud-lib/lib/weka"
	"github.com/weka/go-cloud-lib/logging"
	"github.com/weka/go-cloud-lib/protocol"

	"github.com/google/uuid"
)

type hostState int

const unhealthyDeactivateTimeout = 120 * time.Minute
const backendCleanupDelay = 5 * time.Minute // Giving own HG chance to take care
const downKickOutTimeout = 3 * time.Hour

func (h hostState) String() string {
	switch h {
	case DEACTIVATING:
		return "DEACTIVATING"
	case HEALTHY:
		return "HEALTHY"
	case UNHEALTHY:
		return "UNHEALTHY"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", h)
	}
}

const (
	/*
		Order matters, it defines priority of hosts removal
	*/
	DEACTIVATING hostState = iota
	UNHEALTHY
	HEALTHY
)

type driveMap map[weka.DriveId]weka.Drive
type nodeMap map[weka.NodeId]weka.Node
type hostInfo struct {
	weka.Host
	id         weka.HostId
	drives     driveMap
	nodes      nodeMap
	scaleState hostState
}

func (host hostInfo) belongsToHgIpBased(instances []protocol.HgInstance) bool {
	for _, instance := range instances {
		if host.HostIp == instance.PrivateIp {
			return true
		}
	}
	return false
}

func (host hostInfo) numNotHealthyDrives() int {
	notActive := 0
	for _, drive := range host.drives {
		if strings.AnyOf(drive.Status, "INACTIVE") && time.Since(host.AddedTime) > time.Minute*5 {
			notActive += 1
		}
	}
	return notActive
}

func (host hostInfo) allDisksBeingRemoved() bool {
	ret := false
	for _, drive := range host.drives {
		ret = true
		if drive.ShouldBeActive {
			return false
		}
	}
	return ret
}

func (host hostInfo) anyDiskBeingRemoved() bool {
	for _, drive := range host.drives {
		if !drive.ShouldBeActive {
			return true
		}
	}
	return false
}

func (host hostInfo) allDrivesInactive() bool {
	for _, drive := range host.drives {
		if drive.Status != "INACTIVE" {
			return false
		}
	}
	return true
}

func (host hostInfo) managementTimedOut(timeout time.Duration) bool {
	for nodeId, node := range host.nodes {
		if !nodeId.IsManagement() {
			continue
		}
		var period time.Time
		if node.LastFencingTime != nil {
			period = *node.LastFencingTime
		} else {
			period = host.StateChangedTime
		}
		if node.Status == "DOWN" && time.Since(period) > timeout {
			return true
		}
	}
	return false
}

func remoteDownHosts(hosts []hostInfo, jpool *jrpc.Pool) {

}

type machineState struct {
	Healthy      int
	Unhealthy    int
	Deactivating int
}

func getNumToDeactivate(ctx context.Context, hostInfo []hostInfo, desired int) int {
	/*
		A - Fully active, healthy
		T - Target state
		U - Unhealthy, we want to remove it for whatever reason. DOWN host, FAILED drive, so on
		D - Drives/hosts being deactivated
		new_D - Decision to start deactivating, i.e transition to D, basing on U. Never more then 2 for U

		new_D = func(A, U, T, D)

		new_D = max(A+U+D-T, min(2-D, U), 0)
	*/
	logger := logging.LoggerFromCtx(ctx)

	nHealthy := 0
	nUnhealthy := 0
	nDeactivating := 0

	machines := make(map[string]*machineState)
	for _, host := range hostInfo {
		if _, ok := machines[host.HostIp]; !ok {
			machines[host.HostIp] = &machineState{0, 0, 0}
		}
		switch host.scaleState {
		case HEALTHY:
			machines[host.HostIp].Healthy++
		case UNHEALTHY:
			machines[host.HostIp].Unhealthy++
		case DEACTIVATING:
			machines[host.HostIp].Deactivating++
		}
	}

	for _, machine := range machines {
		if machine.Unhealthy > 0 {
			nUnhealthy++
		} else if machine.Deactivating > 0 {
			nDeactivating++
		} else {
			nHealthy++
		}
	}

	toDeactivate := CalculateDeactivateTarget(nHealthy, nUnhealthy, nDeactivating, desired)
	logger.Info().Msgf("%d machines set to deactivate. nHealthy: %d nUnhealthy:%d nDeactivating: %d desired:%d", toDeactivate, nHealthy, nUnhealthy, nDeactivating, desired)
	return toDeactivate
}

func getMachineDriveContainerByHost(hosts []hostInfo, host hostInfo) (hostInfo, error) {
	if strings2.Contains(host.ContainerName, "drive") {
		return host, nil
	}

	for _, currentHost := range hosts {
		if strings2.Contains(currentHost.ContainerName, "drive") && currentHost.HostIp == host.HostIp {
			return currentHost, nil
		}
	}
	return hostInfo{}, fmt.Errorf("no drive container found for machine %s", host.HostIp)
}

func getDriveContainers(hostInfo []hostInfo) (driveContainers []hostInfo, err error) {
	hostsIps := make(map[string]types.Nilt)
	for _, host := range hostInfo {
		if _, ok := hostsIps[host.HostIp]; ok {
			continue
		}
		driveContainer, err2 := getMachineDriveContainerByHost(hostInfo, host)
		if err2 != nil {
			err = err2
			return
		}
		hostsIps[host.HostIp] = types.Nilv
		driveContainers = append(driveContainers, driveContainer)
	}
	return
}

func CalculateDeactivateTarget(nHealthy int, nUnhealthy int, nDeactivating int, desired int) int {
	ret := math.Max(nHealthy+nUnhealthy+nDeactivating-desired, math.Min(2-nDeactivating, nUnhealthy))
	ret = math.Max(nDeactivating, ret)
	return ret
}

func isAllowedToScale(status weka.StatusResponse) error {
	if status.IoStatus != "STARTED" {
		return errors.New(fmt.Sprintf("io status:%s, aborting scale", status.IoStatus))
	}

	if status.Upgrade != "" {
		return errors.New("upgrade is running, aborting scale")
	}
	return nil
}

func deriveHostState(ctx context.Context, host *hostInfo) hostState {
	logger := logging.LoggerFromCtx(ctx)

	if strings2.Contains(host.ContainerName, "drive") && host.allDisksBeingRemoved() {
		logger.Info().Msgf("Marking %s as deactivating due to unhealthy disks", host.id.String())
		return DEACTIVATING
	}
	if strings.AnyOf(host.State, "DEACTIVATING", "REMOVING", "INACTIVE") {
		return DEACTIVATING
	}
	if strings.AnyOf(host.Status, "DOWN", "DEGRADED") && host.managementTimedOut(unhealthyDeactivateTimeout) {
		logger.Info().Msgf("Marking %s as unhealthy due to DOWN", host.id.String())
		return UNHEALTHY
	}
	if host.numNotHealthyDrives() > 0 || host.anyDiskBeingRemoved() {
		logger.Info().Msgf("Marking %s as unhealthy due to unhealthy drives", host.id.String())
		return UNHEALTHY
	}
	return HEALTHY
}

func calculateHostsState(ctx context.Context, hosts []hostInfo) {
	for i := range hosts {
		host := &hosts[i]
		host.scaleState = deriveHostState(ctx, host)
	}
}

func selectInstanceByIp(ip string, instances []protocol.HgInstance) *protocol.HgInstance {
	for _, i := range instances {
		if i.PrivateIp == ip {
			return &i
		}
	}
	return nil
}

func removeContainer(ctx context.Context, jpool *jrpc.Pool, hostId int, p *protocol.ScaleResponse) (err error) {
	logger := logging.LoggerFromCtx(ctx)
	err = jpool.Call(weka.JrpcRemoveHost, types.JsonDict{
		"host_id": hostId,
		"no_wait": true,
	}, nil)
	if err != nil {
		logger.Error().Err(err).Send()
		p.AddTransientError(err, "removeInactive")
	}
	return
}

func removeInactive(ctx context.Context, hostsApiList weka.HostListResponse, inactiveHosts []hostInfo, jpool *jrpc.Pool, instances []protocol.HgInstance, p *protocol.ScaleResponse, allHostsMap hostsMap) {
	logger := logging.LoggerFromCtx(ctx)
	for _, host := range inactiveHosts {
		deactivateHost(ctx, jpool, hostsApiList, p, host)
		logger.Info().Msgf("Removing machine with inactive container/s: %s", host.HostIp)
		jpool.Drop(host.HostIp)
		hostIds := getMachineContainersHostIds(hostsApiList, host)

		removeFailure := false
		readyForRemove := true
		for _, hostId := range hostIds {
			container := allHostsMap[hostId]
			if container.Status == "INACTIVE" {
				err := removeContainer(ctx, jpool, hostId.Int(), p)
				if err != nil {
					removeFailure = true
				}
			} else {
				readyForRemove = false
			}
		}

		if removeFailure || !readyForRemove {
			continue
		}

		instance := selectInstanceByIp(host.HostIp, instances)
		if instance != nil {
			p.ToTerminate = append(p.ToTerminate, *instance)
		}

		for _, drive := range host.drives {
			removeDrive(ctx, jpool, drive, p)
		}
	}
	return
}

func removeOldDrives(ctx context.Context, drives weka.DriveListResponse, jpool *jrpc.Pool, p *protocol.ScaleResponse) {
	for _, drive := range drives {
		if drive.HostId.Int() == -1 && drive.Status == "INACTIVE" {
			removeDrive(ctx, jpool, drive, p)
		}
	}
}

func removeDrive(ctx context.Context, jpool *jrpc.Pool, drive weka.Drive, p *protocol.ScaleResponse) {
	logger := logging.LoggerFromCtx(ctx)

	err := jpool.Call(weka.JrpcRemoveDrive, types.JsonDict{
		"drive_uuids": []uuid.UUID{drive.Uuid},
	}, nil)
	if err != nil {
		logger.Error().Err(err).Send()
		p.AddTransientError(err, "removeDrive")
	}
}

func getMachineContainersHostIds(hostsApiList weka.HostListResponse, inputHost hostInfo) (hostIds []weka.HostId) {
	for hostId, host := range hostsApiList {
		if host.HostIp == inputHost.HostIp {
			hostIds = append(hostIds, hostId)
		}
	}
	return
}

func isMachineInactive(hostsApiList weka.HostListResponse, hostIds []weka.HostId) bool {
	for _, hostId := range hostIds {
		if hostsApiList[hostId].State != "INACTIVE" {
			return false
		}
	}

	return true
}

func deactivateHost(ctx context.Context, jpool *jrpc.Pool, hostsApiList weka.HostListResponse, response *protocol.ScaleResponse, host hostInfo) {
	logger := logging.LoggerFromCtx(ctx)
	logger.Info().Msgf("Trying to deactivate machine %s...", host.HostIp)
	for _, drive := range host.drives {
		logger.Info().Msgf("Trying to deactivate drive: %s", drive.Uuid.String())
		if drive.ShouldBeActive {
			err1 := jpool.Call(weka.JrpcDeactivateDrives, types.JsonDict{
				"drive_uuids": []uuid.UUID{drive.Uuid},
			}, nil)
			if err1 != nil {
				logger.Error().Err(err1).Send()
				response.AddTransientError(err1, "deactivateDrive")
			}
		}
	}

	hostIds := getMachineContainersHostIds(hostsApiList, host)
	logger.Info().Msgf(
		"Trying to deactivate machine %s containers: %s",
		host.HostIp,
		hostIds,
	)

	err1 := jpool.Call(weka.JrpcDeactivateHosts, types.JsonDict{
		"host_ids":                 hostIds,
		"skip_resource_validation": false,
	}, nil)
	if err1 != nil {
		logger.Error().Err(err1).Send()
		response.AddTransientError(err1, "deactivateHost")
	} else {
		jpool.Drop(host.HostIp)
	}

}

func isMBC(hostsApiList weka.HostListResponse) bool {
	for _, host := range hostsApiList {
		if strings2.Contains(host.ContainerName, "drive") {
			return true
		}
	}
	return false
}

type hostsMap map[weka.HostId]hostInfo
type machinesDriveMap map[string]hostInfo

func getMachineDriveHost(drivesMap machinesDriveMap, hostIp string) (driveHost hostInfo, err error) {
	if _, ok := drivesMap[hostIp]; !ok {
		err = fmt.Errorf("no drive container found for machine %s", hostIp)
		return
	}
	driveHost = drivesMap[hostIp]
	return
}

func ScaleDown(ctx context.Context, info protocol.HostGroupInfoResponse) (response protocol.ScaleResponse, err error) {
	/*
		Code in here based on following logic:

		A - Fully active, healthy
		T - Desired target number
		U - Unhealthy, we want to remove it for whatever reason. DOWN host, FAILED drive, so on
		D - Drives/hosts being deactivated
		NEW_D - Decision to start deactivating, i.e transition to D, basing on U. Never more then 2 for U

		NEW_D = func(A, U, T, D)

		NEW_D = max(A+U+D-T, min(2-D, U), 0)
	*/
	logger := logging.LoggerFromCtx(ctx)
	logger.Info().Msg("Running scale down...")
	response.Version = protocol.Version

	jrpcBuilder := func(ip string) *jrpc.BaseClient {
		return connectors.NewJrpcClient(ctx, ip, weka.ManagementJrpcPort, info.Username, info.Password)
	}
	ips := info.BackendIps
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(ips), func(i, j int) { ips[i], ips[j] = ips[j], ips[i] })
	jpool := &jrpc.Pool{
		Ips:     ips,
		Clients: map[string]*jrpc.BaseClient{},
		Active:  "",
		Builder: jrpcBuilder,
		Ctx:     ctx,
	}

	systemStatus := weka.StatusResponse{}
	hostsApiList := weka.HostListResponse{}
	driveApiList := weka.DriveListResponse{}
	nodeApiList := weka.NodeListResponse{}

	err = jpool.Call(weka.JrpcStatus, struct{}{}, &systemStatus)
	if err != nil {
		logger.Error().Err(err).Send()
		return
	}
	err = isAllowedToScale(systemStatus)
	if err != nil {
		logger.Error().Err(err).Send()
		return
	}
	err = jpool.Call(weka.JrpcHostList, struct{}{}, &hostsApiList)
	if err != nil {
		logger.Error().Err(err).Send()
		return
	}

	if !isMBC(hostsApiList) {
		err = fmt.Errorf("this wekactl version supports only multi backend constainer cluster")
		logger.Error().Err(err).Send()
		return
	}

	if info.Role == "backend" {
		err = jpool.Call(weka.JrpcDrivesList, struct{}{}, &driveApiList)
		if err != nil {
			logger.Error().Err(err).Send()
			return
		}
	}
	err = jpool.Call(weka.JrpcNodeList, struct{}{}, &nodeApiList)
	if err != nil {
		logger.Error().Err(err).Send()
		return
	}

	hosts := make(hostsMap)
	for hostId, host := range hostsApiList {
		hosts[hostId] = hostInfo{
			Host:   host,
			id:     hostId,
			drives: driveMap{},
			nodes:  nodeMap{},
		}
	}
	for driveId, drive := range driveApiList {
		if _, ok := hosts[drive.HostId]; ok {
			hosts[drive.HostId].drives[driveId] = drive
		}
	}

	for nodeId, node := range nodeApiList {
		if _, ok := hosts[node.HostId]; ok {
			hosts[node.HostId].nodes[nodeId] = node
		}
	}

	drivesMap := make(machinesDriveMap)
	for _, host := range hosts {
		if strings2.Contains(host.ContainerName, "drive") {
			if _, ok := drivesMap[host.HostIp]; ok {
				err = fmt.Errorf(
					"more then one drive container found for machine %s: %s %s",
					host.HostIp, host.id, drivesMap[host.HostIp].id)
				return
			}
			drivesMap[host.HostIp] = host
			if len(host.drives) == 0 {
				logger.Warn().Msgf("No drives found for drive host %s:%s", host.HostIp, host.id)
			}
		}
	}

	var hostsList []hostInfo
	var inactiveHosts []hostInfo
	var downHosts []hostInfo

	inactiveOrDownHostsIps := make(map[string]types.Nilt)
	for _, host := range hosts {
		if _, ok := inactiveOrDownHostsIps[host.HostIp]; ok {
			continue
		}

		driveHost, err2 := getMachineDriveHost(drivesMap, host.HostIp)
		if err2 != nil {
			err = err2
			logger.Error().Err(err).Send()
			return
		}

		switch host.State {
		case "INACTIVE":
			hostIds := getMachineContainersHostIds(hostsApiList, host)
			if host.belongsToHgIpBased(info.Instances) {
				if isMachineInactive(hostsApiList, hostIds) {
					logger.Info().Msgf("Inactive machine found: %s", host.HostIp)
					inactiveHosts = append(inactiveHosts, driveHost)
					inactiveOrDownHostsIps[host.HostIp] = types.Nilv
				} else {
					hostsList = append(hostsList, host)
				}
				continue
			} else {
				if info.Role == "backend" {
					logger.Info().Msgf("host %s is inactive and does not belong to HG, removing from cluster", host.id)
					inactiveHosts = append(inactiveHosts, driveHost)
					inactiveOrDownHostsIps[host.HostIp] = types.Nilv
					continue
				}
			}
		default:
			if host.belongsToHgIpBased(info.Instances) {
				hostsList = append(hostsList, host)
				continue
			}
		}

		switch host.Status {
		case "DOWN":
			logger.Info().Msgf("found down host %s %s %s", host.id, host.Aws.InstanceId, host.HostIp)
			if info.Role == "backend" {
				if host.State != "INACTIVE" && host.managementTimedOut(downKickOutTimeout) {
					logger.Info().Msgf("host %s is still active but down for too long, kicking out", host.id)
					downHosts = append(downHosts, driveHost)
					inactiveOrDownHostsIps[host.HostIp] = types.Nilv
					continue
				}
			}
		}

	}

	calculateHostsState(ctx, hostsList)

	sort.Slice(hostsList, func(i, j int) bool {
		// Giving priority to disks to hosts with disk being removed
		// Then hosts with disks not in active state
		// Then hosts sorted by add time
		a := hostsList[i]
		b := hostsList[j]
		if a.scaleState < b.scaleState {
			return true
		}
		if a.scaleState > b.scaleState {
			return false
		}
		if a.numNotHealthyDrives() > b.numNotHealthyDrives() {
			return true
		}
		if a.numNotHealthyDrives() < b.numNotHealthyDrives() {
			return false
		}
		return a.AddedTime.Before(b.AddedTime)
	})

	removeInactive(ctx, hostsApiList, inactiveHosts, jpool, info.Instances, &response, hosts)
	removeOldDrives(ctx, driveApiList, jpool, &response)

	driveContainers, err := getDriveContainers(hostsList)
	if err != nil {
		logger.Error().Err(err).Send()
		return
	}
	machinesNumber := len(driveContainers)
	logger.Info().Msgf("Machines number:%d, Desired number:%d", machinesNumber, info.DesiredCapacity)
	numToDeactivate := getNumToDeactivate(ctx, hostsList, info.DesiredCapacity)

	for _, host := range driveContainers[:numToDeactivate] {
		deactivateHost(ctx, jpool, hostsApiList, &response, host)
	}

	for _, host := range downHosts {
		deactivateHost(ctx, jpool, hostsApiList, &response, host)
	}

	for _, host := range hostsList {
		response.Hosts = append(response.Hosts, protocol.ScaleResponseHost{
			InstanceId: host.Aws.InstanceId,
			PrivateIp:  host.HostIp,
			State:      host.State,
			AddedTime:  host.AddedTime,
			HostId:     host.id,
		})
	}

	return
}
