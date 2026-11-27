package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// What the <page> and <database> tags of Markdown written to a page do
// (notion_page_markdown_references.go): the sub-pages and databases they
// keep, make, mention and rename, planned against the page's current
// Markdown.

// notionMarkdownTagPlan is what the tags of Markdown written to a page do:
// the sub-pages and databases they keep, by the block of the Markdown that
// names each, the sub-pages and inline databases they make with their
// titles, and the new titles they give.
type notionMarkdownTagPlan struct {
	Kept         map[string]notionMarkdownReference
	NewPages     map[string]string
	NewDatabases map[string]string
	// PageMentions are <page> tags naming pages that aren't sub-pages here,
	// which mention them, by the ID their address ends in.
	PageMentions map[string]string
	// FullPageDatabases are the titles of the full-page databases tags
	// without an address make, in order; they are made before the body.
	FullPageDatabases []string
	Renames           []notionMarkdownRename
	SourceTitles      map[string]string
	// Spans are the formatting and mentions of the titles of the sub-pages
	// and databases tags make, by the block of the Markdown that names each,
	// and SourceSpans of the titles they give databases, by source; none for
	// plain words.
	Spans, SourceSpans map[string][]graphTextSpan
}

// planNotionMarkdownTags resolves the tags of Markdown written to a page
// against the sub-pages and databases its current Markdown names, a page
// tag naming a page of site elsewhere mentioning it, any other refused
// (notionMarkdownElsewhereError). Those left out go only when deleting
// content is allowed.
func planNotionMarkdownTags(
	items []workspaceImportStagedItem,
	current []notionMarkdownReference,
	unknown map[string]string,
	allowDeleting bool,
	site *notionMarkdownSitePages,
) (notionMarkdownTagPlan, error) {
	plan := notionMarkdownTagPlan{
		Kept: map[string]notionMarkdownReference{}, NewPages: map[string]string{},
		NewDatabases: map[string]string{}, PageMentions: map[string]string{}, SourceTitles: map[string]string{},
		Spans: map[string][]graphTextSpan{}, SourceSpans: map[string][]graphTextSpan{},
	}
	used := map[string]bool{}
	fresh, freshLines, named := []string{}, []string{}, map[string]bool{}
	for _, item := range items {
		if item.Type != notionMarkdownReferenceBlock {
			continue
		}
		tag := notionMarkdownTag{}
		if err := json.Unmarshal(item.Content, &tag); err != nil {
			return plan, err
		}
		// An <unknown/> line keeps the block it names, once
		// (notion_page_markdown_kept_lines.go).
		if tag.Tag == notionMarkdownUnknownTag {
			blockID := unknown[notionMarkdownUnknownKey(tag.URL)]
			if blockID == "" || used[blockID] {
				// Named by its address once, or as written without one.
				name := tag.URL
				if strings.TrimSpace(name) == "" {
					name = tag.Line
				}
				if !named[name] {
					fresh, named[name] = append(fresh, name), true
				}
				freshLines = append(freshLines, tag.Line)
				continue
			}
			used[blockID] = true
			plan.Kept[item.ID] = notionMarkdownReference{BlockID: blockID, Tag: notionMarkdownUnknownTag}
			continue
		}
		if tag.URL == "" && tag.DataSourceURL == "" {
			if tag.Spans != nil {
				plan.Spans[item.ID] = tag.Spans
			}
			switch {
			case tag.Tag == "page":
				plan.NewPages[item.ID] = tag.Title
			case strings.EqualFold(tag.Inline, "true"):
				plan.NewDatabases[item.ID] = tag.Title
			default:
				plan.FullPageDatabases = append(plan.FullPageDatabases, tag.Title)
			}
			continue
		}
		reference, found := matchNotionMarkdownReference(current, tag)
		if !found && tag.Tag == "page" && notionMarkdownMentionsElsewhere(tag.URL, site) {
			plan.PageMentions[item.ID] = notionMarkdownAddressID(tag.URL)
			continue
		}
		if !found {
			return plan, notionMarkdownElsewhereError{tag: tag.Tag, address: tag.URL}
		}
		if used[reference.BlockID] {
			return plan, errors.New("A sub-page or database can appear only once in the Markdown.")
		}
		used[reference.BlockID] = true
		plan.Kept[item.ID] = reference
		// An empty title keeps what it names as it is, as does a page's of
		// spaces in "v1"; a database's of spaces leaves it unnamed, as
		// Notion's (seen 2026-11-13).
		if tag.Title == "" || notionMarkdownSameTitle(tag, reference) {
			continue
		}
		switch {
		case reference.Tag == "page":
			plan.Renames = append(plan.Renames, notionMarkdownRename{
				PageID: reference.pageID(), Title: tag.Title, Spans: tag.Spans, Trimmed: tag.Trimmed,
			})
		case reference.Inline && reference.Several:
			// One of several sources takes the title as its own name, its
			// sources keeping theirs, as a rename through the API gives it
			// (updateNotionDatabaseSource); one of spaces alone leaves it
			// unnamed, named by its sources.
			title, spans := tag.Title, tag.Spans
			if strings.TrimSpace(title) == "" {
				title, spans = "", nil
			}
			plan.Renames = append(plan.Renames, notionMarkdownRename{DatabaseBlockID: reference.BlockID, Title: title, Spans: spans})
		case reference.Inline && reference.OwnSource:
			plan.SourceTitles[reference.SourceID] = tag.Title
			plan.SourceSpans[reference.SourceID] = tag.Spans
		case !reference.Inline:
			// A full-page database is its page, titled as its first source,
			// or one of several as its own name alone; one of spaces alone,
			// unnamed, its page untitled.
			title, spans := tag.Title, tag.Spans
			if strings.TrimSpace(title) == "" {
				title, spans = "", nil
			}
			rename := notionMarkdownRename{PageID: reference.BlockID, Title: title, Spans: spans, SourceID: reference.SourceID}
			if reference.Several {
				rename.SourceID = ""
			}
			plan.Renames = append(plan.Renames, rename)
		default:
			return plan, errors.New("A linked database shows its source's title. Rename the source database instead.")
		}
	}
	if len(fresh) > 0 {
		return plan, notionMarkdownNewUnknownError{addresses: fresh, lines: freshLines}
	}
	missing := []notionMarkdownReference{}
	for _, reference := range current {
		if !used[reference.BlockID] {
			missing = append(missing, reference)
		}
	}
	if len(missing) > 0 && !allowDeleting {
		var message strings.Builder
		fmt.Fprintf(&message, "This change would delete %d page(s), database(s) or meeting note(s) inside the page:\n", len(missing))
		for _, reference := range missing {
			fmt.Fprintf(&message, "- %s: %q (id: %s)\n", reference.Tag, reference.Title, reference.ID)
		}
		message.WriteString("\nTo go on, do one of these:\n1. Keep them in the content with <page url=\"...\"> or " +
			"<database url=\"...\"> tags; for one inside a synced block, leave that synced block's reference empty " +
			"instead.\n2. Set allow_deleting_content: true to delete them.")
		return plan, errors.New(message.String())
	}
	return plan, nil
}

