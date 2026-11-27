package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A migration's run after the first goes on from what already moved
// (decision "설계 우선 Notion 이사" 3절 원칙 4; 2026-11-16: a real run again
// after a stop made all 13,391 things anew in its count, asked 11,300 blocks
// for their comments again for over an hour, gave every page it moved the
// run's time as its last change, and reported 0 made). It reads Notion again,
// and then:
//
//   - a page, row or block that came before and that Notion didn't change
//     since is left as it is: nothing writes it, so its times and the people
//     who made and changed it stay (publishing passes it by, and the page
//     stamps keep it);
//   - one Notion changed takes Notion's change, and one new comes;
//   - after a stopped run, the pages, rows and blocks the stopped run asked
//     for their comments are not asked again — it is the same move going on;
//   - it counts what it made new, what it changed and what it left as it
//     was, for its progress and its report (notionMigrationRunTally).
//
// A stopped run can also go on from the step it stopped at, reading nothing
// again (resumeNotionMigrationRun); a run again reads all of Notion again.

// notionMigrationRunAgain is what a migration's run knows of the runs before
// it.
type notionMigrationRunAgain struct {
	// When the run whose state this run starts from began, in milliseconds:
	// the last that finished, or the stopped one this run takes over when
	// none finished. Notion's changes after it are taken again.
	MigrationRerunSince int64 `json:"migrationRerunSince,omitzero"`
	// When the last run that finished began; -1 when none finished, so every
	// page takes its Notion history (a stopped run hadn't stamped them yet).
	// 0 in a run started before runs kept it.
	MigrationStampedBefore int64 `json:"migrationStampedBefore,omitzero"`
	// After a stopped run: since when the comments asked belong to this same
	// move, so they aren't asked again; 0 asks every one.
	CommentsAskedSince int64 `json:"commentsAskedSince,omitzero"`
	// What its first turn takes from the ledger (notion_migration_run_ledger_start.go).
	notionMigrationLedgerStart
}

// notionMigrationRunAgainOf is what a new run of a migration knows of its
// runs before, with the payload of the run that stopped, if one did.
func notionMigrationRunAgainOf(stored storedNotionMigration, stopped *notionImportJobPayload) notionMigrationRunAgain {
	again := notionMigrationRunAgain{MigrationRerunSince: stored.LastDoneStarted, MigrationStampedBefore: stored.LastDoneStarted}
	if again.MigrationRerunSince == 0 && stored.RunState == notionMigrationRunDone {
		again.MigrationRerunSince = stored.RunStarted
		again.MigrationStampedBefore = stored.RunStarted
	}
	if again.MigrationStampedBefore == 0 {
		again.MigrationStampedBefore = -1
	}
	if stopped == nil {
		return again
	}
	// A first run that stopped: what it made is what this run goes on from.
	if again.MigrationRerunSince == 0 {
		again.MigrationRerunSince = stored.RunStarted
	}
	// Runs that stopped one after another are one move: the first's start.
	again.CommentsAskedSince = stored.RunStarted
	if stopped.CommentsAskedSince > 0 {
		again.CommentsAskedSince = stopped.CommentsAskedSince
	}
	return again
}

// loadNotionMigrationStoppedPayload is the payload of a migration's run's
// job — the stopped one's, or the one going on — nil when it can't be read.
func loadNotionMigrationStoppedPayload(app core.App, jobID string) *notionImportJobPayload {
	job, err := loadStoredJob(app, jobID, "")
	if err != nil {
		return nil
	}
	payload := notionImportJobPayload{}
	if json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil {
		return nil
	}
	return &payload
}

// notionMigrationKeepsStamp says whether a run again leaves a page's or a
// row's record as it is: an earlier run gave it its Notion history, and
// Notion didn't change it since that run began — what happened in Leafen
// since stays. A record made since then hasn't had its history yet.
func notionMigrationKeepsStamp(app core.App, payload notionImportJobPayload, pageID string, edited time.Time) (bool, error) {
	before := payload.MigrationStampedBefore
	if before == 0 {
		// A run started before runs kept when the last finished one began.
		before = payload.MigrationRerunSince
	}
	// Notion's time is to the minute: one in the minute that run began may be
	// after it (notionMigrationEditedSince).
	if payload.MigrationRerunSince <= 0 || before <= 0 || notionMigrationEditedSince(edited, before) {
		return false, nil
	}
	var made int64
	if err := app.DB().NewQuery(`
		SELECT COALESCE(CAST(strftime('%s', [[created]]) AS INTEGER), 0) * 1000 FROM {{leafen_pages}} WHERE [[id]]={:id}
	`).Bind(dbx.Params{"id": pageID}).Row(&made); err != nil {
		return false, nil
	}
	return made < before, nil
}

