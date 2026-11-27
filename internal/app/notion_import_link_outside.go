package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page link whose page or database the import didn't bring (2026-11-16: a
// link to a page outside the import left nothing where it stood, and a column
// holding only such links stood empty) stays as Notion's Markdown export
// writes a link to a page the export leaves out — the page's name linked to
// it in Notion (seen 2026-11-16 on an export of a synthetic page:
// "[name](https://app.notion.com/p/〈id〉?pvs=21)") — a paragraph of that name
// and link, as a Notion export brought in reads that line
// (workspace_import_markdown_page_lines.go). The report counts it apart from
// what was left out (notionImportIssueLinkOutside). Its target's name is read
// from Notion while the link is staged, for a target the import hasn't staged
// yet; one the connection can't read is named by its address. A synced copy
// whose original didn't come is left out, as before.

// notionImportNotionPageAddress is where a page or database is in Notion, by
// its ID without dashes.
const notionImportNotionPageAddress = "https://app.notion.com/p/"

// A mention or text link to a page or database the import didn't bring
// keeps its words, linked to that page in Notion, as Notion's Markdown export
// writes the same mention or link — "[name](https://app.notion.com/p/〈id〉)"
// (2026-11-16: the import dropped the link and kept the words alone). The
// report counts them apart (notionImportIssueTextReferenceOutside).

// notionImportOutsideAddress is where a pending page or database reference
// is in Notion, at the block its pending anchor names, if any.
func notionImportOutsideAddress(reference, anchor string) string {
	id := reference
	for _, prefix := range []string{notionImportPendingPagePrefix, notionImportPendingDatabasePrefix} {
		id = strings.TrimPrefix(id, prefix)
	}
	address := notionImportNotionPageAddress + strings.ReplaceAll(strings.TrimSpace(id), "-", "")
	if block := strings.TrimSpace(strings.TrimPrefix(anchor, notionImportPendingBlockPrefix)); block != "" && block != anchor {
		address += "#" + strings.ReplaceAll(block, "-", "")
	}
	return address
}

// notionImportLinkTarget is the Notion page or database a staged page link
// names, while the import hasn't pointed it at what it made.
func notionImportLinkTarget(content map[string]any) string {
	for field, prefix := range map[string]string{
		"childPageId": notionImportPendingPagePrefix, "databaseContainerId": notionImportPendingDatabasePrefix,
	} {
		if value, _ := content[field].(string); strings.HasPrefix(value, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(value, prefix))
		}
	}
	return ""
}

// nameNotionImportOutsideLinks gives each staged page link whose page or
// database the import hasn't staged that target's name in Notion, as the
// link's staged title: what its page's publication keeps if the import
// doesn't bring the target after all.
func nameNotionImportOutsideLinks(
	ctx context.Context, app core.App, client notionProviderClient, jobID string, sources []stagedNotionSource,
) error {
	staged, named := map[string]bool{}, map[string]string{}
	for _, source := range sources {
		staged[notionScopeID(source.SourceID)] = true
	}
	for index := range sources {
		source := &sources[index]
		if source.Kind != "block" || !strings.Contains(source.Text, graphBlockTypeLinkToPage) {
			continue
		}
		plan, err := decodeNotionParagraphSourceText(source.Text)
		if err != nil || plan.BlockType != graphBlockTypeLinkToPage {
			continue
		}
		target := notionImportLinkTarget(plan.Content)
		if target == "" || staged[notionScopeID(target)] {
			continue
		}
		if name, read := named[notionScopeID(target)]; read {
			source.Title = name
			continue
		}
		var found int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_notion_import_sources}} WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId}
		`).Bind(dbx.Params{"jobId": jobID, "sourceId": target}).Row(&found); err != nil {
			return err
		}
		if found > 0 {
			continue
		}
		name := ""
		if _, database := plan.Content["databaseContainerId"]; database {
			name, _ = client.scopeDatabaseTitle(ctx, target)
		} else if page, err := client.page(ctx, target); err == nil {
			name = notionObjectTitle(page)
		} else if !notionProviderRefused(err) {
			return err
		}
		named[notionScopeID(target)], source.Title = name, name
	}
	return nil
}

// notionImportKeptLinks are the texts of the page links a page's publication
// keeps as their targets' names linked to Notion, by block.
type notionImportKeptLinks map[string]string

// leftOut is what a page's publication does with a block whose target the
// import didn't bring, and the reason the report gives: a page link becomes
// the paragraph that keeps it; anything else goes.
func (kept notionImportKeptLinks) leftOut(
	app core.App, jobID, sourceID string, block storedNotionBlock, content map[string]any, reason string,
) (graphOperation, string, error) {
	target := notionImportLinkTarget(content)
	if block.Type != graphBlockTypeLinkToPage || target == "" {
		return graphOperation{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: block.ID}}, reason, nil
	}
	name := ""
	err := app.DB().NewQuery(`
		SELECT [[title]] FROM {{leafen_notion_import_sources}} WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId}
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": sourceID}).Row(&name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return graphOperation{}, "", err
	}
	address := notionImportNotionPageAddress + strings.ReplaceAll(target, "-", "")
	// A target without a name, or one the connection couldn't read, is
	// named by its address.
	if strings.TrimSpace(name) == "" {
		name = address
	}
	text, err := encodeNotionParagraphSourcePlan(notionParagraphSourcePlan{
		Text: name, Spans: []notionParagraphSpan{{Text: name, Attributes: map[string]any{"link": address}}},
	})
	if err != nil {
		return graphOperation{}, "", err
	}
	kept[block.ID] = text
	return graphOperation{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
		ID: block.ID, ParentID: optionalNotionImportParentID(block.ParentID), Position: block.Position,
		Type: graphBlockTypeParagraph, Content: json.RawMessage(`{}`),
	}}, notionImportIssueLinkOutside, nil
}

