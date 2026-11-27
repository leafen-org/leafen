package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf16"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Notion import keeps a page, database, date or link mention as a reference.
// Date and link mentions are complete when staged. A page or database mention
// names its Notion object until the import has brought everything in: pages
// stay unpublished until then, and publishing a page points each of its
// mentions at the imported page or database, or leaves the mention as the
// visible text Notion gave it when the import did not bring its target. A
// database row page is not a native mention target, so a mention of one
// stays visible text. A page mention may name a block on its page, as
// Notion's API gives a mention of a block (seen 2026-11-11, Notion-Version
// 2022-06-28: a mention of the block's page, its href the page's address
// with the block's ID for a fragment, its plain text the block's words,
// "Untitled" for none); it names the Notion block until then too, and
// keeps the block the import made from it (page_mention_block_anchor.go).

const (
	notionImportPendingPagePrefix     = "notion-page:"
	notionImportPendingDatabasePrefix = "notion-database:"
)

// notionProviderMentionReference returns the rich-text attributes a Notion
// mention is staged with, or nil when it stays visible text.
func notionProviderMentionReference(mention notionProviderMention) map[string]any {
	switch mention.Type {
	case "page":
		if id := strings.TrimSpace(mention.Page.ID); id != "" {
			return map[string]any{"mention": "page", "pageId": notionImportPendingPagePrefix + id}
		}
	case "database":
		if id := strings.TrimSpace(mention.Database.ID); id != "" {
			return map[string]any{"mention": "database", "databaseId": notionImportPendingDatabasePrefix + id}
		}
	case "date":
		start, end, timeZone := strings.TrimSpace(mention.Date.Start), "", ""
		if mention.Date.End != nil {
			end = strings.TrimSpace(*mention.Date.End)
		}
		if mention.Date.TimeZone != nil {
			timeZone = strings.TrimSpace(*mention.Date.TimeZone)
		}
		if !validNotionMentionDateRange(start, end, timeZone) {
			// One Notion's Markdown kept as written that isn't a date (seen
			// 2026-11-11) comes as its API shows it, and stays a date mention
			// with those words, as Leafen's Markdown keeps one
			// (notionMarkdownDateSpan); an empty one is its text.
			start, end, timeZone = mention.Date.Start, "", ""
			if mention.Date.End != nil {
				end = *mention.Date.End
			}
			if start == "" {
				return nil
			}
		}
		reference := map[string]any{"mention": "date", "date": start}
		if end != "" {
			reference["dateEnd"] = end
		}
		if timeZone != "" {
			reference["dateTimeZone"] = timeZone
		}
		return reference
	case "template_mention":
		return notionProviderTemplateMentionReference(mention)
	case "link_mention":
		link := strings.TrimSpace(mention.LinkMention.Href)
		if !validGraphExternalFileURL(link) {
			return nil
		}
		reference := map[string]any{"mention": "external", "link": link}
		if icon := strings.TrimSpace(mention.LinkMention.IconURL); validGraphExternalFileURL(icon) {
			reference["iconUrl"] = icon
		}
		// What the mention keeps of its page's preview for its card and the
		// site's name before its title, as Notion's link mention does.
		if provider := strings.TrimSpace(mention.LinkMention.LinkProvider); provider != "" {
			reference["provider"] = provider
		}
		if description := strings.TrimSpace(mention.LinkMention.Description); description != "" {
			reference["description"] = description
		}
		if picture := strings.TrimSpace(mention.LinkMention.ThumbnailURL); validGraphExternalFileURL(picture) {
			reference["imageUrl"] = picture
		}
		return reference
	}
	return nil
}

type notionImportReferenceResolver struct {
	app      core.App
	jobID    string
	resolved map[string]string
	// lost says rich text just lost a mention or link to something the import
	// did not bring, and outside that one became a link to it in Notion, for
	// the caller to report.
	lost, outside bool
}

// pending reports whether a rich-text attribute still names a Notion object.
func notionImportPendingReference(attribute string, value any) bool {
	text, _ := value.(string)
	return attribute == "pageId" && strings.HasPrefix(text, notionImportPendingPagePrefix) ||
		attribute == "databaseId" && strings.HasPrefix(text, notionImportPendingDatabasePrefix)
}

