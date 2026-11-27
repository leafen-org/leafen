package app

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A migration's run that stops says so where the person and their agent look
// — the run's status, the top of its report page and their inbox — with the
// step it stopped at, what it was changing, why in words, whether trying
// again is safe and what to do (2026-11-16: a real run stopped after two
// hours with an error code and a reference only, its report reading as if it
// had ended, and "run it again" read everything from Notion again). Trying
// again goes on from the step that stopped, as the job kept it; a run from
// the start reads all of Notion again and says so.

// notionImportTargetError names what a step was changing when it stopped.
type notionImportTargetError struct {
	kind, id string
	err      error
}

func (e *notionImportTargetError) Error() string { return e.err.Error() }
func (e *notionImportTargetError) Unwrap() error { return e.err }

// notionImportAt names the page, row or block a failed write was for.
func notionImportAt(kind, id string, err error) error {
	if err == nil || id == "" {
		return err
	}
	return &notionImportTargetError{kind: kind, id: id, err: err}
}

// notionImportStage is the step a run's job is at, as its failure names it.
func notionImportStage(app core.App, job storedJob, payload notionImportJobPayload) string {
	switch {
	case payload.Phase == notionImportPhaseScope:
		return "read"
	case payload.StampingRows:
		return "finish"
	case payload.Restructuring:
		steps, err := notionMigrationRestructures(app, job.Owner, payload)
		if err != nil || payload.RestructureIndex >= len(steps) {
			return "plan"
		}
		switch steps[payload.RestructureIndex].Kind {
		case notionMigrationStepComments:
			return "comments"
		case notionMigrationStepMovePage, notionMigrationStepMoveDatabase:
			return "moves"
		case notionMigrationStepPeople:
			return "people"
		case notionMigrationStepPlace, notionMigrationStepRemove:
			return "rerun"
		case notionMigrationStepLedger:
			return "ledger"
		case notionMigrationStepVerify:
			return "verify"
		case notionMigrationStepFormats:
			return "formats"
		case notionMigrationStepReportPage:
			return "report"
		}
		return "plan"
	case payload.Publishing:
		return "publish"
	case payload.Linking || payload.LinkingViews:
		return "link"
	}
	return "make"
}

// notionImportStageNames name the steps for the agent, and in the report and
// the notice in the person's language.
var notionImportStageNames = map[string]map[string]string{
	accountLanguageKo: {
		"read": "Notion 읽기", "make": "리펜에 만들기", "link": "관계·롤업 잇기", "publish": "페이지 보이기",
		"plan": "계획대로 속성 바꾸기", "comments": "댓글 옮기기", "moves": "계획대로 페이지 옮기기", "people": "사람 잇기",
		"rerun": "다시 실행에서 옮기고 지운 블록 정리", "formats": "Notion 화면대로 너비·레이아웃·보기 맞추기", "ledger": "옮긴 것 기록", "verify": "원본과 대조", "report": "보고서 쓰기", "finish": "템플릿과 행의 Notion 기록 마무리",
	},
	accountLanguageEn: {
		"read": "reading Notion", "make": "making it in Leafen", "link": "linking relations and rollups", "publish": "showing the pages",
		"plan": "changing properties as planned", "comments": "moving comments", "moves": "moving pages as planned", "people": "matching people",
		"rerun": "placing and removing the blocks Notion moved or deleted", "formats": "matching widths, layouts and views to Notion’s screen", "ledger": "recording what moved", "verify": "checking it against Notion", "report": "writing the report",
		"finish": "finishing templates and rows’ Notion history",
	},
}

func notionImportStageName(language, stage string) string {
	names := notionImportStageNames[language]
	if names == nil {
		names = notionImportStageNames[accountLanguageEn]
	}
	return cmpOr(names[stage], stage)
}

// notionImportFailureCauseReason says in words what a cause the job's own
// code doesn't name was, or "".
func notionImportFailureCauseReason(cause string) string {
	switch cause {
	case errorCodeDatabaseRowPageInTrash, errorCodePageInTrash:
		return "a page or row it was changing had been moved to the trash during the run"
	case errorCodeDatabaseRowPageNotFound, errorCodePageNotFound:
		return "a page or row it was changing had been deleted during the run"
	}
	return ""
}

