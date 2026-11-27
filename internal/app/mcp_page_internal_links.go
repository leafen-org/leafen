package app

import (
	"database/sql"
	"errors"
	"net/url"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Notion's MCP makes a link to one of the workspace's pages a mention of the
// page when it creates pages, unless asked to keep internal links (seen
// 2026-09-30): [words](page address) becomes the page's mention and its
// words go, [address](address) and <address> too (seen 2026-11-11). One to a
// block on such a page (#block-ID, or Notion's #ID) becomes the block's
// mention, named by its words, one to a database the database's mention,
// and one to a data source (collection://ID) the source's (seen 2026-11-11).
// A link to one of Notion's web addresses of its pages — app.notion.com,
// notion.so, a notion.site, the title before the ID, [address](address) —
// is so too, its words gone (seen 2026-11-21): here it is the mention of
// what its ID names here or what a Notion move made from it, as that address
// written alone is (notionAddressMention), and for an ID nothing answers to
// the link it was. Kept, such a link points here when its ID
// answers here (notion_address_links_here.go), as Notion's keeps it at its
// page's address. An address of this server's or of Notion's written alone
// is a mention either way, as everywhere the MCP reads Markdown
// (notionMarkdownSitePages). Leafen's MCP names pages at /pages/<id>, and its
// app at /p/<id>. A link to anything that isn't a page or a database the
// writer can read stays a link.

// mcpInternalPageID is the page an address names, or "", and the block its
// anchor names on it, or "": by its ID, or by the public ID the API names it
// by (/p/ID, maybe after its title), which the app opens too. An ID holds
// letters, digits, - and _ alone; a path with any other character after
// them names no page, as Notion's MCP reads its own address: the old tag
// `<mention-page url="…/p/ID"/>` in a title it keeps as text, the address
// standing bare in it running to "…/p/ID\"/", a link to that address and no
// mention (seen 2026-11-16), while "…/p/ID\"" ends before its quote and
// mentions the page.
func mcpInternalPageID(address string) (string, string) {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return "", ""
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	for _, prefix := range []string{"/pages/", "/p/"} {
		if id, named := strings.CutPrefix(path, prefix); named && id != "" && !strings.Contains(id, "/") {
			if public := notionLinkedID(id); public != "" {
				if own, err := resolvePageOwnPublicID(public); err == nil {
					id = own
				}
			} else if !workspaceImportReferenceIDPattern.MatchString(id) {
				return "", ""
			}
			return id, mcpLinkedBlockID(parsed.Fragment)
		}
	}
	return "", ""
}

// mcpLinkedBlockID is the block an address's anchor names: the app's
// (block-ID) or Notion's (the ID, with or without dashes); "" for none.
func mcpLinkedBlockID(fragment string) string {
	if own, found := strings.CutPrefix(fragment, "block-"); found && workspaceImportReferenceIDPattern.MatchString(own) {
		return own
	}
	if notionAddressBlockPattern.MatchString(fragment) {
		return notionLinkedID(fragment)
	}
	return ""
}

// mentionMCPInternalLinks turns the text updates' links to pages and
// databases the writer can read into mentions of them, as Notion's MCP does,
// apart from the text of code blocks, and gives the links the reader left as
// written theirs (mcpLinkAsWritten): a link to one of Notion's web addresses
// the mention its address makes, which the write's boundary names here
// (notionAddressMention).
func mentionMCPInternalLinks(
	resolves func(pageID string) (bool, error),
	site *notionMarkdownSitePages,
	operations []graphOperation,
	updates []graphTextUpdate,
	preserve bool,
) error {
	code := map[string]bool{}
	for _, operation := range operations {
		if operation.Block != nil && operation.Block.Type == graphBlockTypeCode {
			code[operation.Block.ID] = true
		}
	}
	for index := range updates {
		spans := make([]graphTextSpan, 0, len(updates[index].Spans))
		for _, span := range updates[index].Spans {
			keep := preserve || code[updates[index].BlockID]
			if written, kept := mcpLinkAsWritten(span, keep); kept {
				if mention, named := site.notionAddressMention(written.Link, written); named && !keep && written.Mention == "" &&
					!written.Code && written.Equation == "" {
					written = mention
				}
				spans = append(spans, written)
				continue
			}
			if span.Mention != "" || span.Code || span.Equation != "" || code[updates[index].BlockID] {
				spans = append(spans, span)
				continue
			}
			// Only a link to this server names one of its pages: another
			// server's address stays a link, as Notion's MCP keeps one to
			// another host (seen 2026-11-11, worker w297).
			if id, blockID := mcpInternalPageID(span.Link); span.Link != "" && id != "" && !preserve && site.names(span.Link) {
				readable, err := resolves(id)
				if err != nil {
					return err
				}
				if readable {
					spans = append(spans, mcpPageMentionSpan(span, id, blockID))
					continue
				}
			}
			spans = append(spans, span)
		}
		updates[index].Spans = spans
	}
	return nil
}

// mcpLinkTargetReadable is whether the writer may mention what an internal
// link names in the workspace: a page, or a database by the page that holds
// it.
func mcpLinkTargetReadable(app core.App, authority mcpAuthority, ownerID, workspaceID string) func(string) (bool, error) {
	reaches := mcpMentionReaches(authority)
	return func(pageID string) (bool, error) {
		target, err := mcpLinkedDatabasePage(app, workspaceID, pageID)
		if err != nil {
			return false, err
		}
		if target == "" {
			target = pageID
		}
		_, allowed, err := authorizePageMentionTarget(
			app, authority.PrincipalID, ownerID, workspaceID, target,
			func(target pageAuthorization) bool { return reaches == nil || reaches(app, target) },
		)
		return allowed, err
	}
}

// mcpCreatedBodyInput writes a body notion-create-pages gives a page it made
// as any other it writes: its links to pages and databases the writer can
// read become their mentions (mentionMCPInternalLinks), kept links with
// preserve, before prepare checks its mentions — a body that makes sub-pages
// or databases, or a database page's, written after the page is made
// (seen 2026-11-11: Notion's MCP makes those links mentions too). Its code of
// nothing has no text, as a page made with Markdown (seen 2026-11-14: rich
// text [], where notion-update-page gives [""]).
func mcpCreatedBodyInput(
	authority mcpAuthority, preserve bool,
	prepare func(core.App, pageAuthorization, *crdt.Doc, graphMutationInput) (graphMutationInput, error),
) func(core.App, pageAuthorization, *crdt.Doc, graphMutationInput) (graphMutationInput, error) {
	prepare = notionCreatedBodyEmptyCodeInput(prepare)
	return func(app core.App, current pageAuthorization, doc *crdt.Doc, input graphMutationInput) (graphMutationInput, error) {
		readable := mcpLinkTargetReadable(app, authority, current.Workspace.OwnerID, current.Workspace.WorkspaceID)
		if err := mentionMCPInternalLinks(readable, mcpSitePages(authority), input.Operations, input.TextUpdates, preserve); err != nil {
			return graphMutationInput{}, err
		}
		return prepare(app, current, doc, input)
	}
}

// mcpLinkAsWritten is a span whose link the reader left as written for a
// writer that makes links mentions (notionMarkdownSitePages.LinksMentioned):
// a link to an address of Notion's or one standing bare naming nothing by
// its ID, a link at the app's address, and a link to a data source, the
// source's mention, which the writer names
// (notion_markdown_data_source_mentions.go), by Leafen's own ID too
// (notionMarkdownOwnSourceAddress) — or, kept, a link as a write keeps it
// too: at the app's address, a web address of Notion's as written, which the
// write points here when its ID answers here (notionMarkdownWrittenAddress),
// a data source's or a view's address by Leafen's own ID as written
// (notionOwnSourceLinkHere). kept is false for any other span.
func mcpLinkAsWritten(span graphTextSpan, keep bool) (graphTextSpan, bool) {
	if span.Mention != "" || span.Link == "" || strings.HasPrefix(span.Link, "/") {
		return span, false
	}
	address, named := notionMarkdownWrittenAddress(span.Link)
	own, ownForm := notionMarkdownOwnSourceAddress(span.Link)
	if !named && !ownForm {
		return span, false
	}
	// It keeps its words and address for the write, which makes it the link
	// again where nothing here is that source (dataSourceLinkMention).
	if source := strings.HasPrefix(span.Link, notionMarkdownDataSourceAddress); source && !keep && !span.Code && span.Equation == "" {
		span.Mention, span.DatabaseID = "database", span.Link
		return span, true
	}
	// One by Leafen's own ID stays as written for the write to point here.
	if span.Link = address; ownForm {
		span.Link = own
	}
	return span, true
}

// mcpLinkedDatabasePage is the page that holds the database an ID names in
// the workspace — the public API's ID for it, or Leafen's as the MCP tools'
// addresses name it (databaseAppAddress: a UUID without its dashes, any
// other ID as it is), as a link to the database names it — or "" for none,
// or one in the trash (principalDatabaseMention).
func mcpLinkedDatabasePage(app core.App, workspaceID, id string) (string, error) {
	identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityDatabase, id)
	switch {
	case err == nil && identity.Workspace != workspaceID:
		return "", nil
	case errors.Is(err, errInvalidNotionObjectIdentity):
		identity.InternalID = id
		if dashed := markdownMentionedDatabaseID(app, id); dashed != "" {
			identity.InternalID = dashed
		}
	case err != nil:
		return "", err
	}
	pageID := ""
	err = app.DB().NewQuery(`
		SELECT [[container]].[[page_id]] FROM {{leafen_database_containers}} AS [[container]]
		WHERE [[container]].[[id]] = {:id} AND [[container]].[[kind]] = 'source'
			AND [[container]].[[workspace_id]] = {:workspaceId}
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
				WHERE [[trash]].[[container_id]] = [[container]].[[id]]
			)
		LIMIT 1
	`).Bind(dbx.Params{"id": identity.InternalID, "workspaceId": workspaceID}).Row(&pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return pageID, err
}

// mcpPageMentionSpan is a page's mention in place of a span, keeping its
// style; the writer gives it the page's title. One of a block on the page is
// written with the block's ID in the page's place, which the writer anchors
// at the block's page and names by its words (anchorPageMention); one of a
// database by the database's ID, which the writer makes the database's
// mention (principalDatabaseMention).
func mcpPageMentionSpan(span graphTextSpan, pageID, blockID string) graphTextSpan {
	if blockID != "" {
		pageID = blockID
	}
	span.Link, span.Mention, span.PageID, span.Text = "", "page", pageID, pageID
	return span
}
