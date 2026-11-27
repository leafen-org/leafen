package app

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Waiting on a long job (2026-11-22: an agent following a backup that ran
// for an hour asked again every minute, each ask growing its conversation):
// a read of a long job's state — leafen_call's GET of a scheduled backup and
// a restore (/admin/backups/schedule), a backup file, an export, an import,
// a job the server carries on with by itself, and a Notion migration's
// status — takes wait_seconds and waits up to that many, at most 45 as the
// Notion connection's wait does, for the job to end, answering as soon as
// it has; remote access (/admin/tunnel) for its next stage
// (server_tunnel_stage.go). Any other read answers at once, wait or not.
//
// A call that sets a long job going or goes on with one answers within
// longJobCallMost, the job going on on the server, at once when it ends:
// inside that wait, and well under the minute a reverse proxy in front of
// Leafen gives a request (2026-11-22: a Notion migration's scan held 1m01s
// and a Synology proxy cut it off with a 504; notion_migration_scan_on.go).
const (
	longJobWaitMost = 45
	longJobWaitLook = 2 * time.Second
	longJobCallMost = 25 * time.Second
)

// waitWhileJobGoes reads a job's state, and again every two seconds while
// goes says it still goes, until it ends or the wait does.
func waitWhileJobGoes[T any](ctx context.Context, waitSeconds int, read func() (T, error), goes func(T) bool) (T, error) {
	until := time.Now().Add(time.Duration(min(max(waitSeconds, 0), longJobWaitMost)) * time.Second)
	for {
		answer, err := read()
		left := time.Until(until)
		if err != nil || left <= 0 || !goes(answer) {
			return answer, err
		}
		timer := time.NewTimer(min(longJobWaitLook, left))
		select {
		case <-ctx.Done():
			timer.Stop()
			return answer, nil
		case <-timer.C:
		}
	}
}

// leafenCallLongJobReads are the reads of a long job's state, by path: the
// backups, exports and imports, and the jobs the server carries on with by
// itself once asked (a block move, a page copy, a page's move to another
// workspace, a wiki or database change,
// an owner transfer, a file cleanup, an organization's export). Left out:
// jobs that go on only as their caller or a person continues them — a
// comment deletion, an account's cleanup waiting for its assignments.
var leafenCallLongJobReads = []*regexp.Regexp{
	regexp.MustCompile(`^/admin/backups/schedule$`),
	regexp.MustCompile(`^/admin/backups/[^/]+$`),
	regexp.MustCompile(`^/admin/product-exports/[^/]+$`),
	regexp.MustCompile(`^/workspaces/[^/]+/(exports|imports|owner-transfer|file-cleanup|notion-import/jobs)/[^/]+$`),
	regexp.MustCompile(`^/organizations/[^/]+/(audit|discovery)-exports/[^/]+$`),
	regexp.MustCompile(`^/(jobs|block-moves|page-duplications|page-workspace-moves|wiki-mutations)/[^/]+$`),
	regexp.MustCompile(`^/pages/[^/]+/database-mutations/[^/]+$`),
	regexp.MustCompile(`^` + serverTunnelPath + `$`),
}

// Reads beside them that answer at once: settings and what a place holds.
var leafenCallNotLongJobRead = regexp.MustCompile(`^/admin/backups/(replica|protection|replication|standby)$|/(policy|bundle-sources)$`)

// leafenCallWaitsFor is whether a call reads a long job's state.
func leafenCallWaitsFor(method, path string) bool {
	if !strings.EqualFold(method, http.MethodGet) || leafenCallNotLongJobRead.MatchString(path) {
		return false
	}
	for _, read := range leafenCallLongJobReads {
		if read.MatchString(path) {
			return true
		}
	}
	return false
}

// The states of a job the server is still working on.
var longJobGoingStates = map[string]bool{"queued": true, "pending": true, "preparing": true, "running": true, "cancelling": true}

// leafenCallJobGoing is whether an answer says its job still goes; for the
// backup schedule, a backup or restore under way, or one due now that
// hasn't begun.
func leafenCallJobGoing(path string, result nativeOperationResult) bool {
	if result.Status != http.StatusOK {
		return false
	}
	body, _ := result.Body.(map[string]any)
	if path == "/admin/backups/schedule" {
		schedule, _ := body["schedule"].(map[string]any)
		if schedule == nil {
			return false
		}
		if schedule["running"] == true || schedule["progress"] != nil {
			return true
		}
		// Asked for, or due, and not begun yet: the runner starts it in a moment.
		next, _ := schedule["nextRunAt"].(string)
		due, err := time.Parse(time.RFC3339, next)
		return schedule["enabled"] == true && err == nil && !due.After(time.Now()) &&
			schedule["lastErrorCode"] != errorCodeBackupProtectionRequired
	}
	status, _ := body["status"].(string)
	if job, ok := body["job"].(map[string]any); ok {
		status, _ = job["status"].(string)
	}
	return longJobGoingStates[status]
}

// callNativeOperationWaiting runs leafen_call's call, waiting on a long
// job's state when it reads one.
func callNativeOperationWaiting(ctx context.Context, waitSeconds int, call func() (nativeOperationResult, error),
	method, path string) (nativeOperationResult, error) {
	if !leafenCallWaitsFor(method, path) {
		return call()
	}
	if path == serverTunnelPath {
		return waitWhileJobGoes(ctx, waitSeconds, call, tunnelStageChanges())
	}
	return waitWhileJobGoes(ctx, waitSeconds, call, func(result nativeOperationResult) bool { return leafenCallJobGoing(path, result) })
}

// notionMigrationStageGoes is whether a migration's run still goes by
// itself (a scan goes on only as its tool is called again).
func notionMigrationStageGoes(stage string) bool {
	switch stage {
	case notionMigrationStageReading, notionMigrationStageCreating, notionMigrationStageComments, notionMigrationStageVerifying:
		return true
	}
	return false
}

// leafenCallWaitNoteFor is an operation's note in the operations list, with
// the wait beside each read that waits.
func leafenCallWaitNoteFor(id, note string) string {
	method, path, _ := strings.Cut(id, " ")
	if !leafenCallWaitsFor(method, path) {
		return note
	}
	if path == serverTunnelPath {
		return strings.TrimSpace(note + " With leafen_call's wait_seconds (at most 45) it waits for remote access's next stage " +
			"(its stage field) and answers as soon as it comes: follow it so rather than asking again and again.")
	}
	return strings.TrimSpace(note + " With leafen_call's wait_seconds (at most 45) it waits for the job to end and answers as soon as it has: " +
		"follow the job so rather than asking again and again.")
}
