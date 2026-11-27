package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A link that Markdown written through the MCP tools or the public API
// makes to one of Notion's web addresses naming a page by its ID
// (notion.so, www.notion.so, app.notion.com, notion.site;
// notionMarkdownAppAddress) points at this server only when something in
// the page's workspace answers to that ID (user 2026-11-21, public issue
// 1: a workspace moved in part links to the pages left in Notion):
//   - a page, a database page or a database of the workspace by the ID the
//     app and its API name it by, kept at the app's address, /p/ and the
//     ID's 32 hexadecimal digits (notionAPITextLink reads it with this
//     server's address before it);
//   - else what a Notion move into the workspace made from that Notion page,
//     database page or database (its ID map, and its migration's ledger once
//     the map is retired; notion_address_moved_ledger.go), at that page's own address,
//     /p/<page ID>, a database in a page at its block — and the anchor's
//     block at the block the move made of it — as the move's own links
//     point at what it brought (notionImportReferenceResolver.resolveLink).
//
// A link to an ID nothing here answers to keeps its address as written,
// https before one written without a scheme, and opens the page in Notion.
// A link to a path in the app naming a page by an ID — Notion's path, which
// the Markdown readers keep at the app's address (/p/ID), or Notion's form of
// a page link a JSON write takes (/ID, kept as /p/ID; notionTextLinkRequest)
// — stays as it is, unless nothing here answers to the ID and a Notion move
// made something from it: then it points at that, as a web address does.
// The Markdown readers keep a web address as written
// (notionMarkdownWrittenAddress); the writes' boundaries point it here before
// the write, in the blocks' text, table cells and captions alike: the MCP
// tools' Markdown (narrowedMarkdownMentionInput), and every public API write,
// JSON and Markdown alike — a page's content, a block appended or changed, a
// database page's body (prepareNotionWriteTargetsInput), a name, title or
// value (notionSpansHere) and a comment (notionCommentGraphSpansHere,
// notionCommentMarkdownSpans), and an MCP comment's rich text
// (mcpCommentRichTextSpans). A page's, a database's or a data source's
// mention there is pointed by the same rule (notion_address_mentions_here.go).

// notionMarkdownWrittenAddress is the link a Markdown reader keeps for one
// of Notion's addresses naming a page, a database, a data source or a block
// by its ID (notionMarkdownAppAddress), before the write knows its
// workspace: a path in Notion's app, a data source's or a view's address at
// the app's address, as notionMarkdownAppAddress keeps it; a web address of
// Notion's as written (notionMarkdownNotionWebAddress), which the write
// points here when its ID answers here (resolveNotionMarkdownWorkspaceTargets).
func notionMarkdownWrittenAddress(raw string) (string, bool) {
	kept, named := notionMarkdownAppAddress(raw)
	if !named {
		return "", false
	}
	if web := notionMarkdownNotionWebAddress(raw); web != "" {
		return web, true
	}
	return kept, true
}

// notionMarkdownNotionWebAddress is one of Notion's web addresses as a link
// keeps it — its scheme https where it has none, as notionMarkdownAppAddress
// reads one — or "" for a path, a data source's or a view's address, or any
// other.
func notionMarkdownNotionWebAddress(raw string) string {
	address := strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(address, notionMarkdownDataSourceAddress) || strings.HasPrefix(address, notionMarkdownViewAddress):
		return ""
	case strings.HasPrefix(address, "//"):
		address = "https:" + address
	case strings.HasPrefix(address, "/"):
		return ""
	case !notionLinkSchemePattern.MatchString(address):
		address = "https://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil || !notionWebAddress(parsed) {
		return ""
	}
	return address
}

// resolveNotionMarkdownWorkspaceTargets resolves what Markdown written through
// the MCP tools names in the page's workspace before its mentions are
// prepared: its callouts' custom emoji (resolveNotionMarkdownCalloutIcons),
// and its links and mentions (notionWriteTargetsHere).
func resolveNotionMarkdownWorkspaceTargets(
	app core.App, workspaceID string, updates []graphTextUpdate, operations []graphOperation,
) error {
	if err := resolveNotionMarkdownCalloutIcons(app, workspaceID, operations); err != nil {
		return err
	}
	return notionWriteTargetsHere(app, workspaceID, updates, operations)
}

