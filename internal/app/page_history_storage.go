package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	pageHistoryVersionBaseline = "baseline"
	// pageHistoryVersionLock is a version that only locked or unlocked an
	// inline database: its event is the graph change, its listing a lock.
	pageHistoryVersionLock = "lock"
	// pageHistoryVersionCommentMarks is a version that only marked text with
	// the discussion left on it — a page's text, or the words of a database
	// page's value (comment_cell_anchors.go): its event is the text, graph or
	// page-state change, replayed as any other, but a discussion is no edit of
	// the page, as Notion's updates list none for one and its last edited
	// time stays (seen 2026-11-11): it is not among the page's versions,
	// updates, editors or its blocks' last edits, nor its activity or
	// webhooks (pageHistoryEditVersion).
	pageHistoryVersionCommentMarks = "comment_marks"
	pageHistoryEventGraph          = "graph"
	pageHistoryEventCollaboration  = "collaboration"
	pageHistoryEventPage           = "page"
	pageHistoryEventMetadata       = "metadata"
	pageHistoryEventRestore        = "restore"
	pageHistoryEventRewrite        = "rewrite"
	// An agent's last note when it says it is done on the page; the page
	// itself is as it was.
	pageHistoryEventAgentNote = "agent_note"
)

type pageHistoryPageState struct {
	Title string `json:"title"`
	// TitleSpans are an ordinary page's title's (page_title_spans.go).
	TitleSpans             string `json:"titleSpans,omitempty"`
	Content                string `json:"content"`
	Icon                   string `json:"icon"`
	IconType               string `json:"iconType"`
	Cover                  string `json:"cover"`
	CoverPosition          int    `json:"coverPosition"`
	Font                   string `json:"font"`
	SmallText              bool   `json:"smallText"`
	FullWidth              bool   `json:"fullWidth"`
	BacklinksDisplay       string `json:"backlinksDisplay"`
	PageCommentsDisplay    string `json:"pageCommentsDisplay"`
	TableOfContentsDisplay string `json:"tableOfContentsDisplay"`
	InlineCommentsDisplay  string `json:"inlineCommentsDisplay"`
	// Values are a database page's property values.
	Values map[string]pageHistoryRowValue `json:"values,omitempty"`
}

type pageHistoryRowValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
	Spans json.RawMessage `json:"spans,omitempty"`
}

type pageHistoryBlockState struct {
	ID        string          `json:"id" db:"id"`
	ParentID  string          `json:"parentId" db:"parent_id"`
	Position  string          `json:"position" db:"position"`
	Type      string          `json:"type" db:"type"`
	Content   json.RawMessage `json:"content" db:"content"`
	CreatedAt int64           `json:"createdAt" db:"created"`
	UpdatedAt int64           `json:"updatedAt" db:"updated"`
	PlainText string          `json:"plainText"`
}

type storedPageHistoryBlockState struct {
	ID        string `db:"id"`
	ParentID  string `db:"parent_id"`
	Position  string `db:"position"`
	Type      string `db:"type"`
	Content   string `db:"content"`
	CreatedAt int64  `db:"created"`
	UpdatedAt int64  `db:"updated"`
}

type pageHistoryCheckpoint struct {
	PageID             string `db:"page_id"`
	VersionCursor      int    `db:"version_cursor"`
	PageJSON           string `db:"page_json"`
	BlocksJSON         string `db:"blocks_json"`
	StructuralState    []byte `db:"structural_state"`
	CollaborationState []byte `db:"collaboration_state"`
	// DatabasesJSON is the page's databases, '' in a checkpoint kept before
	// the history kept them.
	DatabasesJSON string `db:"databases_json"`
}

type storedPageHistoryEvent struct {
	VersionCursor int    `db:"version_cursor"`
	Kind          string `db:"kind"`
	Payload       []byte `db:"payload"`
	CreatedAt     int64  `db:"created_at"`
}

