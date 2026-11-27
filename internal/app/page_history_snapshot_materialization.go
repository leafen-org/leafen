package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const pageHistorySnapshotWorkSize = pageHistoryBlockPageSize + 1

type storedPageHistorySnapshotJob struct {
	MaterializationID string `db:"materialization_id"`
	PageID            string `db:"page_id"`
	VersionID         string `db:"version_id"`
	VersionCursor     int    `db:"version_cursor"`
	SnapshotFrontier  int    `db:"snapshot_frontier"`
	WorkCursor        string `db:"work_cursor"`
	WorkParentID      string `db:"work_parent_id"`
	WorkPosition      int    `db:"work_position"`
	Status            string `db:"status"`
}

type pageHistorySnapshotSourceBlock struct {
	BlockID  string `db:"block_id"`
	ParentID string `db:"parent_id"`
	Type     string `db:"type"`
	Content  string `db:"content"`
	Plain    string `db:"plain_text"`
	Rich     string `db:"rich_text"`
	Created  int64  `db:"created_at"`
	Updated  int64  `db:"updated_at"`
}

type storedPageHistorySnapshotBlock struct {
	Sequence  int    `db:"sequence"`
	BlockID   string `db:"block_id"`
	ParentID  string `db:"parent_id"`
	Position  string `db:"position"`
	Type      string `db:"type"`
	Content   string `db:"content"`
	PlainText string `db:"plain_text"`
	CreatedAt int64  `db:"created_at"`
	UpdatedAt int64  `db:"updated_at"`
}

func pageHistorySnapshotMaterializationID(pageID, versionID string) string {
	digest := sha256.Sum256([]byte("page-history-snapshot\x00" + pageID + "\x00" + versionID))
	return hex.EncodeToString(digest[:16])
}

func ensurePageHistorySnapshotJob(
	app core.App, pageID, versionID string, versionCursor, snapshotFrontier int,
) (storedPageHistorySnapshotJob, error) {
	// The selected immutable version is shared across readers, while each wire
	// cursor keeps its own stable frontier and principal identity.
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return storedPageHistorySnapshotJob{}, err
	}
	now := time.Now().UTC().UnixMilli()
	materializationID := pageHistorySnapshotMaterializationID(pageID, versionID)
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_history_snapshot_jobs}}
			([[materialization_id]],[[workspace_id]],[[page_id]],[[version_id]],[[version_cursor]],
			 [[snapshot_frontier]],[[work_cursor]],[[work_parent_id]],[[work_position]],[[status]],[[created_at]],[[updated_at]])
		VALUES ({:id},{:workspaceId},{:pageId},{:versionId},{:versionCursor},
			{:snapshotFrontier},'','',0,'pending',{:now},{:now})
		ON CONFLICT ([[page_id]],[[version_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"id": materializationID, "workspaceId": page.GetString("workspace"),
		"pageId": pageID, "versionId": versionID, "versionCursor": versionCursor,
		"snapshotFrontier": snapshotFrontier, "now": now,
	}).Execute(); err != nil {
		return storedPageHistorySnapshotJob{}, err
	}
	job := storedPageHistorySnapshotJob{}
	if err := app.DB().NewQuery(`
		SELECT [[materialization_id]],[[page_id]],[[version_id]],[[version_cursor]],[[snapshot_frontier]],
			[[work_cursor]],[[work_parent_id]],[[work_position]],[[status]]
		FROM {{leafen_page_history_snapshot_jobs}}
		WHERE [[page_id]]={:pageId} AND [[version_id]]={:versionId}
	`).Bind(dbx.Params{"pageId": pageID, "versionId": versionID}).One(&job); err != nil {
		return job, err
	}
	if job.VersionCursor != versionCursor || job.SnapshotFrontier < versionCursor || snapshotFrontier < versionCursor {
		return job, errPageHistoryCursorInvalid
	}
	return job, nil
}

