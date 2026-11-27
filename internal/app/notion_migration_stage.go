package app

import "github.com/pocketbase/pocketbase/core"

// A migration's run as the person and their agent follow it: one stage,
// in this order — reading Notion, creating it in Leafen, comments, checking
// it against Notion — then done, or stopped (2026-11-16: a real run showed
// the migration's status "scanned", its run "running", the import "running"
// and its phase "scope" then "apply" at once, and the person couldn't tell
// which to look at). Each of the run's steps (notionImportStage) is in one
// stage.

const (
	notionMigrationStageReading   = "reading"
	notionMigrationStageCreating  = "creating"
	notionMigrationStageComments  = "comments"
	notionMigrationStageVerifying = "verifying"
	notionMigrationStageDone      = "done"
	notionMigrationStageStopped   = "stopped"
)

// notionMigrationRunOrder is the order a run's job takes its steps in.
type notionMigrationRunOrder struct {
	// CommentsLast: the comments come once everything else is made and in
	// its place, so the content is done before them, and the check against
	// Notion follows them (runs started since 2026-11-16; a run started
	// before goes on in the order it began with).
	CommentsLast bool `json:"commentsLast,omitzero"`
}

// notionMigrationRunStepsOf are a run's steps in the order it takes them
// (notionImportStage); a run again also places and removes what Notion moved
// or deleted. The steps a run doesn't take are passed over.
func notionMigrationRunStepsOf(order notionMigrationRunOrder) []string {
	if order.CommentsLast {
		return []string{"read", "make", "link", "publish", "plan", "moves", "people", "rerun", "formats", "comments", "ledger", "verify", "report", "finish"}
	}
	return []string{"read", "make", "link", "publish", "plan", "comments", "moves", "people", "rerun", "formats", "ledger", "verify", "report", "finish"}
}

// notionMigrationStageOfStep is the stage a run's step is in. Recording what
// moved comes after the comments and right before the check, in either order
// (2026-11-19: as a step of making it put a run's stage back from comments
// to creating, where the run's estimate stayed once it was done).
func notionMigrationStageOfStep(step string) string {
	switch step {
	case "read":
		return notionMigrationStageReading
	case "comments":
		return notionMigrationStageComments
	case "ledger", "verify", "report", "finish":
		return notionMigrationStageVerifying
	}
	return notionMigrationStageCreating
}

// notionImportJobStage is an import's one stage, as its screen and a
// migration's agent follow it; while the blocks' comments come, the job
// counts the blocks asked of all (commentBlocksDone, commentBlocksTotal).
func notionImportJobStage(app core.App, job storedJob, payload notionImportJobPayload) string {
	stage, _ := notionImportJobStageStep(app, job, payload)
	return stage
}

// notionImportJobStageStep is an import's stage and, for a migration's run
// that goes on, the step it is at (notionImportStage): its window names the
// step while the stage's own count stands — after making, the second pass for
// what the plan merges ("merge"), the links, showing the pages and the plan's
// changes (2026-11-20: "마무리하는 중…" stood with no count through them).
func notionImportJobStageStep(app core.App, job storedJob, payload notionImportJobPayload) (string, string) {
	switch job.Status {
	case durableJobStatusCompleted:
		return notionMigrationStageDone, ""
	case durableJobStatusFailed, durableJobStatusCanceled:
		return notionMigrationStageStopped, ""
	}
	if payload.MigrationID == "" {
		if payload.Phase == notionImportPhaseScope {
			return notionMigrationStageReading, ""
		}
		return notionMigrationStageCreating, ""
	}
	step := notionImportStage(app, job, payload)
	stage := notionMigrationStageOfStep(step)
	if step == "make" && payload.ApplyingMerges {
		step = "merge"
	}
	return stage, step
}
