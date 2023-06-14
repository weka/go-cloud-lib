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
		res=${core_ids["$core_idx_begin"]}
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

		if [ -n "$gateways" ]; then #azure and gcp
			gateways=($gateways)
		fi

		net=" "
		for ((i; i<$j; i++)); do
			eth=eth$i
			subnet_inet=$(ifconfig $eth | grep 'inet ' | awk '{print $2}')
			if [ -z $subnet_inet ];then
				net=""
				break
			fi
			enp=$(ls -l /sys/class/net/$eth/ | grep lower | awk -F"_" '{print $2}' | awk '{print $1}') #for azure
			if [ -z $enp ];then
				enp=$(ethtool -i $eth | grep bus-info | awk '{print $2}') #pci for gcp
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
