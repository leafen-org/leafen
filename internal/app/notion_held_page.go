package app

import (
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page an operation holds for a while — its blocks being moved, a wiki's
// change, its copy, its workspace moving (pageHeldForMember) — takes changes
// again once that is done. Notion has no such hold: its public
// API is answered as Notion answers a write that met another, 409
// conflict_error (notionConflictWhileSaving), which trying again gets past, as a database
// that stayed busy is (notionCompatibilityErrorWithData) — never as a page
// not found, nor as a request Leafen couldn't complete (2026-11-19: a parent
// turning into a wiki answered POST /v1/pages 404 object_not_found).

// notionPageHeld is the operation holding a page this connection reaches
// for a while, told to a member of the page's workspace (pageHeldForMember);
// "" for any other page, one being deleted for good included.
func notionPageHeld(app core.App, identity notionOAuthBearerIdentity, pageID string) string {
	page, err := findRecordRemembered(app, pagesCollection, pageID)
	if err != nil || page.GetString("workspace") != identity.WorkspaceID ||
		!notionOAuthIdentityAllowsPage(app, identity, pageAuthorization{Page: page}) {
		return ""
	}
	return pageHeldForMember(app, identity.PrincipalID, pageID)
}

// notionHeldAnswer says whether a refusal is about a page an operation holds:
// one that says so (pageHeldRefusal), or a page or block not found whose
// page — the one the request's address or body names (notionObjectAsked) —
// the connection reaches and an operation holds.
func notionHeldAnswer(event *core.RequestEvent, status int, code string, cause error) bool {
	if pageHeldRefusal(cause) != "" {
		return true
	}
	if status != http.StatusNotFound || code != notionErrorObjectNotFound {
		return false
	}
	asked := notionObjectAsked(event)
	if asked.Kind != "page" && asked.Kind != "block" {
		return false
	}
	identity, ok, err := notionPublicAPIBearerFromRequest(event)
	if err != nil || !ok {
		return false
	}
	pageID, err := resolveNotionParentPageRecordID(event.App, asked.ID)
	if err != nil && asked.Kind == "block" {
		// A block is held with the page it is on.
		if blockID, found := resolveNotionBlockID(event.App, asked.ID); found {
			err = event.App.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_blocks}} WHERE [[id]]={:id} LIMIT 1`).
				Bind(dbx.Params{"id": blockID}).Row(&pageID)
		}
	}
	return err == nil && pageID != "" && notionPageHeld(event.App, identity, pageID) != ""
}
