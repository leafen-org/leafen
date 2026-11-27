package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// An import that adds to a page someone already has — Markdown added to its
// body or put in place of it, a CSV's rows added to its database — writes
// into that page as the editor does: never while the page, or one above it,
// is in the trash, which Notion keeps read-only until it is restored
// (pageInTrashError; Notion's API refuses to add blocks to a page in the
// trash too, seen 2026-11-20), nor once it is deleted for good, behind its
// fence for its 30 days (page_deletion_hold.go). Its admission asks, each turn
// that writes into the page asks, and its publishing asks, so an import whose
// page went to the trash or was deleted for good on the way ends in words
// (workspaceImportRefusedForGood) and its cleanup takes out what it put in
// (2026-11-20: an append whose page went to the trash just before it
// published finished into it, 2001 blocks). An import that makes a new page
// asks the same of the place it asks for: the page it goes under
// (authorizeWorkspaceImportParent), the database whose row it becomes
// (authorizeWorkspaceImportDataSource) and a Notion move's destination
// (notionMigrationDestinationProblem). A new page whose place goes to the
// trash on the way is published at the top level, where it can still be
// seen (workspaceImportParentLost); a Notion move stops.

// authorizeWorkspaceImportTarget admits an import's turn to change the body
// of the page it adds to, in its workspace, while the page takes changes. Its
// importer could change the page when it was admitted, so a page refused
// since as not theirs is told as deleted for good or in the trash when it is.
func authorizeWorkspaceImportTarget(app core.App, ownerID, workspaceID, pageID string) (pageAuthorization, error) {
	return authorizeWorkspaceImportTargetFor(app, ownerID, workspaceID, pageID, true)
}

// admitWorkspaceImportTarget is authorizeWorkspaceImportTarget for asking to
// import: someone who may not change the page hears only that.
func admitWorkspaceImportTarget(app core.App, ownerID, workspaceID, pageID string) (pageAuthorization, error) {
	return authorizeWorkspaceImportTargetFor(app, ownerID, workspaceID, pageID, false)
}

func authorizeWorkspaceImportTargetFor(app core.App, ownerID, workspaceID, pageID string, admitted bool) (pageAuthorization, error) {
	authorization, err := authorizePageBody(app, ownerID, pageID, permissionWorkspaceWrite)
	if err == nil && authorization.Workspace.WorkspaceID != workspaceID {
		err = permissionDenied()
	}
	if err != nil && (!admitted || !isPermissionDenied(err)) {
		return pageAuthorization{}, err
	}
	if gone := workspaceImportTargetGone(app, pageID); gone != nil {
		return pageAuthorization{}, gone
	}
	return authorization, err
}

// workspaceImportTargetGone is the refusal of a page an import adds to that
// is deleted for good or in the trash, itself or under a page that is (a
// database page also with its database's page); nil while it takes changes.
// A deleted page is told as deleted, not as one the importer may not change.
func workspaceImportTargetGone(app core.App, pageID string) error {
	if fenced, err := pagePermanentDeleteFenceActive(app, pageID); err != nil {
		return err
	} else if fenced {
		return workspaceImportPageDeletedError()
	}
	row, err := isDatabaseRowPageRoom(app, pageID)
	if err != nil {
		return err
	}
	trashed := false
	if row {
		trashed, err = commentTargetInTrash(app, "", pageID)
	} else {
		trashed, err = pageInTrashPath(app, pageID)
	}
	if err != nil {
		return err
	}
	if trashed {
		return pageInTrashError()
	}
	return nil
}

// workspaceImportPageDeletedError refuses a change to a page deleted for
// good, in its 30 days or being deleted.
func workspaceImportPageDeletedError() error {
	return &graphRequestError{
		status: http.StatusNotFound, code: errorCodePageNotFound,
		message: "The page was deleted for good.",
	}
}

// workspaceImportTargetGoneRefusal is err's refusal of a page in the trash
// or deleted for good, when it is one.
func workspaceImportTargetGoneRefusal(err error) (*graphRequestError, bool) {
	refusal := &graphRequestError{}
	if errors.As(err, &refusal) && (refusal.code == errorCodePageInTrash || refusal.code == errorCodePageNotFound) {
		return refusal, true
	}
	return nil, false
}

// workspaceImportRefusalDetail is what an import refused at a turn says:
// where and what was wrong (workspaceImportFailureDetail), or, refused at
// its page in the trash or deleted for good, that, in the importer's
// language.
func workspaceImportRefusalDetail(app core.App, job storedJob, err error) string {
	if refusal, gone := workspaceImportTargetGoneRefusal(err); gone {
		return workspaceImportTargetGoneWords(app, job, refusal.code)
	}
	return workspaceImportFailureDetail(err)
}

func workspaceImportTargetGoneWords(app core.App, job storedJob, code string) string {
	locale := workspaceImportLocaleEnglish
	if payload, err := decodeWorkspaceImportPayload(job); err == nil && payload.Locale != "" {
		locale = payload.Locale
	} else if language, err := serverTextLanguage(app, job.Owner, nil); err == nil && language == accountLanguageKo {
		locale = workspaceImportLocaleKorean
	}
	text := workspaceImportTargetGoneText[workspaceImportLocaleEnglish]
	if locale == workspaceImportLocaleKorean {
		text = workspaceImportTargetGoneText[workspaceImportLocaleKorean]
	}
	return text[code]
}

var workspaceImportTargetGoneText = map[string]map[string]string{
	workspaceImportLocaleKorean: {
		errorCodePageInTrash:  "가져올 곳의 페이지가 휴지통으로 옮겨져 가져오기를 멈췄고, 가져오던 내용은 뺐습니다. 페이지를 복원한 뒤 다시 가져오세요.",
		errorCodePageNotFound: "가져올 곳의 페이지가 영구 삭제되어 가져오기를 멈췄습니다. 워크스페이스 소유자가 30일 안에 보안 → 데이터 보존에서 복원하면 가져오던 내용은 그때 빠지고, 휴지통에서 다시 복원한 뒤 가져올 수 있습니다.",
	},
	workspaceImportLocaleEnglish: {
		errorCodePageInTrash:  "The page this goes into was moved to Trash, so the import stopped and what it had added was taken out. Restore the page and import again.",
		errorCodePageNotFound: "The page this goes into was deleted for good, so the import stopped. If the workspace owner restores it within 30 days under Security → Data retention, what the import had added is taken out then, and the page can be restored from Trash and imported into again.",
	},
}
