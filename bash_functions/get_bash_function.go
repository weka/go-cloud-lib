package bash_functions

import (
	"github.com/lithammer/dedent"
)

func GetCoreIds() string {
	s := `
	numa_ranges=()
	numa=()

	append_numa_core_ids_to_list() {
		r=$1
		dynamic_array=$2
		numa_min=$(echo "$r" | awk -F"-" '{print $1}')
		numa_max=$(echo "$r" | awk -F"-" '{print $2}')

		thread_siblings_list=$(cat /sys/devices/system/cpu/cpu*/topology/thread_siblings_list)
		while IFS= read -r thread_siblings; do
			core_id=$(echo "$thread_siblings" | cut -d '-' -f 1 |  cut -d ',' -f 1)
			if [[ $core_id -ne 0 && $core_id -ge $numa_min && $core_id -le $numa_max && ! " ${dynamic_array[@]} " =~ " $core_id " ]];then
				dynamic_array+=($core_id)
			fi
		done <<< "$thread_siblings_list"
	}

	numa_num=$(lscpu | grep "NUMA node(s):" | awk '{print $3}')
	
	for ((i=0; i<$numa_num; i++));do
		numa_ids=$(lscpu | grep "NUMA node$i CPU(s):" | awk '{print $4}')
		numa_ranges[$i]=$numa_ids
	done
	for ((j=0; j<$numa_num; j++)); do
    		dynamic_array=()
			if [[ "${numa_ranges[$j]}" =~ "," ]]; then
				IFS=',' read -ra range <<< "${numa_ranges[$j]}"
				for i in "${range[@]}"; do
					append_numa_core_ids_to_list "$i" $dynamic_array
					numa[$j]="${dynamic_array[@]}"
				done
			else
				append_numa_core_ids_to_list "${numa_ranges[$j]}" $dynamic_array
				numa[$j]="${dynamic_array[@]}"
			fi
	done

	core_idx_begin=0
	get_core_ids() {
		core_idx_end=$(($core_idx_begin + $1))
		if [[ ${numa_num} > 1 ]]; then
			index=$((core_idx_begin%2))
			core_ids=(${numa[$index]})
			res=${core_ids[$((core_idx_begin/2))]}
			for (( i=$(($core_idx_begin+1)); i<$core_idx_end; i++ )); do
				index=$(($i%2))
				core_ids=(${numa[$index]})
				res=$res,${core_ids[$((i/2))]}
			done
		else
			core_ids=(${numa[0]})
			res=${core_ids["$core_idx_begin"]}
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
		# depends on getAllInterfaces function call
		i=$1
		j=$2
		gateways=$3
		gateways=($gateways) #azure and gcp

		net=""
		for ((i; i<$j; i++)); do
			interface=${all_interfaces[$i]}
			subnet_inet=$(ip -4 addr show $interface | grep inet | awk '{print $2}' | cut -d/ -f1)
			if [ -z $subnet_inet ] || [ ${#gateways[@]} -eq 0 ];then
				net="$net --net $interface" #aws
				continue
			fi
			enp=$(ls -l /sys/class/net/$interface/ | grep lower | awk -F"_" '{print $2}' | awk '{print $1}') #for azure
			if [ -n "$enp" ]; then
				net="$net --net $enp" #azure
				continue
			fi

			enp=$(ethtool -i $interface | grep bus-info | awk '{print $2}') #pci for gcp
			bits=$(ip -o -f inet addr show $interface | awk '{print $4}')
			IFS='/' read -ra netmask <<< "$bits"

			gateway=${gateways[$i]}
			net="$net --net $enp/$subnet_inet/${netmask[1]}/$gateway"
		done
	}
	`
	return dedent.Dedent(s)
}

func SetCurrentManagementIp() string {
	s := `
	# depends on getAllInterfaces function call
	current_mngmnt_ip=$(ip -4 addr show ${all_interfaces[0]} | grep inet | awk '{print $2}' | cut -d/ -f1)
	`
	return dedent.Dedent(s)
}

func GetHashedPrivateIpBashCmd() string {
	return "printf $(hostname -I) | sha256sum | tr -d '-' | cut -c1-16"
}

