package app

import (
	"encoding/json"
	"errors"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page's Markdown writes a link to a page as the page or database it
// leads to, its address and its name, as the screen shows the block:
// <link-to-page url="…">Title</link-to-page>, type="database" after the
// address for a database's. Where a mention of the target reads by its
// address alone — one gone, one the reader can't open, one in the trash in
// "v2" (notionMarkdownMentionText) — so does the link, <link-to-page url="…" />.
// Every form writes it so — the MCP's fetch and leafen_fetch, the public
// API's "v1" ("/>" closing it, as "v1" closes its tags) and "v2", and
// Leafen's own Markdown — and every write reads it back into a link to that
// page or database: as fetch gave it, it is the block it was, kept in its
// place or moved with its line; a new line, or one naming another target,
// makes a link to a page or database of the page's workspace the writer
// can open, and any other address is refused. Its name is read as nothing:
// a link always shows its target's name now.
//
// Notion's MCP and public API Markdown write the block as unknown,
// <unknown url="…#block" alt="alias" />, naming only the block itself, and
// read <link-to-page> as text (seen 2026-11-21, 2026-11-22 and, the public
// API's "v1" and "v2", 2026-11-27); so an agent could neither tell where a
// link leads nor make one, where a person sees its target and picks it (the
// operating loop's principle 5, a deliberate difference). A link to a
// comment, or one naming nothing yet, is still written as unknown. A link a
// wiki marks as its page's is that page's <page> tag
// (notion_page_markdown_wiki_pages.go).

var notionMarkdownLinkToPagePattern = regexp.MustCompile(
	`^<link-to-page((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*(?:/>|>(.*)</link-to-page>)$`)

// A link line's address, and its type="database", until the write finds
// what they name (notionMarkdownLinkTargets).
const (
	notionMarkdownLinkAddressKey = "markdownLinkAddress"
	notionMarkdownLinkKindKey    = "markdownLinkKind"
)

// linkToPage is a link block's line in its reader's form, false for one
// naming no page or database it can write.
func (w *notionMarkdownWriter) linkToPage(block workspaceExportMarkdownBlock) (string, bool) {
	if block.Type != graphBlockTypeLinkToPage {
		return "", false
	}
	address, title, titled, database, ok := w.mentions.linkTarget(block.Content)
	if !ok {
		return "", false
	}
	escaped, closing := notionMarkdownAttributeEscaped.Replace, " />"
	if w.mentions.APIForm && !w.mentions.Version2 {
		escaped, closing = notionMarkdownAPIAttributeEscaped.Replace, "/>"
	}
	opening := `<link-to-page url="` + escaped(address) + `"`
	if database {
		opening += ` type="database"`
	}
	if !titled {
		return opening + closing, true
	}
	return opening + ">" + notionMarkdownEscaped.Replace(title) + "</link-to-page>", true
}

// notionMarkdownUnknownAlt is the kind an <unknown> line names a block by,
// as Notion's: a link to a page is its "alias" (seen 2026-11-10).
func notionMarkdownUnknownAlt(blockType string) string {
	if blockType == graphBlockTypeLinkToPage {
		return "alias"
	}
	return blockType
}

// linkTarget is what a link block leads to as its reader's mention of it
// reads: its address, and its name where the mention reads one.
func (mentions notionMarkdownMentions) linkTarget(content map[string]any) (address, title string, titled, database, ok bool) {
	pageID := strings.TrimSpace(mapString(content, "childPageId"))
	if pageID == "" {
		pageID = strings.TrimSpace(mapString(content, "pageId"))
	}
	if pageID != "" && mentions.Page != nil {
		address, ok = mentions.Page(pageID)
		if ok && mentions.titled(graphTextSpan{Mention: "page", PageID: pageID}) {
			return address, mentions.PageTitle(pageID), true, false, true
		}
		if !ok && mentions.Unnamed != nil {
			address, ok = mentions.Unnamed("page", pageID)
		}
		return address, "", false, false, ok && address != ""
	}
	databaseID := strings.TrimSpace(mapString(content, "databaseContainerId"))
	if pageID != "" || databaseID == "" || mentions.Database == nil {
		return "", "", false, false, false
	}
	address, ok = mentions.Database(databaseID)
	if ok && mentions.DatabaseTitle != nil && !mentions.inTrash(graphTextSpan{Mention: "database", DatabaseID: databaseID}) {
		return address, mentions.DatabaseTitle(databaseID), true, true, true
	}
	if !ok && mentions.Unnamed != nil {
		address, ok = mentions.Unnamed("database", databaseID)
	}
	return address, "", false, true, ok && address != ""
}

// notionMarkdownLinkToPageLine reads a link line into a link block holding
// its address until the write finds its target.
func notionMarkdownLinkToPageLine(trimmed string) (string, json.RawMessage, bool) {
	match := notionMarkdownLinkToPagePattern.FindStringSubmatch(trimmed)
	if match == nil {
		return "", nil, false
	}
	attributes := notionMarkdownAttributes(match[1])
	address := strings.TrimSpace(html.UnescapeString(attributes["url"]))
	if address == "" {
		return "", nil, false
	}
	content := map[string]string{notionMarkdownLinkAddressKey: address}
	if attributes["type"] == "database" {
		content[notionMarkdownLinkKindKey] = "database"
	}
	encoded, err := json.Marshal(content)
	return graphBlockTypeLinkToPage, encoded, err == nil
}

// notionMarkdownAddressedBlock tells a block whose target its write finds:
// a synced block reference's original, a link's page or database, or the
// button a button's line names.
func notionMarkdownAddressedBlock(block *graphBlock) bool {
	return block != nil && (block.Type == graphBlockTypeSyncedBlock &&
		strings.Contains(string(block.Content), notionMarkdownSyncedAddressKey) ||
		block.Type == graphBlockTypeLinkToPage && strings.Contains(string(block.Content), notionMarkdownLinkAddressKey) ||
		block.Type == graphBlockTypeButton && strings.Contains(string(block.Content), notionMarkdownButtonAddressKey))
}

// addressedBlocks points the blocks a write's Markdown names by address at
// what they name: synced block references at their originals, links at
// their pages and databases, and buttons at what they were.
func (request pageMarkdownReplacement) addressedBlocks(app core.App, current pageAuthorization, doc *crdt.Doc, operations []graphOperation) error {
	if err := notionMarkdownSyncedReferences(app, request.PrincipalID, current, request.Allows, request.Reader.Origin,
		request.Version2 || request.Reader.APIForm, request.CreatesPage, operations); err != nil {
		return err
	}
	if err := notionMarkdownButtonWrites(doc, operations); err != nil {
		return err
	}
	return notionMarkdownLinkTargets(app, request.PrincipalID, current, request.Reader, operations)
}

// notionMarkdownLinkTargets gives each link a write makes from a line the
// page or database its address names, one of the page's workspace its
// writer and reader may open; any other is refused.
func notionMarkdownLinkTargets(
	app core.App, principalID string, current pageAuthorization, reader notionMarkdownReader, operations []graphOperation,
) error {
	allows := func(page pageAuthorization) bool {
		if reader.Readable == nil {
			return true
		}
		open, err := reader.Readable(app, page.Page.Id)
		return open && err == nil
	}
	for _, operation := range operations {
		block := operation.Block
		if operation.Kind != graphOperationKindUpsertBlock || block == nil || block.Type != graphBlockTypeLinkToPage {
			continue
		}
		content := map[string]any{}
		if json.Unmarshal(block.Content, &content) != nil {
			continue
		}
		address, addressed := content[notionMarkdownLinkAddressKey].(string)
		if !addressed {
			continue
		}
		kind, _ := content[notionMarkdownLinkKindKey].(string)
		target, err := notionMarkdownLinkBlockTarget(app, principalID, current, reader.Origin, address, kind == "database", allows)
		if err != nil {
			return err
		}
		if target == nil {
			return graphInputError(errorCodeGraphReferenceInvalid, "<link-to-page> names no page or database of this "+
				"workspace that this can open: "+address+". Give the address fetch or search gives one.")
		}
		if block.Content, err = json.Marshal(target); err != nil {
			return err
		}
	}
	return nil
}

// notionMarkdownLinkBlockTarget is a link's content leading where an address
// names, nil for nothing the writer may link to: a page by its own ID, or a
// database by its own (a full-page one's page's address names it too, as
// the editor's picker links it).
func notionMarkdownLinkBlockTarget(
	app core.App, principalID string, current pageAuthorization, origin, address string, database bool,
	allows func(pageAuthorization) bool,
) (map[string]any, error) {
	id, err := notionMarkdownLinkAddressID(app, current.Workspace.WorkspaceID, origin, address)
	if err != nil || id == "" {
		return nil, err
	}
	if pageID := notionMarkdownLinkPageID(app, id, database); pageID != "" {
		containerID := mcpPageAddressDatabaseContainer(app, pageID)
		if containerID == "" {
			_, allowed, err := authorizePageMentionTarget(
				app, principalID, current.Workspace.OwnerID, current.Workspace.WorkspaceID, pageID, allows)
			if err != nil || !allowed {
				return nil, err
			}
			return map[string]any{"childPageId": pageID}, nil
		}
		id = containerID
	}
	containerID, err := notionMarkdownLinkDatabaseID(app, current, id)
	if err != nil || containerID == "" {
		return nil, err
	}
	stored, found, err := loadNotionMentionedDatabase(app, containerID)
	if err != nil || !found || stored.Trashed || stored.WorkspaceID != current.Workspace.WorkspaceID {
		return nil, err
	}
	_, allowed, err := authorizePageMentionTarget(
		app, principalID, current.Workspace.OwnerID, current.Workspace.WorkspaceID, stored.PageID, allows)
	if err != nil || !allowed {
		return nil, err
	}
	return map[string]any{"databaseContainerId": containerID}, nil
}

// notionMarkdownLinkAddressID is the ID a link's address names: this
// server's /p/ or /pages/ address, as fetch writes it, or a path of one;
// one of Notion's web addresses naming what is here, or what a Notion move
// made here of it (notionAddressHere); or an ID alone. "" for any other.
func notionMarkdownLinkAddressID(app core.App, workspaceID, origin, address string) (string, error) {
	if notionMarkdownNotionWebAddress(address) != "" {
		pointed, err := notionAddressHere(app, workspaceID, address)
		if err != nil || pointed == address {
			return "", err
		}
		address = pointed
	}
	if !strings.ContainsAny(address, "/:") {
		address = "/p/" + address
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return "", nil
	}
	if parsed.Host != "" || parsed.Scheme != "" {
		here, err := url.Parse(origin)
		if err != nil || origin == "" || !strings.EqualFold(parsed.Host, here.Host) ||
			parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", nil
		}
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	for _, prefix := range []string{"/p/", "/pages/"} {
		if id, named := strings.CutPrefix(path, prefix); named && id != "" && !strings.Contains(id, "/") {
			return id, nil
		}
	}
	return "", nil
}

// notionMarkdownLinkPageID is the page an address's ID names: by its own ID
// or the public API's, a database page by its stored public ID too; "" for
// none, and for a database's link.
func notionMarkdownLinkPageID(app core.App, id string, database bool) string {
	if database {
		return ""
	}
	if pageID, _ := mcpInternalPageID("/p/" + id); pageID != "" {
		if _, _, there := pageMentionTargetNow(app, pageID); there {
			return pageID
		}
	}
	public := notionLinkedID(id)
	if public == "" {
		return ""
	}
	if pageID, found := resolveNotionPageMentionID(app, notionPageMentionRowPrefix+public); found {
		if _, _, there := pageMentionTargetNow(app, pageID); there {
			return pageID
		}
	}
	return ""
}

// notionMarkdownLinkDatabaseID is the database an address's ID names in the
// page's workspace — by the public API's ID or its own, with its dashes or
// without, or a full-page one by its page's — "" for none.
func notionMarkdownLinkDatabaseID(app core.App, current pageAuthorization, id string) (string, error) {
	databaseID := id
	if dashed, uuid := normalizeNotionUUIDRequest(id); uuid {
		databaseID = dashed
	}
	identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityDatabase, databaseID)
	switch {
	case err == nil && identity.Workspace == current.Workspace.WorkspaceID:
		return identity.InternalID, nil
	case err != nil && !errors.Is(err, errInvalidNotionObjectIdentity):
		return "", err
	}
	if containerID := mcpPageAddressDatabaseContainer(app, databaseID); containerID != "" {
		return containerID, nil
	}
	for _, candidate := range []string{databaseID, id} {
		if _, found, err := loadNotionMentionedDatabase(app, candidate); err != nil || found {
			return candidate, err
		}
	}
	return "", nil
}
