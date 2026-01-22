package instance_refresh

import (
	"fmt"
	"time"

	"github.com/weka/go-cloud-lib/protocol"
)

// CheckClusterHealthy checks if the Weka cluster is healthy for a given expected size.
// This is used both after scale up and scale down - only the expectedSize differs.
// Returns nil if healthy, error describing what's not ready otherwise.
func CheckClusterHealthy(status protocol.WekaStatus, expectedSize, containersPerVm, drivesPerVm int) error {
	expectedContainers := expectedSize * containersPerVm
	expectedDrives := expectedSize * drivesPerVm

	if status.IoStatus != "STARTED" {
		return fmt.Errorf("io_status is %s, expected STARTED", status.IoStatus)
	}
	if status.Status != "OK" {
		return fmt.Errorf("cluster status is %s, expected OK", status.Status)
	}
	if status.Hosts.Backends.Active != expectedContainers {
		return fmt.Errorf("active backend containers %d != expected %d", status.Hosts.Backends.Active, expectedContainers)
	}
	if status.Hosts.Backends.Total != expectedContainers {
		return fmt.Errorf("total backend containers %d != expected %d", status.Hosts.Backends.Total, expectedContainers)
	}
	if status.Drives.Active != expectedDrives {
		return fmt.Errorf("active drives %d != expected %d", status.Drives.Active, expectedDrives)
	}
	if status.Drives.Total != expectedDrives {
		return fmt.Errorf("total drives %d != expected %d", status.Drives.Total, expectedDrives)
	}
	return nil
}

// CalculateTotalIterations computes how many iterations are needed for an instance refresh
func CalculateTotalIterations(clusterSize, scaleUpInterval int) int {
	if scaleUpInterval <= 0 {
		return 0
	}
	iterations := clusterSize / scaleUpInterval
	if clusterSize%scaleUpInterval != 0 {
		iterations++
	}
	return iterations
}

// GetScaledUpSize returns the target size during scale up phase
func GetScaledUpSize(originalSize, scaleUpInterval, currentIteration, totalIterations int) int {
	scaledUpSize := originalSize + scaleUpInterval
	// On the last iteration, we might scale up by less than the full interval
	if currentIteration == totalIterations {
		remaining := originalSize - (currentIteration-1)*scaleUpInterval
		if remaining < scaleUpInterval {
			scaledUpSize = originalSize + remaining
		}
	}
	return scaledUpSize
}

// FormatDuration formats a duration in a human-readable way
// Examples: "1h 30m", "45m 20s", "30s"
func FormatDuration(d time.Duration) string {
	d = d.Round(time.Second)

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}
	if minutes > 0 {
		if seconds > 0 {
			return fmt.Sprintf("%dm %ds", minutes, seconds)
		}
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%ds", seconds)
}

// CalculateProgress computes progress metrics including duration and ETA
// currentInstanceIds should be the current list of instance IDs in the scale set (nil for terminal states)
func CalculateProgress(state *protocol.InstanceRefreshState, currentInstanceIds []string) protocol.InstanceRefreshProgress {
	// Use CompletedAt for terminal states to freeze the duration
	endTime := time.Now()
	if state.CompletedAt != nil {
		endTime = *state.CompletedAt
	}
	duration := endTime.Sub(state.StartedAt)

	progress := protocol.InstanceRefreshProgress{
		Phase:            state.Phase,
		InstancesInitial: len(state.OriginalInstanceIds),
		CurrentIteration: state.CurrentIteration,
		TotalIterations:  state.TotalIterations,
		StartedAt:        state.StartedAt.UTC().Truncate(time.Second).Format(time.RFC3339),
		Duration:         FormatDuration(duration),
		LastError:        state.LastError,
	}

	// Only include live instance counts when we have current instance data
	if currentInstanceIds != nil {
		currentCount := len(currentInstanceIds)
		replacedCount := countReplacedInstances(state.OriginalInstanceIds, currentInstanceIds)
		progress.InstancesCurrent = &currentCount
		progress.InstancesReplaced = &replacedCount
	}

	// Show average iteration duration if we have completed iterations
	if len(state.IterationDurations) > 0 {
		avgDuration := calculateAverageDuration(state.IterationDurations)
		progress.AvgIterationDuration = FormatDuration(avgDuration)
	}

	return progress
}

