package app

import (
	"context"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// ensureBootstrapVisibilityProjectionReadiness has the jobs touching a
// person's workspaces done before what reads them — theirs alone, a few,
// done here first; the rest of the server's go on behind
// (bootstrap_visibility_projection_runner.go), so many members joining at
// once keep nobody else waiting.
func ensureBootstrapVisibilityProjectionReadiness(
	event *core.RequestEvent,
	principalID string,
	failureCode string,
	failureMessage string,
	operation string,
) (bool, error) {
	settled, err := settleBootstrapVisibilityProjectionFor(event.Request.Context(), event.App, principalID)
	if err != nil {
		return false, apiErrorWithCause(
			event,
			http.StatusInternalServerError,
			failureCode,
			failureMessage,
			operation,
			"visibility_projection",
			errorCodeBootstrapVisibilityProjection,
			err,
		)
	}
	if !settled {
		event.Response.Header().Set("Retry-After", "1")
		return false, apiError(
			event,
			http.StatusServiceUnavailable,
			errorCodeBootstrapVisibilityProjectionPending,
			"Workspace permissions are syncing. Try again shortly.",
		)
	}
	return true, nil
}

// settleBootstrapVisibilityProjectionForWithin does the jobs touching a
// person until none is left, for up to wait: for what no request's own
// readiness settles — the first workspace an agent's confirmed name made by
// the setup request file (first_run_person_ask.go) and the list `leafen
// connect` reads (agent_connect_command.go). It says whether none is left.
func settleBootstrapVisibilityProjectionForWithin(
	ctx context.Context, app core.App, principalID string, wait time.Duration,
) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		settled, err := settleBootstrapVisibilityProjectionFor(ctx, app, principalID)
		if settled || err != nil {
			return settled, err
		}
	}
}