// resolve returns the imported object a pending reference names, or "" when
// the import did not bring it.
func (resolver *notionImportReferenceResolver) resolve(attribute, value string) (string, error) {
	if nativeID, found := resolver.resolved[value]; found {
		return nativeID, nil
	}
	lookups := []struct{ sourceID, kind string }{}
	if attribute == "pageId" {
		// Notion mentions a database row page as a page; the import holds it
		// as a row.
		id := strings.TrimPrefix(value, notionImportPendingPagePrefix)
		lookups = append(lookups, struct{ sourceID, kind string }{id, "page"}, struct{ sourceID, kind string }{id, "row"})
	} else {
		id := strings.TrimPrefix(value, notionImportPendingDatabasePrefix)
		// A data source's names it by its address (notion_import_data_source_mentions.go).
		if source, named := strings.CutPrefix(id, notionMarkdownDataSourceAddress); named {
			nativeID, err := resolver.dataSource(source)
			if err == nil {
				resolver.resolved[value] = nativeID
			}
			return nativeID, err
		}
		lookups = append(lookups, struct{ sourceID, kind string }{id + ":block", "block"})
	}
	nativeID := ""
	for _, lookup := range lookups {
		var err error
		if nativeID, err = resolver.lookup(lookup.sourceID, lookup.kind); err != nil || nativeID != "" {
			return nativeID, err
		}
	}
	resolver.resolved[value] = nativeID
	return nativeID, nil
}

// resolveMention returns what a pending page or database mention names now,
// "" when the import did not bring it, and for a page mention that names a
// block the block: the one the import made from it, whose page the mention
// then names; one the import did not bring — trashed in Notion, or left
// out — keeps its Notion ID, naming no block here, as Notion's mention of a
// block gone from its page reads with the words it was written with and
// opens the page (seen 2026-11-11). Notion's API may also name a block by
// its own ID where a page's stands (seen 2026-11-11 in a table cell, which
// its screen draws as the block's mention); one the import brought is
// mentioned so too.
func (resolver *notionImportReferenceResolver) resolveMention(attribute, value, block string) (string, string, error) {
	nativeID, err := resolver.resolve(attribute, value)
	if err != nil || attribute != "pageId" {
		return nativeID, "", err
	}
	sourceBlock, anchored := strings.CutPrefix(block, notionImportPendingBlockPrefix)
	switch {
	case nativeID == "" && anchored:
		return "", "", nil
	case nativeID == "":
		sourceBlock = strings.TrimPrefix(value, notionImportPendingPagePrefix)
	case !anchored || sourceBlock == "":
		return nativeID, "", nil
	}
	blockID, err := resolver.lookup(sourceBlock, "block")
	if err != nil || blockID == "" {
		if nativeID == "" {
			sourceBlock = ""
		}
		return nativeID, sourceBlock, err
	}
	holder := ""
	err = resolver.app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_blocks}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": blockID}).Row(&holder)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	if holder != "" {
		nativeID = holder
	}
	if nativeID == "" {
		return "", "", nil
	}
	return nativeID, blockID, nil
}

