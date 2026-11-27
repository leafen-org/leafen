package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// databaseContainerBlockContent is the graph-authoritative membership of an
// inline database block. The ordered own sources are dataSourceId followed by
// additionalDataSourceIds; omitting the additional list keeps the historical
// single-source meaning, so stored blocks need no rewrite.
type databaseContainerBlockContent struct {
	DataSourceID            string   `json:"dataSourceId"`
	AdditionalDataSourceIDs []string `json:"additionalDataSourceIds"`
	ViewID                  string   `json:"viewId"`
	LinkedDatabaseSource    bool     `json:"linkedDatabaseSource"`
	SourceContainerID       string   `json:"sourceContainerId"`
	// LinkedMembers follow the own members; see database_container_linked_members.go.
	LinkedMembers []databaseContainerLinkedMember `json:"linkedMembers"`
	// TrashedDataSourceIDs are own members in the trash where they stand
	// (database_source_trash_in_place.go).
	TrashedDataSourceIDs []string `json:"trashedDataSourceIds"`
}

func parseDatabaseContainerBlockContent(content json.RawMessage) (databaseContainerBlockContent, error) {
	value := databaseContainerBlockContent{}
	if err := json.Unmarshal(content, &value); err != nil {
		return databaseContainerBlockContent{}, err
	}
	return value, nil
}

func (content databaseContainerBlockContent) dataSourceIDs() []string {
	if content.DataSourceID == "" {
		return nil
	}
	return append([]string{content.DataSourceID}, content.AdditionalDataSourceIDs...)
}

func databaseContainerMemberPosition(index int) string {
	return fmt.Sprintf("a%08d", index)
}

// validateDatabaseContainerBlockShape rejects member lists that cannot be one
// ordered own-source membership before any projection or reference lookup.
func validateDatabaseContainerBlockShape(content json.RawMessage) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(content, &raw); err != nil {
		return graphInputError(errorCodeGraphInvalid, "Check the inline database content.")
	}
	encoded, present := raw["additionalDataSourceIds"]
	if !present {
		parsed, err := parseDatabaseContainerBlockContent(content)
		if err != nil {
			return graphInputError(errorCodeGraphInvalid, "Check the inline database content.")
		}
		if err := validateDatabaseContainerLinkedMembers(raw, parsed); err != nil {
			return err
		}
		if err := validateLinkedDatabaseName(raw, parsed); err != nil {
			return err
		}
		return validateDatabaseTrashedSources(raw)
	}
	additional := []string{}
	if err := json.Unmarshal(encoded, &additional); err != nil || additional == nil {
		return graphInputError(errorCodeGraphInvalid, "Check the database's data source list.")
	}
	parsed, err := parseDatabaseContainerBlockContent(content)
	if err != nil {
		return graphInputError(errorCodeGraphInvalid, "Check the inline database content.")
	}
	if len(additional) > 0 && parsed.LinkedDatabaseSource {
		return graphInputError(
			errorCodeGraphReferenceInvalid, "A data source can't be added to a linked database.",
		)
	}
	seen := map[string]struct{}{strings.TrimSpace(parsed.DataSourceID): {}}
	for _, id := range additional {
		if id == "" || strings.TrimSpace(id) != id || len(id) > maxGraphIdentifierLength {
			return graphInputError(errorCodeGraphInvalid, "Check the database's data source list.")
		}
		if _, duplicate := seen[id]; duplicate {
			return graphInputError(
				errorCodeGraphReferenceInvalid, "A database can't contain the same data source twice.",
			)
		}
		seen[id] = struct{}{}
	}
	if err := validateDatabaseContainerLinkedMembers(raw, parsed); err != nil {
		return err
	}
	if err := validateLinkedDatabaseName(raw, parsed); err != nil {
		return err
	}
	return validateDatabaseTrashedSources(raw)
}

// validateDatabaseContainerMemberReferences checks that every own member and
// the selected view belong to this page's graph as one database.
func validateDatabaseContainerMemberReferences(
	projection graphProjection, blockID string, content databaseContainerBlockContent, pageID string,
) error {
	members := content.dataSourceIDs()
	view, viewExists := projection.views[content.ViewID]
	if len(members) == 0 || !viewExists {
		return graphInputError(errorCodeGraphReferenceInvalid, "Inline database link not found.")
	}
	if !graphViewShownByContainer(view, blockID, pageID, content) {
		return graphInputError(errorCodeGraphReferenceInvalid, "Check the database's selected view.")
	}
	for index, id := range members {
		source, exists := projection.sources[id]
		if !exists {
			return graphInputError(errorCodeGraphReferenceInvalid, "Inline database link not found.")
		}
		if index > 0 && source.PageID != pageID {
			return graphInputError(errorCodeGraphReferenceInvalid, "Check the database link on the other page.")
		}
	}
	if projection.sources[content.DataSourceID].PageID != pageID && !content.LinkedDatabaseSource {
		return graphInputError(errorCodeGraphReferenceInvalid, "Check the database link on the other page.")
	}
	for _, member := range content.LinkedMembers {
		if _, exists := projection.sources[member.DataSourceID]; !exists {
			return graphInputError(errorCodeGraphReferenceInvalid, "The database to link was not found.")
		}
	}
	return nil
}

