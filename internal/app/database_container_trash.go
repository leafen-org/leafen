package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func recoverableLinkedDatabaseContainer(
	block recoverableBlockTrashBlock,
) (databaseContainerBlockContent, bool) {
	content, kind, ok := recoverableDatabaseContainer(block)
	return content, ok && kind == databaseContainerKindLinked
}

func recoverableDatabaseContainer(
	block recoverableBlockTrashBlock,
) (databaseContainerBlockContent, string, bool) {
	if block.Type != graphBlockTypeInlineDatabase || validateDatabaseContainerBlockShape(block.Content) != nil {
		return databaseContainerBlockContent{}, "", false
	}
	content, err := parseDatabaseContainerBlockContent(block.Content)
	if err != nil || content.DataSourceID == "" || content.ViewID == "" {
		return databaseContainerBlockContent{}, "", false
	}
	if content.LinkedDatabaseSource {
		if content.SourceContainerID == "" {
			return databaseContainerBlockContent{}, "", false
		}
		return content, databaseContainerKindLinked, true
	}
	if content.SourceContainerID != "" {
		return databaseContainerBlockContent{}, "", false
	}
	return content, databaseContainerKindSource, true
}

func markRecoverableDatabaseContainerTrash(
	app core.App,
	ownerID, workspaceID, pageID, actorID string,
	snapshot recoverableBlockTrashSnapshot,
	deletedAt, expiresAt int64,
) error {
	type candidate struct {
		block   recoverableBlockTrashBlock
		content databaseContainerBlockContent
		kind    string
	}
	candidates := []candidate{}
	for _, block := range snapshot.Blocks {
		if block.Type != graphBlockTypeInlineDatabase {
			continue
		}
		content, kind, valid := recoverableDatabaseContainer(block)
		if !valid {
			return graphInputError(
				errorCodeGraphInvalid,
				"The database container cannot use the recoverable deletion lifecycle.",
			)
		}
		// One its page can't show goes as a block alone: nothing of a
		// database goes with it (database_unavailable_blocks.go).
		if unavailable, err := databaseBlockUnavailable(app, pageID, block.ID, content); err != nil {
			return err
		} else if unavailable {
			continue
		}
		members, err := json.Marshal(content.memberDataSourceIDs())
		if err != nil {
			return err
		}
		var exact int
		exactQuery := `
			SELECT COUNT(*)
			FROM {{leafen_database_containers}} AS [[container]]
			WHERE [[container]].[[id]] = {:containerId}
				AND [[container]].[[block_id]] = {:containerId}
				AND [[container]].[[owner]] = {:owner}
				AND [[container]].[[workspace_id]] = {:workspaceId}
				AND [[container]].[[page_id]] = {:pageId}
				AND [[container]].[[kind]] = {:kind}
				AND [[container]].[[source_container_id]] = {:sourceContainerId}
				AND (
					SELECT json_group_array([[data_source_id]]) FROM (
						SELECT [[all_membership]].[[data_source_id]]
						FROM {{leafen_database_container_sources}} AS [[all_membership]]
						WHERE [[all_membership]].[[container_id]] = [[container]].[[id]]
						ORDER BY [[all_membership]].[[position]], [[all_membership]].[[data_source_id]]
					)
				) = json({:members})
		`
		if kind == databaseContainerKindLinked {
			exactQuery += ` AND EXISTS (
				SELECT 1 FROM {{leafen_database_containers}} AS [[source]]
				WHERE [[source]].[[id]] = [[container]].[[source_container_id]]
					AND [[source]].[[owner]] = [[container]].[[owner]]
					AND [[source]].[[workspace_id]] = [[container]].[[workspace_id]]
					AND [[source]].[[kind]] = 'source'
					AND NOT EXISTS (
						SELECT 1 FROM {{leafen_database_container_trash}} AS [[source_trash]]
						WHERE [[source_trash]].[[container_id]] = [[source]].[[id]]
					)
			)`
		}
		if err := app.DB().NewQuery(exactQuery).Bind(dbx.Params{
			"containerId": block.ID, "owner": ownerID,
			"workspaceId": workspaceID,
			"pageId":      pageID, "kind": kind,
			"sourceContainerId": content.SourceContainerID,
			"members":           string(members),
		}).Row(&exact); err != nil {
			return err
		}
		if exact != 1 {
			return graphInputError(
				errorCodeGraphReferenceInvalid,
				"The database container changed before it could be moved to trash.",
			)
		}
		if kind == databaseContainerKindSource {
			// Linked members stay with the databases that own them, so only own
			// sources are checked for references and pending deletion.
			ownMembers, err := json.Marshal(content.dataSourceIDs())
			if err != nil {
				return err
			}
			var blocked int
			if err := app.DB().NewQuery(`
				SELECT
					(SELECT COUNT(*) FROM {{leafen_database_relation_property_targets}}
					 WHERE [[owner]] = {:owner}
						AND [[target_source_id]] IN (SELECT [[value]] FROM json_each({:members}))
						AND [[source_id]] NOT IN (SELECT [[value]] FROM json_each({:members})))
					+
					(SELECT COUNT(*) FROM {{leafen_database_mutation_jobs}}
					 WHERE [[owner_id]] = {:owner} AND [[page_id]] = {:pageId}
						AND [[target_kind]] = 'source'
						AND [[target_id]] IN (SELECT [[value]] FROM json_each({:members}))
						AND [[status]] = 'pending')
			`).Bind(dbx.Params{
				"owner": ownerID, "pageId": pageID, "members": string(ownMembers),
			}).Row(&blocked); err != nil {
				return err
			}
			if blocked != 0 {
				return graphInputError(
					errorCodeGraphReferenceInvalid,
					"Remove relation references and pending database deletion before moving this source database to trash.",
				)
			}
		}
		candidates = append(candidates, candidate{block: block, content: content, kind: kind})
	}
	for _, candidate := range candidates {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_container_trash}}
				([[container_id]],[[root_block_id]],[[kind]],[[deleted_at]],[[expires_at]])
			VALUES ({:containerId},{:rootBlockId},{:kind},{:deletedAt},{:expiresAt})
		`).Bind(dbx.Params{
			"containerId": candidate.block.ID, "rootBlockId": snapshot.RootID,
			"kind": candidate.kind, "deletedAt": deletedAt, "expiresAt": expiresAt,
		}).Execute(); err != nil {
			return err
		}
		if candidate.kind == databaseContainerKindSource {
			for index, dataSourceID := range candidate.content.dataSourceIDs() {
				if err := admitDatabaseContainerTrashRetentionDeletion(
					app, ownerID, workspaceID, pageID, actorID,
					databaseContainerTrashRetentionRequestID(snapshot.RootID, candidate.block.ID, index, dataSourceID),
					dataSourceID, deletedAt, expiresAt,
				); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// databaseContainerTrashRetentionRequestID keeps the historical request ID for
// the first member so pending single-source retention jobs stay recognizable.
func databaseContainerTrashRetentionRequestID(rootBlockID, containerID string, index int, dataSourceID string) string {
	requestID := "block-trash-retention:" + rootBlockID + ":" + containerID
	if index > 0 {
		requestID += ":" + dataSourceID
	}
	return requestID
}

func admitDatabaseContainerTrashRetentionDeletion(
	app core.App,
	ownerID, workspaceID, pageID, actorID, requestID, dataSourceID string,
	deletedAt, expiresAt int64,
) error {
	input := databaseMutationInput{
		RequestID:  requestID,
		MutationID: requestID,
		TargetKind: databaseMutationTargetSource,
		TargetID:   dataSourceID,
	}
	_, err := app.DB().Insert(databaseMutationJobsTable, dbx.Params{
		"id": core.GenerateDefaultRandomId(), "actor_id": actorID,
		"owner_id": ownerID, "workspace_id": workspaceID, "page_id": pageID,
		"request_id":   input.RequestID,
		"request_hash": databaseMutationDeletionRequestHash(pageID, input),
		"mutation_id":  input.MutationID, "target_kind": input.TargetKind,
		"target_id": input.TargetID, "status": databaseMutationStatusPending,
		"phase": databaseMutationPhaseSourceCells, "revision": 1,
		"processed_items": 0, "cleanup_lane": 0, "not_before": expiresAt,
		"receipt_cursor": 0, "receipt_counts_json": "", "receipt_state_vector": "",
		"created_at": deletedAt, "updated_at": deletedAt, "completed_at": 0,
	}).Execute()
	if err == nil {
		notifyDatabaseMutationDeletionAfterCommit(app)
	}
	return err
}

func databaseContainerTrashRootDue(
	app core.App, now int64,
) (string, bool, error) {
	rootID := ""
	err := app.DB().NewQuery(`
		SELECT [[tombstone]].[[root_block_id]]
		FROM {{leafen_block_trash_tombstones}} AS [[tombstone]]
		WHERE [[tombstone]].[[expires_at]] <= {:now} AND EXISTS (
			SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
			WHERE [[trash]].[[root_block_id]] = [[tombstone]].[[root_block_id]]
		) AND NOT EXISTS (
			SELECT 1 FROM {{leafen_database_container_trash}} AS [[source_trash]]
			WHERE [[source_trash]].[[root_block_id]] = [[tombstone]].[[root_block_id]]
				AND [[source_trash]].[[kind]] = 'source'
		) AND NOT ` + legalHoldRetainsPageSQL("[[tombstone]].[[page_id]]", "[[tombstone]].[[workspace_id]]") + `
		ORDER BY [[tombstone]].[[expires_at]],[[tombstone]].[[root_block_id]] LIMIT 1
	`).Bind(dbx.Params{"now": now}).Row(&rootID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return rootID, err == nil, err
}

func databaseContainerTrashRetentionJob(
	app core.App, job storedDatabaseMutationDeletion, now int64,
) (string, bool, error) {
	if job.TargetKind != databaseMutationTargetSource || job.Status != databaseMutationStatusPending {
		return "", false, nil
	}
	rootBlockID := ""
	err := app.DB().NewQuery(`
		SELECT [[trash]].[[root_block_id]]
		FROM {{leafen_database_container_trash}} AS [[trash]]
		JOIN {{leafen_block_trash_tombstones}} AS [[tombstone]]
			ON [[tombstone]].[[root_block_id]] = [[trash]].[[root_block_id]]
		JOIN {{leafen_database_containers}} AS [[container]]
			ON [[container]].[[id]] = [[trash]].[[container_id]] AND [[container]].[[kind]] = 'source'
		JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[container_id]] = [[container]].[[id]]
		WHERE [[trash]].[[kind]] = 'source'
			AND [[trash]].[[expires_at]] <= {:now}
			AND [[tombstone]].[[owner_id]] = {:owner}
			AND [[tombstone]].[[workspace_id]] = {:workspaceId}
			AND [[tombstone]].[[page_id]] = {:pageId}
			AND [[membership]].[[data_source_id]] = {:dataSourceId}
			AND {:requestId} IN (
				'block-trash-retention:' || [[trash]].[[root_block_id]] || ':' || [[trash]].[[container_id]],
				'block-trash-retention:' || [[trash]].[[root_block_id]] || ':' || [[trash]].[[container_id]]
					|| ':' || [[membership]].[[data_source_id]]
			)
		LIMIT 2
	`).Bind(dbx.Params{
		"now": now, "owner": job.OwnerID, "workspaceId": job.WorkspaceID,
		"pageId": job.PageID, "dataSourceId": job.TargetID, "requestId": job.RequestID,
	}).Row(&rootBlockID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return rootBlockID, err == nil, err
}

func completeDatabaseContainerTrashRetentionJob(
	app core.App, rootBlockID string, job storedDatabaseMutationDeletion,
) error {
	var pending int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*)
		FROM {{leafen_database_mutation_jobs}}
		WHERE [[status]] = 'pending'
			AND instr([[request_id]], 'block-trash-retention:' || {:rootBlockId} || ':') = 1
	`).Bind(dbx.Params{"rootBlockId": rootBlockID}).Row(&pending); err != nil {
		return err
	}
	if pending != 0 {
		return releaseDatabaseContainerTrashRetentionMember(app, rootBlockID, job.TargetID)
	}
	if err := deleteRecoverableDatabaseContainerTrashProjections(app, rootBlockID); err != nil {
		return err
	}
	return deleteRecoverableBlockTrash(app, rootBlockID, job.PageID)
}