// notionImportRetrySafe is whether trying the same run again can go through
// as things are, and what to do first when it can't.
func notionImportRetrySafe(code string) (bool, string) {
	switch code {
	case errorCodeNotionMigrationPlanConflict, errorCodeNotionMigrationPlanBlocked, errorCodeNotionMigrationInvalid:
		return false, "the plan has to be checked and agreed again first; then a run from the start"
	case errorCodeNotionImportTokenRejected, errorCodeNotionImportConnectionFailed, errorCodeNotionImportConnectionNotFound:
		return false, "Notion is connected again first, with its token got again in the conversation as at first; then trying again goes on from where it stopped"
	case errorCodeNotionImportScopeFailed:
		return false, "the person shares the pages with the Notion connection again first; then trying again goes on from where it stopped"
	}
	return true, ""
}

// notionMigrationRunFailureOf is what a failed run's job says of its stop.
func notionMigrationRunFailureOf(
	app core.App, job storedJob, payload notionImportJobPayload, source, reference, code string, cause error,
) notionMigrationRunFailure {
	failure := notionMigrationRunFailure{
		Stage: notionImportStage(app, job, payload), Code: code, CauseCode: diagnosticCauseCode(cause, code),
		DiagnosticReference: reference, At: time.Now().UTC().UnixMilli(),
	}
	target, refused := &notionImportTargetError{}, &notionImportItemRefusedError{}
	if errors.As(cause, &target) {
		failure.TargetKind, failure.TargetID = target.kind, target.id
	} else if errors.As(cause, &refused) {
		failure.TargetKind, failure.TargetID = refused.source.Kind, refused.source.SourceID
	}
	failure.Reason = notionImportFailureReason(code, source)
	if why := notionImportFailureCauseReason(failure.CauseCode); why != "" && code == errorCodeJobRunnerFailed {
		failure.Reason = why
	}
	// A refusal says what the graph checked and why, another cause its own
	// words (notion_import_left_out.go).
	if words := notionImportRefusalWords(cause); words != "" {
		failure.Reason = "Leafen refused what it was making: " + words
	} else if words := notionImportCauseWords(cause); words != "" {
		failure.Reason += ": " + words
	}
	safe, first := notionImportRetrySafe(code)
	failure.RetrySafe = safe
	stage := notionImportStageName(accountLanguageEn, failure.Stage)
	failure.Retry = "Trying again goes on from " + stage + ": what already moved stays, and nothing done before it is read from Notion or made again."
	if !safe {
		failure.Retry = "Not as things are: " + first + "."
	}
	return failure
}

// recordNotionImportFailure keeps a failed migration run's stop in its
// report, puts it at the top of the report page once the run's pages show,
// and tells the person in their inbox. A plain import's card says its stop
// already (notionImportFailure.ts).
func (r *durableJobRunner) recordNotionImportFailure(ctx context.Context, jobID, reference, code string, cause error) {
	job, err := loadStoredJob(r.app, jobID, "")
	if err != nil || job.Kind != notionImportJobKind {
		return
	}
	payload := notionImportJobPayload{}
	if json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil || payload.MigrationID == "" {
		return
	}
	stored, err := loadNotionMigration(r.app, job.Owner, job.WorkspaceID, payload.MigrationID)
	if err != nil {
		return
	}
	failure := notionMigrationRunFailureOf(r.app, job, payload, stored.SourceLabel, reference, code, cause)
	if err := updateNotionMigrationRunReport(r.app, payload.MigrationID, func(report *notionMigrationRunReport) {
		report.Failure = &failure
	}); err != nil {
		logDurableJobDiagnostic(r.app, code, reference, jobID, "notion-migration.failure", "record", err)
		return
	}
	if payload.Restructuring || payload.StampingRows {
		if err := r.writeNotionMigrationReport(ctx, r.app, job, payload); err != nil {
			logDurableJobDiagnostic(r.app, code, reference, jobID, "notion-migration.failure", "report", err)
		}
	}
	if err := notifyNotionMigrationFailure(r.app, job, payload.MigrationID, failure); err != nil {
		logDurableJobDiagnostic(r.app, code, reference, jobID, "notion-migration.failure", "notice", err)
	}
	if err := settleNotionMigrationNoticeHeads(r.app, job, payload.MigrationID, false); err != nil {
		logDurableJobDiagnostic(r.app, code, reference, jobID, "notion-migration.failure", "notice-heads", err)
	}
}