// notionMigrationLedgerCommentsAsked marks, by its source's ID, a page, row
// or block a run asked Notion for its comments, with when.
const notionMigrationLedgerCommentsAsked = "comments_asked"

// notionMigrationCommentsAskedSQL leaves out of the comments to ask, with
// {:askedSince} above 0, the sources this same move asked already.
const notionMigrationCommentsAskedSQL = `
			AND ({:askedSince} <= 0 OR NOT EXISTS (SELECT 1 FROM {{leafen_notion_migration_ledger}} AS [[asked]]
				WHERE [[asked]].[[migration_id]]={:migrationId} AND [[asked]].[[source_id]]=[[mapping]].[[source_id]]
					AND [[asked]].[[kind]]='` + notionMigrationLedgerCommentsAsked + `'
					AND CAST([[asked]].[[state]] AS INTEGER) >= {:askedSince}))`

// recordNotionMigrationCommentsAsked keeps that a run asked these sources
// for their comments, now.
func recordNotionMigrationCommentsAsked(app core.App, migrationID string, sourceIDs []string) error {
	if len(sourceIDs) == 0 {
		return nil
	}
	encoded, err := json.Marshal(sourceIDs)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_migration_ledger}}
			([[migration_id]],[[workspace_id]],[[source_id]],[[kind]],[[native_id]],[[page_id]],[[state]])
		SELECT [[migration]].[[id]], [[migration]].[[workspace_id]], [[asked]].[[value]], {:kind}, '', '', {:at}
		FROM {{leafen_notion_migrations}} AS [[migration]], json_each({:asked}) AS [[asked]]
		WHERE [[migration]].[[id]]={:migrationId}
		ON CONFLICT ([[migration_id]],[[source_id]],[[kind]]) DO UPDATE SET [[state]]=excluded.[[state]]
	`).Bind(dbx.Params{
		"migrationId": migrationID, "kind": notionMigrationLedgerCommentsAsked, "asked": string(encoded),
		"at": strconv.FormatInt(time.Now().UTC().UnixMilli(), 10),
	}).Execute()
	return err
}

// notionMigrationTally counts, as a run's make step goes, what it made new,
// changed and left as it was.
type notionMigrationTally struct {
	notionMigrationRunTally
	// The sources it counted as made, by their sequence: a run again counts
	// those a template's body held before as a page's body counts its own
	// (notionMigrationTally.countTemplateBodiesAgain).
	made []int64
}

// count counts a source as the report's line of what moved does
// (notion_import_counted.go): a template or a row in Notion's trash, which
// that line says apart, not among the rows.
func (tally *notionMigrationTally) count(source storedNotionSource, made, changed bool) {
	counts := &tally.Kept
	switch {
	case made:
		counts = &tally.Made
	case changed:
		counts = &tally.Changed
	}
	if counts.countKind(notionImportCountedKind(source.Kind, source.SourceID, source.ParentKind, source.Text), 1) && made {
		tally.made = append(tally.made, source.Sequence)
	}
}

// countKind adds by to the count of what notionImportCountedKind calls
// counted, and says whether it is one of them.
func (counts *notionMigrationTallyCounts) countKind(counted string, by int) bool {
	switch counted {
	case notionImportCountedPage:
		counts.Pages += by
	case notionImportCountedDatabase:
		counts.Databases += by
	case notionImportCountedRow:
		counts.Rows += by
	case notionImportCountedBlock:
		counts.Blocks += by
	default:
		return false
	}
	return true
}

func (tally notionMigrationTally) empty() bool {
	return tally.Made == notionMigrationTallyCounts{} && tally.Changed == notionMigrationTallyCounts{} &&
		tally.Kept == notionMigrationTallyCounts{}
}

func (counts *notionMigrationTallyCounts) add(more notionMigrationTallyCounts) {
	counts.Pages += more.Pages
	counts.Databases += more.Databases
	counts.Rows += more.Rows
	counts.Blocks += more.Blocks
}

// addNotionMigrationRunTally adds a turn's counts to its run's, what it made
// of the body of a template it read again counted by what that body held
// before; the first adds what the bodies of the templates the run left as
// they were hold, which no turn goes over (notion_import_counted.go).
func addNotionMigrationRunTally(app core.App, jobID string, payload notionImportJobPayload, tally notionMigrationTally) error {
	if payload.MigrationID == "" || tally.empty() {
		return nil
	}
	if err := tally.countTemplateBodiesAgain(app, jobID, payload); err != nil {
		return err
	}
	first := false
	if err := updateNotionMigrationRunReport(app, payload.MigrationID, func(report *notionMigrationRunReport) {
		if report.Tally == nil {
			report.Tally, first = &notionMigrationRunTally{}, true
		}
		report.Tally.Made.add(tally.Made)
		report.Tally.Changed.add(tally.Changed)
		report.Tally.Kept.add(tally.Kept)
	}); err != nil || !first {
		return err
	}
	kept, err := notionImportKeptTemplates(app, jobID, payload.MigrationID)
	if err != nil || kept.Body == (notionMigrationTallyCounts{}) {
		return err
	}
	return updateNotionMigrationRunReport(app, payload.MigrationID, func(report *notionMigrationRunReport) {
		report.Tally.Kept.add(kept.Body)
	})
}

// notionMigrationDatabaseRenamed applies a staged database and says whether
// one made before took the name Notion gave it since.
func notionMigrationDatabaseRenamed(app core.App, source storedNotionSource, mappings *notionApplyMappings, apply func() error) (bool, error) {
	dataSourceID, made := mappings.nativeID(source.SourceID, "data_source")
	if !made {
		return false, apply()
	}
	title := func() string {
		name := ""
		_ = app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"id": dataSourceID}).Row(&name)
		return name
	}
	before := title()
	if err := apply(); err != nil {
		return false, err
	}
	return title() != before, nil
}

// notionMigrationTallyText says a run again's counts as its progress and its
// report say them: what it made new, changed and left as it was.
func notionMigrationTallyText(language string, tally notionMigrationRunTally) string {
	total := func(counts notionMigrationTallyCounts) int {
		return counts.Pages + counts.Databases + counts.Rows + counts.Blocks
	}
	if language == accountLanguageKo {
		return fmt.Sprintf("새로 만듦 %d · 바꿈 %d · 그대로 둠 %d", total(tally.Made), total(tally.Changed), total(tally.Kept))
	}
	return fmt.Sprintf("%d made new, %d changed, %d left as they were", total(tally.Made), total(tally.Changed), total(tally.Kept))
}

// notionMigrationEstimateAgain says, for the estimate, whether a migration's
// run is or would be a run again, and how many pages, rows and blocks a
// stopped run of the same move asked for their comments already, which it
// doesn't ask again.
func notionMigrationEstimateAgain(app core.App, stored storedNotionMigration) (bool, int) {
	since := int64(0)
	again := stored.RunState != ""
	switch stored.RunState {
	case notionMigrationRunFailed:
		since = notionMigrationRunAgainOf(stored, loadNotionMigrationStoppedPayload(app, stored.ImportJobID)).CommentsAskedSince
	case notionMigrationRunRunning:
		// The run going on knows.
		payload := loadNotionMigrationStoppedPayload(app, stored.ImportJobID)
		again = payload != nil && payload.MigrationRerunSince > 0
		if payload != nil {
			since = payload.CommentsAskedSince
		}
	}
	if since <= 0 {
		return again, 0
	}
	asked := 0
	_ = app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_notion_migration_ledger}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]={:kind} AND CAST([[state]] AS INTEGER) >= {:since}
	`).Bind(dbx.Params{"migrationId": stored.ID, "kind": notionMigrationLedgerCommentsAsked, "since": since}).Row(&asked)
	return again, asked
}