func GetWekaPartitionScript() string {
	s := `
	# requires 'report' function to be and PROTOCOL var (if needed)
	handle_error() {
	if [ "$1" -ne 0 ]; then
		report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"$PROTOCOL\", \"type\": \"error\", \"message\": \"${2}\"}"
		exit 1
	fi
	}

	if [ ! -z "$wekaiosw_device" ]; then
		echo "--------------------------------------------"
		echo " Creating local filesystem on WekaIO volume "
		echo "--------------------------------------------"
		echo "$(date -u): wekaiosw_device: $wekaiosw_device"

		sleep 4
		mkfs.ext4 -F -L wekaiosw "$wekaiosw_device" || handle_error $? "Failed to create filesystem on WekaIO volume"
		mkdir -p /opt/weka || handle_error $? "Failed to create /opt/weka directory"
		mount "$wekaiosw_device" /opt/weka || handle_error $? "Failed to mount WekaIO volume"
		echo "LABEL=wekaiosw /opt/weka ext4 defaults 0 2" >>/etc/fstab
	fi`
	return dedent.Dedent(s)
}

func WekaRestFunction() string {
	s := `
	function weka_rest() {
		# requires WEKA_USERNAME, WEKA_PASSWORD and backend_ip to be set
		endpoint="$1"
		data="$2"
		set +x
		tmpfile=$(mktemp)
		http_code=$(curl -sS -X POST --insecure -w "%{http_code}" -o "$tmpfile" "https://$backend_ip:14000/api/v2/login" -H "Content-Type: application/json" -d "{\"username\":\"$WEKA_USERNAME\",\"password\":\"$WEKA_PASSWORD\"}")
		response=$(cat "$tmpfile")
		rm -f "$tmpfile"
		if [ "$http_code" -lt 200 ] || [ "$http_code" -ge 300 ]; then
			echo "Login request failed with HTTP code: $http_code. Response: $response"
			set -x
			return 1
		fi
		access_token=$(echo "$response" | jq -r '.data.access_token')
		if [ -z "$access_token" ] || [ "$access_token" = "null" ]; then
			echo "Failed to extract access token. Response: $response"
			set -x
			return 1
		fi
		tmpfile=$(mktemp)
		if [ -z "$data" ]; then
			http_code=$(curl -sS --insecure -w "%{http_code}" -o "$tmpfile" "https://$backend_ip:14000/api/v2/$endpoint" -H "Authorization: Bearer $access_token")
		else
			http_code=$(curl -sS -X POST --insecure -w "%{http_code}" -o "$tmpfile" "https://$backend_ip:14000/api/v2/$endpoint" -H "Authorization: Bearer $access_token" -H "Content-Type: application/json" -d "$data")
		fi
		response=$(cat "$tmpfile")
		rm -f "$tmpfile"
		if [ "$http_code" -lt 200 ] || [ "$http_code" -ge 300 ]; then
			echo "API request failed for endpoint: $endpoint with HTTP code: $http_code. Response: $response"
			set -x
			return 1
		fi
		echo "$response"
		set -x
	}
	`
	return dedent.Dedent(s)
}

func SetBackendIpFunction() string {
	s := `
	function set_backend_ip() {
		# requires fetch func to be defined and LOAD_BALANCER_IP if exists
		if [ -z "$LOAD_BALANCER_IP" ]
		then
			ips_str=$(fetch | jq -r '.backend_ips | join(",")')

			random=$$
			echo $random
			ips_array=${ips_str//,/ }
			for backend_ip in ${ips_array[@]}; do
				if VERSION=$(curl -s -XPOST --insecure --data '{"jsonrpc":"2.0", "method":"client_query_backend", "id":"'$random'"}' https://$backend_ip:14000/api/v1 | sed  's/.*"software_release":"\([^"]*\)".*$/\1/g'); then
					if [[ "$VERSION" != "" ]]; then
						echo "(date -u): using backend ip: $backend_ip"
						break
					fi
				fi
			done
		else
			echo "(date -u): using load balancer ip: $LOAD_BALANCER_IP"
			backend_ip="$LOAD_BALANCER_IP"
		fi
	}
	`
	return dedent.Dedent(s)
}

func GetAllInterfaces() string {
	s := `
	function getAllInterfaces {
		# Store all interfaces in array for non-sequential interface naming (e.g., OCI: ens3, ens5, ens6, ens7)
		all_interfaces=($(ls /sys/class/net | grep -vE 'docker|veth|lo|enP|dtap' | sort --version-sort))
	}
	`

	return dedent.Dedent(s)
}

func PrefixToNetmask() string {
	s := `
	function prefix_to_netmask() {
		# Converts CIDR prefix length to dotted decimal netmask
		# Example: prefix_to_netmask 20 -> 255.255.240.0
		local prefix=$1
		local mask=$((0xffffffff << (32 - prefix)))
		echo "$(( (mask >> 24) & 0xff )).$(( (mask >> 16) & 0xff )).$(( (mask >> 8) & 0xff )).$(( mask & 0xff ))"
	}
	`

	return dedent.Dedent(s)
}