// notifyNotionMigrationFailure tells the person who ran it in their inbox,
// once for this stop: the report page opens from it while there is one, the
// import screen otherwise.
func notifyNotionMigrationFailure(app core.App, job storedJob, migrationID string, failure notionMigrationRunFailure) error {
	language, err := serverTextLanguage(app, job.Owner, nil)
	if err != nil {
		return err
	}
	report, err := loadNotionMigration(app, job.Owner, job.WorkspaceID, migrationID)
	if err != nil {
		return err
	}
	run := emptyNotionMigrationRunReport()
	if err := json.Unmarshal([]byte(report.RunReport), &run); err != nil {
		return err
	}
	pageID, target := workspaceNoticePagePrefix+job.WorkspaceID, "/settings?section=import&source=notion"
	if run.ReportPageID != "" {
		if opened, err := pageNotificationTarget(app, run.ReportPageID); err == nil && opened != "" {
			pageID, target = run.ReportPageID, opened
		}
	}
	// Opened in the migration's workspace, whichever the app shows then (person_links.go).
	target = workspacePath(app, job.WorkspaceID, target)
	text := notionMigrationFailureText(language, failure)
	stamp := strconv.FormatInt(failure.At, 10)
	_, err = upsertProductNotifications(context.Background(), app, []productNotificationProjection{{
		ID: appNoticeID("notion-migration-failed", migrationID, job.ID, stamp), EventID: appNoticeID("notion-migration-failed-event", job.ID, stamp),
		WorkspaceID: job.WorkspaceID, UserID: job.Owner, ActivityKey: "notion-migration-failed:" + migrationID + ":" + stamp,
		Kind: "system", PageID: pageID, Title: text.Title, Preview: text.Line, Target: target, OccurredAt: failure.At, Personal: true,
	}}, failure.At)
	return err
}

type notionMigrationFailureWords struct{ Title, Line string }

// notionMigrationFailureCallout is a stop as the report's first line says
// it, with a link to the page or row it was changing.
func notionMigrationFailureCallout(language string, failure notionMigrationRunFailure) string {
	words := notionMigrationFailureText(language, failure)
	callout := "**" + words.Title + "** — " + words.Line
	if failure.TargetID != "" && (failure.TargetKind == "page" || failure.TargetKind == "row") {
		open := map[string]string{accountLanguageKo: "멈춘 곳 열기"}[language]
		callout += " [" + cmpOr(open, "Open where it stopped") + "](/p/" + failure.TargetID + ")"
	}
	return callout
}

// notionMigrationFailureText is a stop as the report's top and the notice say
// it, in the person's language.
func notionMigrationFailureText(language string, failure notionMigrationRunFailure) notionMigrationFailureWords {
	stage := notionImportStageName(language, failure.Stage)
	if language == accountLanguageKo {
		line := fmt.Sprintf("%s 단계에서 멈췄습니다: %s.", stage, notionMigrationFailureReasonKo(failure))
		if failure.RetrySafe {
			line += " 다시 시도하면 이 단계부터 이어 가고, 이미 옮긴 것은 그대로이며 앞 단계는 Notion에서 다시 읽지 않습니다."
		} else {
			line += " 그대로 다시 시도하면 같은 곳에서 멈춥니다 — 먼저 까닭을 해결해 주세요."
		}
		return notionMigrationFailureWords{Title: "Notion 옮기기가 끝나지 못했습니다", Line: line}
	}
	line := fmt.Sprintf("It stopped while %s: %s.", stage, failure.Reason)
	if failure.RetrySafe {
		line += " Trying again goes on from this step; what moved stays, and nothing before it is read from Notion again."
	} else {
		line += " Trying again as things are stops at the same place — " + failure.Retry
	}
	return notionMigrationFailureWords{Title: "The Notion move didn’t finish", Line: line}
}

