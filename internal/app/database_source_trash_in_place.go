package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A data source goes to the trash where it stands: its database stays where
// it is with the source in it, as Notion's (seen 2026-11-11: a database's only
// data source sent to the trash leaves the database out of the trash, its
// block on its page, its view listed and read and its name changeable, the
// source and its pages in_trash, a new source addable; taking the source out
// brings it all back, and trashing and restoring the database leaves the
// source where it was; one of three sent there stays one of the database's
// data_sources, its view tab and rows shown, an inline database's tab with
// the "원본 데이터베이스가 휴지통으로 이동되어…" band, its name kept apart from
// the database's, the trash listing it under the database and its restore
// bringing it back in its place, while 데이터 소스 관리 and a view's
// 데이터베이스 list leave it out). Its database block lists such own sources in
// trashedDataSourceIds; databaseSourceNotTrashedSQL and
// databaseSourceIsTrashed read the list, so every reader takes such a source
// as in the trash.

const databaseTrashedSourcesField = "trashedDataSourceIds"

// A source sent to the trash from the database's 데이터 소스 관리 takes its
// views along, as Notion's says it does ("데이터 소스 및 보기 1개가 휴지통으로
// 이동됩니다", seen 2026-11-11): its view tabs leave and the database shows a
// view of a source it keeps, with no band. Its database block lists such
// sources in trashedWithViewsDataSourceIds as well, so readers leave their
// views out; taking the source out of the trash brings them back. A source
// the public API sends there keeps its tabs and band.
const databaseTrashedWithViewsField = "trashedWithViewsDataSourceIds"

// databaseSourceTrashedInPlaceSQL holds for a data source its own database
// on its page keeps in the trash where it stands: the source's own
// membership in the database whose block lists it. A block found by its page
// and kind keeps the check to that page's databases.
func databaseSourceTrashedInPlaceSQL(dataSourceExpression string) string {
	return `EXISTS (
		SELECT 1
		FROM {{leafen_data_sources}} AS [[in_place_source]]
		JOIN {{leafen_blocks}} AS [[in_place_block]]
			ON [[in_place_block]].[[page_id]] = [[in_place_source]].[[page_id]]
			AND [[in_place_block]].[[type]] = 'inline_database'
			AND [[in_place_block]].[[owner]] = [[in_place_source]].[[owner]]
		JOIN {{leafen_database_containers}} AS [[in_place_container]]
			ON [[in_place_container]].[[block_id]] = [[in_place_block]].[[id]]
			AND [[in_place_container]].[[page_id]] = [[in_place_block]].[[page_id]]
			AND [[in_place_container]].[[owner]] = [[in_place_block]].[[owner]]
			AND [[in_place_container]].[[kind]] = 'source'
		JOIN {{leafen_database_container_sources}} AS [[in_place_membership]]
			ON [[in_place_membership]].[[container_id]] = [[in_place_container]].[[id]]
			AND [[in_place_membership]].[[data_source_id]] = [[in_place_source]].[[id]]
			AND [[in_place_membership]].[[source_container_id]] = ''
		WHERE [[in_place_source]].[[id]] = ` + dataSourceExpression + `
			AND instr([[in_place_block]].[[content]], '"` + databaseTrashedSourcesField + `"') > 0
			AND EXISTS (
				SELECT 1 FROM json_each([[in_place_block]].[[content]], '$.` + databaseTrashedSourcesField + `') AS [[in_place_listed]]
				WHERE [[in_place_listed]].[[value]] = [[in_place_source]].[[id]]
			)
	)`
}

