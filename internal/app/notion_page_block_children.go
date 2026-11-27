package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionPageBodyItemReference struct {
	Lane       int     `db:"lane"`
	SortText   string  `db:"sort_text"`
	SortNumber float64 `db:"sort_number"`
	Kind       string  `db:"kind"`
	ID         string  `db:"id"`
}

type notionPageBlockChildrenSnapshot struct {
	Results    []map[string]any
	HasMore    bool
	NextCursor any
}

func serveNotionChildPageBlockRetrieve(event *core.RequestEvent, publicPageID string) error {
	authorization, authorized, err := authorizeNotionPage(
		event, publicPageID, "pages:read", permissionWorkspaceRead,
	)
	if err != nil || !authorized {
		return err
	}
	response := map[string]any{}
	err = runNotionReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		current, err := authorizePage(
			txApp, authorization.Identity.PrincipalID,
			authorization.Page.Page.Id, permissionWorkspaceRead,
		)
		if err != nil || current.Workspace.WorkspaceID != authorization.Identity.WorkspaceID ||
			!notionOAuthIdentityAllowsPage(txApp, authorization.Identity, current) {
			if err == nil || isPermissionDenied(err) {
				return permissionDenied()
			}
			return err
		}
		if strings.TrimSpace(current.Page.GetString("parent_id")) == "" {
			return permissionDenied()
		}
		parent, err := notionChildPageParent(txApp, current.Page)
		if err != nil {
			return err
		}
		hasChildren, err := loadNotionPageHasChildren(txApp, []string{current.Page.Id})
		if err != nil {
			return err
		}
		response, err = notionChildPageBlockObject(
			txApp, current.Page, parent, hasChildren[current.Page.Id],
		)
		if err != nil {
			return err
		}
		// A page's block reads in the trash with the page, itself or under
		// one (notion_trash_reads.go).
		place, err := notionPageTrashPlace(txApp, current.Page.Id)
		response["in_trash"] = place.inTrash()
		return err
	})
	if isPermissionDenied(err) {
		return notionBlockNotFound(event)
	}
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func serveNotionPageBlockChildren(event *core.RequestEvent, publicPageID string) error {
	authorization, authorized, err := authorizeNotionPage(
		event, publicPageID, "pages:read", permissionWorkspaceRead,
	)
	if err != nil || !authorized {
		return err
	}
	return serveAuthorizedNotionPageBlockChildren(event, authorization)
}

// serveAuthorizedNotionPageBlockChildren lists a page's body, a database
// page's included, once the page is authorized.
func serveAuthorizedNotionPageBlockChildren(event *core.RequestEvent, authorization notionPageAuthorization) error {
	// A page in the trash itself isn't found for its children, and one under
	// something in the trash lists none (notion_trash_reads.go).
	place, err := notionPageTrashPlace(event.App, authorization.Page.Page.Id)
	if err != nil {
		return notionBlockProjectionError(event, err)
	}
	switch place {
	case notionInTrashItself:
		return notionBlockNotFound(event)
	case notionUnderTrash:
		return notionNoChildren(event)
	}
	// A full-page database read as a block lists no children, as an inline
	// one doesn't: its body is the database itself (Notion's, seen
	// 2026-11-11).
	if isFullPageDatabase(authorization.Page.Page) {
		return notionNoChildren(event)
	}
	pageSize := notionBlockDefaultPageSize
	if raw := notionQueryDigits(event.Request.URL.Query().Get("page_size")); raw != "" {
		pageSize, err = strconv.Atoi(raw)
		if err != nil {
			return notionPageValidationError(event, "page_size must be a number.")
		}
		pageSize = notionListPageSize(pageSize, notionBlockDefaultPageSize)
	}
	cursor, err := parseNotionPageBodyCursor(event.App, event.Request.URL.Query().Get("start_cursor"))
	if err == nil && cursor.Kind == "block" {
		cursor.ID, _ = resolveNotionBlockID(event.App, cursor.ID)
	}
	if err != nil {
		return notionStartCursorRefused(event, event.Request.URL.Query().Get("start_cursor"))
	}
	snapshot, err := loadNotionPageBlockChildrenSnapshot(
		event.App, authorization, cursor, pageSize,
		newNotionBlockFileCapabilityIssuer(event.Request, authorization.Identity),
	)
	if isPermissionDenied(err) {
		return notionPageNotFound(event)
	}
	if errors.Is(err, errNotionBlockCursorInvalid) {
		return notionStartCursorRefused(event, event.Request.URL.Query().Get("start_cursor"))
	}
	if err != nil {
		return notionBlockProjectionError(event, err)
	}
	recordAPIReads(event.App, authorization.Identity, []contentRead{contentReadOfPage(authorization.Page.Page)})
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]any{
		"object": "list", "results": snapshot.Results, "next_cursor": snapshot.NextCursor,
		"has_more": snapshot.HasMore, "type": "block", "block": map[string]any{},
	})
}

