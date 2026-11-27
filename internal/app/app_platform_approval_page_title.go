//go:build !leafen_noapps

package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An approval's field that names a page reads as the page's title, by
// whatever ID the app was given for it: the ID in the page's address (a
// UUID, with or without its dashes, or a page made before's 15
// characters), a page's public ID or a database page's row identity
// (resolveNotionPageRecordID) — 2026-11-16: the approvals app's "페이지"
// field read "bce87f90-…", the server naming only 15-character IDs. A page
// of the app's own workspace reads so for anyone (the app shows it on its
// screen anyway; "요청 번호 0baf…" told the person nothing, 2026-11-05);
// any other as the person deciding reads it — a mention in its title they
// can't open in marks, and a page they may not open as one closed mark,
// which the screen says as a mention closed to them ("사용 권한 없음").
// One with no title is empty: the reader's screen names it untitled in
// their language (2026-11-15: an untitled draft read "Untitled" on a
// Korean screen).

// appApprovalPageTitle is the title of the page or database row page an
// input's ID names, as readerID reads it; false for an ID naming none.
func appApprovalPageTitle(app core.App, id, appWorkspaceID, readerID string) (string, bool) {
	if id == "" || len(id) > 64 || strings.ContainsAny(id, " \n") {
		return "", false
	}
	if appWorkspaceID != "" {
		var count int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_rows}} WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspace}`).
			Bind(dbx.Params{"id": id, "workspace": appWorkspaceID}).Row(&count); err == nil && count > 0 {
			return databaseRowPageTitle(app, id), true
		}
	}
	pageID, err := resolveNotionPageRecordID(app, id)
	if err != nil {
		return "", false
	}
	title, workspace, found := pageMentionTargetNow(app, pageID)
	switch {
	case !found:
		return "", false
	case appWorkspaceID != "" && workspace == appWorkspaceID:
		return title, true
	}
	if readerID == "" {
		return string(closedMentionMark), true
	}
	if opens, err := openablePageIDs(app, readerID, []string{pageID}); err != nil || !opens[pageID] {
		return string(closedMentionMark), true
	}
	return rowTitleAsRead(app, readerID, pageID, title), true
}

// appApprovalPageWords is a page field's value in words, for what shows it
// as words (an agent's question to the person): a page closed to them
// "사용 권한 없음", one with no title "제목 없음", a mention in its title
// they can't open "@찾을 수 없음".
func appApprovalPageWords(value, language string) string {
	switch {
	case value == "":
		return mcpWordsUntitled.in(language)
	case isClosedMentionText(value):
		return closedMentionLabel(language)
	}
	return closedMentionsInTitle(value, language)
}