// releaseDatabaseContainerTrashRetentionMember drops the projection of one
// permanently deleted member while other members of the trashed database wait
// for their own retention jobs.
func releaseDatabaseContainerTrashRetentionMember(app core.App, rootBlockID, dataSourceID string) error {
	params := dbx.Params{"rootBlockId": rootBlockID, "dataSourceId": dataSourceID}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_container_sources}}
		WHERE [[data_source_id]] = {:dataSourceId} AND [[container_id]] IN (
			SELECT [[container_id]] FROM {{leafen_database_container_trash}}
			WHERE [[root_block_id]] = {:rootBlockId} AND [[kind]] = 'source'
		)
	`).Bind(params).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_container_views}}
		WHERE [[container_id]] IN (
			SELECT [[container_id]] FROM {{leafen_database_container_trash}}
			WHERE [[root_block_id]] = {:rootBlockId} AND [[kind]] = 'source'
		) AND NOT EXISTS (
			SELECT 1 FROM {{leafen_database_views}} AS [[view]]
			JOIN {{leafen_database_container_sources}} AS [[membership]]
				ON [[membership]].[[data_source_id]] = [[view]].[[data_source_id]]
			WHERE [[view]].[[id]] = {{leafen_database_container_views}}.[[view_id]]
				AND [[membership]].[[container_id]] = {{leafen_database_container_views}}.[[container_id]]
		)
	`).Bind(params).Execute()
	return err
}