func loadNotionPageBlockChildrenSnapshot(
	app core.App,
	authorization notionPageAuthorization,
	cursor notionPageBodyCursor,
	pageSize int,
	fileIssuer *notionBlockFileCapabilityIssuer,
) (notionPageBlockChildrenSnapshot, error) {
	snapshot := notionPageBlockChildrenSnapshot{}
	err := runNotionReadSnapshot(context.Background(), app, func(txApp core.App) error {
		snapshot = notionPageBlockChildrenSnapshot{}
		current, err := authorizePage(
			txApp, authorization.Identity.PrincipalID,
			authorization.Page.Page.Id, permissionWorkspaceRead,
		)
		if err != nil || current.Workspace.WorkspaceID != authorization.Identity.WorkspaceID ||
			!notionOAuthIdentityAllowsPage(txApp, authorization.Identity, current) ||
			current.Page.GetBool("in_trash") {
			if err == nil || isPermissionDenied(err) {
				return permissionDenied()
			}
			return err
		}
		var from *notionPageBodyItemReference
		// The page's own ID, the cursor Leafen gave a read of none before,
		// starts from its first item.
		if cursor.Kind != "" && cursor.ID != current.Page.Id {
			cursorReference, found, err := loadNotionPageBodyCursorReference(
				txApp, current, cursor,
			)
			if err != nil {
				return err
			}
			if !found {
				return errNotionBlockCursorInvalid
			}
			from = &cursorReference
		}
		items, err := loadNotionPageBodyItemReferences(txApp, current, from, pageSize+1)
		if err != nil {
			return err
		}
		snapshot.HasMore = len(items) > pageSize
		if snapshot.HasMore {
			// The cursor names the item after the last given (notionPageBodyNextCursor).
			if snapshot.NextCursor, err = notionPageBodyNextCursor(app, txApp, current, items[pageSize]); err != nil {
				return err
			}
			items = items[:pageSize]
		}
		document, err := loadNotionBlockTextDocument(txApp, current.Page.Id)
		if err != nil {
			return err
		}
		blockIDs, pageIDs := notionPageBodyItemIDs(items)
		childCounts, err := loadNotionBlockChildCounts(txApp, blockIDs)
		if err != nil {
			return err
		}
		pageHasChildren, err := loadNotionPageHasChildren(txApp, pageIDs)
		if err != nil {
			return err
		}
		blockAuthorization := notionBlockAuthorization{
			Identity: authorization.Identity, Page: current,
		}
		pageItemParent, err := notionParentInPage(txApp, current.Page.Id)
		if err != nil {
			return err
		}
		snapshot.Results = make([]map[string]any, 0, len(items))
		for _, item := range items {
			if item.Kind == "block" {
				block, found, err := loadStoredNotionBlock(txApp, item.ID)
				if err != nil || !found {
					return refusedUnlessStorage(errNotionPageMarkdownChanged, err)
				}
				value, err := notionBlockObject(
					txApp, blockAuthorization, block, childCounts[block.ID] > 0, document,
					fileIssuer,
				)
				if err != nil {
					return err
				}
				snapshot.Results = append(snapshot.Results, value)
				continue
			}
			// A page item is a sub-page of this page, or one of the pages of a
			// wiki under it, which are its wiki database's rows.
			page, err := txApp.FindRecordById(pagesCollection, item.ID)
			if err != nil || page.GetBool("in_trash") ||
				page.GetString("parent_id") != current.Page.Id && page.GetString("data_source_id") == "" {
				return refusedUnlessStorage(errNotionPageMarkdownChanged, err)
			}
			value, err := notionChildPageBlockObject(txApp, page, pageItemParent, pageHasChildren[page.Id])
			if err != nil {
				return err
			}
			snapshot.Results = append(snapshot.Results, value)
		}
		return nil
	})
	return snapshot, err
}

