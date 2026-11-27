package app

import (
	"context"
	"net/url"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's notion-create-pages moves a page here that a new page's <unknown
// url="…"/> line names into the new page, where the line stands (seen
// 2026-11-17 in two runs, its page and a database's page alike): a line of
// its own, in a toggle too, is the page's block there; one a list's item or
// a quote holds is an item or a quote of no text holding the page's block;
// a database's page leaves its database as a page of its own, a full-page
// database moves as its page does, and a page in the trash comes out of it
// (seen 2026-11-17 twice, on a line of its own and an item's). A to-do's is its text
// (notionMarkdownCreateHeldUnknown). One naming the new page's parent or a
// page above it is refused before any page is made, as Notion's is; one
// naming no page this account can read here, or a page with a block or a
// view after it, stays the refusal it was (mcpCreatePageUnknownRefusal).
// The public API's create refuses each such line, the page there or not
// (seen 2026-11-17, "v1" and "v2": notionMarkdownCreateUnknownError).
// A page named twice stands at its first line, the others left without it:
// Notion shows its block at each, which a page's one block can't be.

// mcpCreatePageMovesRefusal refuses, as Notion does, a line naming the new
// page's parent or a page above it (seen 2026-11-17).
const mcpCreatePageMovesRefusal = "The page change isn't valid."

// mcpCreatePageMoves is what a new page's content moves into it: the
// content with each line that moves a page written as that page's tag, the
// pages it moves in the order of their lines, and those pages as the
// Markdown's plan reads them (notionMarkdownReplacementPlan).
type mcpCreatePageMoves struct {
	content    string
	pageIDs    []string
	references []notionMarkdownReference
	// trashed are those of them in the trash, brought back before they move.
	trashed []string
	// left says, by its words in notion-move-pages, each database item among
	// them that leaves its database (mcpCreateMovesRows).
	left []mcpMarkdownWarning
}

// mcpCreatePageMovesOf reads which pages here a new page's content moves
// into it, parentID being the page the new page goes under ("" for none), in
// workspaceID; a line it moves no page by is left as it is. A database's
// item it moves is refused when the definition keeps items in their
// databases (mcpCreateMovesRows).
func mcpCreatePageMovesOf(
	app core.App, authority mcpAuthority, workspaceID, parentID string, definition notionMCPCreatePageDefinition, origin string,
) (mcpCreatePageMoves, error) {
	content := definition.Content
	moves := mcpCreatePageMoves{content: content}
	if !strings.Contains(content, "<unknown") {
		return moves, nil
	}
	markdown, _, _ := notionMarkdownVersion2SpacedAttributes(content)
	lines := strings.Split(markdown, "\n")
	places := notionMarkdownCreateUnknownPlaces(lines)
	named := map[string]bool{}
	for _, place := range places {
		pageID, tag, trashed, found := mcpCreateMovedPage(app, authority, workspaceID, place.address, origin)
		if !found {
			continue
		}
		if parentID != "" {
			above, err := pageAncestryIncludes(app, parentID, pageID)
			if err != nil {
				return moves, failedBecause(errMCPToolFailed, err)
			}
			if above {
				return moves, mcpInvalid("%s", mcpCreatePageMovesRefusal)
			}
		}
		if !mcpCreateMovesPage(app, authority.PrincipalID, pageID) {
			return moves, mcpInvalid("%s", mcpCreatePageMovesRefusal)
		}
		written := ""
		if !named[pageID] {
			named[pageID] = true
			written = "<" + tag + ` url="` + place.address + `"></` + tag + ">"
			moves.pageIDs = append(moves.pageIDs, pageID)
			if trashed {
				moves.trashed = append(moves.trashed, pageID)
			}
			moves.references = append(moves.references, notionMarkdownReference{
				BlockID: pageID, ID: pageID, Tag: tag, URL: place.address,
			})
		}
		lines[place.line] = place.rewritten(written)
	}
	if len(moves.pageIDs) > 0 {
		moves.content = strings.Join(lines, "\n")
	}
	return moves, mcpCreateMovesRows(app, &moves, definition.rowsStay)
}

// mcpCreateMovesRows tells of the database items a new page's content moves
// in, each leaving its database as a page of its own with its values kept
// should it go back (moveMCPDatabasePagesOut): the Notion-compatible
// endpoint moves them, as Notion's MCP does, and says so for each in the
// words notion-move-pages says it with; the Leafen-native endpoint without
// leave_database (stay) refuses the call before any page is made, in that
// tool's words, as an item leaves its database only when the call says so
// (2026-11-16: an agent moved one under a page unawares).
func mcpCreateMovesRows(app core.App, moves *mcpCreatePageMoves, stay bool) error {
	rows := 0
	for _, pageID := range moves.pageIDs {
		var dataSourceID string
		if err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": pageID}).Row(&dataSourceID); err != nil {
			return failedBecause(errMCPToolFailed, err)
		}
		if dataSourceID == "" {
			continue
		}
		rows++
		moves.left = append(moves.left, mcpMarkdownWarning{
			Code: "database_item_left", Message: databaseRowLeftWarning(pageID, false),
		})
	}
	if rows > 0 && stay {
		return mcpInvalid("the content moves %d database item(s) into the new page by <unknown url=\"…\"/> lines: "+
			"under a page each leaves its database as a page of its own; send leave_database true to do it, "+
			"or leave those lines out", rows)
	}
	return nil
}