func deleteRecoverableDatabaseContainerTrashProjections(
	app core.App, rootBlockID string,
) error {
	rows := []struct {
		ContainerID string `db:"container_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[container_id]] FROM {{leafen_database_container_trash}}
		WHERE [[root_block_id]] = {:rootBlockId}
		ORDER BY [[container_id]]
	`).Bind(dbx.Params{"rootBlockId": rootBlockID}).All(&rows); err != nil {
		return err
	}
	for _, row := range rows {
		if err := deleteDatabaseContainerProjection(app, row.ContainerID); err != nil {
			return err
		}
	}
	return nil
}

func databaseContainerIsTrashed(app core.App, containerID string) (bool, error) {
	var exists int
	err := app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_database_container_trash}} WHERE [[container_id]]={:containerId}
		)
	`).Bind(dbx.Params{"containerId": containerID}).Row(&exists)
	return exists == 1, err
}

// databaseSourceIsTrashed reports whether a data source is in the trash:
// its own database in the trash, or kept in its database in the trash where
// it stands (database_source_trash_in_place.go).
func databaseSourceIsTrashed(app core.App, dataSourceID string) (bool, error) {
	var exists int
	err := app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1
			FROM {{leafen_database_containers}} AS [[container]]
			JOIN {{leafen_database_container_sources}} AS [[membership]]
				ON [[membership]].[[container_id]] = [[container]].[[id]]
			JOIN {{leafen_database_container_trash}} AS [[trash]]
				ON [[trash]].[[container_id]] = [[container]].[[id]] AND [[trash]].[[kind]] = 'source'
			WHERE [[container]].[[kind]] = 'source' AND [[membership]].[[source_container_id]] = ''
				AND [[membership]].[[data_source_id]] = {:dataSourceId}
		) OR ` + databaseSourceTrashedInPlaceSQL("{:dataSourceId}") + `
	`).Bind(dbx.Params{"dataSourceId": dataSourceID}).Row(&exists)
	return exists == 1, err
}

