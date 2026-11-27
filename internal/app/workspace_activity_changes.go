package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// What a person's edit of a page changed, as Notion's workspace updates
// list it under "〈page〉 페이지를 편집했습니다" (seen 2026-10-19): the lines
// the edit added, a line it changed with what it added, and the lines and
// sub-pages it deleted, each as it was at the edit. The group's own versions
// of its page say which blocks it changed, and they're listed in the order it
// first changed them (workspaceActivityChangedBlocks) — not the page's: a
// line typed later between two earlier ones comes after them, and a sub-page
// duplicated later from its block's handle comes last (seen 2026-11-17 by
// worker w1632 in two tabs, first account, test workspace: "one, A, two,
// mid, A (1)" for a page of one, mid, A, A (1), two; and by w1625). Each
// block is read before the group's first version and at its last as a
// version's snapshot reads it — its state, and its text from the checkpoint
// with the typing since replayed.

var errWorkspaceActivityMissing = errors.New(errorCodeWorkspaceActivityMissing)

const (
	workspaceActivityChangeAdded       = "added"
	workspaceActivityChangeChanged     = "changed"
	workspaceActivityChangeDeleted     = "deleted"
	workspaceActivityChangePage        = "page"
	workspaceActivityChangeDeletedPage = "deletedPage"
	// A database page's property changed: Text its name, Before and After its values.
	workspaceActivityChangeProperty = "property"
)

func readWorkspaceActivityChanges(app core.App, user *core.Record, workspaceID, activityID string) (workspaceActivityChangesResponse, error) {
	return readWorkspaceActivityChangesFor(context.Background(), app, user, workspaceID, activityID)
}

// readWorkspaceActivityChangesFor is one update's changes for the reader whose
// own request ctx carries, its untitled rows named in its language.
func readWorkspaceActivityChangesFor(ctx context.Context, app core.App, user *core.Record, workspaceID, activityID string) (workspaceActivityChangesResponse, error) {
	response := workspaceActivityChangesResponse{ActivityID: activityID, Changes: []workspaceActivityChange{}}
	err := runReadSnapshot(context.Background(), app, func(tx core.App) error {
		scope, err := authorizeCommentNotificationScope(tx, user.Id, workspaceID, true)
		if err != nil {
			return err
		}
		retention, err := loadOrganizationActivityRetention(tx, scope.Organization.OrganizationID)
		if err != nil {
			return err
		}
		row := storedWorkspaceActivity{}
		err = tx.DB().NewQuery(`SELECT [[id]],[[actor_id]],[[action]],[[subject_type]],[[subject_id]],[[metadata_json]],[[created]],[[last_at]],[[first_version]],[[last_version]]
			FROM {{leafen_workspace_activity}}
			WHERE [[id]]={:id} AND [[workspace_id]]={:workspace} AND [[organization_id]]={:organization} AND [[last_at]]>={:cutoff}`).Bind(dbx.Params{
			"id": activityID, "workspace": scope.WorkspaceID, "organization": scope.Organization.OrganizationID,
			"cutoff": activityRetentionCutoff(retention.FeedDays, time.Now()),
		}).One(&row)
		if errors.Is(err, sql.ErrNoRows) {
			return errWorkspaceActivityMissing
		}
		if err != nil {
			return err
		}
		// The group's own authority, as the list shows it.
		targets, err := loadWorkspaceActivityTargets(tx, []storedWorkspaceActivity{row})
		if err != nil {
			return err
		}
		cache := map[string]*pageAuthorization{}
		item, visible, err := visibleWorkspaceActivity(ctx, tx, user.Id, scope, row, targets, cache, nil)
		if err != nil {
			return err
		}
		if !visible {
			return errWorkspaceActivityMissing
		}
		response.PageID = item.PageID
		if !item.HasChanges {
			return nil
		}
		// Its history, as the page's own history reads it.
		if _, err := authorizePage(tx, user.Id, item.PageID, permissionWorkspaceRead); isPermissionDenied(err) {
			return nil
		} else if err != nil {
			return err
		}
		// A database page's record lists its properties, its body its lines
		// (workspaceActivityRowPart); a group from before they were two, both.
		var metadata workspaceActivityMetadata
		_ = json.Unmarshal([]byte(row.Metadata), &metadata)
		if metadata.Part != workspaceActivityPartRecord {
			// A copy's own edit lists what it holds, or what it put in the page
			// (workspace_activity_copy_changes.go).
			read := workspaceActivityChangesOf
			switch workspaceActivityCopyKind(row, metadata, targets.copyJobs) {
			case pageDuplicationJobKind:
				read = workspaceActivityCopyChanges
			case blockMoveJobKind:
				read = workspaceActivityBlockCopyChanges
			}
			response.Changes, response.More, err = read(tx, user.Id, scope, item.PageID, row, cache)
			if err != nil {
				return err
			}
			// Its new title first (workspace_activity_title_change.go).
			if page := cache[item.PageID]; page != nil && !isDatabaseRowPageRecord(page.Page) {
				if response.Changes, response.More, err = withWorkspaceActivityTitle(tx, item.PageID, row, response.Changes, response.More); err != nil {
					return err
				}
			}
		}
		// A database page's properties come first, as its page shows them above its body.
		if page := cache[item.PageID]; page != nil && isDatabaseRowPageRecord(page.Page) && metadata.Part != workspaceActivityPartBody {
			properties, err := workspaceActivityPropertyChanges(tx, user.Id, item.PageID, row)
			if err != nil {
				return err
			}
			response.Changes = append(properties, response.Changes...)
			if over := len(response.Changes) - workspaceActivityChangeItems; over > 0 {
				response.Changes, response.More = response.Changes[:workspaceActivityChangeItems], response.More+over
			}
		}
		return nil
	})
	return response, err
}

