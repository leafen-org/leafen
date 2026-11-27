package app

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type workspaceImportArchiveTransformPass struct {
	kind       string
	mode       string
	pageSorted bool
	// level is a database's nesting for its data source, properties, views
	// and rows: a page's databases are level 0, a database in the body of a
	// level-n database's page is level n+1.
	level int
}

// workspaceImportArchiveNestingLevels is the fewest nesting levels an
// archive is written in. An archive whose databases nest deeper is written in
// as many as it has (2026-10-13: past 8 the deeper databases and all in them
// were left out without a word); the fewest stays 8 so an import already
// under way goes on through the same passes.
const workspaceImportArchiveNestingLevels = 8

// workspaceImportArchiveTransformPassesFor writes an archive in the order it
// is made: pages, then each nesting level's databases and their rows' pages,
// whose bodies may hold the next level's; then the rows' values, the pages'
// and database pages' blocks, and what refers to them.
func workspaceImportArchiveTransformPassesFor(levels int) []workspaceImportArchiveTransformPass {
	levels = max(levels, workspaceImportArchiveNestingLevels)
	passes := []workspaceImportArchiveTransformPass{{kind: workspaceExportItemPage}}
	for level := range levels {
		passes = append(passes,
			workspaceImportArchiveTransformPass{kind: workspaceExportItemDataSource, mode: "full", pageSorted: true, level: level},
			workspaceImportArchiveTransformPass{kind: workspaceExportItemProperty, mode: "full", pageSorted: true, level: level},
			workspaceImportArchiveTransformPass{kind: workspaceExportItemView, mode: "full", pageSorted: true, level: level},
			workspaceImportArchiveTransformPass{kind: workspaceExportItemRow, mode: "skeleton", pageSorted: true, level: level},
		)
	}
	for level := range levels {
		passes = append(passes,
			workspaceImportArchiveTransformPass{kind: workspaceExportItemRow, mode: "full", pageSorted: true, level: level})
	}
	for _, mode := range []string{"root_source", "root_remaining", "full_source", "full_remaining"} {
		passes = append(passes,
			workspaceImportArchiveTransformPass{kind: workspaceExportItemBlock, mode: mode, pageSorted: true},
			workspaceImportArchiveTransformPass{kind: workspaceExportItemRowPageBlock, mode: mode, pageSorted: true},
		)
	}
	return append(passes,
		workspaceImportArchiveTransformPass{kind: workspaceExportItemTemplate},
		workspaceImportArchiveTransformPass{kind: workspaceExportItemComment},
		workspaceImportArchiveTransformPass{kind: workspaceExportItemBlockTrash, pageSorted: true},
		workspaceImportArchiveTransformPass{kind: workspaceExportItemRowBlockTrash, pageSorted: true},
		workspaceImportArchiveTransformPass{kind: workspaceExportItemCustomEmoji},
		// A database's automations, once its views and properties are there.
		workspaceImportArchiveTransformPass{kind: workspaceExportItemDatabaseAutomation},
	)
}

// workspaceImportArchiveTransformPassesOf are an archive's passes, as many
// levels as its databases nest.
func workspaceImportArchiveTransformPassesOf(db *sql.DB) ([]workspaceImportArchiveTransformPass, error) {
	var levels int
	if err := db.QueryRow(`SELECT COALESCE(MAX(level), -1) + 1 FROM source_levels`).Scan(&levels); err != nil {
		return nil, workspaceImportStorageError(err)
	}
	return workspaceImportArchiveTransformPassesFor(levels), nil
}

// workspaceImportArchiveLeveledKind reports whether a kind's entities are
// written level by level.
func workspaceImportArchiveLeveledKind(kind string) bool {
	return kind == workspaceExportItemDataSource || kind == workspaceExportItemProperty ||
		kind == workspaceExportItemView || kind == workspaceExportItemRow
}

// workspaceImportArchiveLevelFilter keeps a level's entities of a leveled
// kind: a data source by its own level, the others by their data source's.
func workspaceImportArchiveLevelFilter(pass workspaceImportArchiveTransformPass) (string, []any) {
	if !workspaceImportArchiveLeveledKind(pass.kind) {
		return "", nil
	}
	if pass.kind == workspaceExportItemDataSource {
		return ` AND source_id IN (SELECT source_id FROM source_levels WHERE level = ?)`, []any{pass.level}
	}
	return ` AND json_extract(value_json, '$.dataSourceId') IN (SELECT source_id FROM source_levels WHERE level = ?)`,
		[]any{pass.level}
}

