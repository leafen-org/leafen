package app

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
)

// Markdown written to a page changes only what it changes, as Notion's
// Markdown updates do: the blocks the new Markdown still holds stay as they
// are, with their IDs and formatting; a text block whose line changed keeps
// its ID and takes the new line; blocks the Markdown adds are made where they
// stand and blocks it leaves out go. When the page's current Markdown can't
// be matched to its blocks, the whole body is written anew.

// parseNotionMarkdown reads Notion's enhanced Markdown into blocks; keepLineEnds
// reads it as the public API's "v1" does (notionPageMarkdownProjection), and
// sitePages the addresses of its pages standing bare in the text as their
// mentions (notionMarkdownSitePages).
func parseNotionMarkdown(markdown string, keepLineEnds bool, sitePages *notionMarkdownSitePages) ([]workspaceImportStagedItem, error) {
	// Notion's tags around a block's blocks (notion_page_markdown_containers.go),
	// and a toggle as Notion also reads one (notion_page_markdown_toggles.go).
	markdown, err := notionMarkdownToggleBlocks(notionMarkdownContainerForms(markdown))
	if err != nil {
		return nil, err
	}
	return parseMarkdownItems(markdown, markdownParseOptions{KeepLineEnds: keepLineEnds, ReferenceImages: true, SitePages: sitePages, Reference: func(line string) (string, json.RawMessage, bool) {
		if kind, content, reference := notionMarkdownReferenceLine(line, keepLineEnds); reference {
			return kind, content, true
		}
		if kind, content, unknown := notionMarkdownUnknownLine(line); unknown {
			return kind, content, true
		}
		if kind, content, block := notionMarkdownLinkBlockLine(line, keepLineEnds, sitePages); block {
			return kind, content, true
		}
		return notionMarkdownFileLine(line, keepLineEnds, sitePages)
	}})
}

// notionMarkdownPlan is the change that makes a page's body the blocks of
// new Markdown: its operations and texts, the blocks that go first in their
// parents, and the sub-pages whose tags give them new titles.
type notionMarkdownPlan struct {
	Operations  []graphOperation
	TextUpdates []graphTextUpdate
	// TextPieces are the texts it leaves as they are, in the pieces its
	// reading kept apart (notionMarkdownKeptPieces).
	TextPieces    []graphTextUpdate
	StartBlockIDs map[string]struct{}
	Renames       []notionMarkdownRename
	// FullPageDatabases, when there are any, are the titles of the full-page
	// databases to make before the body is planned again; the plan holds
	// nothing else.
	FullPageDatabases []string
	// PageFiles are the stored files on the page now, which its new
	// Markdown may place again (notionMarkdownFiles).
	PageFiles map[string]bool
}

// notionMarkdownSynchronousBytes is the most Markdown a page's body takes in
// one synchronous update.
const notionMarkdownSynchronousBytes = 4 << 20

// The two refusals a page's Markdown gets before any other, in Notion's API
// words: more than one write takes (4 MB, or more block changes than one
// mutation holds), and Markdown that doesn't parse.
var (
	errNotionMarkdownTooLarge = errors.New("Markdown content is too large for a synchronous update.")
	errNotionMarkdownUnparsed = errors.New("Markdown content could not be parsed.")
)

