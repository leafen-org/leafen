package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page mention may name a block on its page, as Notion's mention of a
// block does (seen 2026-11-11, Notion-Version 2026-03-11): a mention of the
// page holding the block, named by the block's words now — "Untitled" for a
// block without words (an empty line, a divider, a synced block, a
// bookmark) — that leads to the page at the block, `/p/<page>#<block>`; its
// Markdown reads `<mention url="…#<block>">words</mention>`, a pair even
// without words. A request names the block by its ID where a page's stands:
// a JSON mention's page ID, an address's fragment or its path's last part;
// an ID that names neither stays a mention of nothing by that ID. The span
// keeps the page as its pageId and the block as its blockId, with the
// block's words when it was written. Notion's screen draws one as the
// page's title, " - " and the block's words now, and opening it goes to
// the block; this server answers those words to the app by the block
// (readBlockMentionPreview). A block gone from its page leaves the mention
// with the words it was written with, through the JSON API and the public
// API's Markdown "v1"; its Markdown "v2", and the MCP's written alike, as a
// mention of nothing by the block's ID, as Notion's "v2" does (seen
// 2026-11-11, a block in the trash). A Notion import keeps a mention of a block it did not bring
// by the block's Notion ID, a block gone from the page here.

const blockMentionPreviewPath = "/pages/{pageId}/blocks/{blockId}/mention-preview"

// pageMentionBlock is the block a page mention names in place of a page, in
// the workspace: by its own ID, its Public API ID, or that ID without
// dashes, when no page answers to it.
func pageMentionBlock(app core.App, workspaceID, written string) (storedNotionBlock, bool, error) {
	written = strings.TrimSpace(strings.TrimPrefix(written, notionPageMentionRowPrefix))
	if written == "" {
		return storedNotionBlock{}, false, nil
	}
	candidates := []string{written}
	if blockID, ok := resolveNotionBlockID(app, written); ok && blockID != written {
		candidates = append([]string{blockID}, candidates...)
	}
	for _, id := range candidates {
		block, found, err := loadStoredNotionBlock(app, id)
		if err != nil {
			return storedNotionBlock{}, false, err
		}
		if !found || block.WorkspaceID != workspaceID || block.PageID == "" || block.PageID == id {
			continue
		}
		pages := 0
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": id}).Row(&pages); err != nil {
			return storedNotionBlock{}, false, err
		}
		if pages == 0 {
			return block, true, nil
		}
	}
	return storedNotionBlock{}, false, nil
}

// anchorPageMention makes a page mention written with a block's ID in place
// of a page's a mention of the block's page at that block; it reports
// whether it named a block.
func anchorPageMention(app core.App, workspaceID string, span *graphTextSpan) (storedNotionBlock, bool, error) {
	if span.Mention != "page" || span.BlockID != "" {
		return storedNotionBlock{}, false, nil
	}
	block, found, err := pageMentionBlock(app, workspaceID, span.PageID)
	if err != nil || !found {
		return storedNotionBlock{}, false, err
	}
	span.PageID, span.BlockID = block.PageID, block.ID
	return block, true, nil
}

// pageMentionAnchorBlock is a page mention's block while it is still on
// the mention's page.
func pageMentionAnchorBlock(app core.App, pageID, blockID string) (storedNotionBlock, bool) {
	block, found, err := loadStoredNotionBlock(app, blockID)
	if err != nil || !found || block.PageID != pageID {
		return storedNotionBlock{}, false
	}
	return block, true
}

// pageMentionBlockWords is what a mention of a block reads by, as Notion
// says a block's words (seen 2026-11-11): its text as it stands, a mention
// in it as say says it ("@" and what it names), an inline equation between
// "$"s; an equation block its expression; nothing for a block without text.
func pageMentionBlockWords(app core.App, block storedNotionBlock, say func(graphTextSpan) string) string {
	if block.Type == graphBlockTypeEquation {
		content := map[string]any{}
		_ = json.Unmarshal([]byte(block.Content), &content)
		expression, _ := content["expression"].(string)
		return expression
	}
	document, found, err := loadYgoDoc(app, block.PageID)
	if err != nil || !found {
		return ""
	}
	spans, _ := rowPageBlockTextAsItStands(document, block.ID, false)
	var words strings.Builder
	for _, span := range spans {
		switch {
		case span.Mention == "" && span.Equation != "":
			words.WriteString("$" + span.Equation + "$")
		case span.Mention == "page" || span.Mention == "database" || span.Mention == "date" ||
			span.Mention == "person" || span.Mention == "custom_emoji":
			words.WriteString(say(span))
		default:
			words.WriteString(span.Text)
		}
	}
	return words.String()
}

