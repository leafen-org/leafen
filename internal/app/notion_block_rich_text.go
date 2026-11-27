package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// notionMentionPlainTextKey carries a mention's text now from where the
// mention is read to its rich text item; the item doesn't keep it.
const notionMentionPlainTextKey = "\x00plainText"

// notionMentionUnnamedKey marks a mention its reader can't open for its rich
// text item (notionUnnamedMention).
const notionMentionUnnamedKey = "\x00unnamed"

// notionMentionHrefKey carries where a page or database mention leads to its
// rich text item's href.
const notionMentionHrefKey = "\x00href"

// notionMentionLeftOutKey marks a mention its rich text leaves out, as Notion's
// API does: a custom emoji the workspace hasn't (notion_missing_custom_emoji.go),
// a data source's kept by the ID written (notionBlockMentionOrKept).
const notionMentionLeftOutKey = "\x00leftOut"

// notionUntitledPageMentionTitle is what a mention of a page with no title
// reads through Notion's API (seen 2026-11-09), as its screen names one.
const notionUntitledPageMentionTitle = "New page"

// mentionAddress is where a page or database mention leads, as Notion's API
// gives every one's href, one its reader can't reach or that isn't there too
// (seen 2026-11-09): the address on this server by the ID the mention names
// it by; nil for an answer made with no address of its own.
func (issuer *notionBlockFileCapabilityIssuer) mentionAddress(app core.App, publicID string) any {
	if publicID == "" || issuer.initialize(app) != nil {
		return nil
	}
	return notionMentionURL(issuer.origin, publicID)
}

// notionUnnamedMention is a mention of a page or database a connection can't
// reach, or that nothing answers to, as Notion's API gives either (seen
// 2026-11-09): a page mention by the target's ID — a database's too — whose
// item reads "Untitled", with no formatting, and leads to that ID's address
// (mentionAddress; mcp_reader_mentions.go).
func notionUnnamedMention(publicID string) map[string]any {
	return map[string]any{"type": "page", "page": map[string]any{"id": publicID}, notionMentionUnnamedKey: true}
}

// notionUnnamedMentionItem makes a rich text item of an unnamed mention as
// Notion's reads.
func notionUnnamedMentionItem(item map[string]any) {
	item["plain_text"] = notionUnresolvedMentionTitle
	item["annotations"] = map[string]any{
		"bold": false, "italic": false, "strikethrough": false,
		"underline": false, "code": false, "color": "default",
	}
}

// notionTableCellMentionItem reads a table row cell's mention as Notion's
// table rows read every one (seen 2026-11-11, written through JSON or page
// Markdown, before and after the target's rename): a page's or a database's
// as an unnamed page mention, "Untitled" without its formatting, and a
// person's by the ID alone as "@Anonymous", formatting kept. Page Markdown
// names them as any other mention.
func notionTableCellMentionItem(item map[string]any) {
	mention, _ := item["mention"].(map[string]any)
	switch kind, _ := mention["type"].(string); kind {
	case "page", "database":
		target, _ := mention[kind].(map[string]any)
		id, _ := target["id"].(string)
		item["mention"] = map[string]any{"type": "page", "page": map[string]any{"id": id}}
		notionUnnamedMentionItem(item)
	case "user":
		person, _ := mention["user"].(map[string]any)
		id, _ := person["id"].(string)
		item["mention"] = map[string]any{"type": "user", "user": map[string]any{"object": "user", "id": id}}
		item["plain_text"] = notionUnknownPersonMentionText
	}
}

// notionRichTextRun is one stretch of text with its formatting, as block
// text and database text cells keep it; Empty names the kind of an empty
// piece (graph_text_empty_pieces.go).
type notionRichTextRun struct {
	Content    string
	Attributes crdt.Attributes
	Empty      string
}

func notionBlockRichText(
	app core.App,
	authorization notionBlockAuthorization,
	text *crdt.YText,
	fileIssuer *notionBlockFileCapabilityIssuer,
) ([]map[string]any, bool, error) {
	deltas := textDeltaAsItStands(text.ToDelta(), false)
	runs := make([]notionRichTextRun, 0, len(deltas))
	for _, delta := range deltas {
		content, ok := delta.Insert.(string)
		empty := graphTextEmptyPieceKind(delta.Insert)
		if delta.Op != crdt.DeltaOpInsert || !ok && empty == "" {
			return nil, false, nil
		}
		runs = append(runs, notionRichTextRun{Content: content, Attributes: delta.Attributes, Empty: empty})
	}
	return notionRichTextFromRuns(app, authorization, runs, fileIssuer)
}

