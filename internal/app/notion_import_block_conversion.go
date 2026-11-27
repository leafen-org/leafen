package app

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion import converts a Notion block into the native block the
// Notion-compatible block API stores for the same request, so an imported
// heading, list item, callout or column is the block a person or an
// integration would have made. Rich text and captions come from the import's
// own rich-text staging instead, which keeps page and database mentions
// pending until every imported page exists. A block Leafen cannot bring over is
// staged as an issue for the import report instead of disappearing, and so is
// a setting the import had to leave out of a block it did bring over.

const (
	notionImportPendingBlockPrefix = "notion-block:"
	// notionImportSourceContentKey names, until the page is published, the
	// Notion block a page link or a synced copy came from, so one whose target
	// the import did not bring can be reported where it was.
	notionImportSourceContentKey = "notionImportSource"
)

// notionImportRequestKeys lists, for each block type converted through the
// block API normalization, the fields of Notion's block object that are also
// request fields. Rich text, captions and icons are converted separately.
var notionImportRequestKeys = map[string][]string{
	"paragraph":          {"color"},
	"heading_1":          {"color", "is_toggleable"},
	"heading_2":          {"color", "is_toggleable"},
	"heading_3":          {"color", "is_toggleable"},
	"heading_4":          {"color", "is_toggleable"},
	"bulleted_list_item": {"color"},
	"numbered_list_item": {"color", "list_start_index", "list_format"},
	"to_do":              {"color", "checked"},
	"toggle":             {"color"},
	"quote":              {"color"},
	"callout":            {"color"},
	"code":               {"language"},
	"equation":           {"expression"},
	"divider":            {},
	"breadcrumb":         {},
	"table_of_contents":  {"color"},
	"column_list":        {},
	"column":             {"width_ratio"},
	"embed":              {"url"},
	"tab":                {},
}

// A block whose optional settings Leafen cannot take is still brought over
// without them.
var notionImportOptionalRequestKeys = []string{"color", "list_start_index", "list_format", "width_ratio"}

var notionImportBlockTypeLabel = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// stageNotionConvertedBlock stages a block of a type the import converts
// through the block API normalization, a page link or a synced block. It
// returns the staged block, or nil when the block was left out, and the
// sources staged after it: a callout's first line, then the issues to report.
// A blueprint takes no partial block: one with any issue is left out.
func stageNotionConvertedBlock(
	parentSourceID, parentKind string,
	blueprint bool,
	position int,
	block notionProviderObject,
) (*stagedNotionSource, []stagedNotionSource, error) {
	if strings.TrimSpace(block.ID) == "" {
		return nil, nil, errors.New(errorCodeNotionImportProviderFailed)
	}
	fields := map[string]json.RawMessage{}
	if len(block.Payload) > 0 && string(block.Payload) != "null" {
		if err := json.Unmarshal(block.Payload, &fields); err != nil {
			return nil, nil, errors.New(errorCodeNotionImportProviderFailed)
		}
	} else if block.Type == graphBlockTypeParagraph {
		// A paragraph read into its typed fields alone keeps its rich text.
		richText, err := json.Marshal(block.Paragraph.RichText)
		if err != nil {
			return nil, nil, err
		}
		fields["rich_text"] = richText
	}
	plan, discovery, issues, err := convertNotionBlockPayload(block, fields, blueprint)
	if err != nil {
		return nil, nil, err
	}
	if plan != nil {
		plan.EditedAt = block.LastEditedTime
	}
	// A block counts once for each reason it is reported for.
	placed, reported := []stagedNotionSource{}, map[string]bool{}
	for _, issue := range issues {
		if !reported[issue.Text] {
			reported[issue.Text] = true
			placed = append(placed, notionImportIssueSource(parentSourceID, parentKind, position, block, issue.Text))
		}
	}
	issues = placed
	if blueprint && len(issues) > 0 {
		return nil, issues, nil
	}
	if plan == nil {
		return nil, issues, nil
	}
	firstLine, err := notionImportCalloutFirstLine(block, plan, blueprint)
	if err != nil {
		return nil, nil, err
	}
	text, err := encodeNotionParagraphSourcePlan(*plan)
	if err != nil {
		return nil, nil, err
	}
	kind := "block"
	if block.Type == graphBlockTypeParagraph {
		kind = graphBlockTypeParagraph
	}
	staged := &stagedNotionSource{
		SourceID: block.ID, Kind: kind, ParentSourceID: parentSourceID, ParentKind: parentKind,
		Position: position, Text: text, Blueprint: blueprint, DiscoveryStatus: discovery,
	}
	if firstLine != nil {
		// The callout's children follow its first line, in the source too.
		staged.DiscoveryPosition = 1
		issues = append([]stagedNotionSource{*firstLine}, issues...)
	}
	return staged, issues, nil
}

