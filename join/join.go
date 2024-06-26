package join

import (
	"context"
	"fmt"
	"strings"

	"github.com/weka/go-cloud-lib/bash_functions"
	"github.com/weka/go-cloud-lib/functions_def"

	"github.com/lithammer/dedent"
	"github.com/weka/go-cloud-lib/common"
	"github.com/weka/go-cloud-lib/protocol"
)

type JoinParams struct {
	IPs            []string
	InstallDpdk    bool
	InstanceParams protocol.BackendCoreCount
	Gateways       []string
	ProxyUrl       string
}

type JoinScriptGenerator struct {
	DeviceNameCmd      string
	GetInstanceNameCmd string
	FindDrivesScript   string
	ScriptBase         string
	Params             JoinParams
	FuncDef            functions_def.FunctionDef
}

func (j *JoinScriptGenerator) GetJoinScript(ctx context.Context) string {
	fetchFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.Fetch)
	reportFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.Report)
	joinFinalizationFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.JoinFinalization)

	getCoreIdsFunc := bash_functions.GetCoreIds()
	getNetStrForDpdkFunc := bash_functions.GetNetStrForDpdk()
	gateways := strings.Join(j.Params.Gateways, " ")
	failureDomainCmd := bash_functions.GetHashedPrivateIpBashCmd()

	ips := j.Params.IPs
	common.ShuffleSlice(ips)

	bashScriptTemplate := `
	IPS=(%s)
	HASHED_IP=$(%s)
	COMPUTE=%d
	FRONTEND=%d
	DRIVES=%d
	COMPUTE_MEMORY=%s
	INSTALL_DPDK=%t
	GATEWAYS="%s"
	host_ips=$(IFS=, ;echo "${IPS[*]}")
	PROXY_URL="%s"

	# fetch function definition
	%s

	# report function definition
	%s

	# join_finalization function definition
	%s

	# get_core_ids bash function definition
	%s

	# getNetStrForDpdk bash function definitiion
	%s


	set +x
	fetch_result=$(fetch "{\"fetch_weka_credentials\": true}")
	export WEKA_USERNAME="$(echo $fetch_result | jq -r .username)"
	export WEKA_PASSWORD="$(echo $fetch_result | jq -r .password)"
	export WEKA_RUN_CREDS="-e WEKA_USERNAME=$WEKA_USERNAME -e WEKA_PASSWORD=$WEKA_PASSWORD"
	set -x

	# deviceNameCmd
	wekaiosw_device="%s"
	# wekio partition setup
	%s

	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Joining new instance started\"}"

	random=$$
	echo $random
	for backend_ip in ${IPS[@]}; do
		if VERSION=$(curl -s -XPOST --data '{"jsonrpc":"2.0", "method":"client_query_backend", "id":"'$random'"}' $backend_ip:14000/api/v1 | sed  's/.*"software_release":"\([^"]*\)".*$/\1/g'); then
			if [[ "$VERSION" != "" ]]; then
				break
			fi
		fi
	done

	ip=$(ifconfig eth0 | grep "inet " | awk '{ print $2}')
	while [ ! $ip ] ; do
		sleep 1
		ip=$(ifconfig eth0 | grep "inet " | awk '{ print $2}')
	done

	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Installing weka\"}"
	curl $backend_ip:14000/dist/v1/install -o install.sh
	chmod +x install.sh
	PROXY="$PROXY_URL" ./install.sh
	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Weka software installation completed\"}"

	weka version get --from $backend_ip:14000 $VERSION --set-current
	weka version prepare $VERSION
	weka local stop && weka local rm --all -f

	# weka containers setup

	get_core_ids $DRIVES drive_core_ids
	get_core_ids $COMPUTE compute_core_ids

	mgmt_ip=$(hostname -I | awk '{print $1}')
	if [[ $INSTALL_DPDK == true ]]; then
		getNetStrForDpdk 1 $(($DRIVES+1)) "$GATEWAYS"
		sudo weka local setup container --name drives0 --base-port 14000 --cores $DRIVES --no-frontends --drives-dedicated-cores $DRIVES --join-ips $host_ips --failure-domain "$HASHED_IP" --core-ids $drive_core_ids --management-ips $mgmt_ip --dedicate $net
		getNetStrForDpdk $((1+$DRIVES)) $((1+$DRIVES+$COMPUTE)) "$GATEWAYS"
		sudo weka local setup container --name compute0 --base-port 15000 --cores $COMPUTE --memory "$COMPUTE_MEMORY" --no-frontends --compute-dedicated-cores $COMPUTE --join-ips $host_ips --failure-domain "$HASHED_IP" --core-ids $compute_core_ids --management-ips $mgmt_ip --dedicate $net
	else
		sudo weka local setup container --name drives0 --base-port 14000 --cores $DRIVES --no-frontends --drives-dedicated-cores $DRIVES --join-ips $host_ips --failure-domain "$HASHED_IP" --core-ids $drive_core_ids --management-ips $mgmt_ip --dedicate --net udp
		sudo weka local setup container --name compute0 --base-port 15000 --cores $COMPUTE --memory "$COMPUTE_MEMORY" --no-frontends --compute-dedicated-cores $COMPUTE --join-ips $host_ips --failure-domain "$HASHED_IP" --core-ids $compute_core_ids --management-ips $mgmt_ip --dedicate --net udp
	fi

	if [[ $FRONTEND -gt 0 ]]; then
		get_core_ids $FRONTEND frontend_core_ids
		if [[ $INSTALL_DPDK == true ]]; then
			getNetStrForDpdk $((1+$DRIVES+$COMPUTE)) $((1+$DRIVES+$COMPUTE+1)) "$GATEWAYS"
			sudo weka local setup container --name frontend0 --base-port 16000 --cores $FRONTEND --allow-protocols true --frontend-dedicated-cores $FRONTEND --join-ips $host_ips --failure-domain "$HASHED_IP" --core-ids $frontend_core_ids --management-ips $mgmt_ip --dedicate $net
		else
			sudo weka local setup container --name frontend0 --base-port 16000 --cores $FRONTEND --allow-protocols true --frontend-dedicated-cores $FRONTEND --join-ips $host_ips --failure-domain "$HASHED_IP" --core-ids $frontend_core_ids --management-ips $mgmt_ip --dedicate --net udp
		fi
	fi
	`

	frontend := j.Params.InstanceParams.Frontend
	drive := j.Params.InstanceParams.Drive
	compute := j.Params.InstanceParams.Compute
	mem := j.Params.InstanceParams.ComputeMemory

	isReady := j.getIsReadyScript()
	addDrives := j.getAddDrivesScript()

	bashScriptTemplate = j.ScriptBase + dedent.Dedent(bashScriptTemplate)
	bashScriptTemplate += isReady + addDrives
	bashScript := fmt.Sprintf(
		bashScriptTemplate,
		strings.Join(ips, " "),
		failureDomainCmd,
		compute,
		frontend,
		drive,
		mem,
		j.Params.InstallDpdk,
		gateways,
		j.Params.ProxyUrl,
		fetchFunc,
		reportFunc,
		joinFinalizationFunc,
		getCoreIdsFunc,
		getNetStrForDpdkFunc,
		j.DeviceNameCmd,
		bash_functions.GetWekaPartitionScript(),
	)
	return dedent.Dedent(bashScript)
}

