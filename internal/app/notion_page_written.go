package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// A page a Notion-compatible write puts under a wiki's page, or under a page
// of a wiki — made there, moved there, or taken out of the trash there —
// becomes the wiki's page, as the app's own writes make it
// (wiki_page_adoption.go), and the write answers it
// as it reads after that: a database's page as the database page it is now
// (notionDatabaseRowPageObject), as Notion's API answers a page made under a
// page of a wiki (seen 2026-11-19: 200, the page with the wiki's properties)
// and a page moved to a wiki's own page (200, the wiki's page). Read as an
// ordinary page, its parent, the wiki's data source, named no page, and the
// write that had made the page answered 500 (2026-11-19).

// settleNotionPageInWiki makes a page a write put somewhere the wiki's when
// it sits under one; one outside every wiki stays as it is.
func settleNotionPageInWiki(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity, pageID string,
) error {
	if ygo == nil || pageID == "" {
		return nil
	}
	_, err := ygo.adoptPageTreeIntoWiki(event.Request.Context(), identity.PrincipalID, pageID)
	return err
}

// recordConnectionWrittenPage notes a page a connection made, or edited when
// its edit mark moved from before (the page's own), as the page or the
// database page it is now.
func recordConnectionWrittenPage(
	app core.App, identity notionOAuthBearerIdentity, page *core.Record, created bool, before string,
) error {
	if page != nil && page.GetString("data_source_id") != "" {
		return recordConnectionStoredMark(app, identity, connectionAuthoredRow, page.Id, created, "")
	}
	return recordConnectionPage(app, identity, page, created, before)
}

// notionWrittenPageObject is a page a write made or changed as the API reads
// it now: a database's page by its database page's object, any other as a
// page. Its errors are answered by notionDatabaseRowPageReadError.
func notionWrittenPageObject(
	event *core.RequestEvent, identity notionOAuthBearerIdentity, page *core.Record,
) (map[string]any, error) {
	if page.GetString("data_source_id") != "" {
		return loadNotionDatabaseRowPageResponse(event, identity, page.Id, permissionWorkspaceRead)
	}
	return notionPageObject(event.App, event.Request, identity, page)
}

// notionPageCreationRefused is an admission's refusal of a page the API
// makes, said as its other refusals are: a parent this connection may not
// add to now is not found, as authorizeNotionParentPage answers it before;
// one that changed in between (a page taking it held, in the trash, or no
// longer a page that takes pages) is a conflict that trying again settles.
// It is nil for a page made.
func notionPageCreationRefused(status pageCreationAdmissionStatus) error {
	switch status {
	case pageCreationApplied:
		return nil
	case pageCreationWorkspaceMissing, pageCreationParentDenied:
		return graphNotFoundError()
	}
	return graphConflictError(errorCodeGraphPageCursorConflict, "The page parent changed before creation completed.")
}
