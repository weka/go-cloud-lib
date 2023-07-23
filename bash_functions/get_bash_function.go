package bash_functions

import (
	"github.com/lithammer/dedent"
)

func GetCoreIds() string {
	s := `
	set -ex
	numa_range=()
	numa=()
	create_numa_range() {
		r=$1
		dynamic_array=$2
		start_value=$(echo "$r" | awk -F"-" '{print $1}')
		end_value=$(echo "$r" | awk -F"-" '{print $2}')

		for (( i=$(($start_value + 1)) ; i<=$end_value ; i++ )); do
				rem=$(( $i % 2 ))
				if [[ $rem -eq 0 ]]; then
					dynamic_array+=("$i")
				fi
		done
	}
	numa_num=$(lscpu | grep "NUMA node(s):" | awk '{print $3}')
	
	for ((i=0; i<$numa_num; i++));do
		numa_ids=$(lscpu | grep "NUMA node$i CPU(s):" | awk '{print $4}')
		numa_range[$i]=$numa_ids
	done
	for ((j=0; j<$numa_num; j++)); do
    		dynamic_array=()
			if [[ "${numa_range[$j]}" =~ "," ]]; then
				IFS=',' read -ra range <<< "${numa_range[$j]}"
				for i in "${range[@]}"; do
					create_numa_range "$i" $dynamic_array
					numa[$j]="${dynamic_array[@]}"
				done
			else
				create_numa_range "${numa_range[$j]}" $dynamic_array
				numa[$j]="${dynamic_array[@]}"
			fi
	done

	core_idx_begin=0
	get_core_ids() {
		core_idx_end=$(($core_idx_begin + $1))
		core_ids=(${numa[0]})
		res=${core_ids["$core_idx_begin"]}
		if [[ ${numa_num} > 1 && $2 == compute_core_ids ]]; then
			for (( i=$(($core_idx_begin+1)); i<$core_idx_end; i++ )); do
				index=$(($i%2))
				core_ids=(${numa[$index]})
				res=$res,${core_ids[i]}
			done
		else
			core_ids=(${numa[0]})
			for (( i=$(($core_idx_begin + 1)); i<$core_idx_end; i++ )); do
				res=$res,${core_ids[i]}
			done
		fi
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
		gateways=($gateways) #azure and gcp

		net=""
		for ((i; i<$j; i++)); do
			eth=eth$i
			subnet_inet=$(ifconfig $eth | grep 'inet ' | awk '{print $2}')
			if [ -z $subnet_inet ] || [ ${#gateways[@]} -eq 0 ];then
				net="$net --net $eth" #aws
				continue
			fi
			enp=$(ls -l /sys/class/net/$eth/ | grep lower | awk -F"_" '{print $2}' | awk '{print $1}') #for azure
			if [ -z $enp ];then
				enp=$(ethtool -i $eth | grep bus-info | awk '{print $2}') #pci for gcp
			fi
			bits=$(ip -o -f inet addr show $eth | awk '{print $4}')
			IFS='/' read -ra netmask <<< "$bits"
			
			gateway=${gateways[$i]}
			net="$net --net $enp/$subnet_inet/${netmask[1]}/$gateway"
		done
	}
	`
	return dedent.Dedent(s)
}

func GetHashedPrivateIpBashCmd() string {
	return "printf $(hostname -I) | sha256sum | tr -d '-' | cut -c1-16"
}

func GetWekaPartitionScript() string {
	s := `
	if [ ! -z "$wekaiosw_device" ]; then
		echo "--------------------------------------------"
		echo " Creating local filesystem on WekaIO volume "
		echo "--------------------------------------------"

		sleep 4
		mkfs.ext4 -L wekaiosw "$wekaiosw_device" || return 1
		mkdir -p /opt/weka || return 1
		mount "$wekaiosw_device" /opt/weka || return 1
		echo "LABEL=wekaiosw /opt/weka ext4 defaults 0 2" >>/etc/fstab
	fi`
	return dedent.Dedent(s)
}
