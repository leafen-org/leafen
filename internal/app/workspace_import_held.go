package app

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An import that meets the page it goes under or into held for a while by
// another operation — that page, or one above it, turning into a wiki or
// back, its blocks being moved or copied, its copy (pageHeldForMember) —
// waits and goes on once that is done, as its publishing step does: it tries
// again from where it was after durableJobReadyDelay, failing nothing
// meanwhile. A workspace moved to another server never takes it again, so an
// import there ends at once.
//
// A batch's documents and pages wait the same way. Those not begun when the
// hold has lasted past workspaceImportHeldWait end in words saying which
// operation held their place (workspaceImportHeldWords), and choosing the
// same files again brings them in; a document already going in keeps
// waiting, since what of it is in could only be taken out again once the
// page takes changes, which is when it would go on anyway.

const (
	workspaceImportHeldWait = 10 * time.Minute
	// A hold not met again for this long has ended: one met after it is a
	// new wait.
	workspaceImportHeldGap = 30 * time.Second
)

// workspaceImportHeldError is an import refused at a page an operation holds
// for a while: a conflict, in the words a held page is told in
// (pageBusyMessage).
type workspaceImportHeldError struct {
	*graphRequestError
	reason string
}

func (err *workspaceImportHeldError) Unwrap() error { return err.graphRequestError }

// workspaceImportParentHeld is a page to import under or into refused while
// an operation holds it.
func workspaceImportParentHeld(reason string) error {
	return &workspaceImportHeldError{
		graphRequestError: &graphRequestError{status: http.StatusConflict, code: errorCodePageCreateParentConflict, message: pageBusyMessage(reason)},
		reason:            reason,
	}
}

// workspaceImportHeldReason is the operation holding the page an import's
// step was refused at: one refused as held, or as not the importer's to
// change at one of these pages while an operation holds it (a held page's
// body refuses every writer, authorizePageBody). "" for any other refusal.
func workspaceImportHeldReason(app core.App, ownerID string, err error, pageIDs ...string) string {
	held := &workspaceImportHeldError{}
	if errors.As(err, &held) {
		return held.reason
	}
	if reason := pageHeldRefusal(err); reason != "" {
		return reason
	}
	if !isPermissionDenied(err) {
		return ""
	}
	for _, pageID := range pageIDs {
		if pageID == "" {
			continue
		}
		if busy := pageHeldForMember(app, ownerID, pageID); busy != "" {
			return busy
		}
	}
	return ""
}

// workspaceImportRefusedForGood reports whether an import's step was refused
// in a way that doesn't clear by itself: the page it goes into gone, in the
// trash or no longer the importer's to change, a workspace moved away, or
// the import no longer in the state the step needs. A failure of the store
// or the server isn't, and a publishing step keeps its finished work and
// tries it again (2026-11-19: publishing tried every refusal again every
// 250 ms, so an import whose page's edit right was taken away never ended
// and kept its turn). A page held for a while is waited for before this.
func workspaceImportRefusedForGood(err error) bool {
	if isPermissionDenied(err) || errors.Is(err, sql.ErrNoRows) {
		return true
	}
	failure := &workspaceImportFailureError{}
	if errors.As(err, &failure) {
		switch failure.code {
		case errorCodeWorkspaceImportStorageUnavailable, errorCodeWorkspaceImportDatabaseBusy, errorCodeWorkspaceImportUnavailable:
		default:
			return true
		}
	}
	refusal := &graphRequestError{}
	return errors.As(err, &refusal) && refusal.status >= 400 && refusal.status < 500
}

// workspaceImportPageRefused answers a request to import into a page refused
// as not the importer's (err): while an operation holds the page, as held,
// which takes the import once that is done.
func workspaceImportPageRefused(event *core.RequestEvent, principalID, pageID string, err error, stage string) error {
	if isPermissionDenied(err) {
		if busy := pageHeldForMember(event.App, principalID, pageID); busy != "" {
			return workspaceImportAPIError(event, workspaceImportParentHeld(busy), stage)
		}
	}
	return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to import.")
}

// workspaceImportHeldWaits reports whether a place refused with err is held
// by an operation that ends, which an import waits for.
func workspaceImportHeldWaits(err error) bool {
	held := &workspaceImportHeldError{}
	return errors.As(err, &held) && held.reason != pageBusyWorkspaceMoved
}

