package app

import (
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A line lists in the sidebar the sub-pages its words mention where they
// mention them, as Notion's sidebar does (seen 2026-11-18 in two tabs of
// 한지테스트페이지, 'W1905 order' and 'W1905 place'): those hanging on the
// line, and those whose block sits right inside it — a page restored from
// the trash keeps its mention there — whatever the block's own place; a
// callout's words, which Leafen holds as the callout's first line
// (callout_words_line.go), list so the pages whose blocks sit right inside
// the callout. A page hanging on the line that its words no longer mention
// follows those they do; a block its line doesn't mention stands where the
// block does (childPageBodyOrder).

// textPageMentions lists the pages a block's text, as it stands, mentions,
// in order and once each (a mention of one of a page's blocks is none of the
// page).
func textPageMentions(text *crdt.YText) []string {
	if text == nil {
		return nil
	}
	pages := []string{}
	for _, delta := range textDeltaAsItStands(text.ToDelta(), false) {
		if delta.Op != crdt.DeltaOpInsert {
			continue
		}
		mention, _ := delta.Attributes["mention"].(string)
		target, _ := delta.Attributes["pageId"].(string)
		block, _ := delta.Attributes["blockId"].(string)
		target = strings.TrimSpace(target)
		if mention == "page" && target != "" && block == "" && !slices.Contains(pages, target) {
			pages = append(pages, target)
		}
	}
	return pages
}

// childPageLines lists, by line, the sub-pages of a page that the sidebar
// lists at that line, in their order there: anchors are the pages hanging on
// its lines and title in their sidebar order (loadMentionAnchors), holders
// tells the block holding each sub-page's block when that is not the body
// itself, and callouts which of those blocks are callouts. A line holding a
// sub-page's block, or two pages hanging on it, is read; the rest stay as
// anchors has them.
func childPageLines(
	app core.App, pageID string, anchors map[string][]string, holders map[string]string, callouts map[string]bool,
) (map[string][]string, error) {
	held := map[string][]string{}
	for page, holder := range holders {
		held[holder] = append(held[holder], page)
	}
	// A callout's words stand for the callout: the pages with blocks right
	// inside it are its words line's to list.
	var document *crdt.Doc
	load := func() (*crdt.Doc, error) {
		if document != nil {
			return document, nil
		}
		loaded, _, err := loadYgoDoc(app, pageID)
		document = loaded
		return loaded, err
	}
	for holder := range callouts {
		if len(held[holder]) == 0 {
			continue
		}
		callout, found, err := loadStoredNotionBlock(app, holder)
		if err != nil {
			return nil, err
		}
		if !found || callout.PageID != pageID || callout.Type != graphBlockTypeCallout {
			continue
		}
		doc, err := load()
		if err != nil {
			return nil, err
		}
		first, _, found, err := notionCalloutFirstLine(app, callout, doc)
		if err != nil {
			return nil, err
		}
		if found {
			held[first.ID] = append(held[first.ID], held[holder]...)
		}
	}
	read := []string{}
	for line, pages := range anchors {
		if !pageAnchorOnField(line) && len(pages)+len(held[line]) > 1 {
			read = append(read, line)
		}
	}
	for line, pages := range held {
		if len(anchors[line]) == 0 && !pageAnchorOnField(line) && len(pages) > 0 {
			read = append(read, line)
		}
	}
	if len(read) == 0 {
		return anchors, nil
	}
	doc, err := load()
	if err != nil {
		return nil, err
	}
	lines := make(map[string][]string, len(anchors)+len(read))
	for line, pages := range anchors {
		lines[line] = pages
	}
	for _, line := range read {
		own := map[string]bool{}
		for _, page := range slices.Concat(anchors[line], held[line]) {
			own[page] = true
		}
		listed := []string{}
		for _, page := range textPageMentions(doc.GetText(ygoBlockTextRootPrefix + line)) {
			if own[page] {
				listed = append(listed, page)
			}
		}
		for _, page := range anchors[line] {
			if !slices.Contains(listed, page) {
				listed = append(listed, page)
			}
		}
		if len(listed) > 0 {
			lines[line] = listed
		}
	}
	return lines, nil
}