type storedPageHistoryVersion struct {
	ID            string `db:"id"`
	PageID        string `db:"page_id"`
	VersionCursor int    `db:"version_cursor"`
	ActorID       string `db:"actor_id"`
	Source        string `db:"source"`
	CreatedAt     int64  `db:"created_at"`
	// ConnectionName names the connection the change came through, if any,
	// and ConnectionID is which one it is.
	ConnectionName string `db:"connection_name"`
	ConnectionID   string `db:"connection_id"`
}

func ensurePageHistoryBaseline(app core.App, pageID string) error {
	return ensurePageHistoryBaselineAs(app, pageID, pageHistoryConnection{})
}

// ensureCreatedPageHistoryBaseline is a new page's first version, which
// names the connection that made the page, if one did.
func ensureCreatedPageHistoryBaseline(app core.App, pageID string) error {
	connection := pageHistoryConnectionOf(app)
	if err := ensurePageHistoryBaselineAs(app, pageID, connection); err != nil {
		return err
	}
	// A full-page database is told of as a database, once its block is made
	// (event_webhook_database_events.go).
	if page, err := app.FindRecordById(pagesCollection, pageID); err == nil && !isFullPageDatabase(page) {
		recordEventWebhookEvent(app, eventWebhookEventInput{
			WorkspaceID: page.GetString("workspace"), Type: eventWebhookPageCreated, EntityID: page.Id,
			EntityType: "page", PageID: page.Id, ActorID: page.GetString("created_by"), Connection: connection,
			Data: map[string]any{"parent": eventWebhookParent(page)},
		})
	}
	return nil
}

func ensurePageHistoryBaselineAs(app core.App, pageID string, connection pageHistoryConnection) error {
	var checkpointExists int
	if err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_page_history_checkpoints}}
			WHERE [[page_id]]={:pageId} AND [[version_cursor]]=0)
	`).Bind(dbx.Params{"pageId": pageID}).Row(&checkpointExists); err != nil {
		return err
	}
	if checkpointExists == 0 {
		if err := capturePageHistoryCheckpoint(app, pageID, 0, nil); err != nil {
			return err
		}
	}
	if err := ensurePageHistoryProjectionBaseline(app, pageID); err != nil {
		return err
	}
	var versionExists int
	if err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_page_history_versions}}
			WHERE [[page_id]]={:pageId} AND [[version_cursor]]=0)
	`).Bind(dbx.Params{"pageId": pageID}).Row(&versionExists); err != nil {
		return err
	}
	if versionExists == 1 {
		return nil
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	return insertPageHistoryVersion(app, connection, dbx.Params{
		"id": pageHistoryVersionID(pageID, 0), "workspace_id": page.GetString("workspace"),
		"page_id": pageID, "version_cursor": 0,
		"actor_id": page.GetString("created_by"), "source": pageHistoryVersionBaseline,
		"created_at": pageHistoryMoment(app), "connection_name": connection.Name,
		"connection_id": connection.ID,
	})
}