// notionSpansRichText reads a database text cell's spans as Notion rich text.
func notionSpansRichText(
	app core.App,
	authorization notionBlockAuthorization,
	spans []graphTextSpan,
	fileIssuer *notionBlockFileCapabilityIssuer,
) ([]map[string]any, bool, error) {
	runs := make([]notionRichTextRun, 0, len(spans))
	for _, span := range graphTextSpansAsTheyStand(spans, false) {
		runs = append(runs, notionRichTextRun{Content: span.Text, Attributes: graphTextAttributes(span), Empty: span.EmptyPiece})
	}
	return notionRichTextFromRuns(app, authorization, runs, fileIssuer)
}

// notionRichTextWorkspaceID is the workspace whose public IDs a reading
// names the pages and blocks it links to by.
func notionRichTextWorkspaceID(authorization notionBlockAuthorization) string {
	if authorization.Identity.WorkspaceID != "" {
		return authorization.Identity.WorkspaceID
	}
	return authorization.Page.Workspace.WorkspaceID
}

// notionRichTextPageID is the page whose text a reading reads, "" for none.
func notionRichTextPageID(authorization notionBlockAuthorization) string {
	if authorization.Page.Page == nil {
		return ""
	}
	return authorization.Page.Page.Id
}

// notionRichTextFromRuns reads formatted text as Notion rich text items,
// every one — Notion's API reads out 300 pieces of a paragraph (seen
// 2026-10-13); 100 is only what it takes in one request — or reports false
// for text the official form can't hold.
func notionRichTextFromRuns(
	app core.App,
	authorization notionBlockAuthorization,
	runs []notionRichTextRun,
	fileIssuer *notionBlockFileCapabilityIssuer,
) ([]map[string]any, bool, error) {
	items := make([]map[string]any, 0, len(runs))
	for _, run := range runs {
		content := run.Content
		annotations, link, ok := notionBlockRichTextAttributes(run.Attributes)
		if !ok {
			return nil, false, nil
		}
		mention, mentioned, err := notionBlockMentionOrKept(
			app, authorization, run.Attributes, content, fileIssuer,
		)
		if err != nil {
			return nil, false, err
		}
		if _, leftOut := mention[notionMentionLeftOutKey]; leftOut {
			continue
		}
		equation, equationOK := notionRichTextStringAttribute(run.Attributes["equation"])
		if !equationOK {
			return nil, false, nil
		}
		mentionType, _ := mention["type"].(string)
		if !mentioned && notionBlockHasMentionIdentity(run.Attributes) ||
			mentioned && link != "" && mentionType != "link_mention" ||
			equation != "" && (mentioned || link != "" || equation != content ||
				len([]rune(equation)) > maxNotionInlineEquationRunes) {
			return nil, false, nil
		}
		var textLink any
		var href any
		if link != "" {
			// The page keeps the address as written; the API reads it out as
			// Notion's does (notionAPITextLink).
			address := notionAPITextLink(app, notionRichTextWorkspaceID(authorization), notionRichTextPageID(authorization), link,
				authorization.Identity.Origin)
			textLink = map[string]any{"url": address}
			href = address
		}
		item := map[string]any{
			"type":        "text",
			"text":        map[string]any{"content": content, "link": textLink},
			"annotations": annotations,
			"plain_text":  content,
			"href":        href,
		}
		if mentioned {
			item["type"] = "mention"
			item["mention"] = mention
			delete(item, "text")
			// A date mention reads as its date shows.
			if date, ok := notionDateFields(mention["date"]); ok {
				start, _ := date["start"].(string)
				end, _ := date["end"].(string)
				item["plain_text"] = notionMentionDatePlainText(start, end)
			}
			// A link mention reads as its address, its title being the mention's.
			if mentionType == "link_mention" {
				item["plain_text"] = link
			}
			if text, ok := mention[notionMentionPlainTextKey].(string); ok {
				item["plain_text"] = text
				delete(mention, notionMentionPlainTextKey)
			}
			if address, ok := mention[notionMentionHrefKey]; ok {
				item["href"] = address
				delete(mention, notionMentionHrefKey)
			}
			if _, unnamed := mention[notionMentionUnnamedKey]; unnamed {
				notionUnnamedMentionItem(item)
				delete(mention, notionMentionUnnamedKey)
			}
		} else if equation != "" || run.Empty == graphTextEmptyEquation {
			item["type"] = "equation"
			item["equation"] = map[string]any{"expression": equation}
			delete(item, "text")
		}
		items = append(items, notionRichTextPieces(item)...)
	}
	return items, true, nil
}

