package app

import (
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A database a person starts on an empty page takes the page's place, and
// the page as it was goes to the trash, as Notion's start makes a new
// database where the page stood and moves the page to the trash (seen
// 2026-11-18 in Chrome: a template on a titled page and on an untitled one,
// an existing source, a blank database; the page in_trash through the API,
// back from the trash whole). Here the page keeps its ID and becomes the
// database (its address, links and place go on as the database's), so the
// page as it was goes to the trash as a page of its own: its title, icon,
// cover, look and place, its empty body none. Restored, it comes back under
// its parent as any page does (child_page_block_lifecycle.go). The add
// window's and a line's new database pages, made as databases from the
// start, leave nothing in the trash, as Notion's do.

// databaseStartPageFields are the page's own fields its copy in the trash keeps.
var databaseStartPageFields = []string{
	"owner", "workspace", "teamspace_id", "private_owner_id", "parent_id", "parent_type", "position",
	"title", "title_spans", "icon", "icon_type", "cover", "cover_position",
	"font", "small_text", "full_width", "backlinks_display", "page_comments_display",
	"table_of_contents_display", "inline_comments_display",
}

// trashPageAsItWas puts the page as it was before its database start in the
// trash, in the same write as the start.
func trashPageAsItWas(app core.App, authorization pageAuthorization) error {
	page := authorization.Page
	pages, err := app.FindCachedCollectionByNameOrId(pagesCollection)
	if err != nil {
		return err
	}
	cursor, err := nextWorkspaceUserStateCursor(app, authorization.Workspace.OwnerID)
	if err != nil {
		return err
	}
	actorID, now := authorization.Workspace.PrincipalID, time.Now().UTC()
	copied := core.NewRecord(pages)
	copied.Id = newPageID()
	initializePageStyleMetadata(copied)
	initializePageCustomizationMetadata(copied)
	for _, field := range databaseStartPageFields {
		if pages.Fields.GetByName(field) != nil {
			copied.Set(field, page.Get(field))
		}
	}
	copied.Set("content", "")
	copied.Set("revision", 0)
	copied.Set("cursor", cursor)
	copied.Set("kind", pageKindPage)
	applyPageTrashState(copied, true, now.Format(time.RFC3339Nano))
	setNewPageCustodianProvenance(copied, actorID)
	if err := app.Save(copied); err != nil {
		return err
	}
	workspaceID := page.GetString("workspace")
	// An uploaded icon or cover is the copy's too.
	if err := replaceProductFilePageReferences(app, workspaceID, copied.Id,
		copied.GetString("icon"), copied.GetString("icon_type"), copied.GetString("cover")); err != nil {
		return err
	}
	if err := recordNotionAdminLegalHoldPageInteraction(app, pageCreatedInteraction(
		authorization.Workspace.Organization.OrganizationID, workspaceID, copied.Id, actorID, now.UnixMilli(),
	)); err != nil {
		return err
	}
	// The trash's search finds it by its title.
	return markSearchDirty(app, copied)
}
