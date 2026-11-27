package app

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a page's Markdown reads of the pages its mentions name — whether a
// page has the ID, its title and data source, and its trash — it read for
// each mention alone (notion_markdown_read_ahead.go). The writer tells them
// ahead the pages its blocks and titles mention (learnAhead), and in a read
// scope they are read together: the pages in one read, their trash in one
// more (primePageTrashEffectsRemembered); each mention then reads as before.

// notionMarkdownMentionTargets is what a mention reads of what it names:
// whether it is in the trash itself (in), the page a page mention names
// (page, missing), and how to tell it ahead the pages it will be asked of
// (learn); its zero value names none in the trash.
type notionMarkdownMentionTargets struct {
	in    func(mention, targetID string) bool
	learn func(pageIDs []string, trash bool)
	// pages are the pages told ahead, by ID: nil for one no page has.
	pages map[string]*notionMarkdownMentionedPage
}

// notionMarkdownMentionedPage is a mentioned page's title and data source.
type notionMarkdownMentionedPage struct {
	ID           string `db:"id"`
	Title        string `db:"title"`
	DataSourceID string `db:"data_source_id"`
}

// readerMentionTargets is what a reader's Markdown reads of its mentions'
// targets; a failure to read the trash is kept in failed. What it couldn't
// read ahead is read alone when asked, as before.
func readerMentionTargets(app core.App, failed *error) notionMarkdownMentionTargets {
	pages := map[string]*notionMarkdownMentionedPage{}
	_, scoped := app.(accessMemoHolder)
	return notionMarkdownMentionTargets{
		in: func(mention, targetID string) bool {
			trashed, err := notionMentionTargetInTrash(app, mention, targetID)
			if err != nil && *failed == nil {
				*failed = err
			}
			return trashed
		},
		learn: func(pageIDs []string, trash bool) {
			if trash {
				_ = primePageTrashEffectsRemembered(app, pageIDs)
			}
			if scoped {
				learnMentionedPages(app, pages, pageIDs)
			}
		},
		pages: pages,
	}
}

// learnMentionedPages reads the pages pageIDs name together into pages; a
// part it couldn't read is left to be read alone.
func learnMentionedPages(app core.App, pages map[string]*notionMarkdownMentionedPage, pageIDs []string) {
	wanted, listed := make([]string, 0, len(pageIDs)), map[string]bool{}
	for _, pageID := range pageIDs {
		if _, known := pages[pageID]; !known && !listed[pageID] {
			listed[pageID] = true
			wanted = append(wanted, pageID)
		}
	}
	for start := 0; start < len(wanted); start += 500 {
		part := wanted[start:min(start+500, len(wanted))]
		params, placeholders := dbx.Params{}, make([]string, 0, len(part))
		for index, pageID := range part {
			name := "page" + strconv.Itoa(index)
			params[name] = pageID
			placeholders = append(placeholders, "{:"+name+"}")
		}
		rows := []notionMarkdownMentionedPage{}
		if err := app.DB().NewQuery(`SELECT [[id]], [[title]], [[data_source_id]] FROM {{leafen_pages}}
			WHERE [[id]] IN (` + strings.Join(placeholders, ",") + `)`).Bind(params).All(&rows); err != nil {
			return
		}
		for _, pageID := range part {
			pages[pageID] = nil
		}
		for index := range rows {
			pages[rows[index].ID] = &rows[index]
		}
	}
}

// page is the page a page mention names, and whether a page has its ID.
func (targets notionMarkdownMentionTargets) page(app core.App, pageID string) (notionMarkdownMentionedPage, bool) {
	if stored, known := targets.pages[pageID]; known {
		if stored == nil {
			return notionMarkdownMentionedPage{}, false
		}
		return *stored, true
	}
	stored := notionMarkdownMentionedPage{}
	err := app.DB().NewQuery(`SELECT [[id]], [[title]], [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).One(&stored)
	return stored, err == nil
}

// missing is whether a page mention names a Notion ID written that no page
// answered to (notionUnresolvedPageMention).
func (targets notionMarkdownMentionTargets) missing(app core.App, pageID string) bool {
	stored, known := targets.pages[pageID]
	if !known {
		return notionUnresolvedPageMention(app, pageID)
	}
	normalized, ok := normalizeNotionUUIDRequest(pageID)
	return ok && strings.EqualFold(normalized, pageID) && stored == nil
}

// learnAhead tells the mentions' targets the pages the blocks written and
// the references' titles mention — in their text, a caption and a table's
// cells — and whether "v2" asks of their trash.
func (mentions notionMarkdownMentions) learnAhead(
	blocks []workspaceExportMarkdownBlock, references map[string]notionMarkdownReference,
) {
	if mentions.Targets.learn == nil {
		return
	}
	pageIDs := []string{}
	named := func(spans []graphTextSpan) {
		for _, span := range spans {
			if span.Mention == "page" && span.PageID != "" {
				pageIDs = append(pageIDs, span.PageID)
			}
		}
	}
	for _, block := range blocks {
		named(block.Spans)
		for _, spans := range notionMarkdownBlockHeldSpans(block) {
			named(spans)
		}
	}
	for _, reference := range references {
		named(reference.Spans)
	}
	if len(pageIDs) > 0 {
		mentions.Targets.learn(pageIDs, mentions.Version2)
	}
}

// notionMarkdownBlockHeldSpans are the texts a block holds beside its own,
// whose mentions its Markdown writes: a table row's cells, a caption.
func notionMarkdownBlockHeldSpans(block workspaceExportMarkdownBlock) [][]graphTextSpan {
	if block.Type == graphBlockTypeTableRow {
		encoded, err := json.Marshal(block.Content)
		row := graphTableRowBlockContent{}
		if err != nil || json.Unmarshal(encoded, &row) != nil {
			return nil
		}
		return row.Cells
	}
	raw, present := block.Content["caption"]
	if !present {
		return nil
	}
	encoded, err := json.Marshal(raw)
	caption := []graphTextSpan{}
	if err != nil || json.Unmarshal(encoded, &caption) != nil {
		return nil
	}
	return [][]graphTextSpan{caption}
}
