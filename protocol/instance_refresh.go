package protocol

import "time"

// InstanceRefreshAction represents the action to perform on an instance refresh
type InstanceRefreshAction string

const (
	InstanceRefreshActionStart  InstanceRefreshAction = "start"
	InstanceRefreshActionStatus InstanceRefreshAction = "status"
	InstanceRefreshActionCancel InstanceRefreshAction = "cancel"
)

// InstanceRefreshPhase represents the current phase of an instance refresh
type InstanceRefreshPhase string

const (
	InstanceRefreshPhaseIdle      InstanceRefreshPhase = "idle"
	InstanceRefreshPhaseCompleted InstanceRefreshPhase = "completed"
	InstanceRefreshPhaseCancelled InstanceRefreshPhase = "cancelled"

	// Scale up phases
	InstanceRefreshPhaseProvisioning            InstanceRefreshPhase = "provisioning"
	InstanceRefreshPhaseWaitingWekaAfterScaleUp InstanceRefreshPhase = "waiting_weka_reach_desired_state_after_scale_up"

	// Scale down phases
	InstanceRefreshPhaseWaitingWekaAfterScaleDown InstanceRefreshPhase = "waiting_weka_reach_desired_state_after_scale_down"
	InstanceRefreshPhaseTerminating               InstanceRefreshPhase = "terminating"
)

// InstanceRefreshState represents the persistent state of an instance refresh
// This is stored in blob storage and updated as the refresh progresses
type InstanceRefreshState struct {
	// Configuration (set at trigger time)
	OriginalSize    int `json:"original_size"`
	ScaleUpInterval int `json:"scale_up_interval"`

	// Progress tracking
	Phase            InstanceRefreshPhase `json:"phase"`
	CurrentIteration int                  `json:"current_iteration"`
	TotalIterations  int                  `json:"total_iterations"`

	// Instance tracking - snapshot of original instances at start
	OriginalInstanceIds []string `json:"original_instance_ids"`

	// Timing
	StartedAt          time.Time  `json:"started_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	IterationStartedAt *time.Time `json:"iteration_started_at,omitempty"`

	// Per-iteration timing (for ETA calculation)
	IterationDurations []time.Duration `json:"iteration_durations,omitempty"`

	// Error tracking
	LastError *string `json:"last_error,omitempty"`
}

// WekaHealthStatus shows the Weka cluster health metrics being tracked during instance refresh
type WekaHealthStatus struct {
	IoStatus                 string `json:"io_status"`
	ClusterStatus            string `json:"cluster_status"`
	BackendContainersActive  int    `json:"backend_containers_active"`
	BackendContainersTotal   int    `json:"backend_containers_total"`
	DrivesActive             int    `json:"drives_active"`
	DrivesTotal              int    `json:"drives_total"`
	TargetBackendContainers  int    `json:"target_backend_containers"`
	TargetDrives             int    `json:"target_drives"`
}

// InstanceRefreshProgress represents the current progress of an instance refresh
// This is returned to the user when checking status
type InstanceRefreshProgress struct {
	Phase                InstanceRefreshPhase `json:"phase"`
	PhaseCompleted       bool                 `json:"phase_completed,omitempty"`
	InstancesInitial     int                  `json:"instances_initial"`
	InstancesCurrent     *int                 `json:"instances_current,omitempty"`
	InstancesReplaced    *int                 `json:"instances_replaced,omitempty"`
	CurrentIteration     int                  `json:"current_iteration"`
	TotalIterations      int                  `json:"total_iterations"`
	StartedAt            string               `json:"started_at,omitempty"`
	Duration             string               `json:"duration,omitempty"`
	AvgIterationDuration string               `json:"avg_iteration_duration,omitempty"`
	LastError            *string              `json:"last_error,omitempty"`
	WekaHealth           *WekaHealthStatus    `json:"weka_health,omitempty"`
}