// mcpCreateMovedPage is the page an <unknown/> line's address names here
// for a new page to move in: a page or a full-page database this account
// reads in the workspace and the connection reaches, with the tag that
// names it ("page" or "database") and whether it is in the trash.
func mcpCreateMovedPage(app core.App, authority mcpAuthority, workspaceID, address, origin string) (string, string, bool, bool) {
	if notionMarkdownCreateUnknownAddress(address, origin) != notionMarkdownCreateUnknownReference {
		return "", "", false, false
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Fragment != "" || parsed.Query().Get("v") != "" {
		return "", "", false, false
	}
	id := notionMarkdownAddressPageID(parsed)
	if id == "" {
		return "", "", false, false
	}
	dashed := id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
	tag := "page"
	record, err := app.FindRecordById(pagesCollection, mcpLocalID(app, dashed))
	if err != nil {
		containerID, ok := resolveMCPDatabaseID(app, dashed)
		if !ok {
			return "", "", false, false
		}
		// A full-page database is its page; an inline one is a block of another.
		location, err := loadMCPDatabaseContainerLocation(app, containerID)
		if err != nil {
			return "", "", false, false
		}
		if record, err = app.FindRecordById(pagesCollection, location.PageID); err != nil || record.GetString("kind") != pageKindDatabase {
			return "", "", false, false
		}
		tag = "database"
	}
	authorization, err := authorizePage(app, authority.PrincipalID, record.Id, permissionWorkspaceRead)
	if err != nil || authorization.Workspace.WorkspaceID != workspaceID ||
		!mcpAuthorityReachesPage(app, authority, authorization.Page) {
		return "", "", false, false
	}
	return record.Id, tag, record.GetBool("in_trash"), true
}

// mcpCreateMovesPage tells a page this account may move: one it has full
// access to, a database's page one it may change.
func mcpCreateMovesPage(app core.App, principalID, pageID string) bool {
	record, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return false
	}
	if record.GetString("data_source_id") != "" {
		_, err = authorizeDatabaseRowPage(app, principalID, pageID, permissionWorkspaceWrite)
		return err == nil
	}
	_, err = authorizePageAccessRole(app, principalID, pageID, pageAccessRoleFullAccess)
	return err == nil
}

// moveMCPCreatePagePages moves the pages a new page's content moves into it
// under it, at its body's end, before its body is written there
// (mcpCreatePageMoves): one in the trash comes back first, as
// notion-restore-pages brings it, and a database's page leaves its database.
func moveMCPCreatePagePages(ctx context.Context, app core.App, pageID string, moves mcpCreatePageMoves) error {
	if len(moves.pageIDs) == 0 {
		return nil
	}
	for _, trashed := range moves.trashed {
		if _, _, err := trashMCPPage(ctx, app, "", trashed, false, "", nil); err != nil {
			return err
		}
	}
	_, _, err := moveMCPPages(ctx, nil, app, notionMCPMovePagesInput{
		NewParent: notionMCPMovePagesParent{PageID: pageID}, PageOrDatabaseIDs: moves.pageIDs,
	}, true, false)
	return err
}

// writeMCPCreatedBody writes a page made by notion-create-pages its body as
// notion-update-page writes Markdown, once the pages its content moves in
// are under it (moveMCPCreatePagePages), with what reading it said.
func writeMCPCreatedBody(
	ctx context.Context, app core.App, pageID string, definition notionMCPCreatePageDefinition, moves mcpCreatePageMoves,
) ([]mcpMarkdownWarning, error) {
	if err := moveMCPCreatePagePages(ctx, app, pageID, moves); err != nil {
		return nil, err
	}
	content := definition.Content
	if len(moves.pageIDs) > 0 {
		content = moves.content
	}
	_, written, err := updateMCPPage(ctx, app, mcpUpdatePageInput{
		PageID: pageID, Command: "replace_content", NewString: content,
		CreatedBody: true, PreserveInternalLinks: definition.PreserveInternalLinks != nil && *definition.PreserveInternalLinks,
	})
	// The database items it moved in say they left their databases after
	// what reading its Markdown said.
	return append(written.Warnings, moves.left...), err
}