// lookup returns what the import made from one Notion object, or "" when it
// made nothing.
func (resolver *notionImportReferenceResolver) lookup(sourceID, kind string) (string, error) {
	cacheKey := kind + "\x00" + sourceID
	if nativeID, found := resolver.resolved[cacheKey]; found {
		return nativeID, nil
	}
	nativeID := ""
	err := resolver.app.DB().NewQuery(`
		SELECT [[native_id]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId} AND [[kind]]={:kind}
	`).Bind(dbx.Params{"jobId": resolver.jobID, "sourceId": sourceID, "kind": kind}).Row(&nativeID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	resolver.resolved[cacheKey] = nativeID
	return nativeID, nil
}

// resolveNotionImportPageReferences points the pending mentions on one
// imported page at what the import brought in, before the page is published.
func resolveNotionImportPageReferences(
	app core.App,
	job storedJob,
	payload notionImportJobPayload,
	pageID string,
	resolver *notionImportReferenceResolver,
) error {
	blocks := []storedNotionBlock{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[parent_id]],[[position]],[[type]],[[content]] FROM {{leafen_blocks}}
		WHERE [[page_id]]={:pageId} ORDER BY [[id]]
	`).Bind(dbx.Params{"pageId": pageID}).All(&blocks); err != nil {
		return err
	}
	if err := resolveNotionImportBlockTexts(app, job.Owner, pageID, blocks, resolver); err != nil {
		return err
	}
	operations, kept := []graphOperation{}, notionImportKeptLinks{}
	for _, block := range blocks {
		if !strings.Contains(block.Content, `"`+notionImportPendingPagePrefix) &&
			!strings.Contains(block.Content, `"`+notionImportPendingDatabasePrefix) &&
			!strings.Contains(block.Content, `"`+notionImportPendingBlockPrefix) &&
			!strings.Contains(block.Content, `"`+notionImportPendingPageLinkPrefix) {
			continue
		}
		content := map[string]any{}
		if json.Unmarshal([]byte(block.Content), &content) != nil {
			return errors.New(errorCodeNotionImportCreateFailed)
		}
		if sourceID, linked := content[notionImportSourceContentKey].(string); linked {
			resolved, reason, err := resolveNotionImportLinkedBlock(content, resolver)
			if err != nil {
				return err
			}
			if !resolved {
				// What the block points at was not imported (notion_import_link_outside.go).
				operation, reason, err := kept.leftOut(app, job.ID, sourceID, block, content, reason)
				if err == nil {
					err = recordNotionImportPublicationIssue(app, job.ID, sourceID, block.Type, reason)
				}
				if err != nil {
					return err
				}
				operations = append(operations, operation)
				continue
			}
		}
		resolver.lost, resolver.outside = false, false
		if err := resolveNotionImportContentReferences(content, false, resolver); err != nil {
			return err
		}
		for reason, happened := range map[string]bool{
			notionImportIssueTextReferenceUnresolved: resolver.lost, notionImportIssueTextReferenceOutside: resolver.outside,
		} {
			if !happened {
				continue
			}
			if err := recordNotionImportBlockTextIssue(app, job.ID, block, reason); err != nil {
				return err
			}
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			return err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
			ID: block.ID, ParentID: optionalNotionImportParentID(block.ParentID),
			Position: block.Position, Type: block.Type, Content: encoded,
		}})
	}
	rowOperations, err := resolveNotionImportRowTextReferences(job.ID, pageID, resolver)
	if err != nil {
		return err
	}
	operations = append(operations, rowOperations...)
	if len(operations) == 0 {
		return nil
	}
	batches := map[string]*structuralMutationBatch{}
	batch, err := loadStructuralMutationBatch(app, pageID, batches)
	if err != nil {
		return err
	}
	if err := applyNotionGraphOperations(
		app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner, operations,
	); err != nil {
		return err
	}
	if err := saveStructuralMutationBatches(app, job.Owner, batches); err != nil {
		return err
	}
	return kept.write(app, job.Owner, pageID)
}

