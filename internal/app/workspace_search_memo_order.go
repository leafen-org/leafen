package app

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A graph write — a block made, moved or deleted, a block's text set — names
// the blocks it touched (workspace_search_dirty_blocks.go), and a kept page
// (workspace_search_memo.go) follows it without the whole page made again
// (2026-10-20: every Enter or drag in a long page's editor, and every block an
// agent wrote, had the page's whole entry made again). The page's order is
// taken again from its structure, which reads no text. The parts the write
// didn't touch must stand in the order they did — else something unnamed
// changed and the whole page is made. The touched ones, with the blocks
// inside a moved one, which move with it, are taken out and put in again
// where they now stand, the chunks around each cut again; the parts gone, the
// blocks inside a deleted one, are taken out.

// searchMemoReorderMost is how many parts a change may take out and put in
// again; with more, the whole page is made.
const searchMemoReorderMost = 512

// reorder makes a kept page's parts follow the page's order again, making
// the named blocks, and those inside them, again where they now stand.
func (memo *searchPageMemo) reorder(app core.App, pageID string, text *crdt.Doc, named []string, read func(*crdt.YText) string) error {
	// A page with files' words isn't kept.
	files, err := searchFileTexts(app, pageID)
	if err != nil {
		return err
	}
	if len(files) > 0 {
		return errSearchMemoMiss
	}
	// What the page's search reads: the parts kept and the named blocks of a
	// text kind or sub-pages with titles. No other block changed kind — the
	// write named those it changed, and anything else has the page made whole.
	titles, texts, err := searchNamedIndexed(app, pageID, named)
	if err != nil {
		return err
	}
	orderDoc := text
	structure, structureFound, structureDone, err := borrowYgoStreamDocument(app, ygoStructuralStream, pageID)
	if err != nil {
		return err
	}
	defer structureDone()
	if structureFound {
		orderDoc = structure
	}
	order, stands, walked := memo.order(orderDoc, titles, texts)
	if !walked {
		return errSearchMemoMiss
	}

	// touched: true for a named block, read from the text document; false for
	// one inside it, which keeps its text.
	touched := make(map[string]bool, len(named))
	for _, id := range named {
		touched[id] = true
	}
	for _, id := range named {
		if !searchMemoTouchInside(orderDoc, id, touched) {
			return errSearchMemoMiss
		}
	}
	left := make([]string, 0, len(memo.parts))
	for _, part := range memo.parts[1:] {
		_, moved := touched[part.id]
		_, still := stands[part.id]
		if !moved && still {
			left = append(left, part.id)
		}
	}
	at := 0
	for _, id := range order {
		if _, moved := touched[id]; moved {
			continue
		}
		if at == len(left) || left[at] != id {
			return errSearchMemoMiss
		}
		at++
	}
	if at != len(left) {
		return errSearchMemoMiss
	}

	// Out, from the end, so where each part starts stays as counted.
	starts := make([]int, len(memo.parts))
	for index := 1; index < len(memo.parts); index++ {
		starts[index] = starts[index-1] + memo.parts[index-1].units
	}
	kept := map[string]string{}
	for index := len(memo.parts) - 1; index >= 1; index-- {
		part := memo.parts[index]
		read, moved := touched[part.id]
		if _, still := stands[part.id]; still && !moved {
			continue
		}
		if moved && !read {
			kept[part.id] = part.text
		}
		memo.removePart(index, starts[index])
	}
	// In, where they now stand.
	position, start := 1, memo.parts[0].units
	for _, id := range order {
		if _, moved := touched[id]; !moved {
			start += memo.parts[position].units
			position++
			continue
		}
		partText, unchanged := kept[id]
		if title, child := titles[id]; child {
			partText = title
		} else if !unchanged {
			if _, text := texts[id]; !text {
				// A named block no longer of a text kind isn't read.
				continue
			}
			partText = searchBlockText(read(text.GetText(ygoBlockTextRootPrefix + id)))
		}
		memo.insertPart(position, start, id, partText)
		start += memo.parts[position].units
		position++
	}
	memo.at = make(map[string]int, len(memo.parts))
	for index := 1; index < len(memo.parts); index++ {
		memo.at[memo.parts[index].id] = index
	}
	return nil
}

// How many places in a page's structure, and how deep, the walk below goes
// before it takes the structure for a broken one.
const (
	searchMemoWalkMost  = 1 << 20
	searchMemoWalkDepth = 256
)

