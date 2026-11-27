package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/reearth/ygo/crdt"
)

// Deleted databases (schema_baseline.json). A source database's block
// leaving its page — deleted, or turned into a simple table — records it;
// a block that shows it again (undo, paste or restore) takes it back out.
// The trash lists them for those who may edit their page, restores one by
// putting its block back at the end of its page, and deletes one forever
// with its rows once no other database relates to it. Thirty days after a
// deletion it is deleted forever the same way; those found without a block
// when the trash arrived wait for the person.

const deletedDatabaseRetention = recoverableBlockTrashRetention

// deletedDatabasesPurgedPerCall bounds the expired databases one call
// deletes forever, so no request carries the whole backlog.
const deletedDatabasesPurgedPerCall = 4

var errDeletedDatabaseRelated = errors.New(errorCodeDeletedDatabaseRelated)

type storedDeletedDatabase struct {
	BlockID       string `db:"block_id"`
	Owner         string `db:"owner"`
	WorkspaceID   string `db:"workspace_id"`
	PageID        string `db:"page_id"`
	DataSourceIDs string `db:"data_source_ids"`
	BlockContent  string `db:"block_content"`
	Title         string `db:"title"`
	DeletedAt     int64  `db:"deleted_at"`
	ExpiresAt     int64  `db:"expires_at"`
}

func registerDeletedDatabaseRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+deletedDatabaseListPath, listDeletedDatabases)
	event.Router.POST(apiRoot+deletedDatabaseRestorePath, restoreDeletedDatabase)
	event.Router.POST(apiRoot+deletedDatabaseDeletePath, deleteDeletedDatabaseForever)
}

// recordDeletedDatabase keeps a source database whose block is leaving its
// page, before the block's records go.
func recordDeletedDatabase(app core.App, blockID, actorID string) error {
	var block struct {
		Owner       string `db:"owner"`
		WorkspaceID string `db:"workspace_id"`
		PageID      string `db:"page_id"`
		ParentID    string `db:"parent_id"`
		Type        string `db:"type"`
		Content     string `db:"content"`
	}
	err := app.DB().NewQuery(`
		SELECT [[owner]],[[workspace_id]],[[page_id]],[[parent_id]],[[type]],[[content]] FROM {{leafen_blocks}} WHERE [[id]]={:id}
	`).Bind(dbx.Params{"id": blockID}).One(&block)
	if errors.Is(err, sql.ErrNoRows) || err == nil && block.Type != graphBlockTypeInlineDatabase {
		return nil
	}
	if err != nil {
		return err
	}
	content, err := parseDatabaseContainerBlockContent(json.RawMessage(block.Content))
	if err != nil || content.LinkedDatabaseSource || content.SourceContainerID != "" || content.DataSourceID == "" {
		return nil
	}
	members, err := json.Marshal(content.dataSourceIDs())
	if err != nil {
		return err
	}
	var title string
	if err := app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": content.DataSourceID}).Row(&title); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := time.Now().UTC()
	if _, err := app.DB().NewQuery(`
		INSERT OR REPLACE INTO {{leafen_deleted_databases}}
			([[block_id]],[[owner]],[[workspace_id]],[[page_id]],[[parent_block_id]],[[data_source_ids]],[[block_content]],
			 [[title]],[[deleted_by]],[[deleted_at]],[[expires_at]])
		VALUES ({:blockId},{:owner},{:workspaceId},{:pageId},{:parentId},{:members},{:content},{:title},{:actor},{:now},{:expires})
	`).Bind(dbx.Params{
		"blockId": blockID, "owner": block.Owner, "workspaceId": block.WorkspaceID, "pageId": block.PageID,
		"parentId": block.ParentID, "members": string(members), "content": block.Content, "title": title,
		"actor": actorID, "now": now.UnixMilli(), "expires": now.Add(deletedDatabaseRetention).UnixMilli(),
	}).Execute(); err != nil {
		return err
	}
	return purgeExpiredDeletedDatabases(app, now.UnixMilli())
}

