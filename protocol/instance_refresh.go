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
	InstanceRefreshPhaseFailed    InstanceRefreshPhase = "failed"
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
	TargetConfigHash string `json:"target_config_hash"`
	OriginalSize     int    `json:"original_size"`
	ScaleUpInterval  int    `json:"scale_up_interval"`

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
}

// InstanceRefreshParams contains parameters needed for instance refresh health checks
type InstanceRefreshParams struct {
	OriginalSize    int
	ScaleUpInterval int
	ContainersPerVm int
	DrivesPerVm     int
}