func notionBlockRichTextAttributes(attributes crdt.Attributes) (map[string]any, string, bool) {
	result := map[string]any{
		"bold": false, "italic": false, "strikethrough": false,
		"underline": false, "code": false, "color": "default",
	}
	link, background := "", ""
	for key, value := range attributes {
		switch key {
		case "bold", "italic", "strikethrough", "underline", "code":
			active, valid := notionRichTextBooleanAttribute(value)
			if !valid {
				return nil, "", false
			}
			result[key] = active
		case "color":
			color, valid := notionRichTextStringAttribute(value)
			if !valid || color != "" && color != "default" && !validNotionBlockColor(color) {
				return nil, "", false
			}
			if color != "" {
				result[key] = color
			}
		case "background":
			candidate, valid := notionRichTextStringAttribute(value)
			if !valid || candidate != "" && !validNotionSpanBackground(candidate) {
				return nil, "", false
			}
			background = candidate
		case "link":
			candidate, valid := notionRichTextStringAttribute(value)
			if !valid || candidate != "" && !validGraphHeldTextLink(candidate) {
				return nil, "", false
			}
			link = candidate
		case "mention", "pageId", "blockId", "databaseId", "customEmojiId", "date", "dateStartUnset", "dateEnd", "dateTimeZone", "dateReminder", "dateFormat", "dateTimeFormat",
			"userId", "iconUrl", "provider", "description", "imageUrl", "equation", "template",
			// What keeps two pieces that read alike apart is no
			// formatting (graph_text_pieces.go).
			graphTextPieceKey,
			// A comment's place in the text is no formatting: Notion's rich
			// text holds none, its discussions being the comments API's
			// (2026-11-03 audit: a commented paragraph read as unsupported).
			"commentId":
		default:
			if notionRichTextAttributeActive(value) {
				return nil, "", false
			}
		}
	}
	// A span with both colors shows Notion's API its background (seen 2026-10-10).
	if color, _ := result["color"].(string); background != "" || strings.HasSuffix(color, "_background") {
		result["color"] = notionSpanColor(color, background)
	}
	return result, link, true
}