// notionPageBodyCTE lists a page's body as readers see it: its top-level blocks in order, a
// sub-page's block standing for the page, then the sub-pages with no block in the body yet, none
// hanging on a line, row, caption, title or cell (Notion lists none, seen 2026-11-18), and a wiki's
// pages, each once (notionPageBodyWikiItemsSQL). An earlier child_page block naming a live sub-page
// gives way to that page, and a database page an inline database block shows stays with that block.
func notionPageBodyCTE() string {
	return `
		WITH [[live_children]] AS (
			SELECT [[child]].[[id]], [[child]].[[position]], [[child]].[[anchor_block_id]] FROM {{leafen_pages}} [[child]]
			WHERE [[child]].[[owner]]={:owner} AND [[child]].[[workspace]]={:workspaceId}
				AND [[child]].[[parent_id]]={:pageId} AND [[child]].[[in_trash]]=FALSE
				AND [[child]].[[notion_import_staging]]=FALSE
		), ` + notionPageBodyWikiPagesCTE() + `, [[items]] AS (
			SELECT 0 AS [[lane]], [[block]].[[position]] AS [[sort_text]], 0.0 AS [[sort_number]],
				'block' AS [[kind]], [[block]].[[id]] AS [[id]]
			FROM {{leafen_blocks}} [[block]]
			WHERE [[block]].[[owner]]={:owner} AND [[block]].[[page_id]]={:pageId} AND [[block]].[[parent_id]]=''
				AND ` + blockStandsSQL("block") + `
				AND NOT (
					[[block]].[[type]] IN ('child_page','child_database') AND json_valid([[block]].[[content]])
					AND json_extract([[block]].[[content]], '$.childPageId') IN (SELECT [[id]] FROM [[live_children]])
				)
				AND NOT ` + notionPageBodyWikiBlockLeftOutSQL("[[block]]") + `
			UNION ALL
			SELECT 0, [[block]].[[position]], 0.0, 'page', [[block]].[[id]]
			FROM {{leafen_blocks}} [[block]]
			JOIN [[live_children]] ON [[live_children]].[[id]]=[[block]].[[id]]
			WHERE [[block]].[[owner]]={:owner} AND [[block]].[[page_id]]={:pageId} AND [[block]].[[parent_id]]=''
				AND [[block]].[[type]]='child_page' AND json_valid([[block]].[[content]])
				AND json_extract([[block]].[[content]], '$.childPageId')=[[block]].[[id]]
			UNION ALL
			SELECT 1, '', CAST([[live_children]].[[position]] AS REAL), 'page', [[live_children]].[[id]]
			FROM [[live_children]]
			WHERE [[live_children]].[[anchor_block_id]]='' AND NOT EXISTS (
				SELECT 1 FROM {{leafen_blocks}} [[shown]]
				WHERE [[shown]].[[owner]]={:owner} AND [[shown]].[[page_id]]={:pageId}
					AND (
						([[shown]].[[id]]=[[live_children]].[[id]] AND [[shown]].[[type]]='child_page')
						OR ([[shown]].[[type]]='inline_database' AND json_valid([[shown]].[[content]])
							AND json_extract([[shown]].[[content]], '$.childPageId')=[[live_children]].[[id]])
					)
			)` + notionPageBodyWikiItemsSQL() + `
		)
	`
}

func notionPageBodyParams(current pageAuthorization) dbx.Params {
	return dbx.Params{
		"owner": current.Workspace.OwnerID, "workspaceId": current.Workspace.WorkspaceID,
		"pageId": current.Page.Id,
	}
}

func loadNotionPageBodyCursorReference(
	app core.App,
	current pageAuthorization,
	cursor notionPageBodyCursor,
) (notionPageBodyItemReference, bool, error) {
	reference := notionPageBodyItemReference{}
	params := notionPageBodyParams(current)
	params["cursorKind"], params["cursorId"] = cursor.Kind, cursor.ID
	err := app.DB().NewQuery(notionPageBodyCTE() + `
		SELECT [[lane]],[[sort_text]],[[sort_number]],[[kind]],[[id]] FROM [[items]]
		WHERE [[kind]]={:cursorKind} AND [[id]]={:cursorId} LIMIT 2
	`).Bind(params).One(&reference)
	if errors.Is(err, sql.ErrNoRows) {
		return notionPageBodyItemReference{}, false, nil
	}
	return reference, err == nil, err
}

// loadNotionPageBodyItemReferences lists a page's body items from the one
// from names, that one first, or from its first item.
func loadNotionPageBodyItemReferences(
	app core.App,
	current pageAuthorization,
	from *notionPageBodyItemReference,
	limit int,
) ([]notionPageBodyItemReference, error) {
	items := []notionPageBodyItemReference{}
	params := notionPageBodyParams(current)
	params["fromLane"], params["fromText"], params["fromNumber"], params["fromId"] = -1, "", 0.0, ""
	if from != nil {
		params["fromLane"], params["fromText"], params["fromNumber"], params["fromId"] =
			from.Lane, from.SortText, from.SortNumber, from.ID
	}
	params["limit"] = limit
	err := app.DB().NewQuery(notionPageBodyCTE() + `
		SELECT [[lane]],[[sort_text]],[[sort_number]],[[kind]],[[id]] FROM [[items]]
		WHERE ([[lane]],[[sort_text]],[[sort_number]],[[id]]) >=
			({:fromLane},{:fromText},{:fromNumber},{:fromId})
		ORDER BY [[lane]],[[sort_text]],[[sort_number]],[[id]] LIMIT {:limit}
	`).Bind(params).All(&items)
	return items, err
}

