package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A data source's mention of nothing its writer may read — no data source
// here has its ID, or one the writer can't read — is kept by the ID
// written, as Notion's Markdown keeps one (seen 2026-11-22: <mention
// url="collection://<a UUID no data source has>"/> in a page's "v2" body by
// the MCP's insert_content and the public API, in a title by create-pages,
// and in a comment by the MCP and the public API's "v2"). "v2" and the MCP
// read it back as <mention url="collection://ID" />, "v1" as
// <mention-data-source url="collection://ID"/>, and the public API's rich
// text leaves it out, in a block's text and a comment's alike; the app shows
// it as a mention nothing answers to, "찾을 수 없음", as Notion's does. It
// stays the database mention named by its collection:// address that
// Markdown reads (notionMarkdownDataSource), as a data source here's own
// mention is by the source's own ID (data_source_mentions.go), its words
// "Untitled" as a page mention of nothing's are (keepUnresolvedPageMention);
// the ID written, a public API's or Notion's, is no source's own, so it
// never names a data source here that the writer could not read. One by an
// ID of no UUID's form leaves no text, as before.

// keepOrRefuseUnresolvedDataSource keeps a data source's mention of nothing
// the writer may read (keepUnresolvedDataSourceMention); one of an ID of no
// UUID's form refuses the write, as Notion refuses collection://x (seen
// 2026-11-22, a page's "v2" and a comment, by the MCP and the public API
// alike) in its words (notionMarkdownMentionPointSaid) — after its write
// looked for a data source here of that ID, as Leafen's own IDs, which its
// MCP wrote before it named one by the public API's
// (mcp_data_source_public_ids.go), are of any form; one the writer can't
// read is refused as one not there.
func keepOrRefuseUnresolvedDataSource(span *graphTextSpan, written string) error {
	if _, isUUID := notionKeptUUID(written); isUUID {
		keepUnresolvedDataSourceMention(span, written)
		return nil
	}
	return graphInputError(errorCodeGraphInvalid,
		notionMarkdownRefusedHead+notionMarkdownMentionPointSaid(notionMarkdownDataSourceAddress+strings.TrimSpace(written)))
}

// keepUnresolvedDataSourceMention keeps a data source's mention of nothing
// the writer may read by the ID written, in Notion's UUID form
// (notionKeptUUID), keeping its formatting; one of no UUID's form leaves no
// text (dropUnresolvedDataSourceMention).
func keepUnresolvedDataSourceMention(span *graphTextSpan, written string) {
	kept, isUUID := notionKeptUUID(written)
	if !isUUID {
		dropUnresolvedDataSourceMention(span)
		return
	}
	span.Mention, span.PageID, span.BlockID = "database", "", ""
	span.DatabaseID, span.Text = notionMarkdownDataSourceAddress+kept, notionUnresolvedMentionTitle
}

// notionKeptDataSourceID is the ID a mention kept by its address names —
// a data source's (above, or one here, data_source_mentions.go), a
// teamspace's or a group's (notionMarkdownTeamOrGroupMention) — false for any
// other span: once a write has named its mentions, a database mention still
// named by such an address is one.
func notionKeptDataSourceID(span graphTextSpan) (string, bool) {
	_, id, kept := notionKeptAddress(span)
	return id, kept
}

// notionKeptAddress is the scheme and ID of a mention kept by its address
// (notionKeptDataSourceID). A data source's is so by any ID: a source here
// is named by its own, of any form — a Notion move's or an import's, the
// app's —, and one kept as written by a UUID (keepUnresolvedDataSourceMention),
// as the app reads it (dataSourceMentionNames.ts).
func notionKeptAddress(span graphTextSpan) (string, string, bool) {
	if span.Mention != "database" {
		return "", "", false
	}
	scheme, id, addressed := strings.Cut(span.DatabaseID, "://")
	_, isUUID := normalizeNotionUUIDRequest(id)
	switch scheme {
	case "collection":
		return scheme, id, addressed && id != ""
	case "teamspace", "group":
		return scheme, id, addressed && (isUUID || leafenOwnID.MatchString(id))
	}
	return "", "", false
}

// notionMarkdownAddressed is notionMarkdownDataSource, and a mention by a
// teamspace's or a group's address too: one a write keeps as it is.
func notionMarkdownAddressed(span graphTextSpan) (string, bool) {
	if scheme, id, kept := notionKeptAddress(span); kept && scheme != "collection" {
		return id, true
	}
	return notionMarkdownDataSource(span)
}

