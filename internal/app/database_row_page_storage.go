package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func loadDatabaseRowPageGraphValues(
	app core.App,
	authority databaseRowPageAuthority,
) (map[string]graphRowValue, error) {
	stored, err := loadGraphRowValues(
		app, authority.OwnerID, authority.ContainerPageID, []string{authority.RowID},
	)
	if err != nil {
		return nil, err
	}
	values := make(map[string]graphRowValue, len(stored))
	for _, value := range stored {
		raw := json.RawMessage(value.Value)
		if !json.Valid(raw) {
			return nil, fmt.Errorf("invalid row page value: %s", value.PropertyID)
		}
		values[value.PropertyID] = graphRowValue{Type: value.Type, Value: raw}
	}
	return values, nil
}

func loadDatabaseRowPageWindowInTransaction(
	app core.App, principalID string, rowID string, rawCursor string, limit int, firstBlock bool,
) (databaseRowPageSnapshot, error) {
	return loadDatabaseRowPageWindowInTransactionFor(context.Background(), app, principalID, rowID, rawCursor, limit, firstBlock)
}

// loadDatabaseRowPageWindowInTransactionFor is the row page window for the
// reader whose own request ctx carries, its untitled rows named in its language.
func loadDatabaseRowPageWindowInTransactionFor(
	ctx context.Context,
	app core.App,
	principalID string,
	rowID string,
	rawCursor string,
	limit int,
	firstBlock bool,
) (databaseRowPageSnapshot, error) {
	// A page of a data source in the trash reads as a page in the trash.
	authority, sourceInTrash, err := authorizeDatabaseRowPageRead(app, principalID, rowID)
	if err != nil {
		return databaseRowPageSnapshot{}, err
	}
	cursor, err := decodeDatabaseRowPageCursor(rawCursor, principalID, rowID, limit)
	if err != nil {
		return databaseRowPageSnapshot{}, err
	}
	var containerRevision int
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[cursor]]), 0)
		FROM {{leafen_graph_mutations}}
		WHERE [[owner]]={:ownerId} AND [[page_id]]={:pageId}
	`).Bind(dbx.Params{
		"ownerId": authority.OwnerID, "pageId": authority.ContainerPageID,
	}).Row(&containerRevision); err != nil {
		return databaseRowPageSnapshot{}, err
	}
	// A change to the row, its body or its database between windows doesn't
	// start the read over, as the content graph's doesn't
	// (contentGraphCollection.ts): the window after it reads the page as it
	// is then, after the cursor's block, and says the revisions it read, so
	// its reader keeps the first window's and finds itself behind. Whether
	// the reader may still read the row is asked again above, window by window
	// (2026-11-14: a row page of 161 blocks, opened while another person
	// renamed a row of its database every 60 ms, failed six reads in a row
	// and closed).

	// A template's values are its own, which its window reads with its metadata.
	values := json.RawMessage(`{}`)
	if !authority.Template {
		if values, err = loadDatabaseRowPageValues(app, authority); err != nil {
			return databaseRowPageSnapshot{}, err
		}
	}
	load := loadDatabaseRowPageBodyWindow
	if firstBlock {
		load = func(app core.App, rowID, _ string, _ int) ([]storedDatabaseRowPageBodyBlock, error) {
			return loadDatabaseRowPageFirstBlock(app, rowID)
		}
	}
	body, err := load(app, rowID, cursor.SortKey, limit)
	if err != nil {
		return databaseRowPageSnapshot{}, err
	}
	hasMore := !firstBlock && len(body) > limit
	if hasMore {
		body = body[:limit]
	}
	// The body's text lives in its page's document; the read carries it
	// where the row page's readers look for it.
	textDocument, err := loadRowPageTextDocument(app, rowID)
	if err != nil {
		return databaseRowPageSnapshot{}, err
	}
	blocks := make([]databaseRowPageBodyBlockWire, 0, len(body))
	for _, stored := range body {
		fields := map[string]any{}
		if err := json.Unmarshal([]byte(stored.Content), &fields); err != nil {
			return databaseRowPageSnapshot{}, fmt.Errorf("invalid row page block content: %s", stored.ID)
		}
		// A divider's are none of its words (graphBlockWordless).
		if spans, plain := rowPageBlockText(textDocument, stored.ID); len(spans) > 0 && !graphBlockWordless(stored.Type) {
			fields["notionRichTextSpans"], fields["text"] = spans, plain
		}
		content, err := json.Marshal(fields)
		if err != nil {
			return databaseRowPageSnapshot{}, err
		}
		var parentID *string
		if stored.ParentID != "" {
			value := stored.ParentID
			parentID = &value
		}
		blocks = append(blocks, databaseRowPageBodyBlockWire{
			ID: stored.ID, ParentID: parentID, Position: stored.Position,
			Type: stored.Type, Content: content,
		})
	}
	var nextCursor *string
	if hasMore && len(body) > 0 {
		encoded, err := encodeDatabaseRowPageCursor(databaseRowPageCursor{
			PrincipalID: principalID, RowID: rowID,
			ContainerSnapshotRevision: containerRevision,
			AuthorityRevision:         authority.AuthorityRevision,
			BodySnapshotRevision:      authority.BodySnapshotRevision,
			SortKey:                   body[len(body)-1].SortKey, Limit: limit,
		})
		if err != nil {
			return databaseRowPageSnapshot{}, err
		}
		nextCursor = &encoded
	}
	var favoriteCount int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_page_favorites}}
		WHERE [[page_id]]={:rowId} AND [[principal_id]]={:principalId} AND [[favorite]]=1
	`).Bind(dbx.Params{"rowId": rowID, "principalId": principalID}).Row(&favoriteCount); err != nil {
		return databaseRowPageSnapshot{}, err
	}
	var subitemParentID *string
	if authority.SubitemParentID != "" {
		value := authority.SubitemParentID
		subitemParentID = &value
	}
	properties, err := loadDatabaseRowPagePropertyWires(app, authority)
	if err != nil {
		return databaseRowPageSnapshot{}, err
	}
	role, err := databaseRowPageAccessRole(app, principalID, authority)
	if err != nil {
		return databaseRowPageSnapshot{}, err
	}
	if sourceInTrash {
		// The page holding its database answers for it (database_source_trash_rows.go).
		container, err := authorizePageBody(app, principalID, authority.ContainerPageID, permissionWorkspaceRead)
		if err != nil {
			return databaseRowPageSnapshot{}, err
		}
		role = container.AccessRole
	}
	lock, err := databaseRowPageLockStateFor(app, authority, principalID)
	if err != nil {
		return databaseRowPageSnapshot{}, err
	}
	databaseWritable, databaseLocked, databaseHeld, err := databaseRowPageDatabaseWritable(app, principalID, authority)
	if err != nil {
		return databaseRowPageSnapshot{}, err
	}
	holderLocked, err := databaseRowPageHolderLockedFor(app, principalID, authority)
	if err != nil {
		return databaseRowPageSnapshot{}, err
	}
	taskRoles, err := loadDatabaseTaskRoles(app, authority.DataSourceID)
	if err != nil {
		return databaseRowPageSnapshot{}, err
	}
	// A row is called after its database's default template; a template is not.
	untitledName := ""
	if !authority.Template {
		if untitledName, err = databaseUntitledRowNameFor(ctx, app, principalID, authority.DataSourceID); err != nil {
			return databaseRowPageSnapshot{}, err
		}
	}
	snapshot := databaseRowPageSnapshot{
		SnapshotIdentity: databaseRowPageSnapshotIdentity(
			rowID, containerRevision, authority.AuthorityRevision, authority.BodySnapshotRevision,
		),
		ContainerSnapshotRevision: containerRevision,
		BodySnapshotRevision:      authority.BodySnapshotRevision,
		Row: databaseRowPageRowWire{
			ID: rowID, ContainerPageID: authority.ContainerPageID,
			DataSourceID: authority.DataSourceID, SubitemParentID: subitemParentID,
			Position: authority.Position, Values: values,
			Icon: authority.Icon, IconType: authority.IconType,
			Cover: authority.Cover, CoverPosition: authority.CoverPosition, Favorite: favoriteCount == 1,
			Font: databaseRowPageFont(authority.Font), SmallText: authority.SmallText, FullWidth: authority.FullWidth, TableOfContentsDisplay: normalizedPageTableOfContentsDisplay(&authority.TableOfContentsDisplay),
			Locked: lock.Locked, UnlockedForMe: lock.UnlockedForMe, LockedBy: authority.LockedBy,
			ArchivedAt: authority.ArchivedAt, ArchivedBy: authority.ArchivedBy,
			TrashedAt: databaseRowTrashTime(authority.TrashedAt), TrashedBy: authority.TrashedBy, SourceInTrash: sourceInTrash,
			CreatedAt: databaseRowPageTime(authority.CreatedAt), CreatedBy: authority.CreatedBy,
			UpdatedAt: databaseRowPageTime(authority.UpdatedAt), LastEditedBy: authority.LastEditedBy,
			AuthorityRevision: authority.AuthorityRevision,
		},
		Body: blocks, NextCursor: nextCursor, HasMore: hasMore,
		Properties: properties, AccessRole: pageAccessRoleName(role), WorkspaceID: authority.WorkspaceID,
		DatabaseWritable: databaseWritable, DatabaseLocked: databaseLocked, DatabaseStructureLocked: databaseHeld, HolderLocked: holderLocked, Template: authority.Template, TaskDatabase: taskRoles.Enabled,
		UntitledName: untitledName,
	}
	// A People row's page is its person's profile, which the app opens in its place.
	if snapshot.Person, err = peopleRowPersonOf(app, authority.DataSourceID, rowID); err != nil {
		return databaseRowPageSnapshot{}, err
	}
	// What the database's property access rules keep from this reader, and
	// whether they may change them, as from the database.
	if err := redactDatabaseRowPage(app, principalID, &snapshot); err != nil {
		return databaseRowPageSnapshot{}, err
	}
	if snapshot.ManageAccess, err = propertyAccessManager(app, principalID, authority.DataSourceID, nil); err != nil {
		return databaseRowPageSnapshot{}, err
	}
	if snapshot.Wiki, err = databaseRowPageWiki(app, rowID, authority.DataSourceID, snapshot.Properties); err != nil {
		return databaseRowPageSnapshot{}, err
	}
	if snapshot.RelatedPages, err = databaseRowPageRelatedPages(ctx, app, principalID, snapshot.Row.Values); err != nil {
		return databaseRowPageSnapshot{}, err
	}
	return snapshot, nil
}

