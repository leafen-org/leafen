package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A data source whose 30 days in the trash end while its database keeps
// another source leaves it as its own recoverable database, due at once,
// whose block records the database it came from
// (database_source_trash_in_place_retention.go). Sources trashed before every
// source stayed in its database were kept so for their 30 days; their reads
// and restore still find them by that record.
const dataSourceTrashOriginField = "trashedFromContainerId"

type notionDataSourceTrashState struct {
	ContainerID string         `db:"container_id"`
	OwnerID     string         `db:"owner"`
	WorkspaceID string         `db:"workspace_id"`
	PageID      string         `db:"page_id"`
	DataSource  string         `db:"data_source_id"`
	Title       string         `db:"title"`
	CreatedAt   int64          `db:"created"`
	UpdatedAt   int64          `db:"updated"`
	RootBlockID sql.NullString `db:"root_block_id"`
	// InPlace is a source its live database keeps in the trash where it
	// stands (database_source_trash_in_place.go).
	InPlace bool `db:"in_place"`
	// Deleted is a source of a database whose block left its page, in the
	// trash with it (notion_deleted_database_reads.go).
	Deleted bool `db:"-"`
}

func (state notionDataSourceTrashState) trashed() bool {
	return state.RootBlockID.Valid || state.InPlace || state.Deleted
}

// loadNotionDataSourceTrashState finds the own source container of a data
// source whether it is live, kept in the trash in its database, in
// recoverable trash as a database, or in a deleted database.
func loadNotionDataSourceTrashState(
	app core.App, workspaceID, dataSourceID string,
) (notionDataSourceTrashState, error) {
	rows := []notionDataSourceTrashState{}
	// The source is found by its id; the workspace is read after (+), or
	// SQLite, which keeps no statistics, reads the workspace's every container.
	if err := app.DB().NewQuery(`
		SELECT [[container]].[[id]] AS [[container_id]], [[container]].[[owner]],
			[[container]].[[workspace_id]], [[container]].[[page_id]],
			[[source]].[[id]] AS [[data_source_id]], [[source]].[[title]],
			[[source]].[[created]], [[source]].[[updated]], [[trash]].[[root_block_id]],
			` + databaseSourceTrashedInPlaceSQL("[[source]].[[id]]") + ` AS [[in_place]]
		FROM {{leafen_data_sources}} AS [[source]]
		JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[data_source_id]]=[[source]].[[id]] AND [[membership]].[[source_container_id]]=''
		JOIN {{leafen_database_containers}} AS [[container]]
			ON [[container]].[[id]]=[[membership]].[[container_id]]
			AND [[container]].[[owner]]=[[source]].[[owner]]
			AND [[container]].[[workspace_id]]=[[source]].[[workspace_id]]
			AND [[container]].[[page_id]]=[[source]].[[page_id]]
			AND [[container]].[[kind]]='source'
		LEFT JOIN {{leafen_database_container_trash}} AS [[trash]]
			ON [[trash]].[[container_id]]=[[container]].[[id]]
		WHERE +[[source]].[[workspace_id]]={:workspaceId} AND [[source]].[[id]]={:dataSourceId}
		ORDER BY [[container]].[[id]] LIMIT 2
	`).Bind(dbx.Params{"workspaceId": workspaceID, "dataSourceId": dataSourceID}).All(&rows); err != nil {
		return notionDataSourceTrashState{}, err
	}
	if len(rows) == 0 {
		deleted, found, err := loadNotionDeletedDatabaseOfSource(app, workspaceID, dataSourceID)
		if err != nil {
			return notionDataSourceTrashState{}, err
		}
		if found {
			return deleted.sourceTrashState(app, dataSourceID)
		}
	}
	if len(rows) != 1 {
		return notionDataSourceTrashState{}, errDatabaseRowPageNotFound
	}
	return rows[0], nil
}

// authorizeNotionDataSourceAnyState authorizes a live or trashed data source
// through the page that owns it.
func authorizeNotionDataSourceAnyState(
	app core.App,
	identity notionOAuthBearerIdentity,
	dataSourceID string,
	requirement permissionRequirement,
) (notionDatabaseRowPageAuthorization, notionDataSourceTrashState, error) {
	return authorizeNotionDataSourceAnyPlace(app, identity, dataSourceID, requirement, false)
}