// workspaceImportArchivePassFilter keeps the entities a pass writes, the same
// for an archive prepared as it uploads and one read whole: a linked
// database's views come with its block (workspaceImportArchiveTransformItems),
// not among its source's views, and a block pass takes either the databases
// holding their own source or the other blocks.
func workspaceImportArchivePassFilter(pass workspaceImportArchiveTransformPass) (string, error) {
	switch {
	case pass.kind == workspaceExportItemView:
		return ` AND COALESCE(json_extract(value_json, '$.containerId'), '') = ''`, nil
	case pass.kind == workspaceExportItemBlock || pass.kind == workspaceExportItemRowPageBlock && pass.mode != "":
		_, sourceOnly, valid := workspaceImportArchiveBlockMode(pass.mode)
		if !valid {
			return "", workspaceImportInputError()
		}
		if sourceOnly {
			return ` AND (` + workspaceImportArchiveSourcePredicate + `)`, nil
		}
		return ` AND NOT (` + workspaceImportArchiveSourcePredicate + `)`, nil
	}
	return "", nil
}

// workspaceImportArchiveSourcePredicate holds for a block that is a
// database's own inline block.
const workspaceImportArchiveSourcePredicate = `json_extract(value_json, '$.type') = 'inline_database'
	AND COALESCE(json_extract(value_json, '$.content.dataSourceId'), '') != ''
	AND COALESCE(json_extract(value_json, '$.content.linkedDatabaseSource'), 0) = 0`

// ensureWorkspaceImportArchiveSourceLevels records each archived data
// source's nesting level once, however deep: a nesting can't go past the
// number of data sources but by a cycle, which ends there.
func ensureWorkspaceImportArchiveSourceLevels(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS source_levels (
		source_id TEXT PRIMARY KEY NOT NULL, level INTEGER NOT NULL)`); err != nil {
		return workspaceImportStorageError(err)
	}
	var recorded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM source_levels`).Scan(&recorded); err != nil {
		return workspaceImportStorageError(err)
	}
	if recorded != 0 {
		return nil
	}
	if _, err := db.Exec(`
		WITH RECURSIVE levels(source_id, level) AS (
			SELECT source.source_id, 0 FROM entities AS source
			WHERE source.kind = ? AND EXISTS (
				SELECT 1 FROM entities AS host WHERE host.kind = ? AND host.source_id = source.page_id)
			UNION
			SELECT source.source_id, levels.level + 1 FROM entities AS source
			JOIN entities AS row ON row.kind = ? AND row.source_id = source.page_id
			JOIN levels ON levels.source_id = json_extract(row.value_json, '$.dataSourceId')
			WHERE source.kind = ? AND levels.level + 1 < (SELECT COUNT(*) FROM entities WHERE kind = ?)
		)
		INSERT OR IGNORE INTO source_levels (source_id, level)
		SELECT source_id, MIN(level) FROM levels GROUP BY source_id
	`, workspaceExportItemDataSource, workspaceExportItemPage, workspaceExportItemRow,
		workspaceExportItemDataSource, workspaceExportItemDataSource); err != nil {
		return workspaceImportStorageError(err)
	}
	return nil
}

type workspaceImportArchiveTransformRow struct {
	sequence int
	sourceID string
	pageID   string
	encoded  string
}

