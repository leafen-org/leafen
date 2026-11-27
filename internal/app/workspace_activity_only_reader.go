package app

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

// An update line's filled lock (OnlyReader) is Notion's mark of a page only
// its reader opens now — the lock its share button wears
// (populatePublicMutationLinkAccess) — read when the line is shown, not when
// it was written: a private page's lines lost the lock, the earlier ones
// too, once everyone in the workspace was given it, and had it again once
// that was taken back, the page above, still private, keeping its own
// (seen 2026-11-17 03:2x in the free test workspace's '모든 워크스페이스
// 업데이트'); and pages made private and later shared or moved out showed
// none on any line, their "개인 페이지로 생성" among them (seen the same
// night in another reading). Where the page is now names it in those
// sentences (Scope, workspace_activity_page_scope.go).

// workspaceActivityOnlyReader keeps, for one read of the feed, whether only
// the reader opens each page it asked about.
type workspaceActivityOnlyReader map[string]bool

// of is whether only the reader opens the page now: one of their private
// pages no one else is given, on it or above it, and no live web link or
// published site opens.
func (known workspaceActivityOnlyReader) of(app core.App, readerID string, page *pageAuthorization) (bool, error) {
	if page == nil || page.Page.GetString(privatePageOwnerField) != readerID {
		return false, nil
	}
	if only, found := known[page.Page.Id]; found {
		return only, nil
	}
	only, err := pageOnlyReaderOpens(context.Background(), app, *page)
	if err != nil {
		return false, err
	}
	if known != nil {
		known[page.Page.Id] = only
	}
	return only, nil
}

// pageOnlyReaderOpens is whether only the authorized reader opens the page,
// by the rule its share button's lock follows.
func pageOnlyReaderOpens(ctx context.Context, app core.App, authorization pageAuthorization) (bool, error) {
	workspaceID := authorization.Page.GetString("workspace")
	chain, err := loadPageAccessAncestorIDs(app, workspaceID, authorization.Page.Id)
	if err != nil {
		return false, err
	}
	state := publicMutationState{WorkspaceID: workspaceID}
	if err := populatePublicMutationLinkAccess(ctx, app, authorization, chain, &state); err != nil {
		return false, err
	}
	return state.OnlyReaderOpens, nil
}
