package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type checkpointBlock struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
	Position string `db:"position"`
	Type     string `db:"type"`
	Content  string `db:"content"`
}

type pageCheckpointPayload struct {
	PageID      string `json:"pageId"`
	DigestChain string `json:"digestChain"`
}

func applyPageCheckpointChunk(app core.App, job storedJob) error {
	page, err := app.FindRecordById(pagesCollection, job.PageID)
	if err != nil {
		return err
	}
	if page.GetString("owner") != job.Owner {
		return fmt.Errorf("%s: page owner mismatch", errorCodeJobRunnerFailed)
	}
	payload := pageCheckpointPayload{}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil || payload.PageID != job.PageID {
		return fmt.Errorf("%s: invalid page-checkpoint payload", errorCodeJobRunnerFailed)
	}

	blocks := make([]checkpointBlock, 0, pageCheckpointJobChunkSize+1)
	err = app.DB().NewQuery(`
		SELECT [[id]], [[parent_id]], [[position]], [[type]], [[content]]
		FROM {{leafen_blocks}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[id]] > {:cursor}
		ORDER BY [[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": job.Owner, "pageId": job.PageID, "cursor": job.Cursor,
		"limit": pageCheckpointJobChunkSize + 1,
	}).All(&blocks)
	if err != nil {
		return err
	}

	hasMore := len(blocks) > pageCheckpointJobChunkSize
	if hasMore {
		blocks = blocks[:pageCheckpointJobChunkSize]
	}
	nextCursor := job.Cursor
	if len(blocks) > 0 {
		nextCursor = blocks[len(blocks)-1].ID
	}
	chunkDigest := digestCheckpointBlocks(blocks)
	payload.DigestChain = advanceCheckpointDigest(payload.DigestChain, chunkDigest, len(blocks))
	nextPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	chunkIndex := job.ChunkCount + 1
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_job_chunks}}
			([[job_id]], [[chunk_index]], [[item_count]], [[cursor]], [[sha256]], [[created]])
		VALUES ({:jobId}, {:chunkIndex}, {:itemCount}, {:cursor}, {:sha256}, {:created})
		ON CONFLICT ([[job_id]], [[chunk_index]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": job.ID, "chunkIndex": chunkIndex, "itemCount": len(blocks),
		"cursor": nextCursor, "sha256": chunkDigest, "created": now.UnixMilli(),
	}).Execute(); err != nil {
		return err
	}

	processed := job.Processed + len(blocks)
	if hasMore {
		_, err = app.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[cursor]] = {:cursor}, [[processed_items]] = {:processed},
				[[payload_json]] = {:payload}, [[chunk_count]] = {:chunkCount},
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": job.ID, "cursor": nextCursor, "processed": processed,
			"payload": string(nextPayload), "chunkCount": chunkIndex,
			"notBefore": now.Add(durableJobReadyDelay).UnixMilli(), "updated": now.UnixMilli(),
		}).Execute()
		return err
	}

	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_job_effects}} ([[job_id]], [[page_id]], [[revision]], [[sha256]], [[created]])
		VALUES ({:jobId}, {:pageId}, {:revision}, {:sha256}, {:created})
		ON CONFLICT ([[job_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": job.ID, "pageId": page.Id, "revision": page.GetInt("revision"),
		"sha256": payload.DigestChain, "created": now.UnixMilli(),
	}).Execute(); err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]] = 'completed', [[cursor]] = {:cursor},
			[[processed_items]] = {:processed}, [[total_items]] = {:processed},
			[[payload_json]] = {:payload}, [[chunk_count]] = {:chunkCount}, [[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued'
	`).Bind(dbx.Params{
		"id": job.ID, "cursor": nextCursor, "processed": processed,
		"payload": string(nextPayload), "chunkCount": chunkIndex,
		"updated": now.UnixMilli(),
	}).Execute()
	return err
}

func digestCheckpointBlocks(blocks []checkpointBlock) string {
	hasher := sha256.New()
	for _, block := range blocks {
		_, _ = fmt.Fprintf(
			hasher,
			"%s\x00%s\x00%s\x00%s\x00%s\n",
			block.ID,
			block.ParentID,
			block.Position,
			block.Type,
			block.Content,
		)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func advanceCheckpointDigest(prior string, chunk string, itemCount int) string {
	if prior == "" && itemCount == 0 {
		digest := sha256.Sum256(nil)
		return hex.EncodeToString(digest[:])
	}
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(prior))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(chunk))
	return hex.EncodeToString(hasher.Sum(nil))
}