func (r *durableJobRunner) transformWorkspaceArchiveTurn(
	job storedJob,
	payload workspaceImportJobPayload,
	db *sql.DB,
	state workspaceImportArchivePreparationState,
) error {
	rows, nextState, complete, err := loadWorkspaceImportArchiveTransformRows(db, state)
	if err != nil {
		return err
	}
	if len(rows) == 0 && !complete {
		return workspaceImportStateErrorAt("archive-transform-frontier")
	}
	directory := filepath.Join(r.app.DataDir(), workspaceImportDirectory)
	path := workspaceImportArchivePreparationOutputPath(directory, job.ID)
	file, err := openWorkspaceImportArchiveTransformOutput(path, state.OutputBytes)
	if err != nil {
		return err
	}
	hasher, err := workspaceImportArchivePreparationHasher(state.OutputHashState)
	if err != nil {
		_ = file.Close()
		return err
	}
	writer := &workspaceImportArchiveChunkWriter{
		file: file, hasher: hasher, written: state.OutputBytes,
		index:       state.OutputChunks,
		chunkHashes: append([]string(nil), state.OutputChunkHashes...),
	}
	passes, err := workspaceImportArchiveTransformPassesOf(db)
	if err != nil {
		return err
	}
	pass := passes[state.TransformPass]
	for index, row := range rows {
		items, title, err := workspaceImportArchiveTransformItems(db, pass, row)
		if err != nil {
			_ = file.Close()
			return workspaceImportAt(err, fmt.Sprintf("%s line %d", workspaceExportEntitiesPath, row.sequence+1))
		}
		if state.TransformPass == 0 && state.TransformSequence < 0 && index == 0 && title != "" {
			nextState.Title = title
		}
		for _, item := range items {
			if err := writer.append(item); err != nil {
				_ = file.Close()
				return err
			}
		}
	}
	if err := writer.flush(complete); err != nil {
		_ = file.Close()
		return err
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-transform-output-sync")
	}
	nextState.OutputBytes = writer.written
	nextState.OutputChunks = writer.index
	nextState.OutputChunkHashes = writer.chunkHashes
	nextState.OutputHashState, err = marshalWorkspaceImportArchiveHash(hasher)
	if err != nil {
		return err
	}
	if complete {
		nextState.OutputSHA256 = hex.EncodeToString(hasher.Sum(nil))
		nextState.Phase = workspaceImportArchivePreparePhaseTransformFinish
	}
	return r.commitWorkspaceArchivePreparationTurn(job, payload, db, nextState)
}

func loadWorkspaceImportArchiveTransformRows(
	db *sql.DB,
	state workspaceImportArchivePreparationState,
) ([]workspaceImportArchiveTransformRow, workspaceImportArchivePreparationState, bool, error) {
	if err := ensureWorkspaceImportArchiveSourceLevels(db); err != nil {
		return nil, state, false, err
	}
	if err := ensureWorkspaceImportArchivePageOrder(db); err != nil {
		return nil, state, false, err
	}
	passes, err := workspaceImportArchiveTransformPassesOf(db)
	if err != nil {
		return nil, state, false, err
	}
	nextPass := func() {
		state.TransformPass++
		state.TransformPageCursor = ""
		state.TransformSequence = -1
	}
	for state.TransformPass < len(passes) {
		pass := passes[state.TransformPass]
		if state.EntityCounts[pass.kind] == 0 {
			nextPass()
			continue
		}
		query, args, err := workspaceImportArchiveTransformQuery(
			pass, state.TransformPageCursor, state.TransformSequence, workspaceImportMutationTurnItems+1,
		)
		if err != nil {
			return nil, state, false, err
		}
		result, err := queryWorkspaceImportArchiveTransformRows(db, query, args...)
		if err != nil {
			return nil, state, false, err
		}
		more := len(result) > workspaceImportMutationTurnItems
		if more {
			result = result[:workspaceImportMutationTurnItems]
		}
		if len(result) == 0 {
			nextPass()
			continue
		}
		last := result[len(result)-1]
		state.TransformPageCursor = last.pageID
		state.TransformSequence = last.sequence
		if !more {
			// The rows end the transform when no later pass has any, so that
			// their chunk is the last one.
			nextPass()
			for state.TransformPass < len(passes) {
				later := passes[state.TransformPass]
				if state.EntityCounts[later.kind] != 0 {
					query, args, err := workspaceImportArchiveTransformQuery(later, "", -1, 1)
					if err != nil {
						return nil, state, false, err
					}
					found, err := queryWorkspaceImportArchiveTransformRows(db, query, args...)
					if err != nil {
						return nil, state, false, err
					}
					if len(found) != 0 {
						break
					}
				}
				state.TransformPass++
			}
		}
		return result, state, state.TransformPass == len(passes), nil
	}
	return nil, state, true, nil
}

