package app

import (
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A write that edits a block without changing it — a line given a span
// naming no discussion of the page (notion_page_markdown_touched_blocks.go)
// — lists no record of its own in get-version-changes, the block being the
// same before and after, but is among the edits of a block listed for its
// making, as Notion's counts it (seen 2026-11-22, worker w2345, by MCP and by
// the public API: a block an integration made with the page, then given an
// unknown discussion's span by the agent, was added with both as
// contributors, last edited at the span; from after its making, nothing).
// The history keeps such an edit as the block's state again, its last edit
// alone moved; the versions that wrote one count for the blocks already
// counted, as a discussion does (mcpVersionDiscussionEdits).

// mcpVersionTouchWindow is how many blocks one statement asks about.
const mcpVersionTouchWindow = 200

// mcpVersionTouchEdits counts the versions between start (not included) and
// end, in milliseconds, that wrote a block counted among the edits again as
// it stood — its parent, kind and content the same, its last edit moved.
func mcpVersionTouchEdits(app core.App, pageID string, start, end int64, edits map[string]*mcpVersionEdits) error {
	ids := make([]string, 0, len(edits))
	for key := range edits {
		if key != "title" && key != "data_source" && !strings.Contains(key, ":") {
			ids = append(ids, key)
		}
	}
	for low := 0; low < len(ids); low += mcpVersionTouchWindow {
		window := ids[low:min(low+mcpVersionTouchWindow, len(ids))]
		params := dbx.Params{"pageId": pageID, "start": start, "end": end}
		bindings := make([]string, len(window))
		for index, id := range window {
			key := fmt.Sprintf("block%d", index)
			params[key], bindings[index] = id, "{:"+key+"}"
		}
		touches := []struct {
			BlockID        string `db:"block_id"`
			ActorID        string `db:"actor_id"`
			CreatedAt      int64  `db:"created_at"`
			ConnectionName string `db:"connection_name"`
			ConnectionID   string `db:"connection_id"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[span]].[[block_id]], [[version]].[[actor_id]], [[version]].[[created_at]],
				[[version]].[[connection_name]], [[version]].[[connection_id]]
			FROM {{leafen_page_history_block_spans}} [[span]]
			JOIN {{leafen_page_history_block_spans}} [[before]]
				ON [[before]].[[page_id]] = [[span]].[[page_id]] AND [[before]].[[block_id]] = [[span]].[[block_id]]
					AND [[before]].[[valid_through]] = [[span]].[[valid_from]] - 1
			JOIN {{leafen_page_history_versions}} [[version]]
				ON [[version]].[[page_id]] = [[span]].[[page_id]] AND [[version]].[[version_cursor]] = [[span]].[[valid_from]]
			WHERE [[span]].[[page_id]] = {:pageId} AND [[span]].[[block_id]] IN (` + strings.Join(bindings, ", ") + `)
				AND [[before]].[[parent_id]] = [[span]].[[parent_id]] AND [[before]].[[type]] = [[span]].[[type]]
				AND [[before]].[[content]] = [[span]].[[content]] AND [[before]].[[updated_at]] != [[span]].[[updated_at]]
				AND [[version]].[[created_at]] > {:start} AND [[version]].[[created_at]] <= {:end}
				AND [[version]].[[source]] != '` + pageHistoryVersionBaseline + `' AND ` + pageHistoryEditVersion("version") + `
		`).Bind(params).All(&touches); err != nil {
			return err
		}
		for _, touch := range touches {
			if edits[touch.BlockID] == nil {
				continue
			}
			edits[touch.BlockID].add(mcpVersionContributor(pageHistoryVersionWire{
				ActorID: touch.ActorID, ConnectionName: touch.ConnectionName, ConnectionKind: pageHistoryConnectionKind(touch.ConnectionID),
			}), touch.CreatedAt)
		}
	}
	return nil
}