// workspaceActivityVersionChanges lists what a group's own versions of its
// page changed, each with when the edit first changed its block, but the
// blocks skip names among them (workspace_activity_copy_merge.go).
func workspaceActivityVersionChanges(app core.App, actor string, scope workspaceAuthorization, pageID string, row storedWorkspaceActivity,
	cache map[string]*pageAuthorization, skip func([]string) (map[string]bool, error)) ([]workspaceActivityTimedChange, int, error) {
	changes := []workspaceActivityTimedChange{}
	first, last := int(row.FirstVersion.Int64), int(row.LastVersion.Int64)
	if !row.FirstVersion.Valid || !row.LastVersion.Valid {
		return changes, 0, nil
	}
	start, err := pageHistoryStartCursor(app, pageID)
	if err != nil || first-1 < start {
		return changes, 0, err
	}
	blocks, times, err := workspaceActivityChangedBlocks(app, pageID, row.ActorID, first, last, row.Created)
	if err != nil {
		return nil, 0, err
	}
	skipped, err := skip(blocks)
	if err != nil {
		return nil, 0, err
	}
	reader := pageHistoryChangeReader{app: app, pageID: pageID, spans: map[string]pageHistoryChangeSpan{}}
	texts := workspaceActivityTexts{reader: reader, replayed: map[int]map[string]pageHistoryReplayedText{}}
	more := 0
	add := func(change workspaceActivityChange) {
		changes = append(changes, workspaceActivityTimedChange{change: change, at: times[change.BlockID]})
	}
	kept := blocks[:0:0]
	for _, blockID := range blocks {
		if !skipped[blockID] {
			kept = append(kept, blockID)
		}
	}
	// The blocks past those read are counted, not read.
	read := kept[:min(len(kept), workspaceActivityChangeBlocks)]
	more += len(kept) - len(read)
	states := make([]workspaceActivityBlock, 0, len(read))
	for _, blockID := range read {
		state := workspaceActivityBlock{id: blockID}
		var err error
		if state.before, state.existed, err = reader.spanAt(blockID, first-1); err != nil {
			return nil, 0, err
		}
		if state.after, state.exists, err = reader.spanAt(blockID, last); err != nil {
			return nil, 0, err
		}
		states = append(states, state)
	}
	for _, state := range states {
		blockID, before, existed, after, exists := state.id, state.before, state.existed, state.after, state.exists
		page := (exists && after.Type == graphBlockTypeChildPage) || (!exists && existed && before.Type == graphBlockTypeChildPage)
		shown := after
		if !exists {
			shown = before
		}
		// Each line says its block's kind, as Notion's draws it
		// (workspaceActivityMarked).
		boxed := func(change workspaceActivityChange) {
			add(workspaceActivityMarked(change, shown))
		}
		switch {
		case exists && !existed:
			words := func() (workspaceActivityText, error) { return texts.at(blockID, last) }
			change, made, err := workspaceActivityMadeLine(app, actor, scope, blockID, after, words, cache)
			if err != nil {
				return nil, 0, err
			}
			if made {
				add(change)
			}
		case shown.Type == graphBlockTypeEquation:
			// An equation's words are its expression; a changed one shows what
			// it was too, for its line to strike what went, as Notion's does.
			was, now := "", ""
			if existed && before.Type == graphBlockTypeEquation {
				was = strings.TrimSpace(workspaceActivityExpression(before))
			}
			if exists {
				now = strings.TrimSpace(workspaceActivityExpression(after))
			}
			switch {
			case now != "" && was == "":
				boxed(workspaceActivityChange{Kind: workspaceActivityChangeAdded, BlockID: blockID, Added: now})
			case now != "" && now != was:
				kept, added, rest := workspaceActivityAddedText(was, now)
				boxed(workspaceActivityChange{Kind: workspaceActivityChangeChanged, BlockID: blockID, Before: kept, Added: added, After: rest, Was: was})
			case !exists && was != "":
				boxed(workspaceActivityChange{Kind: workspaceActivityChangeDeleted, BlockID: blockID, Text: was})
			}
		case page && existed && exists && before.Type == graphBlockTypeChildPage:
			// A sub-page renamed is its own activity.
		case page:
			// A sub-page shows its name only to who can see it.
			child, err := workspaceActivityPage(app, actor, scope, blockID, cache)
			if err != nil {
				return nil, 0, err
			}
			if child == nil {
				continue
			}
			kind := workspaceActivityChangePage
			if !exists {
				kind = workspaceActivityChangeDeletedPage
			}
			add(workspaceActivityChange{Kind: kind, BlockID: blockID, Text: strings.TrimSpace(child.Page.GetString("title"))})
		case shown.Type == graphBlockTypeInlineDatabase:
			// One there before the edit is no line (workspaceActivityDatabaseLine).
		case exists:
			was, err := texts.at(blockID, first-1)
			if err != nil {
				return nil, 0, err
			}
			now, err := texts.at(blockID, last)
			if err != nil {
				return nil, 0, err
			}
			// Text only taken away isn't shown, as the page's updates show it;
			// an empty line written in is a line added.
			kept, added, rest := workspaceActivityAddedText(strings.TrimSpace(was.plain), strings.TrimSpace(now.plain))
			if strings.TrimSpace(was.plain) == "" && strings.TrimSpace(now.plain) != "" {
				boxed(workspaceActivityChange{Kind: workspaceActivityChangeAdded, BlockID: blockID, Added: strings.TrimSpace(now.plain), Spans: now.spans})
			} else if strings.TrimSpace(added) != "" {
				boxed(workspaceActivityChange{Kind: workspaceActivityChangeChanged, BlockID: blockID, Before: kept, Added: added, After: rest, Spans: now.spans})
			}
		case existed && before.Type == graphBlockTypeButton:
			// A deleted button shows by its name and steps (workspaceActivityMarked).
			boxed(workspaceActivityChange{Kind: workspaceActivityChangeDeleted, BlockID: blockID})
		case existed:
			text, err := texts.at(blockID, first-1)
			if err != nil {
				return nil, 0, err
			}
			if deleted := strings.TrimSpace(text.plain); deleted != "" {
				boxed(workspaceActivityChange{Kind: workspaceActivityChangeDeleted, BlockID: blockID, Text: deleted, Spans: text.spans})
			}
		}
	}
	return changes, more, nil
}