// databaseRowPageRelatedPageLimit bounds the rows a page names by title; the
// rest show as the rows they are until opened from the database.
const databaseRowPageRelatedPageLimit = 200

// databaseRowPageRelatedPages names the rows a row's relations hold, for a
// page opened at its own address, away from its database: those this reader
// may read, so a relation shows their names.
func databaseRowPageRelatedPages(ctx context.Context, app core.App, principalID string, encoded json.RawMessage) ([]databaseRowPageRelatedPageWire, error) {
	values := map[string]graphRowValue{}
	if err := json.Unmarshal(encoded, &values); err != nil {
		return nil, nil
	}
	seen := map[string]struct{}{}
	related := []databaseRowPageRelatedPageWire{}
	untitled := newDatabaseUntitledRowNames(ctx, app, principalID)
	for _, propertyID := range workspaceExportSortedKeys(values) {
		cell := values[propertyID]
		if cell.Type != graphValueTypeRelation {
			continue
		}
		ids := []string{}
		if json.Unmarshal(cell.Value, &ids) != nil {
			continue
		}
		for _, id := range ids {
			if _, done := seen[id]; done || len(seen) >= databaseRowPageRelatedPageLimit {
				continue
			}
			seen[id] = struct{}{}
			authority, err := authorizeDatabaseRowPage(app, principalID, id, permissionWorkspaceRead)
			if errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			page := databaseRowPageRelatedPageWire{
				ID: authority.RowID, Title: rowTitleAsRead(app, principalID, authority.RowID, databaseRowPageTitle(app, authority.RowID)),
				Icon: authority.Icon, IconType: authority.IconType,
			}
			// A related row is called after its database's default template, as the row is.
			if !authority.Template {
				if page.UntitledName, err = untitled.of(authority.DataSourceID); err != nil {
					return nil, err
				}
			}
			related = append(related, page)
		}
	}
	return related, nil
}

