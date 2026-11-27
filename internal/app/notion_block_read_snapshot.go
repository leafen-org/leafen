package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var errNotionBlockCursorInvalid = errors.New("Notion-compatible block cursor is invalid")

type notionBlockChildrenReadSnapshot struct {
	Results    []map[string]any
	HasMore    bool
	NextCursor any
}

func loadNotionBlockReadSnapshot(
	app core.App,
	authorization notionBlockAuthorization,
	fileIssuer *notionBlockFileCapabilityIssuer,
) (map[string]any, error) {
	response := map[string]any{}
	err := runNotionReadSnapshot(context.Background(), app, func(txApp core.App) error {
		current, err := reauthorizeNotionBlockReadSnapshot(
			txApp, authorization, permissionWorkspaceRead,
		)
		if err != nil {
			return err
		}
		document, err := loadNotionBlockTextDocument(txApp, current.Block.PageID)
		if err != nil {
			return err
		}
		counts, err := loadNotionBlockChildCounts(txApp, []string{current.Block.ID})
		if err != nil {
			return err
		}
		response, err = notionBlockObject(
			txApp, current, current.Block, counts[current.Block.ID] > 0, document, fileIssuer,
		)
		return err
	})
	return response, err
}

// loadNotionBlockChildrenReadSnapshot reads a block's children from the
// child startCursor names, that child first, and names the child after the
// last it gives as next_cursor, as Notion's API reads them (seen 2026-11-21,
// Notion-Version 2026-03-11, synthetic pages under a Notion test page: a page,
// a toggle and a synced copy at page_size 1 and 2 — a negative one names the
// first child — and an append's answer, which reads from the first block it
// added, notion_block_append_answer.go). A cursor that names the last child
// a read gave, as Leafen's named it before, reads it again.
func loadNotionBlockChildrenReadSnapshot(
	app core.App,
	authorization notionBlockAuthorization,
	startCursor string,
	pageSize int,
	fileIssuer *notionBlockFileCapabilityIssuer,
) (notionBlockChildrenReadSnapshot, error) {
	snapshot := notionBlockChildrenReadSnapshot{}
	err := runNotionReadSnapshot(context.Background(), app, func(txApp core.App) error {
		snapshot = notionBlockChildrenReadSnapshot{}
		current, err := reauthorizeNotionBlockReadSnapshot(
			txApp, authorization, permissionWorkspaceRead,
		)
		if err != nil {
			return err
		}
		afterPosition, afterID, including := "", "", false
		if current.Block.Type == graphBlockTypeCallout {
			// A callout's first line reads as its text, not among its children.
			document, err := loadNotionBlockTextDocument(txApp, current.Block.PageID)
			if err != nil {
				return err
			}
			line, _, found, err := notionCalloutFirstLine(txApp, current.Block, document)
			if err != nil {
				return err
			}
			if found {
				afterPosition, afterID = line.Position, line.ID
			}
		}
		// The block's own ID, the cursor Leafen gave a read of none before,
		// starts from its first child.
		if startCursor != "" && startCursor != current.Block.ID {
			cursor := storedNotionBlock{}
			err := txApp.DB().NewQuery(`
				SELECT [[id]],[[position]],[[parent_id]],[[page_id]] FROM {{leafen_blocks}}
				WHERE [[id]]={:id} LIMIT 2
			`).Bind(dbx.Params{"id": startCursor}).One(&cursor)
			if errors.Is(err, sql.ErrNoRows) || err == nil &&
				(cursor.PageID != current.Block.PageID || cursor.ParentID != current.Block.ID || cursor.ID == afterID) {
				return errNotionBlockCursorInvalid
			}
			if err != nil {
				return err
			}
			afterPosition, afterID, including = cursor.Position, cursor.ID, true
		}

		rows := []storedNotionBlock{}
		err = txApp.DB().NewQuery(`
			SELECT [[id]],[[owner]],[[workspace_id]],[[page_id]],[[parent_id]],[[position]],
				[[type]],[[content]],[[created_by]],[[last_edited_by]],[[created]],[[updated]]
			FROM {{leafen_blocks}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[parent_id]]={:parentId}
				AND ` + blockStandsSQL("") + `
				AND ([[position]]>{:afterPosition} OR ([[position]]={:afterPosition} AND [[id]]` + notionCursorComparison(including) + `{:afterId}))
			ORDER BY [[position]],[[id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"owner": current.Block.Owner, "pageId": current.Block.PageID,
			"parentId": current.Block.ID, "afterPosition": afterPosition,
			"afterId": afterID, "limit": pageSize + 1,
		}).All(&rows)
		if err != nil {
			return err
		}
		snapshot.HasMore = len(rows) > pageSize
		if snapshot.HasMore {
			next := rows[pageSize]
			if snapshot.NextCursor, err = notionBlockPublicID(txApp, next.WorkspaceID, next.ID); err != nil {
				return err
			}
			rows = rows[:pageSize]
		}
		ids := make([]string, len(rows))
		for index := range rows {
			ids[index] = rows[index].ID
		}
		counts, err := loadNotionBlockChildCounts(txApp, ids)
		if err != nil {
			return err
		}
		document, err := loadNotionBlockTextDocument(txApp, current.Block.PageID)
		if err != nil {
			return err
		}
		snapshot.Results = make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			value, err := notionBlockObject(
				txApp, current, row, counts[row.ID] > 0, document, fileIssuer,
			)
			if err != nil {
				return err
			}
			snapshot.Results = append(snapshot.Results, value)
		}
		return nil
	})
	return snapshot, err
}

func reauthorizeNotionBlockReadSnapshot(
	app core.App,
	authorization notionBlockAuthorization,
	requirement permissionRequirement,
) (notionBlockAuthorization, error) {
	block, found, err := loadStoredNotionBlock(app, authorization.Block.ID)
	if err != nil {
		return notionBlockAuthorization{}, err
	}
	// A suggested block isn't part of the page as it stands.
	if !found || graphBlockSuggestion(block.Content) != "" {
		return notionBlockAuthorization{}, permissionDenied()
	}
	page, err := authorizePageBody(
		app, authorization.Identity.PrincipalID, block.PageID, requirement,
	)
	if isPermissionDenied(err) {
		return notionBlockAuthorization{}, permissionDenied()
	}
	if err != nil {
		return notionBlockAuthorization{}, err
	}
	if page.Workspace.WorkspaceID != authorization.Identity.WorkspaceID ||
		block.WorkspaceID != authorization.Identity.WorkspaceID ||
		block.Owner != page.Workspace.OwnerID ||
		!notionOAuthIdentityAllowsPage(app, authorization.Identity, page) {
		return notionBlockAuthorization{}, permissionDenied()
	}
	authorization.Page = page
	authorization.Block = block
	return authorization, nil
}
