package weka

import (
	"github.com/google/uuid"
	"time"
)

type JrpcMethod string

const (
	JrpcHostList         JrpcMethod = "hosts_list"
	JrpcNodeList         JrpcMethod = "nodes_list"
	JrpcDrivesList       JrpcMethod = "disks_list"
	JrpcRemoveDrive      JrpcMethod = "cluster_remove_drives"
	JrpcRemoveHost       JrpcMethod = "cluster_remove_host"
	JrpcDeactivateDrives JrpcMethod = "cluster_deactivate_drives"
	JrpcDeactivateHosts  JrpcMethod = "cluster_deactivate_hosts"
	JrpcStatus           JrpcMethod = "status"
)

type HostListResponse map[HostId]Host
type DriveListResponse map[DriveId]Drive
type NodeListResponse map[NodeId]Node

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
