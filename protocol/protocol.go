package protocol

import (
	"fmt"
	"github.com/weka/go-cloud-lib/lib/weka"
	"time"
)

var Version = 1

type HgInstance struct {
	Id        string
	PrivateIp string
}

type HostGroupInfoResponse struct {
	Username                    string       `json:"username"`
	Password                    string       `json:"password"`
	WekaBackendsDesiredCapacity int          `json:"weka_backends_desired_capacity"`
	NFSBackendsDesiredCapacity  int          `json:"nfs_backends_desired_capacity"`
	WekaBackendInstances        []HgInstance `json:"weka_backend_instances"`
	NfsBackendInstances         []HgInstance `json:"nfs_backend_instances"`
	BackendIps                  []string     `json:"backend_ips"`
	Role                        string       `json:"role"`
	Version                     int          `json:"version"`
}

type ScaleResponseHost struct {
	InstanceId string      `json:"instance_id"`
	PrivateIp  string      `json:"private_ip"`
	State      string      `json:"status"`
	AddedTime  time.Time   `json:"added_time"`
	HostId     weka.HostId `json:"host_id"`
}

type ScaleResponse struct {
	Hosts           []ScaleResponseHost `json:"hosts"`
	ToTerminate     []HgInstance        `json:"to_terminate"`
	TransientErrors []string
	Version         int `json:"version"`
}

func (r *ScaleResponse) AddTransientErrors(errs []error, caller string) {
	for _, err := range errs {
		r.TransientErrors = append(r.TransientErrors, fmt.Sprintf("%s:%s", caller, err.Error()))
	}
}

func (r *ScaleResponse) AddTransientError(err error, caller string) {
	r.TransientErrors = append(r.TransientErrors, fmt.Sprintf("%s:%s", caller, err.Error()))
}

type TerminatedInstance struct {
	InstanceId string    `json:"instance_id"`
	Creation   time.Time `json:"creation_date"`
}
type TerminatedInstancesResponse struct {
	Instances       []TerminatedInstance `json:"set_to_terminate_instances"`
	TransientErrors []string
	Version         int `json:"version"`
}

func (r *TerminatedInstancesResponse) AddTransientErrors(errs []error) {
	for _, err := range errs {
		r.TransientErrors = append(r.TransientErrors, err.Error())
	}
}

func (r *TerminatedInstancesResponse) AddTransientError(err error, caller string) {
	r.TransientErrors = append(r.TransientErrors, fmt.Sprintf("%s:%s", caller, err.Error()))
}

type BackendCoreCount struct {
	Compute       int
	Frontend      int
	Drive         int
	Converged     bool
	ComputeMemory string
}

type BackendCoreCounts map[string]BackendCoreCount

type ObsParams struct {
	Name              string
	TieringSsdPercent string
}

type ClusterCreds struct {
	Username string
	Password string
}

type ProtocolGW string

const (
	NFS  ProtocolGW = "nfs"
	SMB  ProtocolGW = "smb"
	SMBW ProtocolGW = "smbw"
)

type NFSParams struct {
	InterfaceGroupName string
	ClientGroupName    string
	SecondaryIps       []string
	ContainersUid      []string
	NicNames           []string
	HostsNum           int
}

type Vm struct {
	Name         string     `json:"name"`
	Protocol     ProtocolGW `json:"protocol"`
	ContainerUid string     `json:"container_uid"` // protocol frontend container uid
	NicName      string     `json:"nic_name"`      // protocol management nic name
}