// resolveNotionImportContentReferences resolves the pending mentions in the
// rich text a block keeps in its content: a caption or table cells.
func resolveNotionImportContentReferences(value any, richText bool, resolver *notionImportReferenceResolver) error {
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			if err := resolveNotionImportContentReferences(child, richText, resolver); err != nil {
				return err
			}
		}
	case map[string]any:
		if richText {
			for _, attribute := range []string{"pageId", "databaseId"} {
				if !notionImportPendingReference(attribute, typed[attribute]) {
					continue
				}
				anchor, _ := typed["blockId"].(string)
				nativeID, blockID, err := resolver.resolveMention(attribute, typed[attribute].(string), anchor)
				if err != nil {
					return err
				}
				delete(typed, "blockId")
				if nativeID == "" {
					delete(typed, "mention")
					typed["link"] = notionImportOutsideAddress(typed[attribute].(string), anchor)
					delete(typed, attribute)
					resolver.outside = true
				} else {
					typed[attribute] = nativeID
				}
				if words, named := notionImportSourceMentionWords(resolver.app, attribute, nativeID); named {
					typed["text"] = words
				}
				if blockID != "" {
					typed["blockId"] = blockID
				}
			}
			if notionImportPendingPageLink(typed["link"]) {
				target, err := resolver.resolveLink(typed["link"].(string))
				if err != nil {
					return err
				}
				if target == "" {
					delete(typed, "link")
					resolver.lost = true
				} else {
					typed["link"] = target
				}
			}
		}
		for key, child := range typed {
			if err := resolveNotionImportContentReferences(
				child, richText || graphContentRichTextField(key), resolver,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveNotionImportBlockTexts resolves the pending mentions in a page's block text, as
// actorID's change (ygo_server_text_documents.go): sent to its editors, in its history.
func resolveNotionImportBlockTexts(
	app core.App,
	actorID, pageID string,
	blocks []storedNotionBlock,
	resolver *notionImportReferenceResolver,
) error {
	document, err := loadNotionYgoDocument(app, pageID, map[string]*notionYgoDocument{})
	if err != nil {
		return err
	}
	type textFormat struct {
		blockID       string
		index, length int
		attributes    crdt.Attributes
		// words, when set, take the place of the run's, a data source
		// mention's named (notionImportSourceMentionWords).
		words string
	}
	formats := []textFormat{}
	for _, block := range blocks {
		index := 0
		for _, delta := range document.doc.GetText(ygoBlockTextRootPrefix + block.ID).ToDelta() {
			insert, _ := delta.Insert.(string)
			length := len(utf16.Encode([]rune(insert)))
			for _, attribute := range []string{"pageId", "databaseId"} {
				if !notionImportPendingReference(attribute, delta.Attributes[attribute]) {
					continue
				}
				anchor, _ := delta.Attributes["blockId"].(string)
				nativeID, blockID, err := resolver.resolveMention(attribute, delta.Attributes[attribute].(string), anchor)
				if err != nil {
					return err
				}
				attributes := crdt.Attributes{attribute: nativeID, "blockId": nil}
				if blockID != "" {
					attributes["blockId"] = blockID
				}
				if words, named := notionImportSourceMentionWords(app, attribute, nativeID); named && words != insert {
					formats = append(formats, textFormat{block.ID, index, length, attributes, words})
					continue
				}
				if nativeID == "" {
					// Its words stay, linked to it in Notion (notion_import_link_outside.go).
					attributes = crdt.Attributes{"mention": nil, attribute: nil, "blockId": nil,
						"link": notionImportOutsideAddress(delta.Attributes[attribute].(string), anchor)}
					if err := recordNotionImportBlockTextIssue(app, resolver.jobID, block, notionImportIssueTextReferenceOutside); err != nil {
						return err
					}
				}
				formats = append(formats, textFormat{block.ID, index, length, attributes, ""})
			}
			if notionImportPendingPageLink(delta.Attributes["link"]) {
				resolver.outside = false
				target, err := resolver.resolveLink(delta.Attributes["link"].(string))
				if err != nil {
					return err
				}
				attributes := crdt.Attributes{"link": target}
				reason := ""
				switch {
				case target == "":
					attributes, reason = crdt.Attributes{"link": nil}, notionImportIssueTextReferenceUnresolved
				case resolver.outside:
					reason = notionImportIssueTextReferenceOutside
				}
				if reason != "" {
					if err := recordNotionImportBlockTextIssue(app, resolver.jobID, block, reason); err != nil {
						return err
					}
				}
				formats = append(formats, textFormat{block.ID, index, length, attributes, ""})
			}
			if insert == "" {
				length = 1
			}
			index += length
		}
	}
	if len(formats) == 0 {
		return nil
	}
	document.doc.Transact(func(txn *crdt.Transaction) {
		// From the end back, so that words put in place move nothing still to do.
		for index := len(formats) - 1; index >= 0; index-- {
			format := formats[index]
			text := txn.GetText(ygoBlockTextRootPrefix + format.blockID)
			if format.words != "" && format.length > 0 {
				// The words go in after the run's first character, taking its
				// formatting, before its old ones go: put in place of a run
				// deleted first, they would lose it with the run's marks.
				words := utf16TextLength(format.words)
				text.Insert(txn, format.index+1, format.words, nil)
				text.Delete(txn, format.index+1+words, format.length-1)
				text.Delete(txn, format.index, 1)
				format.length = words
			}
			text.Format(txn, format.index, format.length, format.attributes)
		}
	})
	return saveNotionYgoDocuments(app, actorID, map[string]*notionYgoDocument{pageID: document})
}