// reviveDeletedDatabases takes out of the trash the databases a block shows
// again, and tells whether there were any. A data source stays in its
// workspace, so only that workspace's trash is looked through for it. One
// shown again under another block — restored under a new ID, its source
// shown by another block — lets its own public ID go, as one deleted
// forever does: no database stands under it.
func reviveDeletedDatabases(app core.App, workspaceID, blockID string, dataSourceIDs []string) (bool, error) {
	members, err := json.Marshal(dataSourceIDs)
	if err != nil {
		return false, err
	}
	params := dbx.Params{"blockId": blockID, "workspaceId": workspaceID, "members": string(members)}
	where := `[[block_id]]={:blockId} OR ([[workspace_id]]={:workspaceId} AND EXISTS (
			SELECT 1 FROM json_each([[data_source_ids]]) [[member]]
			WHERE [[member]].[[value]] IN (SELECT [[value]] FROM json_each({:members}))))`
	revived := []struct {
		BlockID     string `db:"block_id"`
		WorkspaceID string `db:"workspace_id"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[block_id]],[[workspace_id]] FROM {{leafen_deleted_databases}} WHERE ` + where).
		Bind(params).All(&revived); err != nil || len(revived) == 0 {
		return false, err
	}
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_deleted_databases}} WHERE ` + where).Bind(params).Execute(); err != nil {
		return false, err
	}
	for _, other := range revived {
		if other.BlockID == blockID {
			continue
		}
		if err := dropDeletedDatabasePublicID(app, other.WorkspaceID, other.BlockID); err != nil {
			return false, err
		}
	}
	return true, nil
}

// purgeDeletedDatabase deletes a database in the trash forever, with its
// rows, unless another database still relates to it.
func purgeDeletedDatabase(app core.App, stored storedDeletedDatabase) error {
	members := []string{}
	if err := json.Unmarshal([]byte(stored.DataSourceIDs), &members); err != nil {
		return err
	}
	var related int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_relation_property_targets}}
		WHERE [[target_source_id]] IN (SELECT [[value]] FROM json_each({:members}))
			AND [[source_id]] NOT IN (SELECT [[value]] FROM json_each({:members}))
	`).Bind(dbx.Params{"members": stored.DataSourceIDs}).Row(&related); err != nil {
		return err
	}
	if related > 0 {
		return errDeletedDatabaseRelated
	}
	for _, sourceID := range members {
		// A source a block shows again was revived meanwhile; it stays.
		var shown int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_container_sources}} WHERE [[data_source_id]]={:id}`).
			Bind(dbx.Params{"id": sourceID}).Row(&shown); err != nil {
			return err
		}
		if shown > 0 {
			continue
		}
		if err := deleteDatabaseSourceProjection(app, stored.Owner, stored.WorkspaceID, stored.PageID, sourceID); err != nil {
			return err
		}
	}
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_deleted_databases}} WHERE [[block_id]]={:id}`).
		Bind(dbx.Params{"id": stored.BlockID}).Execute(); err != nil {
		return err
	}
	return dropDeletedDatabasePublicID(app, stored.WorkspaceID, stored.BlockID)
}

// purgeExpiredDeletedDatabases deletes forever a few databases whose time in
// the trash is over; one another database relates to waits.
func purgeExpiredDeletedDatabases(app core.App, now int64) error {
	expired := []storedDeletedDatabase{}
	if err := app.DB().NewQuery(`
		SELECT [[block_id]],[[owner]],[[workspace_id]],[[page_id]],[[data_source_ids]],[[block_content]],[[title]],
			[[deleted_at]],[[expires_at]]
		FROM {{leafen_deleted_databases}} WHERE [[expires_at]] > 0 AND [[expires_at]] <= {:now}
		ORDER BY [[expires_at]],[[block_id]] LIMIT {:limit}
	`).Bind(dbx.Params{"now": now, "limit": deletedDatabasesPurgedPerCall}).All(&expired); err != nil {
		return err
	}
	for _, stored := range expired {
		if err := purgeDeletedDatabase(app, stored); err != nil && !errors.Is(err, errDeletedDatabaseRelated) {
			return err
		}
	}
	return nil
}

// loadDeletedDatabase reads one database of the workspace's trash for a
// person who may edit its page.
func loadDeletedDatabase(app core.App, userID, workspaceID, blockID string) (storedDeletedDatabase, pageAuthorization, error) {
	stored := storedDeletedDatabase{}
	err := app.DB().NewQuery(`
		SELECT [[block_id]],[[owner]],[[workspace_id]],[[page_id]],[[data_source_ids]],[[block_content]],[[title]],
			[[deleted_at]],[[expires_at]]
		FROM {{leafen_deleted_databases}} WHERE [[block_id]]={:blockId} AND [[workspace_id]]={:workspaceId}
	`).Bind(dbx.Params{"blockId": blockID, "workspaceId": workspaceID}).One(&stored)
	if err != nil {
		return stored, pageAuthorization{}, err
	}
	authorization, err := authorizePageBody(app, userID, stored.PageID, permissionWorkspaceWrite)
	return stored, authorization, err
}

