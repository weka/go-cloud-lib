package join

import (
	"fmt"

	"github.com/lithammer/dedent"
	"github.com/weka/go-cloud-lib/deploy"
	"github.com/weka/go-cloud-lib/functions_def"
)

type JoinNFSScriptGenerator struct {
	DeviceNameCmd      string
	DeploymentParams   deploy.DeploymentParams
	InterfaceGroupName string
	FuncDef            functions_def.FunctionDef
	Name               string //for AWS we provide here the instance id
}

func (j *JoinNFSScriptGenerator) GetJoinNFSHostScript() string {
	deployScriptGenerator := deploy.DeployScriptGenerator{
		DeviceNameCmd: j.DeviceNameCmd,
		FuncDef:       j.FuncDef,
		Params:        j.DeploymentParams,
	}
	deploymentBashScript := deployScriptGenerator.GetBaseProtocolGWDeployScript()
	joinNfsFinalizationFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.JoinNfsFinalization)
	reportFunc := j.FuncDef.GetFunctionCmdDefinition(functions_def.Report)
	joinScriptTemplate := `
	interface_group_name="%s"

	# join_nfs_finalization function definition
	%s

	# report function definition
	%s

	instance_name="%s"

	function wait_for_nfs_interface_group(){
	  max_retries=12 # 12 * 10 = 2 minutes
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
		return 1
	  fi
	}
	
	weka_rest interfacegroups | jq -r .data
	interface_group_uid=$(weka_rest interfacegroups | jq -r .data[].uid)
	weka_rest "interfacegroups/$interface_group_uid/ports/$container_uid" "{\"port\":\"$nic_name\"}"

	wait_for_nfs_interface_group || exit 1
	weka_rest interfacegroups | jq -r .data

	echo "$(date -u): NFS setup complete"

	join_nfs_finalization "{\"name\": \"$instance_name\", \"protocol\": \"nfs\"}"
	echo "completed successfully" > /tmp/weka_join_nfs_completion_validation
	report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"nfs\", \"type\": \"progress\", \"message\": \"Joining new NFS instance completed successfully\"}"
	`
	nfsSetupScript := fmt.Sprintf(
		joinScriptTemplate,
		j.InterfaceGroupName,
		joinNfsFinalizationFunc,
		reportFunc,
		j.Name,
	)

	return deploymentBashScript + dedent.Dedent(nfsSetupScript)
}
