package clusterize

import (
	"fmt"
	"strings"

	"github.com/weka/go-cloud-lib/functions_def"

	"github.com/lithammer/dedent"
)

type DataProtectionParams struct {
	StripeWidth     int
	ProtectionLevel int
	Hotspare        int
}

type ClusterParams struct {
	VMNames                   []string
	IPs                       []string
	ClusterName               string
	Prefix                    string
	ClusterizationTarget      int
	SetObs                    bool
	CreateConfigFs            bool
	ObsScript                 string
	TieringTargetSSDRetention int
	TieringStartDemote        int
	DataProtection            DataProtectionParams
	InstallDpdk               bool
	AddFrontend               bool
	FindDrivesScript          string
	ProxyUrl                  string
	WekaHomeUrl               string
	PreStartIoScript          string
	PostClusterCreationScript string
	SetDefaultFs              bool
	PostClusterSetupScript    string
}

type ClusterizeScriptGenerator struct {
	Params  ClusterParams
	FuncDef functions_def.FunctionDef
}

func (c *ClusterizeScriptGenerator) GetClusterizeScript() string {
	reportFuncDef := c.FuncDef.GetFunctionCmdDefinition(functions_def.Report)
	clusterizeFinFuncDef := c.FuncDef.GetFunctionCmdDefinition(functions_def.ClusterizeFinalization)
	fetchFuncDef := c.FuncDef.GetFunctionCmdDefinition(functions_def.Fetch)
	params := c.Params

	clusterizeScriptTemplate := `
	#!/bin/bash
	set -ex
	VMS=(%s)
	IPS=(%s)
	CLUSTER_NAME=%s
	HOSTS_NUM=%d
	SET_OBS=%t
	STRIPE_WIDTH=%d
	PROTECTION_LEVEL=%d
	HOTSPARE=%d
	INSTALL_DPDK=%t
	ADD_FRONTEND=%t
	PROXY_URL="%s"
	WEKA_HOME_URL="%s"
	TARGET_SSD_RETENTION=%d
	START_DEMOTE=%d
	SET_DEFAULT_FS=%t

	mkdir -p /opt/weka/tmp
	cat >/opt/weka/tmp/find_drives.py <<EOL%sEOL

	# fetch function definition
	%s

	set +x
	fetch_result=$(fetch "{\"fetch_weka_credentials\": true, \"show_admin_password\": true}")
	export WEKA_DEPLOYMENT_USERNAME="$(echo $fetch_result | jq -r .username)"
	export WEKA_DEPLOYMENT_PASSWORD="$(echo $fetch_result | jq -r .password)"
	export WEKA_ADMIN_PASSWORD="$(echo $fetch_result | jq -r .admin_password)"
	export WEKA_RUN_CREDS="-e WEKA_USERNAME=admin -e WEKA_PASSWORD=$WEKA_ADMIN_PASSWORD"
	devices=$(weka local run --container compute0 $WEKA_RUN_CREDS bash -ce 'wapi machine-query-info --info-types=DISKS -J | python3 /opt/weka/tmp/find_drives.py')
	set -x
	devices=($devices)

	CONTAINER_NAMES=(drives0 compute0)
	PORTS=(14000 15000)

	# report function definition
	%s

	# clusterize_finalization function definition
	%s

	last_vm_name=${VMS[${#VMS[@]} - 1]}
	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"This ($last_vm_name) is instance $HOSTS_NUM that is ready for clusterization\"}"

	if [[ $ADD_FRONTEND == true ]]; then
		CONTAINER_NAMES+=(frontend0)
		PORTS+=(16000)
	fi


	HOST_IPS=()
	HOST_NAMES=()
	for i in "${!IPS[@]}"; do
		for j in "${!PORTS[@]}"; do
			HOST_IPS+=($(echo "${IPS[i]}:${PORTS[j]}"))
			HOST_NAMES+=($(echo "${VMS[i]}-${CONTAINER_NAMES[j]}"))
		done
	done
	host_ips=$(IFS=, ;echo "${HOST_IPS[*]}")
	host_names=$(IFS=' ' ;echo "${HOST_NAMES[*]}")

	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Running Clusterization\"}"

	vms_string=$(printf "%%s "  "${VMS[@]}" | rev | cut -c2- | rev)

	set +x
	weka cluster create $host_names --host-ips $host_ips --admin-password "$WEKA_ADMIN_PASSWORD"
	weka user login admin $WEKA_ADMIN_PASSWORD

	# setup weka deployment user (internal, only used by cloud functions)
	# weka user add <username> <role> [password]
	weka user add $WEKA_DEPLOYMENT_USERNAME clusteradmin "$WEKA_DEPLOYMENT_PASSWORD" || report "{\"hostname\": \"$HOSTNAME\", \"type\": \"error\", \"message\": \"Failed creating deployment user\"}"
	weka user
	set -x
	
	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Deployment user was created successfully\"}"
	# post cluster creation script
	function post_cluster_creation() {
		echo "running post cluster creation script"
		%s
	}
	post_cluster_creation || report "{\"hostname\": \"$HOSTNAME\", \"type\": \"error\", \"message\": \"Failed running post cluster create script\"}"
	
	sleep 30s

	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Adding drives\"}"

	DRIVE_NUMS=( $(weka cluster container | grep drives | awk '{print $1;}') )
	devices_str=$(IFS=' ' ;echo "${devices[*]}")

	function add_drives() {
		bad_drives=false
		drive_num=$1
		if ! weka cluster drive add $drive_num $devices_str; then
			report "{\"hostname\": \"$HOSTNAME\", \"type\": \"error\", \"message\": \"Failed adding drives: $drive_num: $devices_str\"}"
			bad_drives=true
		fi

		if [ $bad_drives = true ]; then
			weka_hostname=$(weka cluster container -c $drive_num | tail -n +2 | awk '{print $2}')
			containers=($(weka cluster container | grep $weka_hostname | awk '{print $1}'))
			for c in "${containers[@]}"
			do
				report "{\"hostname\": \"$HOSTNAME\", \"type\": \"debug\", \"message\": \"Deactivating container: $c\"}"
				weka cluster container deactivate $c || true
			done

			all_inactive=false
			while [ $all_inactive = false ] ; do
				all_inactive=true
				for c in "${containers[@]}"
				do
					status=$(weka cluster container -c $c | tail -n +2 | awk '{print $5}')
					if [ "$status" != "INACTIVE" ]; then
						echo "Container $c status:$status"
						all_inactive=false
						sleep 5
						break
					fi
				done
			done

			report "{\"hostname\": \"$HOSTNAME\", \"type\": \"debug\", \"message\": \"Removing drives: $drive_num\"}"
			drives=($(weka cluster drive | grep $weka_hostname | awk '{print $2}'))
			for d in "${drives[@]}"
			do
				weka cluster drive remove $d -f || true
			done
		fi
	}

	for drive_container_id in "${DRIVE_NUMS[@]}"; do
		add_drives $drive_container_id &
		sleep 0.1 # give some time between drives additions to allow first drives additions to complete
	done
	wait

	weka cluster update --cluster-name="$CLUSTER_NAME"

	if [ -n "$PROXY_URL" ]; then
		weka cloud proxy --set "$PROXY_URL"
	fi
	cloud_url_option=""
	if [ -n "$WEKA_HOME_URL" ]; then
		cloud_url_option="--cloud-url $WEKA_HOME_URL"
	fi
	weka cloud enable $cloud_url_option || true # skipping required for private network

	if [ "$STRIPE_WIDTH" -gt 0 ] && [ "$PROTECTION_LEVEL" -gt 0 ]; then
		weka cluster update --data-drives $STRIPE_WIDTH --parity-drives $PROTECTION_LEVEL
	fi
	weka cluster hot-spare $HOTSPARE

	# pre start-io script
	function pre_start_io() {
		echo "running pre start-io script"
		%s
	}
	pre_start_io || report "{\"hostname\": \"$HOSTNAME\", \"type\": \"error\", \"message\": \"Failed running pre start-io script\"}"

	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Running start-io\"}"
	weka cluster start-io
	
	sleep 15s
	
	weka cluster process
	weka cluster drive
	weka cluster container
	
	weka fs group create default --target-ssd-retention=$TARGET_SSD_RETENTION --start-demote=$START_DEMOTE || report "{\"hostname\": \"$HOSTNAME\", \"type\": \"error\", \"message\": \"Failed to create fs group\"}"
	weka fs create .config_fs default 22GB
	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"FS '.config_fs' was created successfully\"}"
	weka nfs global-config set --config-fs .config_fs || echo "Failed to set NFS global config fs"
	weka dataservice global-config set --config-fs .config_fs || true

	if [[ $SET_DEFAULT_FS == true ]]; then
		full_capacity=$(weka status -J | jq .capacity.unprovisioned_bytes)
		weka fs create default default "$full_capacity"B
		report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Default FS was created successfully\"}"
	fi

	if [[ $INSTALL_DPDK == true ]]; then
		weka alerts mute NodeRDMANotActive 365d
	else
		weka alerts mute JumboConnectivity 365d
		weka alerts mute UdpModePerformanceWarning 365d
	fi

	echo "completed successfully" > /tmp/weka_clusterization_completion_validation
	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Clusterization completed successfully\"}"

	clusterize_finalization "{}"

	if [[ $SET_OBS == true ]]; then
		function set_obs() {
			# 'set obs' script
			%s
		}
		set_obs || (report "{\"hostname\": \"$HOSTNAME\", \"type\": \"error\", \"message\": \"OBS setup failed\"}" && exit 1)
		report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"OBS setup completed successfully\"}"
	else
		report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Skipping OBS setup\"}"
	fi

	post_cluster_setup_script="%s"
	if [ -n "$post_cluster_setup_script" ]; then
		report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Running post cluster setup script\"}"
		post_cluster_setup_script_path=/tmp/weka_post_cluster_setup_script.sh
		echo "$post_cluster_setup_script" > "$post_cluster_setup_script_path"
		chmod +x "$post_cluster_setup_script_path"
		echo "running post clusterization script"
		if "$post_cluster_setup_script_path"; then
			report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Running post cluster setup script completed successfully\"}"
		else
			report "{\"hostname\": \"$HOSTNAME\", \"type\": \"error\", \"message\": \"Running post cluster setup script failed\"}"
		fi
	fi
	`
	script := fmt.Sprintf(
		dedent.Dedent(clusterizeScriptTemplate),
		strings.Join(params.VMNames, " "),
		strings.Join(params.IPs, " "),
		params.ClusterName,
		params.ClusterizationTarget,
		params.SetObs,
		params.DataProtection.StripeWidth,
		params.DataProtection.ProtectionLevel,
		params.DataProtection.Hotspare,
		params.InstallDpdk,
		params.AddFrontend,
		params.ProxyUrl,
		params.WekaHomeUrl,
		params.TieringTargetSSDRetention,
		params.TieringStartDemote,
		params.SetDefaultFs,
		params.FindDrivesScript,
		fetchFuncDef,
		reportFuncDef,
		clusterizeFinFuncDef,
		params.PostClusterCreationScript,
		params.PreStartIoScript,
		params.ObsScript,
		params.PostClusterSetupScript,
	)
	return script
}