func deletedDatabaseRouteError(event *core.RequestEvent, err error, step string) error {
	var request *graphRequestError
	switch {
	case errors.Is(err, sql.ErrNoRows), isPermissionDenied(err), errors.Is(err, errDatabaseRowPageNotFound):
		return apiError(event, http.StatusNotFound, errorCodeDatabaseDataSourceNotFound, "Database not found in the trash.")
	case errors.Is(err, errLegalHoldPreventsPermanentDelete):
		return apiError(event, http.StatusPreconditionFailed, errorCodeLegalHoldPreventsPermanentDelete,
			"An active legal hold keeps this database. It can be deleted once the hold is released.")
	case errors.Is(err, errDeletedDatabaseRelated):
		return apiError(event, http.StatusConflict, errorCodeDeletedDatabaseRelated,
			"Another database relates to this one; remove those relations first.")
	case errors.As(err, &request):
		// A database in a page in the trash moving out of it, or still moving.
		return apiError(event, request.status, request.code, request.message)
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeDatabaseDataSourceConflict,
		"Couldn't change the database in the trash.", "deleted_databases", step, errorCodeDatabaseDataSourceConflict, err)
}

func deleteDeletedDatabaseForever(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	blockID := strings.TrimSpace(event.Request.PathValue("blockId"))
	// One an active legal hold keeps, by its page, stays (page_trash_legal_hold_databases.go).
	if err := refuseHeldTrashedDatabase(event.App, user.Id, workspaceID, blockID); err != nil {
		return deletedDatabaseRouteError(event, err, "delete-legal-hold")
	}
	// A database in a page in the trash leaves that page's body as it goes;
	// a key's call asks its person once the database is found and theirs to
	// change, and any other once its deletion would go through
	// (user_key_acting_ask.go).
	if item, err := loadTrashedDatabaseContainer(event.App, user.Id, workspaceID, blockID); err == nil && item.InTrashedPage {
		persistence, ok := event.App.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
		if !ok || persistence == nil {
			return deletedDatabaseRouteError(event, errors.New("no content persistence"), "delete-persistence")
		}
		if asked, err := askBeforeActing(event); asked {
			return err
		}
		if err := deleteDatabaseInTrashedPageForever(event, persistence, user.Id, item); err != nil {
			return deletedDatabaseRouteError(event, err, "delete-in-trashed-page")
		}
		return event.NoContent(http.StatusNoContent)
	}
	deletion := func(txApp core.App) error {
		stored, _, err := loadDeletedDatabase(txApp, user.Id, workspaceID, blockID)
		if errors.Is(err, sql.ErrNoRows) {
			item, err := loadTrashedDatabaseContainer(txApp, user.Id, workspaceID, blockID)
			if err != nil {
				// A full-page database is in the trash as its page (deleted_database_page_trash.go).
				return deletedDatabaseNotFound(txApp, user.Id, workspaceID, blockID, err)
			}
			return deleteTrashedDatabaseContainerForever(txApp, item)
		}
		if err != nil {
			return err
		}
		return purgeDeletedDatabase(txApp, stored)
	}
	if err := rehearseBeforeAsking(event, deletion); err != nil {
		return deletedDatabaseRouteError(event, err, "delete")
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	err := event.App.RunInTransaction(deletion)
	if err != nil {
		return deletedDatabaseRouteError(event, err, "delete")
	}
	return event.NoContent(http.StatusNoContent)
}

// restoreDeletedDatabase puts the block back at the end of its page, which
// takes the database out of the trash; a database in a page in the trash
// comes back as a full-page database in private pages
// (database_in_trashed_page.go).
func restoreDeletedDatabase(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	blockID := strings.TrimSpace(event.Request.PathValue("blockId"))
	persistence, ok := event.App.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return deletedDatabaseRouteError(event, errors.New("no content persistence"), "restore-persistence")
	}
	stored, authorization, err := loadDeletedDatabase(event.App, user.Id, workspaceID, blockID)
	if errors.Is(err, sql.ErrNoRows) {
		item, err := loadTrashedDatabaseContainer(event.App, user.Id, workspaceID, blockID)
		if err == nil && item.InTrashedPage {
			// A database in a page in the trash comes back as a full-page database.
			pageID, err := restoreDatabaseFromTrashedPage(event, persistence, user.Id, workspaceID, item)
			if err != nil {
				return deletedDatabaseRouteError(event, err, "restore-from-trashed-page")
			}
			return event.JSON(http.StatusOK, deletedDatabaseRestored{PageID: pageID, BlockID: item.RootBlockID})
		}
		if err == nil {
			err = restoreTrashedDatabaseContainer(event, persistence, user.Id, workspaceID, item)
			if isTrashedDatabaseContainerMissing(err) || isPermissionDenied(err) {
				// Listed, but its page went to the trash after it (deleted_database_page_trash.go).
				err = deletedDatabasePageInTrash(event.App, item.PageID, err)
			}
		}
		if isTrashedDatabaseContainerMissing(err) {
			err = deletedDatabaseNotFound(event.App, user.Id, workspaceID, blockID, sql.ErrNoRows)
		}
		if err != nil {
			return deletedDatabaseRouteError(event, err, "restore-container")
		}
		restored := deletedDatabaseRestored{PageID: item.PageID, BlockID: item.ContainerID}
		if item.InPlace {
			// A source kept in its database names its first view there, which
			// the trash opens that database at, as Notion's (seen 2026-11-13).
			if view, err := firstViewOfSourceIn(event.App, item.ContainerID, item.DataSourceID); err == nil {
				restored.ViewID = view
			}
		}
		return event.JSON(http.StatusOK, restored)
	}
	if err != nil {
		return deletedDatabaseRouteError(event, err, "restore-load")
	}
	if authorization.Page.GetBool("in_trash") {
		return deletedDatabaseRouteError(event, deletedDatabasePageInTrash(event.App, stored.PageID, permissionDenied()), "restore-page")
	}
	newBlockID, err := restoreDeletedDatabaseBlock(event.Request.Context(), event.App, persistence, user.Id, stored,
		func(txApp core.App) (pageAuthorization, error) {
			return authorizePageBody(txApp, user.Id, stored.PageID, permissionWorkspaceWrite)
		})
	if err != nil {
		return deletedDatabaseRouteError(event, err, "restore")
	}
	return event.JSON(http.StatusOK, deletedDatabaseRestored{PageID: stored.PageID, BlockID: newBlockID})
}

