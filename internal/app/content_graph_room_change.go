package app

import (
	"encoding/base64"
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A change of a page's lines alone — made, moved or removed in its editor,
// with the text written with it — goes to the page's open room as it is
// committed, ahead of the word of the graph's new position
// (ygo_room_publication.go): the blocks it made or moved as a read gives
// them, the places of the siblings around them, the blocks it removed and
// its words. A page open there at the position before draws it at once, its
// blocks and its text together, without reading the page again (web
// lib/roomGraphChange.ts); one elsewhere reads it as before. Read apart, the
// change's text came through the room before its blocks came back from the
// read, and a page open elsewhere showed a merged line beside its emptied
// one for a moment (2026-11-13). Only what every reader reads alike goes:
// lines whose content names no page and holds no spans, among siblings that
// hold no database, and words that mention nothing; any other change is told
// by its position alone.

// The block types the room is told of: lines that carry their words in the
// page's text and name nothing in their content.
var graphRoomChangeBlockTypes = map[string]bool{
	graphBlockTypeParagraph: true, graphBlockTypeHeading1: true, graphBlockTypeHeading2: true,
	graphBlockTypeHeading3: true, graphBlockTypeHeading4: true, graphBlockTypeToggleHeading1: true,
	graphBlockTypeToggleHeading2: true, graphBlockTypeToggleHeading3: true, graphBlockTypeToggleHeading4: true,
	graphBlockTypeBulletedListItem: true, graphBlockTypeNumberedListItem: true, graphBlockTypeToDo: true,
	graphBlockTypeToggle: true, graphBlockTypeQuote: true, graphBlockTypeCallout: true,
	graphBlockTypeDivider: true, graphBlockTypeCode: true,
}

// What a told change may hold: its operations, the siblings whose places it
// gives, and its words.
const (
	graphRoomChangeMaxOperations = 50
	graphRoomChangeMaxSiblings   = 2000
)

// graphRoomChange is a block change the room is told of, as it is made;
// payload, what it is told once the change is committed.
type graphRoomChange struct {
	shown   []string
	removed []string
	parents map[string]struct{}
	payload string
}

// graphRoomChangeFrame is what the room is told.
type graphRoomChangeFrame struct {
	Kind    string              `json:"kind"`
	From    int                 `json:"from"`
	Cursor  int                 `json:"cursor"`
	Blocks  []map[string]any    `json:"blocks"`
	Orders  []graphRoomSiblings `json:"orders"`
	Removed []string            `json:"removed"`
	Words   string              `json:"words,omitempty"`
}

// graphRoomSiblings are a parent's blocks in order, each with its place.
type graphRoomSiblings struct {
	ParentID *string     `json:"parentId"`
	Children [][2]string `json:"children"`
}

// beginGraphRoomChange reads, before a change is made, where the blocks it
// moves or removes stand; none for a change the room isn't told of. The room
// is told before any text the change stores reaches it.
func beginGraphRoomChange(app core.App, owner, pageID string, operations []graphOperation, hooks contentGraphMutationHooks) (*graphRoomChange, error) {
	if !hooks.TellRoom || len(operations) == 0 || len(operations) > graphRoomChangeMaxOperations {
		return nil, nil
	}
	change := &graphRoomChange{parents: map[string]struct{}{}}
	named := []string{}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil:
			if !graphRoomChangeBlockTypes[operation.Block.Type] {
				return nil, nil
			}
			change.shown = append(change.shown, operation.Block.ID)
			named = append(named, operation.Block.ID)
		case operation.Kind == graphOperationKindMoveBlock && operation.Move != nil:
			change.shown = append(change.shown, operation.Move.BlockID)
			named = append(named, operation.Move.BlockID)
		case operation.Kind == graphOperationKindDeleteBlock && operation.Deletion != nil:
			change.removed = append(change.removed, operation.Deletion.BlockID)
			named = append(named, operation.Deletion.BlockID)
		default:
			return nil, nil
		}
	}
	ids, err := json.Marshal(named)
	if err != nil {
		return nil, err
	}
	params := dbx.Params{"owner": owner, "pageId": pageID, "ids": string(ids)}
	var before []struct {
		ParentID string `db:"parent_id"`
		Type     string `db:"type"`
	}
	if err := app.DB().NewQuery(`
		SELECT [[parent_id]], [[type]] FROM {{leafen_blocks}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(params).All(&before); err != nil {
		return nil, err
	}
	for _, block := range before {
		if !graphRoomChangeBlockTypes[block.Type] {
			return nil, nil
		}
		change.parents[block.ParentID] = struct{}{}
	}
	// A block removed with blocks under it takes them along, which the
	// room isn't told one by one.
	if len(change.removed) > 0 {
		removed, err := json.Marshal(change.removed)
		if err != nil {
			return nil, err
		}
		var under int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_blocks}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[parent_id]] IN (SELECT [[value]] FROM json_each({:removed}))
		`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "removed": string(removed)}).Row(&under); err != nil {
			return nil, err
		}
		if under > 0 {
			return nil, nil
		}
	}
	sendGraphRoomChangeAfterCommit(app, pageID, func() string { return change.payload })
	return change, nil
}

