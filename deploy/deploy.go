package deploy

func (d *DeployScriptGenerator) GetDeployScript() string {
	if d.Params.Protocol == "" {
		return d.GetBackendDeployScript()
	}
	return d.GetProtocolGWDeployScript()
}
