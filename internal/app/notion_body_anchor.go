package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	// errNotionBodyAnchorMissing names no block or page this connection
	// reads, which Notion says it can't find (seen 2026-11-14: a missing
	// ID, a data source's, a page out of its reach).
	errNotionBodyAnchorMissing = errors.New("after_block.id names nothing this connection reads")
	// errNotionBodyAnchorOutside names a block or page it reads elsewhere:
	// another page's, one inside a toggle, the container itself.
	errNotionBodyAnchorOutside = errors.New("after_block.id is not a child of this container")
	// errNotionBodyAnchorTrashed names a child of this container in the
	// trash, after which new blocks go at the end, as Notion's do (seen
	// 2026-11-14: blocks after trashed ones, a trashed sub-page's or a
	// trashed toggle's, all at the end).
	errNotionBodyAnchorTrashed = errors.New("after_block.id is a child of this container in the trash")
)

// notionBodyAnchor resolves an official after_block ID among the children of
// a page body or block: the block new blocks follow, a sub-page's block when
// the ID is a sub-page's, as the connection reads them.
func notionBodyAnchor(
	app core.App, identity notionOAuthBearerIdentity, pageID, parentID, publicID string,
) (*string, error) {
	normalized, isPage := "", false
	if childPageID, err := resolveNotionPageRecordID(app, publicID); err == nil {
		normalized, isPage = childPageID, true
	} else if row, err := resolveNotionObjectIdentity(app, notionObjectIdentityRowPage, publicID); err == nil {
		normalized, isPage = row.InternalID, true
	} else if blockID, ok := resolveNotionBlockID(app, publicID); ok {
		normalized = blockID
	} else {
		return nil, errNotionBodyAnchorMissing
	}
	candidate, found, err := loadStoredNotionBlock(app, normalized)
	if err != nil {
		return nil, err
	}
	if found {
		if reads, err := notionBodyAnchorReads(app, identity, candidate.PageID); err != nil || !reads {
			return nil, notionBodyAnchorUnread(err)
		}
		if candidate.PageID != pageID || candidate.ParentID != parentID {
			return nil, errNotionBodyAnchorOutside
		}
		return &normalized, nil
	}
	if isPage {
		return nil, notionBodyAnchorPage(app, identity, pageID, parentID, normalized)
	}
	read, found, err := loadNotionTrashedBlockRead(app, identity, normalized)
	if err != nil || !found {
		return nil, notionBodyAnchorUnread(err)
	}
	if read.Tombstone.PageID == pageID && read.Snapshot.Blocks[read.Index].ParentID == parentID {
		return nil, errNotionBodyAnchorTrashed
	}
	return nil, errNotionBodyAnchorOutside
}

// notionBodyAnchorPage places after a page without a block in a body: one
// in the trash under this container, or one the connection reads
// elsewhere.
func notionBodyAnchorPage(app core.App, identity notionOAuthBearerIdentity, pageID, parentID, recordID string) error {
	if reads, err := notionBodyAnchorReads(app, identity, recordID); err != nil || !reads {
		return notionBodyAnchorUnread(err)
	}
	page, err := app.FindRecordById(pagesCollection, recordID)
	if err != nil {
		return err
	}
	if !page.GetBool("in_trash") || page.GetString("parent_id") != pageID {
		return errNotionBodyAnchorOutside
	}
	container := ""
	err = app.DB().NewQuery(`
		SELECT [[parent_block_id]] FROM {{leafen_child_page_block_places}}
		WHERE [[page_id]] = {:pageId} AND [[parent_page_id]] = {:parentPageId}
	`).Bind(dbx.Params{"pageId": recordID, "parentPageId": pageID}).Row(&container)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if container != parentID {
		return errNotionBodyAnchorOutside
	}
	return errNotionBodyAnchorTrashed
}

// notionBodyAnchorReads tells whether the connection reads a page's body.
func notionBodyAnchorReads(app core.App, identity notionOAuthBearerIdentity, pageID string) (bool, error) {
	page, err := authorizePageBody(app, identity.PrincipalID, pageID, permissionWorkspaceRead)
	if isPermissionDenied(err) || errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return page.Workspace.WorkspaceID == identity.WorkspaceID && notionOAuthIdentityAllowsPage(app, identity, page), nil
}

// notionBodyAnchorUnread is a failed read's error, or the anchor missing.
func notionBodyAnchorUnread(err error) error {
	if err != nil {
		return err
	}
	return errNotionBodyAnchorMissing
}

// notionBodyAnchorError refuses an after_block as Notion does: one it can't
// find, as not found, and one it finds elsewhere naming it and the
// container, both dashed (seen 2026-11-14).
func notionBodyAnchorError(event *core.RequestEvent, err error, afterID string) error {
	switch {
	case errors.Is(err, errNotionBodyAnchorOutside):
		return notionPageValidationError(event, "The block to add children after ("+notionDashedUUID(afterID)+
			") isn't a child of "+notionObjectAsked(event).ID+".")
	case errors.Is(err, errNotionBodyAnchorMissing):
		return notionNamedObjectNotFound(event, "block", notionDashedUUID(afterID))
	}
	return notionRequestNotCompleted(event, err)
}

// notionAppendPosition is where appended blocks go, its shape checked before
// (notionAppendPositionShape): read by its type, and an after_block's ID,
// alone — Notion leaves a start's or end's value, and every other key (seen
// 2026-11-14).
type notionAppendPosition struct {
	Type       string `json:"type"`
	AfterBlock *struct {
		ID string `json:"id"`
	} `json:"after_block,omitempty"`
}

func (position *notionAppendPosition) UnmarshalJSON(raw []byte) error {
	value := struct {
		Type       string          `json:"type"`
		AfterBlock json.RawMessage `json:"after_block"`
	}{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	position.Type = value.Type
	if value.Type == "after_block" && value.AfterBlock != nil {
		if err := json.Unmarshal(value.AfterBlock, &position.AfterBlock); err != nil {
			return err
		}
	}
	return nil
}