// databaseSourceGoneWithViewsSQL holds for a data source its database keeps
// in the trash with its views gone (databaseTrashedWithViewsField), which the
// database's name and the API's list of its sources leave out, as Notion's
// (seen 2026-11-19), its block in the trash alone listing it still
// (databaseBlockContentSQL). The container's expression is a bound value or
// a qualified column.
func databaseSourceGoneWithViewsSQL(containerExpression, dataSourceExpression string) string {
	content := databaseBlockContentSQL("[[gone_container]]", "[[gone_block]]")
	return `EXISTS (
		SELECT 1 FROM {{leafen_database_containers}} AS [[gone_container]]
		LEFT JOIN {{leafen_blocks}} AS [[gone_block]] ON [[gone_block]].[[id]] = [[gone_container]].[[block_id]]
			AND [[gone_block]].[[page_id]] = [[gone_container]].[[page_id]]
		WHERE [[gone_container]].[[id]] = ` + containerExpression + `
			AND instr(COALESCE(` + content + `, ''), '"` + databaseTrashedWithViewsField + `"') > 0
			AND EXISTS (
				SELECT 1 FROM json_each(` + content + `, '$.` + databaseTrashedWithViewsField + `') AS [[gone_listed]]
				WHERE [[gone_listed]].[[value]] = ` + dataSourceExpression + `
			)
	)`
}

// trashedInPlace reports whether the database keeps an own source of it in
// the trash where it stands.
func (content databaseContainerBlockContent) trashedInPlace(dataSourceID string) bool {
	return !content.LinkedDatabaseSource && slices.Contains(content.TrashedDataSourceIDs, dataSourceID) &&
		slices.Contains(content.dataSourceIDs(), dataSourceID)
}

// liveOwnDataSourceIDs is the database's own sources out of the trash.
func (content databaseContainerBlockContent) liveOwnDataSourceIDs() []string {
	return slices.DeleteFunc(content.ownDataSourceIDs(), content.trashedInPlace)
}

// validateDatabaseTrashedSources keeps the list of sources in the trash a
// list of names; one that is no own member of the database means nothing.
func validateDatabaseTrashedSources(raw map[string]json.RawMessage) error {
	for _, field := range []string{databaseTrashedSourcesField, databaseTrashedWithViewsField} {
		encoded, present := raw[field]
		if !present {
			continue
		}
		listed := []string{}
		if err := json.Unmarshal(encoded, &listed); err != nil || listed == nil {
			return graphInputError(errorCodeGraphInvalid, "Check the database's data source list.")
		}
		for _, id := range listed {
			if id == "" || len(id) > maxGraphIdentifierLength {
				return graphInputError(errorCodeGraphInvalid, "Check the database's data source list.")
			}
		}
	}
	return nil
}

// withDatabaseTrashedSources keeps in stored block content only the listed
// sources that are still own members, and of the sources whose views went to
// the trash with them only those still in the trash.
func withDatabaseTrashedSources(content map[string]any, own []string, trashed []string) {
	kept := []string{}
	for _, id := range trashed {
		if slices.Contains(own, id) && !slices.Contains(kept, id) {
			kept = append(kept, id)
		}
	}
	withViews := []string{}
	for _, id := range storedDatabaseListedSources(content, databaseTrashedWithViewsField) {
		if slices.Contains(kept, id) && !slices.Contains(withViews, id) {
			withViews = append(withViews, id)
		}
	}
	if len(withViews) == 0 {
		delete(content, databaseTrashedWithViewsField)
	} else {
		content[databaseTrashedWithViewsField] = withViews
	}
	if len(kept) == 0 {
		delete(content, databaseTrashedSourcesField)
		return
	}
	content[databaseTrashedSourcesField] = kept
}

// storedDatabaseTrashedSources reads the list from stored block content.
func storedDatabaseTrashedSources(content map[string]any) []string {
	return storedDatabaseListedSources(content, databaseTrashedSourcesField)
}

// storedDatabaseListedSources reads one list of sources from stored block
// content.
func storedDatabaseListedSources(content map[string]any, field string) []string {
	listed := []string{}
	values, _ := content[field].([]any)
	for _, value := range values {
		if id, ok := value.(string); ok {
			listed = append(listed, id)
		}
	}
	if typed, ok := content[field].([]string); ok {
		listed = append(listed, typed...)
	}
	return listed
}