// notionWriteTargetsHere points the links and the page, database and data
// source mentions a write holds here, in its text (updates) and its blocks'
// table cells and captions (notionAddressLinksHere).
func notionWriteTargetsHere(app core.App, workspaceID string, updates []graphTextUpdate, operations []graphOperation) error {
	here := newNotionAddressLinksHere(app, workspaceID)
	for index := range updates {
		if _, err := here.spans(updates[index].Spans); err != nil {
			return err
		}
	}
	return here.operations(operations)
}

// notionAddressLinksHere points the links to Notion's addresses a write
// holds at what answers to their IDs in its workspace (notionAddressHere),
// and its page, database and data source mentions (mention), each address
// and ID looked up once.
type notionAddressLinksHere struct {
	app         core.App
	workspaceID string
	links       map[string]string
	mentioned   map[string]notionMentionedHere
	sources     map[string]string
}

func newNotionAddressLinksHere(app core.App, workspaceID string) *notionAddressLinksHere {
	return &notionAddressLinksHere{
		app: app, workspaceID: workspaceID, links: map[string]string{}, mentioned: map[string]notionMentionedHere{},
		sources: map[string]string{},
	}
}

// spans points the spans' links and mentions here, in place, and tells
// whether any changed.
func (here *notionAddressLinksHere) spans(spans []graphTextSpan) (bool, error) {
	changed := false
	for index := range spans {
		if spans[index].Mention != "" {
			pointed, err := here.mention(&spans[index])
			if err != nil {
				return false, err
			}
			changed = changed || pointed
			continue
		}
		link := spans[index].Link
		if link == "" {
			continue
		}
		pointed, err := here.address(link)
		if err != nil {
			return false, err
		}
		if pointed != link {
			spans[index].Link, changed = pointed, true
		}
	}
	return changed, nil
}

// address is the link a write keeps for a link (notionAddressHere).
func (here *notionAddressLinksHere) address(link string) (string, error) {
	if pointed, found := here.links[link]; found {
		return pointed, nil
	}
	pointed, err := notionAddressHere(here.app, here.workspaceID, link)
	if err == nil {
		here.links[link] = pointed
	}
	return pointed, err
}

// operations points the links and mentions in the table cells and captions
// of the blocks a write upserts here, its link_to_page blocks' pages and
// databases (linkBlock) and its synced blocks' originals (syncedBlock).
func (here *notionAddressLinksHere) operations(operations []graphOperation) error {
	for index := range operations {
		block := operations[index].Block
		if operations[index].Kind != graphOperationKindUpsertBlock || block == nil || len(block.Content) == 0 {
			continue
		}
		if block.Type == graphBlockTypeLinkToPage || block.Type == graphBlockTypeSyncedBlock {
			pointTo := here.linkBlock
			if block.Type == graphBlockTypeSyncedBlock {
				pointTo = here.syncedBlock
			}
			if _, err := pointTo(block); err != nil {
				return err
			}
			continue
		}
		if block.Type == graphBlockTypeTableRow {
			row, err := parseGraphTableRowBlockContent(block.Content)
			if err != nil {
				continue
			}
			changed := false
			for cell := range row.Cells {
				pointed, err := here.spans(row.Cells[cell])
				if err != nil {
					return err
				}
				changed = changed || pointed
			}
			if changed {
				if block.Content, err = json.Marshal(row); err != nil {
					return err
				}
			}
			continue
		}
		fields := map[string]json.RawMessage{}
		caption := []graphTextSpan{}
		if json.Unmarshal(block.Content, &fields) != nil || len(fields["caption"]) == 0 ||
			json.Unmarshal(fields["caption"], &caption) != nil {
			continue
		}
		changed, err := here.spans(caption)
		if err != nil {
			return err
		}
		if !changed {
			continue
		}
		if fields["caption"], err = json.Marshal(caption); err != nil {
			return err
		}
		if block.Content, err = json.Marshal(fields); err != nil {
			return err
		}
	}
	return nil
}

