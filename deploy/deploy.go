package deploy

import (
	"fmt"
	"strings"

	"github.com/weka/go-cloud-lib/protocol"

	"github.com/weka/go-cloud-lib/bash_functions"
	"github.com/weka/go-cloud-lib/functions_def"

	"github.com/lithammer/dedent"
)

type DeploymentParams struct {
	VMName         string
	InstanceParams protocol.BackendCoreCount
	WekaInstallUrl string
	WekaToken      string
	InstallDpdk    bool
	NicsNum        string
	ProxyUrl       string
	Gateways       []string
}

type DeployScriptGenerator struct {
	FailureDomainCmd string
	DeviceNameCmd    string
	Params           DeploymentParams
	FuncDef          functions_def.FunctionDef
}

func (d *DeployScriptGenerator) GetDeployScript() string {
	wekaInstallScript := d.GetWekaInstallScript()
	protectFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Protect)
	clusterizeFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Clusterize)

	getCoreIdsFunc := bash_functions.GetCoreIds()
	getNetStrForDpdkFunc := bash_functions.GetNetStrForDpdk()
	gateways := strings.Join(d.Params.Gateways, " ")

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
	INSTALL_DPDK=%t
	GATEWAYS="%s"

	# clusterize function definition
	%s

	# protect function definition (if any)
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

	weka local stop
	weka local rm default --force

	# weka containers setup
	get_core_ids $DRIVE_CONTAINER_CORES_NUM drive_core_ids
	get_core_ids $COMPUTE_CONTAINER_CORES_NUM compute_core_ids

	total_containers=2

	if [[ $INSTALL_DPDK == true ]]; then
		getNetStrForDpdk 1 $(($DRIVE_CONTAINER_CORES_NUM+1)) "$GATEWAYS"
		sudo weka local setup container --name drives0 --base-port 14000 --cores $DRIVE_CONTAINER_CORES_NUM --no-frontends --drives-dedicated-cores $DRIVE_CONTAINER_CORES_NUM --failure-domain $FAILURE_DOMAIN --core-ids $drive_core_ids $net --dedicate
		getNetStrForDpdk $((1+$DRIVE_CONTAINER_CORES_NUM)) $((1+$DRIVE_CONTAINER_CORES_NUM+$COMPUTE_CONTAINER_CORES_NUM )) "$GATEWAYS"
		sudo weka local setup container --name compute0 --base-port 15000 --cores $COMPUTE_CONTAINER_CORES_NUM --no-frontends --compute-dedicated-cores $COMPUTE_CONTAINER_CORES_NUM  --memory $COMPUTE_MEMORY --failure-domain $FAILURE_DOMAIN --core-ids $compute_core_ids $net --dedicate
	else
		sudo weka local setup container --name drives0 --base-port 14000 --cores $DRIVE_CONTAINER_CORES_NUM --no-frontends --drives-dedicated-cores $DRIVE_CONTAINER_CORES_NUM --failure-domain $FAILURE_DOMAIN --core-ids $drive_core_ids  --dedicate
		sudo weka local setup container --name compute0 --base-port 15000 --cores $COMPUTE_CONTAINER_CORES_NUM --no-frontends --compute-dedicated-cores $COMPUTE_CONTAINER_CORES_NUM  --memory $COMPUTE_MEMORY --failure-domain $FAILURE_DOMAIN --core-ids $compute_core_ids  --dedicate
	fi

	if [[ $FRONTEND_CONTAINER_CORES_NUM -gt 0 ]]; then
		total_containers=3
		get_core_ids $FRONTEND_CONTAINER_CORES_NUM frontend_core_ids
		if [[ $INSTALL_DPDK == true ]]; then
			getNetStrForDpdk $(($NICS_NUM-1)) $(($NICS_NUM)) "$GATEWAYS" "$SUBNETS"
			sudo weka local setup container --name frontend0 --base-port 16000 --cores $FRONTEND_CONTAINER_CORES_NUM --frontend-dedicated-cores $FRONTEND_CONTAINER_CORES_NUM --allow-protocols true --failure-domain $FAILURE_DOMAIN --core-ids $frontend_core_ids $net --dedicate
		else
			sudo weka local setup container --name frontend0 --base-port 16000 --cores $FRONTEND_CONTAINER_CORES_NUM --frontend-dedicated-cores $FRONTEND_CONTAINER_CORES_NUM --allow-protocols true --failure-domain $FAILURE_DOMAIN --core-ids $frontend_core_ids  --dedicate
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
	clusterize "{\"vm\": \"$VM\"}" > /tmp/clusterize.sh
	chmod +x /tmp/clusterize.sh
	/tmp/clusterize.sh 2>&1 | tee /tmp/weka_clusterization.log
	`
	script := fmt.Sprintf(
		template, d.Params.VMName, d.FailureDomainCmd, d.Params.InstanceParams.ComputeMemory, d.Params.InstanceParams.Compute,
		d.Params.InstanceParams.Frontend, d.Params.InstanceParams.Drive, d.Params.NicsNum, d.Params.InstallDpdk,
		gateways, clusterizeFunc, protectFunc, getCoreIdsFunc, getNetStrForDpdkFunc, d.DeviceNameCmd,
		bash_functions.GetWekaPartitionScript(), wekaInstallScript,
	)
	return dedent.Dedent(script)
}

func (d *DeployScriptGenerator) GetWekaInstallScript() string {
	installUrl := d.Params.WekaInstallUrl
	reportFuncDef := d.FuncDef.GetFunctionCmdDefinition(functions_def.Report)

	installScriptTemplate := `
	# report function definition
	%s
	TOKEN="%s"
	INSTALL_URL="%s"
	PROXY_URL="%s"
	`
	installScript := fmt.Sprintf(
		installScriptTemplate, reportFuncDef, d.Params.WekaToken, installUrl, d.Params.ProxyUrl)

	if strings.HasSuffix(installUrl, ".tar") || strings.Contains(installUrl, ".tar?") {
		split := strings.Split(installUrl, "?")
		split = strings.Split(split[0], "/")
		tarName := split[len(split)-1]
		packageName := strings.TrimSuffix(tarName, ".tar")
		installTemplate := `
		TAR_NAME=%s
		PACKAGE_NAME=%s

		gsutil cp "$INSTALL_URL" /tmp || wget "$INSTALL_URL" -O /tmp/$TAR_NAME
		cd /tmp
		tar -xvf $TAR_NAME
		cd $PACKAGE_NAME
		`
		installScript += fmt.Sprintf(installTemplate, tarName, packageName)
	} else {
		installScript += `
		# https://gist.github.com/fungusakafungus/1026804
		function retry {
			local retry_max=$1
			local retry_sleep=$2
			shift 2
			local count=$retry_max
			while [ $count -gt 0 ]; do
					"$@" && break
					count=$(($count - 1))
					sleep $retry_sleep
			done
			[ $count -eq 0 ] && {
					echo "Retry failed [$retry_max]"
					return 1
			}
			return 0
		}

		retry 300 2 curl --fail --proxy "$PROXY_URL" --max-time 10 "$INSTALL_URL" -o install.sh
		`
	}

	installScript += `
	chmod +x install.sh
	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Installing weka\"}"
	status_code=$(curl -s -o /dev/null -w "%{http_code}" -X PUT http://169.254.169.254/latest/api/token -H 'X-aws-ec2-metadata-token-ttl-seconds: 21600')
	if [[ "$status_code" -eq 200 ]] ; then
		echo "Succeeded to get aws token"
	else
		echo "Failed to get aws token"
		sed -i -e 's/--noproxy \".amazonaws.com\"//g' ./install.sh
		sed -i '/no_proxy/d' install.sh
	fi
	PROXY="$PROXY_URL" ./install.sh
	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"Weka software installation completed\"}"
	`

	return dedent.Dedent(installScript)
}
