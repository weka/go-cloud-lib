package deploy

import (
	"fmt"
	"github.com/lithammer/dedent"
	"github.com/weka/go-cloud-lib/bash_functions"
	"github.com/weka/go-cloud-lib/functions_def"
)

func (d *DeployScriptGenerator) GetDataServiceDeployScript() string {
	wekaInstallScript := d.GetWekaInstallScript()
	protectFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Protect)
	statusFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Status)
	reportFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Report)
	fetchFunc := d.FuncDef.GetFunctionCmdDefinition(functions_def.Fetch)
	setBackendIpFunc := bash_functions.SetBackendIpFunction()

	template := `
	#!/bin/bash
	VM=%s
	LOAD_BALANCER_IP="%s"

	# protect function definition (if any)
	%s

	# fetch function definition
	%s

	# status function definition
	%s

	# report function definition
	%s

	# deviceNameCmd
	wekaiosw_device="%s"
	# wekio partition setup
	%s

	# install script
	%s

	# set_backend_ip bash function definition
	%s

	weka local stop
	weka local rm default --force

	clusterized=$(status "{\"type\": \"status\"}" | jq .clusterized)
	while [ "$clusterized" != "true" ];
	do
		sleep 10
		clusterized=$(status "{\"type\": \"status\"}" | jq .clusterized)
		echo "Clusterized: $clusterized, going to sleep for 10 seconds"
	done

	fetch_result=$(fetch "{\"fetch_weka_credentials\": false}")
	# set value for backend_ip variable
	set_backend_ip
	echo "(date -u): backend_ip: $backend_ip"

	echo "$(date -u): setting up weka data service"

	if [ -z "$LOAD_BALANCER_IP" ]; then
		join_ips=$ips_str
	else
		join_ips=$LOAD_BALANCER_IP
	fi

	weka local setup container --name dataserv --base-port 14000 --join-ips $join_ips  --only-dataserv-cores --memory 3.5GB --allow-mix-setting
	echo "$(date -u): success to run weka data services container"

	protect "{\"vm\": \"$VM\", \"protocol\": \"data\"}"

	echo "$(date -u): finished preparation for data services container"
	`
	script := fmt.Sprintf(
		template,
		d.Params.VMName,
		d.Params.LoadBalancerIP,
		protectFunc,
		fetchFunc,
		statusFunc,
		reportFunc,
		d.DeviceNameCmd,
		bash_functions.GetWekaPartitionScript(),
		wekaInstallScript,
		setBackendIpFunc,
	)
	return dedent.Dedent(script)
}
