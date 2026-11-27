package app

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A migration's run tells the person, in their inbox and by push, as each
// stage ends — Notion read, the content in Leafen, the comments — and when it
// is done, checked against Notion, or stopped (notion_migration_failure.go),
// so nobody has to keep the conversation open, and the agent needn't ask
// every few minutes (2026-11-16: a real move ended with no word from Leafen,
// and its agent posted over twenty progress messages). The run sees its
// stage every half minute (notion_migration_estimate_run.go); when stages
// passed between two looks, the last that ended is told — what comes next
// is the stage it is in now — and once the run is done only the end is.

// notionMigrationStageOrder is the order a run's stages come in.
var notionMigrationStageOrder = []string{
	notionMigrationStageReading, notionMigrationStageCreating, notionMigrationStageComments, notionMigrationStageVerifying,
}

// notifyNotionMigrationStagesEnded tells the person of the last stage that
// ended between the one the run was seen in and the one it is in now (a
// stage passed unseen would say "now making" of content made already).
func notifyNotionMigrationStagesEnded(app core.App, job storedJob, migrationID string, estimate notionMigrationEstimate, from, to string) error {
	start, end := slices.Index(notionMigrationStageOrder, from), slices.Index(notionMigrationStageOrder, to)
	if start < 0 || end <= start {
		return nil
	}
	return notifyNotionMigrationStage(app, job, migrationID, notionMigrationStageOrder[end-1], to, estimate)
}

// notifyNotionMigrationStage tells the person a stage ended, once a run. It
// opens the person's live view of the move, Leafen's import window, while
// the run goes, so they get the link at every stage whatever their agent
// says (user decision 2026-11-23 "묶어서 올려"; the command
// session's choice the same day, as agents kept the address to their own
// thinking); as the run ends, the moved content
// (settleNotionMigrationNoticeHeads).
func notifyNotionMigrationStage(app core.App, job storedJob, migrationID, ended, now string, estimate notionMigrationEstimate) error {
	language, err := serverTextLanguage(app, job.Owner, nil)
	if err != nil {
		return err
	}
	left := notionMigrationMinutesLeft(&estimate, time.Now())
	title, line := notionMigrationStageNotice(language, ended, now, left)
	return upsertNotionMigrationNotice(app, job, migrationID, "stage-"+ended, "", title, line)
}

// notifyNotionMigrationDone tells the person the move is done, with what the
// check against Notion left, and opens its report.
func notifyNotionMigrationDone(app core.App, job storedJob, migrationID string) error {
	language, err := serverTextLanguage(app, job.Owner, nil)
	if err != nil {
		return err
	}
	stored, err := loadNotionMigration(app, job.Owner, job.WorkspaceID, migrationID)
	if err != nil {
		return err
	}
	migration, err := notionMigrationWire(stored)
	if err != nil {
		return err
	}
	remaining := -1
	if verify := migration.RunReport.Verify; verify != nil {
		remaining = verify.RemainingTotal
	}
	title, line := notionMigrationDoneNotice(language, remaining)
	// An agent's move goes on in Leafen alone once the agent's turn ends:
	// the person tells it the move ended, and it checks and reports, as the
	// import window says too (NotionMigrationDraftView).
	if stored.AgentHandoff {
		line += notionMigrationDoneAgentLine(language)
	}
	pageID := migration.RunReport.ReportPageID
	if pageID == "" {
		if pageID, err = notionMigrationRunTopPage(app, stored, job.ID); err != nil {
			return err
		}
	}
	// The run's earlier notices lose their time left (notion_migration_time_said.go).
	if err := settleNotionMigrationNoticeHeads(app, job, migrationID, true); err != nil {
		return err
	}
	return upsertNotionMigrationNotice(app, job, migrationID, "done", pageID, title, line)
}

// upsertNotionMigrationNotice puts a run's notice in the person's inbox,
// with a push, once for this run and kind: opening the page when there is
// one, the import screen otherwise.
func upsertNotionMigrationNotice(app core.App, job storedJob, migrationID, kind, pageID, title, line string) error {
	target, noticePage := "/settings?section=import&source=notion", workspaceNoticePagePrefix+job.WorkspaceID
	if pageID != "" {
		if opened, err := pageNotificationTarget(app, pageID); err == nil && opened != "" {
			target, noticePage = opened, pageID
		}
	}
	// Opened in the migration's workspace, whichever the app shows then (person_links.go).
	target = workspacePath(app, job.WorkspaceID, target)
	now := time.Now().UTC().UnixMilli()
	_, err := upsertProductNotifications(context.Background(), app, []productNotificationProjection{{
		ID: appNoticeID("notion-migration-"+kind, migrationID, job.ID), EventID: appNoticeID("notion-migration-"+kind+"-event", job.ID),
		WorkspaceID: job.WorkspaceID, UserID: job.Owner, ActivityKey: "notion-migration-" + kind + ":" + migrationID + ":" + job.ID,
		Kind: "system", PageID: noticePage, Title: title, Preview: line, Target: target, OccurredAt: now, Personal: true,
	}}, now)
	return err
}

