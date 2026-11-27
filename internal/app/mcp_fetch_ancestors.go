package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"

	"github.com/pocketbase/pocketbase/core"
)

// Notion's fetch lists what stands above a page, nearest first (seen
// 2026-09-30): a database page's data source and database, then the pages
// above them, and the blocks one stands in within the page above it, such
// as a column (mcp_fetch_ancestor_blocks.go); a full-page database's page
// is its database. The list stops at
// the first one the person can't read, and a page's path is the titles of
// its pages and databases from the top down, an untitled one by what it is
// called (mcp_untitled_names.go), where Notion's leaves it out. The
// app's path above a page reads the same walk (page_path_route.go). A
// database's line in the list names it by the name it has of its own, as
// Notion's names it by its block's own title, while its path and its fetch
// head name it by its source (seen 2026-11-21: "" for one made inline; for a
// full-page one whose source alone was renamed, the name it was made with
// over its source's new one; "" for a full-page one made without a name,
// its title typed after): the title its block keeps, none included
// (database_block_title.go), which a move brings from the reading of the
// person's Notion, else a full-page one's own name, else none.

type mcpAncestorWalk struct {
	app       core.App
	authority mcpAuthority
	seen      map[string]bool
	ancestors []mcpFetchAncestor
}

// mcpPageAncestors lists what stands above a page for a person.
func mcpPageAncestors(app core.App, authority mcpAuthority, page *core.Record) ([]mcpFetchAncestor, error) {
	walk := &mcpAncestorWalk{app: app, authority: authority, seen: map[string]bool{page.Id: true}}
	err := walk.above(page)
	return walk.ancestors, err
}