// notionAddressHere is the link a write keeps for a link: for one of
// Notion's web addresses (notionMarkdownNotionWebAddress), the app's address
// of what answers to its ID in the workspace, or of what a Notion move made
// from it there (notionMovedAddress), or else the address itself; for a path
// in the app, notionAppPathHere; any other as it is.
func notionAddressHere(app core.App, workspaceID, address string) (string, error) {
	if notionMarkdownNotionWebAddress(address) == "" {
		return notionAppPathHere(app, workspaceID, address)
	}
	kept, id, anchor := notionAddressNamedIDs(address)
	if id == "" {
		return address, nil
	}
	here, err := notionIDInWorkspace(app, workspaceID, id)
	if err != nil || here {
		return kept, err
	}
	moved, err := notionMovedAddress(app, workspaceID, id, anchor)
	if err != nil || moved != "" {
		return moved, err
	}
	return address, nil
}

// notionAddressNamedIDs are the app's address one of Notion's addresses
// stands for (notionMarkdownAppAddress), the ID of the page it names, dashed,
// and of the block its anchor names; id is "" for one naming no page by ID.
func notionAddressNamedIDs(address string) (kept, id, anchor string) {
	kept, named := notionMarkdownAppAddress(address)
	if !named {
		return "", "", ""
	}
	pageHex, blockHex, _ := strings.Cut(strings.TrimPrefix(kept, "/p/"), "#")
	return kept, notionLinkedID(pageHex), notionLinkedID(blockHex)
}

// notionAppPathHere is the link a write keeps for a path in the app naming a
// page by an ID, a UUID or its 32 hexadecimal digits maybe after a title
// (/p/ID, and a block after #, #block- or none): as it is where something here
// answers to the ID, or nothing a Notion move made does; else the address of
// what the move made (notionMovedAddress), a data source's or a view's too,
// which a link to collection://ID or view://ID reads as
// (notionMovedDataSourcePath). A data source or a view here by its own ID,
// and an address of one by an ID of the app's own form, reads at its public
// API ID's address (notionOwnSourceLinkHere). Any other link is kept as it is.
func notionAppPathHere(app core.App, workspaceID, link string) (string, error) {
	if pointed, own, err := notionOwnSourceLinkHere(app, workspaceID, link); own || err != nil {
		return pointed, err
	}
	path, fragment, _ := strings.Cut(link, "#")
	ref, named := strings.CutPrefix(path, "/p/")
	id := notionLinkedID(ref)
	if !named || id == "" {
		return link, nil
	}
	here, err := notionIDInWorkspace(app, workspaceID, id)
	if err != nil || here {
		return link, err
	}
	// A data source or a view here by its own ID reads at its public API ID's
	// address (notion_address_own_sources_here.go).
	if own, err := notionOwnSourcePath(app, workspaceID, id, true, true); err != nil || own != "" {
		return own, err
	}
	moved, err := notionMovedAddress(app, workspaceID, id, notionLinkedID(strings.TrimPrefix(fragment, "block-")))
	if err != nil || moved != "" {
		return moved, err
	}
	return notionMovedDataSourcePath(app, workspaceID, id, link)
}