func projectDatabaseContainerBlock(
	app core.App,
	owner, workspaceID, pageID, blockID, parentID, position string,
	content databaseContainerBlockContent,
) error {
	members := content.memberDataSourceIDs()
	linked := content.linkedSourceContainers()
	attached := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[data_source_id]] FROM {{leafen_database_container_sources}}
		WHERE [[container_id]] = {:id}
	`).Bind(dbx.Params{"id": blockID}).Column(&attached); err != nil {
		return err
	}
	for _, dataSourceID := range members {
		// Only a newly attached member must not come from a trashed database;
		// rewriting a block that already shows it keeps working.
		if slices.Contains(attached, dataSourceID) {
			continue
		}
		trashedSourceContainerID := ""
		err := app.DB().NewQuery(`
			SELECT [[container]].[[id]]
			FROM {{leafen_database_containers}} AS [[container]]
			JOIN {{leafen_database_container_sources}} AS [[membership]]
				ON [[membership]].[[container_id]] = [[container]].[[id]]
			JOIN {{leafen_database_container_trash}} AS [[trash]]
				ON [[trash]].[[container_id]] = [[container]].[[id]] AND [[trash]].[[kind]] = 'source'
			WHERE [[container]].[[kind]] = 'source' AND [[membership]].[[source_container_id]] = ''
				AND [[membership]].[[data_source_id]] = {:dataSourceId}
			ORDER BY [[container]].[[id]] LIMIT 1
		`).Bind(dbx.Params{"dataSourceId": dataSourceID}).Row(&trashedSourceContainerID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && blockID != trashedSourceContainerID {
			return graphInputError(
				errorCodeGraphReferenceInvalid,
				"A database in trash cannot be attached to another active container.",
			)
		}
	}
	if !content.LinkedDatabaseSource {
		if err := rejectSharedDatabaseContainerMembers(app, owner, workspaceID, blockID, content.dataSourceIDs()); err != nil {
			return err
		}
	}
	// Webhooks hear of a database made, brought back or moved, and of the
	// data sources it gained or lost (event_webhook_database_events.go).
	before := databaseContainerNote{}
	if len(attached) > 0 || len(members) > 0 {
		if err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_container_sources}}
			WHERE [[container_id]] = {:id} AND [[source_container_id]] = '' ORDER BY [[position]]`).
			Bind(dbx.Params{"id": blockID}).Column(&before.SourcesBefore); err != nil {
			return err
		}
		err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_database_containers}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": blockID}).Row(&before.PageBefore)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		before.Existed = err == nil
		if before.Existed {
			if before.Revived, err = databaseContainerIsTrashed(app, blockID); err != nil {
				return err
			}
		}
	}
	if len(members) == 0 {
		return deleteDatabaseContainerProjection(app, blockID)
	}
	// A database written again keeps its row, and with it who keeps it
	// among favorites and its public ID, which a deleted row takes with it
	// (2026-10-31: a database whose data source moved away and back lost
	// its ID, its block rewritten).
	if err := deleteDatabaseContainerMembers(app, blockID); err != nil {
		return err
	}
	// A database shown again leaves the trash.
	if !content.LinkedDatabaseSource {
		revived, err := reviveDeletedDatabases(app, workspaceID, blockID, content.dataSourceIDs())
		if err != nil {
			return err
		}
		before.Revived = before.Revived || revived
	}
	own := []string{}
	if !content.LinkedDatabaseSource {
		own = content.dataSourceIDs()
	}
	// The trash keeps when each source it lists in the trash where it
	// stands went there, and webhooks hear of it going and coming back
	// (database_source_trash_in_place_record.go).
	trashedBefore, trashedAfter, err := reconcileDatabaseSourceTrashInPlace(app, owner, workspaceID, pageID, blockID, content)
	if err != nil {
		return err
	}
	before.TrashedInPlace = trashedBefore
	defer noteDatabaseContainerProjection(app, workspaceID, blockID, before, pageID, own, trashedAfter, content.LinkedDatabaseSource)
	kind := databaseContainerKindSource
	sourceContainerID := ""
	if content.LinkedDatabaseSource {
		kind = databaseContainerKindLinked
		sourceContainerID = content.SourceContainerID
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_database_containers}}
			([[id]], [[owner]], [[workspace_id]], [[page_id]], [[block_id]], [[parent_block_id]],
			 [[position]], [[kind]], [[source_container_id]], [[created]], [[updated]])
		VALUES ({:id}, {:owner}, {:workspaceId}, {:pageId}, {:id}, {:parentId}, {:position},
			{:kind}, {:sourceContainerId}, {:now}, {:now})
		ON CONFLICT ([[id]]) DO UPDATE SET [[owner]] = excluded.[[owner]], [[workspace_id]] = excluded.[[workspace_id]],
			[[page_id]] = excluded.[[page_id]], [[block_id]] = excluded.[[block_id]],
			[[parent_block_id]] = excluded.[[parent_block_id]], [[position]] = excluded.[[position]],
			[[kind]] = excluded.[[kind]], [[source_container_id]] = excluded.[[source_container_id]],
			[[updated]] = excluded.[[updated]];
	`).Bind(dbx.Params{
		"id": blockID, "owner": owner, "workspaceId": workspaceID, "pageId": pageID,
		"parentId": parentID, "position": position, "kind": kind,
		"sourceContainerId": sourceContainerID, "now": now,
	}).Execute(); err != nil {
		return err
	}
	for index, dataSourceID := range members {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_container_sources}}
				([[owner]], [[workspace_id]], [[page_id]], [[container_id]], [[data_source_id]],
				 [[position]], [[source_container_id]], [[created]], [[updated]])
			VALUES ({:owner}, {:workspaceId}, {:pageId}, {:id}, {:dataSourceId}, {:position}, {:sourceContainerId}, {:now}, {:now});
		`).Bind(dbx.Params{
			"id": blockID, "owner": owner, "workspaceId": workspaceID, "pageId": pageID,
			"dataSourceId": dataSourceID, "position": databaseContainerMemberPosition(index),
			"sourceContainerId": linked[dataSourceID], "now": now,
		}).Execute(); err != nil {
			return err
		}
	}
	for _, dataSourceID := range members {
		if err := refreshDatabaseContainerViewsForDataSource(app, owner, workspaceID, dataSourceID); err != nil {
			return err
		}
		if err := resolveDatabaseContainerSources(app, owner, workspaceID, dataSourceID); err != nil {
			return err
		}
	}
	return nil
}

