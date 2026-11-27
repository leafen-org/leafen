package app

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The sending side of a moved workspace's history (workspace_move_history.go
// takes it in): the history of each page and database page the bundle
// carries — and only those, the pages its mover may read — as this server
// keeps it, a page at a time: its start, its versions, checkpoints, events,
// history spans and bookmarks, its end. A page whose history hasn't begun
// sends none, and keeps the history its import writes.

const (
	workspaceMoveBundleLineBytes  = 256 << 20
	workspaceMoveHistoryReadBatch = 256
)

// writeWorkspaceMoveHistory writes the history of the bundle's pages to
// outPath and gives its SHA-256 and length.
func writeWorkspaceMoveHistory(app core.App, workspaceID, bundlePath, outPath string) (string, int64, error) {
	pages, err := workspaceMoveBundlePages(bundlePath)
	if err != nil {
		return "", 0, err
	}
	temporary := outPath + ".tmp"
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(temporary)
	digest := sha256.New()
	counted := &countingWriter{writer: io.MultiWriter(file, digest)}
	buffered := bufio.NewWriterSize(counted, 1<<20)
	write := func(line workspaceMoveHistoryLine) error {
		encoded, err := json.Marshal(line)
		if err != nil {
			return err
		}
		if _, err := buffered.Write(append(encoded, '\n')); err != nil {
			return err
		}
		return nil
	}
	for _, page := range pages {
		var begun int
		if err := app.DB().NewQuery(`SELECT EXISTS(SELECT 1 FROM {{leafen_page_history_checkpoints}} [[checkpoint]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[checkpoint]].[[page_id]]
			WHERE [[checkpoint]].[[page_id]] = {:page} AND [[checkpoint]].[[version_cursor]] = 0 AND [[page]].[[workspace]] = {:workspace})`).
			Bind(dbx.Params{"page": page, "workspace": workspaceID}).Row(&begun); err != nil {
			_ = file.Close()
			return "", 0, err
		}
		if begun == 0 {
			continue
		}
		if err := writeWorkspaceMovePageHistory(app, page, write); err != nil {
			_ = file.Close()
			return "", 0, err
		}
	}
	if err := errors.Join(buffered.Flush(), file.Sync(), file.Close()); err != nil {
		return "", 0, err
	}
	if err := os.Rename(temporary, outPath); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(digest.Sum(nil)), counted.written, nil
}

type countingWriter struct {
	writer  io.Writer
	written int64
}

func (counter *countingWriter) Write(part []byte) (int, error) {
	n, err := counter.writer.Write(part)
	counter.written += int64(n)
	return n, err
}