// notionDatabaseBlockWithTrashedSources rewrites a database block to keep
// the given own sources in the trash where they stand. With viewsGone, that
// source's views go to the trash with it, and a database showing one of them
// shows a view of a source it keeps instead.
func notionDatabaseBlockWithTrashedSources(
	app core.App, entry structuralBlockMetadata, containerID, pageID, ownerID string, trashed []string,
	viewsGone string,
) (*graphBlock, error) {
	var position string
	if err := app.DB().NewQuery(`
		SELECT [[position]] FROM {{leafen_blocks}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]={:id}
	`).Bind(dbx.Params{"owner": ownerID, "pageId": pageID, "id": containerID}).Row(&position); err != nil {
		return nil, err
	}
	content := make(map[string]any, len(entry.Content)+1)
	for key, value := range entry.Content {
		content[key] = value
	}
	encoded, err := json.Marshal(entry.Content)
	if err != nil {
		return nil, err
	}
	parsed, err := parseDatabaseContainerBlockContent(encoded)
	if err != nil {
		return nil, err
	}
	if viewsGone != "" {
		content[databaseTrashedWithViewsField] = append(
			storedDatabaseListedSources(content, databaseTrashedWithViewsField), viewsGone,
		)
		if err := selectDatabaseViewKept(app, content, containerID, pageID, ownerID, viewsGone, trashed, parsed); err != nil {
			return nil, err
		}
	}
	withDatabaseTrashedSources(content, parsed.ownDataSourceIDs(), trashed)
	if encoded, err = json.Marshal(content); err != nil {
		return nil, err
	}
	block := &graphBlock{ID: containerID, Position: position, Type: graphBlockTypeInlineDatabase, Content: encoded}
	if entry.ParentID != "" {
		parentID := entry.ParentID
		block.ParentID = &parentID
	}
	return block, nil
}

// trashNotionDataSourceInPlace is the change that keeps the source in its
// database in the trash, with its views when withViews.
func trashNotionDataSourceInPlace(
	txApp core.App, entry structuralBlockMetadata, content databaseContainerBlockContent,
	container notionDataSourceParentAuthority, withViews bool,
) (contentGraphMutationPreparation, error) {
	viewsGone := ""
	if withViews {
		viewsGone = container.DataSource
	}
	block, err := notionDatabaseBlockWithTrashedSources(
		txApp, entry, container.ContainerID, container.PageID, container.OwnerID,
		append(slices.Clone(content.TrashedDataSourceIDs), container.DataSource), viewsGone,
	)
	if err != nil {
		return contentGraphMutationPreparation{}, err
	}
	return contentGraphMutationPreparation{Operations: []graphOperation{{
		Kind: graphOperationKindUpsertBlock, Block: block,
	}}}, nil
}

// restoreNotionDataSourceInPlace takes a source its database keeps in the
// trash out of it, where it stands — on a page in the trash too, where it
// reads in the trash until the page comes out, as Notion's full-page
// database (seen 2026-11-11).
func restoreNotionDataSourceInPlace(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	identity notionOAuthBearerIdentity,
	state notionDataSourceTrashState,
) error {
	_, err := ygo.applyContentGraphMutationContext(
		event.Request.Context(), identity.PrincipalID, state.PageID,
		graphMutationInput{MutationID: "notion-public-data-source-restore:" + newDiagnosticReference()},
		contentGraphMutationHooks{
			AllowTrashedPage: true,
			Authorize: func(app core.App) (pageAuthorization, error) {
				current, err := authorizePageBody(app, identity.PrincipalID, state.PageID, permissionWorkspaceWrite)
				if isPermissionDenied(err) || err == nil && (current.Workspace.WorkspaceID != identity.WorkspaceID ||
					!notionOAuthIdentityAllowsPage(app, identity, current)) {
					return pageAuthorization{}, graphNotFoundError()
				}
				return current, err
			},
			Prepare: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				entry, content, err := notionDataSourceContainerBlock(doc, state.ContainerID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				trashed := slices.DeleteFunc(slices.Clone(content.TrashedDataSourceIDs), func(id string) bool {
					return id == state.DataSource
				})
				block, err := notionDatabaseBlockWithTrashedSources(
					txApp, entry, state.ContainerID, state.PageID, state.OwnerID, trashed, "",
				)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind: graphOperationKindUpsertBlock, Block: block,
				}}}, nil
			},
		},
	)
	return err
}

