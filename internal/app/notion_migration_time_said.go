package app

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A running migration's time left is one number wherever it shows — the
// person's inbox, Leafen's import screen and its Completed row, the agent's
// status answer with its runReport.estimate (2026-11-23: one move said about
// 16 hours in the inbox, 364 minutes and "about 6 hours 4 minutes" in its
// status, and an end about 13 hours away in the same answer's estimate) —
// the run's one reckoning (notionMigrationMinutesLeft), and the line saying
// why it is far from the time said, where it is (notionMigrationLeftSay):
//
//   - the run's pace ends it later than said: the later end's reason
//     (notion_migration_estimate_run.go), which moves the time said;
//   - its pace ends it in less than half the time the end said still leaves:
//     which stage went faster than the longer end it was reckoned at. The
//     end said stands (an earlier end is never promised,
//     notion_migration_estimate_early.go), so this holds as long as the pace
//     does, and goes when the pace comes back near it.
//
// The notices a run leaves in the inbox carry its time left in their head;
// every look at the run keeps those heads to the one number, quietly — no
// push, the notice keeps its time and whether it was read — and the run's
// one notice of its time (the later one) says the line beside it. As the run
// ends, the heads lose their time.

// notionMigrationFasterLeast is how much earlier than the end said the pace
// must end a run before the time left says why: less is a few minutes either
// way near the end.
const notionMigrationFasterLeast = 10 * time.Minute

// notionMigrationFaster reports whether a running migration's pace ends it in
// less than half the time the end said still leaves.
func notionMigrationFaster(estimate notionMigrationEstimate, now time.Time) bool {
	end, paced := notionMigrationShownEnd(estimate)
	if !paced || estimate.EndsAt == 0 {
		return false
	}
	at := now.UnixMilli()
	left, said := end-at, estimate.EndsAt-at
	return said > 0 && left*2 < said && said-left >= notionMigrationFasterLeast.Milliseconds()
}

// notionMigrationFasterStage is the stage that went faster, and whether it
// has ended: the stage a later end was reckoned for, once it ended — it went
// faster than that reckoning, which put the end said where it is (2026-11-23:
// a reading said slower at its start went fast later, and the time left fell
// from 30 minutes to 8 while making had only begun); else the first that
// ended in under half the longer end it was reckoned at; else the one the
// run is in.
func notionMigrationFasterStage(estimate notionMigrationEstimate) (string, bool) {
	order := []string{notionMigrationStageReading, notionMigrationStageCreating, notionMigrationStageComments}
	ended := func(stage string) bool {
		now, of := slices.Index(order, estimate.Stage), slices.Index(order, stage)
		return of >= 0 && (now < 0 || of < now)
	}
	if stage := notionMigrationLaterStage(estimate.Reason); estimate.ChangedAt > 0 && ended(stage) {
		return stage, true
	}
	took := map[string][2]int64{
		notionMigrationStageReading:  {estimate.ReadMs, int64(estimate.ReadMinutes)},
		notionMigrationStageCreating: {estimate.MakeMs, int64(estimate.MakeMinutes)},
		notionMigrationStageComments: {estimate.CommentsMs, int64(estimate.CommentMinutes)},
	}
	for _, stage := range order {
		if ms, minutes := took[stage][0], took[stage][1]; ended(stage) && ms > 0 && minutes > 0 && ms*2 < minutes*time.Minute.Milliseconds() {
			return stage, true
		}
	}
	return estimate.Stage, false
}

// notionMigrationLaterStage is the stage a later end's reason was reckoned
// for (notion_migration_estimate_run.go).
func notionMigrationLaterStage(reason string) string {
	switch reason {
	case "slower_reading", "more_found":
		return notionMigrationStageReading
	case "slower_making":
		return notionMigrationStageCreating
	case "slower_comments":
		return notionMigrationStageComments
	}
	return ""
}

// notionMigrationLeftReason is why a running migration's time left is far
// from the time said, as its estimate's reason: faster_reading,
// faster_creating, faster_comments or faster; the later end's reason; ""
// for none.
func notionMigrationLeftReason(estimate notionMigrationEstimate, now time.Time) string {
	if notionMigrationFaster(estimate, now) {
		switch stage, _ := notionMigrationFasterStage(estimate); stage {
		case notionMigrationStageReading, notionMigrationStageCreating, notionMigrationStageComments:
			return "faster_" + stage
		}
		return "faster"
	}
	if estimate.ChangedAt > 0 {
		return estimate.Reason
	}
	return ""
}

// notionMigrationLeftSay is the line beside a running migration's time left,
// in the person's language: why it is far from the time said; "" when it
// isn't.
func notionMigrationLeftSay(language string, estimate notionMigrationEstimate, now time.Time) string {
	switch {
	case notionMigrationFaster(estimate, now):
		return notionMigrationFasterSay(language, estimate, false)
	case estimate.ChangedAt > 0:
		return estimate.Say
	}
	return ""
}