// order is the blocks the page's search reads in its order — the kept parts
// and the named blocks of a text kind or with titles — and where each stands,
// read from its structure a level at a time as searchBlocksInLineOrder reads
// it but without the blocks it doesn't reach, which are gone: a kept part a
// deleted block held, or one inside it. False when a block stands twice, or
// the structure can't be walked, and the whole page is made.
func (memo *searchPageMemo) order(doc *crdt.Doc, titles map[string]string, texts map[string]struct{}) ([]string, map[string]struct{}, bool) {
	order := make([]string, 0, len(memo.parts)+len(texts)+len(titles))
	stands := make(map[string]struct{}, len(memo.parts)+len(texts)+len(titles))
	visits := 0
	for level, depth := []string{""}, 0; len(level) > 0; depth++ {
		if depth > searchMemoWalkDepth {
			return nil, nil, false
		}
		next := []string{}
		for _, parentID := range level {
			children, ok := searchStructureChildren(doc, parentID)
			if !ok {
				return nil, nil, false
			}
			for _, id := range children {
				if visits++; visits > searchMemoWalkMost {
					return nil, nil, false
				}
				_, kept := memo.at[id]
				_, text := texts[id]
				_, child := titles[id]
				if kept || text || child {
					if _, twice := stands[id]; twice {
						return nil, nil, false
					}
					stands[id] = struct{}{}
					order = append(order, id)
				}
				next = append(next, id)
			}
		}
		level = next
	}
	return order, stands, true
}

// searchNamedIndexed is which named blocks the page's search reads: those
// of a text kind, and the sub-page and database blocks with names, with the
// names.
func searchNamedIndexed(app core.App, pageID string, named []string) (map[string]string, map[string]struct{}, error) {
	encoded, err := json.Marshal(named)
	if err != nil {
		return nil, nil, err
	}
	rows := []struct {
		ID      string `db:"id"`
		Text    bool   `db:"text"`
		Title   string `db:"title"`
		ChildID string `db:"child_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[block]].[[id]],
			[[block]].[[type]] IN (SELECT [[value]] FROM json_each({:types})) AS [[text]],
			COALESCE([[child]].[[title]], '') AS [[title]], COALESCE([[child]].[[id]], '') AS [[child_id]]
		FROM {{leafen_blocks}} [[block]]
		LEFT JOIN {{leafen_pages}} [[child]] ON ([[block]].[[type]] = {:childType} OR ` + wikiPageLinkSQL("[[block]]") + `)
			AND [[child]].[[id]] = json_extract([[block]].[[content]], '$.childPageId') AND [[child]].[[in_trash]] = FALSE
		WHERE [[block]].[[page_id]] = {:pageId} AND [[block]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{
		"pageId": pageID, "ids": string(encoded), "types": searchTextBlockTypesJSON, "childType": graphBlockTypeChildPage,
	}).All(&rows); err != nil {
		return nil, nil, err
	}
	// A sub-page's title spelled by a name with mentions is found without
	// their words, nor a custom emoji's the workspace never had
	// (page_name_mentions.go).
	childIDs := []string{}
	for _, row := range rows {
		if row.ChildID != "" {
			childIDs = append(childIDs, row.ChildID)
		}
	}
	if named, err := pagesNamedWith(app, childIDs, searchNameSpansSQL); err == nil && len(named) > 0 {
		words := map[string]bool{}
		for _, id := range named {
			words[id] = true
		}
		for index, row := range rows {
			if words[row.ChildID] {
				rows[index].Title = searchNameWords(app, row.ChildID, row.Title)
			}
		}
	}
	titles, texts := map[string]string{}, map[string]struct{}{}
	for _, row := range rows {
		if row.Text {
			texts[row.ID] = struct{}{}
		} else if strings.TrimSpace(row.Title) != "" {
			titles[row.ID] = row.Title
		}
	}
	// A database the page holds is a line by its name (workspace_search_databases.go).
	databases, err := searchInlineDatabaseLines(app, pageID, named)
	if err != nil {
		return nil, nil, err
	}
	for id, title := range databases {
		titles[id] = title
	}
	return titles, texts, nil
}

// searchMemoTouchInside adds the blocks inside a touched one, which move with
// it; false when they are too many.
func searchMemoTouchInside(doc *crdt.Doc, id string, touched map[string]bool) bool {
	children, ok := searchStructureChildren(doc, id)
	if !ok {
		return false
	}
	for _, child := range children {
		if _, seen := touched[child]; seen {
			continue
		}
		touched[child] = false
		if len(touched) > searchMemoReorderMost || !searchMemoTouchInside(doc, child, touched) {
			return false
		}
	}
	return len(touched) <= searchMemoReorderMost
}

// removePart takes a part out, its units starting at start, cutting the
// chunks it was in again.
func (memo *searchPageMemo) removePart(index, start int) {
	part := memo.parts[index]
	memo.countText(part.text, -1)
	memo.parts = slices.Delete(memo.parts, index, index+1)
	if part.units > 0 {
		gone := slices.Clone(memo.units[start : start+part.units])
		memo.units = slices.Delete(memo.units, start, start+part.units)
		memo.recut(start, gone, 0)
	}
}

// insertPart puts a part in at index, its units starting at start, cutting
// the chunks around it again.
func (memo *searchPageMemo) insertPart(index, start int, id, text string) {
	units := searchPartUnits(text)
	memo.parts = slices.Insert(memo.parts, index, searchMemoPart{id: id, text: text, units: len(units)})
	memo.countText(text, 1)
	if len(units) > 0 {
		memo.units = slices.Insert(memo.units, start, units...)
		memo.recut(start, nil, len(units))
	}
}
