package clusterize

import (
	"fmt"
	"strings"

	"github.com/lithammer/dedent"
	"github.com/weka/go-cloud-lib/bash_functions"
	"github.com/weka/go-cloud-lib/functions_def"
	"github.com/weka/go-cloud-lib/protocol"
)

type ConfigureNfsScriptGenerator struct {
	Params         protocol.NFSParams
	FuncDef        functions_def.FunctionDef
	LoadBalancerIP string
	Name           string // for aws it will be the instance id
}

func (c *ConfigureNfsScriptGenerator) GetNFSSetupScript() string {
	configureNFSScriptTemplate := `
	#!/bin/bash
	set -ex
	
	instance_name="%s"
	interface_group_name="%s"
	containersUid=(%s)
	nic_names=(%s)
	secondary_ips=(%s)
	LOAD_BALANCER_IP="%s"

	# fetch function definition
	%s

	# report function definition
	%s

	# clusterize_finalization function definition
	%s

	# set_backend_ip bash function definition
	%s

	set_backend_ip

	# weka rest function definition (requires $backend_ip var to be set)
	%s

	# getAllInterfaces bash function definition
	%s

	getAllInterfaces
	# set current management ip
	%s

	nfs_count=${#containersUid[@]}

	report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"nfs\", \"type\": \"progress\", \"message\": \"This ($instance_name) is nfs instance $nfs_count/$nfs_count that is ready for joining the interface group\"}"

	set +x
	fetch_result=$(fetch "{\"fetch_weka_credentials\": true}")
	export WEKA_USERNAME="$(echo $fetch_result | jq -r .username)"
	export WEKA_PASSWORD="$(echo $fetch_result | jq -r .password)"
	set -x

	nic_name=$(ip -o -f inet addr show | grep "$current_mngmnt_ip/"| awk '{print $2}')
	gateway=$(ip r | grep default | awk '{print $3}')
	cidr=$(ip -o -f inet addr show $nic_name | grep "$current_mngmnt_ip/" | awk '{print $4}' | cut -d'/' -f2)
	mask=$((0xffffffff << (32-cidr)))
	subnet_mask=$(printf "%d.%d.%d.%d\n" $((mask>>24&0xff)) $((mask>>16&0xff)) $((mask>>8&0xff)) $((mask&0xff)))

	function create_interface_group() {
		if weka_rest interfacegroups | grep ${interface_group_name}; then
			echo "$(date -u): interface group ${interface_group_name} already exists"
			return
		fi
		echo "$(date -u): creating interface group ${interface_group_name}"
		weka_rest interfacegroups "{\"name\":\"$interface_group_name\",\"type\":\"nfs\",\"subnet\":\"$subnet_mask\",\"gateway\":\"$gateway\"}"
		echo "$(date -u): interface group ${interface_group_name} created"
	}
	
	function wait_for_weka_fs(){
		filesystem_name="default"
		max_retries=30 # 30 * 10 = 5 minutes
		for (( i=0; i < max_retries; i++ )); do
			if [ "$(weka_rest filesystems | grep -c $filesystem_name)" -ge 1 ]; then
				echo "$(date -u): weka filesystem $filesystem_name is up"
				break
			fi
			echo "$(date -u): waiting for weka filesystem $filesystem_name to be up"
			sleep 10
		done
		if (( i > max_retries )); then
			echo "$(date -u): timeout: weka filesystem $filesystem_name is not up after $max_retries attempts."
			return 1
		fi
	}

	function wait_for_nfs_interface_group(){
	  max_retries=60 # 60 * 10 = 10 minutes
	  for ((i=0; i<max_retries; i++)); do
		status=$(weka_rest interfacegroups | jq .data | jq -r '.[] | select(.name == "'${interface_group_name}'").status')
		if [ "$status" == "OK" ]; then
			echo "$(date -u): interface group status: $status"
			break
		fi
		echo "$(date -u): waiting for interface group status to be OK, current status: $status"
		sleep 10
	  done
	  if [ "$status" != "OK" ]; then
		echo "$(date -u): failed to wait for the interface group status to be OK"
		report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"nfs\", \"type\": \"error\", \"message\": \"NFS interface group status is not OK after 10 minutes\"}"
		return 1
	  fi
	}

	# create interface group if not exists
	create_interface_group || (report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"nfs\", \"type\": \"error\", \"message\": \"Failed to create NFS interface group\"}" && exit 1)
	
	# show interface group
	#weka nfs interface-group
	weka_rest interfacegroups | jq -r .data

	#weka nfs interface-group port add ${interface_group_name} $container_id $nic_name
	# add "port" to the interface group - basically it means adding a host and its net device to the group
	interface_group_uid=$(weka_rest interfacegroups | jq -r .data[].uid)
	
	for index in "${!containersUid[@]}"; do
		container_uid=${containersUid[$index]}
		nic_name=${nic_names[$index]}
		weka_rest "interfacegroups/$interface_group_uid/ports/$container_uid" "{\"port\":\"$nic_name\"}"
	done

	wait_for_nfs_interface_group || exit 1

	# add secondary IPs for the group to use - these IPs will be used in order to mount
	for secondary_ip in "${secondary_ips[@]}"; do
		# add secondary ip to the interface group
		#weka nfs interface-group ip-range add ${interface_group_name} $secondary_ip
		weka_rest "interfacegroups/$interface_group_uid/ips" "{\"ips\":\"$secondary_ip\"}"
		wait_for_nfs_interface_group || exit 1
	done

	weka_rest interfacegroups | jq -r .data

	echo "$(date -u): NFS setup complete"
	
	echo "completed successfully" > /tmp/weka_clusterization_completion_validation
	report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"nfs\", \"type\": \"progress\", \"message\": \"NFS configuration completed successfully\"}"

	clusterize_finalization "{\"protocol\": \"nfs\"}"
	`
	nfsSetupScript := fmt.Sprintf(
		configureNFSScriptTemplate,
		c.Name,
		c.Params.InterfaceGroupName,
		strings.Join(c.Params.ContainersUid, " "),
		strings.Join(c.Params.NicNames, " "),
		strings.Join(c.Params.SecondaryIps, " "),
		c.LoadBalancerIP,
		c.FuncDef.GetFunctionCmdDefinition(functions_def.Fetch),
		c.FuncDef.GetFunctionCmdDefinition(functions_def.Report),
		c.FuncDef.GetFunctionCmdDefinition(functions_def.ClusterizeFinalization),
		bash_functions.SetBackendIpFunction(),
		bash_functions.WekaRestFunction(),
		bash_functions.GetAllInterfaces(),
		bash_functions.SetCurrentManagementIp(),
	)

	return dedent.Dedent(nfsSetupScript)
}
