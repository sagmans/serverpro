package cli

import (
	"errors"
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

const (
	deletePartialStatus                = "partial"
	deletePartialFailureStage          = "external_cleanup"
	deletePartialErrorPrefix           = "compute deletion completed, but tracked external cleanup failed; local state retained for retry"
	deletePartialStateReloadError      = "retained cleanup state reload failed"
	deletePartialRemainingPreviewError = "remaining external cleanup preview failed"
	deletePartialOutputError           = "partial delete result write failed"
	deletePartialNextAction            = "resolve the external cleanup error and rerun the same delete command"
)

type serverDeletePartialFailure struct {
	Row   serverOperationRow
	Cause error
}

func (e *serverDeletePartialFailure) Error() string {
	return fmt.Sprintf("%s: %v", deletePartialErrorPrefix, e.Cause)
}

func (e *serverDeletePartialFailure) Unwrap() error {
	return e.Cause
}

func newServerDeletePartialFailure(cleanup serverDeleteCleanup, retained state.State, cause error) *serverDeletePartialFailure {
	remaining, err := serverDeleteExternalCleanupPreview(retained)
	if err != nil {
		cause = errors.Join(cause, fmt.Errorf("%s: %w", deletePartialRemainingPreviewError, err))
	}
	return &serverDeletePartialFailure{
		Row: serverOperationRow{
			Status:                   deletePartialStatus,
			Action:                   deleteOperationAction,
			Namespace:                retained.Namespace,
			Server:                   retained.Server,
			Provider:                 retained.Compute.Provider,
			StatePath:                config.Expand(cleanup.StatePath),
			ComputeServer:            retained.Compute.ID,
			FailureStage:             deletePartialFailureStage,
			ComputeDeleted:           true,
			LocalStateRetained:       true,
			Retryable:                true,
			Error:                    cause.Error(),
			NextAction:               deletePartialNextAction,
			RemainingExternalCleanup: remaining,
		},
		Cause: cause,
	}
}