func capturePageHistoryCheckpoint(app core.App, pageID string, versionCursor int, documents *pageHistoryDocuments) error {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	state, err := pageHistoryPageStateFor(app, page)
	if err != nil {
		return err
	}
	pageJSON, err := json.Marshal(state)
	if err != nil {
		return err
	}
	storedBlocks := []storedPageHistoryBlockState{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[parent_id]],[[position]],[[type]],[[content]],[[created]],[[updated]]
		FROM {{leafen_blocks}} WHERE [[page_id]]={:pageId}
		ORDER BY [[parent_id]],[[position]],[[id]]
	`).Bind(dbx.Params{"pageId": pageID}).All(&storedBlocks); err != nil {
		return err
	}
	blocks := make([]pageHistoryBlockState, len(storedBlocks))
	for index, block := range storedBlocks {
		blocks[index] = pageHistoryBlockState{
			ID: block.ID, ParentID: block.ParentID, Position: block.Position,
			Type: block.Type, Content: json.RawMessage(block.Content),
			CreatedAt: block.CreatedAt, UpdatedAt: block.UpdatedAt,
		}
	}
	// The page's documents as they encode themselves, without what was
	// deleted (encodedYgoState): kept as stored, a checkpoint taken after most
	// of a page was deleted held all it had ever held, which every replay
	// from it read again (2026-11-18).
	collaboration, collaborationState, _, err := documents.read(app, ygoCollaborationStream, pageID)
	if err != nil {
		return err
	}
	collaborationState = encodedYgoState(collaboration, collaborationState)
	for index := range blocks {
		blocks[index].PlainText = collaboration.GetText(ygoBlockTextRootPrefix + blocks[index].ID).ToString()
	}
	blocksJSON, err := json.Marshal(blocks)
	if err != nil {
		return err
	}
	structural, structuralState, structuralFound, err := documents.checkedState(app, ygoStructuralStream, pageID)
	if err != nil {
		return err
	}
	if !structuralFound {
		if err := seedStructuralGraphDocument(app, pageID, structural); err != nil {
			return err
		}
	}
	structuralState = encodedYgoState(structural, structuralState)
	databases, err := pageHistoryDatabasesNow(app, pageID)
	if err != nil {
		return err
	}
	result, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_history_checkpoints}}
			([[workspace_id]],[[page_id]],[[version_cursor]],[[page_json]],[[blocks_json]],[[structural_state]],[[collaboration_state]],[[databases_json]],[[created_at]])
		VALUES ({:workspaceId},{:pageId},{:versionCursor},{:pageJSON},{:blocksJSON},{:structuralState},{:collaborationState},{:databases},{:createdAt})
		ON CONFLICT ([[page_id]],[[version_cursor]]) DO NOTHING
	`).Bind(dbx.Params{
		"workspaceId": page.GetString("workspace"), "pageId": pageID, "versionCursor": versionCursor,
		"pageJSON": packPageHistoryText(string(pageJSON)), "blocksJSON": packPageHistoryText(string(blocksJSON)),
		"structuralState":    packPageHistory(structuralState),
		"collaborationState": packPageHistory(collaborationState), "databases": packPageHistoryText(databases),
		"createdAt": time.Now().UTC().UnixMilli(),
	}).Execute()
	return documents.took(result, err, pageID, versionCursor, string(blocksJSON), collaboration)
}

func pageHistoryPageStateFromRecord(page *core.Record) pageHistoryPageState {
	return pageHistoryPageState{
		Title: page.GetString("title"), TitleSpans: page.GetString("title_spans"), Content: page.GetString("content"),
		Icon: page.GetString("icon"), IconType: page.GetString("icon_type"),
		Cover: page.GetString("cover"), CoverPosition: page.GetInt("cover_position"),
		Font: page.GetString("font"), SmallText: page.GetBool("small_text"),
		FullWidth:              page.GetBool("full_width"),
		BacklinksDisplay:       page.GetString("backlinks_display"),
		PageCommentsDisplay:    page.GetString("page_comments_display"),
		TableOfContentsDisplay: pageTableOfContentsDisplay(page), InlineCommentsDisplay: pageInlineCommentsDisplay(page),
	}
}

// pageHistoryPageStateFor is a page's state for its history, with a
// database page's property values.
func pageHistoryPageStateFor(app core.App, page *core.Record) (pageHistoryPageState, error) {
	state := pageHistoryPageStateFromRecord(page)
	if !isDatabaseRowPageRecord(page) {
		return state, nil
	}
	stored := []struct {
		PropertyID string `db:"property_id"`
		Type       string `db:"type"`
		Value      string `db:"value"`
		Spans      string `db:"spans"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[property_id]], [[type]], [[value]], [[spans]] FROM {{leafen_database_row_values}} WHERE [[row_id]]={:rowId}
	`).Bind(dbx.Params{"rowId": page.Id}).All(&stored); err != nil {
		return state, err
	}
	state.Values = make(map[string]pageHistoryRowValue, len(stored))
	for _, cell := range stored {
		value := pageHistoryRowValue{Type: cell.Type, Value: json.RawMessage(cell.Value)}
		if cell.Spans != "" {
			value.Spans = json.RawMessage(cell.Spans)
		}
		state.Values[cell.PropertyID] = value
	}
	return state, nil
}

