package app

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace moved from another Leafen server brings its pages' history
// (the edit history decision of 2026-10-18: a page's history is never cut).
// The move keeps the bundle's IDs (workspaceImportKeepIDsSalt), so the
// history the old server sends after the bundle — each page's and database
// page's versions, events, checkpoints, history spans and bookmarks as it
// keeps them (workspace_move_history_out.go) — names this server's pages
// and blocks. Once the import has put the pages in, the history goes in
// place of the history the import wrote, with the people paired: inside
// its JSON and its Ygo updates (crdt.RewriteUpdateV1) a person paired names
// the account here and one not paired the stand-in the content names. The
// page's last version is then taken again from the page as imported: the
// import made its documents anew, and the edits after the move go on from
// them, while the versions before replay from the old checkpoints.

const (
	workspaceMoveHistoryLineBytes = 512 << 20
	workspaceMoveHistoryTurnLines = 2000
	workspaceMoveHistoryTurnBytes = 64 << 20
)

// workspaceMoveHistorySource is a moved workspace's history file and how
// far it is in: Offset the bytes read, Page the page whose lines are coming,
// Skip when that page isn't here.
type workspaceMoveHistorySource struct {
	StorageKey string `json:"storageKey"`
	SHA256     string `json:"sha256"`
	Bytes      int64  `json:"bytes"`
	Verified   bool   `json:"verified,omitempty"`
	Offset     int64  `json:"offset,omitempty"`
	Page       string `json:"page,omitempty"`
	Skip       bool   `json:"skip,omitzero"`
	Done       bool   `json:"done,omitempty"`
	// Files are those only the history names (workspace_move_history_files.go),
	// put in before it, FilesDone once they are.
	Files     []workspaceMoveHistoryFile `json:"files,omitempty"`
	FilesDone bool                       `json:"filesDone,omitzero"`
}

// workspaceMoveHistoryLine is one line of the file: a page's start, one of
// its rows, or its end.
type workspaceMoveHistoryLine struct {
	Page       string                          `json:"page,omitempty"`
	End        string                          `json:"end,omitempty"`
	Version    *workspaceMoveHistoryVersion    `json:"version,omitempty"`
	Event      *workspaceMoveHistoryEvent      `json:"event,omitempty"`
	Checkpoint *workspaceMoveHistoryCheckpoint `json:"checkpoint,omitempty"`
	BlockSpan  *workspaceMoveHistoryBlockSpan  `json:"blockSpan,omitempty"`
	TextSpan   *workspaceMoveHistoryTextSpan   `json:"textSpan,omitempty"`
	Bookmark   *workspaceMoveHistoryBookmark   `json:"bookmark,omitempty"`
}

type workspaceMoveHistoryVersion struct {
	ID             string `db:"id" json:"id"`
	VersionCursor  int    `db:"version_cursor" json:"versionCursor"`
	ActorID        string `db:"actor_id" json:"actorId"`
	Source         string `db:"source" json:"source"`
	CreatedAt      int64  `db:"created_at" json:"createdAt"`
	ConnectionName string `db:"connection_name" json:"connectionName,omitempty"`
}

type workspaceMoveHistoryEvent struct {
	VersionCursor int    `db:"version_cursor" json:"versionCursor"`
	Kind          string `db:"kind" json:"kind"`
	Payload       []byte `db:"payload" json:"payload"`
	CreatedAt     int64  `db:"created_at" json:"createdAt"`
}

// A checkpoint's columns are kept as stored, deflated or not.
type workspaceMoveHistoryCheckpoint struct {
	VersionCursor      int    `db:"version_cursor" json:"versionCursor"`
	PageJSON           []byte `db:"page_json" json:"pageJson"`
	BlocksJSON         []byte `db:"blocks_json" json:"blocksJson"`
	StructuralState    []byte `db:"structural_state" json:"structuralState"`
	CollaborationState []byte `db:"collaboration_state" json:"collaborationState"`
	DatabasesJSON      []byte `db:"databases_json" json:"databasesJson"`
	CreatedAt          int64  `db:"created_at" json:"createdAt"`
}