// matchNotionMarkdownReference finds the sub-page or database a tag names:
// by its data source, or by its address, which for an inline database is its
// page's at its block.
func matchNotionMarkdownReference(
	current []notionMarkdownReference, tag notionMarkdownTag,
) (notionMarkdownReference, bool) {
	if tag.DataSourceURL != "" {
		key := notionMarkdownKey(tag.DataSourceURL)
		for _, reference := range current {
			if reference.DataSourceID != "" && (notionMarkdownKey(reference.DataSourceID) == key || notionMarkdownKey(reference.SourceID) == key) {
				return reference, true
			}
		}
	}
	if tag.URL != "" {
		if _, block, inline := strings.Cut(tag.URL, mcpDatabaseLinkFragment); inline {
			for _, reference := range current {
				if reference.Inline && notionMarkdownKey(reference.ID) == notionMarkdownKey(block) {
					return reference, true
				}
			}
			return notionMarkdownReference{}, false
		}
		key := notionMarkdownKey(tag.URL)
		for _, reference := range current {
			// A full-page database's own ID names it too, as its address
			// once did.
			if notionMarkdownKey(reference.URL) == key ||
				reference.Tag == "database" && !reference.Inline && notionMarkdownKey(reference.ID) == key {
				return reference, true
			}
		}
	}
	return notionMarkdownReference{}, false
}

// renameNotionMarkdownSubPages gives sub-pages the titles their tags gave.
func renameNotionMarkdownSubPages(app core.App, principalID string, renames []notionMarkdownRename) error {
	for _, rename := range renames {
		err := app.RunInTransaction(func(txApp core.App) error {
			page, err := txApp.FindRecordById(pagesCollection, rename.PageID)
			if err != nil {
				return err
			}
			// Its title takes the spans the tag gave, none for plain words.
			spans := rename.Spans
			if spans == nil {
				spans = []graphTextSpan{}
			}
			metadata := pageMetadataInput{
				Title: rename.Title, TitleSpans: &spans, ParentID: page.GetString("parent_id"), InTrash: page.GetBool("in_trash"),
				MutationID: "markdown-sub-page-title:" + newDiagnosticReference(), BaseCursor: page.GetInt("cursor"),
			}
			_, status, err := applyPageMetadataTransaction(
				txApp, principalID, page.Id, metadata, pageMetadataRequestHash(page.Id, metadata),
			)
			if err == nil && status != pageMetadataApplied {
				return graphConflictError(errorCodeGraphRevisionConflict, "A sub-page could not be renamed in its current state.")
			}
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