// notionMigrationFasterSay is the line for a run whose pace ends it well
// before the end said: which stage went faster; done, for one that ended so.
func notionMigrationFasterSay(language string, estimate notionMigrationEstimate, done bool) string {
	stage, ended := notionMigrationFasterStage(estimate)
	if language == accountLanguageKo {
		why := map[string][2]string{
			notionMigrationStageReading:  {"Notion을 예상보다 빨리 읽고 있습니다", "Notion을 예상보다 빨리 읽었습니다"},
			notionMigrationStageCreating: {"리펜에 예상보다 빨리 만들고 있습니다", "리펜에 예상보다 빨리 만들었습니다"},
			notionMigrationStageComments: {"댓글을 예상보다 빨리 묻고 있습니다", "댓글을 예상보다 빨리 물었습니다"},
		}
		said := "지금까지 예상보다 빨리 진행됐습니다"
		if words, ok := why[stage]; ok {
			said = words[notionMigrationEndedIndex(ended)]
		}
		if done {
			return fmt.Sprintf("말씀드린 것보다 일찍 끝났습니다 — %s.", said)
		}
		return fmt.Sprintf("말씀드린 것보다 일찍 끝날 것 같습니다 — %s. 남은 시간은 지금 속도대로 셉니다.", said)
	}
	why := map[string][2]string{
		notionMigrationStageReading:  {"Notion is read faster than reckoned", "Notion was read faster than reckoned"},
		notionMigrationStageCreating: {"making it in Leafen goes faster than reckoned", "making it in Leafen went faster than reckoned"},
		notionMigrationStageComments: {"asking for the comments goes faster than reckoned", "asking for the comments went faster than reckoned"},
	}
	said := "it has gone faster than reckoned so far"
	if words, ok := why[stage]; ok {
		said = words[notionMigrationEndedIndex(ended)]
	}
	if done {
		return fmt.Sprintf("It ended sooner than said — %s.", said)
	}
	return fmt.Sprintf("It looks to end sooner than said — %s. The time left goes by this pace.", said)
}

func notionMigrationEndedIndex(ended bool) int {
	if ended {
		return 1
	}
	return 0
}

// notionMigrationTimeNoticeTitle is the head of a run's one notice of its
// time: later, or sooner, than said; done, as it ended.
func notionMigrationTimeNoticeTitle(language string, faster, done bool) string {
	words := [4]string{"The Notion move takes longer than said", "The Notion move looks to end sooner than said",
		"The Notion move took longer than said", "The Notion move ended sooner than said"}
	if language == accountLanguageKo {
		words = [4]string{"Notion 옮기기가 말씀드린 것보다 오래 걸립니다", "Notion 옮기기가 말씀드린 것보다 일찍 끝날 것 같습니다",
			"Notion 옮기기가 말씀드린 것보다 오래 걸렸습니다", "Notion 옮기기가 말씀드린 것보다 일찍 끝났습니다"}
	}
	return words[notionMigrationEndedIndex(faster)+2*notionMigrationEndedIndex(done)]
}

// notionMigrationNoticeHead is a notice's head with a run's time left, or
// without one (left 0).
func notionMigrationNoticeHead(language, title string, left int) string {
	if words := notionMigrationLeftWords(language, left); words != "" {
		return title + " · " + words
	}
	return title
}

// notionMigrationStageNoticeKinds are the stages whose ended notices carry
// a run's time left.
var notionMigrationStageNoticeKinds = []string{notionMigrationStageReading, notionMigrationStageCreating, notionMigrationStageComments}

// keepNotionMigrationNoticeHeads keeps the run's notices to its time left now
// and its time notice to whether it ends sooner than said, with the line
// why; as the run ends (left 0), the heads without a time, and a done run's
// time notice as it ended. Quietly, writing only what changed.
func keepNotionMigrationNoticeHeads(
	app core.App, job storedJob, migrationID string, estimate notionMigrationEstimate, left int, faster, done bool, now time.Time,
) error {
	language, err := serverTextLanguage(app, job.Owner, nil)
	if err != nil {
		return err
	}
	changed := int64(0)
	set := func(kind, title, preview string) error {
		query := `UPDATE {{leafen_notifications}} SET [[title]]={:title}, [[updated_at]]={:now}
			WHERE [[id]]={:id} AND [[title]]<>{:title}`
		params := dbx.Params{"id": appNoticeID("notion-migration-"+kind, migrationID, job.ID), "title": title, "now": now.UnixMilli()}
		if preview != "" {
			query = `UPDATE {{leafen_notifications}} SET [[title]]={:title}, [[preview]]={:preview}, [[updated_at]]={:now}
				WHERE [[id]]={:id} AND ([[title]]<>{:title} OR [[preview]]<>{:preview})`
			params["preview"] = preview
		}
		result, err := app.DB().NewQuery(query).Bind(params).Execute()
		if err != nil {
			return err
		}
		if rows, err := result.RowsAffected(); err == nil {
			changed += rows
		}
		return nil
	}
	for _, ended := range notionMigrationStageNoticeKinds {
		title, _ := notionMigrationStageNoticeWords(language, ended, "")
		if err := set("stage-"+ended, notionMigrationNoticeHead(language, title, left), ""); err != nil {
			return err
		}
	}
	if estimate.ChangedAt > 0 {
		preview := ""
		switch {
		case (left > 0 || done) && faster:
			preview = notionMigrationFasterSay(language, estimate, done)
		case left > 0:
			preview = estimate.Say
		}
		if err := set("later", notionMigrationNoticeHead(language, notionMigrationTimeNoticeTitle(language, faster, done), left), preview); err != nil {
			return err
		}
	}
	if changed > 0 {
		noteWorkspaceNotices(map[[2]string]struct{}{{job.WorkspaceID, job.Owner}: {}})
	}
	return nil
}

