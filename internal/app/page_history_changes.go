package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A version entry's changes one by one, as Notion's "버전의 모든 변경 사항
// 보기" lists them (seen 2026-10-01): a block edited, added or deleted, moved
// or turned into another type, the page's name and its other fields, and its
// databases, newest first. Edits one person made to one thing in a row are
// one change, at the latest of them.

const (
	pageHistoryChangeEdited         = "edited"
	pageHistoryChangeMoved          = "moved"
	pageHistoryChangeTypeChanged    = "typeChanged"
	pageHistoryChangeRenamed        = "renamed"
	pageHistoryChangePageEdited     = "pageEdited"
	pageHistoryChangeDatabaseEdited = "databaseEdited"
)

type pageHistoryChangeSpan struct {
	ValidFrom    int           `db:"valid_from"`
	ValidThrough sql.NullInt64 `db:"valid_through"`
	AfterID      string        `db:"after_id"`
	ParentID     string        `db:"parent_id"`
	Type         string        `db:"type"`
	Content      string        `db:"content"`
}

// holds reports whether the span is a block's state at a version.
func (span pageHistoryChangeSpan) holds(version int) bool {
	return span.ValidFrom <= version && (!span.ValidThrough.Valid || int(span.ValidThrough.Int64) >= version)
}

func listPageHistoryChanges(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	versionID := strings.TrimSpace(event.Request.PathValue("versionId"))
	sinceID := strings.TrimSpace(event.Request.URL.Query().Get("since"))
	if sinceID == "" {
		sinceID = versionID
	}
	limit, err := parsePageHistoryLimit(event.Request.URL.Query().Get("limit"), pageHistoryChangePageSize)
	if err != nil || pageID == "" || versionID == "" {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the page history query range.")
	}
	response := pageHistoryChangesResponse{}
	err = runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		if _, err := authorizePage(txApp, user.Id, pageID, permissionWorkspaceRead); err != nil {
			return err
		}
		version, err := loadPageHistoryVersion(txApp, pageID, versionID)
		if err != nil {
			return pageHistoryChangesVersionError(err)
		}
		since, err := loadPageHistoryVersion(txApp, pageID, sinceID)
		if err != nil {
			return pageHistoryChangesVersionError(err)
		}
		start, err := pageHistoryStartCursor(txApp, pageID)
		if err != nil {
			return err
		}
		if since.VersionCursor < start {
			return errPageHistoryVersionMissing
		}
		if since.VersionCursor > version.VersionCursor {
			return errPageHistoryCursorInvalid
		}
		cursor, err := decodePageHistoryCursor(
			event.Request.URL.Query().Get("cursor"), "changes", user.Id, pageID, versionID, limit,
		)
		if err != nil {
			return err
		}
		through := version.VersionCursor
		if cursor.SnapshotFrontier != 0 {
			if cursor.SinceID != sinceID || cursor.SnapshotFrontier != version.VersionCursor ||
				cursor.VersionCursor >= version.VersionCursor || cursor.VersionCursor < since.VersionCursor {
				return errPageHistoryCursorInvalid
			}
			through = cursor.VersionCursor
		}
		// Each page of changes reads the changes of at most limit versions.
		low := max(since.VersionCursor, through-limit+1, 1)
		changes, err := pageHistoryChangesBetween(txApp, pageID, start, low, through)
		if err != nil {
			return err
		}
		// A person's run of edits to one thing reads as its latest, as the
		// history panel lists it; coalesce=false keeps every change, for
		// get-version-changes' first edits and contributors.
		if event.Request.URL.Query().Get("coalesce") != "false" {
			changes = coalescePageHistoryChanges(changes)
		}
		actorIDs := make([]string, 0, len(changes))
		for _, change := range changes {
			actorIDs = append(actorIDs, change.ActorID)
		}
		profiles, err := pageHistoryActorProfiles(txApp, actorIDs)
		if err != nil {
			return err
		}
		if changes, err = redactPageHistoryChanges(txApp, user.Id, changes); err != nil {
			return err
		}
		// What names a page closed to this reader (content_graph_reader_mentions.go).
		historyChangesAsRead(txApp, user.Id, changes)
		response = pageHistoryChangesResponse{
			PageID: pageID, VersionID: versionID, SinceID: sinceID,
			Changes: changes, ActorProfiles: profiles,
		}
		if low-1 >= max(since.VersionCursor, 1) {
			next, err := encodePageHistoryCursor(pageHistoryCursor{
				Kind: "changes", PrincipalID: user.Id, PageID: pageID, VersionID: versionID, SinceID: sinceID,
				SnapshotFrontier: version.VersionCursor, VersionCursor: low - 1, Limit: limit,
			})
			if err != nil {
				return err
			}
			response.NextCursor, response.HasMore = &next, true
		}
		return nil
	})
	return writePageHistoryResult(event, response, err, "changes")
}