// workspaceImportArchiveTransformQuery reads a pass's entities after the
// cursor in the order the pass writes them.
func workspaceImportArchiveTransformQuery(
	pass workspaceImportArchiveTransformPass,
	pageCursor string,
	sequence, limit int,
) (string, []any, error) {
	levelFilter, levelArgs := workspaceImportArchiveLevelFilter(pass)
	// Pages come parents first (page_order), their cursor a rank; an import
	// begun by sequence goes on by it.
	if pass.kind == workspaceExportItemPage && (workspaceImportArchivePageRankCursor(pageCursor) || pageCursor == "" && sequence < 0) {
		return `
			SELECT entities.sequence, entities.source_id, printf('%012d', page_order.rank), entities.value_json
			FROM page_order JOIN entities ON entities.kind = ? AND entities.sequence = page_order.sequence
			WHERE printf('%012d', page_order.rank) > ? ORDER BY page_order.rank LIMIT ?
		`, []any{pass.kind, pageCursor, limit}, nil
	}
	if !pass.pageSorted {
		return `
			SELECT sequence, source_id, page_id, value_json FROM entities
			WHERE kind = ? AND sequence > ?` + levelFilter + ` ORDER BY sequence LIMIT ?
		`, append(append([]any{pass.kind, sequence}, levelArgs...), limit), nil
	}
	passFilter, err := workspaceImportArchivePassFilter(pass)
	if err != nil {
		return "", nil, err
	}
	filter := levelFilter + passFilter
	return `
		SELECT sequence, source_id, page_id, value_json FROM entities
		WHERE kind = ? AND (page_id > ? OR (page_id = ? AND sequence > ?))` + filter + `
		ORDER BY page_id, sequence LIMIT ?
	`, append(append([]any{pass.kind, pageCursor, pageCursor, sequence}, levelArgs...), limit), nil
}

func queryWorkspaceImportArchiveTransformRows(
	db *sql.DB,
	query string,
	args ...any,
) ([]workspaceImportArchiveTransformRow, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, workspaceImportStorageErrorAt(err, "archive-transform-read")
	}
	defer rows.Close()
	result := []workspaceImportArchiveTransformRow{}
	for rows.Next() {
		row := workspaceImportArchiveTransformRow{}
		if rows.Scan(&row.sequence, &row.sourceID, &row.pageID, &row.encoded) != nil {
			return nil, workspaceImportInputError()
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, workspaceImportStorageErrorAt(err, "archive-transform-read")
	}
	return result, nil
}

// workspaceImportArchiveTransformItems are what one archived entity writes:
// its own item, a table's with its rows after it, or none for a table row a
// pass already wrote with its table.
func workspaceImportArchiveTransformItems(
	db *sql.DB,
	pass workspaceImportArchiveTransformPass,
	row workspaceImportArchiveTransformRow,
) ([]workspaceImportStagedItem, string, error) {
	blockType := ""
	if workspaceImportArchiveRootTableMode(pass.kind, pass.mode) {
		var err error
		if blockType, err = workspaceImportArchiveBlockType(row.encoded); err != nil {
			return nil, "", err
		}
		if blockType == graphBlockTypeTableRow {
			return nil, "", nil
		}
	}
	item, title, err := workspaceImportArchiveTransformItem(db, pass, row)
	if err != nil {
		return nil, "", err
	}
	items := []workspaceImportStagedItem{item}
	if blockType == graphBlockTypeTable {
		tableRows, err := workspaceImportArchiveTableRowItems(db, pass.kind, pass.mode, row.sourceID, item.PageID, item.Operation.Block.ID)
		if err != nil {
			return nil, "", err
		}
		items = append(items, tableRows...)
	}
	if (pass.kind == workspaceExportItemBlock || pass.kind == workspaceExportItemRowPageBlock) && pass.mode == "root_remaining" {
		views, err := workspaceImportArchiveLinkedViewItems(db, row.encoded, item)
		if err != nil {
			return nil, "", err
		}
		items = append(items, views...)
	}
	return items, title, nil
}

func workspaceImportArchiveTransformItem(
	db *sql.DB,
	pass workspaceImportArchiveTransformPass,
	row workspaceImportArchiveTransformRow,
) (workspaceImportStagedItem, string, error) {
	switch pass.kind {
	case workspaceExportItemPage:
		item, title, err := workspaceImportArchivePageItem(db, row.encoded, float64(row.sequence))
		return item, title, err
	case workspaceExportItemTemplate:
		item, err := workspaceImportArchiveTemplateItem(db, row.encoded)
		return item, "", err
	case workspaceExportItemComment:
		item, err := workspaceImportArchiveCommentItem(db, row.encoded)
		return item, "", err
	case workspaceExportItemBlockTrash:
		item, err := workspaceImportArchiveBlockTrashItem(db, row.encoded)
		return item, "", err
	case workspaceExportItemRowPageBlock:
		item, err := workspaceImportArchiveRowBlockOperation(db, pass.mode, row.encoded)
		return item, "", err
	case workspaceExportItemRowBlockTrash:
		item, err := workspaceImportArchiveRowBlockTrashItem(db, row.encoded)
		return item, "", err
	case workspaceExportItemCustomEmoji:
		item, err := workspaceImportArchiveCustomEmojiItem(db, row.encoded)
		return item, "", err
	case workspaceExportItemDatabaseAutomation:
		item, err := workspaceImportArchiveAutomationItem(db, row.encoded)
		return item, "", err
	default:
		pageID, err := workspaceImportArchiveHostReference(db, row.pageID)
		if err != nil {
			return workspaceImportStagedItem{}, "", err
		}
		item, err := workspaceImportArchiveOperation(
			db, pass.kind, pass.mode, row.sourceID, pageID, row.encoded,
		)
		return item, "", err
	}
}