// databaseSourceTrashedInPlace reports whether a data source's database
// keeps it in the trash where it stands.
func databaseSourceTrashedInPlace(app core.App, dataSourceID string) (bool, error) {
	var exists int
	err := app.DB().NewQuery(`SELECT ` + databaseSourceTrashedInPlaceSQL("{:dataSourceId}")).
		Bind(dbx.Params{"dataSourceId": dataSourceID}).Row(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return exists == 1, err
}

// notionTrashedDataSourceNotFound answers a request only a data source out
// of the trash serves, about one in it, as a data source Notion's finds
// none of (seen 2026-11-11: the views listed by it, a new view of it, its
// query).
func notionTrashedDataSourceNotFound(event *core.RequestEvent, publicID string) error {
	noteNotionAskedObject(event, "data_source", publicID)
	return notionCompatibilityError(
		event, http.StatusNotFound, notionErrorObjectNotFound, "There's no data_source with the requested ID.",
	)
}

// restoreNotionDataSourceInTrashedDatabase takes a source its database keeps
// in the trash where it stands out of it while the database itself is in the
// trash: the database comes back with the source out of the trash, as
// Notion's (seen 2026-11-11: the source reads in_trash until its database is
// taken out, and is out of the trash then). found is false when the trashed
// database keeps no such source.
func restoreNotionDataSourceInTrashedDatabase(app core.App, state notionDataSourceTrashState) (bool, error) {
	tombstone, found, err := loadRecoverableBlockTrash(app, state.RootBlockID.String)
	if err != nil || !found || tombstone.PageID != state.PageID {
		return false, err
	}
	snapshot, err := parseRecoverableBlockTrashSnapshot(tombstone)
	if err != nil {
		return false, err
	}
	for index, block := range snapshot.Blocks {
		if block.ID != state.ContainerID || block.Type != graphBlockTypeInlineDatabase {
			continue
		}
		parsed, err := parseDatabaseContainerBlockContent(block.Content)
		if err != nil || !parsed.trashedInPlace(state.DataSource) {
			return false, err
		}
		content := map[string]any{}
		if err := json.Unmarshal(block.Content, &content); err != nil {
			return false, err
		}
		withDatabaseTrashedSources(content, parsed.ownDataSourceIDs(), slices.DeleteFunc(
			slices.Clone(parsed.TrashedDataSourceIDs), func(id string) bool { return id == state.DataSource },
		))
		if snapshot.Blocks[index].Content, err = json.Marshal(content); err != nil {
			return false, err
		}
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return false, err
		}
		_, err = app.DB().NewQuery(`
			UPDATE {{leafen_block_trash_tombstones}} SET [[snapshot_json]]={:snapshot}, [[byte_size]]={:bytes}
			WHERE [[root_block_id]]={:rootId}
		`).Bind(dbx.Params{"snapshot": string(encoded), "bytes": len(encoded), "rootId": tombstone.RootBlockID}).Execute()
		return err == nil, err
	}
	return false, nil
}

// selectDatabaseViewKept moves a database that shows a view of the source
// whose views go to the trash to its first view, in tab order, of a source
// it keeps: its own out of the trash, or one it links. With none it keeps
// showing what it showed.
func selectDatabaseViewKept(
	app core.App, content map[string]any, containerID, pageID, ownerID, gone string,
	trashed []string, parsed databaseContainerBlockContent,
) error {
	selected, _ := content["viewId"].(string)
	var source, container string
	err := app.DB().NewQuery(`SELECT [[data_source_id]], [[container_id]] FROM {{leafen_database_views}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": selected}).Row(&source, &container)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && (source != gone || container != "") {
		return nil
	}
	kept, err := json.Marshal(slices.DeleteFunc(parsed.ownDataSourceIDs(), func(id string) bool {
		return slices.Contains(trashed, id)
	}))
	if err != nil {
		return err
	}
	viewID := ""
	err = app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_views}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId}
			AND COALESCE(json_extract([[config]], '$.dashboardViewId'), '') = ''
			AND ([[container_id]]={:containerId}
				OR [[container_id]]='' AND [[data_source_id]] IN (SELECT [[value]] FROM json_each({:kept})))
		ORDER BY [[position]], [[id]] LIMIT 1
	`).Bind(dbx.Params{"owner": ownerID, "pageId": pageID, "containerId": containerID, "kept": string(kept)}).Row(&viewID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	content["viewId"] = viewID
	return err
}