func pageHistoryChangesVersionError(err error) error {
	if pageHistoryVersionMissing(err) {
		return errPageHistoryVersionMissing
	}
	return err
}

// pageHistoryChangesBetween lists the changes of a page's versions from low
// through through, newest first, its history starting at start. A version
// that only marked words with a discussion lists none, though the page's
// state it leaves is the one the next version changes.
func pageHistoryChangesBetween(app core.App, pageID string, start, low, through int) ([]pageHistoryChangeWire, error) {
	if low > through {
		return []pageHistoryChangeWire{}, nil
	}
	versions := []storedPageHistoryVersion{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[page_id]],[[version_cursor]],[[actor_id]],[[source]],[[created_at]]
		FROM {{leafen_page_history_versions}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]>={:low} AND [[version_cursor]]<={:through}
		ORDER BY [[version_cursor]]
	`).Bind(dbx.Params{"pageId": pageID, "low": low, "through": through}).All(&versions); err != nil {
		return nil, err
	}
	events, err := loadPageHistoryEvents(app, pageID, low-1, through)
	if err != nil {
		return nil, err
	}
	byCursor := make(map[int]storedPageHistoryEvent, len(events))
	for _, event := range events {
		byCursor[event.VersionCursor] = event
	}
	page, err := readPageHistorySnapshotPageState(app, pageID, max(low-1, start))
	if err != nil {
		return nil, err
	}
	reader := pageHistoryChangeReader{app: app, pageID: pageID, spans: map[string]pageHistoryChangeSpan{}}
	// The databases' states before a version: read where a version first
	// changes one, then carried from version to version.
	var databases map[string]pageHistoryDatabaseState
	databasesBefore := func(version int) (map[string]pageHistoryDatabaseState, error) {
		if databases == nil {
			state, err := materializePageHistoryVersion(app, pageID, max(version-1, start))
			if err != nil {
				return nil, err
			}
			databases = state.Databases
			if databases == nil {
				databases = map[string]pageHistoryDatabaseState{}
			}
		}
		return databases, nil
	}
	changes := []pageHistoryChangeWire{}
	for _, version := range versions {
		event, found := byCursor[version.VersionCursor]
		if !found {
			continue
		}
		add := func(change pageHistoryChangeWire) {
			if version.Source == pageHistoryVersionCommentMarks {
				return
			}
			change.VersionID, change.ActorID, change.CreatedAt = version.ID, version.ActorID, version.CreatedAt
			changes = append(changes, change)
		}
		switch event.Kind {
		case pageHistoryEventGraph:
			envelope, err := pageHistoryEventPayload(event.Payload)
			if err != nil {
				return nil, err
			}
			for _, delta := range envelope.GraphDeltas {
				blockChanges, err := reader.blockChanges(delta, version.VersionCursor)
				if err != nil {
					return nil, err
				}
				for _, change := range blockChanges {
					add(change)
				}
			}
			sources := make([]string, 0, len(envelope.Databases))
			for sourceID := range envelope.Databases {
				sources = append(sources, sourceID)
			}
			sort.Strings(sources)
			if len(sources) > 0 {
				states, err := databasesBefore(version.VersionCursor)
				if err != nil {
					return nil, err
				}
				for _, sourceID := range sources {
					before, held := states[sourceID]
					after := envelope.Databases[sourceID]
					made := false
					for _, change := range pageHistoryDatabaseChangeCards(before, held, after) {
						add(change)
						made = made || change.Kind == pageHistoryChangeViewCreated
					}
					// A view made edits its database's block too, as Notion lists it.
					if made {
						block, err := pageHistoryDatabaseBlock(app, pageID, sourceID)
						if err != nil {
							return nil, err
						}
						if block.ID != "" {
							add(pageHistoryChangeWire{Kind: pageHistoryChangeEdited, BlockID: block.ID, BlockType: block.Type})
						}
					}
					if after == nil {
						delete(states, sourceID)
					} else {
						states[sourceID] = *after
					}
				}
			}
			for _, row := range envelope.Rows {
				kind := row.Change
				switch kind {
				case pageHistoryChangeRowTrashed:
					kind = pageHistoryChangeRowRemoved
				case pageHistoryChangeRowRestored:
					kind = pageHistoryChangeRowAdded
				}
				add(pageHistoryChangeWire{Kind: kind, ItemID: row.ID, RowValues: row.Values})
			}
		case pageHistoryEventCollaboration:
			envelope, err := pageHistoryEventPayload(event.Payload)
			if err != nil {
				return nil, err
			}
			seen := map[string]struct{}{}
			for _, delta := range envelope.TextDeltas {
				if _, done := seen[delta.BlockID]; done {
					continue
				}
				seen[delta.BlockID] = struct{}{}
				span, found, err := reader.spanAt(delta.BlockID, version.VersionCursor)
				if err != nil {
					return nil, err
				}
				change := pageHistoryChangeWire{Kind: pageHistoryChangeEdited, BlockID: delta.BlockID}
				if found {
					change.BlockType = span.Type
				}
				add(change)
			}
		case pageHistoryEventPage, pageHistoryEventMetadata:
			next := pageHistoryPageState{}
			if err := json.Unmarshal(event.Payload, &next); err != nil {
				return nil, err
			}
			if next.Title != page.Title {
				add(pageHistoryChangeWire{Kind: pageHistoryChangeRenamed})
			}
			before, after := page, next
			before.Title, after.Title = "", ""
			if !reflect.DeepEqual(pageHistoryComparablePage(before), pageHistoryComparablePage(after)) {
				add(pageHistoryChangeWire{Kind: pageHistoryChangePageEdited})
			}
			page = next
		case pageHistoryEventRestore, pageHistoryEventRewrite:
			// These versions keep their whole state; the page reads as it,
			// and its databases are read again where next changed.
			if page, err = readPageHistorySnapshotPageState(app, pageID, version.VersionCursor); err != nil {
				return nil, err
			}
			databases = nil
			add(pageHistoryChangeWire{Kind: pageHistoryChangePageEdited})
		}
	}
	for left, right := 0, len(changes)-1; left < right; left, right = left+1, right-1 {
		changes[left], changes[right] = changes[right], changes[left]
	}
	return changes, nil
}

// pageHistoryComparablePage is a page state as JSON values, so states that
// read the same compare equal however their values were written.
func pageHistoryComparablePage(state pageHistoryPageState) any {
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil
	}
	var value any
	if json.Unmarshal(encoded, &value) != nil {
		return nil
	}
	return value
}

// coalescePageHistoryChanges keeps, of changes listed newest first, the
// latest of each run one person made to one thing: edits to a block until
// another change to it, as a move or a type change. Type changes and
// deletions stay each.
func coalescePageHistoryChanges(changes []pageHistoryChangeWire) []pageHistoryChangeWire {
	kept := changes[:0]
	// The runs still open, by block and then by kind and person.
	open := map[string]map[string]struct{}{}
	for _, change := range changes {
		run := change.Kind + "\x00" + change.ActorID
		merges := change.Kind != pageHistoryChangeTypeChanged && !change.Deleted
		// The thing changed: a block, or a database's property or view.
		thing := change.BlockID + "\x00" + change.ItemID
		if _, found := open[thing][run]; found && merges {
			continue
		}
		// Another change to a block ends its other runs.
		if thing != "\x00" || !merges || open[thing] == nil {
			open[thing] = map[string]struct{}{}
		}
		if merges {
			open[thing][run] = struct{}{}
		}
		kept = append(kept, change)
	}
	return kept
}

// pageHistoryChangeReader reads blocks' states in a page's history, keeping
// the last read of each block.
type pageHistoryChangeReader struct {
	app    core.App
	pageID string
	spans  map[string]pageHistoryChangeSpan
}

// spanAt reads a block's state at a version, if it had one.
func (reader pageHistoryChangeReader) spanAt(blockID string, version int) (pageHistoryChangeSpan, bool, error) {
	if span, found := reader.spans[blockID]; found && span.holds(version) {
		return span, true, nil
	}
	span := pageHistoryChangeSpan{}
	err := reader.app.DB().NewQuery(`
		SELECT [[valid_from]],[[valid_through]],[[after_id]],[[parent_id]],[[type]],[[content]]
		FROM {{leafen_page_history_block_spans}}
		WHERE [[page_id]]={:pageId} AND [[block_id]]={:blockId} AND [[valid_from]]<={:version}
		ORDER BY [[valid_from]] DESC LIMIT 1
	`).Bind(dbx.Params{"pageId": reader.pageID, "blockId": blockID, "version": version}).One(&span)
	if errors.Is(err, sql.ErrNoRows) {
		return span, false, nil
	}
	if err != nil {
		return span, false, err
	}
	reader.spans[blockID] = span
	return span, span.holds(version), nil
}

// blockChanges reads what a version's change to one block was against the
// block's state before it. A block only shifted by the blocks around it
// changed nothing.
func (reader pageHistoryChangeReader) blockChanges(
	delta pageHistoryGraphDelta, version int,
) ([]pageHistoryChangeWire, error) {
	before, existed, err := reader.spanAt(delta.BlockID, version-1)
	if err != nil {
		return nil, err
	}
	change := pageHistoryChangeWire{Kind: pageHistoryChangeEdited, BlockID: delta.BlockID, BlockType: delta.Type}
	switch {
	case delta.Deleted:
		// A database taken off the page edits the page, as Notion lists it;
		// its preview leaves the database out (seen 2026-11-07).
		content := map[string]any{}
		_ = json.Unmarshal([]byte(before.Content), &content)
		_, database, err := pageHistoryDatabaseOf(reader.app, delta.BlockID, structuralBlockMetadata{Type: before.Type, Content: content})
		if err != nil {
			return nil, err
		}
		if database {
			return []pageHistoryChangeWire{{
				Kind: pageHistoryChangePageEdited, BlockID: delta.BlockID, BlockType: before.Type, Deleted: true,
			}}, nil
		}
		change.Deleted, change.BlockType = true, before.Type
		return []pageHistoryChangeWire{change}, nil
	case !existed:
		return []pageHistoryChangeWire{change}, nil
	}
	changes := []pageHistoryChangeWire{}
	if delta.Moved || before.ParentID != delta.ParentID {
		changes = append(changes, pageHistoryChangeWire{
			Kind: pageHistoryChangeMoved, BlockID: delta.BlockID, BlockType: delta.Type,
		})
	}
	if before.Type != delta.Type {
		changes = append(changes, pageHistoryChangeWire{
			Kind: pageHistoryChangeTypeChanged, BlockID: delta.BlockID, BlockType: delta.Type, FromType: before.Type,
		})
	} else if !pageHistorySameJSON(before.Content, string(delta.Content)) {
		changes = append(changes, change)
	}
	return changes, nil
}

func pageHistorySameJSON(left, right string) bool {
	if left == right {
		return true
	}
	var a, b any
	if json.Unmarshal([]byte(left), &a) != nil || json.Unmarshal([]byte(right), &b) != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}
