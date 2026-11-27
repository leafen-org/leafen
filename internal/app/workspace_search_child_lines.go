package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The blocks of a page's body whose lines are names: its sub-pages' titles,
// a wiki home's links to its pages, and the databases it holds. They are
// lines of its search text where their blocks stand, a page's
// (searchPageIndexed) and a database page's alike (appendSearchRowPageBody).

// searchChildLine is a sub-page block's line of its page: the sub-page's
// title, and the sub-page, which may live elsewhere; a database's name, with
// no page.
type searchChildLine struct {
	Title  string
	PageID string
}

// searchPageChildLines is a page's blocks whose names are lines of it, in
// the order read, with those lines.
func searchPageChildLines(app core.App, pageID string) ([]string, map[string]searchChildLine, error) {
	// A sub-page's title is a line of the page, as Notion's search finds it
	// (seen 2026-09-30); so is the title of a wiki's page linked in its home,
	// the link standing where the page's block stood (child_page_block_moves.go),
	// as Notion's wiki is found by its home's pages and shows them as lines
	// (seen 2026-11-11: "plistar" finds the wiki "W383 Vorquen wikitwo" with
	// the line "W383 wtwo child Plistar").
	children := []struct {
		ID      string `db:"id"`
		Title   string `db:"title"`
		ChildID string `db:"child_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[block]].[[id]], [[child]].[[title]], [[child]].[[id]] AS [[child_id]]
		FROM {{leafen_blocks}} [[block]] INDEXED BY [[idx_leafen_blocks_search_projection]]
		JOIN {{leafen_pages}} [[child]] ON [[child]].[[id]] = json_extract([[block]].[[content]], '$.childPageId')
		WHERE [[block]].[[page_id]] = {:pageId} AND [[block]].[[type]] IN ({:type}, {:link})
			AND ([[block]].[[type]] = {:type} OR ` + wikiPageLinkSQL("[[block]]") + `) AND [[child]].[[in_trash]] = FALSE
	`).Bind(dbx.Params{"pageId": pageID, "type": graphBlockTypeChildPage, "link": graphBlockTypeLinkToPage}).All(&children); err != nil {
		return nil, nil, err
	}
	// A sub-page's title spelled by a name with mentions is found without
	// their words, as its own title is, and its line says nothing for a
	// custom emoji the workspace never had, as Notion's search line (seen
	// 2026-11-11: "w284 kidprobe  end"; page_name_mentions.go).
	childIDs := make([]string, 0, len(children))
	for _, child := range children {
		childIDs = append(childIDs, child.ChildID)
	}
	if spelled, err := pagesNamedWith(app, childIDs, searchNameSpansSQL); err == nil && len(spelled) > 0 {
		words := map[string]bool{}
		for _, id := range spelled {
			words[id] = true
		}
		for index, child := range children {
			if words[child.ChildID] {
				children[index].Title = searchNameWords(app, child.ChildID, child.Title)
			}
		}
	}
	named := make([]string, 0, len(children))
	lines := make(map[string]searchChildLine, len(children))
	for _, child := range children {
		if strings.TrimSpace(child.Title) != "" {
			lines[child.ID] = searchChildLine{Title: child.Title, PageID: child.ChildID}
			named = append(named, child.ID)
		}
	}
	// So is the name of a database the page holds (workspace_search_databases.go).
	databases, err := searchInlineDatabaseLines(app, pageID, nil)
	if err != nil {
		return nil, nil, err
	}
	for id, title := range databases {
		lines[id] = searchChildLine{Title: title}
		named = append(named, id)
	}
	return named, lines, nil
}