// notionMigrationFailureReasonKo is a stop's reason in Korean, as the import
// screen says it (importDialog:migration.failure).
func notionMigrationFailureReasonKo(failure notionMigrationRunFailure) string {
	switch failure.CauseCode {
	case errorCodeDatabaseRowPageInTrash, errorCodePageInTrash:
		if failure.Code == errorCodeJobRunnerFailed {
			return "실행하는 동안 바꾸던 페이지나 행이 휴지통으로 옮겨졌습니다"
		}
	case errorCodeDatabaseRowPageNotFound, errorCodePageNotFound:
		if failure.Code == errorCodeJobRunnerFailed {
			return "실행하는 동안 바꾸던 페이지나 행이 지워졌습니다"
		}
	}
	switch failure.Code {
	case errorCodeNotionImportProviderFailed:
		return "Notion이 요청을 거부하거나 답하지 않았습니다"
	case errorCodeNotionImportTokenRejected:
		return "Notion이 연결의 토큰을 더 이상 받지 않습니다 — 가져오기 화면에서 다시 연결하세요"
	case errorCodeNotionImportConnectionFailed, errorCodeNotionImportConnectionNotFound:
		return "Notion 연결을 쓸 수 없습니다 — 다시 연결하세요"
	case errorCodeNotionImportScopeFailed:
		return "Notion이 선택한 페이지를 읽게 하지 않았습니다 — 연결에 다시 공유하세요"
	case errorCodeNotionImportFileCopyRequired, errorCodeNotionImportFilePermanent:
		return "Notion에 있는 파일 하나를 복사하지 못했습니다"
	case errorCodeNotionMigrationPlanConflict:
		return "실행하는 동안 계획이 바뀌었습니다 — 계획을 다시 확인하세요"
	case errorCodeNotionMigrationPlanBlocked, errorCodeNotionMigrationInvalid:
		return "계획대로 할 수 없습니다 — 계획을 다시 확인하세요"
	case errorCodeImportedPeopleMappingFailed:
		return "Notion 사람을 멤버와 잇다가 멈췄습니다"
	case errorCodeNotionImportCreateFailed, errorCodeNotionMigrationFailed:
		return "리펜이 가져온 것을 만들지 못했습니다"
	}
	return "리펜이 가져오는 도중 멈췄습니다"
}

// notionMigrationStoppedNext is what a stopped run's status tells the agent
// to do: why, where, and the two ways on with what each costs — going on
// from where it stopped, about how long that takes, and a run from the start
// with the sentence it says first (again, notion_migration_run_again.go),
// which the agent tells the person before either.
func notionMigrationStoppedNext(migration notionMigration, job *notionImportJob, startedAt, stoppedAt int64, again string) string {
	failure := migration.RunReport.Failure
	code := ""
	if job != nil {
		code = job.ErrorCode
	}
	minutes := ""
	if stoppedAt > startedAt && startedAt > 0 {
		minutes = " (this run went " + notionMigrationDuration(accountLanguageEn, int((stoppedAt-startedAt)/60000+1)) + " before it stopped)"
	}
	resume, step := "", "read"
	if failure != nil {
		step = failure.Stage
	}
	if left := notionMigrationResumeMinutes(migration.RunReport.Estimate, step, job); left > 0 {
		resume = " (about " + notionMigrationDuration(accountLanguageEn, left) + " from there)"
	}
	tell := " Which way, and how long it takes, is the person's choice."
	if again != "" {
		tell = fmt.Sprintf(" Which way, and how long it takes, is the person's choice; a run from the start says first: %q", again)
	}
	if failure == nil {
		return "Stopped: " + notionImportFailureReason(code, migration.Source) + " (" + cmpOr(code, errorCodeJobRunnerFailed) + "); their inbox says so too. " +
			"leafen_notion_migration_run with resume true goes on from where it stopped" + resume +
			"; without resume it reads everything from Notion again" + minutes + ", writing only what changed." + tell
	}
	next := "Stopped while " + notionImportStageName(accountLanguageEn, failure.Stage) + ": " + failure.Reason + " (" + failure.Code
	if failure.CauseCode != "" && failure.CauseCode != failure.Code {
		next += ", cause " + failure.CauseCode
	}
	next += ")"
	if failure.TargetID != "" {
		next += ", at the " + failure.TargetKind + " " + failure.TargetID
	}
	next += "; their inbox and the report page (report_link) say why. "
	if failure.RetrySafe {
		return next + "To go on: leafen_notion_migration_run with resume true" + resume + " — " + failure.Retry +
			" Only if the person wants everything brought from Notion again, leafen_notion_migration_run without resume: it reads all of Notion again" + minutes +
			", and leaves what didn't change as it is, writing only what changed." + tell
	}
	return next + failure.Retry + " Then leafen_notion_migration_run with resume true goes on from where it stopped" + resume +
		"; without resume it reads all of Notion again" + minutes + ", writing only what changed." + tell
}