func notionPageBodyItemIDs(items []notionPageBodyItemReference) ([]string, []string) {
	blocks, pages := []string{}, []string{}
	for _, item := range items {
		if item.Kind == "block" {
			blocks = append(blocks, item.ID)
		} else {
			pages = append(pages, item.ID)
		}
	}
	return blocks, pages
}

func loadNotionPageHasChildren(app core.App, pageIDs []string) (map[string]bool, error) {
	result := map[string]bool{}
	if len(pageIDs) == 0 {
		return result, nil
	}
	params, placeholders := dbx.Params{}, make([]string, len(pageIDs))
	for index, pageID := range pageIDs {
		key := "page" + strconv.Itoa(index)
		params[key], placeholders[index] = pageID, "{:"+key+"}"
	}
	rows := []struct {
		ID string `db:"id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[page_id]] AS [[id]] FROM {{leafen_blocks}}
		WHERE [[page_id]] IN (` + strings.Join(placeholders, ",") + `) AND [[parent_id]]=''
			AND ` + blockStandsSQL("") + `
		GROUP BY [[page_id]]
		UNION
		SELECT [[parent_id]] AS [[id]] FROM {{leafen_pages}}
		WHERE [[parent_id]] IN (` + strings.Join(placeholders, ",") + `) AND [[in_trash]]=FALSE AND [[anchor_block_id]]=''
		GROUP BY [[parent_id]]
	`).Bind(params).All(&rows)
	for _, row := range rows {
		result[row.ID] = true
	}
	return result, err
}

func notionChildPageBlockObject(
	app core.App,
	page *core.Record,
	parent map[string]any,
	hasChildren bool,
) (map[string]any, error) {
	publicID, err := notionPagePublicID(app, page.Id)
	if err != nil {
		return nil, err
	}
	createdBy, lastEditedBy, err := notionPageAuthorIDs(app, page)
	if err != nil {
		return nil, err
	}
	createdTime, err := notionPageMinuteTime(page.GetString("created"))
	if err != nil {
		return nil, err
	}
	lastEditedTime, err := notionPageMinuteTime(page.GetString("updated"))
	if err != nil {
		return nil, err
	}
	trashed := page.GetBool("in_trash")
	// A full-page database or a wiki is a child database, named by its
	// database's ID and title (notionPageDatabaseBlock).
	if database, found, err := notionPageDatabaseBlock(app, page, hasChildren); err != nil {
		return nil, err
	} else if found {
		databaseID, err := ensureNotionObjectIdentity(
			app, notionObjectIdentityDatabase, page.GetString("workspace"), database.ContainerID,
		)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"object": "block", "id": databaseID, "parent": parent,
			"created_time":     createdTime,
			"created_by":       map[string]any{"object": "user", "id": createdBy},
			"last_edited_time": lastEditedTime,
			"last_edited_by":   map[string]any{"object": "user", "id": lastEditedBy},
			"has_children":     database.HasChildren, "in_trash": trashed,
			"type": "child_database", "child_database": map[string]any{"title": database.Title},
		}, nil
	}
	return map[string]any{
		"object": "block", "id": publicID,
		"parent":           parent,
		"created_time":     createdTime,
		"created_by":       map[string]any{"object": "user", "id": createdBy},
		"last_edited_time": lastEditedTime,
		"last_edited_by":   map[string]any{"object": "user", "id": lastEditedBy},
		"has_children":     hasChildren,
		"in_trash":         trashed,
		"type":             "child_page",
		"child_page":       map[string]any{"title": page.GetString("title")},
	}, nil
}

// notionChildPageParent is a sub-page's parent as Notion gives it: the line
// (a callout's words, its callout), table row or captioned block it hangs on,
// the block its page block sits in, or else the page above it — one hanging
// on that page's title or cell too (mention_anchored_fields.go) — named as
// notionParentInPage names a page.
func notionChildPageParent(app core.App, page *core.Record) (map[string]any, error) {
	if anchor := page.GetString(pageAnchorBlockField); anchor != "" && !pageAnchorOnField(anchor) {
		blockID, err := notionHangingPageParentID(app, page, anchor)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "block_id", "block_id": blockID}, nil
	}
	holder := struct {
		PageID   string `db:"page_id"`
		ParentID string `db:"parent_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[page_id]], [[parent_id]] FROM {{leafen_blocks}}
		WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[type]] = 'child_page'
	`).Bind(dbx.Params{"id": page.Id, "owner": page.GetString("owner")}).One(&holder)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil && holder.ParentID != "" && holder.PageID == page.GetString("parent_id") {
		blockID, err := notionBlockPublicID(app, page.GetString("workspace"), holder.ParentID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "block_id", "block_id": blockID}, nil
	}
	return notionParentInPage(app, page.GetString("parent_id"))
}