// databaseSourceNotTrashedSQL holds for a data source out of the trash; see
// databaseSourceIsTrashed. Its expression is a bound value or a qualified
// column: an unqualified [[id]] is ambiguous inside its subqueries, whose
// tables all have one, and fails the query.
func databaseSourceNotTrashedSQL(dataSourceExpression string) string {
	return `(NOT ` + databaseSourceTrashedInPlaceSQL(dataSourceExpression) + ` AND ` +
		databaseSourceShownSQL(dataSourceExpression) + `)`
}

// databaseSourceShownSQL holds for a data source whose own database is out
// of the trash: out of the trash itself, or kept in its database in the
// trash where it stands, which a page shows read-only.
func databaseSourceShownSQL(dataSourceExpression string) string {
	return `NOT EXISTS (
		SELECT 1
		FROM {{leafen_database_containers}} AS [[trash_source_container]]
		JOIN {{leafen_database_container_sources}} AS [[trash_source_membership]]
			ON [[trash_source_membership]].[[container_id]] = [[trash_source_container]].[[id]]
		JOIN {{leafen_database_container_trash}} AS [[trash_source_marker]]
			ON [[trash_source_marker]].[[container_id]] = [[trash_source_container]].[[id]]
			AND [[trash_source_marker]].[[kind]] = 'source'
		WHERE [[trash_source_container]].[[kind]] = 'source'
			AND [[trash_source_membership]].[[source_container_id]] = ''
			AND [[trash_source_membership]].[[data_source_id]] = ` + dataSourceExpression + `
	)`
}
