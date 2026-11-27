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

func (r *durableJobRunner) commitNotionDiscovery(job storedJob, payload notionImportJobPayload, sources []stagedNotionSource) error {
	return r.commitNotionDiscoveryWithPage(job, payload, sources, nil)
}

func (r *durableJobRunner) commitNotionDiscoveryWithPage(
	job storedJob,
	payload notionImportJobPayload,
	sources []stagedNotionSource,
	pageAdvance *notionImportPageDiscoveryAdvance,
) error {
	return r.commitNotionDiscoveryTakingOver(job, payload, sources, pageAdvance, nil)
}

// notionImportSourceTakeover turns a staged source into another, which
// takes its place in the order the sources apply in.
type notionImportSourceTakeover struct {
	sourceID string
	source   stagedNotionSource
}

// commitNotionDiscoveryTakingOver stages sources once a staged source, when
// one is given, has become another in its place.
func (r *durableJobRunner) commitNotionDiscoveryTakingOver(
	job storedJob,
	payload notionImportJobPayload,
	sources []stagedNotionSource,
	pageAdvance *notionImportPageDiscoveryAdvance,
	takeover *notionImportSourceTakeover,
) error {
	// What a move's run read shows in Leafen's import window (notion_migration_live.go).
	for index := len(sources) - 1; index >= 0 && payload.MigrationID != ""; index-- {
		if kind := notionMigrationLiveKind(sources[index].Kind); kind != "" && sources[index].Title != "" {
			noteNotionMigrationLive(payload.MigrationID, notionMigrationStageReading, kind, sources[index].Title)
			break
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if _, err := authorizeNotionImportTurn(txApp, current, payload); err != nil {
			return err
		}
		now := time.Now().UTC()
		if takeover != nil {
			if _, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_notion_import_sources}}
				SET [[source_id]] = {:newSourceId}, [[kind]] = {:kind}, [[parent_source_id]] = {:parentSourceId},
					[[parent_kind]] = {:parentKind}, [[position]] = {:position}, [[title]] = {:title}, [[text]] = {:text},
					[[discovery_status]] = 'complete'
				WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId}
			`).Bind(dbx.Params{
				"jobId": job.ID, "sourceId": takeover.sourceID, "newSourceId": takeover.source.SourceID,
				"kind": takeover.source.Kind, "parentSourceId": takeover.source.ParentSourceID,
				"parentKind": takeover.source.ParentKind, "position": takeover.source.Position,
				"title": takeover.source.Title, "text": takeover.source.Text,
			}).Execute(); err != nil {
				return err
			}
		}
		// A sub-page a block's children list takes the place its mention
		// was staged in (notion_import_line_pages.go).
		released, err := releaseNotionImportListedLinePages(txApp, job.ID, sources)
		if err != nil {
			return err
		}
		inserted, err := storeNotionDiscoverySources(txApp, job.ID, sources, now)
		if err != nil {
			return err
		}
		inserted -= released
		if err := advanceNotionImportPageDiscovery(txApp, job.ID, pageAdvance); err != nil {
			return err
		}
		chunkIndex := current.ChunkCount + 1
		chunkCursor := payload.ProviderCursor
		if pageAdvance != nil {
			chunkCursor = pageAdvance.NextCursor
		}
		if err := recordNotionImportChunk(txApp, current.ID, chunkIndex, len(sources), chunkCursor, notionDiscoveryDigest(sources), now); err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[payload_json]] = {:payload}, [[total_items]] = [[total_items]] + {:inserted},
				[[chunk_count]] = {:chunkCount}, [[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "payload": string(encoded), "inserted": inserted,
			"chunkCount": chunkIndex, "notBefore": now.Add(durableJobReadyDelay).UnixMilli(),
			"updated": now.UnixMilli(),
		}).Execute()
		return err
	})
}

func storeNotionDiscoverySources(app core.App, jobID string, sources []stagedNotionSource, now time.Time) (int64, error) {
	if len(sources) == 0 {
		return 0, nil
	}
	encodedSources, err := json.Marshal(sources)
	if err != nil {
		return 0, err
	}
	result, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_import_sources}}
			([[job_id]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[position]], [[title]], [[text]], [[blueprint]],
			 [[discovery_status]], [[discovery_cursor]], [[discovery_position]], [[created]])
		SELECT {:jobId}, json_extract([[source]].[[value]], '$.sourceId'),
			json_extract([[source]].[[value]], '$.kind'), json_extract([[source]].[[value]], '$.parentSourceId'),
			json_extract([[source]].[[value]], '$.parentKind'),
			json_extract([[source]].[[value]], '$.position'), json_extract([[source]].[[value]], '$.title'),
			json_extract([[source]].[[value]], '$.text'),
			CASE WHEN json_extract([[source]].[[value]], '$.blueprint') THEN 1 ELSE 0 END,
			CASE WHEN json_extract([[source]].[[value]], '$.discoveryStatus') = 'pending'
				THEN 'pending' ELSE 'complete' END, '',
			COALESCE(json_extract([[source]].[[value]], '$.discoveryPosition'), 0), {:created}
		FROM json_each({:sources}) AS [[source]] WHERE TRUE
		ORDER BY CAST([[source]].[[key]] AS INTEGER)
		ON CONFLICT ([[job_id]], [[source_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": jobID, "sources": string(encodedSources), "created": now.UnixMilli(),
	}).Execute()
	if err != nil {
		return 0, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	files := make([]stagedNotionSource, 0, len(sources))
	for _, source := range sources {
		if notionImportFileSourceKind(source.Kind) && source.FileURLCipher != "" {
			files = append(files, source)
		}
	}
	if len(files) == 0 {
		return inserted, nil
	}
	encodedFiles, err := json.Marshal(files)
	if err != nil {
		return 0, err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_import_file_tasks}}
			([[job_id]], [[source_id]], [[name]], [[url_cipher]], [[status]], [[file_id]], [[error_code]], [[attempt]], [[created]], [[updated]])
		SELECT {:jobId}, json_extract([[file]].[[value]], '$.sourceId'),
			json_extract([[file]].[[value]], '$.title'), json_extract([[file]].[[value]], '$.fileURLCipher'),
			'pending', '', '', 0, {:now}, {:now}
		FROM json_each({:files}) AS [[file]] WHERE TRUE
		ORDER BY CAST([[file]].[[key]] AS INTEGER)
		ON CONFLICT ([[job_id]], [[source_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": jobID, "files": string(encodedFiles), "now": now.UnixMilli(),
	}).Execute()
	return inserted, err
}

func recordNotionImportChunk(app core.App, jobID string, index, count int, cursor, digest string, now time.Time) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_job_chunks}}
			([[job_id]], [[chunk_index]], [[item_count]], [[cursor]], [[sha256]], [[created]])
		VALUES ({:jobId}, {:chunkIndex}, {:itemCount}, {:cursor}, {:sha256}, {:created})
		ON CONFLICT ([[job_id]], [[chunk_index]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": jobID, "chunkIndex": index, "itemCount": count,
		"cursor": cursor, "sha256": digest, "created": now.UnixMilli(),
	}).Execute()
	return err
}

func notionDiscoveryDigest(sources []stagedNotionSource) string {
	hasher := sha256.New()
	for _, source := range sources {
		_, _ = fmt.Fprintf(hasher, "%s\x00%s\x00%d\n", source.SourceID, source.Kind, source.Position)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}