// authorizeNotionDataSourceAnyPlace with trashedPage also finds, for a
// change, one whose database's page is in the trash, as taking a source out
// of the trash there does (database_source_trash_in_place.go).
func authorizeNotionDataSourceAnyPlace(
	app core.App,
	identity notionOAuthBearerIdentity,
	dataSourceID string,
	requirement permissionRequirement,
	trashedPage bool,
) (notionDatabaseRowPageAuthorization, notionDataSourceTrashState, error) {
	state, err := loadNotionDataSourceTrashState(app, identity.WorkspaceID, dataSourceID)
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, state, err
	}
	page, err := authorizePageBody(app, identity.PrincipalID, state.PageID, requirement)
	// A read finds it with its database's page in the trash too
	// (notion_trash_reads.go); a change doesn't.
	if isPermissionDenied(err) || err == nil && (page.Page.GetBool("in_trash") && requirement != permissionWorkspaceRead && !trashedPage ||
		page.Workspace.WorkspaceID != identity.WorkspaceID ||
		!notionOAuthIdentityAllowsPage(app, identity, page)) {
		return notionDatabaseRowPageAuthorization{}, state, errDatabaseRowPageNotFound
	}
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, state, err
	}
	return notionDatabaseRowPageAuthorization{
		Identity: identity, Page: page,
		Container: notionDataSourceParentAuthority{
			ContainerID: state.ContainerID, OwnerID: state.OwnerID, WorkspaceID: state.WorkspaceID,
			PageID: state.PageID, DataSource: state.DataSource, Title: state.Title,
			CreatedAt: state.CreatedAt, UpdatedAt: state.UpdatedAt,
		},
	}, state, nil
}

// notionDataSourceTrashOrigin returns the database a detached trashed source
// came from, read from its recoverable block snapshot.
func notionDataSourceTrashOrigin(app core.App, state notionDataSourceTrashState) (string, error) {
	if !state.trashed() || state.RootBlockID.String != state.ContainerID {
		return "", nil
	}
	origin := ""
	err := app.DB().NewQuery(`
		SELECT COALESCE(json_extract([[block]].[[value]], '$.content.` + dataSourceTrashOriginField + `'), '')
		FROM {{leafen_block_trash_tombstones}} AS [[tombstone]],
			json_each([[tombstone]].[[snapshot_json]], '$.blocks') AS [[block]]
		WHERE [[tombstone]].[[root_block_id]]={:rootId}
			AND json_extract([[block]].[[value]], '$.id')={:rootId}
	`).Bind(dbx.Params{"rootId": state.RootBlockID.String}).Row(&origin)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return origin, err
}

func serveNotionDataSourceTrashChange(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	identity notionOAuthBearerIdentity,
	dataSourceID string,
	inTrash bool,
) error {
	authorization, state, err := authorizeNotionDataSourceAnyPlace(
		event.App, identity, dataSourceID, permissionWorkspaceWrite, !inTrash,
	)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	if inTrash && authorization.Page.Page.GetBool("in_trash") {
		return notionDataSourceMutationError(event, errDatabaseRowPageNotFound)
	}
	origin, err := notionDataSourceTrashOrigin(event.App, state)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	switch {
	// One of a deleted database stays in the trash with it, as Notion's
	// (seen 2026-11-13: in_trash false answers it in the trash).
	case state.Deleted && !inTrash:
	// So does one whose database's block is in the trash (seen 2026-11-13);
	// one that database also keeps in the trash where it stands comes out
	// of it there.
	case !inTrash && !state.InPlace && state.RootBlockID.Valid && origin == "":
		_, err = restoreNotionDataSourceInTrashedDatabase(event.App, state)
	case inTrash && (state.RootBlockID.Valid && origin == "" || state.Deleted):
		// One whose database is in the trash isn't found to trash, as
		// Notion's (seen 2026-11-11).
		return notionTrashedDataSourceNotFound(event, "")
	case inTrash && !state.trashed():
		err = trashNotionDataSource(event, ygo, authorization, false)
	case !inTrash && state.trashed():
		err = restoreNotionDataSource(event, ygo, authorization, state, false)
	}
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	response, err := loadNotionDataSourceResponse(
		event.App, event.Request, identity, dataSourceID, permissionWorkspaceRead,
	)
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func notionDataSourceContainerBlock(doc *crdt.Doc, containerID string) (structuralBlockMetadata, databaseContainerBlockContent, error) {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return structuralBlockMetadata{}, databaseContainerBlockContent{}, err
	}
	entry, exists := metadata[containerID]
	if !exists || entry.Type != graphBlockTypeInlineDatabase {
		return structuralBlockMetadata{}, databaseContainerBlockContent{}, errDatabaseRowPageNotFound
	}
	encoded, err := json.Marshal(entry.Content)
	if err != nil {
		return structuralBlockMetadata{}, databaseContainerBlockContent{}, err
	}
	content, err := parseDatabaseContainerBlockContent(encoded)
	if err != nil || content.LinkedDatabaseSource {
		return structuralBlockMetadata{}, databaseContainerBlockContent{}, errDatabaseRowPageNotFound
	}
	return entry, content, nil
}