// notionMigrationRunAgainSay is the sentence the agent and the import screen
// say before a run again: how long, how long the run before took when it is
// reckoned by that (took, in minutes; notion_migration_estimate_took.go),
// that it reads Notion again to find what changed, and that what didn't
// change stays as it is.
func notionMigrationRunAgainSay(language string, estimate notionMigrationEstimate, bundle bool, asked, took int, read, comments [2]int) string {
	whole := notionMigrationDurationRange(language, estimate.MinMinutes, estimate.MaxMinutes)
	readRange := notionMigrationDurationRange(language, read[0], read[1])
	commentRange := notionMigrationDurationRange(language, comments[0], comments[1])
	if language == accountLanguageKo {
		bodies := fmt.Sprintf("페이지 %d개와 행 %d개", estimate.Pages, estimate.Rows)
		if estimate.Rows == 0 {
			bodies = fmt.Sprintf("페이지 %d개", estimate.Pages)
		}
		parts := []string{fmt.Sprintf("다시 가져오는 데 약 %s 걸립니다.", whole)}
		if took > 0 {
			parts = append(parts, fmt.Sprintf("지난 실행은 %s 걸렸습니다.", notionMigrationDuration(language, took)))
		}
		if bundle {
			parts = append(parts, bodies+"를 블록까지 다시 읽어 바뀐 것을 찾기 때문입니다.")
		} else {
			why := fmt.Sprintf("Notion을 처음부터 다시 읽어 바뀐 것을 찾기 때문입니다 — 본문은 블록까지 Notion에 하나씩 물어 읽어야 해서, %s를 다시 읽는 데 %s", bodies, readRange)
			if estimate.CommentBlocks > 0 {
				why += fmt.Sprintf(", 블록 약 %d개에 댓글이 있는지 묻는 데 %s", estimate.CommentBlocks, commentRange)
			}
			parts = append(parts, why+" 걸립니다.")
		}
		if asked > 0 {
			parts = append(parts, fmt.Sprintf("멈춘 실행이 댓글을 이미 물은 페이지·행·블록 %d개는 다시 묻지 않습니다.", asked))
		}
		parts = append(parts, "이미 옮긴 것 가운데 Notion에서 바뀌지 않은 페이지·행·블록은 다시 쓰지 않아 수정 시각과 편집한 사람도 그대로이고, 바뀐 것만 바꾸고 새것만 만듭니다. "+
			"그동안 리펜의 페이지는 그대로 쓸 수 있고, 단계가 끝날 때마다 받은편지함으로 알려 드립니다.")
		return strings.Join(parts, " ")
	}
	bodies := fmt.Sprintf("%d pages and %d rows", estimate.Pages, estimate.Rows)
	if estimate.Rows == 0 {
		bodies = fmt.Sprintf("%d pages", estimate.Pages)
	}
	parts := []string{fmt.Sprintf("Bringing it again takes about %s.", whole)}
	if took > 0 {
		parts = append(parts, fmt.Sprintf("The last run took %s.", notionMigrationDuration(language, took)))
	}
	if bundle {
		parts = append(parts, "Leafen reads "+bodies+" again, block by block, to find what changed.")
	} else {
		why := "It reads all of Notion again to find what changed: Notion gives a body block by block, one request at a time, so reading " + bodies +
			" again takes " + readRange
		if estimate.CommentBlocks > 0 {
			why += fmt.Sprintf(", and asking about %d blocks for their comments %s", estimate.CommentBlocks, commentRange)
		}
		parts = append(parts, why+".")
	}
	if asked > 0 {
		parts = append(parts, fmt.Sprintf("The %d pages, rows and blocks the stopped run asked for their comments aren’t asked again.", asked))
	}
	parts = append(parts, "What moved already and didn’t change in Notion isn’t written again, so its times and editors stay; only what changed is changed, and only what is new is made. "+
		"Leafen’s pages stay usable meanwhile, and Leafen tells you in your inbox as each stage ends.")
	return strings.Join(parts, " ")
}

