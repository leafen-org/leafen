package app

import (
	"errors"
	"strings"
)

// The MCP's notion-create-pages, unless asked to keep internal links, makes
// a link to a data source ([words](collection://ID)) the source's mention
// (mcpLinkAsWritten), as Notion's MCP does (seen 2026-11-11) — when there is
// such a source: one of the workspace's, by its public API ID or its own
// (dataSourceDatabase, the trash's too), or one a Notion move made from the
// Notion data source of that ID (dataSourceMention). One of no such source
// stays the link, its words kept, at the address a link to it kept as a link
// reads at (notionAppPathHere): the app's address for an ID of no UUID's
// form, /p/ and the ID for a UUID, as Notion's MCP, reading as its public
// API's "v2" does, links it at its app's address (seen 2026-11-21, "v1" and
// "v2": [f](collection://x) https://app.notion.com/, [f](collection://<UUID>)
// https://app.notion.com/p/<hex>). Before 2026-11-21 it was the mention of a
// source that is not there, which leaves no text, the words gone.

// dataSourceLinkMention points a data source's mention here, in place, and
// tells whether it changed (dataSourceMention); one the MCP's writer made of
// a link, which keeps the link's words and address (span.Link), names the
// source here or the one a move made, or else turns back into that link.
func (here *notionAddressLinksHere) dataSourceLinkMention(span *graphTextSpan, written string) (bool, error) {
	link := span.Link
	if link == "" {
		return here.dataSourceMention(span, written)
	}
	span.Link = ""
	if moved, err := here.dataSourceMention(span, written); err != nil || moved {
		return true, err
	}
	if found, err := here.dataSourceHere(written); err != nil || found {
		return true, err
	}
	kept, named := notionMarkdownAppAddress(link)
	if !named {
		kept = link
	}
	pointed, err := here.address(kept)
	span.Mention, span.DatabaseID, span.Link = "", "", pointed
	return true, err
}

// dataSourceHere is whether an ID names one of the workspace's data sources,
// by its public API ID or its own, one in the trash too (dataSourceDatabase).
func (here *notionAddressLinksHere) dataSourceHere(written string) (bool, error) {
	id := strings.TrimSpace(written)
	if internal, err := resolveNotionDataSourceInternalID(here.app, here.workspaceID, id); err == nil {
		id = internal
	} else if !errors.Is(err, errInvalidNotionObjectIdentity) {
		return false, err
	}
	_, _, found, err := dataSourceDatabase(here.app, here.workspaceID, id)
	return found, err
}