func (j *JoinScriptGenerator) GetExistingContainersJoinScript(ctx context.Context) string {
	fetchFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.Fetch)
	reportFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.Report)
	joinFinalizationFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.JoinFinalization)
	statusFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.Status)

	ips := j.Params.IPs
	common.ShuffleSlice(ips)

	bashScriptTemplate := `
	set -ex

	host_ips="%s"

	# fetch function definition
	%s

	fetch_result=$(fetch "{\"fetch_weka_credentials\": true}")
	export WEKA_USERNAME="$(echo $fetch_result | jq -r .username)"
	export WEKA_PASSWORD="$(echo $fetch_result | jq -r .password)"
	export WEKA_RUN_CREDS="-e WEKA_USERNAME=$WEKA_USERNAME -e WEKA_PASSWORD=$WEKA_PASSWORD"
	
	# report function definition
	%s

	# join_finalization function definition
	%s
	
	# status function definition
	%s
	
	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Joining instance (Initial setup)\"}"

	clusterized=$(status | jq .clusterized)
	while [ $clusterized != true ]; do
		echo "Waiting for clusterization to complete"
		sleep 5
		clusterized=$(status | jq .clusterized)
	done

	mgmt_ip=$(hostname -I | awk '{print $1}')

	sudo weka local resources management-ips $mgmt_ip -C drives0
	weka local resources join-ips --container drives0 $host_ips
	weka local resources apply -f --container drives0

	sudo weka local resources management-ips $mgmt_ip -C compute0
	weka local resources join-ips --container compute0 $host_ips
	weka local resources apply -f --container compute0

	if [[ $FRONTEND -gt 0 ]]; then
		sudo weka local resources management-ips $mgmt_ip -C frontend0
		weka local resources join-ips --container frontend0 $host_ips
		weka local resources apply -f --container frontend0
	fi
	`

	isReady := j.getIsReadyScript()
	addDrives := j.getAddDrivesScript()

	bashScriptTemplate = j.ScriptBase + dedent.Dedent(bashScriptTemplate)
	bashScriptTemplate += isReady + addDrives
	bashScript := fmt.Sprintf(
		bashScriptTemplate, strings.Join(ips, " "), fetchFunc, reportFunc,
		joinFinalizationFunc, statusFunc,
	)
	return dedent.Dedent(bashScript)
}

func (j *JoinScriptGenerator) getIsReadyScript() string {
	s := `
	while ! weka debug manhole -s 0 operational_status | grep '"is_ready": true' ; do
		sleep 1
	done
	echo Connected to cluster
	`
	return dedent.Dedent(s)
}

func (j *JoinScriptGenerator) getAddDrivesScript() string {
	// supposes 'report' and 'join_finalization' are already defined
	s := `
	compute_name=$(%s)

	mkdir -p /opt/weka/tmp

	# write down find_drives script (another string input for this template)
	cat >/opt/weka/tmp/find_drives.py <<EOL%sEOL
	set +x
	devices=$(weka local run --container compute0 $WEKA_RUN_CREDS bash -ce 'wapi machine-query-info --info-types=DISKS -J | python3 /opt/weka/tmp/find_drives.py')
	host_id=$(weka local run --container compute0 $WEKA_RUN_CREDS manhole getServerInfo | grep hostIdValue: | awk '{print $2}')
	set -x
	for device in $devices; do
		weka local exec --container drives0 /weka/tools/weka_sign_drive $device
	done
	ready=0
	while [ $ready -eq 0 ] ; do
		ready=1
		lsblk
		for device in $devices; do
			if [ ! "$(lsblk | grep ${device#"/dev/"} | grep part)" ]; then
				ready=0
				sleep 5
				break
			fi
		done
	done
	weka cluster drive scan $host_id

	weka events trigger-event "Scale up operation completed on host $HOSTNAME, data redistribution may still be running"

	join_finalization "{\"name\": \"$compute_name\"}"
	echo "completed successfully" > /tmp/weka_join_completion_validation
	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Joining new instance completed successfully\"}"
	`
	s = dedent.Dedent(s)
	return fmt.Sprintf(s, j.GetInstanceNameCmd, j.FindDrivesScript)
}