// mcpDatabaseAncestors lists what stands above a database held by a page:
// the blocks it stands in there and the page, or for a full-page database
// what stands above its page.
func mcpDatabaseAncestors(app core.App, authority mcpAuthority, holderPageID, containerID string) ([]mcpFetchAncestor, error) {
	walk := &mcpAncestorWalk{app: app, authority: authority, seen: map[string]bool{}}
	var blockID string
	if err := app.DB().NewQuery(`SELECT [[block_id]] FROM {{leafen_database_containers}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": containerID}).Row(&blockID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	start, err := walk.holder(holderPageID, blockID)
	if err == nil && start != nil {
		err = walk.above(start)
	}
	// Its fetch names the blocks above it by their words.
	if err == nil {
		err = mcpAncestorBlockTitles(app, walk.ancestors)
	}
	return walk.ancestors, err
}

func (walk *mcpAncestorWalk) above(page *core.Record) error {
	// To the top however deep: each page is visited once (walk.seen).
	for current := page; current != nil; {
		next, err := walk.step(current)
		if err != nil {
			return err
		}
		current = next
	}
	return nil
}

// step adds what stands just above a page and returns the page to go on
// from, or nil at the top or at one the person can't read.
func (walk *mcpAncestorWalk) step(page *core.Record) (*core.Record, error) {
	// A token narrowed to chosen pages sees nothing above them.
	if walk.authority.PagesNarrowed && slices.Contains(walk.authority.PageIDs, page.Id) {
		return nil, nil
	}
	if !isDatabaseRowPageRecord(page) {
		parentID, err := pageAncestryParentID(walk.app, page)
		if err != nil {
			return nil, err
		}
		return walk.page(parentID, page.Id)
	}
	// One whose data source is in the trash names them as any's
	// (mcp_fetch_trash.go).
	above, err := mcpRowPageAbove(walk.app, page)
	if isPermissionDenied(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	source, err := loadMCPDataSourceLocationToRead(walk.app, page.GetString("data_source_id"))
	if errors.Is(err, errMCPScopeDenied) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if above != source.PageID {
		// A page of a wiki sits under another page of it.
		return walk.page(above, page.Id)
	}
	holder, readable, err := walk.readable(source.PageID)
	if err != nil || !readable {
		return nil, err
	}
	// Its name as the agent reads it (page_name_mentions.go).
	source.Title = mcpSourceNameAsRead(walk.app, walk.authority, source.ID, source.Title)
	walk.ancestors = append(walk.ancestors, mcpFetchAncestor{
		Kind: mcpFetchAncestorDataSource, URL: mcpDataSourceURL(walk.app, source.ID), Title: source.Title,
		Untitled: walk.untitled(source.Title, func() string { return mcpDataSourceShownTitle(walk.app, walk.authority.PrincipalID, source) }),
		Page:     holder, Source: source,
	})
	containerID, err := loadDatabaseSQLSourceContainerID(walk.app, source)
	var blockID string
	if err == nil {
		walk.ancestors[len(walk.ancestors)-1].ContainerID = containerID
		title, ownNamed, listed := source.Title, false, false
		if walk.app.DB().NewQuery(`SELECT [[block_id]] FROM {{leafen_database_containers}} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"id": containerID}).Row(&blockID) == nil {
			if own, spans, named := databaseOwnName(walk.app, source.OwnerID, source.PageID, blockID, holder); named {
				title, ownNamed = mcpDatabaseOwnNameAsRead(walk.app, walk.authority, source.PageID, own, spans), true
			} else if title, listed, err = walk.listedName(containerID, title); err != nil {
				return nil, err
			}
		}
		// An untitled one is called what the screen calls it
		// (mcp_untitled_names.go), where Notion's names it by nothing
		// (title="", seen 2026-11-14) and its path leaves it out.
		// A full-page database is its page's, and so is a wiki's; any other
		// stands in its page, with no name of its own (mcpFetchAncestor) but
		// the one an inline one of several sources keeps, which its block's
		// title is (databaseOwnName).
		inline, own := true, ""
		if top, found, err := loadDatabasePageBlock(walk.app, source.OwnerID, source.PageID); err == nil && found &&
			top.ID == blockID && holder != nil && isFullPageDatabase(holder) {
			inline, own = false, walk.fullPageKeptName(top, holder)
		} else if wikiPageID, err := wikiPageOfDatabase(walk.app, containerID); err == nil && wikiPageID != "" {
			inline = false
		} else if ownNamed {
			own = title
		}
		// The title its block keeps of its own names it first, inline too
		// (database_block_title.go); kept as none, nothing names it, as one
		// in a page (Inline) is named, and so one named by its sources'
		// names alone, which its block keeps none of.
		if kept, found := databaseBlockTitle(loadDatabaseBlockContent(walk.app, source.OwnerID, source.PageID, blockID)); found {
			own, inline = kept, inline || kept == ""
		} else if listed {
			inline = true
		}
		walk.ancestors = append(walk.ancestors, mcpFetchAncestor{
			Kind: mcpFetchAncestorDatabase, URL: mcpDatabaseAddress(walk.authority, containerID), Title: title,
			Inline: inline, OwnTitle: own,
			Untitled: walk.untitled(title, func() string { return mcpUntitledDatabaseName(walk.app, walk.authority.PrincipalID, holder, inline) }),
			Page:     holder, Source: source, ContainerID: containerID,
		})
	} else if !errors.Is(err, errMCPScopeDenied) {
		return nil, err
	}
	return walk.holder(source.PageID, blockID)
}

// listedName is the name of a database of two or more sources without one
// of its own, by which its path names it, as its pages' paths name it in the
// app and as it is named everywhere (databaseNameFrom): their names listed
// as Notion's search keeps them (mcpDatabaseNameAsIndexed); title, and
// false, for any other.
func (walk *mcpAncestorWalk) listedName(containerID, title string) (string, bool, error) {
	names, err := databaseContainerNames(walk.app, []string{containerID})
	if name := names[containerID]; err == nil && mcpDatabaseListed(name) {
		return mcpDatabaseNameAsIndexed(walk.app, walk.authority.PrincipalID, name), true, nil
	}
	return title, false, err
}