// pageMentionBlockSayer says a mention in a block's words for a person, by
// their ID, reading it: a page or database they may open by "@" and its
// name now, one they can't by "@Untitled", a date by "@" and its long form,
// a person by "@" and their name; a reading for no one opens every one.
func pageMentionBlockSayer(app core.App, readerID, workspaceID string) func(graphTextSpan) string {
	closer := func() searchMentionCloser { return nil }
	if readerID != "" {
		closer = readerCloser(app, readerID)
	}
	return pageMentionBlockSayerClosing(app, closer, workspaceID)
}

// pageMentionBlockSayerClosing says a mention in a block's words as
// pageMentionBlockSayer does, for a reader whose closer says what they can't
// open (one that closes nothing for no one).
func pageMentionBlockSayerClosing(app core.App, closer func() searchMentionCloser, workspaceID string) func(graphTextSpan) string {
	return func(span graphTextSpan) string {
		switch span.Mention {
		case "page", "database":
			if searchMentionReach(closer(), span.Mention, span.PageID, span.DatabaseID) != mentionOpens {
				return "@" + notionUnreachableMentionTitle
			}
			name := ""
			if span.Mention == "database" {
				name, _, _ = databaseContainerTitle(app, span.DatabaseID)
				name = notionDatabaseShownTitle(name)
			} else {
				name, _, _ = pageMentionTargetNow(app, span.PageID)
			}
			if name == "" {
				name = notionUnreachableMentionTitle
			}
			return "@" + name
		case "date":
			return "@" + notionMentionedDateText(span)
		case "person":
			names, err := personMentionNames(app, workspaceID, []string{span.UserID})
			if err != nil {
				return span.Text
			}
			return personMentionText(names, span.UserID, span.Text)
		}
		return span.Text
	}
}

// notionBlockMentionWords is a block's words as a mention of it reads
// through the Public API, its mentions said as Notion's API says them.
func notionBlockMentionWords(
	app core.App, identity notionOAuthBearerIdentity, ownerID, workspaceID string, block storedNotionBlock,
) string {
	return pageMentionBlockWords(app, block, func(span graphTextSpan) string {
		return notionSaidMention(app, identity, ownerID, workspaceID, span, nil)
	})
}

// notionBlockMentionReading is a mention of a block as the Public API reads
// it, as Notion's (seen 2026-11-11): a mention of its page, its words now
// ("Untitled" for none), leading to the page at the block; a block gone
// from the page keeps the words it was written with.
func notionBlockMentionReading(
	app core.App, authorization notionBlockAuthorization, target notionPageMentionTarget,
	blockID, written string, fileIssuer *notionBlockFileCapabilityIssuer,
) map[string]any {
	ownerID, workspaceID := authorization.Page.Workspace.OwnerID, authorization.Page.Workspace.WorkspaceID
	words := written
	if block, there := pageMentionAnchorBlock(app, target.InternalID, blockID); there {
		words = notionBlockMentionWords(app, authorization.Identity, ownerID, workspaceID, block)
	}
	if words == "" {
		words = notionUnresolvedMentionTitle
	}
	var href any
	if address, ok := fileIssuer.mentionAddress(app, target.PublicID).(string); ok {
		href = address + "#" + notionBlockAnchorHex(app, workspaceID, blockID)
	}
	return map[string]any{
		"type": "page", "page": map[string]any{"id": target.PublicID},
		notionMentionPlainTextKey: words, notionMentionHrefKey: href,
	}
}

// nameAnchoredPageMention gives a page mention of a block the block's words
// as the writer, by their ID, says them, "Untitled" for none, as Notion's
// API reads it back.
func nameAnchoredPageMention(app core.App, writerID, workspaceID string, span *graphTextSpan, block storedNotionBlock) {
	span.Text = pageMentionBlockWords(app, block, pageMentionBlockSayer(app, writerID, workspaceID))
	if span.Text == "" {
		span.Text = notionUnresolvedMentionTitle
	}
}