// recordPageHistoryGraphMutation records a change to a page's blocks, with
// the page's databases and their rows it changed.
func recordPageHistoryGraphMutation(
	app core.App, pageID, actorID string, update []byte, deltas []pageHistoryGraphDelta,
	databases pageHistoryDatabaseChanges, rows []pageHistoryRowChange,
	activityContext ...workspaceActivityGraphContext,
) error {
	payload, err := json.Marshal(pageHistoryEventEnvelope{
		Format: 1, Update: append([]byte(nil), update...), GraphDeltas: deltas, Databases: databases, Rows: rows,
	})
	if err != nil {
		return err
	}
	var activity workspaceActivityGraphContext
	if len(activityContext) > 0 {
		activity = activityContext[0]
	}
	// The page as the event read it (2026-10-17: read again right after).
	source := pageHistoryEventGraph
	if activity.CommentMarks {
		source = pageHistoryVersionCommentMarks
	}
	version, page, err := recordPageHistoryEventKind(app, pageID, actorID, source, pageHistoryEventGraph, payload,
		activity.LockOnly || activity.CommentMarks)
	if err != nil {
		return err
	}
	if activity.LockOnly {
		// Replayed as the graph change it is, listed as a lock, not an edit.
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_page_history_versions}} SET [[source]]={:source} WHERE [[id]]={:id}
		`).Bind(dbx.Params{"source": pageHistoryVersionLock, "id": version.ID}).Execute(); err != nil {
			return err
		}
	}
	if !activity.Maintenance && !activity.LockOnly && !activity.CommentMarks {
		if err := recordWorkspaceGraphActivities(app, page, version, update, deltas, activity, databases, rows); err != nil {
			return err
		}
	}
	return applyPageHistoryGraphDeltas(
		app, page.GetString("workspace"), pageID, version.VersionCursor, deltas,
	)
}

// pageHistoryStarted reports whether a page's history has begun.
func pageHistoryStarted(app core.App, pageID string) (bool, error) {
	var baseline int
	err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_page_history_checkpoints}}
			WHERE [[page_id]]={:pageId} AND [[version_cursor]]=0)
	`).Bind(dbx.Params{"pageId": pageID}).Row(&baseline)
	return baseline == 1, err
}

// recordPageHistoryRewrite records a change that rewrote a page's documents
// apart from its recorded changes, as blocks moved or copied into or out of
// it, as a version whose whole state is kept, as a restore's is. A page
// without history starts it at its next change.
func recordPageHistoryRewrite(app core.App, pageID, actorID string, maintenance ...bool) error {
	if started, err := pageHistoryStarted(app, pageID); err != nil || !started {
		return err
	}
	version, err := recordPageHistoryEvent(app, pageID, actorID, pageHistoryEventRewrite, []byte(`{}`), len(maintenance) > 0 && maintenance[0])
	if err != nil {
		return err
	}
	return capturePageHistoryCheckpointAndProject(app, pageID, version.VersionCursor, nil)
}

func recordPageHistoryPageMutation(app core.App, pageID, actorID string) error {
	return recordPageHistoryPageState(app, pageID, actorID, pageHistoryEventPage)
}

func recordPageHistoryMetadataMutation(app core.App, pageID, actorID string) error {
	return recordPageHistoryPageState(app, pageID, actorID, pageHistoryEventMetadata)
}

// recordPageHistoryCommentMarksState records a database page whose value's
// words a discussion marked: its state, as no edit (pageHistoryVersionCommentMarks).
func recordPageHistoryCommentMarksState(app core.App, pageID, actorID string) error {
	return recordPageHistoryPageState(app, pageID, actorID, pageHistoryVersionCommentMarks)
}