// notionMarkdownReplacementPlan plans a page's body made of Markdown, from its
// current body when there is one: each sub-page or database a tag names
// stays, moved to where the tag stands, and the sub-pages and inline
// databases tags make come where they stand.
func notionMarkdownReplacementPlan(
	markdown string,
	current *notionPageMarkdownProjection,
	allowDeleting bool,
) (notionMarkdownPlan, error) {
	plan := notionMarkdownPlan{StartBlockIDs: map[string]struct{}{}}
	if len([]byte(markdown)) > notionMarkdownSynchronousBytes {
		return plan, errNotionMarkdownTooLarge
	}
	keepLineEnds := current != nil && current.KeepLineEnds
	var sitePages *notionMarkdownSitePages
	if current != nil {
		sitePages = current.SitePages
	}
	items, err := parseNotionMarkdown(markdown, keepLineEnds, sitePages)
	if errors.Is(err, errNotionMarkdownToggleOpen) {
		return plan, err
	}
	if err != nil {
		return plan, errNotionMarkdownUnparsed
	}
	references, unknown := []notionMarkdownReference{}, map[string]string{}
	if current != nil {
		references, unknown = current.References, current.UnknownBlocks
	}
	tags, err := planNotionMarkdownTags(items, references, unknown, allowDeleting, sitePages)
	if elsewhere := (notionMarkdownElsewhereError{}); errors.As(err, &elsewhere) && current != nil {
		elsewhere.version1 = current.Version1
		return plan, elsewhere
	}
	if err != nil {
		return plan, err
	}
	for _, item := range items {
		// An unknown block's blocks are written under its line.
		if parent := item.ParentID; parent != "" {
			kept, keptParent := tags.Kept[parent]
			_, newPage := tags.NewPages[parent]
			_, newDatabase := tags.NewDatabases[parent]
			if keptParent && kept.Tag != notionMarkdownUnknownTag || newPage || newDatabase {
				return plan, errors.New("A <page> or <database> tag can't hold other blocks.")
			}
		}
	}
	if len(tags.FullPageDatabases) > 0 {
		if current == nil {
			return plan, errors.New("A new page's Markdown can't make a full-page database; add it to the page afterwards.")
		}
		plan.FullPageDatabases = tags.FullPageDatabases
		return plan, nil
	}

	// The current blocks, as their Markdown reads back, one for each block.
	// A paragraph of spaces reads back as no block, so the new Markdown
	// leaves it out and it goes, as in Notion.
	old, oldIDs, blankIDs := []workspaceImportStagedItem{}, []string{}, []string{}
	wholeBody := current != nil && len(current.RootBlockIDs) > 0
	if current != nil && current.Markdown != "" {
		// As its blocks write it, a last line's ending space and all
		// (notionPageMarkdownProjection.Lines).
		lines := current.Lines
		if lines == "" {
			lines = current.Markdown
		}
		parsed, err := parseNotionMarkdown(lines, keepLineEnds, sitePages)
		plan.PageFiles = notionMarkdownPageFiles(parsed)
		read := make([]string, 0, len(current.RenderedBlockIDs))
		blank := []string{}
		for _, blockID := range current.RenderedBlockIDs {
			if current.BlankBlockIDs[blockID] {
				blank = append(blank, blockID)
			} else {
				read = append(read, blockID)
			}
		}
		if err == nil && len(parsed) == len(read) {
			old, oldIDs, blankIDs, wholeBody = parsed, read, blank, false
		}
	}
	matched, edited, moved := matchNotionMarkdownBlocks(old, oldIDs, items, tags, current)
	notionMarkdownRereadEquations(items, oldIDs, matched, edited, current)
	notionMarkdownRereadRows(items, oldIDs, matched, edited, current)
	notionMarkdownCalloutTexts(old, oldIDs, items, matched, edited, moved)
	notionMarkdownHeadingHolds(items, matched, edited)
	// Lines given a span naming no discussion of the page edit their blocks
	// (notion_page_markdown_touched_blocks.go).
	touched := notionMarkdownTouchedLines(markdown, keepLineEnds, sitePages, len(items))

	ids := make(map[string]string, len(items))
	used := map[string]bool{}
	claimed := notionMarkdownClaimedBlocks(oldIDs, matched, edited)
	for index, item := range items {
		switch {
		case matched[index] >= 0:
			ids[item.ID] = oldIDs[matched[index]]
		case edited[index] >= 0:
			ids[item.ID] = oldIDs[edited[index]]
		default:
			if reference, ok := tags.Kept[item.ID]; ok {
				ids[item.ID] = reference.BlockID
			} else if _, ok := tags.NewPages[item.ID]; ok {
				// A sub-page is its block, and its ID is a page's.
				ids[item.ID] = newPageID()
			} else if button := current.keptButton(item, claimed); button != "" {
				// A button's line names its button (notion_page_markdown_button.go).
				ids[item.ID] = button
			} else {
				ids[item.ID] = uuid.NewString()
			}
		}
		used[ids[item.ID]] = true
	}
	calloutTexts := notionMarkdownCalloutTextChanges(old, oldIDs, items, ids, edited, used)

	operations := []graphOperation{}
	textUpdates := []graphTextUpdate{}
	renames := tags.Renames
	for sourceID, title := range tags.SourceTitles {
		operations = append(operations, graphOperation{
			Kind: graphOperationKindUpsertDataSource, DataSource: notionMarkdownTitledSource(sourceID, title, tags.SourceSpans[sourceID]),
		})
	}
	for index, item := range items {
		blockID := ids[item.ID]
		var parentID, afterID *string
		parent := ""
		if item.ParentID != "" {
			parent = ids[item.ParentID]
			parentID = &parent
		}
		// A callout's own text stands before its blocks, as no block of its own.
		if _, own := notionMarkdownCalloutTextOf(blockID); own {
			continue
		}
		if item.AfterID != "" {
			if value := ids[item.AfterID]; !strings.HasSuffix(value, notionMarkdownCalloutTextLine) {
				afterID = &value
			}
		}
		place := func() {
			if afterID == nil {
				plan.StartBlockIDs[blockID] = struct{}{}
			}
		}
		calloutText, retexted := calloutTexts[blockID]
		// A text that stays keeps its words, in the pieces its line read
		// them in.
		keepPieces := func(kind string) {
			// Code of nothing that stays keeps its text as it is, rich text
			// [] or [""] (notionPageCreateEmptyCode), as Notion's does.
			if _, holdsText := graphTextBlockTypes[kind]; holdsText && (kind != graphBlockTypeCode || item.Text != "") {
				if pieces := notionMarkdownKeptPieces(item); pieces != nil {
					plan.TextPieces = append(plan.TextPieces, graphTextUpdate{BlockID: blockID, Spans: pieces})
				}
			}
		}
		if !retexted && (matched[index] >= 0 || edited[index] < 0 && tags.Kept[item.ID].BlockID != "") {
			if matched[index] >= 0 {
				keepPieces(item.Type)
			}
			// A block that stays moves only when its parent changed, its tag
			// moved or its line moved among the others.
			if matched[index] >= 0 && !moved[index] && current.BlockParents[blockID] == parent {
				if touched[index] {
					operations = append(operations, notionMarkdownTouch(blockID, parentID, item, current)...)
				}
				continue
			}
			operations = append(operations, graphOperation{
				Kind: graphOperationKindMoveBlock,
				Move: &graphBlockMove{BlockID: blockID, ParentID: parentID, AfterID: afterID},
			})
			continue
		}
		kind, content := item.Type, json.RawMessage(item.Content)
		if pageID, ok := tags.PageMentions[item.ID]; ok {
			kind, content = graphBlockTypeParagraph, json.RawMessage(`{}`)
			item.Text, item.Spans = pageID, []graphTextSpan{{Text: pageID, Mention: "page", PageID: pageID}}
		}
		if title, ok := tags.NewPages[item.ID]; ok {
			kind, content = graphBlockTypeChildPage, childPageBlockContent(blockID)
			if title != "" {
				renames = append(renames, notionMarkdownRename{PageID: blockID, Title: title, Spans: tags.Spans[item.ID]})
			}
		}
		if title, ok := tags.NewDatabases[item.ID]; ok {
			sourceID, viewID := newDatabaseEntityID(), newDatabaseEntityID()
			operations = append(operations,
				graphOperation{Kind: graphOperationKindUpsertDataSource, DataSource: notionMarkdownTitledSource(sourceID, title, tags.Spans[item.ID])},
				graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
					ID: core.GenerateDefaultRandomId(), DataSourceID: sourceID, Type: graphValueTypeTitle,
					Name: "Name", Position: "a0", Config: json.RawMessage(`{}`),
				}},
				graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
					ID: viewID, DataSourceID: sourceID, Type: graphViewTypeTable, Name: "",
					Position: "a0", Config: json.RawMessage(`{}`),
				}},
			)
			kind = graphBlockTypeInlineDatabase
			if content, err = json.Marshal(map[string]string{"dataSourceId": sourceID, "viewId": viewID}); err != nil {
				return plan, err
			}
		}
		place()
		if edited[index] < 0 {
			if content, err = closeNewToggleContent(kind, content); err != nil {
				return plan, err
			}
		}
		block := &graphBlock{
			ID: blockID, ParentID: parentID, AfterID: afterID,
			Position: item.Position, Type: kind, Content: content,
		}
		if _, file := notionMarkdownFileKey(item); file && edited[index] >= 0 {
			// Only what the line changed changes: the caption.
			block.BaseContent = old[edited[index]].Content
		}
		if notionMarkdownLinkKinds[kind] {
			var before *workspaceImportStagedItem
			var stored map[string]any
			if edited[index] >= 0 {
				before, stored = &old[edited[index]], current.LinkContents[blockID]
			}
			if block.Content, block.BaseContent, err = notionMarkdownLinkWrite(kind, content, before, stored); err != nil {
				return plan, err
			}
		}
		if _, heading := notionMarkdownToggleHeadings[kind]; edited[index] >= 0 && current.BlockTypes[blockID] == kind &&
			(heading || kind == graphBlockTypeToggle) {
			// A toggle keeps whether it is open.
			block.BaseContent = old[edited[index]].Content
		}
		if retexted && edited[index] < 0 {
			// A callout whose lines kept it changes only its text.
			block.BaseContent = content
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: block})
		if retexted {
			textUpdates = append(textUpdates, graphTextUpdate{BlockID: blockID, Spans: calloutText})
			continue
		}
		// An edited block keeps its text, and its formatting, unless its line
		// changed it.
		if edited[index] >= 0 {
			before := old[edited[index]]
			if before.Text == item.Text && sameGraphTextSpans(before.Spans, item.Spans) {
				keepPieces(kind)
				continue
			}
			// A line of the same words and marks written otherwise leaves its
			// text as it is too, in the pieces it reads in.
			if notionMarkdownSameWords(before.Text, before.Spans, item.Text, item.Spans) {
				keepPieces(kind)
				continue
			}
			if _, holdsText := graphTextBlockTypes[kind]; !holdsText {
				continue
			}
			spans := item.Spans
			if len(spans) == 0 && item.Text != "" {
				spans = []graphTextSpan{{Text: item.Text}}
			}
			textUpdates = append(textUpdates, graphTextUpdate{BlockID: blockID, Spans: spans})
			continue
		}
		if item.Text != "" || len(item.Spans) > 0 {
			spans := item.Spans
			if len(spans) == 0 {
				spans = []graphTextSpan{{Text: item.Text}}
			}
			textUpdates = append(textUpdates, graphTextUpdate{BlockID: blockID, Spans: spans})
		}
	}

	// Blocks left out go, each with the blocks inside it; blocks that stay
	// moved out of them above.
	if wholeBody {
		for _, blockID := range current.RootBlockIDs {
			if !used[blockID] {
				operations = append(operations, graphOperation{
					Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: blockID},
				})
			}
		}
	} else {
		for _, blockID := range slices.Concat(oldIDs, blankIDs) {
			if _, own := notionMarkdownCalloutTextOf(blockID); used[blockID] || own {
				continue
			}
			parent := current.BlockParents[blockID]
			if parent != "" && current.BlockTypes[parent] != "" && !used[parent] {
				continue
			}
			operations = append(operations, graphOperation{
				Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: blockID},
			})
		}
	}
	if len(operations) > maxGraphMutationOperations {
		return plan, errNotionMarkdownTooLarge
	}
	plan.Operations, plan.TextUpdates, plan.Renames = operations, textUpdates, renames
	return plan, nil
}
