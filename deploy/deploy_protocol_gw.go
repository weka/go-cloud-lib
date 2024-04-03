package deploy

import (
	"fmt"
	"github.com/lithammer/dedent"
	"github.com/weka/go-cloud-lib/bash_functions"
	"github.com/weka/go-cloud-lib/functions_def"
	"strings"
)

func (d *DeployScriptGenerator) GetBaseProtocolGWDeployScript() string {
	wekaInstallScript := d.GetWekaInstallScript()
	protectFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Protect)
	statusFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Status)
	fetchFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Fetch)

	getCoreIdsFunc := bash_functions.GetCoreIds()
	getNetStrForDpdkFunc := bash_functions.GetNetStrForDpdk()
	gateways := strings.Join(d.Params.Gateways, " ")

	wekaRestFunc := bash_functions.WekaRestFunction()
	setBackendIpFunc := bash_functions.SetBackendIpFunction()

	template := `
	#!/bin/bash
	VM=%s
	FRONTEND_CONTAINER_CORES_NUM=%d
	NICS_NUM=%s
	INSTALL_DPDK=%t
	LOAD_BALANCER_IP="%s"
	SECONDARY_IPS_NUM=%d
	GATEWAYS="%s"

	# protect function definition (if any)
	%s

	# fetch function definition
	%s

	# status function definition
	%s

	# get_core_ids bash function definition
	%s

	# getNetStrForDpdk bash function definition
	%s

	# deviceNameCmd
	wekaiosw_device="%s"
	# wekio partition setup
	%s

	# install script
	%s

	# weka rest function definition
	%s

	# set_backend_ip bash function definition
	%s

	set +x
	fetch_result=$(fetch "{\"fetch_weka_credentials\": true}")
	export WEKA_USERNAME="$(echo $fetch_result | jq -r .username)"
	export WEKA_PASSWORD="$(echo $fetch_result | jq -r .password)"
	set -x

	weka local stop
	weka local rm default --force

	# weka frontend setup
	get_core_ids $FRONTEND_CONTAINER_CORES_NUM frontend_core_ids

	clusterized=$(status "{\"type\": \"status\"}" | jq .clusterized)
	while [ "$clusterized" != "true" ];
	do
		sleep 10
		clusterized=$(status "{\"type\": \"status\"}" | jq .clusterized)
		echo "Clusterized: $clusterized, going to sleep for 10 seconds"
	done

	ips_str=$(fetch | jq -r '.backend_ips | join(",")')

	if [[ $INSTALL_DPDK == true ]]; then
		getNetStrForDpdk 1 $(($FRONTEND_CONTAINER_CORES_NUM + 1)) "$GATEWAYS"
		weka local setup container --name frontend0 --base-port 14000 --cores $FRONTEND_CONTAINER_CORES_NUM --frontend-dedicated-cores $FRONTEND_CONTAINER_CORES_NUM --allow-protocols true --core-ids $frontend_core_ids $net --dedicate --join-ips $ips_str
	else
		weka local setup container --name frontend0 --base-port 14000 --cores $FRONTEND_CONTAINER_CORES_NUM --frontend-dedicated-cores $FRONTEND_CONTAINER_CORES_NUM --allow-protocols true --core-ids $frontend_core_ids  --dedicate --join-ips $ips_str
	fi

	ready_containers=0
	while [[ $ready_containers -ne 1 ]];
	do
		sleep 10
		ready_containers=$( weka local ps | grep -i 'running' | wc -l )
		echo "Running containers: $ready_containers"
	done

	protect "{\"vm\": \"$VM\"}"
	set +x
	weka user login $WEKA_USERNAME $WEKA_PASSWORD
	set -x
	weka local ps

	set_backend_ip

	current_mngmnt_ip=$(weka local resources | grep 'Management IPs' | awk '{print $NF}')
	nic_name=$(ip -o -f inet addr show | grep "$current_mngmnt_ip/"| awk '{print $2}')

	# set container_uid with frontend0 container uid
	max_retries=12 # 12 * 10 = 2 minutes
	for ((i=0; i<max_retries; i++)); do
		container_uid=$(weka_rest containers | jq .data | jq -r --arg HOSTNAME "$HOSTNAME" '.[] | select ( .container_name == "frontend0" and .status == "UP" and .hostname == $HOSTNAME )' | jq -r '.uid')
		if [ -n "$container_uid" ]; then
			echo "$(date -u): frontend0 container uid: $container_uid"
			break
		fi
		echo "$(date -u): waiting for frontend0 container to be up"
		sleep 10
	done
	if [ -z "$container_uid" ]; then
		echo "$(date -u): Failed to get the frontend0 container UID."
		exit 1
	fi

	if [[ "$PROXY_URL" ]]; then
		sed -i 's/force_no_proxy=false/force_no_proxy=true/g' /etc/wekaio/service.conf
		systemctl restart weka-agent
	fi
	`
	script := fmt.Sprintf(
		template,
		d.Params.VMName,
		d.Params.NFSProtocolGatewayFeCoresNum,
		d.Params.NicsNum,
		d.Params.InstallDpdk,
		d.Params.LoadBalancerIP,
		d.Params.NFSSecondaryIpsNum,
		gateways,
		protectFunc,
		fetchFunc,
		statusFunc,
		getCoreIdsFunc,
		getNetStrForDpdkFunc,
		d.DeviceNameCmd,
		bash_functions.GetWekaPartitionScript(),
		wekaInstallScript,
		wekaRestFunc,
		setBackendIpFunc,
	)
	return dedent.Dedent(script)
}

func (d *DeployScriptGenerator) GetProtocolGWDeployScript() string {
	baseDeploymentScript := d.GetBaseProtocolGWDeployScript()
	clusterizeFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Clusterize)

	template := `
	PROTOCOL="%s"

	# clusterize function definition
	%s

	clusterize "{\"name\": \"$VM\", \"protocol\": \"$PROTOCOL\", \"container_uid\": \"$container_uid\", \"nic_name\": \"$nic_name\"}" > /tmp/clusterize.sh
	chmod +x /tmp/clusterize.sh
	/tmp/clusterize.sh 2>&1 | tee /tmp/weka_clusterization.log
	`
	script := fmt.Sprintf(
		template,
		d.Params.Protocol,
		clusterizeFunc,
	)

	return baseDeploymentScript + dedent.Dedent(script)
}
