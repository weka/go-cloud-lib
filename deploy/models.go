package deploy

import (
	"fmt"
	"strings"

	"github.com/lithammer/dedent"
	"github.com/weka/go-cloud-lib/functions_def"
	"github.com/weka/go-cloud-lib/protocol"
)

type DeploymentParams struct {
	VMName                    string
	InstanceParams            protocol.BackendCoreCount
	WekaInstallUrl            string
	WekaToken                 string
	InstallDpdk               bool
	NicsNum                   string
	NvmesNum                  int
	ProxyUrl                  string
	FindDrivesScript          string
	Gateways                  []string
	Protocol                  protocol.ProtocolGW
	NFSInterfaceGroupName     string //for NFS protocol gw setup
	NFSClientGroupName        string //for NFS protocol gw setup
	NFSSecondaryIpsNum        int    //for NFS protocol gw setup
	ProtocolGatewayFeCoresNum int    //for protocol gw setup
	LoadBalancerIP            string
	GetPrimaryIpCmd           string
}

type DeployScriptGenerator struct {
	DeviceNameCmd string
	Params        DeploymentParams
	FuncDef       functions_def.FunctionDef
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
	PROTOCOL="%s"
	`
	installScript := fmt.Sprintf(
		installScriptTemplate,
		reportFuncDef,
		d.Params.WekaToken,
		installUrl,
		d.Params.ProxyUrl,
		d.Params.Protocol,
	)

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
					echo "Retrying $* in $retry_sleep seconds..."
					sleep $retry_sleep
			done
			[ $count -eq 0 ] && {
					echo "$(date -u): Retry failed [$retry_max]"
					return 1
			}
			return 0
		}

		report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"$PROTOCOL\", \"type\": \"progress\", \"message\": \"Downloading weka install script\"}"
		retry 300 2 curl --fail --proxy "$PROXY_URL" --max-time 10 "$INSTALL_URL" -o install.sh
		`
	}

	installScript += `
	chmod +x install.sh
	report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"$PROTOCOL\", \"type\": \"progress\", \"message\": \"Installing weka\"}"
	status_code=$(curl -s -o /dev/null -w "%{http_code}" -X PUT http://169.254.169.254/latest/api/token -H 'X-aws-ec2-metadata-token-ttl-seconds: 21600')
	if [[ "$status_code" -eq 200 ]] ; then
		echo "Succeeded to get aws token"
	else
		echo "Failed to get aws token"
		sed -i -e 's/--noproxy \".amazonaws.com\"//g' ./install.sh
		sed -i '/no_proxy/d' install.sh
	fi
	PROXY="$PROXY_URL" ./install.sh
	report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"$PROTOCOL\", \"type\": \"progress\", \"message\": \"Weka software installation completed\"}"
	`

	return dedent.Dedent(installScript)
}
