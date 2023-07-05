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
	VMNames           []string
	IPs               []string
	ClusterName       string
	Prefix            string
	HostsNum          int
	NvmesNum          int
	WekaUsername      string
	WekaPassword      string
	SetObs            bool
	ObsScript         string
	DataProtection    DataProtectionParams
	InstallDpdk       bool
	DebugOverrideCmds string
	AddFrontend       bool
}

type ClusterizeScriptGenerator struct {
	Params  ClusterParams
	FuncDef functions_def.FunctionDef
}

func (c *ClusterizeScriptGenerator) GetClusterizeScript() string {
	reportFuncDef := c.FuncDef.GetFunctionCmdDefinition(functions_def.Report)
	clusterizeFinFuncDef := c.FuncDef.GetFunctionCmdDefinition(functions_def.ClusterizeFinalizaition)
	params := c.Params

	clusterizeScriptTemplate := `
	#!/bin/bash
	set -ex
	VMS=(%s)
	IPS=(%s)
	CLUSTER_NAME=%s
	HOSTS_NUM=%d
	NVMES_NUM=%d
	SET_OBS=%t
	STRIPE_WIDTH=%d
	PROTECTION_LEVEL=%d
	HOTSPARE=%d
	WEKA_USERNAME="%s"
	WEKA_PASSWORD="%s"
	INSTALL_DPDK=%t
	ADD_FRONTEND=%t

	CONTAINER_NAMES=(drives0 compute0)
	PORTS=(14000 15000)

	# report function definition
	%s

	# clusterize_finalization function definition
	%s
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
	weka cluster create $host_names --host-ips $host_ips --admin-password "$WEKA_PASSWORD"
	weka user login $WEKA_USERNAME $WEKA_PASSWORD
	
	if [[ $INSTALL_DPDK == true ]]; then
		%s
	fi
	
	sleep 30s

	DRIVE_NUMS=( $(weka cluster container | grep drives | awk '{print $1;}') )
	
	for drive_num in "${DRIVE_NUMS[@]}"; do
		for (( d=0; d<$NVMES_NUM; d++ )); do
			while true; do
				if lsblk "/dev/nvme$d"n1 >/dev/null 2>&1 ;then
					weka cluster drive add $drive_num "/dev/nvme$d"n1 # azure
					break
				fi
				if lsblk "/dev/nvme0n$((d+1))" >/dev/null 2>&1 ;then
					weka cluster drive add $drive_num "/dev/nvme0n$((d+1))" #gcp
					break
				fi
				echo "waiting for nvme to be ready"
				sleep 5
			done
		done
	done

	weka cluster update --cluster-name="$CLUSTER_NAME"

	weka cloud enable || true # skipping required for private network

	if [ "$STRIPE_WIDTH" -gt 0 ] && [ "$PROTECTION_LEVEL" -gt 0 ]; then
		weka cluster update --data-drives $STRIPE_WIDTH --parity-drives $PROTECTION_LEVEL
	fi
	weka cluster hot-spare $HOTSPARE
	weka cluster start-io
	
	sleep 15s
	
	weka cluster process
	weka cluster drive
	weka cluster container
	
	full_capacity=$(weka status -J | jq .capacity.unprovisioned_bytes)
	weka fs group create default
	weka fs create default default "$full_capacity"B

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
		# 'set obs' script
		%s
	fi
	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"OBS setup completed successfully\"}"
	`
	script := fmt.Sprintf(
		dedent.Dedent(clusterizeScriptTemplate), strings.Join(params.VMNames, " "), strings.Join(params.IPs, " "), params.ClusterName, params.HostsNum, params.NvmesNum,
		params.SetObs, params.DataProtection.StripeWidth, params.DataProtection.ProtectionLevel, params.DataProtection.Hotspare,
		params.WekaUsername, params.WekaPassword, params.InstallDpdk, params.AddFrontend, reportFuncDef, clusterizeFinFuncDef, params.DebugOverrideCmds, params.ObsScript,
	)
	return script
}