// resumeNotionMigrationRun has a migration's stopped run try again from the
// step it stopped at.
func resumeNotionMigrationRun(ctx context.Context, app core.App, ownerID, workspaceID, id string) (notionMigration, error) {
	stored, err := loadNotionMigration(app, ownerID, workspaceID, id)
	if err != nil {
		return notionMigration{}, err
	}
	if stored.RunState != notionMigrationRunFailed || stored.ImportJobID == "" {
		return notionMigration{}, errNotionMigrationNothingToResume
	}
	runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner)
	if !ok || runner == nil {
		return notionMigration{}, errNotionMigrationNothingToResume
	}
	outcome, err := runner.retryNotionImport(ctx, app, stored.ImportJobID, ownerID, workspaceID)
	if err != nil {
		return notionMigration{}, err
	}
	if outcome.missing || outcome.unavailable {
		return notionMigration{}, errNotionMigrationNothingToResume
	}
	runner.notify()
	if stored, err = loadNotionMigration(app, ownerID, workspaceID, id); err != nil {
		return notionMigration{}, err
	}
	return notionMigrationWireWithTop(app, stored)
}

// errNotionMigrationNothingToResume is a migration whose run didn't stop, or
// whose stopped run can't go on (its import was let go): it runs from the start.
var errNotionMigrationNothingToResume = errors.New("notion migration has no stopped run to resume")

// retryNotionImport has a failed import's job try again from where it was:
// over the connection that reaches its source now, unless retention has
// claimed what it made. It writes through app, the request's. An ID that
// isn't the person's own import into this workspace is answered not found
// (retryOwnedStoredJob), with nothing of the job it names read or changed
// first (2026-11-26: an unknown ID reached the left-out step, which read the
// job of any owner and answered its missing row as a 500 "Couldn't restart
// the import.").
func (r *durableJobRunner) retryNotionImport(ctx context.Context, app core.App, jobID, ownerID, workspaceID string) (storedJobControlOutcome, error) {
	if err := carryNotionImportJobOnConnection(ctx, app, jobID, ownerID, workspaceID); err != nil {
		return storedJobControlOutcome{}, err
	}
	outcome, err := r.retryOwnedStoredJob(
		app, jobID, ownerID, notionImportJobKind, workspaceID,
		func(txApp core.App) error {
			if _, authorizeErr := authorizeWorkspace(txApp, ownerID, workspaceID, permissionWorkspaceWrite); authorizeErr != nil {
				return authorizeErr
			}
			job, loadErr := loadStoredJob(txApp, jobID, ownerID)
			if errors.Is(loadErr, sql.ErrNoRows) || loadErr == nil && !ownedJobMatches(job, notionImportJobKind, workspaceID) {
				return nil
			}
			if loadErr != nil {
				return loadErr
			}
			claimed, claimErr := notionImportRetentionOwnsJob(ctx, txApp, jobID)
			if claimErr != nil {
				return claimErr
			}
			if claimed {
				return errNotionImportRetentionClaimed
			}
			// What it left out and hasn't passed yet is tried again
			// (notion_import_left_out.go).
			return retryNotionImportLeftOut(txApp, jobID)
		},
		nil,
	)
	if errors.Is(err, errNotionImportRetentionClaimed) {
		outcome.unavailable = true
		err = nil
	}
	return outcome, err
}

// notionMigrationRunEnd is when a migration's last run ended, its report
// showing a stop only while the run is stopped: trying again takes it away.
// A run's time said — the first, or its later end and why — is said only
// while it goes (2026-11-22: a done move's status still said "예상보다
// 길어지고 있습니다 … 그동안 다른 일을 하셔도 됩니다", which an agent could pass
// on as it was); what the stages took stays, for the run again's estimate.
func notionMigrationRunEnd(stored storedNotionMigration, report *notionMigrationRunReport) int64 {
	if stored.RunState != notionMigrationRunFailed {
		report.Failure = nil
	}
	if stored.RunState != notionMigrationRunRunning && report.Estimate != nil {
		ended := *report.Estimate
		ended.Say, ended.Reason, ended.ChangedAt = "", "", 0
		report.Estimate = &ended
	}
	return notionMigrationRunEndedAt(stored, *report)
}

// notionMigrationRunEndedAt is when a migration's last run ended, done or
// stopped; 0 while one runs or before any. A plan saved since moves the
// migration's updated time and not this (2026-11-13: the 완료됨 row's
// "완료 · 시각" followed an agent's plan change). A run done before its
// report kept the time ended when its job last changed.
func notionMigrationRunEndedAt(stored storedNotionMigration, report notionMigrationRunReport) int64 {
	switch stored.RunState {
	case notionMigrationRunDone:
		return cmp.Or(report.EndedAt, stored.JobChanged, stored.UpdatedAt)
	case notionMigrationRunFailed:
		return cmp.Or(stored.JobChanged, stored.RunStarted, stored.UpdatedAt)
	}
	return 0
}