func advancePageHistorySnapshotMaterialization(
	app core.App,
	pageID, versionID string,
	versionCursor, snapshotFrontier, requiredSequence int,
) (storedPageHistorySnapshotJob, error) {
	// The blocks typed in since the checkpoint before the version take
	// their text from the version replayed (page_history_text_replay.go),
	// read before the write: a long page's replay held the write connection.
	replayed, err := pageHistoryReplayedTexts(app, pageID, versionCursor)
	if err != nil {
		return storedPageHistorySnapshotJob{}, err
	}
	job := storedPageHistorySnapshotJob{}
	err = app.RunInTransaction(func(txApp core.App) error {
		if err := ensurePageHistoryProjectionBaseline(txApp, pageID); err != nil {
			return err
		}
		var err error
		job, err = ensurePageHistorySnapshotJob(
			txApp, pageID, versionID, versionCursor, snapshotFrontier,
		)
		if err != nil {
			return err
		}
		var available int
		if err := txApp.DB().NewQuery(`SELECT COALESCE(MAX([[sequence]]),0)
			FROM {{leafen_page_history_snapshot_blocks}} WHERE [[materialization_id]]={:id}`).Bind(
			dbx.Params{"id": job.MaterializationID},
		).Row(&available); err != nil {
			return err
		}
		if available >= requiredSequence || job.Status == pageHistorySnapshotStatusReady {
			return nil
		}
		rows := []pageHistorySnapshotSourceBlock{}
		if err := txApp.DB().NewQuery(`
			WITH RECURSIVE [[chain]](
				[[block_id]],[[parent_id]],[[type]],[[content]],[[created_at]],[[updated_at]]
			) AS (
				SELECT [[span]].[[block_id]],[[span]].[[parent_id]],[[span]].[[type]],
					[[span]].[[content]],[[span]].[[created_at]],[[span]].[[updated_at]]
				FROM {{leafen_page_history_block_spans}} [[span]]
				WHERE [[span]].[[page_id]]={:pageId} AND [[span]].[[after_id]]={:afterId}
					AND [[span]].[[valid_from]]<={:versionCursor}
					AND ([[span]].[[valid_through]] IS NULL OR [[span]].[[valid_through]]>={:versionCursor})
				UNION ALL
				-- CROSS JOIN steps from the block before by key (page, after):
				-- SQLite's own order read the page's every span for each block
				-- (2026-11-18: 202 blocks of a page with 9,500 spans, 0.7 s).
				SELECT [[next]].[[block_id]],[[next]].[[parent_id]],[[next]].[[type]],
					[[next]].[[content]],[[next]].[[created_at]],[[next]].[[updated_at]]
				FROM [[chain]] [[previous]]
				CROSS JOIN {{leafen_page_history_block_spans}} [[next]] ON [[next]].[[after_id]]=[[previous]].[[block_id]]
				WHERE [[next]].[[page_id]]={:pageId} AND [[next]].[[valid_from]]<={:versionCursor}
					AND ([[next]].[[valid_through]] IS NULL OR [[next]].[[valid_through]]>={:versionCursor})
				LIMIT {:limit}
			)
			SELECT [[chain]].*,COALESCE((
				SELECT [[text]].[[plain_text]] FROM {{leafen_page_history_text_spans}} [[text]]
				WHERE [[text]].[[page_id]]={:pageId} AND [[text]].[[block_id]]=[[chain]].[[block_id]]
					AND [[text]].[[valid_from]]<={:versionCursor}
					AND ([[text]].[[valid_through]] IS NULL OR [[text]].[[valid_through]]>={:versionCursor})
				ORDER BY [[text]].[[valid_from]] DESC LIMIT 1
			),'') AS [[plain_text]],COALESCE((
				SELECT [[text]].[[rich_text]] FROM {{leafen_page_history_text_spans}} [[text]]
				WHERE [[text]].[[page_id]]={:pageId} AND [[text]].[[block_id]]=[[chain]].[[block_id]]
					AND [[text]].[[valid_from]]<={:versionCursor}
					AND ([[text]].[[valid_through]] IS NULL OR [[text]].[[valid_through]]>={:versionCursor})
				ORDER BY [[text]].[[valid_from]] DESC LIMIT 1
			),'') AS [[rich_text]] FROM [[chain]]
		`).Bind(dbx.Params{
			"pageId": pageID, "afterId": job.WorkCursor, "versionCursor": versionCursor,
			"limit": pageHistorySnapshotWorkSize + 1,
		}).All(&rows); err != nil {
			return err
		}
		for index := range rows {
			if text, found := replayed[rows[index].BlockID]; found {
				rows[index].Plain, rows[index].Rich = text.plain, text.rich
			}
		}
		writeRows := rows
		if len(writeRows) > pageHistorySnapshotWorkSize {
			writeRows = writeRows[:pageHistorySnapshotWorkSize]
		}
		sequence := available
		parentID, position := job.WorkParentID, job.WorkPosition
		for _, row := range writeRows {
			sequence++
			if sequence == 1 || row.ParentID != parentID {
				parentID, position = row.ParentID, 1
			} else {
				position++
			}
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_page_history_snapshot_blocks}}
					([[materialization_id]],[[sequence]],[[block_id]],[[parent_id]],[[position]],
					 [[type]],[[content]],[[plain_text]],[[created_at]],[[updated_at]])
				VALUES ({:id},{:sequence},{:blockId},{:parentId},{:position},
					{:type},{:content},{:plainText},{:created},{:updated})
				ON CONFLICT ([[materialization_id]],[[sequence]]) DO NOTHING
			`).Bind(dbx.Params{
				"id": job.MaterializationID, "sequence": sequence, "blockId": row.BlockID,
				"parentId": row.ParentID, "position": fmt.Sprintf("%020d", position),
				"type": row.Type, "content": pageHistoryContentWithRich(row.Content, row.Rich, row.Plain), "plainText": row.Plain,
				"created": row.Created, "updated": row.Updated,
			}).Execute(); err != nil {
				return err
			}
			job.WorkCursor, job.WorkParentID, job.WorkPosition = row.BlockID, parentID, position
		}
		if len(rows) <= pageHistorySnapshotWorkSize {
			job.Status = pageHistorySnapshotStatusReady
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_page_history_snapshot_jobs}}
			SET [[work_cursor]]={:cursor},[[work_parent_id]]={:parentId},[[work_position]]={:position},
				[[status]]={:status},[[updated_at]]={:now}
			WHERE [[materialization_id]]={:id}
		`).Bind(dbx.Params{
			"cursor": job.WorkCursor, "parentId": job.WorkParentID, "position": job.WorkPosition,
			"status": job.Status, "now": time.Now().UTC().UnixMilli(), "id": job.MaterializationID,
		}).Execute()
		return err
	})
	return job, err
}

