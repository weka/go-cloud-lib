package instance_refresh

import (
	"fmt"
	"time"

	"github.com/weka/go-cloud-lib/protocol"
)

// CalculateTargetContainers calculates the total expected backend containers.
// This includes containers from backend VMs plus any protocol gateway containers (NFS/SMB/S3).
func CalculateTargetContainers(vmCount, containersPerVm, protocolGatewayContainers int) int {
	return vmCount*containersPerVm + protocolGatewayContainers
}

// CheckClusterHealthy checks if the Weka cluster is healthy for a given expected size.
// This is used both after scale up and scale down - only the expectedSize differs.
// protocolGatewayContainers is the number of additional backend containers from NFS/SMB/S3 gateways.
// Returns nil if healthy, error describing what's not ready otherwise.
func CheckClusterHealthy(status protocol.WekaStatus, expectedSize, containersPerVm, drivesPerVm, protocolGatewayContainers int) error {
	expectedContainers := CalculateTargetContainers(expectedSize, containersPerVm, protocolGatewayContainers)
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
		replacedCount := CountReplacedInstances(state.OriginalInstanceIds, currentInstanceIds)
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
func InitializeState(originalSize, scaleUpInterval int, originalInstanceIds []string) *protocol.InstanceRefreshState {
	now := time.Now()
	totalIterations := CalculateTotalIterations(originalSize, scaleUpInterval)

	return &protocol.InstanceRefreshState{
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

// CountReplacedInstances counts how many original instances are no longer in current list
func CountReplacedInstances(originalIds, currentIds []string) int {
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

// GetExpectedReplaced returns how many instances should be replaced by the current iteration
func GetExpectedReplaced(state *protocol.InstanceRefreshState) int {
	expectedReplaced := state.CurrentIteration * state.ScaleUpInterval
	if expectedReplaced > len(state.OriginalInstanceIds) {
		expectedReplaced = len(state.OriginalInstanceIds)
	}
	return expectedReplaced
}

// completeIterationAndProceed handles the common logic when an iteration is complete:
// marks the iteration complete, then either starts the next iteration or marks the refresh as completed.
// Returns the new desired size if scaling up for next iteration, nil otherwise.
func completeIterationAndProceed(state *protocol.InstanceRefreshState) *int {
	MarkIterationComplete(state)

	if ShouldContinue(state) {
		StartNextIteration(state)
		scaledUpSize := GetScaledUpSize(state.OriginalSize, state.ScaleUpInterval,
			state.CurrentIteration, state.TotalIterations)
		return &scaledUpSize
	}

	MarkCompleted(state)
	return nil
}

// GetExpectedDesiredSize returns the expected DesiredSize for the current phase.
// This is used to ensure DesiredSize is always correct, even after partial write failures.
func GetExpectedDesiredSize(state *protocol.InstanceRefreshState) int {
	switch state.Phase {
	case protocol.InstanceRefreshPhaseProvisioning,
		protocol.InstanceRefreshPhaseWaitingWekaAfterScaleUp:
		return GetScaledUpSize(state.OriginalSize, state.ScaleUpInterval,
			state.CurrentIteration, state.TotalIterations)
	case protocol.InstanceRefreshPhaseWaitingWekaAfterScaleDown,
		protocol.InstanceRefreshPhaseTerminating:
		return state.OriginalSize
	default:
		return state.OriginalSize
	}
}

// AdvanceStateMachine advances the instance refresh state machine.
// This is a pure function - it modifies state in place and returns what actions the caller should take.
// Returns:
//   - stateChanged: whether the state was modified
//   - newDesiredSize: the cluster size to set (nil if no change needed)
//   - err: error if an unexpected condition was detected
//
// Note: newDesiredSize is always returned for active phases to ensure idempotency.
// If a previous write failed, the next run will correct the DesiredSize.
// The caller should only write if the value differs from current DesiredSize.
// protocolGatewayContainers is the number of additional backend containers from NFS/SMB/S3 gateways.
func AdvanceStateMachine(
	state *protocol.InstanceRefreshState,
	wekaStatus protocol.WekaStatus,
	currentInstanceIds []string,
	drivesPerVm int,
	containersPerVm int,
	protocolGatewayContainers int,
) (stateChanged bool, newDesiredSize *int, err error) {

	switch state.Phase {
	case protocol.InstanceRefreshPhaseProvisioning,
		protocol.InstanceRefreshPhaseWaitingWekaAfterScaleUp:

		// Calculate expected scaled up size
		scaledUpSize := GetScaledUpSize(state.OriginalSize, state.ScaleUpInterval,
			state.CurrentIteration, state.TotalIterations)

		// First, verify the instance count has reached the scaled up size
		if len(currentInstanceIds) < scaledUpSize {
			if state.Phase != protocol.InstanceRefreshPhaseProvisioning {
				state.Phase = protocol.InstanceRefreshPhaseProvisioning
				state.UpdatedAt = time.Now()
				return true, &scaledUpSize, nil
			}
			// Return scaledUpSize to ensure DesiredSize is correct (idempotent)
			return false, &scaledUpSize, nil
		}

		// Instances are up, now wait for Weka to reach desired state
		if state.Phase != protocol.InstanceRefreshPhaseWaitingWekaAfterScaleUp {
			TransitionToWaitingWekaAfterScaleUp(state)
			return true, &scaledUpSize, nil
		}

		// Check if cluster is healthy at scaled up size
		if err := CheckClusterHealthy(wekaStatus, scaledUpSize, containersPerVm, drivesPerVm, protocolGatewayContainers); err != nil {
			// Return scaledUpSize to ensure DesiredSize is correct (idempotent)
			return false, &scaledUpSize, nil
		}

		// Scale up complete, transition to waiting for Weka at original size
		TransitionToWaitingWekaAfterScaleDown(state)

		// Return the original size to trigger scale down
		return true, &state.OriginalSize, nil

	case protocol.InstanceRefreshPhaseWaitingWekaAfterScaleDown:

		// Check if cluster is healthy at original size
		if err := CheckClusterHealthy(wekaStatus, state.OriginalSize, containersPerVm, drivesPerVm, protocolGatewayContainers); err != nil {
			// Return OriginalSize to ensure DesiredSize is correct (idempotent)
			return false, &state.OriginalSize, nil
		}

		// Weka is ready, now verify instances are terminated
		expectedReplaced := GetExpectedReplaced(state)
		actualReplaced := CountReplacedInstances(state.OriginalInstanceIds, currentInstanceIds)

		if actualReplaced > expectedReplaced {
			return true, &state.OriginalSize, fmt.Errorf("unexpected state: more instances replaced (%d) than expected (%d)",
				actualReplaced, expectedReplaced)
		}

		// Check if termination is already complete (often happens by the time scale down is done)
		// This avoids an extra iteration through the Terminating phase
		if actualReplaced == expectedReplaced && len(currentInstanceIds) == state.OriginalSize {
			newDesiredSize := completeIterationAndProceed(state)
			return true, newDesiredSize, nil
		}

		// Termination not yet complete, transition to Terminating phase
		TransitionToTerminating(state)
		return true, &state.OriginalSize, nil

	case protocol.InstanceRefreshPhaseTerminating:

		expectedReplaced := GetExpectedReplaced(state)
		actualReplaced := CountReplacedInstances(state.OriginalInstanceIds, currentInstanceIds)

		if actualReplaced > expectedReplaced {
			return true, &state.OriginalSize, fmt.Errorf("unexpected state: more instances replaced (%d) than expected (%d)",
				actualReplaced, expectedReplaced)
		}

		// Verify the expected number of old instances have been replaced and instance count is correct
		if actualReplaced < expectedReplaced || len(currentInstanceIds) != state.OriginalSize {
			// Return OriginalSize to ensure DesiredSize is correct (idempotent)
			return false, &state.OriginalSize, nil
		}

		// Scale down complete
		newDesiredSize := completeIterationAndProceed(state)
		return true, newDesiredSize, nil
	}

	return false, nil, nil
}
