package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// Notion's API refuses a change to what is in the trash, and a change under
// it (seen 2026-11-11 and again 2026-11-23, Notion-Version 2026-03-11, and
// 2025-09-03's archived the same). A page, database or block that is itself
// in the trash takes only being taken out of it, with the rest of its update
// beside it; anything else — the same trash again, a title, an icon, a lock,
// an empty change, children added, a deletion — is refused as an archived
// block, while taking out what is out of the trash leaves it as it is. A
// page, database or block under a page, a block or a database in the trash
// takes nothing, being taken out of the trash neither: it is refused as
// having an archived ancestor. Reads still find them (notion_trash_reads.go).
// A move and a Markdown change say it in their own words, and a data source
// in the trash or under a page in it isn't found for a change, the same
// trash again and an empty one too (notion_data_source_trash_turn.go). Each
// write asks notionTrashedEditMessage once it knows the connection reaches
// what it changes, before anything else of the change.

const notionArchivedAncestorMessage = "A page under an archived block can't be edited until that block is restored."

// notionTrashedEditWords are how one kind of change says it is refused:
// to what is itself in the trash, and to what is under a page in the
// trash, given the public ID of what is changed.
type notionTrashedEditWords struct {
	itself   func(id string) string
	ancestor func(id string) string
}

var (
	notionTrashedEdit = notionTrashedEditWords{
		itself:   func(string) string { return notionArchivedCommentMessage },
		ancestor: func(string) string { return notionArchivedAncestorMessage },
	}
	notionTrashedMove = notionTrashedEditWords{
		itself:   func(id string) string { return "Object " + id + " is in the trash, so it can't move" },
		ancestor: func(id string) string { return "Object " + id + " is in the trash, so it can't move" },
	}
	notionTrashedMarkdownEdit = notionTrashedEditWords{
		itself: func(string) string {
			return "The page is in the trash; restore it to change it."
		},
		ancestor: func(string) string {
			return "A page this one is under is in the trash; restore that page to change this one."
		},
	}
)

// notionTrashedEditMessage is why a change to pageID is refused for the
// trash, or "" when it is not: itself is a change to the page (a database
// page's too), not to a block in it, and restoring one that takes the page
// out of the trash, which the page itself in the trash takes.
func notionTrashedEditMessage(
	app core.App, pageID string, itself, restoring bool, words notionTrashedEditWords,
) (string, error) {
	place, err := notionPageTrashPlace(app, pageID)
	if err != nil || !place.inTrash() {
		return "", err
	}
	id, err := notionPagePublicID(app, pageID)
	if err != nil {
		return "", err
	}
	if itself && place == notionInTrashItself {
		if restoring {
			return "", nil
		}
		return words.itself(id), nil
	}
	return words.ancestor(id), nil
}

// refuseNotionTrashedEdit answers a change notionTrashedEditMessage refuses.
func refuseNotionTrashedEdit(
	event *core.RequestEvent, pageID string, itself, restoring bool, words notionTrashedEditWords,
) (bool, error) {
	message, err := notionTrashedEditMessage(event.App, pageID, itself, restoring, words)
	if err != nil {
		return true, notionRequestNotCompleted(event, err)
	}
	if message == "" {
		return false, nil
	}
	return true, notionPageValidationError(event, message)
}

// notionConnectionReadsPage is whether a connection reads a page, which it
// must before it is told the page is in the trash.
func notionConnectionReadsPage(app core.App, identity notionOAuthBearerIdentity, pageID string) (bool, error) {
	page, err := authorizePageBody(app, identity.PrincipalID, pageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		// A database page with its database in the trash is read through the
		// page holding it (notion_trash_reads.go).
		_, reads, err := authorizeNotionRowInTrashedSource(app, identity, pageID, permissionWorkspaceRead)
		return reads, err
	}
	if err != nil {
		return false, err
	}
	return page.Workspace.WorkspaceID == identity.WorkspaceID && notionOAuthIdentityAllowsPage(app, identity, page), nil
}

// notionTrashedBodyEdit is why a write to a page's body is refused for the
// trash, before the write is authorized (a body in the trash isn't
// writable): itself is the page named as what children are added to, else
// it is a block in the page. "" is a page the connection doesn't read, or
// one not in the trash.
func notionTrashedBodyEdit(
	app core.App, identity notionOAuthBearerIdentity, pageID string, itself bool, words notionTrashedEditWords,
) (string, error) {
	reads, err := notionConnectionReadsPage(app, identity, pageID)
	if err != nil || !reads {
		return "", err
	}
	return notionTrashedEditMessage(app, pageID, itself, false, words)
}

// refuseNotionTrashedBlockEdit answers a change to a block in the trash
// whose page the connection reads (notionTrashedBlockMessage), or as having
// an archived ancestor while its page is in the trash too. A block not in
// the trash, or out of reach, isn't answered here.
func refuseNotionTrashedBlockEdit(
	event *core.RequestEvent, identity notionOAuthBearerIdentity, blockID string,
) (bool, error) {
	read, found, err := loadNotionTrashedBlockRead(event.App, identity, blockID)
	message := ""
	if err == nil && found {
		message = notionTrashedBlockMessage(read)
		ancestor := ""
		ancestor, err = notionTrashedEditMessage(event.App, read.Tombstone.PageID, false, false, notionTrashedEdit)
		if ancestor != "" {
			message = ancestor
		}
	}
	if err != nil {
		return true, notionRequestNotCompleted(event, err)
	}
	if !found {
		return false, nil
	}
	return true, notionPageValidationError(event, message)
}

// refuseNotionTrashedBlockBodyEdit answers a change to a block in a page
// the connection reads while the page is in the trash.
func refuseNotionTrashedBlockBodyEdit(
	event *core.RequestEvent, identity notionOAuthBearerIdentity, pageID string,
) (bool, error) {
	message, err := notionTrashedBodyEdit(event.App, identity, pageID, false, notionTrashedEdit)
	if err != nil {
		return true, notionRequestNotCompleted(event, err)
	}
	if message == "" {
		return false, nil
	}
	return true, notionPageValidationError(event, message)
}

// notionTrashedDatabaseEditMessage is why a change to a database is refused
// for the trash, or "": a full-page database is its page, in the trash
// itself or under one; an inline database is in the trash itself when its
// block is, and under one while the page holding it is in the trash.
func notionTrashedDatabaseEditMessage(
	app core.App, container notionDatabaseContainer, restoring bool,
) (string, error) {
	message, err := notionTrashedEditMessage(app, container.PageID, container.fullPage(), restoring, notionTrashedEdit)
	if err != nil || message != "" || !container.Trashed || restoring {
		return message, err
	}
	return notionArchivedCommentMessage, nil
}
