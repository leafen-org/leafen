package app

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Every MCP argument that names a page, a database, a data source or a view
// reads it here, one way for all of them: its ID, with its dashes or
// without them, the public API's ID of it, its address here, or one of
// Notion's addresses of it (mcpReferenceID, mcpLocalID).

// mcpReferenceID reads the ID a reference names, as written: an ID; a
// /pages/ link as fetch and Markdown write one; a page's web address here,
// /p/ID or /p/Title-ID as the public API names a page (notionPageObjectURL),
// from any origin; or one of Notion's own (notion.so, app.notion.com, a
// notion.site: /ID, /Title-ID, /workspace/Title-ID, /p/ID), as Notion's
// tools read a page's address (seen 2026-11-22: notion-search's page_url
// took https://www.notion.so/Title-ID, https://app.notion.com/p/ID and the
// ID with or without its dashes alike). collection://, view:// and
// database:// URLs stay as they are.
func mcpReferenceID(raw string) string {
	part, slugged := mcpReferencePart(raw)
	if slugged {
		return mcpSlugAddressID(part)
	}
	return part
}

// mcpReferencePart is what mcpReferenceID reads before it takes a slugged
// address's title off, and whether it may (mcpSlugAddressID): an ID alone
// or after /p/ or /pages/ may be Title-ID.
func mcpReferencePart(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "://") && !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return raw, false
	}
	// A template's older address is its database's page with the template
	// open; it names the template (mcpPageAddress).
	if _, query, found := strings.Cut(raw, "?"); found {
		if values, err := url.ParseQuery(query); err == nil && values.Get(mcpTemplateAddressParam) != "" {
			return values.Get(mcpTemplateAddressParam), false
		}
	}
	for _, prefix := range []string{mcpPageURLPrefix, "/p/"} {
		if at := strings.LastIndex(raw, prefix); at >= 0 {
			id := raw[at+len(prefix):]
			if end := strings.IndexAny(id, "?#"); end >= 0 {
				id = id[:end]
			}
			return strings.TrimRight(id, "/"), true
		}
	}
	if parsed, err := url.Parse(raw); err == nil && notionWebAddress(parsed) {
		if pageID, _, named := notionAddressPageIDs(strings.TrimRight(parsed.Path, "/"), "", true); named {
			return pageID, false
		}
	}
	return raw, true
}

// mcpSlugAddressPattern is an address's last part that names a page after
// its title: Title-, then its ID's 32 hexadecimal digits.
var mcpSlugAddressPattern = regexp.MustCompile(`^.+-([0-9a-fA-F]{32})$`)

// mcpSlugAddressID is the ID an address's last part names: after its
// title, Title-ID; as written, any other.
func mcpSlugAddressID(part string) string {
	if match := mcpSlugAddressPattern.FindStringSubmatch(part); match != nil {
		return match[1]
	}
	return part
}

// mcpLocalID reads an ID as mcpReferenceID does, and an ID the public API
// gave — a page's, a database page's, a data source's or a database's UUID,
// with or without dashes — as the MCP tools name the same object, as
// Notion's hosted MCP takes its API's IDs (2026-11-03 audit: they were
// refused as "this account can't open it"). A database's, data source's or
// view's own ID written without its dashes names it too: fetch writes a database's
// address so, /p/ and 32 hexadecimal digits (notionMentionURL), and fetch,
// create-view and the rest refused the addresses fetch gave as "can't open
// it" (2026-11-13). Any other ID stays as it is.
func mcpLocalID(app core.App, raw string) string {
	if own, ok := mcpOwnSlugShapedID(app, raw); ok {
		return own
	}
	id := mcpReferenceID(raw)
	// A collection:// URL may carry a data source's API ID.
	if dataSourceID, ok := parseMCPDataSourceURL(id); ok {
		if identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityDataSource, dataSourceID); err == nil {
			return mcpDataSourceURLPrefix + identity.InternalID
		}
		if dashed, ok := normalizeNotionUUIDRequest(dataSourceID); ok && dashed != dataSourceID {
			if _, err := loadMCPDataSourceLocationToRead(app, dashed); err == nil {
				return mcpDataSourceURLPrefix + dashed
			}
		}
		return id
	}
	if viewID, viewed := strings.CutPrefix(id, mcpViewURLPrefix); viewed {
		if dashed, ok := normalizeNotionUUIDRequest(viewID); ok && dashed != viewID {
			if _, err := loadMCPViewLocationToRead(app, dashed); err == nil {
				return mcpViewURLPrefix + dashed
			}
		}
		return id
	}
	dashed, ok := normalizeNotionUUIDRequest(id)
	if !ok {
		return id
	}
	if pageID, err := resolveNotionPageRecordID(app, id); err == nil {
		return pageID
	}
	if identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityRowPage, id); err == nil {
		return identity.InternalID
	}
	if identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityDataSource, id); err == nil {
		return mcpDataSourceURLPrefix + identity.InternalID
	}
	if identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityDatabase, id); err == nil {
		return identity.InternalID
	}
	if dashed != id {
		if mcpDatabaseContainerExistsToRead(app, dashed) {
			return dashed
		}
		if _, err := loadMCPDataSourceLocationToRead(app, dashed); err == nil {
			return mcpDataSourceURLPrefix + dashed
		}
		if _, err := loadMCPViewLocationToRead(app, dashed); err == nil {
			return dashed
		}
	}
	return id
}