func notionBlockMention(
	app core.App,
	authorization notionBlockAuthorization,
	attributes crdt.Attributes,
	content string,
	fileIssuer *notionBlockFileCapabilityIssuer,
) (map[string]any, bool, error) {
	mention, ok := attributes["mention"].(string)
	if !ok || mention == "" {
		return nil, false, nil
	}
	switch mention {
	case "page":
		pageID, pageOK := attributes["pageId"].(string)
		if !pageOK || pageID == "" || notionRichTextAttributeActive(attributes["databaseId"]) ||
			notionRichTextAttributeActive(attributes["customEmojiId"]) ||
			notionRichTextAttributeActive(attributes["userId"]) ||
			notionRichTextAttributeActive(attributes["date"]) ||
			notionRichTextAttributeActive(attributes["dateEnd"]) ||
			notionRichTextAttributeActive(attributes["dateTimeZone"]) ||
			notionLinkMentionPreviewActive(attributes) {
			return nil, false, nil
		}
		// A mention kept by an ID no page answered to is unnamed by that ID.
		if notionUnresolvedPageMention(app, pageID) {
			mention := notionUnnamedMention(pageID)
			mention[notionMentionHrefKey] = fileIssuer.mentionAddress(app, pageID)
			return mention, true, nil
		}
		target, allowed, err := authorizeNotionPageMentionTarget(
			app, authorization.Identity, authorization.Page.Workspace.OwnerID,
			authorization.Page.Workspace.WorkspaceID, pageID,
		)
		if err != nil {
			return nil, false, err
		}
		if !allowed {
			publicID, err := notionPagePublicID(app, pageID)
			if err != nil {
				return nil, false, nil
			}
			mention := notionUnnamedMention(publicID)
			mention[notionMentionHrefKey] = fileIssuer.mentionAddress(app, publicID)
			return mention, true, nil
		}
		// One of a block reads with the block's words now
		// (page_mention_block_anchor.go).
		if blockID, _ := attributes["blockId"].(string); blockID != "" {
			return notionBlockMentionReading(app, authorization, target, blockID, content, fileIssuer), true, nil
		}
		// It reads with the page's title now, whatever it was written with,
		// as Notion's does (seen 2026-11-09).
		title := notionMentionedPagePlainText(
			app, authorization.Identity, authorization.Page.Workspace.OwnerID, authorization.Page.Workspace.WorkspaceID, target, nil,
		)
		if title == "" {
			title = notionUntitledPageMentionTitle
		}
		return map[string]any{
			"type": "page", "page": map[string]any{"id": target.PublicID},
			notionMentionPlainTextKey: title, notionMentionHrefKey: fileIssuer.mentionAddress(app, target.PublicID),
		}, true, nil
	case "database":
		databaseID, databaseOK := attributes["databaseId"].(string)
		normalized, normalizedOK := normalizeNotionUUIDRequest(databaseID)
		if !databaseOK || !normalizedOK || normalized != databaseID ||
			notionRichTextAttributeActive(attributes["pageId"]) ||
			notionRichTextAttributeActive(attributes["customEmojiId"]) ||
			notionRichTextAttributeActive(attributes["userId"]) ||
			notionRichTextAttributeActive(attributes["date"]) ||
			notionRichTextAttributeActive(attributes["dateEnd"]) ||
			notionRichTextAttributeActive(attributes["dateTimeZone"]) ||
			notionLinkMentionPreviewActive(attributes) {
			return nil, false, nil
		}
		// One in the trash reads by its kind and name as any
		// (authorizeNotionDatabaseMentionTarget).
		allowed, err := authorizeNotionDatabaseMentionTarget(
			app, authorization.Identity, authorization.Page, databaseID,
		)
		if err != nil {
			return nil, false, nil
		}
		if !allowed {
			publicID, err := notionDatabaseMentionPublicID(app, authorization.Page.Workspace.WorkspaceID, databaseID)
			if err != nil {
				return nil, false, err
			}
			mention := notionUnnamedMention(publicID)
			mention[notionMentionHrefKey] = fileIssuer.mentionAddress(app, publicID)
			return mention, true, nil
		}
		// The database as the Public API names it, read with its name now.
		publicID, err := ensureNotionObjectIdentity(
			app, notionObjectIdentityDatabase, authorization.Page.Workspace.WorkspaceID, databaseID,
		)
		if err != nil {
			return nil, false, err
		}
		mention := map[string]any{
			"type": "database", "database": map[string]any{"id": publicID},
			notionMentionHrefKey: fileIssuer.mentionAddress(app, publicID),
		}
		if title, found, err := databaseContainerTitle(app, databaseID); err != nil {
			return nil, false, err
		} else if found && title != "" {
			mention[notionMentionPlainTextKey] = title
		}
		return mention, true, nil
	case "custom_emoji":
		customEmojiID, customEmojiOK := attributes["customEmojiId"].(string)
		if !customEmojiOK || customEmojiID == "" ||
			notionRichTextAttributeActive(attributes["pageId"]) ||
			notionRichTextAttributeActive(attributes["databaseId"]) ||
			notionRichTextAttributeActive(attributes["userId"]) ||
			notionRichTextAttributeActive(attributes["date"]) ||
			notionRichTextAttributeActive(attributes["dateEnd"]) ||
			notionRichTextAttributeActive(attributes["dateTimeZone"]) ||
			notionLinkMentionPreviewActive(attributes) {
			return nil, false, nil
		}
		// One kept by an ID no emoji has, as written (notionMissingCustomEmojiKept),
		// is as missing as one deleted.
		emoji, accessible, err := loadNotionCustomEmojiMention(
			app, authorization.Identity.PrincipalID,
			authorization.Page.Workspace.WorkspaceID, customEmojiID,
		)
		if err != nil {
			return nil, false, err
		}
		if !accessible {
			return map[string]any{notionMentionLeftOutKey: true}, true, nil
		}
		payload, err := fileIssuer.customEmojiPayload(
			app, authorization.Page.Workspace.WorkspaceID, emoji,
		)
		if err != nil {
			return nil, false, err
		}
		return map[string]any{"type": "custom_emoji", "custom_emoji": payload}, true, nil
	case "external":
		link, linkOK := notionRichTextStringAttribute(attributes["link"])
		iconURL, iconOK := notionRichTextStringAttribute(attributes["iconUrl"])
		if !linkOK || !iconOK || notionPageExternalFile(link) == nil ||
			iconURL != "" && notionPageExternalFile(iconURL) == nil ||
			notionRichTextAttributeActive(attributes["pageId"]) ||
			notionRichTextAttributeActive(attributes["databaseId"]) ||
			notionRichTextAttributeActive(attributes["customEmojiId"]) ||
			notionRichTextAttributeActive(attributes["userId"]) ||
			notionRichTextAttributeActive(attributes["date"]) ||
			notionRichTextAttributeActive(attributes["dateEnd"]) ||
			notionRichTextAttributeActive(attributes["dateTimeZone"]) {
			return nil, false, nil
		}
		payload := map[string]any{"href": link}
		if content != "" {
			payload["title"] = content
		}
		if iconURL != "" {
			payload["icon_url"] = iconURL
		}
		// What it keeps of its page's preview reads out as Notion's link
		// mention gives it: its description, its site's name and picture.
		for key, name := range map[string]string{"description": "description", "provider": "link_provider", "imageUrl": "thumbnail_url"} {
			value, valueOK := notionRichTextStringAttribute(attributes[key])
			if !valueOK || key == "imageUrl" && value != "" && notionPageExternalFile(value) == nil {
				return nil, false, nil
			}
			if value != "" {
				payload[name] = value
			}
		}
		return map[string]any{
			"type": "link_mention", "link_mention": payload,
		}, true, nil
	case "person":
		userID, userOK := attributes["userId"].(string)
		if !userOK || userID == "" || notionRichTextAttributeActive(attributes["pageId"]) ||
			notionRichTextAttributeActive(attributes["databaseId"]) ||
			notionRichTextAttributeActive(attributes["customEmojiId"]) ||
			notionRichTextAttributeActive(attributes["date"]) ||
			notionRichTextAttributeActive(attributes["dateEnd"]) ||
			notionRichTextAttributeActive(attributes["dateTimeZone"]) ||
			notionLinkMentionPreviewActive(attributes) {
			return nil, false, nil
		}
		// A connection's bot, or the reading token's own, reads as Notion's
		// mention of one (seen 2026-11-10); another token's bot reads by its
		// ID alone, as its people values do; one page Markdown kept
		// as no one's (notionUnknownPersonMention) reads as such.
		if bot, found := notionMentionBotFor(app, authorization.Identity, authorization.Page.Workspace.WorkspaceID, userID); found &&
			!notionUnknownPersonMention(userID, content) {
			var name any
			if bot.Name != "" {
				name = bot.Name
			}
			return map[string]any{
				"type": "user", "user": map[string]any{
					"object": "user", "id": bot.ID, "name": name, "avatar_url": nil, "type": "bot", "bot": map[string]any{},
				},
				notionMentionPlainTextKey: "@" + bot.Name,
			}, true, nil
		}
		// Someone who has left stays mentioned, by the ID alone.
		user, inWorkspace, err := loadNotionMentionUser(
			app, authorization.Page.Workspace.WorkspaceID, userID,
		)
		if err != nil {
			return nil, false, nil
		}
		publicID, err := notionCompatibleUserID(userID)
		// One kept by an ID no person here has, which Markdown keeps
		// (notion_markdown_person_mentions.go), reads by that ID.
		if _, isUUID := normalizeNotionUUIDRequest(userID); err != nil && isUUID {
			publicID, err = strings.TrimSpace(userID), nil
		}
		if err != nil {
			return nil, false, nil
		}
		// The person reads as a person value does (notionPersonObject).
		_, emails := notionIdentityReadsPeople(app, authorization.Identity)
		person, err := notionPersonObject(authorization.Identity, emails, user, inWorkspace, publicID)
		if err != nil {
			return nil, false, nil
		}
		mention := map[string]any{"type": "user", "user": person}
		// It reads with the person's name now (person_mention_names.go).
		if name := strings.TrimSpace(user.Name); name != "" {
			mention[notionMentionPlainTextKey] = "@" + name
		}
		return mention, true, nil
	case "date":
		date, dateOK := attributes["date"].(string)
		dateEnd, dateEndOK := notionRichTextStringAttribute(attributes["dateEnd"])
		dateTimeZone, dateTimeZoneOK := notionRichTextStringAttribute(attributes["dateTimeZone"])
		value, valid := notionMentionDateObject(date, dateEnd, dateTimeZone)
		// One whose start was never given shows none, as Notion's (seen
		// 2026-11-11: {"end": null, "time_zone": null}); with a clock, the
		// start its time zone doesn't read.
		if unset, _ := attributes["dateStartUnset"].(bool); unset && date == "" {
			delete(value, "start")
		}
		if !dateOK || !dateEndOK || !dateTimeZoneOK || !valid ||
			notionRichTextAttributeActive(attributes["pageId"]) ||
			notionRichTextAttributeActive(attributes["databaseId"]) ||
			notionRichTextAttributeActive(attributes["customEmojiId"]) ||
			notionRichTextAttributeActive(attributes["userId"]) ||
			notionLinkMentionPreviewActive(attributes) {
			return nil, false, nil
		}
		return map[string]any{"type": "date", "date": value}, true, nil
	case graphTemplateMention:
		// A template's "when duplicated" mention, as Notion's template_mention.
		value, _ := attributes["template"].(string)
		if !validGraphTemplateMention(value) || notionBlockHasMentionIdentityBeyond(attributes) {
			return nil, false, nil
		}
		if value == graphTemplateMentionMe {
			return map[string]any{"type": "template_mention", "template_mention": map[string]any{
				"type": "template_mention_user", "template_mention_user": value,
			}}, true, nil
		}
		return map[string]any{"type": "template_mention", "template_mention": map[string]any{
			"type": "template_mention_date", "template_mention_date": value,
		}}, true, nil
	default:
		return nil, false, nil
	}
}