// workspaceActivityBlock is a block an edit changed, before the edit and
// after it.
type workspaceActivityBlock struct {
	id              string
	before, after   pageHistoryChangeSpan
	existed, exists bool
}

// workspaceActivityChangedBlocks is the blocks a person's versions of a page
// from first through last changed, in the order they first changed them —
// a block made in them where it was made, though its words may have been
// stored a version before it (a paste's) — the blocks one change made or
// moved at once, as a paste's lines, which the history keeps by id and by
// no order, in the page's order, as Notion lists a paste (seen 2026-11-13),
// and a block made around lines before them (workspaceActivityWrap).
// made is when the group began: a block made since is one of its own, and
// one of its own deleted again in them is none of them.
func workspaceActivityChangedBlocks(app core.App, pageID, actorID string, first, last int, made int64) ([]string, map[string]int64, error) {
	events := []storedPageHistoryEvent{}
	if err := app.DB().NewQuery(`
		SELECT [[event]].[[version_cursor]],[[event]].[[kind]],[[event]].[[payload]],[[event]].[[created_at]]
		FROM {{leafen_page_history_events}} AS [[event]]
		WHERE [[event]].[[page_id]]={:pageId} AND [[event]].[[version_cursor]] IN (
			SELECT [[version_cursor]] FROM {{leafen_page_history_versions}}
			WHERE [[page_id]]={:pageId} AND [[version_cursor]]>={:first} AND [[version_cursor]]<={:last} AND [[actor_id]]={:actor}
			ORDER BY [[version_cursor]] LIMIT {:limit})
		ORDER BY [[event]].[[version_cursor]]
	`).Bind(dbx.Params{"pageId": pageID, "first": first, "last": last, "actor": actorID, "limit": workspaceActivityChangeVersions}).All(&events); err != nil {
		return nil, nil, err
	}
	// Each block's place: where it was made, else where it was first changed,
	// and when that was.
	place, at := map[string]int{}, map[string]int64{}
	var now int64
	madeHere, gone := map[string]bool{}, map[string]bool{}
	blocks := []string{}
	order := 0
	touch := func(blockID string, making bool) {
		if blockID == "" {
			return
		}
		order++
		if _, seen := place[blockID]; !seen {
			place[blockID], at[blockID] = order, now
			blocks = append(blocks, blockID)
		}
		if making && !madeHere[blockID] {
			madeHere[blockID], place[blockID], at[blockID] = true, order, now
		}
	}
	making := func(delta pageHistoryGraphDelta) bool {
		return !delta.Deleted && delta.Created >= made-workspaceActivityPropertySlack
	}
	wraps := []workspaceActivityWrap{}
	for _, event := range events {
		if event.Kind != pageHistoryEventGraph && event.Kind != pageHistoryEventCollaboration {
			continue
		}
		now = event.CreatedAt
		payload, err := unpackPageHistory(event.Payload)
		if err != nil {
			return nil, nil, err
		}
		envelope, err := pageHistoryEventPayload(payload)
		if err != nil {
			return nil, nil, err
		}
		ids, follows := make([]string, len(envelope.GraphDeltas)), make([]string, len(envelope.GraphDeltas))
		for at, delta := range envelope.GraphDeltas {
			ids[at], follows[at] = delta.BlockID, delta.AfterID
		}
		wraps = append(wraps, workspaceActivityWrapsOf(envelope.GraphDeltas, place, making)...)
		for _, at := range workspaceActivityChainOrder(ids, follows) {
			delta := envelope.GraphDeltas[at]
			touch(delta.BlockID, making(delta))
			gone[delta.BlockID] = delta.Deleted
		}
		for _, delta := range envelope.TextDeltas {
			touch(delta.BlockID, false)
		}
	}
	// A block made and deleted in them changed nothing the edit shows, and
	// takes no part of what is read (workspaceActivityChangeBlocks): 64
	// lines typed and deleted again hid the six kept after them (2026-11-13).
	kept := blocks[:0]
	for _, blockID := range blocks {
		if !madeHere[blockID] || !gone[blockID] {
			kept = append(kept, blockID)
		}
	}
	sort.SliceStable(kept, func(left, right int) bool { return place[kept[left]] < place[kept[right]] })
	return workspaceActivityWrappedFirst(kept, at, wraps), at, nil
}