func recordPageHistoryPageState(app core.App, pageID, actorID, source string) error {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	state, err := pageHistoryPageStateFor(app, page)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if source == pageHistoryVersionCommentMarks {
		_, _, err = recordPageHistoryEventKind(app, pageID, actorID, source, pageHistoryEventMetadata, payload, true)
		return err
	}
	_, err = recordPageHistoryEvent(app, pageID, actorID, source, payload)
	return err
}

func recordPageHistoryEvent(
	app core.App, pageID, actorID, source string, payload []byte,
	maintenance ...bool,
) (storedPageHistoryVersion, error) {
	version, _, err := recordPageHistoryEventOf(app, pageID, actorID, source, payload, maintenance...)
	return version, err
}

// pageHistoryEditVersion is the condition that a version, its row named by
// alias ("" for the table's own), is an edit of the page: one that only
// marked text with a discussion is not (pageHistoryVersionCommentMarks).
func pageHistoryEditVersion(alias string) string {
	column := "[[source]]"
	if alias != "" {
		column = "[[" + alias + "]].[[source]]"
	}
	return column + " != '" + pageHistoryVersionCommentMarks + "'"
}

// recordPageHistoryEventOf records the event and gives the page record it
// read: nothing it does writes the page. A version listed as comment marks
// keeps its change as the text change it is.
func recordPageHistoryEventOf(
	app core.App, pageID, actorID, source string, payload []byte,
	maintenance ...bool,
) (storedPageHistoryVersion, *core.Record, error) {
	kind := source
	if source == pageHistoryVersionCommentMarks {
		kind = pageHistoryEventCollaboration
	}
	return recordPageHistoryEventKind(app, pageID, actorID, source, kind, payload, len(maintenance) > 0 && maintenance[0])
}

// recordPageHistoryEventKind records a version listed as source whose event,
// replayed as kind, is the payload.
func recordPageHistoryEventKind(
	app core.App, pageID, actorID, source, kind string, payload []byte, maintenance bool,
) (storedPageHistoryVersion, *core.Record, error) {
	// Whether the history began, and its last version, asked together
	// (2026-10-17: two statements for each version a write records).
	// and the checkpoint before it (2026-10-21: a statement more for each).
	var begun struct {
		Started        bool `db:"started"`
		Frontier       int  `db:"frontier"`
		LastCheckpoint int  `db:"last_checkpoint"`
	}
	if err := app.DB().NewQuery(`
		WITH [[versions]]([[frontier]]) AS (SELECT COALESCE(MAX([[version_cursor]]),0) FROM {{leafen_page_history_versions}}
				WHERE [[page_id]]={:pageId})
		SELECT EXISTS(SELECT 1 FROM {{leafen_page_history_checkpoints}}
				WHERE [[page_id]]={:pageId} AND [[version_cursor]]=0) AS [[started]],
			[[versions]].[[frontier]],
			COALESCE((SELECT MAX([[version_cursor]]) FROM {{leafen_page_history_checkpoints}}
				WHERE [[page_id]]={:pageId} AND [[version_cursor]]<=[[versions]].[[frontier]]), -1) AS [[last_checkpoint]]
		FROM [[versions]]
	`).Bind(dbx.Params{"pageId": pageID}).One(&begun); err != nil {
		return storedPageHistoryVersion{}, nil, err
	}
	if !begun.Started {
		return storedPageHistoryVersion{}, nil, errors.New(errorCodePageHistoryFailed)
	}
	frontier := begun.Frontier
	versionCursor := frontier + 1
	createdAt := pageHistoryMoment(app)
	version := storedPageHistoryVersion{
		ID: pageHistoryVersionID(pageID, versionCursor), PageID: pageID,
		VersionCursor: versionCursor, ActorID: actorID, Source: source, CreatedAt: createdAt,
	}
	connection := pageHistoryConnectionOf(app)
	version.ConnectionName, version.ConnectionID = connection.Name, connection.ID
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return storedPageHistoryVersion{}, nil, err
	}
	workspaceID := page.GetString("workspace")
	if err := insertPageHistoryVersion(app, connection, dbx.Params{
		"id": version.ID, "workspace_id": workspaceID,
		"page_id": pageID, "version_cursor": versionCursor,
		"actor_id": actorID, "source": source, "created_at": createdAt,
		"connection_name": version.ConnectionName, "connection_id": version.ConnectionID,
	}); err != nil {
		return storedPageHistoryVersion{}, nil, err
	}
	if _, err := app.DB().Insert("leafen_page_history_events", dbx.Params{
		"workspace_id": workspaceID, "page_id": pageID,
		"version_cursor": versionCursor, "kind": kind,
		"payload": packPageHistory(payload), "created_at": createdAt,
	}).Execute(); err != nil {
		return storedPageHistoryVersion{}, nil, err
	}
	due, err := pageHistoryCheckpointDueSince(app, pageID, versionCursor, begun.LastCheckpoint)
	if err != nil {
		return storedPageHistoryVersion{}, nil, err
	}
	if due {
		// The history's text catches up with the page at every checkpoint.
		if err := capturePageHistoryCheckpointAndSettle(app, workspaceID, pageID, versionCursor, nil); err != nil {
			return storedPageHistoryVersion{}, nil, err
		}
	}
	if !maintenance {
		if err := recordWorkspacePageHistoryActivity(app, page, version, payload); err != nil {
			return storedPageHistoryVersion{}, nil, err
		}
	}
	recordPageHistoryWebhookEvent(app, page, version, payload)
	return version, page, nil
}