// writeWorkspaceMovePageHistory writes one page's history, its rows read a
// batch at a time in their order.
func writeWorkspaceMovePageHistory(app core.App, page string, write func(workspaceMoveHistoryLine) error) error {
	if err := write(workspaceMoveHistoryLine{Page: page}); err != nil {
		return err
	}
	params := func(cursor int) dbx.Params {
		return dbx.Params{"page": page, "after": cursor, "limit": workspaceMoveHistoryReadBatch}
	}
	for after := -1; ; {
		rows := []workspaceMoveHistoryVersion{}
		if err := app.DB().NewQuery(`SELECT [[id]], [[version_cursor]], [[actor_id]], [[source]], [[created_at]], [[connection_name]]
			FROM {{leafen_page_history_versions}} WHERE [[page_id]] = {:page} AND [[version_cursor]] > {:after}
			ORDER BY [[version_cursor]] LIMIT {:limit}`).Bind(params(after)).All(&rows); err != nil {
			return err
		}
		for index := range rows {
			if err := write(workspaceMoveHistoryLine{Version: &rows[index]}); err != nil {
				return err
			}
			after = rows[index].VersionCursor
		}
		if len(rows) < workspaceMoveHistoryReadBatch {
			break
		}
	}
	// Checkpoints keep a whole page each: a few at a time.
	for after := -1; ; {
		rows := []workspaceMoveHistoryCheckpoint{}
		if err := app.DB().NewQuery(`SELECT [[version_cursor]], [[page_json]], [[blocks_json]], [[structural_state]],
				[[collaboration_state]], [[databases_json]], [[created_at]]
			FROM {{leafen_page_history_checkpoints}} WHERE [[page_id]] = {:page} AND [[version_cursor]] > {:after}
			ORDER BY [[version_cursor]] LIMIT 4`).Bind(params(after)).All(&rows); err != nil {
			return err
		}
		for index := range rows {
			if err := write(workspaceMoveHistoryLine{Checkpoint: &rows[index]}); err != nil {
				return err
			}
			after = rows[index].VersionCursor
		}
		if len(rows) < 4 {
			break
		}
	}
	for after := 0; ; {
		rows := []workspaceMoveHistoryEvent{}
		if err := app.DB().NewQuery(`SELECT [[version_cursor]], [[kind]], [[payload]], [[created_at]]
			FROM {{leafen_page_history_events}} WHERE [[page_id]] = {:page} AND [[version_cursor]] > {:after}
			ORDER BY [[version_cursor]] LIMIT {:limit}`).Bind(params(after)).All(&rows); err != nil {
			return err
		}
		for index := range rows {
			if err := write(workspaceMoveHistoryLine{Event: &rows[index]}); err != nil {
				return err
			}
			after = rows[index].VersionCursor
		}
		if len(rows) < workspaceMoveHistoryReadBatch {
			break
		}
	}
	if err := writeWorkspaceMoveHistorySpans(app, page, write); err != nil {
		return err
	}
	bookmarks := []workspaceMoveHistoryBookmark{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[version_id]], [[name]], [[author_id]], [[created_at]], [[updated_at]]
		FROM {{leafen_page_history_bookmarks}} WHERE [[page_id]] = {:page} ORDER BY [[id]]`).
		Bind(dbx.Params{"page": page}).All(&bookmarks); err != nil {
		return err
	}
	for index := range bookmarks {
		if err := write(workspaceMoveHistoryLine{Bookmark: &bookmarks[index]}); err != nil {
			return err
		}
	}
	return write(workspaceMoveHistoryLine{End: page})
}

func writeWorkspaceMoveHistorySpans(app core.App, page string, write func(workspaceMoveHistoryLine) error) error {
	for block, from := "", -1; ; {
		rows := []workspaceMoveHistoryBlockSpan{}
		if err := app.DB().NewQuery(`SELECT [[block_id]], [[valid_from]], [[valid_through]], [[after_id]], [[parent_id]], [[type]],
				[[content]], [[created_at]], [[updated_at]]
			FROM {{leafen_page_history_block_spans}} WHERE [[page_id]] = {:page}
				AND ([[block_id]] > {:block} OR ([[block_id]] = {:block} AND [[valid_from]] > {:from}))
			ORDER BY [[block_id]], [[valid_from]] LIMIT {:limit}`).
			Bind(dbx.Params{"page": page, "block": block, "from": from, "limit": workspaceMoveHistoryReadBatch}).All(&rows); err != nil {
			return err
		}
		for index := range rows {
			if err := write(workspaceMoveHistoryLine{BlockSpan: &rows[index]}); err != nil {
				return err
			}
			block, from = rows[index].BlockID, rows[index].ValidFrom
		}
		if len(rows) < workspaceMoveHistoryReadBatch {
			break
		}
	}
	for block, from := "", -1; ; {
		rows := []workspaceMoveHistoryTextSpan{}
		if err := app.DB().NewQuery(`SELECT [[block_id]], [[valid_from]], [[valid_through]], [[plain_text]], [[rich_text]]
			FROM {{leafen_page_history_text_spans}} WHERE [[page_id]] = {:page}
				AND ([[block_id]] > {:block} OR ([[block_id]] = {:block} AND [[valid_from]] > {:from}))
			ORDER BY [[block_id]], [[valid_from]] LIMIT {:limit}`).
			Bind(dbx.Params{"page": page, "block": block, "from": from, "limit": workspaceMoveHistoryReadBatch}).All(&rows); err != nil {
			return err
		}
		for index := range rows {
			if err := write(workspaceMoveHistoryLine{TextSpan: &rows[index]}); err != nil {
				return err
			}
			block, from = rows[index].BlockID, rows[index].ValidFrom
		}
		if len(rows) < workspaceMoveHistoryReadBatch {
			break
		}
	}
	return nil
}

// workspaceMoveBundlePages are the pages and database pages a bundle
// carries, in its order.
func workspaceMoveBundlePages(bundlePath string) ([]string, error) {
	archive, err := zip.OpenReader(bundlePath)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if entry.Name != workspaceExportEntitiesPath {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		lines := bufio.NewReaderSize(reader, 1<<20)
		pages, seen := []string{}, map[string]bool{}
		for {
			line, err := readWorkspaceMoveBundleLine(lines)
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
			if len(bytes.TrimSpace(line)) == 0 {
				if err != nil {
					return pages, nil
				}
				continue
			}
			entity := struct {
				Kind  string `json:"kind"`
				Value struct {
					ID string `json:"id"`
				} `json:"value"`
			}{}
			if json.Unmarshal(line, &entity) != nil {
				return nil, errors.New("a line of the bundle's entities doesn't read")
			}
			if (entity.Kind == workspaceExportItemPage || entity.Kind == workspaceExportItemRow) && entity.Value.ID != "" && !seen[entity.Value.ID] {
				seen[entity.Value.ID] = true
				pages = append(pages, entity.Value.ID)
			}
			if err != nil {
				return pages, nil
			}
		}
	}
	return nil, errors.New("the bundle has no entities")
}

func readWorkspaceMoveBundleLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > workspaceMoveBundleLineBytes {
			return nil, errors.New("a line of the bundle's entities is too long")
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return line, err
	}
}