// notionMigrationStageNotice is a stage's end as the notice says it: what is
// done and, while more comes, about how long in its head, where it is seen
// at a glance (2026-11-20: the time left at the end of a long line went
// unseen), then what comes.
func notionMigrationStageNotice(language, ended, now string, left int) (string, string) {
	title, line := notionMigrationStageNoticeWords(language, ended, now)
	// Kept to the time left as the run goes (notion_migration_time_said.go).
	return notionMigrationNoticeHead(language, title, left), line
}

// notionMigrationStageNoticeWords is what a stage's notice says of it.
func notionMigrationStageNoticeWords(language, ended, now string) (string, string) {
	if language == accountLanguageKo {
		switch ended {
		case notionMigrationStageReading:
			return "Notion을 다 읽었습니다", "이제 리펜에 만듭니다."
		case notionMigrationStageCreating:
			if now == notionMigrationStageComments {
				return "Notion 내용을 다 옮겼습니다", "지금 둘러보셔도 됩니다. 댓글을 옮기고 원본과 대조하는 일이 남았습니다."
			}
			return "Notion 내용을 다 옮겼습니다", "지금 둘러보셔도 됩니다. 원본과 대조하고 보고서를 쓰는 일이 남았습니다."
		}
		return "댓글을 다 옮겼습니다", "원본과 대조하고 보고서를 씁니다."
	}
	switch ended {
	case notionMigrationStageReading:
		return "Notion is read", "Leafen is making it now."
	case notionMigrationStageCreating:
		if now == notionMigrationStageComments {
			return "The Notion content is in", "You can look it over now. The comments and the check against Notion are left."
		}
		return "The Notion content is in", "You can look it over now. The check against Notion and the report are left."
	}
	return "The comments are in", "Leafen checks it against Notion and writes the report."
}

// notionMigrationLeftWords is a run's time left as its notices' heads say
// it, as the import screen does: "약 40분 남음", "곧 끝남"; "" with none.
func notionMigrationLeftWords(language string, left int) string {
	switch {
	case notionMigrationAlmostDone(left):
		if language == accountLanguageKo {
			return "곧 끝남"
		}
		return "almost done"
	case left > 0 && language == accountLanguageKo:
		return "약 " + notionMigrationDuration(language, left) + " 남음"
	case left > 0:
		return "about " + notionMigrationDuration(language, left) + " left"
	}
	return ""
}

// notifyNotionMigrationLater tells the person the run ends later than it
// said, why and how long it still takes: one notice a run, which a later
// change rewrites without telling them again, and whose head and line keep
// to the time left as the run goes (notion_migration_time_said.go).
func notifyNotionMigrationLater(app core.App, job storedJob, migrationID string, estimate notionMigrationEstimate, now time.Time) error {
	language, err := serverTextLanguage(app, job.Owner, nil)
	if err != nil {
		return err
	}
	title := notionMigrationNoticeHead(language, notionMigrationTimeNoticeTitle(language, false, false), notionMigrationMinutesLeft(&estimate, now))
	return upsertNotionMigrationNotice(app, job, migrationID, "later", "", title, estimate.Say)
}

// notionMigrationDoneNotice is the end as the notice says it: done, and what
// the check against Notion left (-1: no check).
func notionMigrationDoneNotice(language string, remaining int) (string, string) {
	if language == accountLanguageKo {
		switch {
		case remaining == 0:
			return "Notion 옮기기를 마쳤습니다", "원본과 대조해 다른 곳이 없습니다. 보고서에서 옮긴 것을 확인하세요."
		case remaining > 0:
			return "Notion 옮기기를 마쳤습니다", "원본과 대조했고 다른 곳 " + strconv.Itoa(remaining) + "개는 까닭과 함께 보고서에 있습니다."
		}
		return "Notion 옮기기를 마쳤습니다", "보고서에서 옮긴 것과 오지 못한 것을 확인하세요."
	}
	switch {
	case remaining == 0:
		return "The Notion move is done", "Checked against Notion, nothing differs. The report says what moved."
	case remaining > 0:
		return "The Notion move is done", "Checked against Notion: " + strconv.Itoa(remaining) + " differences, each with why, are in the report."
	}
	return "The Notion move is done", "The report says what moved and what didn’t come."
}

// notionMigrationDoneAgentLine is what the done notice of an agent's move
// adds: what the person does now.
func notionMigrationDoneAgentLine(language string) string {
	if language == accountLanguageKo {
		return " 에이전트에게 “끝났어”라고 말하면 결과를 확인해 보고합니다."
	}
	return " Tell your agent “It’s done,” and it checks the result and reports back."
}

// notionMigrationRunTopPage is the page a run made of the migration's first
// root, once it shows: the ledger's, or this run's own before the ledger has
// it — so its link is there as soon as the content is, not only at the end —
// or, before a run's first turn, the stopped runs' it takes over.
func notionMigrationRunTopPage(app core.App, stored storedNotionMigration, jobID string) (string, error) {
	if top, err := notionMigrationImportTop(app, stored); err != nil || top != "" {
		return top, err
	}
	if jobID == "" {
		return "", nil
	}
	if top, err := notionMigrationJobTopPage(app, stored, jobID); err != nil || top != "" {
		return top, err
	}
	return notionMigrationTakenOverTop(app, stored, jobID)
}