// fullPageKeptName is the name a full-page database keeps of its own while
// only its source was renamed since (full_page_database_own_name.go), as
// Notion's keeps the name it was made with on its block (seen 2026-11-21:
// its rows' ancestor line "… Full DB" over its source "… Full DB2"); none
// while it keeps none.
func (walk *mcpAncestorWalk) fullPageKeptName(top storedDatabasePageBlock, holder *core.Record) string {
	content := map[string]any{}
	if json.Unmarshal([]byte(top.Content), &content) != nil {
		return ""
	}
	own, spans, kept := keptDatabaseOwnName(content, holder.GetString("title"))
	if !kept || strings.TrimSpace(own) == "" {
		return ""
	}
	return mcpDatabaseOwnNameAsRead(walk.app, walk.authority, holder.Id, own, spans)
}

// holder goes on from the page holding a database at its block: that page,
// or for a full-page database or a wiki, whose page is the database, what
// stands above it.
func (walk *mcpAncestorWalk) holder(pageID, blockID string) (*core.Record, error) {
	record, readable, err := walk.readable(pageID)
	if err != nil || !readable {
		return nil, err
	}
	wikiDatabase, err := wikiDatabaseOfPage(walk.app, pageID)
	if err != nil {
		return nil, err
	}
	if record.GetString("kind") == pageKindDatabase || wikiDatabase != "" {
		walk.seen[pageID] = true
		return record, nil
	}
	return walk.page(pageID, blockID)
}

// page adds a page the person can read, after the blocks it holds heldID in
// (a page's own block, or a database's), and returns it.
func (walk *mcpAncestorWalk) page(pageID, heldID string) (*core.Record, error) {
	if pageID == "" || walk.seen[pageID] {
		return nil, nil
	}
	walk.seen[pageID] = true
	record, readable, err := walk.readable(pageID)
	if err != nil || !readable {
		return nil, err
	}
	if err := walk.blocks(pageID, heldID); err != nil {
		return nil, err
	}
	// An untitled page above is called what the screen calls it, in its
	// line and its path (mcp_untitled_names.go), where Notion's names it by
	// nothing (title="", seen 2026-11-14) and its path leaves it out.
	walk.ancestors = append(walk.ancestors, mcpFetchAncestor{
		Kind: mcpFetchAncestorPage, URL: mcpPageURL(walk.authority, pageID), Title: record.GetString("title"),
		Untitled: walk.untitled(record.GetString("title"), func() string { return mcpUntitledPageName(walk.app, walk.authority.PrincipalID, record) }),
		Page:     record,
	})
	return record, nil
}

// untitled is what a step is called while its title is blank, read only
// then; "" for a titled one.
func (walk *mcpAncestorWalk) untitled(title string, name func() string) string {
	if strings.TrimSpace(title) != "" {
		return ""
	}
	return name()
}

func (walk *mcpAncestorWalk) readable(pageID string) (*core.Record, bool, error) {
	authorization, err := authorizePage(walk.app, walk.authority.PrincipalID, pageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return authorization.Page, true, nil
}

// mcpPageAncestorIDs lists the pages above a page, nearest first, a database
// page's database's page among them, to the top however deep.
func mcpPageAncestorIDs(app core.App, pageID string) ([]string, error) {
	ancestors := []string{}
	seen := map[string]bool{pageID: true}
	current := pageID
	for {
		var parent struct {
			ParentID     string `db:"parent_id"`
			DataSourceID string `db:"data_source_id"`
		}
		if err := app.DB().NewQuery(`SELECT [[parent_id]], [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": current}).One(&parent); err != nil {
			break
		}
		next := parent.ParentID
		if parent.DataSourceID != "" && (next == "" || next == parent.DataSourceID) {
			var holder string
			_ = app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"id": parent.DataSourceID}).Row(&holder)
			next = holder
		}
		if next == "" || seen[next] {
			break
		}
		seen[next] = true
		ancestors = append(ancestors, next)
		current = next
	}
	return ancestors, nil
}