func openWorkspaceImportArchiveTransformOutput(path string, frontier int64) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, workspaceImportStorageErrorAt(err, "archive-transform-output-open")
	}
	info, statErr := file.Stat()
	if statErr != nil || info.Size() < frontier {
		_ = file.Close()
		return nil, workspaceImportStorageErrorAt(statErr, "archive-transform-output-size")
	}
	if info.Size() > frontier {
		if err := file.Truncate(frontier); err != nil {
			_ = file.Close()
			return nil, workspaceImportStorageErrorAt(err, "archive-transform-output-truncate")
		}
	}
	if _, err := file.Seek(frontier, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, workspaceImportStorageErrorAt(err, "archive-transform-output-seek")
	}
	return file, nil
}

func (r *durableJobRunner) finalizeWorkspaceArchiveTransformTurn(
	job storedJob,
	payload workspaceImportJobPayload,
	db *sql.DB,
	state workspaceImportArchivePreparationState,
) error {
	directory := filepath.Join(r.app.DataDir(), workspaceImportDirectory)
	temporaryPath := workspaceImportArchivePreparationOutputPath(directory, job.ID)
	finalPath := filepath.Join(directory, job.ID+".ndjson")
	if err := renameWorkspaceImportArchivePreparedOutput(
		temporaryPath, finalPath, state.OutputBytes,
	); err != nil {
		return err
	}
	state.Phase = workspaceImportArchivePreparePhaseDLP
	return r.commitWorkspaceArchivePreparationTurn(job, payload, db, state)
}

func renameWorkspaceImportArchivePreparedOutput(
	temporaryPath, finalPath string,
	expectedBytes int64,
) error {
	if info, err := os.Stat(temporaryPath); err == nil {
		if info.Size() != expectedBytes {
			return workspaceImportStateErrorAt("archive-transform-output-finalize")
		}
		if err := os.Rename(temporaryPath, finalPath); err != nil {
			return workspaceImportStorageErrorAt(err, "archive-transform-output-finalize")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		info, finalErr := os.Stat(finalPath)
		if finalErr != nil || info.Size() != expectedBytes {
			return workspaceImportStateErrorAt("archive-transform-output-finalize")
		}
	} else {
		return workspaceImportStorageErrorAt(err, "archive-transform-output-finalize")
	}
	return syncDirectory(filepath.Dir(finalPath))
}

func workspaceImportArchivePreparedSource(
	jobID string,
	state workspaceImportArchivePreparationState,
	archiveSource storedWorkspaceImportSource,
) (storedWorkspaceImportSource, error) {
	chunkHashes, err := json.Marshal(state.OutputChunkHashes)
	if err != nil || state.OutputChunks < 1 || state.OutputBytes < 1 || len(state.OutputSHA256) != 64 {
		return storedWorkspaceImportSource{}, workspaceImportStateErrorAt("archive-transform-source")
	}
	return storedWorkspaceImportSource{
		JobID: jobID, SchemaVersion: 3, Format: workspaceImportFormatNativeArchiveV2,
		StorageKey: jobID + ".ndjson", SHA256: state.OutputSHA256,
		SourceBytes: state.OutputBytes, ChunkCount: state.OutputChunks,
		ChunkHashesJSON:     string(chunkHashes),
		ArchiveStorageKey:   archiveSource.ArchiveStorageKey,
		ArchiveSHA256:       archiveSource.ArchiveSHA256,
		ArchiveBytes:        archiveSource.ArchiveBytes,
		FileIndexStorageKey: state.Files.FileIndexStorageKey,
		FileIndexSHA256:     state.Files.FileIndexSHA256,
		FileCount:           state.Files.FileCount, FileBytes: state.Files.FileBytes,
	}, nil
}