// notionImportCalloutFirstLineSuffix names the line an imported callout's
// own words become (notionImportCalloutFirstLine).
const notionImportCalloutFirstLineSuffix = "#callout-first-line"

// notionImportCalloutFirstLine takes a callout's own words out into its first
// line, staged right after it so it comes before the callout's children, and
// leaves the callout holding lines with no text of its own, as one made on
// Leafen's screen does. Notion's API reads a callout made on its screen as
// its first line's words with the other lines as children, the same as one an
// integration made with words and children (seen 2026-11-12), but the screen's
// keeps its lines: ⇧Tab takes none of them out, where the integration's lets
// a child out. An import brings what people made on the screen, so its
// callouts edit as those did. A callout with no words and nothing in it holds
// its one empty line; one with no words but lines in it learns whether its
// first line is empty when they are read (notionImportCalloutEmptyFirstLine).
func notionImportCalloutFirstLine(
	block notionProviderObject, plan *notionParagraphSourcePlan, blueprint bool,
) (*stagedNotionSource, error) {
	if block.Type != graphBlockTypeCallout || plan.BlockType != graphBlockTypeCallout ||
		len(plan.Spans) == 0 && block.HasChildren {
		return nil, nil
	}
	line, err := notionImportCalloutLine(block.ID, *plan, blueprint)
	if err != nil {
		return nil, err
	}
	plan.Text, plan.Spans = "", []notionParagraphSpan{}
	return line, nil
}

// notionImportCalloutLine stages a callout's words as its first line.
func notionImportCalloutLine(
	calloutID string, plan notionParagraphSourcePlan, blueprint bool,
) (*stagedNotionSource, error) {
	spans := plan.Spans
	if spans == nil {
		spans = []notionParagraphSpan{}
	}
	text, err := encodeNotionParagraphSourcePlan(notionParagraphSourcePlan{
		Text: plan.Text, Spans: spans, BlockType: graphBlockTypeParagraph, Content: map[string]any{},
		EditedAt: plan.EditedAt,
	})
	if err != nil {
		return nil, err
	}
	return &stagedNotionSource{
		SourceID: calloutID + notionImportCalloutFirstLineSuffix, Kind: graphBlockTypeParagraph,
		ParentSourceID: calloutID, ParentKind: "block", Position: 0, Text: text, Blueprint: blueprint,
		DiscoveryStatus: notionImportPageDiscoveryComplete,
	}, nil
}