// InitializeState creates a new InstanceRefreshState for starting a refresh
func InitializeState(targetConfigHash string, originalSize, scaleUpInterval int, originalInstanceIds []string) *protocol.InstanceRefreshState {
	now := time.Now()
	totalIterations := CalculateTotalIterations(originalSize, scaleUpInterval)

	return &protocol.InstanceRefreshState{
		TargetConfigHash:    targetConfigHash,
		OriginalSize:        originalSize,
		ScaleUpInterval:     scaleUpInterval,
		Phase:               protocol.InstanceRefreshPhaseProvisioning,
		CurrentIteration:    1,
		TotalIterations:     totalIterations,
		OriginalInstanceIds: originalInstanceIds,
		StartedAt:           now,
		UpdatedAt:           now,
		IterationStartedAt:  &now,
		IterationDurations:  []time.Duration{},
	}
}

// IsInProgress returns true if an instance refresh is currently in progress
func IsInProgress(state *protocol.InstanceRefreshState) bool {
	if state == nil {
		return false
	}
	switch state.Phase {
	case protocol.InstanceRefreshPhaseProvisioning,
		protocol.InstanceRefreshPhaseWaitingWekaAfterScaleUp,
		protocol.InstanceRefreshPhaseWaitingWekaAfterScaleDown,
		protocol.InstanceRefreshPhaseTerminating:
		return true
	}
	return false
}

// CanTrigger returns nil if a new instance refresh can be triggered, error otherwise
func CanTrigger(state *protocol.InstanceRefreshState) error {
	if state == nil {
		return nil
	}
	if IsInProgress(state) {
		return fmt.Errorf("instance refresh already in progress (phase: %s, iteration: %d/%d)",
			state.Phase, state.CurrentIteration, state.TotalIterations)
	}
	return nil
}

// countReplacedInstances counts how many original instances are no longer in current list
func countReplacedInstances(originalIds, currentIds []string) int {
	currentSet := make(map[string]struct{}, len(currentIds))
	for _, id := range currentIds {
		currentSet[id] = struct{}{}
	}

	replaced := 0
	for _, id := range originalIds {
		if _, exists := currentSet[id]; !exists {
			replaced++
		}
	}
	return replaced
}

// calculateAverageDuration calculates the average of a slice of durations
func calculateAverageDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	return total / time.Duration(len(durations))
}

// MarkIterationComplete records the completion of an iteration and updates timing
func MarkIterationComplete(state *protocol.InstanceRefreshState) {
	now := time.Now()
	if state.IterationStartedAt != nil {
		iterationDuration := now.Sub(*state.IterationStartedAt)
		state.IterationDurations = append(state.IterationDurations, iterationDuration)
	}
	state.UpdatedAt = now
}

// StartNextIteration prepares the state for the next iteration
func StartNextIteration(state *protocol.InstanceRefreshState) {
	now := time.Now()
	state.CurrentIteration++
	state.Phase = protocol.InstanceRefreshPhaseProvisioning
	state.IterationStartedAt = &now
	state.UpdatedAt = now
}

// MarkCompleted marks the instance refresh as completed
func MarkCompleted(state *protocol.InstanceRefreshState) {
	now := time.Now()
	state.Phase = protocol.InstanceRefreshPhaseCompleted
	state.CompletedAt = &now
	state.UpdatedAt = now
}

// MarkFailed marks the instance refresh as failed with an error message
func MarkFailed(state *protocol.InstanceRefreshState, err error) {
	now := time.Now()
	state.Phase = protocol.InstanceRefreshPhaseFailed
	errStr := err.Error()
	state.LastError = &errStr
	state.CompletedAt = &now
	state.UpdatedAt = now
}

// MarkCancelled marks the instance refresh as cancelled by the user
func MarkCancelled(state *protocol.InstanceRefreshState) {
	now := time.Now()
	state.Phase = protocol.InstanceRefreshPhaseCancelled
	state.CompletedAt = &now
	state.UpdatedAt = now
}

// TransitionToTerminating transitions the state from scale up health check to terminating
func TransitionToTerminating(state *protocol.InstanceRefreshState) {
	state.Phase = protocol.InstanceRefreshPhaseTerminating
	state.UpdatedAt = time.Now()
}

// TransitionToWaitingWekaAfterScaleUp transitions to waiting for Weka to reach desired state after scale up
func TransitionToWaitingWekaAfterScaleUp(state *protocol.InstanceRefreshState) {
	state.Phase = protocol.InstanceRefreshPhaseWaitingWekaAfterScaleUp
	state.UpdatedAt = time.Now()
}

// TransitionToWaitingWekaAfterScaleDown transitions to waiting for Weka to reach desired state after scale down
func TransitionToWaitingWekaAfterScaleDown(state *protocol.InstanceRefreshState) {
	state.Phase = protocol.InstanceRefreshPhaseWaitingWekaAfterScaleDown
	state.UpdatedAt = time.Now()
}

// ShouldContinue returns true if more iterations are needed after completing the current one
func ShouldContinue(state *protocol.InstanceRefreshState) bool {
	return state.CurrentIteration < state.TotalIterations
}
