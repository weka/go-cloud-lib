package protocol

import (
	"github.com/weka/go-cloud-lib/lib/weka"
	"time"
)

type ClusterStatus struct {
	InitialSize int        `json:"initial_size"`
	DesiredSize int        `json:"desired_size"`
	Clusterized bool       `json:"clusterized"`
	WekaStatus  WekaStatus `json:"weka_status"`
}

type Reports struct {
	ReadyForClusterization []string            `json:"ready_for_clusterization"`
	Progress               map[string][]string `json:"progress"`
	Errors                 map[string][]string `json:"errors"`
}

type ClusterCloud struct {
	Enabled bool   `json:"enabled"`
	Healthy bool   `json:"healthy"`
	Proxy   string `json:"proxy"`
	Url     string `json:"url"`
}

type ClusterCapacity struct {
	TotalBytes         float32 `json:"total_bytes"`
	HotSpareBytes      float32 `json:"hot_spare_bytes"`
	UnprovisionedBytes float32 `json:"unprovisioned_bytes"`
}

type ClusterNodes struct {
	BlackListed int `json:"black_listed"`
	Total       int `json:"total"`
}

type ClusterUsage struct {
	DriveCapacityGb  int `json:"drive_capacity_gb"`
	UsableCapacityGb int `json:"usable_capacity_gb"`
	ObsCapacityGb    int `json:"obs_capacity_gb"`
}

type ClusterLicensing struct {
	IoStartEligibility bool         `json:"io_start_eligibility"`
	Usage              ClusterUsage `json:"usage"`
	Mode               string       `json:"mode"`
}

type WekaStatus struct {
	HotSpare               int               `json:"hot_spare"`
	IoStatus               string            `json:"io_status"`
	Drives                 weka.HostsCount   `json:"drives"`
	Name                   string            `json:"name"`
	IoStatusChangedTime    time.Time         `json:"io_status_changed_time"`
	IoNodes                weka.HostsCount   `json:"io_nodes"`
	Cloud                  ClusterCloud      `json:"cloud"`
	ReleaseHash            string            `json:"release_hash"`
	Hosts                  weka.ClusterCount `json:"hosts"`
	StripeDataDrives       int               `json:"stripe_data_drives"`
	Release                string            `json:"release"`
	ActiveAlertsCount      int               `json:"active_alerts_count"`
	Capacity               ClusterCapacity   `json:"capacity"`
	IsCluster              bool              `json:"is_cluster"`
	Status                 string            `json:"status"`
	StripeProtectionDrives int               `json:"stripe_protection_drives"`
	Guid                   string            `json:"guid"`
	Nodes                  ClusterNodes      `json:"nodes"`
	Licensing              ClusterLicensing  `json:"licensing"`
}