type workspaceMoveHistoryBlockSpan struct {
	BlockID      string `db:"block_id" json:"blockId"`
	ValidFrom    int    `db:"valid_from" json:"validFrom"`
	ValidThrough *int64 `db:"valid_through" json:"validThrough"`
	AfterID      string `db:"after_id" json:"afterId"`
	ParentID     string `db:"parent_id" json:"parentId"`
	Type         string `db:"type" json:"type"`
	Content      string `db:"content" json:"content"`
	CreatedAt    int64  `db:"created_at" json:"createdAt"`
	UpdatedAt    int64  `db:"updated_at" json:"updatedAt"`
}

type workspaceMoveHistoryTextSpan struct {
	BlockID      string `db:"block_id" json:"blockId"`
	ValidFrom    int    `db:"valid_from" json:"validFrom"`
	ValidThrough *int64 `db:"valid_through" json:"validThrough"`
	PlainText    string `db:"plain_text" json:"plainText"`
	RichText     string `db:"rich_text" json:"richText"`
}

type workspaceMoveHistoryBookmark struct {
	ID        string `db:"id" json:"id"`
	VersionID string `db:"version_id" json:"versionId"`
	Name      string `db:"name" json:"name"`
	AuthorID  string `db:"author_id" json:"authorId"`
	CreatedAt int64  `db:"created_at" json:"createdAt"`
	UpdatedAt int64  `db:"updated_at" json:"updatedAt"`
}

// applyWorkspaceMoveHistoryTurn takes in a turn of a moved workspace's
// history, once its pages are in.
func (r *durableJobRunner) applyWorkspaceMoveHistoryTurn(job storedJob, payload workspaceImportJobPayload) error {
	history := payload.History
	path := filepath.Join(r.app.DataDir(), workspaceImportDirectory, history.StorageKey)
	if filepath.Base(history.StorageKey) != history.StorageKey || history.StorageKey == "" {
		return workspaceImportStateErrorAt("move-history-key")
	}
	if !history.Verified {
		sum, size, err := workspaceMoveHistoryDigest(path)
		if err != nil || sum != history.SHA256 || size != history.Bytes {
			return workspaceImportInvalid("the moved workspace's history isn't the bytes the old server sent")
		}
		history.Verified = true
	}
	file, err := os.Open(path)
	if err != nil {
		return workspaceImportStorageError(err)
	}
	defer file.Close()
	if _, err := file.Seek(history.Offset, io.SeekStart); err != nil {
		return workspaceImportStorageError(err)
	}
	reader := bufio.NewReaderSize(file, 1<<20)
	people := workspaceMoveHistoryPeopleOf(payload.People)
	if len(history.Files) > 0 && !history.FilesDone {
		r.publishWorkspaceMoveHistoryFiles(job, history)
		history.FilesDone = true
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if history.Offset == 0 && history.Page == "" {
			if err := keepWorkspaceMovedPeople(txApp, current.WorkspaceID, payload.People); err != nil {
				return err
			}
		}
		read := int64(0)
		for lines := 0; lines < workspaceMoveHistoryTurnLines && read < workspaceMoveHistoryTurnBytes; lines++ {
			line, err := readWorkspaceMoveHistoryLine(reader)
			if errors.Is(err, io.EOF) && len(line) == 0 {
				if history.Page != "" {
					return workspaceImportInvalid("the moved workspace's history ends inside a page")
				}
				history.Done = true
				break
			}
			if err != nil {
				return err
			}
			read += int64(len(line))
			entry := workspaceMoveHistoryLine{}
			if json.Unmarshal(line, &entry) != nil {
				return workspaceImportInvalid("a line of the moved workspace's history isn't one Leafen reads")
			}
			if err := takeWorkspaceMoveHistoryLine(txApp, current.WorkspaceID, history, people, entry); err != nil {
				return err
			}
		}
		history.Offset += read
		payload.History = history
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}} SET [[payload_json]] = {:payload}, [[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "owner": current.Owner, "payload": string(encoded),
			"notBefore": now + durableJobReadyDelay.Milliseconds(), "updated": now,
		}).Execute()
		return err
	})
}

func readWorkspaceMoveHistoryLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > workspaceMoveHistoryLineBytes {
			return nil, workspaceImportInvalid("a line of the moved workspace's history is too long")
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			if len(line) > 0 && errors.Is(err, io.EOF) {
				return nil, workspaceImportInvalid("the moved workspace's history doesn't end with a line")
			}
			return line, err
		}
		return line, nil
	}
}

func workspaceMoveHistoryDigest(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, file)
	return hex.EncodeToString(digest.Sum(nil)), size, err
}

// takeWorkspaceMoveHistoryLine puts one line of the history in.
func takeWorkspaceMoveHistoryLine(app core.App, workspaceID string, history *workspaceMoveHistorySource,
	people workspaceMoveHistoryPeople, entry workspaceMoveHistoryLine) error {
	switch {
	case entry.Page != "":
		if history.Page != "" {
			return workspaceImportInvalid("a page's history starts inside another's")
		}
		history.Page, history.Skip = entry.Page, true
		var here int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]] = {:page} AND [[workspace]] = {:workspace}`).
			Bind(dbx.Params{"page": entry.Page, "workspace": workspaceID}).Row(&here); err != nil {
			return err
		}
		if here == 1 {
			history.Skip = false
			return clearWorkspaceMovedPageHistory(app, entry.Page)
		}
		return nil
	case entry.End != "":
		if entry.End != history.Page {
			return workspaceImportInvalid("a page's history ends without starting")
		}
		page, skip := history.Page, history.Skip
		history.Page, history.Skip = "", false
		if skip {
			return nil
		}
		return settleWorkspaceMovedPageHistory(app, workspaceID, page)
	case history.Page == "":
		return workspaceImportInvalid("a history row comes outside a page")
	case history.Skip:
		return nil
	}
	page := history.Page
	switch {
	case entry.Version != nil:
		row := entry.Version
		_, err := app.DB().Insert("leafen_page_history_versions", dbx.Params{
			"id": row.ID, "workspace_id": workspaceID, "page_id": page, "version_cursor": row.VersionCursor,
			"actor_id": people.id(row.ActorID), "source": row.Source, "created_at": row.CreatedAt,
			"connection_name": row.ConnectionName, "connection_id": "",
		}).Execute()
		return err
	case entry.Event != nil:
		row := entry.Event
		payload, err := people.event(row.Payload)
		if err != nil {
			return workspaceImportInvalid("an event of the moved workspace's history doesn't read")
		}
		_, err = app.DB().Insert("leafen_page_history_events", dbx.Params{
			"workspace_id": workspaceID, "page_id": page, "version_cursor": row.VersionCursor,
			"kind": row.Kind, "payload": payload, "created_at": row.CreatedAt,
		}).Execute()
		return err
	case entry.Checkpoint != nil:
		return takeWorkspaceMoveHistoryCheckpoint(app, workspaceID, page, people, *entry.Checkpoint)
	case entry.BlockSpan != nil:
		row := entry.BlockSpan
		content, err := people.json([]byte(row.Content))
		if err != nil {
			return workspaceImportInvalid("a block of the moved workspace's history doesn't read")
		}
		_, err = app.DB().Insert(pageHistoryBlockSpansTable, dbx.Params{
			"workspace_id": workspaceID, "page_id": page, "block_id": row.BlockID, "valid_from": row.ValidFrom,
			"valid_through": row.ValidThrough, "after_id": row.AfterID, "parent_id": row.ParentID, "type": row.Type,
			"content": string(content), "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		}).Execute()
		return err
	case entry.TextSpan != nil:
		row := entry.TextSpan
		rich, err := people.json([]byte(row.RichText))
		if err != nil {
			return workspaceImportInvalid("a text of the moved workspace's history doesn't read")
		}
		_, err = app.DB().Insert(pageHistoryTextSpansTable, dbx.Params{
			"workspace_id": workspaceID, "page_id": page, "block_id": row.BlockID, "valid_from": row.ValidFrom,
			"valid_through": row.ValidThrough, "plain_text": row.PlainText, "rich_text": string(rich),
		}).Execute()
		return err
	case entry.Bookmark != nil:
		row := entry.Bookmark
		_, err := app.DB().Insert("leafen_page_history_bookmarks", dbx.Params{
			"id": row.ID, "page_id": page, "version_id": row.VersionID, "name": row.Name,
			"author_id": people.id(row.AuthorID), "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		}).Execute()
		return err
	}
	return workspaceImportInvalid("a line of the moved workspace's history holds nothing Leafen keeps")
}

func takeWorkspaceMoveHistoryCheckpoint(app core.App, workspaceID, page string, people workspaceMoveHistoryPeople,
	row workspaceMoveHistoryCheckpoint) error {
	invalid := workspaceImportInvalid("a checkpoint of the moved workspace's history doesn't read")
	pageJSON, err := people.storedText(row.PageJSON)
	if err != nil {
		return invalid
	}
	blocksJSON, err := people.storedText(row.BlocksJSON)
	if err != nil {
		return invalid
	}
	databasesJSON, err := people.storedText(row.DatabasesJSON)
	if err != nil {
		return invalid
	}
	structural, err := people.stored(row.StructuralState, people.ygo)
	if err != nil {
		return invalid
	}
	collaboration, err := people.stored(row.CollaborationState, people.ygo)
	if err != nil {
		return invalid
	}
	_, err = app.DB().Insert("leafen_page_history_checkpoints", dbx.Params{
		"workspace_id": workspaceID, "page_id": page, "version_cursor": row.VersionCursor,
		"page_json": pageJSON, "blocks_json": blocksJSON, "structural_state": structural,
		"collaboration_state": collaboration, "databases_json": databasesJSON, "created_at": row.CreatedAt,
	}).Execute()
	return err
}

// clearWorkspaceMovedPageHistory drops the history the import wrote for a
// page whose own history comes.
func clearWorkspaceMovedPageHistory(app core.App, page string) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_page_history_bookmarks}} WHERE [[page_id]] = {:page};
		DELETE FROM {{leafen_page_history_snapshot_blocks}} WHERE [[materialization_id]] IN
			(SELECT [[materialization_id]] FROM {{leafen_page_history_snapshot_jobs}} WHERE [[page_id]] = {:page});
		DELETE FROM {{leafen_page_history_snapshot_jobs}} WHERE [[page_id]] = {:page};
		DELETE FROM {{leafen_page_history_block_spans}} WHERE [[page_id]] = {:page};
		DELETE FROM {{leafen_page_history_text_spans}} WHERE [[page_id]] = {:page};
		DELETE FROM {{leafen_page_history_events}} WHERE [[page_id]] = {:page};
		DELETE FROM {{leafen_page_history_checkpoints}} WHERE [[page_id]] = {:page};
		DELETE FROM {{leafen_page_history_restore_receipts}} WHERE [[page_id]] = {:page};
		DELETE FROM {{leafen_page_history_versions}} WHERE [[page_id]] = {:page};
	`).Bind(dbx.Params{"page": page}).Execute()
	return err
}

