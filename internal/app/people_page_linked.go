package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// A person's page keeps the access People gives: its share window offers to
// lower what everyone in the workspace reads it with ("초대받은 사용자만",
// 제거) under a note that its access comes from People, and asks "변경 후
// 연결 해제", yet the page never unlinks — Notion's shows the unlinked band
// for a moment and goes back to "〈ws〉의 모든 사용자 — 읽기 허용" with no word
// (seen 2026-11-16 in a free workspace in Chrome at 1440, two tabs, three
// times: a restricted member's page and the owner's own; a Business plan's
// was not measured). So every change that would unlink one
// (cutPageShareInheritance) is refused here, and the window reads its sharing
// again as it was. A person's page lowered before keeps "설정 복원"
// (people_page_relink.go).
var errPersonPageShareLinked = errors.New(errorCodePublicMutationPersonPageLinked)

// refusePersonPageUnlink refuses unlinking a person's page, a row of People's
// database.
func refusePersonPageUnlink(app core.App, page *core.Record) error {
	if !isDatabaseRowPageRecord(page) {
		return nil
	}
	if _, _, people, err := peopleDatabasePageOf(app, page); err != nil || !people {
		return err
	}
	return errPersonPageShareLinked
}

// personPageRefusal answers a sharing change a person's page refuses: one
// that would unlink it, or a share with a restricted member
// (people_page_invites.go).
func personPageRefusal(event *core.RequestEvent, err error) error {
	if errors.Is(err, errPersonPageShareLinked) {
		return apiError(event, http.StatusConflict, errorCodePublicMutationPersonPageLinked,
			"A person's page keeps the access People gives everyone in the workspace; it doesn't unlink from it.")
	}
	return apiError(event, http.StatusForbidden, errorCodePublicMutationPersonPageRestricted, "A person's page isn't shared with the workspace's restricted members.")
}