// mcpCreateRowContent reads a new database page's content before any page is
// made: the pages it moves in, then what it refuses (mcpCreateRowContentRefusal).
func mcpCreateRowContent(
	app core.App, authority mcpAuthority, location mcpDataSourceLocation, definition notionMCPCreatePageDefinition, origin string, pages, index int,
) (mcpCreatePageMoves, error) {
	moves, err := mcpCreatePageMovesOf(app, authority, location.WorkspaceID, location.PageID, definition, origin)
	if err != nil {
		return moves, err
	}
	if refused := mcpCreateRowContentRefusal(moves.content, origin, pages, index); refused != nil {
		return moves, refused
	}
	return moves, nil
}

// notionMarkdownCreateUnknownPlace is an <unknown/> line of a new page's
// "v2" Markdown closed in itself: its line, what stands before its tag
// there, whether a <quote> tag holds it and is its first line, and its
// address.
type notionMarkdownCreateUnknownPlace struct {
	line                int
	before              string
	quoted, quoteOpened bool
	address             string
}

// notionMarkdownCreateUnknownPlaces finds a new page's "v2" <unknown/>
// lines, read as notionMarkdownCreateHeldUnknown reads them: on a line of
// their own, or a list's item's or a quote's text, out of code and
// equations, and not written four columns in at the top.
func notionMarkdownCreateUnknownPlaces(lines []string) []notionMarkdownCreateUnknownPlace {
	places := []notionMarkdownCreateUnknownPlace{}
	quotes, fence, equation, opened := 0, "", false, false
	top := notionMarkdownVersion2TopWritten{}
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		first := opened
		opened = false
		switch {
		case fence != "":
			if notionMarkdownFenceCloses(trimmed, fence, true) {
				fence = ""
			}
			continue
		case equation:
			equation = trimmed != "$$"
			continue
		case notionMarkdownVersion2Fence(trimmed):
			fence = notionMarkdownFenceOf(trimmed)
			continue
		case trimmed == "$$":
			equation = true
			continue
		case notionMarkdownCreateQuoteOpening.MatchString(trimmed):
			quotes++
			opened = true
			continue
		case trimmed == "</quote>" && quotes > 0:
			quotes--
			continue
		}
		marks := notionMarkdownVersion2MarkerRun.FindString(trimmed)
		text := trimmed[len(marks):]
		if top.written(line) || notionMarkdownUpdateMarkedUnknown.FindString(text) == "" ||
			notionMarkdownUnknownPattern.FindString(text) == "" {
			continue
		}
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		places = append(places, notionMarkdownCreateUnknownPlace{
			line: index, before: lead + marks, quoted: quotes > 0 && strings.TrimSpace(marks) == "", quoteOpened: first,
			address: strings.TrimSpace(notionMarkdownAttributes(text)["url"]),
		})
	}
	return places
}

// rewritten is the place's line holding written, the moved page's tag, in
// its stead, as Leafen reads a page's block there (as Notion's fetch writes
// it): on a line of its own where the line stands; under a list's item
// holding nothing, a list mark's width further in (a tab, in lines a tab
// starts); after a quote's mark; first in a <quote> tag's blocks, its text
// <p />. Without written, the place holds nothing.
func (place notionMarkdownCreateUnknownPlace) rewritten(written string) string {
	marks := strings.TrimRight(place.before, " \t")
	switch {
	case place.quoted && place.quoteOpened:
		if written == "" {
			return place.before + "<p />"
		}
		return place.before + "<p />\n\n" + place.before + written
	case place.quoted:
		if written == "" {
			return ""
		}
		return "\n" + place.before + written
	case marks == "" || strings.HasSuffix(marks, ">"):
		return place.before + written
	}
	if written == "" {
		return place.before
	}
	under := ""
	if strings.HasPrefix(place.before, "\t") {
		under = place.before[:len(place.before)-len(strings.TrimLeft(place.before, "\t"))] + "\t"
	} else {
		under = strings.Map(func(character rune) rune {
			if character == '>' || character == '\t' {
				return character
			}
			return ' '
		}, place.before)
	}
	return place.before + "\n" + under + written
}