// settleWorkspaceMovedPageHistory takes the page's last version again from
// the page as imported, so the edits after the move replay on the documents
// the import made; a history that doesn't start where Leafen's starts goes,
// and the page's begins anew.
func settleWorkspaceMovedPageHistory(app core.App, workspaceID, page string) error {
	var last struct {
		Started  bool `db:"started"`
		Frontier int  `db:"frontier"`
	}
	if err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_page_history_checkpoints}} WHERE [[page_id]] = {:page} AND [[version_cursor]] = 0) AS [[started]],
			(SELECT COALESCE(MAX([[version_cursor]]), 0) FROM {{leafen_page_history_versions}} WHERE [[page_id]] = {:page}) AS [[frontier]]
	`).Bind(dbx.Params{"page": page}).One(&last); err != nil {
		return err
	}
	if !last.Started {
		if err := clearWorkspaceMovedPageHistory(app, page); err != nil {
			return err
		}
		return ensurePageHistoryBaseline(app, page)
	}
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_page_history_checkpoints}} WHERE [[page_id]] = {:page} AND [[version_cursor]] = {:cursor}`).
		Bind(dbx.Params{"page": page, "cursor": last.Frontier}).Execute(); err != nil {
		return err
	}
	return capturePageHistoryCheckpointAndSettle(app, workspaceID, page, last.Frontier, nil)
}