// workspaceActivityTexts reads blocks' text at versions as a snapshot does:
// the text replayed to the version for a block typed in since the checkpoint
// before it, else the text the history kept for it.
type workspaceActivityTexts struct {
	reader   pageHistoryChangeReader
	replayed map[int]map[string]pageHistoryReplayedText
}

// workspaceActivityText is a block's text at a version: its plain text, and
// its spans where the history kept its formatting — the whole text, which the
// change list cuts as the page's updates tab cuts its lines, so equations,
// mentions and marks draw as in the page (web/src/lib/richTextSummary.ts).
type workspaceActivityText struct {
	plain string
	spans []graphTextSpan
}

func workspaceActivityTextOf(plain, rich string) workspaceActivityText {
	runs, known := decodePageHistoryTextRuns(plain, rich)
	if !known || plain == "" {
		return workspaceActivityText{plain: plain}
	}
	deltas := make([]crdt.Delta, len(runs))
	for index, run := range runs {
		deltas[index] = crdt.Delta{Op: crdt.DeltaOpInsert, Insert: run.Insert, Attributes: run.Attributes}
	}
	spans := graphTextSpansFromDelta(deltas)
	spelled := strings.Builder{}
	for _, span := range spans {
		spelled.WriteString(span.Text)
	}
	if spelled.String() != plain {
		return workspaceActivityText{plain: plain}
	}
	return workspaceActivityText{plain: plain, spans: spans}
}