// notionMigrationResumeMinutes reckons how long going on from where a run
// stopped takes, from the time the run was said to take (runReport.estimate):
// what is left of the stage it stopped in, by how far its job had come, and
// the stages after it. 0 when the run kept no estimate.
func notionMigrationResumeMinutes(estimate *notionMigrationEstimate, step string, job *notionImportJob) int {
	if estimate == nil || estimate.MaxMinutes == 0 {
		return 0
	}
	left := func(done, total int) float64 {
		if total <= 0 {
			return 1
		}
		return max(0, 1-float64(done)/float64(total))
	}
	read, made, comments := float64(estimate.ReadMinutes), float64(estimate.MakeMinutes), float64(estimate.CommentMinutes)
	// The check against Notion, the report and the rest.
	minutes := 3.0
	switch notionMigrationStageOfStep(step) {
	case notionMigrationStageReading:
		share := 1.0
		if job != nil && job.Progress != nil {
			share = left(job.Progress.BodiesRead, job.Progress.BodiesTotal)
		}
		minutes += read*share + made + comments
	case notionMigrationStageCreating:
		share := 0.0
		if step == "make" {
			share = 1
			if job != nil {
				share = left(job.ProcessedItems, job.TotalItems)
			}
		}
		minutes += made*share + comments
	case notionMigrationStageComments:
		share := 1.0
		if job != nil && job.CommentBlocksTotal > 0 {
			share = left(job.CommentBlocksDone, job.CommentBlocksTotal)
		}
		minutes += comments * share
	}
	return notionMigrationRoundUp(minutes)
}

// notionMigrationRunAgainSentence is what a migration's run again would say
// before it starts — what it reads and redoes, what it leaves as it is and
// about how long — in the person's language; "" when its plan can't say.
func notionMigrationRunAgainSentence(app core.App, ownerID string, migration notionMigration) string {
	stored, err := loadNotionMigration(app, ownerID, migration.WorkspaceID, migration.ID)
	if err != nil {
		return ""
	}
	state, err := notionMigrationPlanStateOf(context.Background(), app, stored)
	if err != nil || state.Estimate == nil {
		return ""
	}
	return state.Estimate.Say
}
