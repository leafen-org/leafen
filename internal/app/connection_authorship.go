package app

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// What a public connection makes or edits reads in the public API as made
// or edited by the connection's bot, as Notion's does (seen 2026-09-29: a
// page an integration creates, and its blocks, read created_by and
// last_edited_by as the integration's bot). The person who connected it
// stays the author everywhere else: the app, notifications and "만든 사람"
// read the person, as decided for Leafen.
//
// The connection sits beside the person, by object: the grant that made it,
// and the grant that last edited it with the object's edit mark (its last
// edited time as stored) right after that edit. Any later edit moves the
// mark, so the object reads as the person's again without every other write
// having to clear it. A personal access token acts as its person.

const (
	connectionAuthoredPage     = "page"
	connectionAuthoredDatabase = "database"
	connectionAuthoredRow      = "row"
	connectionAuthoredBlock    = "block"
	connectionAuthoredComment  = "comment"
	connectionAuthoredView     = "view"
)

// notionConnectionGrant is the grant of a public connection acting through
// the API, or "" for a personal access token, which acts as its person.
func notionConnectionGrant(identity notionOAuthBearerIdentity) string {
	if identity.ClientID == "personal-access-token" {
		return ""
	}
	return identity.GrantID
}

// recordConnectionAuthorship notes that the identity's connection made
// (created) or edited an object, whose edit mark is now mark.
func recordConnectionAuthorship(
	app core.App, identity notionOAuthBearerIdentity, kind, objectID string, created bool, mark string,
) error {
	grant := notionConnectionGrant(identity)
	if grant == "" || objectID == "" {
		return nil
	}
	createdGrant := ""
	if created {
		createdGrant = grant
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_connection_authorship}} ([[kind]], [[object_id]], [[created_grant]], [[edited_grant]], [[edited_mark]])
		VALUES ({:kind}, {:id}, {:created}, {:grant}, {:mark})
		ON CONFLICT ([[kind]], [[object_id]]) DO UPDATE SET
			[[created_grant]] = CASE WHEN [[created_grant]] = '' THEN excluded.[[created_grant]] ELSE [[created_grant]] END,
			[[edited_grant]] = excluded.[[edited_grant]],
			[[edited_mark]] = excluded.[[edited_mark]]
	`).Bind(dbx.Params{"kind": kind, "id": objectID, "created": createdGrant, "grant": grant, "mark": mark}).Execute()
	return err
}

// connectionAuthorIDs reads an object's public authors: its person's
// Notion-compatible IDs, or the bot of the connection that made it and, while
// mark is still the one its last edit left, of the connection that edited it.
func connectionAuthorIDs(app core.App, kind, objectID, mark, createdBy, lastEditedBy string) (string, string, error) {
	created, err := notionCompatibleUserID(createdBy)
	if err != nil {
		return "", "", err
	}
	edited, err := notionCompatibleUserID(lastEditedBy)
	if err != nil {
		return "", "", err
	}
	createdGrant, editedGrant, err := connectionAuthorGrants(app, kind, objectID, mark)
	if err != nil {
		return "", "", err
	}
	if createdGrant != "" {
		created = notionOAuthBotID(createdGrant)
	}
	if editedGrant != "" {
		edited = notionOAuthBotID(editedGrant)
	}
	return created, edited, nil
}

// connectionAuthorGrants reads the connections an object's public authors
// are: the one that made it, and the one that edited it while mark is still
// the one its last edit left; "" where its person is.
func connectionAuthorGrants(app core.App, kind, objectID, mark string) (string, string, error) {
	stored := struct {
		CreatedGrant string `db:"created_grant"`
		EditedGrant  string `db:"edited_grant"`
		EditedMark   string `db:"edited_mark"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[created_grant]], [[edited_grant]], [[edited_mark]] FROM {{leafen_connection_authorship}}
		WHERE [[kind]] = {:kind} AND [[object_id]] = {:id}
	`).Bind(dbx.Params{"kind": kind, "id": objectID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	if stored.EditedMark != mark {
		stored.EditedGrant = ""
	}
	return stored.CreatedGrant, stored.EditedGrant, nil
}

// notionBlockEditMark is the edit mark of an object whose last edit is a
// stored millisecond time: a block, a row's page or a database.
func notionBlockEditMark(updated int64) string {
	return strconv.FormatInt(updated, 10)
}

// notionPageAuthorIDs reads a page's public authors; its edit mark is its
// stored "updated", the time its last_edited_time shows.
func notionPageAuthorIDs(app core.App, page *core.Record) (string, string, error) {
	return connectionAuthorIDs(app, connectionAuthoredPage, page.Id, page.GetString("updated"),
		page.GetString("created_by"), page.GetString("last_edited_by"))
}

type connectionAuthorContextKey struct{}

// withConnectionAuthor marks a request as a public connection's, so the
// graph writes it makes record the connection.
func withConnectionAuthor(ctx context.Context, identity notionOAuthBearerIdentity) context.Context {
	if notionConnectionGrant(identity) == "" {
		// A personal access token writes as its person; only the page's
		// history names it (page_history_connections.go).
		if identity.ClientID == "personal-access-token" {
			return withPageHistoryCallMoment(context.WithValue(ctx, personalAccessTokenContextKey{}, identity))
		}
		return ctx
	}
	// What one call writes is one moment in its pages' histories
	// (page_history_moments.go).
	return withPageHistoryCallMoment(context.WithValue(ctx, connectionAuthorContextKey{}, identity))
}

// personalAccessTokenContextKey carries the personal access token a request
// writes through.
type personalAccessTokenContextKey struct{}

func connectionAuthorFrom(ctx context.Context) (notionOAuthBearerIdentity, bool) {
	if ctx == nil {
		return notionOAuthBearerIdentity{}, false
	}
	identity, ok := ctx.Value(connectionAuthorContextKey{}).(notionOAuthBearerIdentity)
	return identity, ok
}

// withConnectionGraphAuthorship makes a public connection's graph write
// record what it changed as the connection's: its blocks and views (made if
// new, else edited), and the page or database row whose body it changed. It
// records before the write's own AfterApply, so a response that renders the
// write shows the bot; what the write left as it was stays its author's. Its
// steps read and record only, so the write says what it changed as it would
// without them (StepsKeepGraph): before, every write of an agent said
// nothing, and an agent's one value in a database of 5,000 rows had the
// page's open tabs and every page linking it read their 51 windows again,
// some 7 seconds on a NAS-class server (2026-11-25).
func withConnectionGraphAuthorship(
	ctx context.Context, pageID string, input graphMutationInput, hooks contentGraphMutationHooks,
) contentGraphMutationHooks {
	identity, ok := connectionAuthorFrom(ctx)
	if !ok {
		return hooks
	}
	blockIDs, viewIDs := []string{}, []string{}
	seen := map[string]struct{}{}
	note := func(id string) {
		if _, dup := seen[id]; id != "" && !dup {
			seen[id] = struct{}{}
			blockIDs = append(blockIDs, id)
		}
	}
	marks := func(app core.App, owner string) (map[string]string, string, string, error) {
		blocks := map[string]string{}
		for _, blockID := range blockIDs {
			var updated int64
			err := app.DB().NewQuery(`
				SELECT [[updated]] FROM {{leafen_blocks}} WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]={:id}
			`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "id": blockID}).Row(&updated)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, "", "", err
			}
			blocks[blockID] = notionBlockEditMark(updated)
		}
		page := ""
		if record, err := app.FindRecordById(pagesCollection, pageID); err == nil {
			page = record.GetString("updated")
		}
		row := ""
		var rowUpdated int64
		err := app.DB().NewQuery(`
			SELECT [[updated]] FROM {{leafen_database_row_pages}} WHERE [[row_id]]={:id} LIMIT 1
		`).Bind(dbx.Params{"id": pageID}).Row(&rowUpdated)
		if err == nil {
			row = notionBlockEditMark(rowUpdated)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, "", "", err
		}
		return blocks, page, row, nil
	}
	viewMarks := func(app core.App) (map[string]string, error) {
		views := map[string]string{}
		for _, viewID := range viewIDs {
			var updated int64
			err := app.DB().NewQuery(`
				SELECT [[updated]] FROM {{leafen_database_views}} WHERE [[id]]={:id}
			`).Bind(dbx.Params{"id": viewID}).Row(&updated)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			views[viewID] = notionBlockEditMark(updated)
		}
		return views, nil
	}
	beforeBlocks, beforeViews, beforePage, beforeRow := map[string]string{}, map[string]string{}, "", ""
	before := hooks.BeforeApplyOperations
	hooks.StepsKeepGraph = hooks.StepsKeepGraph || before == nil && hooks.AfterApply == nil
	hooks.BeforeApplyOperations = func(app core.App, current pageAuthorization, doc *crdt.Doc, operations []graphOperation) error {
		if before != nil {
			if err := before(app, current, doc, operations); err != nil {
				return err
			}
		}
		// The blocks the write changes, as it applies them (a preparation
		// may have planned them inside the write).
		for _, operation := range operations {
			if operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil {
				note(operation.Block.ID)
			}
			if operation.Kind == graphOperationKindUpsertView && operation.View != nil && !slices.Contains(viewIDs, operation.View.ID) {
				viewIDs = append(viewIDs, operation.View.ID)
			}
		}
		for _, update := range input.TextUpdates {
			note(update.BlockID)
		}
		var err error
		if beforeBlocks, beforePage, beforeRow, err = marks(app, current.Workspace.OwnerID); err != nil {
			return err
		}
		beforeViews, err = viewMarks(app)
		return err
	}
	after := hooks.AfterApply
	hooks.AfterApply = func(app core.App, current pageAuthorization, doc *crdt.Doc) error {
		afterBlocks, afterPage, afterRow, err := marks(app, current.Workspace.OwnerID)
		if err != nil {
			return err
		}
		for _, blockID := range blockIDs {
			mark, present := afterBlocks[blockID]
			previous, existed := beforeBlocks[blockID]
			if !present || existed && previous == mark {
				continue
			}
			if err := recordConnectionAuthorship(app, identity, connectionAuthoredBlock, blockID, !existed, mark); err != nil {
				return err
			}
		}
		afterViews, err := viewMarks(app)
		if err != nil {
			return err
		}
		for viewID, mark := range afterViews {
			previous, existed := beforeViews[viewID]
			if existed && previous == mark {
				continue
			}
			if err := recordConnectionAuthorship(app, identity, connectionAuthoredView, viewID, !existed, mark); err != nil {
				return err
			}
		}
		if afterPage != "" && afterPage != beforePage {
			if err := recordConnectionAuthorship(app, identity, connectionAuthoredPage, pageID, false, afterPage); err != nil {
				return err
			}
		}
		if afterRow != "" && afterRow != beforeRow {
			if err := recordConnectionAuthorship(app, identity, connectionAuthoredRow, pageID, false, afterRow); err != nil {
				return err
			}
		}
		if after != nil {
			return after(app, current, doc)
		}
		return nil
	}
	return hooks
}

// recordConnectionPage notes a page a connection made, or edited when its
// edit mark moved from before.
func recordConnectionPage(app core.App, identity notionOAuthBearerIdentity, page *core.Record, created bool, before string) error {
	if page == nil || !created && page.GetString("updated") == before {
		return nil
	}
	return recordConnectionAuthorship(app, identity, connectionAuthoredPage, page.Id, created, page.GetString("updated"))
}

// connectionStoredMark reads a row's page's or a database's edit mark, the
// stored millisecond time its last edit left, or "" when there is none.
func connectionStoredMark(app core.App, kind, objectID string) (string, error) {
	query := `SELECT [[updated]] FROM {{leafen_database_row_pages}} WHERE [[row_id]]={:id} LIMIT 1`
	if kind == connectionAuthoredDatabase {
		query = `SELECT [[updated]] FROM {{leafen_database_containers}} WHERE [[id]]={:id} LIMIT 1`
	}
	var updated int64
	err := app.DB().NewQuery(query).Bind(dbx.Params{"id": objectID}).Row(&updated)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return notionBlockEditMark(updated), nil
}

// recordConnectionStoredMark notes a row's page or a database a connection
// made, or edited when its edit mark moved from before.
func recordConnectionStoredMark(
	app core.App, identity notionOAuthBearerIdentity, kind, objectID string, created bool, before string,
) error {
	if notionConnectionGrant(identity) == "" || objectID == "" {
		return nil
	}
	mark, err := connectionStoredMark(app, kind, objectID)
	if err != nil || mark == "" || !created && mark == before {
		return err
	}
	return recordConnectionAuthorship(app, identity, kind, objectID, created, mark)
}
