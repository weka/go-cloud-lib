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
	"github.com/weka/go-cloud-lib/weka_events"

	"github.com/google/uuid"
)

type hostState int

const unhealthyDeactivateTimeout = 120 * time.Minute

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

type EventReason string

const (
	ScaleDownEvent       EventReason = "scale down"
	InactiveMachineEvent EventReason = "inactive machine"
	DownMachineEvent     EventReason = "down machine"
)

type deactivateEventInfo struct {
	currentSize int
	desiredSize int
	reason      EventReason
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

func (host hostInfo) managementTimedOut(ctx context.Context, timeout time.Duration) bool {
	logger := logging.LoggerFromCtx(ctx)
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
		logger.Info().Msgf("Node %s status: %s, period: %s, timeout: %s", nodeId.String(), node.Status, time.Since(period), timeout)
		if node.Status == "DOWN" && time.Since(period) > timeout {
			return true
		}
	}
	return false
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
		if host.Mode == "client" {
			logger.Warn().Msgf("Skipping client host scaleState check %s:%s", host.HostIp, host.id)
			continue
		}
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

	if host.Mode == "client" {
		logger.Warn().Msgf("Skipping client host state derive %s:%s", host.HostIp, host.id)
		return HEALTHY
	}

	if strings2.Contains(host.ContainerName, "drive") && host.allDisksBeingRemoved() {
		logger.Info().Msgf("Marking %s as deactivating due to unhealthy disks", host.id.String())
		return DEACTIVATING
	}
	if strings.AnyOf(host.State, "DEACTIVATING", "REMOVING", "INACTIVE") {
		return DEACTIVATING
	}
	if strings.AnyOf(host.Status, "DOWN", "DEGRADED") && host.managementTimedOut(ctx, unhealthyDeactivateTimeout) {
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

func removeInactive(ctx context.Context, inactiveMachines map[string][]hostInfo, jpool *jrpc.Pool, instances []protocol.HgInstance, p *protocol.ScaleResponse) {
	logger := logging.LoggerFromCtx(ctx)
	for hostIp, machineHosts := range inactiveMachines {
		jpool.Drop(hostIp)
		for _, host := range machineHosts {
			if host.State != "INACTIVE" {
				logger.Fatal().Msgf("Machine %s passed for removal has ACTIVE container: %s", host.HostIp, host.id)
			}
			logger.Info().Msgf("Removing machine with inactive container/s: %s", host.HostIp)

			removeFailure := false
			readyForRemove := true
			if host.Status == "INACTIVE" {
				err := removeContainer(ctx, jpool, host.id.Int(), p)
				if err != nil {
					removeFailure = true
				}
			} else {
				readyForRemove = false
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
	}

	logger.Info().Msgf("Instances set to termination %s", p.ToTerminate)
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

func getMachineToHostMap(ctx context.Context, hosts hostsMap) map[string][]hostInfo {
	logger := logging.LoggerFromCtx(ctx)
	machineToHostMap := make(map[string][]hostInfo)

	for _, host := range hosts {
		if host.Mode == "backend" {
			machineToHostMap[host.HostIp] = append(machineToHostMap[host.HostIp], host)
		}
	}

	for hostIp, machineHosts := range machineToHostMap {
		machineIdentifiers := make(map[string]types.Nilt)
		if len(machineHosts) > 3 {
			logger.Fatal().Msgf("Found more than 3 backend containers for host %s", hostIp)
		}
		for _, machineHost := range machineHosts {
			machineIdentifiers[machineHost.MachineIdentifier] = types.Nilv
		}
		if len(machineIdentifiers) > 1 {
			logger.Fatal().Msgf("Found more than 1 machine identifier for host %s", hostIp)
		}
	}

	return machineToHostMap
}

func allContainersInactive(hosts []hostInfo) bool {
	for _, host := range hosts {
		if host.State != "INACTIVE" {
			return false
		}
	}
	return true
}

func allContainersDownOrInactive(hosts []hostInfo) bool {
	for _, host := range hosts {
		if host.Status != "DOWN" && host.State != "INACTIVE" {
			return false
		}
	}
	return true
}

func deactivate(ctx context.Context, jpool *jrpc.Pool, hostIp string, hostIds []weka.HostId, response *protocol.ScaleResponse) {
	logger := logging.LoggerFromCtx(ctx)
	err := jpool.Call(weka.JrpcDeactivateHosts, types.JsonDict{
		"host_ids":                 hostIds,
		"skip_resource_validation": false,
	}, nil)
	if err != nil {
		logger.Error().Err(err).Send()
		response.AddTransientError(err, "deactivateHost")
	} else {
		jpool.Drop(hostIp)
	}
	return
}

func deactivateMachine(ctx context.Context, jpool *jrpc.Pool, machineHosts []hostInfo, response *protocol.ScaleResponse, eventParams *deactivateEventInfo, nfsHostsMap map[weka.HostId]NfsHost) {
	logger := logging.LoggerFromCtx(ctx)
	var hostIds []weka.HostId

	for _, host := range machineHosts {
		hostIds = append(hostIds, host.id)
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
	}

	logger.Info().Msgf("Trying to deactivate machine %s :%s", machineHosts[0].HostIp, hostIds)
	machineTxt := "weka backend machine"
	if len(nfsHostsMap) > 0 {
		machineTxt = "nfs backend machine"
		for _, host := range machineHosts {
			if _, ok := nfsHostsMap[host.id]; !ok {
				continue
			}
			err1 := jpool.Call(weka.JrpcinterfaceGroupDeletePort, types.JsonDict{
				"name":    nfsHostsMap[host.id].InterfaceGroupName,
				"host_id": nfsHostsMap[host.id].HostId.String(),
				"port":    nfsHostsMap[host.id].Port,
			}, nil)
			if err1 != nil {
				logger.Error().Err(err1).Send()
				response.AddTransientError(err1, "interfaceGroupDeletePort")
			}
		}
	}

	// send weka event
	if eventParams.reason == ScaleDownEvent {
		message := fmt.Sprintf(
			"Trying to deactivate Down %s %s",
			machineTxt,
			machineHosts[0].HostIp,
		)

		_ = weka_events.EmitCustomEventUsingJPool(ctx, message, jpool)
	}
	message := fmt.Sprintf(
		"Trying to deactivate %s %s. Desired size: %d, current size: %d, reason: %s.",
		machineTxt,
		machineHosts[0].HostIp,
		eventParams.desiredSize,
		eventParams.currentSize,
		eventParams.reason,
	)

	_ = weka_events.EmitCustomEventUsingJPool(ctx, message, jpool)

	deactivate(ctx, jpool, machineHosts[0].HostIp, hostIds, response)

}

func isMBC(hostsApiList weka.HostListResponse) bool {
	for _, host := range hostsApiList {
		if host.Mode == "backend" && strings2.Contains(host.ContainerName, "drive") {
			return true
		}
	}
	return false
}

type hostsMap map[weka.HostId]hostInfo
type HostGroupType string

type NfsHost struct {
	InterfaceGroupName string
	HostId             weka.HostId
	Port               string
}

func GetNfsHostsMap(ctx context.Context, jpool *jrpc.Pool) (nfsHostsMap map[weka.HostId]NfsHost, err error) {
	logger := logging.LoggerFromCtx(ctx)
	nfsHostsMap = make(map[weka.HostId]NfsHost)
	interfaceGroupList := weka.InterfaceGroupListResponse{}
	err = jpool.Call(weka.JrpcInterfaceGroupList, struct{}{}, &interfaceGroupList)
	if err != nil {
		logger.Error().Err(err).Send()
		return
	}
	for _, interfaceGroup := range interfaceGroupList {
		if interfaceGroup.Type == "NFS" {
			for _, portHost := range interfaceGroup.Ports {
				logger.Debug().Msgf("Found NFS port host %s", portHost.HostId)
				nfsHostsMap[portHost.HostId] = NfsHost{
					InterfaceGroupName: interfaceGroup.Name,
					HostId:             portHost.HostId,
					Port:               portHost.Port,
				}
			}
		}
	}

	return
}

func getHostGroupHosts(hosts map[weka.HostId]hostInfo, instances []protocol.HgInstance) map[weka.HostId]hostInfo {
	hgHosts := make(map[weka.HostId]hostInfo)
	for hostId, host := range hosts {
		if host.belongsToHgIpBased(instances) {
			hgHosts[hostId] = host
		}
	}
	return hgHosts
}

func getNfsHosts(hosts map[weka.HostId]hostInfo, nfsHostsMap map[weka.HostId]NfsHost) map[weka.HostId]hostInfo {
	nfsHosts := make(map[weka.HostId]hostInfo)
	for hostId, host := range hosts {
		if _, ok := nfsHostsMap[hostId]; ok {
			nfsHosts[hostId] = host
		}
	}
	return nfsHosts
}

func getLeftoverHosts(hosts map[weka.HostId]hostInfo, instances []protocol.HgInstance) map[weka.HostId]hostInfo {
	hgHosts := make(map[weka.HostId]hostInfo)
	for hostId, host := range hosts {
		if !host.belongsToHgIpBased(instances) {
			hgHosts[hostId] = host
		}
	}
	return hgHosts
}

func ScaleDown(ctx context.Context, info protocol.HostGroupInfoResponse) (response protocol.ScaleResponse, err error) {
	logger := logging.LoggerFromCtx(ctx)
	logger.Info().Msg("Running scale down...")
	response.Version = protocol.Version

	if info.Role != "backend" {
		logger.Info().Msg("Skipping scale down, not a backend")
	}

	jrpcBuilder := func(ip string) *jrpc.BaseClient {
		return connectors.NewJrpcClient(ctx, ip, weka.ManagementJrpcPort, info.Username, info.Password)
	}
	ips := info.BackendIps
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
	interfaceGroupList := weka.InterfaceGroupListResponse{}

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

	err = jpool.Call(weka.JrpcDrivesList, struct{}{}, &driveApiList)
	if err != nil {
		logger.Error().Err(err).Send()
		return
	}

	err = jpool.Call(weka.JrpcNodeList, struct{}{}, &nodeApiList)
	if err != nil {
		logger.Error().Err(err).Send()
		return
	}

	err = jpool.Call(weka.JrpcInterfaceGroupList, struct{}{}, &interfaceGroupList)
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

	removeOldDrives(ctx, driveApiList, jpool, &response)

	var errs []error
	hgHosts := getHostGroupHosts(hosts, info.WekaBackendInstances)
	logger.Info().Msg("Running scale down on weka backends...")
	err = ScaleHgDown(ctx, jpool, info.WekaBackendInstances, hgHosts, info.WekaBackendsDesiredCapacity, &response, nil)
	if err != nil {
		errs = append(errs, err)
	}

	nfsHostsMap, err := GetNfsHostsMap(ctx, jpool)
	leftOverNfsHosts := make(map[weka.HostId]hostInfo)
	if err != nil {
		response.AddTransientError(err, "GetNfsHostsMap")
	} else {
		logger.Info().Msg("Running scale down on NFS hosts...")
		hgHosts = getHostGroupHosts(hosts, info.NfsBackendInstances)
		nfsHosts := getNfsHosts(hgHosts, nfsHostsMap)
		for hostId, host := range hgHosts {
			if _, ok := nfsHosts[hostId]; !ok {
				logger.Info().Msgf("Host %s:%s is not in NFS interface group", host.HostIp, host.id)
				leftOverNfsHosts[hostId] = host
			}
		}
		err2 := ScaleHgDown(ctx, jpool, info.NfsBackendInstances, nfsHosts, info.NFSBackendsDesiredCapacity, &response, nfsHostsMap)
		if err2 != nil {
			errs = append(errs, err2)
		}
	}

	instances := info.WekaBackendInstances
	instances = append(instances, info.NfsBackendInstances...)
	hgHosts = getLeftoverHosts(hosts, instances)
	for hostId, host := range leftOverNfsHosts {
		hgHosts[hostId] = host
	}
	handleLeftOverHosts(ctx, jpool, instances, hgHosts, -1, &response, nfsHostsMap, info.DownBackendsRemovalTimeout)

	validateDelta(ctx, &response, jpool, instances)

	if len(errs) > 0 {
		err = fmt.Errorf("scale down failed: %v", errs)
	}

	return
}

func getHostIdsString(hosts hostsMap) (hostIds string) {
	for hostId := range hosts {
		hostIds += hostId.String() + " "
	}
	return
}

func ScaleHgDown(ctx context.Context, jpool *jrpc.Pool, instances []protocol.HgInstance, hosts hostsMap, desiredCapacity int, response *protocol.ScaleResponse, nfsHostsMap map[weka.HostId]NfsHost) (err error) {
	/*
		Code in here based on following logic:

		A - Fully active, healthy
		T - Desired target number
		U - Unhealthy, we want to remove it for whatever reason. DOWN host, FAILED drive, so on
		D - Drives/hosts being deactivated
		NEW_D - Decision to start deactivating, i.e transition to D, basing on U. Never more than 2 for U

		NEW_D = func(A, U, T, D)

		NEW_D = max(A+U+D-T, min(2-D, U), 0)
	*/
	logger := logging.LoggerFromCtx(ctx)
	if len(nfsHostsMap) > 0 {
		logger.Info().Msgf("Running NFS HG scale down (%s)", getHostIdsString(hosts))
	} else {
		logger.Info().Msgf("Running HG scale down (%s)", getHostIdsString(hosts))
	}

	var hostsList []hostInfo
	var machinesIps []string
	machinesIpsMap := make(map[string]types.Nilt)

	machineToHostMap := getMachineToHostMap(ctx, hosts) // protocol gws + weka backends
	inactiveMachines := make(map[string][]hostInfo)
	inactiveOrDownHostsIps := make(map[string]types.Nilt)
	for _, host := range hosts {
		if host.Mode == "client" {
			logger.Info().Msgf("Skipping client host %s:%s", host.HostIp, host.id)
			continue
		}

		if _, ok := inactiveOrDownHostsIps[host.HostIp]; ok {
			continue
		}

		switch host.State {
		case "INACTIVE":
			if allContainersInactive(machineToHostMap[host.HostIp]) {
				logger.Info().Msgf("Inactive machine found: %s", host.HostIp)
				inactiveMachines[host.HostIp] = machineToHostMap[host.HostIp]
				inactiveOrDownHostsIps[host.HostIp] = types.Nilv
			} else {
				hostsList = append(hostsList, host)
			}

		default:
			hostsList = append(hostsList, host)
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

	for _, host := range hostsList {
		if _, ok := machinesIpsMap[host.HostIp]; !ok {
			machinesIpsMap[host.HostIp] = types.Nilv
			machinesIps = append(machinesIps, host.HostIp)
		}
	}
	backendMachinesNumber := len(machineToHostMap)
	logger.Info().Msgf("Backend machines number:%d Desired capacity:%d", backendMachinesNumber, desiredCapacity)

	eventParams := deactivateEventInfo{
		currentSize: backendMachinesNumber,
		desiredSize: desiredCapacity,
		reason:      "inactive machine",
	}

	removeInactive(ctx, inactiveMachines, jpool, instances, response)

	numToDeactivate := getNumToDeactivate(ctx, hostsList, desiredCapacity)
	for _, hostIp := range machinesIps[:numToDeactivate] {
		eventParams.reason = ScaleDownEvent
		deactivateMachine(ctx, jpool, machineToHostMap[hostIp], response, &eventParams, nfsHostsMap)
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

func validateDelta(ctx context.Context, response *protocol.ScaleResponse, jpool *jrpc.Pool, instances []protocol.HgInstance) {
	logger := logging.LoggerFromCtx(ctx)
	logger.Info().Msgf("Validating delta")

	hostsApiList := weka.HostListResponse{}
	err := jpool.Call(weka.JrpcHostList, struct{}{}, &hostsApiList)
	if err != nil {
		logger.Error().Err(err).Send()
		return
	}

	systemContainersIps := map[string]types.Nilt{}
	responseContainerIps := map[string]types.Nilt{}
	cloudIps := map[string]types.Nilt{}
	deltaMap := map[string]types.Nilt{}

	for _, host := range hostsApiList {
		systemContainersIps[host.HostIp] = types.Nilv
	}

	for _, host := range response.Hosts {
		responseContainerIps[host.PrivateIp] = types.Nilv
	}

	for _, instance := range instances {
		cloudIps[instance.PrivateIp] = types.Nilv
	}

CloudIps:
	for ip := range cloudIps {
		if _, ok := responseContainerIps[ip]; !ok {
			deltaMap[ip] = types.Nilv
			continue CloudIps
		}
		for _, toTerminate := range response.ToTerminate {
			if toTerminate.PrivateIp == ip {
				deltaMap[ip] = types.Nilv
				continue CloudIps
			}
		}
	}

	var deltaIps []string
	for ip := range deltaMap {
		deltaIps = append(deltaIps, ip)
	}

	logger.Info().Msgf("delta ips for termination: %v", deltaIps)

	for terminatingIp := range deltaMap {
		for hostId, host := range hostsApiList {
			if host.HostIp == terminatingIp && host.State != "INACTIVE" && host.State != "REMOVING" {
				if host.Status == "DOWN" && host.Mode == "client" && host.AutoRemoveTimeout > 0 {
					logger.Warn().Msgf("Detected IP collision between client and backend with ip %s, ignoring as client is down ", host.HostIp)
					continue
				}
				hostDetails := fmt.Sprintf("%s:%s:%s:%s:%s", host.Mode, hostId, host.ContainerName, host.Status, host.State)
				logger.Fatal().Msgf("Aborting scale down. Instance with IP that exists in system and belongs to non-inactive container %s was targeted for termination: %s", hostDetails, host.HostIp)
			}
		}
	}
}

func handleLeftOverHosts(ctx context.Context, jpool *jrpc.Pool, instances []protocol.HgInstance, hosts hostsMap, desiredCapacity int, response *protocol.ScaleResponse, nfsHostsMap map[weka.HostId]NfsHost, downKickOutTimeout time.Duration) {
	logger := logging.LoggerFromCtx(ctx)
	logger.Info().Msgf("Handling leftover hosts (%s)", getHostIdsString(hosts))

	var downMachines []string
	var hostsList []hostInfo
	inactiveMachines := make(map[string][]hostInfo)
	inactiveOrDownHostsIps := make(map[string]types.Nilt)
	machineToHostMap := getMachineToHostMap(ctx, hosts)
	for _, host := range hosts {
		if host.Mode == "client" {
			continue
		}

		if _, ok := inactiveOrDownHostsIps[host.HostIp]; ok {
			continue
		}

		if host.State == "INACTIVE" {
			logger.Info().Msgf("host %s is inactive and does not belong to HG", host.id)
			if allContainersInactive(machineToHostMap[host.HostIp]) {
				inactiveMachines[host.HostIp] = machineToHostMap[host.HostIp]
				inactiveOrDownHostsIps[host.HostIp] = types.Nilv
			}
		} else if host.Status == "DOWN" {
			logger.Info().Msgf("found down host %s %s %s", host.id, host.Aws.InstanceId, host.HostIp)
			if host.managementTimedOut(ctx, downKickOutTimeout) {
				if !allContainersDownOrInactive(machineToHostMap[host.HostIp]) {
					response.TransientErrors = append(
						response.TransientErrors,
						fmt.Sprintf("host %s is down but not all containers on the machine are down", host.id),
					)
					continue
				}

				logger.Info().Msgf("host %s is still active but down for too long, kicking out", host.id)
				downMachines = append(downMachines, host.HostIp)
				inactiveOrDownHostsIps[host.HostIp] = types.Nilv
			} else {
				hostsList = append(hostsList, host)
			}
		} else {
			hostsList = append(hostsList, host)
			logger.Info().Msgf("host %s:%s is active and does not belong to HG", host.HostIp, host.id)
		}
	}
	removeInactive(ctx, inactiveMachines, jpool, instances, response)
	eventParams := deactivateEventInfo{
		currentSize: desiredCapacity,
		desiredSize: desiredCapacity,
		reason:      InactiveMachineEvent,
	}

	for _, hostIp := range downMachines {
		eventParams.reason = DownMachineEvent
		deactivateMachine(ctx, jpool, machineToHostMap[hostIp], response, &eventParams, nfsHostsMap)
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
}