// notionDataSourceMembershipBlock rewrites a database block to the given own
// members, keeping its active view when it still belongs to a member.
func notionDataSourceMembershipBlock(
	app core.App, entry structuralBlockMetadata, containerID, pageID, ownerID string, members []string,
) (*graphBlock, error) {
	var position string
	if err := app.DB().NewQuery(`
		SELECT [[position]] FROM {{leafen_blocks}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]={:id}
	`).Bind(dbx.Params{"owner": ownerID, "pageId": pageID, "id": containerID}).Row(&position); err != nil {
		return nil, err
	}
	viewID, _ := entry.Content["viewId"].(string)
	var viewSource string
	err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_views}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": viewID}).Row(&viewSource)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// A view of a linked member stays selected; only a view of the removed
	// own source moves to another own source.
	linkedView := false
	if encoded, err := json.Marshal(entry.Content); err == nil {
		if parsed, err := parseDatabaseContainerBlockContent(encoded); err == nil {
			_, linkedView = parsed.linkedSourceContainers()[viewSource]
		}
	}
	if !slices.Contains(members, viewSource) && !linkedView {
		if err := app.DB().NewQuery(`
			SELECT [[id]] FROM {{leafen_database_views}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[data_source_id]]={:sourceId}
				AND [[container_id]]=''
			ORDER BY [[position]], [[id]] LIMIT 1
		`).Bind(dbx.Params{"owner": ownerID, "pageId": pageID, "sourceId": members[0]}).Row(&viewID); err != nil {
			return nil, refusedUnlessStorage(errDatabaseRowPageConflict, err)
		}
	}
	content := make(map[string]any, len(entry.Content)+1)
	for key, value := range entry.Content {
		content[key] = value
	}
	content["dataSourceId"], content["viewId"] = members[0], viewID
	if len(members) > 1 {
		content["additionalDataSourceIds"] = members[1:]
	} else {
		delete(content, "additionalDataSourceIds")
	}
	withDatabaseTrashedSources(content, members, storedDatabaseTrashedSources(content))
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	block := &graphBlock{ID: containerID, Position: position, Type: graphBlockTypeInlineDatabase, Content: encoded}
	if entry.ParentID != "" {
		parentID := entry.ParentID
		block.ParentID = &parentID
	}
	return block, nil
}

// trashNotionDataSource sends a source to the trash where it stands in its
// database, as Notion's (database_source_trash_in_place.go). One another
// database links stays out of the trash while its database holds another
// source of its own: its 30 days end by leaving that database, which would
// leave the link pointing at nothing. withViews takes its views along, as
// the database's 데이터 소스 관리 does.
func trashNotionDataSource(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	authorization notionDatabaseRowPageAuthorization,
	withViews bool,
) error {
	identity := authorization.Identity
	container := authorization.Container
	hooks := contentGraphMutationHooks{Authorize: authorizeNotionPageMutation(identity, container.PageID)}
	input := graphMutationInput{MutationID: "notion-public-data-source-trash:" + newDiagnosticReference()}
	hooks.Prepare = func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
		entry, content, err := notionDataSourceContainerBlock(doc, container.ContainerID)
		if err != nil {
			return contentGraphMutationPreparation{}, err
		}
		if !slices.Contains(content.dataSourceIDs(), container.DataSource) || content.trashedInPlace(container.DataSource) {
			return contentGraphMutationPreparation{}, errDatabaseRowPageConflict
		}
		if len(content.ownDataSourceIDs()) > 1 {
			if err := rejectLinkedDataSourceDetach(txApp, container.ContainerID, container.DataSource); err != nil {
				return contentGraphMutationPreparation{}, err
			}
		}
		return trashNotionDataSourceInPlace(txApp, entry, content, container, withViews)
	}
	_, err := ygo.applyContentGraphMutationContext(event.Request.Context(), identity.PrincipalID, container.PageID, input, hooks)
	return err
}