// pageHistoryExtendWindow is how far back, in versions, a person's typing
// looks for their own version of the minute to go on in.
const pageHistoryExtendWindow = 32

// extendPageHistoryCollaborationVersion puts a person's typing into their own
// version of the clock minute rather than a version of its own, when it can:
// the history shows each minute by its last version (pageHistoryGroups.ts),
// and a version for every keystroke the realtime editor sent filled the
// history with versions, checkpoints and copies (2026-10-15: 100 people
// typing, 30,000 versions in four minutes, and changes older than a minute
// out of reverting's reach). Typing's updates of the page's text document
// merge and replay in any order, so the version takes the new one with its
// own. It goes on only in a version made the same minute through the same
// connection, with nothing but typing after it and no checkpoint or bookmark
// at or after it (a checkpoint's replay would miss what it takes; a bookmark
// keeps its version exact), and never with a block change's own text.
func extendPageHistoryCollaborationVersion(
	app core.App, pageID, actorID string, update []byte, deltas []pageHistoryTextDelta,
) (storedPageHistoryVersion, bool, error) {
	now := time.Now().UTC().UnixMilli()
	connection := pageHistoryConnectionOf(app)
	version := storedPageHistoryVersion{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[page_id]],[[version_cursor]],[[actor_id]],[[source]],[[created_at]],[[connection_name]],[[connection_id]]
		FROM (
			SELECT * FROM {{leafen_page_history_versions}} INDEXED BY [[idx_leafen_page_history_versions_page_frontier]]
			WHERE [[page_id]]={:pageId} ORDER BY [[version_cursor]] DESC LIMIT {:window}
		)
		WHERE [[actor_id]]={:actorId}
		ORDER BY [[version_cursor]] DESC LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "actorId": actorID, "window": pageHistoryExtendWindow}).One(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return storedPageHistoryVersion{}, false, nil
	}
	if err != nil {
		return storedPageHistoryVersion{}, false, err
	}
	if version.Source != pageHistoryEventCollaboration || version.CreatedAt/60000 != now/60000 || pageHistoryBlockChangeText(app) ||
		version.ConnectionName != connection.Name || version.ConnectionID != connection.ID || !pageHistorySameAppRun(app, version.ID, connection) {
		return storedPageHistoryVersion{}, false, nil
	}
	var held int
	if err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_page_history_versions}}
			WHERE [[page_id]]={:pageId} AND [[version_cursor]]>{:cursor} AND [[source]]!={:collaboration})
		OR EXISTS(SELECT 1 FROM {{leafen_page_history_checkpoints}}
			WHERE [[page_id]]={:pageId} AND [[version_cursor]]>={:cursor})
		OR EXISTS(SELECT 1 FROM {{leafen_page_history_bookmarks}} [[bookmark]]
			JOIN {{leafen_page_history_versions}} [[marked]] ON [[marked]].[[id]]=[[bookmark]].[[version_id]]
			WHERE [[bookmark]].[[page_id]]={:pageId} AND [[marked]].[[version_cursor]]>={:cursor})
	`).Bind(dbx.Params{
		"pageId": pageID, "cursor": version.VersionCursor, "collaboration": pageHistoryEventCollaboration,
	}).Row(&held); err != nil || held != 0 {
		return storedPageHistoryVersion{}, false, err
	}
	var stored []byte
	if err := app.DB().NewQuery(`
		SELECT [[payload]] FROM {{leafen_page_history_events}} WHERE [[page_id]]={:pageId} AND [[version_cursor]]={:cursor}
	`).Bind(dbx.Params{"pageId": pageID, "cursor": version.VersionCursor}).Row(&stored); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storedPageHistoryVersion{}, false, nil
		}
		return storedPageHistoryVersion{}, false, err
	}
	envelope, err := pageHistoryEventPayload(stored)
	if err != nil {
		return storedPageHistoryVersion{}, false, err
	}
	merged, err := crdt.MergeUpdatesV1(envelope.Update, update)
	if err != nil {
		return storedPageHistoryVersion{}, false, nil
	}
	envelope.Format, envelope.Update = 1, merged
	for _, delta := range deltas {
		if !slices.ContainsFunc(envelope.TextDeltas, func(known pageHistoryTextDelta) bool { return known.BlockID == delta.BlockID }) {
			envelope.TextDeltas = append(envelope.TextDeltas, pageHistoryTextDelta{BlockID: delta.BlockID})
		}
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return storedPageHistoryVersion{}, false, err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_history_events}} SET [[payload]]={:payload},[[created_at]]={:now}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]={:cursor}
	`).Bind(dbx.Params{"payload": packPageHistory(payload), "now": now, "pageId": pageID, "cursor": version.VersionCursor}).Execute(); err != nil {
		return storedPageHistoryVersion{}, false, err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_history_versions}} SET [[created_at]]={:now}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]={:cursor}
	`).Bind(dbx.Params{"now": now, "pageId": pageID, "cursor": version.VersionCursor}).Execute(); err != nil {
		return storedPageHistoryVersion{}, false, err
	}
	version.CreatedAt = now
	// A snapshot already made of it or a later version shows less than it
	// now holds: made again when asked.
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_page_history_snapshot_jobs}} WHERE [[page_id]]={:pageId} AND [[version_cursor]]>={:cursor}
	`).Bind(dbx.Params{"pageId": pageID, "cursor": version.VersionCursor}).Execute(); err != nil {
		return storedPageHistoryVersion{}, false, err
	}
	// What else a new version does: the activity (one a minute for a person's
	// edits of a page) and the webhooks hear of the change.
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return storedPageHistoryVersion{}, false, err
	}
	single, err := json.Marshal(pageHistoryEventEnvelope{Format: 1, Update: update, TextDeltas: deltas})
	if err != nil {
		return storedPageHistoryVersion{}, false, err
	}
	if err := recordWorkspacePageHistoryActivity(app, page, version, single); err != nil {
		return storedPageHistoryVersion{}, false, err
	}
	recordPageHistoryWebhookEvent(app, page, version, single)
	return version, true, nil
}
