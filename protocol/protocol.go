package protocol

import (
	"fmt"
	"time"

	"github.com/weka/go-cloud-lib/lib/types"
	"github.com/weka/go-cloud-lib/lib/weka"
)

var Version = 1

type HgInstance struct {
	Id        string
	PrivateIp string
}

type HostGroupInfoResponse struct {
	Username                     string                `json:"username"`
	Password                     string                `json:"password,omitempty"`
	AdminPassword                string                `json:"admin_password,omitempty"`
	WekaBackendsDesiredCapacity  int                   `json:"weka_backends_desired_capacity"`
	NfsBackendsDesiredCapacity   int                   `json:"nfs_backends_desired_capacity"`
	WekaBackendInstances         []HgInstance          `json:"weka_backend_instances"`
	NfsBackendInstances          []HgInstance          `json:"nfs_backend_instances"`
	NfsInterfaceGroupInstanceIps map[string]types.Nilt `json:"nfs_interface_group_instance_ips"` // the key is the instance ip
	DownBackendsRemovalTimeout   time.Duration         `json:"down_backends_removal_timeout"`
	BackendIps                   []string              `json:"backend_ips"`
	Role                         string                `json:"role"`
	Version                      int                   `json:"version"`
}

func (hg *HostGroupInfoResponse) WithHiddenPassword() HostGroupInfoResponse {
	hgCopy := *hg
	hgCopy.Password = "********"
	hgCopy.AdminPassword = "********"
	return hgCopy
}

func (hg *HostGroupInfoResponse) Validate() error {
	var errs []error
	if hg.Username == "" {
		err := fmt.Errorf("username is empty")
		errs = append(errs, err)
	}
	if hg.Password == "" {
		err := fmt.Errorf("password is empty")
		errs = append(errs, err)
	}
	if hg.Role == "" {
		err := fmt.Errorf("role is empty")
		errs = append(errs, err)
	}
	if hg.WekaBackendsDesiredCapacity <= 0 {
		err := fmt.Errorf("weka_backends_desired_capacity should greater than 0")
		errs = append(errs, err)
	}
	if hg.DownBackendsRemovalTimeout <= 0 {
		err := fmt.Errorf("down_backends_removal_timeout should greater than 0")
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("validation failed: %v", errs)
	}
	return nil
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
	S3   ProtocolGW = "s3"
	DATA ProtocolGW = "data"
)

type NFSParams struct {
	InterfaceGroupName string
	SecondaryIps       []string
	ContainersUid      []string
	NicNames           []string
	HostsNum           int
	Gateway            string // optional
	SubnetMask         string // optional
}

type Vm struct {
	Name         string     `json:"name"`
	Protocol     ProtocolGW `json:"protocol"`
	ContainerUid string     `json:"container_uid"` // protocol frontend container uid
	NicName      string     `json:"nic_name"`      // protocol management nic name
}

type FetchRequest struct {
	FetchWekaCredentials bool `json:"fetch_weka_credentials"`
	ShowAdminPassword    bool `json:"show_admin_password,omitempty"`
}