// blockMention reads a page mention's block for a page's Markdown:
// at the page's address, its fragment the block's ID without dashes, as
// Notion's — the Public API's ID, or Leafen's for the MCP tools
// (mcpBlockAnchor); its words with a mention in them said as "@" and what
// the reader reads it by. A block gone from the page reads by its own
// address, as one nothing answers to.
func (reader notionMarkdownReader) blockMention(
	app core.App, workspaceID string, mentions notionMarkdownMentions, readable func(string) bool,
) func(pageAddress, pageID, blockID string) (string, string, bool) {
	say := func(span graphTextSpan) string {
		switch span.Mention {
		case "page":
			if !readable(span.PageID) {
				return "@" + notionUnreachableMentionTitle
			}
			if title := mentions.PageTitle(span.PageID); title != "" {
				return "@" + title
			}
			return "@" + notionUnreachableMentionTitle
		case "database":
			if _, open := mentions.Database(span.DatabaseID); !open {
				return "@" + notionUnreachableMentionTitle
			}
			return "@" + notionDatabaseShownTitle(mentions.DatabaseTitle(span.DatabaseID))
		case "date":
			return "@" + notionMentionedDateText(span)
		case "person":
			if name := mentions.UserName(span.UserID); name != "" {
				return "@" + strings.TrimPrefix(name, "@")
			}
		}
		return span.Text
	}
	return func(pageAddress, pageID, blockID string) (string, string, bool) {
		block, there := pageMentionAnchorBlock(app, pageID, blockID)
		switch {
		case reader.InternalIDs && there:
			return pageAddress + mcpBlockAnchor(block.ID), pageMentionBlockWords(app, block, say), true
		case reader.InternalIDs:
			return notionMentionURL(reader.Origin, blockID), "", false
		case there:
			return pageAddress + "#" + notionBlockAnchorHex(app, workspaceID, block.ID), pageMentionBlockWords(app, block, say), true
		}
		return notionMentionURL(reader.Origin, notionBlockAnchorHex(app, workspaceID, blockID)), "", false
	}
}

// mcpBlockAnchor is the fragment an address by Leafen's IDs, as the MCP
// tools write one, takes for a block on its page: "#" and the block's ID
// without dashes, as Notion's MCP writes a block's mention (seen 2026-11-22:
// …/p/<page>#<block>, both 32 hexadecimal digits); an ID whose dashes no
// reader could put back keeps Leafen's own "#block-". Either reads as the
// block (mcpLinkedBlockID, notionMarkdownPageMentionID), the dashed form of
// before too.
func mcpBlockAnchor(blockID string) string {
	if id := notionMentionID(blockID); notionMarkdownBareIDPattern.MatchString(id) {
		return "#" + id
	}
	return "#block-" + blockID
}

// notionBlockAnchorHex is the fragment an address to a block on a page takes
// through the Public API: the block's ID there without dashes, as Notion's.
func notionBlockAnchorHex(app core.App, workspaceID, blockID string) string {
	publicID, err := notionBlockPublicID(app, workspaceID, blockID)
	if err != nil {
		publicID = blockID
	}
	return strings.ReplaceAll(publicID, "-", "")
}

// notionMarkdownPageMentionID is the ID a page mention's address names: the
// block its fragment names — Notion's 32 hexadecimal digits, or Leafen's
// "block-" and the block's ID — or else the ID its path ends in.
func notionMarkdownPageMentionID(address string) string {
	if parsed, err := url.Parse(address); err == nil && parsed.Fragment != "" {
		if own, found := strings.CutPrefix(parsed.Fragment, "block-"); found && own != "" {
			return own
		}
		if hex := notionMarkdownAddressHex(parsed.Fragment); hex != "" &&
			len(strings.ReplaceAll(parsed.Fragment, "-", "")) == len(hex) {
			return hex
		}
	}
	return notionMarkdownAddressID(address)
}

// readBlockMentionPreview answers the words a mention of a block on a page
// reads by now for its reader, and the page's title: what the app draws, as
// Notion's screen does; a block no longer on that page isn't found.
func readBlockMentionPreview(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	blockID := strings.TrimSpace(event.Request.PathValue("blockId"))
	block, found, err := loadStoredNotionBlock(event.App, blockID)
	found = found && block.PageID == pageID
	if err == nil && found {
		var visible []workspaceSearchCandidate
		visible, err = filterWorkspaceSearchCandidates(event.App, user.Id, []workspaceSearchCandidate{{PageID: block.PageID}})
		found = len(visible) > 0
	}
	if errors.Is(err, sql.ErrNoRows) || err == nil && !found {
		return apiError(event, http.StatusNotFound, errorCodeBlockNotFound, "Block not found.")
	}
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "block-mention",
			errorCodeWorkspaceSearchFailed, "Couldn't read the block.",
		)
	}
	words := pageMentionBlockWords(event.App, block, pageMentionBlockSayer(event.App, user.Id, block.WorkspaceID))
	// The page's title as its own mention reads it, which the app's list of
	// pages names it by instead when it has the page.
	title, _, _ := pageMentionTargetNow(event.App, block.PageID)
	return event.JSON(http.StatusOK, blockMentionPreviewResponse{PageID: block.PageID, Title: title, Words: words})
}