// notionImportCalloutEmptyFirstLine is the empty first line of a callout with
// no words whose first child, read now, is a plain paragraph: Notion's API
// gives a callout's first line as its words when that line is a plain
// paragraph, so a plain paragraph child with none before it stands under an
// empty first line, where a heading, a list item or a colored line child is
// the first line itself (seen 2026-11-12, as notionCalloutFirstLine reads a
// callout back). Nil for a block that is no such callout; a callout with words
// staged its first line already and reads its children from 1.
func notionImportCalloutEmptyFirstLine(
	app core.App, jobID string, discovery notionImportPageDiscovery, children []notionProviderObject,
) (*stagedNotionSource, error) {
	if discovery.Kind != "block" || discovery.Cursor != "" || discovery.Position != 0 ||
		len(children) == 0 || !notionImportPlainParagraph(children[0]) {
		return nil, nil
	}
	stored := storedNotionSource{}
	err := app.DB().NewQuery(`
		SELECT [[source_id]], [[kind]], [[text]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId}
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": discovery.SourceID}).One(&stored)
	if err != nil {
		return nil, err
	}
	plan, err := decodeNotionParagraphSourceText(stored.Text)
	if err != nil || stored.Kind != "block" || plan.BlockType != graphBlockTypeCallout || plan.Text != "" {
		return nil, nil
	}
	return notionImportCalloutLine(discovery.SourceID, notionParagraphSourcePlan{EditedAt: plan.EditedAt}, discovery.Blueprint)
}

// notionImportPlainParagraph says whether a block is a paragraph in the
// page's own color, the line Notion gives as a callout's words.
func notionImportPlainParagraph(block notionProviderObject) bool {
	if block.Type != graphBlockTypeParagraph {
		return false
	}
	fields := map[string]any{}
	if len(block.Payload) > 0 && string(block.Payload) != "null" && json.Unmarshal(block.Payload, &fields) != nil {
		return false
	}
	color, _ := fields["color"].(string)
	return color == "" || color == "default" || color == "default_background"
}

// convertNotionBlockPayload returns the plan a block is staged with and
// whether its children are still to be read, or a nil plan when it cannot be
// brought over. Issues carry only their reason until they are placed.
func convertNotionBlockPayload(
	block notionProviderObject,
	fields map[string]json.RawMessage,
	blueprint bool,
) (*notionParagraphSourcePlan, string, []stagedNotionSource, error) {
	issue := func(reason string) []stagedNotionSource { return []stagedNotionSource{{Text: reason}} }
	switch block.Type {
	case graphBlockTypeSyncedBlock:
		return convertNotionSyncedBlock(block, fields, blueprint)
	case graphBlockTypeLinkToPage:
		return convertNotionLinkToPage(block, fields, blueprint)
	}
	keys, convertible := notionImportRequestKeys[block.Type]
	if !convertible {
		return nil, "", issue(notionImportIssueUnsupportedBlock), nil
	}
	plan := &notionParagraphSourcePlan{Spans: []notionParagraphSpan{}}
	_, textBlock := notionTextMutationBlockTypes[block.Type]
	textBlock = textBlock && block.Type != graphBlockTypeEquation && block.Type != graphBlockTypeDivider
	if textBlock {
		parts := []notionProviderRichText{}
		if raw, present := fields["rich_text"]; present && string(raw) != "null" {
			if json.Unmarshal(raw, &parts) != nil {
				return nil, "", nil, errors.New(errorCodeNotionImportProviderFailed)
			}
		}
		converted, err := notionProviderRichTextSourcePlan(parts, blueprint)
		if err != nil {
			return nil, "", nil, err
		}
		plan = &converted
	}
	request := map[string]any{}
	for _, key := range keys {
		raw, present := fields[key]
		if !present || string(raw) == "null" {
			continue
		}
		var value any
		if json.Unmarshal(raw, &value) != nil {
			return nil, "", nil, errors.New(errorCodeNotionImportProviderFailed)
		}
		request[key] = value
	}
	if textBlock {
		request["rich_text"] = []any{}
	}
	issues := []stagedNotionSource{}
	nativeType, content, err := normalizeNotionImportRequest(block.Type, request)
	if err != nil {
		for _, key := range notionImportOptionalRequestKeys {
			delete(request, key)
		}
		if block.Type == graphBlockTypeCode {
			request["language"] = "plain text"
		}
		if nativeType, content, err = normalizeNotionImportRequest(block.Type, request); err != nil {
			return nil, "", issue(notionImportIssueInvalidBlock), nil
		}
		issues = append(issues, issue(notionImportIssueSettingOmitted)...)
	}
	// A Notion callout whose icon was taken away comes with none (2026-11-05).
	if raw, present := fields["icon"]; present && string(raw) == "null" && block.Type == graphBlockTypeCallout {
		content["icon"], content["iconType"] = "", calloutIconNone
	}
	// A callout's picture Notion keeps is copied as its uploaded icon
	// (notion_import_block_icon.go).
	if raw := fields["icon"]; notionImportHostedBlockIcon(block.Type, raw) {
		content[notionImportHostedIconContentKey] = true
	} else if raw, present := fields["icon"]; present && string(raw) != "null" &&
		(block.Type == graphBlockTypeCallout || block.Type == graphBlockTypeParagraph) {
		var value any
		icon, iconErr := map[string]any(nil), json.Unmarshal(raw, &value)
		if iconErr == nil {
			// A tab paragraph's picture has no native block icon to become;
			// the block keeps its default icon.
			icon, iconErr = notionNativeBlockIconRequest(value, false)
		}
		if iconErr != nil {
			issues = append(issues, issue(notionImportIssueSettingOmitted)...)
		}
		for key, value := range icon {
			content[key] = value
		}
	}
	if raw, present := fields["caption"]; present && string(raw) != "null" &&
		(block.Type == graphBlockTypeCode || block.Type == graphBlockTypeEmbed) {
		parts := []notionProviderRichText{}
		if json.Unmarshal(raw, &parts) != nil {
			return nil, "", nil, errors.New(errorCodeNotionImportProviderFailed)
		}
		encoded, err := notionProviderRichTextSourceText(parts, blueprint)
		if err != nil {
			return nil, "", nil, err
		}
		caption, err := notionSourceTextGraphSpans(encoded)
		if err != nil {
			return nil, "", nil, err
		}
		content["caption"] = caption
	}
	// An imported toggle comes closed, as any written from outside
	// (closeNewToggle).
	closeNewToggle(nativeType, content)
	plan.BlockType, plan.Content = nativeType, content
	return plan, notionImportBlockDiscoveryStatus(block), issues, nil
}

// convertNotionSyncedBlock keeps an original synced block with its children.
// A synced copy names its original until the page is published, and its
// children, which are the original's, are not read again.
func convertNotionSyncedBlock(
	block notionProviderObject,
	fields map[string]json.RawMessage,
	blueprint bool,
) (*notionParagraphSourcePlan, string, []stagedNotionSource, error) {
	plan := &notionParagraphSourcePlan{Spans: []notionParagraphSpan{}, BlockType: graphBlockTypeSyncedBlock}
	raw, present := fields["synced_from"]
	if !present || string(raw) == "null" {
		plan.Content = map[string]any{}
		return plan, notionImportBlockDiscoveryStatus(block), nil, nil
	}
	syncedFrom := struct {
		Type    string `json:"type"`
		BlockID string `json:"block_id"`
	}{}
	if json.Unmarshal(raw, &syncedFrom) != nil {
		return nil, "", nil, errors.New(errorCodeNotionImportProviderFailed)
	}
	sourceID := strings.TrimSpace(syncedFrom.BlockID)
	if blueprint {
		return nil, "", nil, errors.New(errorCodeNotionImportProviderFailed)
	}
	if syncedFrom.Type != "block_id" || sourceID == "" {
		return nil, "", []stagedNotionSource{{Text: notionImportIssueInvalidBlock}}, nil
	}
	plan.Content = map[string]any{
		"syncedBlockId": notionImportPendingBlockPrefix + sourceID, notionImportSourceContentKey: block.ID,
	}
	return plan, notionImportPageDiscoveryComplete, nil, nil
}

// convertNotionLinkToPage names the linked Notion page or database until the
// page is published.
func convertNotionLinkToPage(
	block notionProviderObject,
	fields map[string]json.RawMessage,
	blueprint bool,
) (*notionParagraphSourcePlan, string, []stagedNotionSource, error) {
	if blueprint {
		return nil, "", nil, errors.New(errorCodeNotionImportProviderFailed)
	}
	field := func(key string) string {
		value := ""
		if raw, present := fields[key]; present {
			_ = json.Unmarshal(raw, &value)
		}
		return strings.TrimSpace(value)
	}
	content := map[string]any{notionImportSourceContentKey: block.ID}
	switch linkType := field("type"); {
	case linkType == "page_id" && field("page_id") != "":
		content["childPageId"] = notionImportPendingPagePrefix + field("page_id")
	case linkType == "database_id" && field("database_id") != "":
		content["databaseContainerId"] = notionImportPendingDatabasePrefix + field("database_id")
	default:
		// A link to a comment, or one Notion does not describe, has nothing
		// the import can point it at.
		return nil, "", []stagedNotionSource{{Text: notionImportIssueInvalidBlock}}, nil
	}
	return &notionParagraphSourcePlan{
		Spans: []notionParagraphSpan{}, BlockType: graphBlockTypeLinkToPage, Content: content,
	}, notionImportPageDiscoveryComplete, nil, nil
}

func normalizeNotionImportRequest(blockType string, request map[string]any) (string, map[string]any, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return "", nil, err
	}
	nativeType, content, _, err := normalizeNotionBlockMutationPayload(blockType, raw, nil)
	if err != nil {
		return "", nil, err
	}
	if content == nil {
		content = map[string]any{}
	}
	return nativeType, content, nil
}

// notionImportIssueSource stages one issue about a Notion block for the
// import report, where the block was.
func notionImportIssueSource(
	parentSourceID, parentKind string,
	position int,
	block notionProviderObject,
	reason string,
) stagedNotionSource {
	return stagedNotionSource{
		SourceID: block.ID + "#" + reason, Kind: "issue", ParentSourceID: parentSourceID,
		ParentKind: parentKind, Position: position, Title: notionImportIssueBlockType(block), Text: reason,
	}
}

// notionImportIssueBlockType names the Notion block type an issue is about;
// an unsupported block is named by the type Notion reports for it.
func notionImportIssueBlockType(block notionProviderObject) string {
	blockType := block.Type
	if blockType == "unsupported" {
		unsupported := struct {
			BlockType string `json:"block_type"`
		}{}
		if len(block.Payload) > 0 && json.Unmarshal(block.Payload, &unsupported) == nil &&
			unsupported.BlockType != "" {
			blockType = unsupported.BlockType
		}
	}
	if !notionImportBlockTypeLabel.MatchString(blockType) {
		return "unsupported"
	}
	return blockType
}
