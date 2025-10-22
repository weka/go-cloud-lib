package deploy

import (
	"fmt"
	"strings"

	"github.com/lithammer/dedent"
	"github.com/weka/go-cloud-lib/bash_functions"
	"github.com/weka/go-cloud-lib/functions_def"
)

func (d *DeployScriptGenerator) GetBaseProtocolGWDeployScript() string {
	wekaInstallScript := d.GetWekaInstallScript()
	protectFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Protect)
	statusFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Status)
	reportFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Report)
	fetchFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Fetch)

	getCoreIdsFunc := bash_functions.GetCoreIds()
	getNetStrForDpdkFunc := bash_functions.GetNetStrForDpdk()
	getAllInterfaces := bash_functions.GetAllInterfaces()
	gateways := strings.Join(d.Params.Gateways, " ")

	wekaRestFunc := bash_functions.WekaRestFunction()
	setBackendIpFunc := bash_functions.SetBackendIpFunction()

	template := `
	#!/bin/bash
	VM=%s
	FRONTEND_CONTAINER_CORES_NUM=%d
	INSTALL_DPDK=%t
	LOAD_BALANCER_IP="%s"
	SECONDARY_IPS_NUM=%d
	PROTOCOL="%s"
	GATEWAYS="%s"

	# protect function definition (if any)
	%s

	# fetch function definition
	%s

	# status function definition
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

	# weka rest function definition
	%s

	# set_backend_ip bash function definition
	%s

	getAllInterfaces
	# set current management ip
	%s

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

	set +x
	fetch_result=$(fetch "{\"fetch_weka_credentials\": true}")
	export WEKA_USERNAME="$(echo $fetch_result | jq -r .username)"
	export WEKA_PASSWORD="$(echo $fetch_result | jq -r .password)"
	set -x

	# set value for backend_ip variable
	set_backend_ip
	echo "(date -u): backend_ip: $backend_ip"

	if [[ $INSTALL_DPDK == true ]]; then
		getNetStrForDpdk 1 $((1+$FRONTEND_CONTAINER_CORES_NUM)) "$GATEWAYS"
	else
		net=""
	fi

	echo "$(date -u): setting up weka frontend"

	if [ -z "$LOAD_BALANCER_IP" ]; then
		join_ips=$ips_str
	else
		join_ips=$LOAD_BALANCER_IP
	fi

	weka local setup container --name frontend0 --base-port 14000 --cores $FRONTEND_CONTAINER_CORES_NUM --frontend-dedicated-cores $FRONTEND_CONTAINER_CORES_NUM --allow-protocols true --core-ids $frontend_core_ids $net --dedicate --join-ips $join_ips
	
	echo "$(date -u): success to run weka frontend container"

	ready_containers=0
	while [[ $ready_containers -ne 1 ]];
	do
		sleep 10
		ready_containers=$( weka local ps | grep frontend0 | grep -i 'running' | wc -l )
		echo "Running containers: $ready_containers"
	done

	echo "$(date -u): frontend is up"

	protect "{\"vm\": \"$VM\", \"protocol\": \"$PROTOCOL\"}"
	set +x
	echo "$(date -u): try to run weka login command"
	weka user login $WEKA_USERNAME $WEKA_PASSWORD
	echo "$(date -u): success to run weka login command"
	set -x
	weka local ps

	ip -o -4 addr show

	nic_name=$(ip -o -f inet addr show | grep "$current_mngmnt_ip/"| awk '{print $2}')

	echo "$(date -u): starting preparation for protocol setup"

	# set container_uid with frontend0 container uid
	max_retries=30 # 30 * 10 = 5 minutes
	for ((i=0; i<max_retries; i++)); do
		container_uid=$(weka_rest containers | jq .data | jq -r --arg HOSTNAME "$HOSTNAME" '.[] | select ( .container_name == "frontend0" and .status == "UP" and .hostname == $HOSTNAME )' | jq -r '.uid')
		container_id=$(weka_rest containers | jq .data | jq -r --arg HOSTNAME "$HOSTNAME" '.[] | select ( .container_name == "frontend0" and .status == "UP" and .hostname == $HOSTNAME )' | jq -r .id | grep -oP '\d+')
		if [ -n "$container_uid" ]; then
			echo "$(date -u): frontend0 container uid: $container_uid (container id: $container_id)"
			break
		fi
		echo "$(date -u): waiting for frontend0 container to be up"
		sleep 10
	done
	if [ -z "$container_uid" ]; then
		msg="Failed to get the frontend0 container UID."
		echo "$(date -u): $msg"
		report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"$PROTOCOL\", \"type\": \"error\", \"message\": \"$msg\"}"
		exit 1
	fi

	# get real primary ip from cloud metadata
	# NOTE: in Azure there are situations where the primary ip is not shown as primary in ip addr
	primary_ip_cmd="%s"
	if [ -n "$primary_ip_cmd" ]; then
		primary_ip=$(eval $primary_ip_cmd)

		# make primary ip the management ip for the weka container
		if [ "$current_mngmnt_ip" != "$primary_ip" ]; then
			weka cluster container management-ips $container_id $primary_ip
			weka cluster container apply $container_id -f

			# wait for container to be up
			max_retries=12 # 12 * 10 = 2 minutes
			for ((i=0; i<max_retries; i++)); do
				status=$(weka cluster container $container_id | grep $container_id | awk '{print $5}')
				if [ "$status" == "UP" ]; then
					echo "$(date -u): frontend0 container status: $status"
					break
				fi
				echo "$(date -u): waiting for frontend0 container status to be UP, current status: $status"
				sleep 10
			done
			if [ "$status" != "UP" ]; then
				msg="Failed to wait for the frontend0 container status to be UP"
				echo "$(date -u): $msg"
				report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"$PROTOCOL\", \"type\": \"error\", \"message\": \"$msg\"}"
				exit 1
			fi
		fi
	fi

	echo "$(date -u): finished preparation for protocol setup"

	echo "$(date -u): running validation for setting protocol script"
	config_filesystem_name=".config_fs"
	function wait_for_config_fs(){
	  max_retries=30 # 30 * 10 = 5 minutes
	  for (( i=0; i < max_retries; i++ )); do
		if [ "$(weka fs | grep -c $config_filesystem_name)" -ge 1 ]; then
		  echo "$(date -u): weka filesystem $config_filesystem_name is up"
		  break
		fi
		echo "$(date -u): waiting for weka filesystem $config_filesystem_name to be up"
		sleep 10
	  done
	  if (( i > max_retries )); then
		  err_msg="timeout: weka filesystem $config_filesystem_name is not up after $max_retries attempts."
		  echo "$(date -u): $err_msg"
		  report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"$PROTOCOL\", \"type\": \"error\", \"message\": \"$err_msg\"}"
		  return 1
	  fi
	}

	# make sure weka cluster is already up
	max_retries=60
	for (( i=0; i < max_retries; i++ )); do
	  if [ $(weka status | grep 'status: OK' | wc -l) -ge 1 ]; then
		echo "$(date -u): weka cluster is up"
		break
	  fi
	  echo "$(date -u): waiting for weka cluster to be up"
	  sleep 30
	done
	if (( i > max_retries )); then
		err_msg="timeout: weka cluster is not up after $max_retries attempts."
		echo "$(date -u): $err_msg"
		report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"$PROTOCOL\", \"type\": \"error\", \"message\": \"$err_msg\"}"
		exit 1
	fi

	# get container id
	for ((i=0; i<20; i++)); do
	  container_id=$(weka cluster container | grep frontend0 | grep "$HOSTNAME" | grep $current_mngmnt_ip | grep UP | awk '{print $1}')
	  if [ -n "$container_id" ]; then
		  echo "$(date -u): frontend0 container id: $container_id"
		  report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"$PROTOCOL\", \"type\": \"progress\", \"message\": \"frontend0 container $container_id is up\"}"
		  break
	  fi
	  echo "$(date -u): waiting for frontend0 container to be up"
	  sleep 5
	done

	if [ -z "$container_id" ]; then
	  err_msg="Failed to get the frontend0 container ID."
	  echo "$(date -u): $err_msg"
	  report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"$PROTOCOL\", \"type\": \"error\", \"message\": \"$err_msg\"}"
	  exit 1
	fi

	# if protocol isn't NFS, wait for the config filesystem to be up
	if [ "$PROTOCOL" != "nfs" ]; then
		wait_for_config_fs
	fi
	`
	script := fmt.Sprintf(
		template,
		d.Params.VMName,
		d.Params.ProtocolGatewayFeCoresNum,
		d.Params.InstallDpdk,
		d.Params.LoadBalancerIP,
		d.Params.NFSSecondaryIpsNum,
		d.Params.Protocol,
		gateways,
		protectFunc,
		fetchFunc,
		statusFunc,
		reportFunc,
		getCoreIdsFunc,
		getNetStrForDpdkFunc,
		getAllInterfaces,
		d.DeviceNameCmd,
		bash_functions.GetWekaPartitionScript(),
		wekaInstallScript,
		wekaRestFunc,
		setBackendIpFunc,
		bash_functions.SetCurrentManagementIp(),
		d.Params.GetPrimaryIpCmd,
	)
	return dedent.Dedent(script)
}

func (d *DeployScriptGenerator) GetProtocolGWDeployScript() string {
	baseDeploymentScript := d.GetBaseProtocolGWDeployScript()
	clusterizeFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Clusterize)

	template := `
	# clusterize function definition
	%s

	clusterize "{\"name\": \"$VM\", \"protocol\": \"$PROTOCOL\", \"container_uid\": \"$container_uid\", \"nic_name\": \"$nic_name\"}" > /tmp/clusterize.sh
	chmod +x /tmp/clusterize.sh
	/tmp/clusterize.sh 2>&1 | tee /tmp/weka_clusterization.log
	`
	script := fmt.Sprintf(template, clusterizeFunc)

	return baseDeploymentScript + dedent.Dedent(script)
}
