package app

import (
	"context"
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/cluster"
	ygowebsocket "github.com/reearth/ygo/provider/websocket"
)

// Text the server writes itself — through the public API, MCP, automations,
// imports, people mapping, history restore or a block move — is stored in the
// page's or database page's collaborative text. Once it is committed, an open
// room takes the change too, so everyone with the page open sees it without
// reconnecting; a room that is not open reads it when it opens.

// publishStoredYgoUpdateAfterCommit brings a stored text change into its
// room once the change is committed.
func publishStoredYgoUpdateAfterCommit(app core.App, room string, update []byte) {
	if room == "" || len(update) == 0 {
		return
	}
	stored := append([]byte(nil), update...)
	publish := func() { applyStoredYgoUpdateToRoom(app, room, stored) }
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				publish()
			}
			return nil
		})
		return
	}
	publish()
}

// applyStoredYgoUpdateToRoom brings a committed text change into its room,
// if it is open, now.
func applyStoredYgoUpdateToRoom(app core.App, room string, update []byte) {
	authorization, ok := app.Store().Get(ygoRoomAuthorizationStoreKey).(*ygoRoomAuthorization)
	if !ok || authorization.server == nil {
		return
	}
	// The change is already stored; a room that cannot take it now reads
	// it when it next loads.
	_ = authorization.server.ApplyStored(context.Background(), room, update)
}

// storeRoomTypingAhead stores what a page's open room took of its text and
// hasn't stored yet ahead of a block change its editor sends: the room
// gathers typing for 2–10 s before it stores it, while the change is stored
// as it comes. Stored after the changes typed after it — an Enter, and the
// words that go with one (content_graph_mutation_words.go), which build on
// it — the typing was missing from the page's versions in between: a
// version of the minute before showed the lines typed in it empty, and the
// page's updates put a first line last (2026-11-13). One more write when the
// room holds typing; the editor showed the change already, and only the
// change's answer waits for it. Run before the change's transaction, which
// holds the write the room's store waits for; what the room couldn't store
// it stores later, as before.
func storeRoomTypingAhead(ctx context.Context, app core.App, pageID string) {
	authorization, ok := app.Store().Get(ygoRoomAuthorizationStoreKey).(*ygoRoomAuthorization)
	if !ok || authorization.server == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	authorization.server.FlushRoom(ctx, pageID)
}

// A page's blocks — made, moved or removed by anyone — change in its content
// graph, not its collaborative text. Once such a change is committed, an open
// room is told the graph's new position through the server's own awareness
// state, which is not stored; the pages open there read the graph again when
// they are behind it, as Notion's open pages show another session's blocks
// within a moment (seen 2026-09-30).

// announceGraphCursorAfterCommit tells the page's open room, if any, the
// graph position a committed change reached.
func announceGraphCursorAfterCommit(app core.App, room string, cursor int) {
	if room == "" || cursor <= 0 {
		return
	}
	announceRoomStateAfterCommit(app, room, "leafenGraph", map[string]any{"cursor": cursor})
	// The items of this page open alone are told by the graph readers
	// (ygo_room_graph_readers.go, "leafenRows").
}

// A change committed while its page's room is not open tells no one, and a
// page whose read came before the change but whose room opens after it would
// stay behind it till the next change: a row made from a template, its body
// read as the server copied the template's body into it and its room opened
// a moment later, showed an empty body and the template list till reloaded
// (2026-11-20, NAS condition with the tab's CPU slowed: 8 of 112 tries). So a
// room opening for its first peer is told the position its page's graph has
// reached, as a commit tells an open room; a page behind it reads what
// changed since, and one that isn't reads nothing.

// announceGraphCursorOnRoomOpen tells a room opening for its first peer the
// position its page's graph has reached, if it has changed at all.
func announceGraphCursorOnRoomOpen(app core.App, workspaceID, room string) {
	workspace, err := findRecordRemembered(app, workspacesCollection, workspaceID)
	if err != nil {
		return
	}
	var cursor int
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[cursor]]), 0) FROM {{leafen_graph_changes}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"owner": workspace.GetString("owner"), "pageId": room}).Row(&cursor); err != nil {
		return
	}
	announceGraphCursorAfterCommit(app, room, cursor)
}