// tell has the page's open room told of the change once it is committed at
// cursor, with the words stored with it and the text the server changed
// with it (its receipt's textUpdate); a change that turned out to be one
// every reader doesn't read alike is told by its position alone.
func (change *graphRoomChange) tell(app core.App, owner, pageID string, cursor int, words []byte, textUpdate string) error {
	if change == nil || cursor <= 1 {
		return nil
	}
	frame := graphRoomChangeFrame{
		Kind: "blocks", From: cursor - 1, Cursor: cursor,
		Blocks: []map[string]any{}, Orders: []graphRoomSiblings{}, Removed: append([]string{}, change.removed...),
	}
	if textUpdate != "" {
		server, err := base64.StdEncoding.DecodeString(textUpdate)
		if err != nil {
			return nil
		}
		if len(words) > 0 {
			if words, err = crdt.MergeUpdatesV1(words, server); err != nil {
				return nil
			}
		} else {
			words = server
		}
	}
	if len(words) > 0 {
		if len(words) > pageWordsMaxBytes || len(collaborationMentionTargets(words)) > 0 {
			return nil
		}
		frame.Words = base64.StdEncoding.EncodeToString(words)
	}
	if len(change.shown) > 0 {
		ids, err := json.Marshal(change.shown)
		if err != nil {
			return err
		}
		var payloads []string
		if err := app.DB().NewQuery(`
			SELECT json_object(
				'id', [[block]].[[id]], 'parentId', NULLIF([[block]].[[parent_id]], ''),
				'afterId', (
					SELECT [[before]].[[id]] FROM {{leafen_blocks}} [[before]] INDEXED BY [[idx_leafen_blocks_page_order]]
					WHERE [[before]].[[owner]] = [[block]].[[owner]] AND [[before]].[[page_id]] = [[block]].[[page_id]]
						AND [[before]].[[parent_id]] = [[block]].[[parent_id]]
						AND ([[before]].[[position]], [[before]].[[id]]) < ([[block]].[[position]], [[block]].[[id]])
					ORDER BY [[before]].[[position]] DESC, [[before]].[[id]] DESC LIMIT 1
				),
				'position', [[block]].[[position]], 'type', [[block]].[[type]],
				'content', CASE
					WHEN [[block]].[[type]] = 'paragraph' THEN json_remove(json([[block]].[[content]]), '$.text')
					ELSE json([[block]].[[content]])
				END
			) FROM {{leafen_blocks}} [[block]]
			WHERE [[block]].[[owner]] = {:owner} AND [[block]].[[page_id]] = {:pageId}
				AND [[block]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
		`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "ids": string(ids)}).Column(&payloads); err != nil {
			return err
		}
		for _, payload := range payloads {
			block := map[string]any{}
			if err := json.Unmarshal([]byte(payload), &block); err != nil {
				return err
			}
			if !graphRoomChangeBlockAlike(block) {
				return nil
			}
			parentID, _ := block["parentId"].(string)
			change.parents[parentID] = struct{}{}
			frame.Blocks = append(frame.Blocks, block)
		}
	}
	parents := make([]string, 0, len(change.parents))
	for parentID := range change.parents {
		parents = append(parents, parentID)
	}
	slices.Sort(parents)
	siblings := 0
	for _, parentID := range parents {
		var children []struct {
			ID       string `db:"id"`
			Position string `db:"position"`
			Type     string `db:"type"`
		}
		if err := app.DB().NewQuery(`
			SELECT [[id]], [[position]], [[type]] FROM {{leafen_blocks}} INDEXED BY [[idx_leafen_blocks_page_order]]
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[parent_id]] = {:parentId}
			ORDER BY [[position]], [[id]]
		`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "parentId": parentID}).All(&children); err != nil {
			return err
		}
		siblings += len(children)
		if siblings > graphRoomChangeMaxSiblings {
			return nil
		}
		order := graphRoomSiblings{Children: make([][2]string, 0, len(children))}
		if parentID != "" {
			order.ParentID = &parentID
		}
		for _, child := range children {
			if child.Type == graphBlockTypeInlineDatabase || child.Type == graphBlockTypeChildDatabase {
				return nil
			}
			order.Children = append(order.Children, [2]string{child.ID, child.Position})
		}
		frame.Orders = append(frame.Orders, order)
	}
	payload, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	change.payload = string(payload)
	return nil
}

// graphRoomChangeBlockAlike is whether a block reads alike to everyone: a
// line whose content names no page and holds no spans.
func graphRoomChangeBlockAlike(block map[string]any) bool {
	kind, _ := block["type"].(string)
	if !graphRoomChangeBlockTypes[kind] {
		return false
	}
	content, _ := block["content"].(map[string]any)
	for _, key := range append(append([]string{}, graphReaderSpanFields...), "cells", "linkedTitleSpans", "pageTitleSpans") {
		if _, held := content[key]; held {
			return false
		}
	}
	return true
}