// write puts the kept links' texts in their paragraphs, in place of what a
// run before left there, as actorID's change.
func (kept notionImportKeptLinks) write(app core.App, actorID, pageID string) error {
	if len(kept) == 0 {
		return nil
	}
	documents := map[string]*notionYgoDocument{}
	document, err := loadNotionYgoDocument(app, pageID, documents)
	if err != nil {
		return err
	}
	for blockID, text := range kept {
		if length := document.doc.GetText(ygoBlockTextRootPrefix + blockID).Len(); length > 0 {
			document.doc.Transact(func(txn *crdt.Transaction) {
				txn.GetText(ygoBlockTextRootPrefix+blockID).Delete(txn, 0, length)
			})
		}
		if err := applyNotionSourceText(app, pageID, blockID, text, documents); err != nil {
			return err
		}
	}
	return saveNotionYgoDocuments(app, actorID, documents)
}

// resolveNotionImportLinkedBlock points a page link or a synced copy at what
// the import made from the Notion page, database or original synced block it
// names. It reports false, with the reason, when the import made nothing.
func resolveNotionImportLinkedBlock(
	content map[string]any,
	resolver *notionImportReferenceResolver,
) (bool, string, error) {
	delete(content, notionImportSourceContentKey)
	for _, reference := range []struct{ field, prefix, suffix, kind, reason string }{
		{"childPageId", notionImportPendingPagePrefix, "", "page", notionImportIssueLinkUnresolved},
		{"databaseContainerId", notionImportPendingDatabasePrefix, ":block", "block", notionImportIssueLinkUnresolved},
		{"syncedBlockId", notionImportPendingBlockPrefix, "", "block", notionImportIssueSyncedUnresolved},
	} {
		value, _ := content[reference.field].(string)
		if !strings.HasPrefix(value, reference.prefix) {
			continue
		}
		nativeID, err := resolver.lookup(strings.TrimPrefix(value, reference.prefix)+reference.suffix, reference.kind)
		if err == nil && nativeID == "" && reference.kind == "page" {
			// Notion links to a database row page as a page; the import holds
			// it as a row.
			nativeID, err = resolver.lookup(strings.TrimPrefix(value, reference.prefix), "row")
		}
		if err == nil && nativeID == "" && reference.field == "syncedBlockId" {
			// One showing a Notion page's body shows its page's (synced_block_page_original.go).
			if page, err := notionImportSyncedPageOriginal(resolver, strings.TrimPrefix(value, reference.prefix)); err != nil || page != "" {
				content["syncedBlockId"], content["syncedPageId"] = page, page
				return page != "", reference.reason, err
			}
		}
		if err != nil || nativeID == "" {
			return false, reference.reason, err
		}
		content[reference.field] = nativeID
		if reference.field == "syncedBlockId" {
			// The copy names the original's page as one pasted in the editor
			// does, so the page shows the original from there.
			pageID := ""
			err := resolver.app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_blocks}} WHERE [[id]]={:id}`).
				Bind(dbx.Params{"id": nativeID}).Row(&pageID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return false, "", err
			}
			if pageID != "" {
				content["syncedPageId"] = pageID
			}
		}
		return true, "", nil
	}
	return false, notionImportIssueInvalidBlock, nil
}
