package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A page's or a database's mention written through the MCP tools' Markdown
// or a comment's rich text, or the public API, JSON or Markdown — in a
// block's text, a table cell, a caption, a value, a title or a comment —
// names what its ID names in the
// page's workspace, by the rule its links follow (notion_address_links_here.go):
//   - what the workspace has by that ID — a page, a database page, a
//     database, or a block on a page — as before;
//   - else what a Notion move into the workspace made from the Notion page,
//     database page, database or block of that ID (its ID map or its
//     migration's ledger, notion_address_moved_ledger.go), the latest
//     move's first: the page's mention, the database's (a full-page one's as
//     the editor mentions it), or the block's on its page, as the move's own
//     mentions name what it brought (notionImportReferenceResolver.resolveMention);
//   - else, for a mention of one of Notion's web addresses naming a page by
//     its ID that a link to it would keep as written (notionAddressHere), a
//     link to that address, its words the tag's or, when it holds none, the
//     address — Notion keeps an "Untitled" mention of the ID, which opens
//     nothing here; the user decided on 2026-11-21 that it is the link a link
//     to that address is, which opens the page in Notion;
//   - else — an ID alone, of no web address, or a page here whose block is
//     gone — as before, the "Untitled" mention of the ID Notion keeps.
//
// The reader keeps the Notion web address for the boundary in the mention's
// link (notionMarkdownIDMention), which a mention leaves here; a JSON mention
// holds no address to link, so one of nothing here stays "Untitled".
//
// A data source's mention (<mention-data-source url="collection://ID">)
// names the workspace's data source of that ID as before, or else the one a
// Notion move made from the Notion data source of that ID
// (notion_address_moved_data_sources.go); else, as before, it is one that is
// not there, which leaves no text, as Notion's Markdown leaves none — but for
// the mention the MCP's writer makes of a link to it, which is that link
// again (notion_address_data_source_links_here.go).

// notionMentionedHere is what a mention's ID names in a workspace: something
// the workspace has (here), or what a Notion move made from the Notion
// object of that ID — a page, a database or a block, by its own ID — or
// neither.
type notionMentionedHere struct {
	here           bool
	kind, nativeID string
}

// mention points a page or database mention here, in place, and tells
// whether it changed.
func (here *notionAddressLinksHere) mention(span *graphTextSpan) (bool, error) {
	written := span.PageID
	switch {
	case span.Mention == "database":
		if dataSourceID, source := notionMarkdownDataSource(*span); source {
			return here.dataSourceLinkMention(span, dataSourceID)
		}
		written = span.DatabaseID
	case span.Mention != "page" || span.BlockID != "":
		return false, nil
	}
	address, changed := "", false
	if span.Link != "" && notionMarkdownNotionWebAddress(span.Link) != "" {
		address, span.Link, changed = span.Link, "", true
	}
	target, err := here.mentionedHere(written)
	switch {
	case err != nil || target.here:
		return changed, err
	case target.kind != "":
		pointNotionMovedMention(span, written, target)
		return true, nil
	case address == "":
		return changed, nil
	}
	if _, id, _ := notionAddressNamedIDs(address); id == "" {
		return changed, nil
	}
	pointed, err := here.address(address)
	if err != nil || pointed != address {
		return changed, err
	}
	// A tag without words kept its ID as them (notionMarkdownIDMention).
	words := span.Text
	shown, wordsID := normalizeNotionUUIDRequest(strings.TrimSpace(words))
	mentioned, _ := normalizeNotionUUIDRequest(strings.TrimPrefix(written, notionPageMentionRowPrefix))
	if strings.TrimSpace(words) == "" || wordsID && shown == mentioned {
		words = address
	}
	span.Mention, span.PageID, span.DatabaseID, span.Text, span.Link = "", "", "", words, address
	return true, nil
}

// mentionedHere is what a mention's ID names here (notionMentionedIn): a
// UUID, dashed or not, as written or as the public API's preparation keeps
// one no page has (notionPageMentionRowPrefix), each looked up once; an ID
// of any other form names nothing a move made.
func (here *notionAddressLinksHere) mentionedHere(written string) (notionMentionedHere, error) {
	id, isUUID := normalizeNotionUUIDRequest(strings.TrimPrefix(written, notionPageMentionRowPrefix))
	if !isUUID {
		return notionMentionedHere{}, nil
	}
	if found, cached := here.mentioned[id]; cached {
		return found, nil
	}
	found, err := notionMentionedIn(here.app, here.workspaceID, id)
	if err == nil {
		here.mentioned[id] = found
	}
	return found, err
}

// notionMentionedIn is what an ID names in a workspace for a mention: a
// page, a database page or a database it has (notionIDInWorkspace), or a
// block on a page (pageMentionBlock); else what a Notion move into it made
// from the Notion object of that ID (notionMovedMappings) — a page or a
// database page first, then a database by its block, a full-page
// database's page without one, then a block.
func notionMentionedIn(app core.App, workspaceID, id string) (notionMentionedHere, error) {
	if here, err := notionIDInWorkspace(app, workspaceID, id); err != nil || here {
		return notionMentionedHere{here: here}, err
	}
	if _, found, err := pageMentionBlock(app, workspaceID, id); err != nil || found {
		return notionMentionedHere{here: found}, err
	}
	mapped, err := notionMovedMappings(app, workspaceID, id, id)
	if err != nil {
		return notionMentionedHere{}, err
	}
	root, database := notionDatabaseRootPageSourceID(id), id+":block"
	found, rank := notionMentionedHere{}, 0
	for _, item := range mapped {
		order, target := 0, notionMentionedHere{}
		switch {
		case item.SourceID == id && item.Kind != "block":
			order, target = 4, notionMentionedHere{kind: "page", nativeID: item.PageID}
		case item.SourceID == database:
			order, target = 3, notionMentionedHere{kind: "database", nativeID: item.BlockID}
		case item.SourceID == root:
			order, target = 2, notionMentionedHere{kind: "page", nativeID: item.PageID}
		case item.SourceID == id:
			order, target = 1, notionMentionedHere{kind: "block", nativeID: item.BlockID}
		}
		if order > rank {
			found, rank = target, order
		}
	}
	return found, nil
}

// pointNotionMovedMention makes a mention name what a Notion move made from
// what it named, by the ID its writer would write for that: a database by
// its own ID, which a page's mention of it is too, as its database's; a
// block by its own, whose page the mention then names at the block
// (anchorPageMention); a page by its public ID — by its own where the
// public API's preparation has turned the written ID into the page's own
// already (notionPageMentionRowPrefix) — and a database's mention of a page
// by the page's public ID, which the writer's rule makes the page's mention
// (pageDatabaseMention, principalDatabaseMention).
func pointNotionMovedMention(span *graphTextSpan, written string, target notionMentionedHere) {
	if target.kind == "database" {
		span.Mention, span.PageID, span.DatabaseID = "database", "", target.nativeID
		return
	}
	id := target.nativeID
	if target.kind == "page" && (span.Mention == "database" || !strings.HasPrefix(written, notionPageMentionRowPrefix)) {
		if public, err := pageOwnPublicID(id); err == nil {
			id = public
		}
	}
	if span.Mention == "database" {
		span.DatabaseID = id
		return
	}
	span.PageID = id
}
