package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A migration is one road that ends on its own (2026-11-16: in a real move
// the person had to step in eight times — the run ended at making the
// content, formats, templates and the check against Notion were steps after
// it that the agent could leave out, and the status said where the run was,
// never what was still to do). Its status says where the road is and what is
// still to do on it, in the road's order:
//
//	connect → scan → one confirmation → run (content, formats, templates,
//	comments) → the check against Notion → one report → finish
//
// Leafen runs what it can itself; the agent's part is the confirmation, the
// report and the finish, and between them nothing waits on the person.
//
// The status says facts, short, each time it is asked (2026-11-23: answers
// of 3–5 KB repeated every stage still to come and told the agent its turn
// and tone — "don't end your turn", "tell them first, as it is", "say no
// other time", "don't send them progress messages" — which an agent minding
// several things can't keep, and a careful one distrusts): the stage now,
// the one time left (notion_migration_time_said.go), where the person
// watches it, what Leafen tells them itself, what waiting can do, and why a
// run ended or stopped. The stages still to come are said once, as the run
// starts (its answer), and with detail (mcp_answer_rule.go: an answer says
// what is so and leads only to Leafen's own tools).

// notionMigrationFlowNext is what is left on a migration's road, and where it
// is; steps says the run's stages still to come.
func notionMigrationFlowNext(app core.App, ownerID string, migration notionMigration, job *notionImportJob, steps bool) string {
	switch {
	case migration.Status == notionMigrationStatusScanning:
		return "The scan isn't done: leafen_notion_migration_scan goes on with it, until its status is scanned."
	case migration.RunState == notionMigrationRunRunning:
		return notionMigrationRunningNext(app, ownerID, migration, job, steps)
	case migration.RunState == notionMigrationRunDone:
		return notionMigrationDoneNext(app, ownerID, migration)
	case migration.RunState == notionMigrationRunFailed:
		started, stopped := int64(0), migration.RunEndedAt
		if stored, err := loadStoredJob(app, migration.ImportJobID, ownerID); err == nil {
			started = stored.Created
		}
		return notionMigrationStoppedNext(migration, job, started, stopped, notionMigrationRunAgainSentence(app, ownerID, migration))
	}
	return "Not running yet: leafen_notion_migration_plan's confirm is the one confirmation (where the move goes, the Notion people no member stands for, what moves), " +
		"each with a recommendation; the answer goes into the plan, and leafen_notion_migration_run runs it on the person's yes."
}

// notionMigrationRunningNext says where a run is: its stage and how far, the
// time left with why it is far from the time said, and — once, or with
// detail — what it still does on its own.
func notionMigrationRunningNext(app core.App, ownerID string, migration notionMigration, job *notionImportJob, steps bool) string {
	next, big := "Running.", notionMigrationStageReading
	if stored, err := loadStoredJob(app, migration.ImportJobID, ownerID); err == nil {
		payload := notionImportJobPayload{}
		if json.Unmarshal([]byte(stored.PayloadJSON), &payload) == nil {
			stage := notionImportStage(app, stored, payload)
			big = notionMigrationStageOfStep(stage)
			next = "Now: " + notionImportStageName(accountLanguageEn, stage) + "."
			if stage == "comments" && payload.CommentsLast {
				next += " The content is all in Leafen and in its place, for the person to look over; only the comments, the check against Notion and the report are left."
			}
			if after := notionMigrationStepsAfter(payload.notionMigrationRunOrder, stage, payload.MigrationRerunSince > 0); steps && len(after) > 0 {
				next += " Then, on its own: " + strings.Join(after, ", ") + "."
			}
		}
	}
	if job != nil {
		if where := notionImportProgressText(*job, time.Now()); where != "" {
			next += " " + where
		}
	}
	language, _ := serverTextLanguage(app, ownerID, nil)
	// The stage's line with its address leads (notion_migration_stage_lines.go).
	lead := ""
	if facts := notionMigrationStageLineNext(language, big); facts != "" {
		lead = facts + ". "
	}
	return lead + next + notionMigrationEstimateNextOf(migration.RunReport.Estimate, job, language) + notionMigrationRunningFacts
}

// notionMigrationRunningFacts is what holds through a run, beside its stage's
// line with the live view's address (notion_migration_stage_lines.go;
// 2026-11-21: an agent ran the move, said the inbox would tell them, and
// gave the link only when asked where to see it; 2026-11-19: the window said
// the agent was on it while it idled, so Leafen's import window says itself
// that Leafen goes on alone and the person tells their agent the end,
// migration.agentRunning).
const notionMigrationRunningFacts = " Between the stages' lines nothing else is left: their inbox tells them as each stage ends and when the move is done or stops, " +
	"and nothing waits on them. pages_link, once there, is the moved content to look over meanwhile. wait_seconds (at most 45) answers as soon as the run ends. " +
	"The move goes on in Leafen without this conversation: its window tells the person they can close it, and that once it is done they tell their agent, which then checks the result and reports."

// notionMigrationEstimateNext is a running migration's time with the line
// why in English (notionMigrationEstimateNextOf).
func notionMigrationEstimateNext(estimate *notionMigrationEstimate) string {
	return notionMigrationEstimateNextOf(estimate, nil, accountLanguageEn)
}

// notionMigrationEstimateNextOf is a running migration's time: the one number
// Leafen's screen and the person's inbox show, and, when it is far from the
// time said at the start, why — the line the screen and the inbox show beside
// it (notion_migration_time_said.go), in the person's language. Leafen
// weighs the time itself; the counts don't make another.
func notionMigrationEstimateNextOf(estimate *notionMigrationEstimate, job *notionImportJob, language string) string {
	minutes := 0
	if job != nil && job.Progress != nil {
		minutes = job.Progress.MinutesLeft
	}
	now := time.Now()
	if estimate != nil && estimate.EndsAt > 0 {
		// The time left at the run's pace (notion_migration_time_left.go).
		minutes = notionMigrationMinutesLeft(estimate, now)
	}
	left := ""
	switch {
	case notionMigrationAlmostDone(minutes):
		left = " Almost done."
	case minutes > 0:
		left = " Still about " + notionMigrationDuration(accountLanguageEn, minutes) + " left."
	}
	if estimate == nil || estimate.EndsAt == 0 {
		return left
	}
	if say := notionMigrationLeftSay(language, *estimate, now); say != "" {
		way := "later"
		if notionMigrationFaster(*estimate, now) {
			way = "sooner"
		}
		return left + fmt.Sprintf(" It ends %s than said at the start. Left to do, unless this conversation said it already: that line to the person with the time left, "+
			"as the window and the inbox say it (runReport.estimate.say): %q", way, say)
	}
	return left + " That is within the time said at the start (runReport.estimate)."
}

// notionMigrationStepsAfter names the steps a run still takes after one.
func notionMigrationStepsAfter(order notionMigrationRunOrder, stage string, again bool) []string {
	after, passed := []string{}, false
	for _, step := range notionMigrationRunStepsOf(order) {
		switch {
		case step == stage:
			passed = true
		case passed && (step != "rerun" || again):
			after = append(after, notionImportStageName(accountLanguageEn, step))
		}
	}
	return after
}

// notionMigrationAfterNext is what follows a move from Notion, as the
// report's last line and Leafen's import window say it (migration.afterDone;
// 2026-11-22: after a move both Notion and Leafen got new pages, nobody
// having said that a run again brings Notion's changes or that Notion is
// better left read-only).
const notionMigrationAfterNext = " What follows it, as Leafen's import window says: if the person keeps writing in Notion, running this same migration again — " +
	"Leafen's import screen, Completed › Import changes again, or an agent — brings only what is new or changed; once everything has moved, " +
	"Notion is best made read-only or left. What they write in Leafen doesn't go back to Notion."

// notionMigrationDoneAgain leads a done move's next with the run again for a
// person who asked to move from Notion after its run ended
// (notionMigrationAskedAgain); the report follows that run. A run this
// conversation made goes on to the report.
func notionMigrationDoneAgain(app core.App, ownerID string, migration notionMigration) string {
	stored, err := loadNotionMigration(app, ownerID, migration.WorkspaceID, migration.ID)
	if err != nil {
		return ""
	}
	return notionMigrationAskedAgain(stored)
}

// notionMigrationDoneNext is what follows a run that ended: one report to
// the person, then the finish, which gives the structure rights back; or,
// asked to move again after it ended, the run again (notionMigrationDoneAgain).
func notionMigrationDoneNext(app core.App, ownerID string, migration notionMigration) string {
	again := notionMigrationDoneAgain(app, ownerID, migration)
	// The run checks itself against Notion before it is done; one that ran
	// before the check was a step is checked first.
	verify := migration.RunReport.Verify
	if verify == nil {
		return "Done, but not yet checked against Notion. " + again + "leafen_notion_migration_verify checks it."
	}
	if next := notionMigrationVerifyNext(*verify); next != notionMigrationVerifiedNext {
		return "Done and checked against Notion (runReport.verify). " + again + next
	}
	next := "Done and checked against Notion (runReport.verify). " + again + "Left: one report to the person, with the moved content (" + notionMigrationLinkMark +
		") and the \"Migration report\" page (report_link), grouped as that page is — " +
		"first what didn't move at all (files not copied, as file_groups says them; comments Notion didn't give; a people mapping that failed; values sent aside), " +
		"then what didn't come whole, as not_whole says it — the three counts Leafen's finish screen and the report show, each item by its say and count — " +
		"then what still differs from Notion (check_groups) and what stays at Leafen's defaults (formats_left_groups, forms), then what moved " +
		"(a run again: made new, changed and left as it was, runReport.tally), what came differently, and what, if anything, is left for them. " +
		"The report page says each kind in one line by its say and count, folding which pages, views or items under it." +
		notionMigrationAfterNext
	return next
}