// waitWorkspaceImportHeld has an import whose step met its page held try that
// step again a little later, its phase, attempts and error kept, and reports
// whether it did. Its cleanup keeps its own slower retry.
func (r *durableJobRunner) waitWorkspaceImportHeld(job storedJob, reference string, applyErr error) (bool, error) {
	payload, err := decodeWorkspaceImportPayload(job)
	if err != nil || payload.Phase == workspaceImportPhaseCleanup || payload.Phase == workspaceImportPhasePublishCleanup {
		return false, nil
	}
	reason := workspaceImportHeldReason(r.app, job.Owner, applyErr, workspaceImportPrimaryRootPageID(payload), payload.ParentPageID)
	if reason == "" || reason == pageBusyWorkspaceMoved {
		return false, nil
	}
	logDurableJobScheduledAgain(
		r.app, slog.LevelDebug, errorCodePageCreateParentConflict, reference, job.ID,
		"workspace-import.apply", "held-wait", reason, applyErr,
	)
	now := r.clock.now().UnixMilli()
	notBefore := now + durableJobReadyDelay.Milliseconds()
	_, err = r.app.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[not_before]] = {:notBefore}, [[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued' AND [[not_before]] < {:notBefore}
	`).Bind(dbx.Params{"id": job.ID, "notBefore": notBefore, "updated": now}).Execute()
	return true, err
}

// workspaceImportBatchHold is what a batch's turn met of a place held for a
// while: since when the batch has waited, and the words for the rows the
// hold ended.
type workspaceImportBatchHold struct {
	since, seen, now int64
	locale           string
	words            map[int]string
}

func newWorkspaceImportBatchHold(payload workspaceImportJobPayload, now time.Time) *workspaceImportBatchHold {
	return &workspaceImportBatchHold{
		since: payload.HeldSince, seen: payload.HeldSeen, now: now.UnixMilli(),
		locale: payload.Locale, words: map[int]string{},
	}
}

// waits reports whether the batch waits for the operation holding the place
// of the row at position ("" for none); when the hold has lasted past
// workspaceImportHeldWait, or never ends, it doesn't, and keeps the row's
// words.
func (hold *workspaceImportBatchHold) waits(position int, reason string) bool {
	if reason == "" {
		return false
	}
	if hold.seen == 0 || hold.now-hold.seen > workspaceImportHeldGap.Milliseconds() {
		hold.since = hold.now
	}
	hold.seen = hold.now
	if reason != pageBusyWorkspaceMoved && hold.now-hold.since <= workspaceImportHeldWait.Milliseconds() {
		return true
	}
	hold.words[position] = workspaceImportHeldWords(hold.locale, reason)
	return false
}

// keep writes when the batch began waiting and last met a hold.
func (hold *workspaceImportBatchHold) keep(payload *workspaceImportJobPayload) {
	payload.HeldSince, payload.HeldSeen = hold.since, hold.seen
}

// workspaceImportHeldWords says, in the batch's language, which operation
// held a file's place past the wait.
func workspaceImportHeldWords(locale, reason string) string {
	text := workspaceImportHeldText[workspaceImportLocaleEnglish]
	if locale == workspaceImportLocaleKorean {
		text = workspaceImportHeldText[workspaceImportLocaleKorean]
	}
	if words, ok := text[reason]; ok {
		return words
	}
	return text[""]
}

var workspaceImportHeldText = map[string]map[string]string{
	workspaceImportLocaleKorean: {
		pageBusyWiki:             "가져올 곳의 페이지를 위키로 바꾸거나 되돌리는 작업이 끝나지 않아 가져오지 못했습니다. 끝난 뒤 다시 가져오세요.",
		pageBusyMoving:           "가져올 곳의 페이지에서 블록을 옮기는 작업이 끝나지 않아 가져오지 못했습니다. 끝난 뒤 다시 가져오세요.",
		pageBusyCopying:          "가져올 곳의 페이지를 복제하는 작업이 끝나지 않아 가져오지 못했습니다. 끝난 뒤 다시 가져오세요.",
		pageBusyWorkspaceSending: "워크스페이스를 다른 서버로 옮기는 작업이 끝나지 않아 가져오지 못했습니다. 옮기기가 멈추면 다시 가져오세요.",
		pageBusyWorkspaceMoved:   "이 워크스페이스는 다른 서버로 옮겨져 여기로는 가져올 수 없습니다.",
		pageBusyWorkspaceMoving:  "가져올 곳의 페이지를 다른 워크스페이스로 옮기는 작업이 끝나지 않아 가져오지 못했습니다. 끝난 뒤 다시 가져오세요.",
		"":                       "가져올 곳의 페이지에서 다른 작업이 끝나지 않아 가져오지 못했습니다. 끝난 뒤 다시 가져오세요.",
	},
	workspaceImportLocaleEnglish: {
		pageBusyWiki:             "The page this goes into is still turning into a wiki or back, so it wasn’t imported. Import it again once that’s done.",
		pageBusyMoving:           "Blocks of the page this goes into are still being moved, so it wasn’t imported. Import it again once that’s done.",
		pageBusyCopying:          "The page this goes into is still being copied, so it wasn’t imported. Import it again once that’s done.",
		pageBusyWorkspaceSending: "This workspace is still moving to another server, so this wasn’t imported. Import it again if the move stops.",
		pageBusyWorkspaceMoved:   "This workspace moved to another server, so nothing can be imported into it here.",
		pageBusyWorkspaceMoving:  "The page this goes into is still being moved to another workspace, so it wasn’t imported. Import it again once that’s done.",
		"":                       "Another change to the page this goes into is still under way, so it wasn’t imported. Import it again once that’s done.",
	},
}