// notionMarkdownKeptDataSourceTag writes a mention's tag
// (notionMarkdownMentionText), one kept by its address as its reader's
// Markdown does: the <mention> of "v2", the MCP and Leafen's own; in "v1" a
// data source's <mention-data-source>, a teamspace's its words "@[Team:ID]"
// and a group's none, as Notion's "v1" writes them (seen 2026-11-22). A data
// source here reads by the ID its reader names it by
// (notionMarkdownDataSourceID), its words left out as Notion's are.
func notionMarkdownKeptDataSourceTag(span graphTextSpan, mentions notionMarkdownMentions) (string, bool) {
	scheme, id, kept := notionKeptAddress(span)
	if !kept {
		return notionMarkdownMentionText(span, mentions)
	}
	if scheme == "collection" && mentions.DataSource != nil {
		id = mentions.DataSource(id)
	}
	address := notionMarkdownAttributeEscaped.Replace(scheme + "://" + id)
	switch {
	case !mentions.APIForm || mentions.Version2:
		return `<mention url="` + address + `" />`, true
	case scheme == "teamspace":
		return `@\[Team:` + id + `\]`, true
	case scheme == "group":
		return "", true
	}
	return `<mention-data-source url="` + address + `"/>`, true
}

// A teamspace's or a group's mention (<mention url="teamspace://ID"/>,
// group://ID) names one by its ID (notionMarkdownMentionNamesOne): Notion's
// page Markdown keeps it, by the ID in its dashed form, its words left out
// (seen 2026-11-22: teamspace://<a UUID no teamspace has>, group://<32
// hexadecimal digits> read back <mention url="group://<dashed>" />, in a
// page's "v2" by the MCP and the public API); "v2" and the MCP read it back
// so, "v1" as above, the public API's rich text leaves it out, and Notion's
// app shows a teamspace's of nothing as a grey bar and a group's as
// "@익명의 그룹". Leafen keeps it by its address too, the ID of one of its
// own teamspaces or groups as it is, whose name it does not show (Notion's
// reading of one there is not seen); a comment refuses it
// (notionCommentMentionHeld).

// notionMarkdownTeamOrGroupMention is the mention a teamspace's or a
// group's address reads as, false for any other address.
func notionMarkdownTeamOrGroupMention(address string) (graphTextSpan, bool) {
	scheme, id, _ := strings.Cut(strings.TrimSpace(address), "://")
	scheme = strings.ToLower(scheme)
	if scheme != "teamspace" && scheme != "group" {
		return graphTextSpan{}, false
	}
	if dashed, isUUID := normalizeNotionUUIDRequest(id); isUUID {
		id = dashed
	}
	return graphTextSpan{Text: notionUnresolvedMentionTitle, Mention: "database", DatabaseID: scheme + "://" + id}, true
}

// namedMention names a span's data source, database and page mentions as a
// Public API connection's write does (prepare); false for a data source's
// kept by its address, one here or the ID written, which no other check of a
// mention reads.
func (preparation *notionMentionPreparation) namedMention(span *graphTextSpan) (bool, error) {
	if err := preparation.dataSourceMention(span); err != nil {
		return false, err
	}
	if _, kept := notionKeptDataSourceID(*span); kept {
		return false, nil
	}
	if err := preparation.databasePageMention(span); err != nil {
		return false, err
	}
	return true, preparation.pageDatabaseMention(span)
}

// notionBlockMentionOrKept is a text run's mention as the public API's rich
// text reads it (notionBlockMention), a data source's kept by its address
// left out (notionMentionLeftOutKey), as Notion's API leaves one out of a
// block's text, a source there or not (seen 2026-11-22: the words around it
// two pieces).
func notionBlockMentionOrKept(
	app core.App, authorization notionBlockAuthorization, attributes crdt.Attributes, content string,
	fileIssuer *notionBlockFileCapabilityIssuer,
) (map[string]any, bool, error) {
	mention, _ := attributes["mention"].(string)
	databaseID, _ := attributes["databaseId"].(string)
	if _, kept := notionKeptDataSourceID(graphTextSpan{Mention: mention, DatabaseID: databaseID}); kept {
		return map[string]any{notionMentionLeftOutKey: true}, true, nil
	}
	return notionBlockMention(app, authorization, attributes, content, fileIssuer)
}

// notionCommentSpanLeftOut reports a piece of a comment the public API's
// rich text leaves out, as it leaves it out of a block's text: a custom
// emoji the workspace hasn't, or one its reader may not use
// (notion_missing_custom_emoji.go), and a data source's mention kept by its
// address (seen 2026-11-22: Notion's API reads a comment's as nothing).
func notionCommentSpanLeftOut(app core.App, principalID, workspaceID string, span mcpCommentSpan) (bool, error) {
	if _, kept := notionKeptDataSourceID(graphTextSpan{Mention: span.Mention, DatabaseID: span.DatabaseID}); kept {
		return true, nil
	}
	if span.Mention != "custom_emoji" {
		return false, nil
	}
	_, there, err := loadNotionCustomEmojiMention(app, principalID, workspaceID, span.CustomEmojiID)
	return !there, err
}
