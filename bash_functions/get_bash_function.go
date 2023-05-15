package bash_functions

import (
	"github.com/lithammer/dedent"
)

func GetCoreIds() string {
	s := `
	core_ids=$(cat /sys/devices/system/cpu/cpu*/topology/thread_siblings_list | cut -d "-" -f 1 |  cut -d "," -f 1 | sort -u | tr '\n' ' ')
	core_ids="${core_ids[@]/0}"
	IFS=', ' read -r -a core_ids <<< "$core_ids"
	core_idx_begin=0
	get_core_ids() {
		core_idx_end=$(($core_idx_begin + $1))
		res=${core_ids[i]}
		for (( i=$(($core_idx_begin + 1)); i<$core_idx_end; i++ ))
		do
			res=$res,${core_ids[i]}
		done
		core_idx_begin=$core_idx_end
		eval "$2=$res"
	}
	`
	return dedent.Dedent(s)
}

func GetNetStrForDpdk() string {
	s := `
	function getNetStrForDpdk() {
		i=$1
		j=$2
		gateways=$3
		subnets=$4

		if [ -n "$gateways" ]; then #azure and gcp
			gateways=($gateways)
		fi

		if [ -n "$subnets" ]; then #azure only
			subnets=($subnets)
		fi

		net=" "
		for ((i; i<$j; i++)); do
			if [ -n "$subnets" ]; then
				subnet=${subnets[$i]}
				subnet_inet=$(curl -s -H Metadata:true –noproxy “*” http://169.254.169.254/metadata/instance/network\?api-version\=2021-02-01 | jq --arg subnet "$subnet" '.interface[] | select(.ipv4.subnet[0].address==$subnet)' | jq -r .ipv4.ipAddress[0].privateIpAddress)
				eth=$(ifconfig | grep -B 1 $subnet_inet |  head -n 1 | cut -d ':' -f1)
			else
				eth=eth$i
				subnet_inet=$(ifconfig $eth | grep 'inet ' | awk '{print $2}')
			fi
			if [ -z $subnet_inet ];then
				net=""
				break
			fi
			enp=$(ls -l /sys/class/net/$eth/ | grep lower | awk -F"_" '{print $2}' | awk '{print $1}') #for azure
			if [ -z $enp ];then
				enp=$eth
			fi
			bits=$(ip -o -f inet addr show $eth | awk '{print $4}')
			IFS='/' read -ra netmask <<< "$bits"
			
			if [ -n "$gateways" ]; then
				gateway=${gateways[$i]}
				net="$net --net $enp/$subnet_inet/${netmask[1]}/$gateway"
			else
				net="$net --net $eth" #aws
			fi
		done
	}
	`
	return dedent.Dedent(s)
}

func GetHashedPrivateIpBashCmd() string {
	return "printf $(hostname -I) | sha256sum | tr -d '-' | cut -c1-16"
}