func (texts workspaceActivityTexts) at(blockID string, version int) (workspaceActivityText, error) {
	replayed, read := texts.replayed[version]
	if !read {
		var err error
		if replayed, err = pageHistoryReplayedTexts(texts.reader.app, texts.reader.pageID, version); err != nil {
			return workspaceActivityText{}, err
		}
		texts.replayed[version] = replayed
	}
	if text, found := replayed[blockID]; found {
		return workspaceActivityTextOf(text.plain, text.rich), nil
	}
	kept := struct {
		Plain string `db:"plain_text"`
		Rich  string `db:"rich_text"`
	}{}
	err := texts.reader.app.DB().NewQuery(`
		SELECT [[plain_text]],[[rich_text]] FROM {{leafen_page_history_text_spans}}
		WHERE [[page_id]]={:pageId} AND [[block_id]]={:blockId} AND [[valid_from]]<={:version}
			AND ([[valid_through]] IS NULL OR [[valid_through]]>={:version})
		ORDER BY [[valid_from]] DESC LIMIT 1
	`).Bind(dbx.Params{"pageId": texts.reader.pageID, "blockId": blockID, "version": version}).One(&kept)
	if errors.Is(err, sql.ErrNoRows) {
		return workspaceActivityText{}, nil
	}
	return workspaceActivityTextOf(kept.Plain, kept.Rich), err
}

// workspaceActivityAddedText is a line's new text cut where it differs from
// its old one: what it kept at its start, what was added, what it kept at
// its end, as the page's updates cut it.
func workspaceActivityAddedText(previous, next string) (string, string, string) {
	was, now := []rune(previous), []rune(next)
	start := 0
	for start < len(was) && start < len(now) && was[start] == now[start] {
		start++
	}
	end := 0
	for end < len(was)-start && end < len(now)-start && was[len(was)-1-end] == now[len(now)-1-end] {
		end++
	}
	return string(now[:start]), string(now[start : len(now)-end]), string(now[len(now)-end:])
}

func workspaceActivityClip(text string) string {
	runes := []rune(text)
	if len(runes) > workspaceActivityChangeText {
		return string(runes[:workspaceActivityChangeText])
	}
	return text
}

// workspaceActivityHasChanges says whether a group is a page's edit that
// knows its versions of the page, or blocks copied into it
// (workspace_activity_copy_merge.go), or a database page's record, whose
// properties' versions are found by its times, or a sub-page's making, its
// name (workspace_activity_sub_page.go).
func workspaceActivityHasChanges(row storedWorkspaceActivity, metadata workspaceActivityMetadata, page *core.Record) bool {
	return row.EntityType == "page" && row.Action == "edit" && row.EntityID == metadata.PageID &&
		(row.FirstVersion.Valid && row.LastVersion.Valid || len(metadata.CopyJobs) > 0 ||
			isDatabaseRowPageRecord(page) && metadata.Part != workspaceActivityPartBody || metadata.Made)
}