// ygoGraphChangeFrame is the tag of the frame that tells a room the blocks a
// change made, with its text (content_graph_room_change.go): one only the
// server sends, never carried on from a peer (ygo's SendServerFrame).
const ygoGraphChangeFrame = ygowebsocket.ServerFrameTagMin

// sendGraphRoomChangeAfterCommit tells the page's open room, if any, of a
// block change once it is committed, with what payload then gives, ahead of
// what the change publishes after it was asked: its text and the graph's new
// position. An empty payload tells nothing.
func sendGraphRoomChangeAfterCommit(app core.App, room string, payload func() string) {
	send := func() {
		authorization, ok := app.Store().Get(ygoRoomAuthorizationStoreKey).(*ygoRoomAuthorization)
		told := payload()
		if !ok || authorization.server == nil || told == "" {
			return
		}
		// A room that isn't open has no one to tell.
		_ = authorization.server.SendServerFrame(room, ygoGraphChangeFrame, told)
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				send()
			}
			return nil
		})
		return
	}
	send()
}

// Comments — posted, edited, resolved, reacted to or removed by anyone —
// live outside the page's text too. Once such a change is committed, an open
// room is told the page's comment revision the same way, and the pages open
// there read their comments again, as Notion's open pages show another
// person's comment at once.

// announceCommentRevisionAfterCommit tells the page's open room, if any, the
// comment revision a committed change reached.
func announceCommentRevisionAfterCommit(app core.App, room string, revision int) {
	if room == "" || revision <= 0 {
		return
	}
	announceRoomStateAfterCommit(app, room, "leafenComments", map[string]any{"revision": revision})
	// A database item's comments are kept with the page holding its
	// database, while the item's own page — open alone or in a peek — is a
	// room of its own that reads them by that page's revision: the items of
	// that page open now are told too, so another person's comment on an
	// item shows at once where it is open (2026-11-06: it showed only after
	// the window was left and come back to).
	for _, row := range openRowRoomsOf(app, room) {
		announceRoomStateAfterCommit(app, row, "leafenComments", map[string]any{"revision": revision})
	}
}

// openRowRoomsOf are the rooms open now that are the pages of rows of a
// database page.
func openRowRoomsOf(app core.App, pageID string) []string {
	authorization, ok := app.Store().Get(ygoRoomAuthorizationStoreKey).(*ygoRoomAuthorization)
	if !ok || authorization.server == nil {
		return nil
	}
	rooms := authorization.server.Rooms()
	if len(rooms) == 0 {
		return nil
	}
	encoded, err := json.Marshal(rooms)
	if err != nil {
		return nil
	}
	rows := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_rows}}
		WHERE [[page_id]] = {:pageId} AND [[id]] IN (SELECT [[value]] FROM json_each({:rooms}))
	`).Bind(dbx.Params{"pageId": pageID, "rooms": string(encoded)}).Column(&rows); err != nil {
		return nil
	}
	return rows
}

// announceRoomStateAfterCommit sets one part of the server's awareness state
// in a page's open room once the current change is committed, keeping its
// other parts.
func announceRoomStateAfterCommit(app core.App, room, key string, value map[string]any) {
	announce := func() {
		authorization, ok := app.Store().Get(ygoRoomAuthorizationStoreKey).(*ygoRoomAuthorization)
		if !ok || authorization.server == nil {
			return
		}
		state, resident := authorization.server.GetAwareness(room)
		if !resident {
			return
		}
		next := map[string]any{}
		for name, part := range state.GetLocalState() {
			next[name] = part
		}
		next[key] = value
		state.SetLocalState(next)
		// The room fans the server's state out to its peers; a room that
		// closed meanwhile has no one to tell.
		_ = authorization.server.Inject(context.Background(), cluster.Inbound{
			Room: room, Kind: cluster.KindAwareness, Data: state.EncodeUpdate([]uint64{state.ClientID()}),
		})
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				announce()
			}
			return nil
		})
		return
	}
	announce()
}