// databaseRowPageTime is a database page's time as its database's rows
// carry it, to the millisecond, or empty when it has none.
func databaseRowPageTime(value int64) string {
	if value == 0 {
		return ""
	}
	return time.UnixMilli(value).UTC().Format("2006-01-02T15:04:05.000Z")
}

func loadDatabaseRowPageValues(
	app core.App,
	authority databaseRowPageAuthority,
) (json.RawMessage, error) {
	stored, err := loadGraphRowValues(
		app, authority.OwnerID, authority.ContainerPageID, []string{authority.RowID},
	)
	if err != nil {
		return nil, err
	}
	if err := hideTrashedRelationTargets(app, stored); err != nil {
		return nil, err
	}
	values := make(map[string]map[string]any, len(stored))
	for _, value := range stored {
		var decoded any
		if err := json.Unmarshal([]byte(value.Value), &decoded); err != nil {
			return nil, err
		}
		cell := map[string]any{"type": value.Type, "value": decoded}
		if spans := databaseStoredSpans(value.Spans); len(spans) > 0 {
			cell["spans"] = spans
		}
		values[value.PropertyID] = cell
	}
	return json.Marshal(values)
}

func loadDatabaseRowPageBodyWindow(
	app core.App,
	rowID string,
	cursor string,
	limit int,
) ([]storedDatabaseRowPageBodyBlock, error) {
	blocks := []storedDatabaseRowPageBodyBlock{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[parent_id]], [[position]], [[type]], [[content]],
			[[parent_id]] || ':' || [[position]] || ':' || [[id]] AS [[sort_key]]
		FROM {{leafen_blocks}}
		WHERE [[page_id]]={:rowId}
			AND ([[parent_id]] || ':' || [[position]] || ':' || [[id]])>{:cursor}
		ORDER BY [[parent_id]], [[position]], [[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"rowId": rowID, "cursor": cursor, "limit": limit + 1,
	}).All(&blocks)
	return blocks, err
}

// loadDatabaseRowPageFirstBlock reads a body's first block and the first block
// inside it: a card's "페이지 콘텐츠" draws the one, or a callout's words,
// which are its first line (cardPreviewLine in the web app).
func loadDatabaseRowPageFirstBlock(app core.App, rowID string) ([]storedDatabaseRowPageBodyBlock, error) {
	blocks := []storedDatabaseRowPageBodyBlock{}
	for parentID := ""; len(blocks) < 2; parentID = blocks[len(blocks)-1].ID {
		var block storedDatabaseRowPageBodyBlock
		err := app.DB().NewQuery(`
			SELECT [[id]], [[parent_id]], [[position]], [[type]], [[content]], '' AS [[sort_key]]
			FROM {{leafen_blocks}}
			WHERE [[page_id]]={:rowId} AND [[parent_id]]={:parentId}
			ORDER BY [[position]], [[id]]
			LIMIT 1
		`).Bind(dbx.Params{"rowId": rowID, "parentId": parentID}).One(&block)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}