// notionIDInWorkspace is whether an ID names a page, a database page or a
// database of the workspace, as the app's address of it (/p/ID) opens it.
func notionIDInWorkspace(app core.App, workspaceID, id string) (bool, error) {
	notFound := func(err error) bool {
		return errors.Is(err, errInvalidNotionObjectIdentity) || errors.Is(err, errInvalidNotionPageIdentity) ||
			errors.Is(err, sql.ErrNoRows)
	}
	if row, err := resolveNotionObjectIdentity(app, notionObjectIdentityRowPage, id); err == nil {
		if row.Workspace == workspaceID {
			return true, nil
		}
	} else if !notFound(err) {
		return false, err
	}
	if pageID, err := resolveNotionPageRecordID(app, id); err == nil {
		workspace := ""
		err := app.DB().NewQuery(`SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": pageID}).Row(&workspace)
		if err != nil && !notFound(err) {
			return false, err
		}
		if workspace == workspaceID {
			return true, nil
		}
	} else if !notFound(err) {
		return false, err
	}
	databasePage, err := mcpLinkedDatabasePage(app, workspaceID, id)
	return databasePage != "", err
}

// notionMovedAddress is the app's address of what a Notion move into the
// workspace made from the Notion object an ID names — a page, a database
// page, a full-page database by its page, a database in a page by its own
// block's ID, which opens it alone as Notion's link to it does (seen
// 2026-11-22; page_links.go) — the anchor's block at the block made of it;
// "" for none.
func notionMovedAddress(app core.App, workspaceID, id, anchor string) (string, error) {
	mapped, err := notionMovedMappings(app, workspaceID, id, anchor)
	if err != nil {
		return "", err
	}
	root, database := notionDatabaseRootPageSourceID(id), id+":block"
	address, rank, anchorBlock := "", 0, ""
	for _, item := range mapped {
		// A page or database page first, then a full-page database's page,
		// then a database in a page.
		order := map[string]int{id: 3, root: 2, database: 1}[item.SourceID]
		if item.SourceID == anchor && item.Kind == "block" {
			if anchorBlock == "" {
				anchorBlock = item.BlockID
			}
			continue
		}
		if order <= rank {
			continue
		}
		address, rank = "/p/"+item.PageID, order
		if item.Kind == "block" {
			address = "/p/" + item.BlockID
		}
	}
	// Not at the page itself, though: Notion's address of a full-page
	// database with its own ID after # opens it unchosen (block_address_anchor.go).
	if address != "" && anchorBlock != "" && rank > 1 && !blockIsPageItself(app, strings.TrimPrefix(address, "/p/"), anchorBlock) {
		address += "#block-" + anchorBlock
	}
	return address, nil
}

// notionMovedMapping is one thing a Notion move into the workspace made from
// a Notion object (its ID map): the Notion object it came from, its kind in
// the map, the page here it is or stands on, and a block's own ID.
type notionMovedMapping struct {
	SourceID string `db:"source_id"`
	Kind     string `db:"kind"`
	PageID   string `db:"page_id"`
	BlockID  string `db:"block_id"`
}

// notionMovedMappings are what Notion moves into the workspace made, and
// keep here, from the Notion object an ID names — a page or a database page,
// a full-page database's page and a database's block by the database's ID —
// and from the block anchor names, the latest move's first: by the jobs' ID
// maps and the migrations' ledgers, which keep them after the jobs' records
// are retired (notion_address_moved_ledger.go).
func notionMovedMappings(app core.App, workspaceID, id, anchor string) ([]notionMovedMapping, error) {
	root, database := notionDatabaseRootPageSourceID(id), id+":block"
	mapped := []notionMovedMapping{}
	err := app.DB().NewQuery(`
		SELECT [[made]].[[source_id]], [[made]].[[kind]],
			COALESCE([[page]].[[id]], [[block]].[[page_id]], '') AS [[page_id]],
			CASE WHEN [[made]].[[kind]] = 'block' THEN [[made]].[[native_id]] ELSE '' END AS [[block_id]]
		FROM ` + notionMovedMadeSQL("{:id}, {:root}, {:database}, {:anchor}") + ` AS [[made]]
		LEFT JOIN {{leafen_pages}} AS [[page]] ON [[made]].[[kind]] IN ('page', 'row')
			AND [[page]].[[id]] = [[made]].[[native_id]] AND [[page]].[[workspace]] = {:workspaceId}
		LEFT JOIN {{leafen_blocks}} AS [[block]] ON [[made]].[[kind]] = 'block'
			AND [[block]].[[id]] = [[made]].[[native_id]]
		WHERE [[made]].[[source_id]] = {:id} AND [[made]].[[kind]] IN ('page', 'row')
			OR [[made]].[[source_id]] = {:root} AND [[made]].[[kind]] = 'page'
			OR [[made]].[[source_id]] IN ({:database}, {:anchor}) AND [[made]].[[kind]] = 'block'
		ORDER BY [[made]].[[made_at]] DESC
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "id": id, "root": root, "database": database, "anchor": anchor,
	}).All(&mapped)
	// One whose page has gone from the workspace names nothing here.
	return slices.DeleteFunc(mapped, func(item notionMovedMapping) bool { return item.PageID == "" }), err
}