// settleNotionMigrationNoticeHeads takes a run's time left out of its
// notices' heads as it ends: done, or stopped or canceled.
func settleNotionMigrationNoticeHeads(app core.App, job storedJob, migrationID string, done bool) error {
	stored, err := loadNotionMigration(app, job.Owner, job.WorkspaceID, migrationID)
	if err != nil {
		return err
	}
	// As stored: an ended run's wire says no later end (notionMigrationWire).
	report := emptyNotionMigrationRunReport()
	if stored.RunReport == "" || json.Unmarshal([]byte(stored.RunReport), &report) != nil || report.Estimate == nil {
		return nil
	}
	// A done run's time notice says it ended sooner than said when it ended
	// well before the end said, else later; a stopped one's keeps its way.
	estimate, now := *report.Estimate, time.Now()
	sooner := notionMigrationFaster(estimate, now)
	if done {
		sooner = estimate.EndsAt-now.UnixMilli() >= notionMigrationFasterLeast.Milliseconds()
	}
	if err := keepNotionMigrationNoticeHeads(app, job, migrationID, estimate, 0, sooner, done, now); err != nil {
		return err
	}
	if !done {
		// A stopped run's notices keep the import window, which says why and
		// how to go on.
		return nil
	}
	top, err := notionMigrationRunTopPage(app, stored, job.ID)
	if err != nil || top == "" {
		return err
	}
	return retargetNotionMigrationNotices(app, job, migrationID, top, now)
}

// retargetNotionMigrationNotices has a done run's stage and time notices open
// the moved content, where they opened the live import window while it ran;
// quietly, as the heads are kept: no push, the notice's time and whether it
// was read stay.
func retargetNotionMigrationNotices(app core.App, job storedJob, migrationID, pageID string, now time.Time) error {
	opened, err := pageNotificationTarget(app, pageID)
	if err != nil || opened == "" {
		return err
	}
	target := workspacePath(app, job.WorkspaceID, opened)
	ids := []any{}
	for _, kind := range append([]string{"later"}, notionMigrationStageKindsNoticed()...) {
		ids = append(ids, appNoticeID("notion-migration-"+kind, migrationID, job.ID))
	}
	result, err := app.DB().NewQuery(`UPDATE {{leafen_notifications}} SET [[target]]={:target}, [[page_id]]={:page}, [[updated_at]]={:now}
		WHERE [[id]] IN ({:a},{:b},{:c},{:d}) AND ([[target]]<>{:target} OR [[page_id]]<>{:page})`).
		Bind(dbx.Params{"target": target, "page": pageID, "now": now.UnixMilli(), "a": ids[0], "b": ids[1], "c": ids[2], "d": ids[3]}).Execute()
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows > 0 {
		noteWorkspaceNotices(map[[2]string]struct{}{{job.WorkspaceID, job.Owner}: {}})
	}
	return nil
}

// notionMigrationStageKindsNoticed are the stage notices' kinds.
func notionMigrationStageKindsNoticed() []string {
	kinds := make([]string, 0, len(notionMigrationStageNoticeKinds))
	for _, ended := range notionMigrationStageNoticeKinds {
		kinds = append(kinds, "stage-"+ended)
	}
	return kinds
}

// settleNotionMigrationNoticeHeadsOfJob settles a canceled import's
// migration notices, if a migration ran it.
func settleNotionMigrationNoticeHeadsOfJob(app core.App, jobID, ownerID string) {
	job, err := loadStoredJob(app, jobID, ownerID)
	if err != nil {
		return
	}
	payload := notionImportJobPayload{}
	if json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil || payload.MigrationID == "" {
		return
	}
	if err := settleNotionMigrationNoticeHeads(app, job, payload.MigrationID, false); err != nil {
		logDurableJobDiagnostic(app, errorCodeJobRunnerFailed, "", jobID, "notion-migration.notice", "settle", err)
	}
}

// notionMigrationEstimateLeftFields is a running migration's estimate as its
// short status says it: endsAt, the end its time left counts down to, and
// why that is far from the time said (reason, say), with neither the range
// said as it began nor that sentence (minMinutes, maxMinutes; detail gives
// the estimate as stored) — an end 13 hours away beside a time left of six
// said two times at once.
func notionMigrationEstimateLeftFields(fields map[string]any, estimate notionMigrationEstimate, language string, now time.Time) {
	end, _ := notionMigrationShownEnd(estimate)
	fields["endsAt"] = float64(end)
	delete(fields, "minMinutes")
	delete(fields, "maxMinutes")
	fields["say"] = notionMigrationLeftSay(language, estimate, now)
	fields["reason"] = notionMigrationLeftReason(estimate, now)
}