// rejectSharedDatabaseContainerMembers keeps each own source in exactly one
// own database; another block showing the source is a linked database.
func rejectSharedDatabaseContainerMembers(
	app core.App, owner, workspaceID, blockID string, members []string,
) error {
	encoded, err := json.Marshal(members)
	if err != nil {
		return err
	}
	var shared int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*)
		FROM {{leafen_database_container_sources}} AS [[membership]]
		JOIN {{leafen_database_containers}} AS [[container]]
			ON [[container]].[[id]] = [[membership]].[[container_id]]
		WHERE [[membership]].[[owner]] = {:owner} AND [[membership]].[[workspace_id]] = {:workspaceId}
			AND [[membership]].[[data_source_id]] IN (SELECT [[value]] FROM json_each({:members}))
			AND [[container]].[[kind]] = 'source' AND [[container]].[[id]] != {:blockId}
			AND [[membership]].[[source_container_id]] = ''
	`).Bind(dbx.Params{
		"owner": owner, "workspaceId": workspaceID, "members": string(encoded), "blockId": blockID,
	}).Row(&shared); err != nil {
		return err
	}
	if shared != 0 {
		return graphInputError(
			errorCodeGraphReferenceInvalid, "A data source can belong to only one database.",
		)
	}
	return nil
}

// refreshDatabaseContainerViewsForDataSource rebuilds the view membership of
// every container holding the source, all its members' views together so one
// member's refresh cannot drop another member's views. A container shows the
// views it owns, in one order across its sources, as the editor's tabs do;
// views without an owner belong to the own database of their source.
//
// The rebuild steps by key from the source's memberships: to each container,
// its members, then the views it owns (by container_id) and, for an own
// database, its members' unowned views on its page (by page and source).
// CROSS JOIN keeps that order, so a write reads only the containers showing
// the source and their views, whatever the size of the workspace
// (2026-11-09: the planner read every view of the workspace times every
// database, about 0.4 s a refresh at 1,560 views and 770 databases). A
// container's id is its block's and never empty, so the views it owns have a
// non-empty container_id, the owned-views index's condition.
func refreshDatabaseContainerViewsForDataSource(
	app core.App, owner, workspaceID, dataSourceID string,
) error {
	now := time.Now().UTC().UnixMilli()
	params := dbx.Params{
		"owner": owner, "workspaceId": workspaceID, "dataSourceId": dataSourceID, "now": now,
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_container_views}}
		WHERE [[container_id]] IN (
			SELECT [[container_id]] FROM {{leafen_database_container_sources}}
			WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
				AND [[data_source_id]] = {:dataSourceId}
		);
	`).Bind(params).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_database_container_views}}
			([[owner]], [[workspace_id]], [[page_id]], [[container_id]], [[view_id]],
			 [[position]], [[created]], [[updated]])
		SELECT [[container]].[[owner]], [[container]].[[workspace_id]], [[container]].[[page_id]],
			[[container]].[[id]], [[view]].[[id]], [[view]].[[position]], {:now}, {:now}
		FROM {{leafen_database_container_sources}} AS [[target]]
		CROSS JOIN {{leafen_database_containers}} AS [[container]]
			ON [[container]].[[id]] = [[target]].[[container_id]]
		CROSS JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[container_id]] = [[container]].[[id]]
		CROSS JOIN {{leafen_database_views}} AS [[view]]
			ON [[view]].[[container_id]] = [[container]].[[id]] AND [[view]].[[container_id]] != ''
			AND [[view]].[[data_source_id]] = [[membership]].[[data_source_id]]
			AND [[view]].[[owner]] = [[container]].[[owner]]
			AND [[view]].[[workspace_id]] = [[container]].[[workspace_id]]
		WHERE ` + databaseContainerRefreshTargets + `
		UNION ALL
		SELECT [[container]].[[owner]], [[container]].[[workspace_id]], [[container]].[[page_id]],
			[[container]].[[id]], [[view]].[[id]], [[view]].[[position]], {:now}, {:now}
		FROM {{leafen_database_container_sources}} AS [[target]]
		CROSS JOIN {{leafen_database_containers}} AS [[container]]
			ON [[container]].[[id]] = [[target]].[[container_id]] AND [[container]].[[kind]] = 'source'
		CROSS JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[container_id]] = [[container]].[[id]]
			AND [[membership]].[[source_container_id]] = ''
		CROSS JOIN {{leafen_database_views}} AS [[view]]
			ON [[view]].[[page_id]] = [[container]].[[page_id]]
			AND [[view]].[[data_source_id]] = [[membership]].[[data_source_id]]
			AND [[view]].[[container_id]] = ''
			AND [[view]].[[owner]] = [[container]].[[owner]]
			AND [[view]].[[workspace_id]] = [[container]].[[workspace_id]]
		WHERE ` + databaseContainerRefreshTargets + `;
	`).Bind(params).Execute()
	return err
}

// databaseContainerRefreshTargets names the containers a refresh rebuilds:
// those of the owner's workspace holding the source.
const databaseContainerRefreshTargets = `[[target]].[[owner]] = {:owner}
			AND [[target]].[[workspace_id]] = {:workspaceId} AND [[target]].[[data_source_id]] = {:dataSourceId}
			AND [[container]].[[owner]] = {:owner} AND [[container]].[[workspace_id]] = {:workspaceId}`

func resolveDatabaseContainerSources(app core.App, owner, workspaceID, dataSourceID string) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_containers}}
		SET [[source_container_id]] = CASE WHEN [[kind]] = 'source' THEN ''
		WHEN [[source_container_id]] != '' THEN [[source_container_id]] ELSE COALESCE((
			SELECT [[candidate]].[[id]]
			FROM {{leafen_database_containers}} AS [[candidate]]
			JOIN {{leafen_database_container_sources}} AS [[source]]
				ON [[source]].[[container_id]] = [[candidate]].[[id]]
			WHERE [[candidate]].[[owner]] = {:owner} AND [[candidate]].[[workspace_id]] = {:workspaceId}
				AND [[candidate]].[[kind]] = 'source' AND [[source]].[[source_container_id]] = ''
				AND [[source]].[[data_source_id]] = {:dataSourceId}
			ORDER BY [[candidate]].[[page_id]], [[candidate]].[[position]], [[candidate]].[[id]] LIMIT 1
		), '') END
		WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
			AND [[id]] IN (
				SELECT [[container_id]] FROM {{leafen_database_container_sources}}
				WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
					AND [[data_source_id]] = {:dataSourceId}
			);
	`).Bind(dbx.Params{
		"owner": owner, "workspaceId": workspaceID, "dataSourceId": dataSourceID,
	}).Execute()
	return err
}
