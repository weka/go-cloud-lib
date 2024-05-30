package weka

import (
	"time"

	"github.com/google/uuid"
)

type JrpcMethod string

const (
	JrpcHostList                 JrpcMethod = "hosts_list"
	JrpcNodeList                 JrpcMethod = "nodes_list"
	JrpcDrivesList               JrpcMethod = "disks_list"
	JrpcRemoveDrive              JrpcMethod = "cluster_remove_drives"
	JrpcRemoveHost               JrpcMethod = "cluster_remove_host"
	JrpcDeactivateDrives         JrpcMethod = "cluster_deactivate_drives"
	JrpcDeactivateHosts          JrpcMethod = "cluster_deactivate_hosts"
	JrpcStatus                   JrpcMethod = "status"
	JrpcEmitCustomEvent          JrpcMethod = "events_trigger_custom"
	JrpcInterfaceGroupList       JrpcMethod = "interface_group_list"
	JrpcInterfaceGroupDeletePort JrpcMethod = "interface_group_delete_port"
	JrpcManualOverrideList       JrpcMethod = "manual_override_list"
)

type HostListResponse map[HostId]Host
type DriveListResponse map[DriveId]Drive
type NodeListResponse map[NodeId]Node
type InterfaceGroupListResponse []InterfaceGroup
type ManualDebugOverrideListResponse map[OverrideId]DebugOverride

type Activity struct {
	NumOps                    float32 `json:"num_ops"`
	NumReads                  float32 `json:"num_reads"`
	NumWrites                 float32 `json:"num_writes"`
	ObsDownloadBytesPerSecond float32 `json:"obs_download_bytes_per_second"`
	ObsUploadBytesPerSecond   float32 `json:"obs_upload_bytes_per_second"`
	SumBytesRead              float32 `json:"sum_bytes_read"`
	SumBytesWritten           float32 `json:"sum_bytes_written"`
}

type HostsCount struct {
	Active int `json:"active"`
	Total  int `json:"total"`
}
type ClusterCount struct {
	ActiveCount int        `json:"active_count"`
	Backends    HostsCount `json:"backends"`
	Clients     HostsCount `json:"clients"`
	TotalCount  int        `json:"total_count"`
}
type StatusResponse struct {
	IoStatus string       `json:"io_status"`
	Upgrade  string       `json:"upgrade"`
	Activity Activity     `json:"activity"`
	Hosts    ClusterCount `json:"hosts"`
}

type Host struct {
	AddedTime        time.Time `json:"added_time"`
	StateChangedTime time.Time `json:"state_changed_time"`
	State            string    `json:"state"`
	Status           string    `json:"status"`
	HostIp           string    `json:"host_ip"`
	Aws              struct {
		InstanceId string `json:"instance_id"`
	} `json:"aws"`
	ContainerName     string `json:"container_name"`
	Mode              string `json:"mode"`
	MachineIdentifier string `json:"machine_identifier"`
	AutoRemoveTimeout int    `json:"auto_remove_timeout"`
}

type Drive struct {
	HostId         HostId    `json:"host_id"`
	Status         string    `json:"status"`
	Uuid           uuid.UUID `json:"uuid"`
	ShouldBeActive bool      `json:"should_be_active"`
}

type Node struct {
	LastFencingTime *time.Time `json:"last_fencing_time"`
	Status          string     `json:"status"`
	UpSince         *time.Time `json:"up_since"`
	HostId          HostId     `json:"host_id"`
}

type InterfaceGroupPort struct {
	HostUid string `json:"host_uid"`
	HostId  HostId `json:"host_id"`
	Port    string `json:"port"`
	Status  string `json:"status"`
}

type InterfaceGroup struct {
	SubnetMask      string               `json:"subnet_mask"`
	Ports           []InterfaceGroupPort `json:"ports"`
	Name            string               `json:"name"`
	Uid             string               `json:"uid"`
	Ips             []string             `json:"ips"`
	AllowManageGids bool                 `json:"allow_manage_gids"`
	Type            string               `json:"type"`
	Gateway         string               `json:"gateway"`
	Status          string               `json:"status"`
}

type DebugOverride struct {
	BucketId     string      `json:"bucket_id"`
	BucketString string      `json:"bucket_string"`
	Comment      string      `json:"comment"`
	Enabled      bool        `json:"enabled"`
	Forced       bool        `json:"forced"`
	Key          string      `json:"key"`
	NegateBucket bool        `json:"negate_bucket"`
	OverrideId   string      `json:"override_id"`
	TimeCreated  time.Time   `json:"time_created"`
	TimeEnabled  bool        `json:"time_enabled"`
	Value        interface{} `json:"value"`
}