// persistDetachedDataSourceTrash records the detached source as a trashed
// database right after its former database so block-trash retention owns it,
// and returns its block.
func persistDetachedDataSourceTrash(
	app core.App,
	page pageAuthorization,
	actorID, originContainerID, successorID, parentID, dataSourceID, viewID string,
) (string, error) {
	var position string
	if err := app.DB().NewQuery(`SELECT [[position]] FROM {{leafen_blocks}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": originContainerID}).Row(&position); err != nil {
		return "", err
	}
	rootID := uuid.NewString()
	content, err := json.Marshal(map[string]any{
		"dataSourceId": dataSourceID, "viewId": viewID, dataSourceTrashOriginField: originContainerID,
	})
	if err != nil {
		return "", err
	}
	snapshot := recoverableBlockTrashSnapshot{Format: 1, RootID: rootID, Blocks: []recoverableBlockTrashBlock{{
		ID: rootID, ParentID: parentID, AfterID: originContainerID, Position: position,
		Type: graphBlockTypeInlineDatabase, Content: content,
	}}}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	deletedAt, expiresAt := now.UnixMilli(), now.Add(recoverableBlockTrashRetention).UnixMilli()
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_block_trash_tombstones}}
			([[root_block_id]],[[owner_id]],[[workspace_id]],[[page_id]],[[parent_id]],
			 [[predecessor_id]],[[successor_id]],[[snapshot_json]],[[block_count]],[[byte_size]],
			 [[deleted_by]],[[deleted_at]],[[expires_at]])
		VALUES ({:rootId},{:ownerId},{:workspaceId},{:pageId},{:parentId},{:predecessorId},{:successorId},
			{:snapshot},1,{:bytes},{:actorId},{:deletedAt},{:expiresAt})
	`).Bind(dbx.Params{
		"rootId": rootID, "ownerId": page.Workspace.OwnerID, "workspaceId": page.Workspace.WorkspaceID,
		"pageId": page.Page.Id, "parentId": parentID, "predecessorId": originContainerID, "successorId": successorID,
		"snapshot": string(encoded), "bytes": len(encoded), "actorId": actorID,
		"deletedAt": deletedAt, "expiresAt": expiresAt,
	}).Execute(); err != nil {
		return "", err
	}
	parsed, err := parseDatabaseContainerBlockContent(content)
	if err != nil {
		return "", err
	}
	if err := projectDatabaseContainerBlock(
		app, page.Workspace.OwnerID, page.Workspace.WorkspaceID, page.Page.Id,
		rootID, parentID, position, parsed,
	); err != nil {
		return "", err
	}
	if err := markRecoverableDatabaseContainerTrash(
		app, page.Workspace.OwnerID, page.Workspace.WorkspaceID, page.Page.Id, actorID,
		snapshot, deletedAt, expiresAt,
	); err != nil {
		return "", err
	}
	notifyRecoverableBlockTrashRetentionAfterCommit(app)
	return rootID, nil
}

// notionDatabaseDetachedSource is a source that left a database as a
// database of its own in the trash.
type notionDatabaseDetachedSource struct {
	ID         string `db:"id"`
	Title      string `db:"title"`
	TitleSpans string `db:"title_spans"`
}

// loadNotionDatabaseDetachedSources reads the sources that left a database
// as databases of their own in the trash, in the order they left. The
// page's trash entries are walked by their root IDs alone, from the page's
// trash index, and only those that are a source's own trash are read whole
// (CROSS JOIN keeps that order): every database read of the page doesn't
// read the page's deleted blocks, nor every database's sources (2026-11-18).
func loadNotionDatabaseDetachedSources(app core.App, container notionDatabaseContainer) ([]notionDatabaseDetachedSource, error) {
	detached := []notionDatabaseDetachedSource{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[id]], [[source]].[[title]], [[source]].[[title_spans]]
		FROM {{leafen_block_trash_tombstones}} AS [[page_trash]]
		CROSS JOIN {{leafen_database_container_trash}} AS [[trash]]
			ON [[trash]].[[root_block_id]] = [[page_trash]].[[root_block_id]]
			AND [[trash]].[[container_id]] = [[page_trash]].[[root_block_id]] AND [[trash]].[[kind]] = 'source'
		CROSS JOIN {{leafen_block_trash_tombstones}} AS [[tombstone]]
			ON [[tombstone]].[[root_block_id]] = [[trash]].[[root_block_id]]
		JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[container_id]] = [[trash]].[[container_id]] AND [[membership]].[[source_container_id]] = ''
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[membership]].[[data_source_id]]
		WHERE [[page_trash]].[[workspace_id]] = {:workspaceId} AND [[page_trash]].[[page_id]] = {:pageId}
			AND [[tombstone]].[[expires_at]] > {:now}
			AND json_extract([[tombstone]].[[snapshot_json]], '$.blocks[0].content.` + dataSourceTrashOriginField + `') = {:containerId}
		ORDER BY [[tombstone]].[[deleted_at]], [[tombstone]].[[root_block_id]]
	`).Bind(dbx.Params{
		"workspaceId": container.WorkspaceID, "pageId": container.PageID, "containerId": container.ID,
		"now": time.Now().UTC().UnixMilli(),
	}).All(&detached)
	return detached, err
}