func pageHistorySnapshotCursorSequence(
	app core.App, materializationID string, cursor pageHistoryCursor,
) (int, error) {
	if cursor.BlockID == "" {
		return 0, nil
	}
	var sequence int
	err := app.DB().NewQuery(`
		SELECT [[sequence]] FROM {{leafen_page_history_snapshot_blocks}}
		WHERE [[materialization_id]]={:id} AND [[block_id]]={:blockId}
			AND [[parent_id]]={:parentId} AND [[position]]={:position}
	`).Bind(dbx.Params{
		"id": materializationID, "blockId": cursor.BlockID,
		"parentId": cursor.ParentID, "position": cursor.Position,
	}).Row(&sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errPageHistoryCursorInvalid
	}
	return sequence, err
}

func readPageHistorySnapshotWindow(
	app core.App, job storedPageHistorySnapshotJob, afterSequence, limit int,
) ([]pageHistoryBlockWire, bool, error) {
	rows := []storedPageHistorySnapshotBlock{}
	if err := app.DB().NewQuery(`
		SELECT [[sequence]],[[block_id]],[[parent_id]],[[position]],[[type]],[[content]],
			[[plain_text]],[[created_at]],[[updated_at]]
		FROM {{leafen_page_history_snapshot_blocks}}
		WHERE [[materialization_id]]={:id} AND [[sequence]]>{:afterSequence}
		ORDER BY [[sequence]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"id": job.MaterializationID, "afterSequence": afterSequence, "limit": limit + 1,
	}).All(&rows); err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > limit || job.Status != pageHistorySnapshotStatusReady
	if len(rows) > limit {
		rows = rows[:limit]
	}
	blocks := make([]pageHistoryBlockWire, len(rows))
	for index, row := range rows {
		if !json.Valid([]byte(row.Content)) {
			return nil, false, errors.New(errorCodePageHistoryFailed)
		}
		blocks[index] = pageHistoryBlockWire{
			ID: row.BlockID, ParentID: row.ParentID, Position: row.Position,
			Type: row.Type, Content: json.RawMessage(row.Content), PlainText: row.PlainText,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
	}
	if hasMore && len(blocks) == 0 {
		return nil, false, errors.New(errorCodePageHistoryFailed)
	}
	return blocks, hasMore, nil
}

func readPageHistorySnapshotPageState(
	app core.App, pageID string, versionCursor int,
) (pageHistoryPageState, error) {
	checkpointCursor, pageJSON, err := pageHistoryCheckpointPage(app, pageID, versionCursor)
	if err != nil {
		return pageHistoryPageState{}, err
	}
	state := pageHistoryPageState{}
	if err := json.Unmarshal([]byte(pageJSON), &state); err != nil {
		return state, err
	}
	event := storedPageHistoryEvent{}
	err = app.DB().NewQuery(`
		SELECT [[version_cursor]],[[kind]],[[payload]],[[created_at]]
		FROM {{leafen_page_history_events}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]>{:afterCursor}
			AND [[version_cursor]]<={:versionCursor} AND [[kind]] IN ('page','metadata')
		ORDER BY [[version_cursor]] DESC LIMIT 1
	`).Bind(dbx.Params{
		"pageId": pageID, "afterCursor": checkpointCursor, "versionCursor": versionCursor,
	}).One(&event)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	payload, err := unpackPageHistory(event.Payload)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(payload, &state); err != nil {
		return state, err
	}
	return state, nil
}
