package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A callout holds its lines as paragraphs, its first too, as the screen's does.
// Notion's public API gives the first line as the callout's rich_text and the
// others as its children (seen 2026-11-05), so that line reads and writes so.
// Two callouts read otherwise, as Notion's do (seen 2026-11-14), and say so in
// their content's calloutTextKey.

// calloutTextKey names, in a callout's content, where its text stands when
// that is not its first line in the page's color.
const calloutTextKey = "calloutText"

const (
	// calloutTextOwn is a callout the block API made: its text is its own and
	// every line inside it a block, as Notion's API keeps one it made
	// (rich_text [] with children [x, y] reads back so; x appended to an
	// empty one, a child taken away, a color changed or its text emptied
	// leave its children as they are) and draws it with no empty first line.
	calloutTextOwn = "own"
	// calloutTextLine is a callout read from Markdown whose first line is a
	// paragraph in a color: that line is its text all the same, as Notion
	// reads "<callout>\n<p color=\"red\">\na\n</p>\n</callout>" (rich_text
	// "a", the line's color kept in its Markdown), where a callout whose
	// first line was colored on the screen or that the API made gives it
	// as a child.
	calloutTextLine = "line"
)

// withCalloutText marks a callout's content with where its text stands.
func withCalloutText(content json.RawMessage, kind string) json.RawMessage {
	fields := map[string]any{}
	if json.Unmarshal(content, &fields) != nil || fields == nil {
		return content
	}
	fields[calloutTextKey] = kind
	marked, err := json.Marshal(fields)
	if err != nil {
		return content
	}
	return marked
}

// notionCalloutFirstLine is the paragraph that stands as a callout's text, and
// how many blocks follow it; none for a callout with text of its own or whose
// first block is no plain paragraph.
func notionCalloutFirstLine(app core.App, block storedNotionBlock, document *crdt.Doc) (storedNotionBlock, int, bool, error) {
	if block.Type != graphBlockTypeCallout || textAsItStands(document.GetText(ygoBlockTextRootPrefix+block.ID)) != "" {
		return storedNotionBlock{}, 0, false, nil
	}
	callout := map[string]any{}
	_ = json.Unmarshal([]byte(block.Content), &callout)
	kind, _ := callout[calloutTextKey].(string)
	if kind == calloutTextOwn {
		return storedNotionBlock{}, 0, false, nil
	}
	children := []storedNotionBlock{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[position]],[[type]],[[content]] FROM {{leafen_blocks}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[parent_id]]={:parentId} AND ` + blockStandsSQL("") + `
		ORDER BY [[position]],[[id]] LIMIT 2
	`).Bind(dbx.Params{"owner": block.Owner, "pageId": block.PageID, "parentId": block.ID}).All(&children)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return storedNotionBlock{}, 0, false, err
	}
	if len(children) == 0 || children[0].Type != graphBlockTypeParagraph {
		return storedNotionBlock{}, 0, false, nil
	}
	content := map[string]any{}
	if json.Unmarshal([]byte(children[0].Content), &content) != nil {
		return storedNotionBlock{}, 0, false, nil
	}
	if color := notionBlockColor(content); color != "default" && color != "default_background" && kind != calloutTextLine {
		return storedNotionBlock{}, 0, false, nil
	}
	return children[0], len(children) - 1, true, nil
}
