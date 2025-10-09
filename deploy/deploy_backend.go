package deploy

import (
	"fmt"
	"strings"

	"github.com/weka/go-cloud-lib/bash_functions"
	"github.com/weka/go-cloud-lib/functions_def"

	"github.com/lithammer/dedent"
)

func (d *DeployScriptGenerator) GetBackendDeployScript() string {
	wekaInstallScript := d.GetWekaInstallScript()
	protectFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Protect)
	clusterizeFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Clusterize)
	reportFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Report)

	getCoreIdsFunc := bash_functions.GetCoreIds()
	getNetStrForDpdkFunc := bash_functions.GetNetStrForDpdk()
	getAllInterfaces := bash_functions.GetAllInterfaces()
	gateways := strings.Join(d.Params.Gateways, " ")
	failureDomainCmd := bash_functions.GetHashedPrivateIpBashCmd()

	template := `
	#!/bin/bash
	set -ex
	VM=%s
	FAILURE_DOMAIN=$(%s)
	COMPUTE_MEMORY=%s
	COMPUTE_CONTAINER_CORES_NUM=%d
	FRONTEND_CONTAINER_CORES_NUM=%d
	DRIVE_CONTAINER_CORES_NUM=%d
	NICS_NUM=%s
	NVMES_NUM=%d
	INSTALL_DPDK=%t
	GATEWAYS="%s"

	# clusterize function definition
	%s

	# protect function definition (if any)
	%s

	# report function definition
	%s

	# get_core_ids bash function definition
	%s

	# getNetStrForDpdk bash function definition
	%s

	# getAllInterfaces bash function definition
	%s

	# deviceNameCmd
	wekaiosw_device="%s"
	# wekio partition setup
	%s

	# install script
	%s

	weka local stop
	weka local rm default --force

	# weka containers setup
	get_core_ids $DRIVE_CONTAINER_CORES_NUM drive_core_ids
	get_core_ids $COMPUTE_CONTAINER_CORES_NUM compute_core_ids

	total_containers=2

	getAllInterfaces

	if [[ $INSTALL_DPDK == true ]]; then
		getNetStrForDpdk 1 $((1+$DRIVE_CONTAINER_CORES_NUM)) "$GATEWAYS"
		sudo weka local setup container --name drives0 --base-port 14000 --cores $DRIVE_CONTAINER_CORES_NUM --no-frontends --drives-dedicated-cores $DRIVE_CONTAINER_CORES_NUM --failure-domain $FAILURE_DOMAIN --core-ids $drive_core_ids --dedicate $net
		getNetStrForDpdk $((1+$DRIVE_CONTAINER_CORES_NUM)) $((1+$DRIVE_CONTAINER_CORES_NUM+$COMPUTE_CONTAINER_CORES_NUM)) "$GATEWAYS"
		sudo weka local setup container --name compute0 --base-port 15000 --cores $COMPUTE_CONTAINER_CORES_NUM --no-frontends --compute-dedicated-cores $COMPUTE_CONTAINER_CORES_NUM  --memory $COMPUTE_MEMORY --failure-domain $FAILURE_DOMAIN --core-ids $compute_core_ids --dedicate $net
	else
		sudo weka local setup container --name drives0 --base-port 14000 --cores $DRIVE_CONTAINER_CORES_NUM --no-frontends --drives-dedicated-cores $DRIVE_CONTAINER_CORES_NUM --failure-domain $FAILURE_DOMAIN --core-ids $drive_core_ids --dedicate --net udp
		sudo weka local setup container --name compute0 --base-port 15000 --cores $COMPUTE_CONTAINER_CORES_NUM --no-frontends --compute-dedicated-cores $COMPUTE_CONTAINER_CORES_NUM  --memory $COMPUTE_MEMORY --failure-domain $FAILURE_DOMAIN --core-ids $compute_core_ids --dedicate --net udp
	fi

	if [[ $FRONTEND_CONTAINER_CORES_NUM -gt 0 ]]; then
		total_containers=3
		get_core_ids $FRONTEND_CONTAINER_CORES_NUM frontend_core_ids
		if [[ $INSTALL_DPDK == true ]]; then
			getNetStrForDpdk $((1+$DRIVE_CONTAINER_CORES_NUM+$COMPUTE_CONTAINER_CORES_NUM)) $NICS_NUM "$GATEWAYS"
			sudo weka local setup container --name frontend0 --base-port 16000 --cores $FRONTEND_CONTAINER_CORES_NUM --frontend-dedicated-cores $FRONTEND_CONTAINER_CORES_NUM --allow-protocols true --failure-domain $FAILURE_DOMAIN --core-ids $frontend_core_ids --dedicate $net
		else
			sudo weka local setup container --name frontend0 --base-port 16000 --cores $FRONTEND_CONTAINER_CORES_NUM --frontend-dedicated-cores $FRONTEND_CONTAINER_CORES_NUM --allow-protocols true --failure-domain $FAILURE_DOMAIN --core-ids $frontend_core_ids --dedicate --net udp
		fi
	fi

	# should not call 'clusterize' until all 2/3 containers are up
	ready_containers=0
	while [[ $ready_containers -ne $total_containers ]];
	do
		sleep 10
		ready_containers=$( weka local ps | grep -i 'running' | wc -l )
		echo "Running containers: $ready_containers"
	done

	protect "{\"vm\": \"$VM\"}"
	
	report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"$PROTOCOL\", \"type\": \"progress\", \"message\": \"Weka containers are ready\"}"

	mkdir -p /opt/weka/tmp
	cat >/opt/weka/tmp/find_drives.py <<EOL%sEOL
	devices=$(weka local run --container compute0 bash -ce 'wapi machine-query-info --info-types=DISKS -J | python3 /opt/weka/tmp/find_drives.py')
	devices=($devices)
	for (( d=0; d<$NVMES_NUM; d++ )); do
		while ! lsblk "${devices[$d]}" >/dev/null 2>&1; do
			echo "waiting for nvme to be ready"
			sleep 5
		done
	done

	clusterize "{\"name\": \"$VM\"}" > /tmp/clusterize.sh
	chmod +x /tmp/clusterize.sh
	/tmp/clusterize.sh 2>&1 | tee /tmp/weka_clusterization.log
	`
	script := fmt.Sprintf(
		template, d.Params.VMName, failureDomainCmd, d.Params.InstanceParams.ComputeMemory, d.Params.InstanceParams.Compute,
		d.Params.InstanceParams.Frontend, d.Params.InstanceParams.Drive, d.Params.NicsNum, d.Params.NvmesNum, d.Params.InstallDpdk,
		gateways, clusterizeFunc, protectFunc, reportFunc, getCoreIdsFunc, getNetStrForDpdkFunc, getAllInterfaces, d.DeviceNameCmd,
		bash_functions.GetWekaPartitionScript(), wekaInstallScript, d.Params.FindDrivesScript,
	)
	return dedent.Dedent(script)
}
