package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// What a lock's refusal of a change in the app says, made and answered here
// alone: which lock refused it — a page's (page_lock_policy.go) or a
// database's own (database_container_lock.go, a full-page database's or a
// wiki's page lock among them) — and, for a database, whether the person
// asking can unlock it. A page's lock refuses only someone who may edit the
// page, who can unlock it. A database's lock also holds its properties for
// someone a row page alone is shared with, who can't: Notion shows them no
// lock at all, its options picker only without "생성" (seen 2026-11-18 in
// Chrome, two tabs: a guest with edit access on the row page and view access
// on the page holding a locked inline database saw no "잠금" on the row page,
// its "⋯" or the database's bar), while to someone who may edit it the bar's
// "잠금" says who locked it and "잠금을 해제하려면 클릭하세요." The refusal
// tells either that much: to unlock it, or that someone who can edit the
// database can.

const (
	errorCodePageLocked = "LEAFEN_PAGE_LOCKED"
	// errorCodeDatabaseLocked refuses someone who may unlock the database.
	errorCodeDatabaseLocked = "LEAFEN_DATABASE_LOCKED"
	// errorCodeDatabaseLockedNotUnlockable refuses someone who can't.
	errorCodeDatabaseLockedNotUnlockable = "LEAFEN_DATABASE_LOCKED_NOT_UNLOCKABLE"
)

func pageLockedError() error {
	return &graphRequestError{
		status: http.StatusLocked, code: errorCodePageLocked,
		message: "The page is locked. Unlock it to change it.",
	}
}

func databaseLockedError(unlockable bool) error {
	if unlockable {
		return &graphRequestError{
			status: http.StatusLocked, code: errorCodeDatabaseLocked,
			message: "The database is locked. Unlock it to change its properties and views.",
		}
	}
	return &graphRequestError{
		status: http.StatusLocked, code: errorCodeDatabaseLockedNotUnlockable,
		message: "The database is locked. Someone who can edit the database can unlock it.",
	}
}

// databaseLockedRefusal refuses a change a database's own lock holds, as
// whoever asks may meet it: they unlock it where they may edit the page
// that holds it, as its "잠금" — the block's for an inline database, the
// page's for a full-page one — does.
func databaseLockedRefusal(app core.App, principalID, containerPageID string) error {
	_, err := authorizePage(app, principalID, containerPageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return databaseLockedError(false)
	}
	if err != nil {
		return err
	}
	return databaseLockedError(true)
}

// isPageLockedError reports a refusal by a page's lock.
func isPageLockedError(err error) bool {
	var requestError *graphRequestError
	return errors.As(err, &requestError) && requestError.code == errorCodePageLocked
}

// isDatabaseLockedError reports a refusal by a database's own lock.
func isDatabaseLockedError(err error) bool {
	var requestError *graphRequestError
	return errors.As(err, &requestError) &&
		(requestError.code == errorCodeDatabaseLocked || requestError.code == errorCodeDatabaseLockedNotUnlockable)
}

// isLockRefusal reports a refusal by a page's or a database's lock.
func isLockRefusal(err error) bool {
	return isPageLockedError(err) || isDatabaseLockedError(err)
}

// answerLockRefusal answers a lock's refusal as it was made.
func answerLockRefusal(event *core.RequestEvent, err error) error {
	var requestError *graphRequestError
	if !errors.As(err, &requestError) {
		requestError = pageLockedError().(*graphRequestError)
	}
	return apiError(event, requestError.status, requestError.code, requestError.message)
}
