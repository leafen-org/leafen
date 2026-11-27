package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The block routes find what an ID names in one place, before any route
// reads it: a page, a database page, a database template's page or a block.
// A database page answers as a page does, by its own ID, as Notion's API
// takes it (seen 2026-11-23 twice, Notion-Version 2026-03-11, pages of a
// full-page and an inline database alike): read as a child_page block whose
// parent is its data source, deleted to the trash as that block, a change
// to it told to go to the pages endpoint, and in the trash refused as a
// page is. A database's own ID is first turned into its block's
// (notionBlockIDForDatabase). Listing children looks for a page body
// instead, whichever page holds it (serveNotionBlockChildrenFollowing).

type notionBlockTarget uint8

const (
	notionBlockTargetBlock notionBlockTarget = iota
	notionBlockTargetPage
	notionBlockTargetRowPage
	// notionBlockTargetTemplate is a database template's page, read as a
	// block (notion_database_template_block.go) and otherwise not found as
	// one.
	notionBlockTargetTemplate
)

func serveNotionBlockCompatibility(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) (bool, error) {
	if !strings.HasPrefix(event.Request.URL.Path, notionAPIBlocksPath+"/") {
		return false, nil
	}
	segments := strings.Split(strings.TrimPrefix(event.Request.URL.Path, notionAPIBlocksPath+"/"), "/")
	if segments[0] == "" || len(segments) > 2 || len(segments) == 2 && segments[1] != "children" {
		return false, nil
	}
	method, children := event.Request.Method, len(segments) == 2
	if method != http.MethodGet && method != http.MethodPatch && (method != http.MethodDelete || children) {
		return false, nil
	}
	// A database's ID names its block too.
	id := notionBlockIDForDatabase(event.App, segments[0])
	if method == http.MethodGet && children {
		return true, serveNotionBlockChildren(event, id)
	}
	target, err := notionBlockTargetOf(event.App, id)
	if err != nil {
		return true, notionBlockProjectionError(event, err)
	}
	switch {
	case method == http.MethodGet:
		return true, serveNotionBlockRetrieve(event, id, target)
	case method == http.MethodPatch && children:
		return true, serveNotionBlockChildrenAppend(event, ygo, id, target)
	case method == http.MethodPatch:
		return true, serveNotionBlockUpdate(event, ygo, id, target)
	default:
		return true, serveNotionBlockDelete(event, ygo, id, target)
	}
}

// notionBlockTargetOf is what an ID given to the block routes names. A page
// that is no database page now is a page whatever ID it keeps.
func notionBlockTargetOf(app core.App, id string) (notionBlockTarget, error) {
	if notionPublicIDNamesPage(app, id) {
		return notionBlockTargetPage, nil
	}
	named, err := resolveNotionObjectIdentity(app, notionObjectIdentityRowPage, strings.TrimSpace(id))
	if errors.Is(err, errInvalidNotionObjectIdentity) {
		return notionBlockTargetBlock, nil
	}
	if err != nil {
		return notionBlockTargetBlock, err
	}
	if template, err := isDatabaseTemplatePageID(app, named.InternalID); err != nil || template {
		return notionBlockTargetTemplate, err
	}
	return notionBlockTargetRowPage, nil
}

// serveNotionPageBlockUpdate answers a change sent to the blocks endpoint
// for a page or a database page, as Notion's API does (seen 2026-11-10, and
// 2026-11-23 for database pages and pages under one in the trash): one that
// may write it is told to send it to the pages endpoint, whatever the body,
// once the trash refuses it — taking it out of the trash only while what
// holds it is in the trash too (notion_trash_refusal.go).
func serveNotionPageBlockUpdate(event *core.RequestEvent, publicID string, target notionBlockTarget) error {
	pageID := ""
	if target == notionBlockTargetRowPage {
		row, authorized, err := authorizeNotionDatabaseRowPageState(
			event, publicID, "pages:write", permissionWorkspaceWrite, notionRowAnyTrash,
		)
		if err != nil || !authorized {
			return err
		}
		pageID = row.Authority.RowID
	} else {
		page, authorized, err := authorizeNotionPage(event, publicID, "pages:write", permissionWorkspaceWrite)
		if err != nil || !authorized {
			return err
		}
		pageID = page.Page.Page.Id
	}
	body := map[string]json.RawMessage{}
	restoring := bindNotionStrictJSON(event, &body) == nil && notionBlockRestoreOnlyRequest(body)
	if refused, err := refuseNotionTrashedEdit(event, pageID, true, restoring, notionTrashedEdit); refused {
		return err
	}
	return notionPageValidationError(event, "A page isn't changed through the blocks endpoint: send PATCH /v1/pages/:page_id instead")
}
