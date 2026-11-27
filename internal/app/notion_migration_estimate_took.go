package app

import (
	"encoding/json"
	"time"
)

// A run again reads all of Notion again, makes only what changed, asks every
// page, row and block for its comments again and checks it all against
// Notion, so the run before it says how long it takes better than the scan
// can: what its stages took on this server, with this connection and this
// Notion. A run keeps what its reading, making and comments took (readMs,
// makeMs, commentsMs, Leafen's off time left out), and a run again reckons
// each stage by it, as many times longer as it has more pages and rows to move:
// reading and the comments about as long as before, making from a pass over
// what didn't change up to all of it changed, and a fifth more at the longer
// end, as a first run's estimate allows (2026-11-21: a move that ended in 228
// minutes was told its run again takes "약 510분", its first promise, in
// minutes alone). A run that ended before it kept its stages' times is
// reckoned by its whole time, shared out as its estimate shared it; a stage
// the run before didn't take, or whose comments the person stopped, goes by
// the scan as a first run does.

// notionMigrationStageTook adds the time a run's stage took to the run's
// estimate, as the stage ends.
func notionMigrationStageTook(estimate *notionMigrationEstimate, stage string, ms int64) {
	if ms <= 0 {
		return
	}
	switch stage {
	case notionMigrationStageReading:
		estimate.ReadMs += ms
	case notionMigrationStageCreating:
		estimate.MakeMs += ms
	case notionMigrationStageComments:
		estimate.CommentsMs += ms
	}
}

// notionMigrationRunTook is how long a migration's last run took, in
// minutes: in all, by its stages, the rest (the check against Notion, the
// report and what goes between), and what its estimate counted to move.
type notionMigrationRunTook struct {
	whole, read, made, comments, rest float64
	counted                           notionMigrationEstimate
}

// notionMigrationLastRunTook is how long a migration's last run took, when it
// ended done and kept its time: its report's estimate, until a run again that
// just started keeps its own there (startNotionMigrationEstimate reckons the
// run again's by it).
func notionMigrationLastRunTook(stored storedNotionMigration) (*notionMigrationRunTook, error) {
	if stored.RunReport == "" || stored.RunState != notionMigrationRunDone && stored.RunState != notionMigrationRunRunning {
		return nil, nil
	}
	report := emptyNotionMigrationRunReport()
	if err := json.Unmarshal([]byte(stored.RunReport), &report); err != nil || report.Estimate == nil {
		return nil, err
	}
	estimate := *report.Estimate
	if stored.RunState == notionMigrationRunRunning && estimate.StartedAt >= stored.RunStarted {
		return nil, nil
	}
	minute := float64(time.Minute.Milliseconds())
	whole := float64(report.EndedAt-estimate.StartedAt-estimate.OffMs) / minute
	if estimate.StartedAt <= 0 || report.EndedAt <= estimate.StartedAt || whole <= 0 {
		return nil, nil
	}
	took := &notionMigrationRunTook{whole: whole, counted: estimate}
	// Every run reads and makes; one whose reading or making went untimed (a
	// server from before the times were kept took part of it) is shared out.
	if estimate.ReadMs > 0 && estimate.MakeMs > 0 {
		took.read, took.made, took.comments = float64(estimate.ReadMs)/minute, float64(estimate.MakeMs)/minute, float64(estimate.CommentsMs)/minute
	} else {
		share := whole / float64(estimate.ReadMinutes+estimate.MakeMinutes+estimate.CommentMinutes+3)
		took.read, took.made, took.comments = float64(estimate.ReadMinutes)*share, float64(estimate.MakeMinutes)*share, float64(estimate.CommentMinutes)*share
	}
	took.rest = max(0, whole-took.read-took.made-took.comments)
	if report.CommentsStopped {
		took.comments = 0
	}
	return took, nil
}

// weigh has a run again's stages take what the run before took, as many
// times longer as the run again has more pages and rows to move (estimate, as
// its plan counts them now): the reading's and the comments' range is what
// they took; making goes from the scan's shorter end, a pass over what didn't
// change, to what it took; the rest is what it took. Pages and rows are the
// yardstick: the run before counted the blocks it read
// (notion_migration_estimate_read_blocks.go), the plan the scan's reckoning
// of them, and the two differ with no change in Notion (2026-11-21: 9,501
// read against 10,002 reckoned put a run again's shorter end a quarter-hour
// later). A stage the run before didn't time keeps the scan's range.
func (took *notionMigrationRunTook) weigh(estimate notionMigrationEstimate, comments bool, read, made, asked, rest *[2]float64) {
	grown := 1.0
	if now, then := estimate.Pages+estimate.Rows, took.counted.Pages+took.counted.Rows; now > 0 && then > 0 {
		grown = float64(now) / float64(then)
	}
	if took.read > 0 {
		*read = [2]float64{took.read * grown, took.read * grown}
	}
	if took.made > 0 {
		made[1] = max(made[0], took.made*grown)
	}
	if comments && took.comments > 0 {
		*asked = [2]float64{took.comments * grown, took.comments * grown}
	}
	*rest = [2]float64{max(rest[0], took.rest), max(rest[1], took.rest)}
}