// notionBlockHasMentionIdentityBeyond reports whether a mention names a
// page, database, emoji, date, person or icon beside its own kind.
func notionBlockHasMentionIdentityBeyond(attributes crdt.Attributes) bool {
	return notionRichTextAttributeActive(attributes["pageId"]) ||
		notionRichTextAttributeActive(attributes["databaseId"]) ||
		notionRichTextAttributeActive(attributes["customEmojiId"]) ||
		notionRichTextAttributeActive(attributes["date"]) ||
		notionRichTextAttributeActive(attributes["dateEnd"]) ||
		notionRichTextAttributeActive(attributes["dateTimeZone"]) ||
		notionRichTextAttributeActive(attributes["userId"]) ||
		notionLinkMentionPreviewActive(attributes)
}

// notionLinkMentionPreviewActive reports whether a span keeps what an
// outside page's mention keeps of it: its icon or its preview.
func notionLinkMentionPreviewActive(attributes crdt.Attributes) bool {
	for _, key := range []string{"iconUrl", "provider", "description", "imageUrl"} {
		if notionRichTextAttributeActive(attributes[key]) {
			return true
		}
	}
	return false
}

func notionBlockHasMentionIdentity(attributes crdt.Attributes) bool {
	return notionRichTextAttributeActive(attributes["mention"]) ||
		notionRichTextAttributeActive(attributes["pageId"]) ||
		notionRichTextAttributeActive(attributes["databaseId"]) ||
		notionRichTextAttributeActive(attributes["customEmojiId"]) ||
		notionRichTextAttributeActive(attributes["date"]) ||
		notionRichTextAttributeActive(attributes["dateEnd"]) ||
		notionRichTextAttributeActive(attributes["dateTimeZone"]) ||
		notionRichTextAttributeActive(attributes["userId"]) ||
		notionLinkMentionPreviewActive(attributes)
}

func notionRichTextBooleanAttribute(value any) (bool, bool) {
	if value == nil {
		return false, true
	}
	active, ok := value.(bool)
	return active, ok
}

func notionRichTextStringAttribute(value any) (string, bool) {
	if value == nil {
		return "", true
	}
	text, ok := value.(string)
	return text, ok
}

func notionRichTextAttributeActive(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	default:
		return true
	}
}