// restoreDeletedDatabaseBlock puts a deleted database's block back at the
// end of its page, under its own ID unless a block has it now, which takes
// the database out of the trash; it answers the block's ID.
func restoreDeletedDatabaseBlock(
	ctx context.Context, app core.App, persistence *pocketBaseYgoPersistence, actorID string,
	stored storedDeletedDatabase, authorize func(core.App) (pageAuthorization, error),
) (string, error) {
	// A block that still exists under the same ID gets a new one.
	newBlockID := stored.BlockID
	var taken int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_blocks}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": newBlockID}).Row(&taken); err != nil {
		return "", err
	}
	if taken > 0 || strings.HasPrefix(newBlockID, "restored-") {
		newBlockID = security.RandomString(15)
	}
	digest := sha256.Sum256([]byte("deleted-database-restore\x00" + stored.BlockID))
	_, err := persistence.applyContentGraphMutationContext(
		ctx, actorID, stored.PageID,
		graphMutationInput{MutationID: "restore-" + hex.EncodeToString(digest[:8]) + "-" + security.RandomString(8)},
		contentGraphMutationHooks{
			StructuralAppend: true,
			Authorize:        authorize,
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(_ core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				// After the page's last block, as a new database goes.
				order := doc.GetArray(blockOrderRoot(""))
				var afterID *string
				if order.Len() > 0 {
					tail, ok := order.Get(order.Len() - 1).(string)
					if !ok || tail == "" {
						return contentGraphMutationPreparation{}, graphStructureAuthorityError()
					}
					afterID = &tail
				}
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
						ID: newBlockID, AfterID: afterID, Position: "p0000000000",
						Type: graphBlockTypeInlineDatabase, Content: json.RawMessage(stored.BlockContent),
					},
				}}}, nil
			},
		},
	)
	return newBlockID, err
}
