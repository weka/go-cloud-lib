package deploy

import "github.com/weka/go-cloud-lib/protocol"

func (d *DeployScriptGenerator) GetDeployScript() string {
	if d.Params.Protocol == "" {
		return d.GetBackendDeployScript()
	} else if d.Params.Protocol == protocol.DATA {
		return d.GetDataServiceDeployScript()
	}
	return d.GetProtocolGWDeployScript()
}
