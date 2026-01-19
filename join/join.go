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
	IsBM           bool
	InstanceParams protocol.BackendCoreCount
	Gateways       []string
	ProxyUrl       string
	CgroupsMode    string
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
	reportFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.Report)
	joinFinalizationFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.JoinFinalization)
	getCoreIdsFunc := bash_functions.GetCoreIds()
	gateways := strings.Join(j.Params.Gateways, " ")
	getNetStrForDpdkFunc := bash_functions.GetNetStrForDpdk(j.Params.IsBM, gateways)
	getAllInterfaces := bash_functions.GetAllInterfaces()
	failureDomainCmd := bash_functions.GetHashedPrivateIpBashCmd()

	ips := j.Params.IPs
	common.ShuffleSlice(ips)

	cgroupsMode := "auto"
	if j.Params.CgroupsMode != "" {
		cgroupsMode = j.Params.CgroupsMode
	}

	bashScriptTemplate := `
	IPS=(%s)
	HASHED_IP=$(%s)
	COMPUTE=%d
	FRONTEND=%d
	DRIVES=%d
	COMPUTE_MEMORY=%s
	INSTALL_DPDK=%t
	host_ips=$(IFS=, ;echo "${IPS[*]}")
	PROXY_URL="%s"
	WEKA_CGROUPS_MODE="%s"

	# report function definition
	%s

	# join_finalization function definition
	%s

	# get_core_ids bash function definition
	%s

	# getNetStrForDpdk bash function definitiion
	%s

	# getAllInterfaces bash function definition
	%s

	# deviceNameCmd
	wekaiosw_device="%s"
	# wekio partition setup
	%s

	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Joining new instance started\"}"

	random=$$
	echo $random
	for backend_ip in ${IPS[@]}; do
		if VERSION=$(curl -s -XPOST --insecure --data '{"jsonrpc":"2.0", "method":"client_query_backend", "id":"'$random'"}' https://$backend_ip:14000/api/v1 | sed  's/.*"software_release":"\([^"]*\)".*$/\1/g'); then
			if [[ "$VERSION" != "" ]]; then
				break
			fi
		fi
	done

	getAllInterfaces

	while true ; do
		if [[ $(ip -4 addr show ${all_interfaces[0]} | grep inet | awk '{print $2}' | cut -d/ -f1) ]]; then
			break
		fi
		sleep 1
	done

	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Installing weka\"}"
	curl --insecure https://$backend_ip:14000/dist/v1/install -o install.sh
	chmod +x install.sh
	PROXY="$PROXY_URL" WEKA_CGROUPS_MODE="$WEKA_CGROUPS_MODE" ./install.sh
	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"WEKA software installation completed\"}"

	weka version get --from $backend_ip:14000 $VERSION --set-current
	weka version prepare $VERSION
	weka local stop && weka local rm --all -f

	# weka containers setup

	get_core_ids $DRIVES drive_core_ids
	get_core_ids $COMPUTE compute_core_ids

	mgmt_ip=$(hostname -I | awk '{print $1}')
	if [[ $INSTALL_DPDK == true ]]; then
		getNetStrForDpdk 1 $((1+$DRIVES))
		sudo weka local setup container --name drives0 --base-port 14000 --cores $DRIVES --no-frontends --drives-dedicated-cores $DRIVES --join-ips $host_ips --failure-domain "$HASHED_IP" --core-ids $drive_core_ids --management-ips $mgmt_ip --dedicate $net
		getNetStrForDpdk $((1+$DRIVES)) $((1+$DRIVES+$COMPUTE))
		sudo weka local setup container --name compute0 --base-port 15000 --cores $COMPUTE --memory "$COMPUTE_MEMORY" --no-frontends --compute-dedicated-cores $COMPUTE --join-ips $host_ips --failure-domain "$HASHED_IP" --core-ids $compute_core_ids --management-ips $mgmt_ip --dedicate $net
	else
		sudo weka local setup container --name drives0 --base-port 14000 --cores $DRIVES --no-frontends --drives-dedicated-cores $DRIVES --join-ips $host_ips --failure-domain "$HASHED_IP" --core-ids $drive_core_ids --management-ips $mgmt_ip --dedicate --net udp
		sudo weka local setup container --name compute0 --base-port 15000 --cores $COMPUTE --memory "$COMPUTE_MEMORY" --no-frontends --compute-dedicated-cores $COMPUTE --join-ips $host_ips --failure-domain "$HASHED_IP" --core-ids $compute_core_ids --management-ips $mgmt_ip --dedicate --net udp
	fi

	if [[ $FRONTEND -gt 0 ]]; then
		get_core_ids $FRONTEND frontend_core_ids
		if [[ $INSTALL_DPDK == true ]]; then
			getNetStrForDpdk $((1+$DRIVES+$COMPUTE)) $((1+$DRIVES+$COMPUTE+1))
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

	bashScriptTemplate = j.ScriptBase + dedent.Dedent(bashScriptTemplate)
	bashScript := fmt.Sprintf(
		bashScriptTemplate,
		strings.Join(ips, " "),
		failureDomainCmd,
		compute,
		frontend,
		drive,
		mem,
		j.Params.InstallDpdk,
		j.Params.ProxyUrl,
		cgroupsMode,
		reportFunc,
		joinFinalizationFunc,
		getCoreIdsFunc,
		getNetStrForDpdkFunc,
		getAllInterfaces,
		j.DeviceNameCmd,
		bash_functions.GetWekaPartitionScript(),
	)

	setWekaCredentials := j.getWekaCredentialsEnvVarsSetup()
	isReady := j.getIsReadyScript()
	addDrives := j.getAddDrivesScript()
	bashScript += setWekaCredentials + isReady + addDrives

	return dedent.Dedent(bashScript)
}

func (j *JoinScriptGenerator) GetExistingContainersJoinScript(ctx context.Context) string {
	reportFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.Report)
	joinFinalizationFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.JoinFinalization)
	statusFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.Status)

	ips := j.Params.IPs
	common.ShuffleSlice(ips)

	bashScriptTemplate := `
	set -ex

	host_ips="%s"

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

	if [[ $(weka local ps | grep frontend0) ]]; then
		sudo weka local resources management-ips $mgmt_ip -C frontend0
		weka local resources join-ips --container frontend0 $host_ips
		weka local resources apply -f --container frontend0
	fi
	`
	bashScriptTemplate = j.ScriptBase + dedent.Dedent(bashScriptTemplate)
	bashScript := fmt.Sprintf(
		bashScriptTemplate, strings.Join(ips, " "), reportFunc,
		joinFinalizationFunc, statusFunc,
	)

	setWekaCredentials := j.getWekaCredentialsEnvVarsSetup()
	isReady := j.getIsReadyScript()
	addDrives := j.getAddDrivesScript()

	bashScript += setWekaCredentials + isReady + addDrives

	return dedent.Dedent(bashScript)
}

func (j *JoinScriptGenerator) getWekaCredentialsEnvVarsSetup() string {
	fetchFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.Fetch)
	s := `
	# fetch function definition
	%s
	
	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Fetching WEKA credentials\"}"

	set +x
	fetch_result=$(fetch "{\"fetch_weka_credentials\": true}")
	export WEKA_USERNAME="$(echo $fetch_result | jq -r .username)"
	export WEKA_PASSWORD="$(echo $fetch_result | jq -r .password)"
	set -x
	`
	s = dedent.Dedent(s)
	return fmt.Sprintf(s, fetchFunc)
}

func (j *JoinScriptGenerator) getIsReadyScript() string {
	s := `
	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Waiting for WEKA cluster to be ready\"}"

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
	set +x
	export WEKA_RUN_CREDS="-e WEKA_USERNAME=$WEKA_USERNAME -e WEKA_PASSWORD=$WEKA_PASSWORD"
	set -x

	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Adding drives to WEKA cluster\"}"

	compute_name=$(%s)

	mkdir -p /opt/weka/tmp

	# write down find_drives script (another string input for this template)
	cat >/opt/weka/tmp/find_drives.py <<EOL%sEOL
	set +x
	devices=$(weka local run --container compute0 $WEKA_RUN_CREDS bash -ce 'wapi machine-query-info --info-types=DISKS -J | python3 /opt/weka/tmp/find_drives.py')
	host_id=$(weka local run --container compute0 $WEKA_RUN_CREDS manhole getServerInfo | grep hostIdValue: | awk '{print $2}')
	set -x

	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Signing drives\"}"
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

	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Running drives scan\"}"

	count=1
	while ! weka cluster drive scan "$host_id"; do
		count=$((count+1))
		if [ "$count" -gt 60 ]; then
			report "{\"hostname\": \"$HOSTNAME\", \"type\": \"error\", \"message\": \"Failed to run drives scan\"}"
			containers=($(weka cluster container | grep "$HOSTNAME" | awk '{print $1}'))
			for c in "${containers[@]}"
			do
				report "{\"hostname\": \"$HOSTNAME\", \"type\": \"debug\", \"message\": \"Deactivating container: $c\"}"
				weka cluster container deactivate $c || true
			done
			exit 1
		fi
		sleep 1
		echo "Retrying drives scan, try: $count/60"
		report "{\"hostname\": \"$HOSTNAME\", \"type\": \"debug\", \"message\": \"Retrying drives scan\"}"
	done

	weka events trigger-event "Scale up operation completed on host $HOSTNAME, data redistribution may still be running"

	join_finalization "{\"name\": \"$compute_name\"}"
	echo "completed successfully" > /tmp/weka_join_completion_validation
	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Joining new instance completed successfully\"}"
	`
	s = dedent.Dedent(s)
	return fmt.Sprintf(s, j.GetInstanceNameCmd, j.FindDrivesScript)
}
