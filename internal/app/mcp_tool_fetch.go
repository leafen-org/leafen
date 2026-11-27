package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// errMCPFetchNothing is fetch's refusal of what it can't open: an ID or an
// address that names nothing here, or something this account or connection
// doesn't reach, alike — as not found, as Notion's fetch answers both
// (seen 2026-11-16: 404 object_not_found, the page named and access worth
// checking), never as a permission it may not lack.
var errMCPFetchNothing = fmt.Errorf("%w: found nothing with that ID or address that this connection can open — "+
	"it isn't here, it was deleted, or this account doesn't reach it. Give one as notion-fetch or notion-search "+
	"gave it (a page's or database's address, collection:// or view://), or ask your person whether this account reaches it",
	errMCPPageMissing)

// fetchMCPNotionObject answers notion-fetch: what an ID or an address names,
// a page, a database, a data source or a view, as Notion's fetch gives it;
// a page with its open discussions named when asked (mcp_fetch_discussions.go).
func fetchMCPNotionObject(
	ctx context.Context,
	app core.App,
	rawID string,
	includeDiscussions bool,
) (*mcp.CallToolResult, notionMCPFetchOutput, error) {
	// A database's address names it after its page, at its block.
	if databaseID, ok := mcpDatabaseLinkID(rawID); ok && mcpDatabaseContainerExistsToRead(app, databaseID) {
		return fetchMCPDatabaseContainer(ctx, app, databaseID)
	}
	// The page Markdown's specification, which the page tools point to (notion_page_markdown_spec.go).
	if strings.TrimSpace(rawID) == notionPageMarkdownSpecURI {
		return nil, fetchMCPPageMarkdownSpec(), nil
	}
	if strings.TrimSpace(rawID) == mcpViewDSLSpecURI {
		return nil, fetchMCPViewDSLSpec(), nil
	}
	rawID = mcpLocalID(app, rawID)
	if mcpDatabaseContainerExistsToRead(app, rawID) {
		return fetchMCPDatabaseContainer(ctx, app, rawID)
	}
	// A formula's or a rollup's result is no address of its own: a database
	// page's fetch gives its value (mcp_page_computed_values.go), where
	// Notion's names it by one of these, which its fetch doesn't open.
	if scheme, _, found := strings.Cut(rawID, "://"); found && (scheme == "formulaResult" || scheme == "rollupResult") {
		return nil, notionMCPFetchOutput{}, mcpInvalid("a %s:// address names no value to fetch here: fetch the database page itself, whose properties give its formulas' and rollups' values, or read its data source's rows with notion-query-data-sources in rows mode", scheme)
	}
	// Like Notion's, fetch doesn't open any other kind of URL.
	if scheme, _, found := strings.Cut(rawID, "://"); found && !slices.Contains([]string{
		"collection", "view", "database", "http", "https",
	}, scheme) {
		return nil, notionMCPFetchOutput{}, fmt.Errorf(
			"%w: the fetch tool doesn't read %s URLs yet.", errMCPToolInvalid, scheme,
		)
	}
	if dataSourceID, ok := parseMCPDataSourceURL(rawID); ok {
		return fetchMCPDataSource(ctx, app, dataSourceID)
	}
	if strings.HasPrefix(rawID, mcpDataSourceURLPrefix) {
		return nil, notionMCPFetchOutput{}, mcpInvalid("that collection:// URL doesn't read: fetch it as notion-search or notion-fetch gave it")
	}
	if strings.HasPrefix(rawID, mcpViewURLPrefix) {
		viewID, ok := parseMCPViewIdentifier(rawID)
		if !ok {
			return nil, notionMCPFetchOutput{}, mcpInvalid("that view:// URL doesn't read: fetch it as notion-fetch gave it")
		}
		return fetchMCPView(ctx, app, viewID)
	}
	if databaseID, ok := parseMCPDatabaseIdentifier(rawID); ok {
		if strings.HasPrefix(rawID, mcpDatabaseURLPrefix) || mcpDatabaseContainerExistsToRead(app, databaseID) {
			return fetchMCPDatabaseContainer(ctx, app, databaseID)
		}
	} else if strings.HasPrefix(rawID, mcpDatabaseURLPrefix) {
		return nil, notionMCPFetchOutput{}, mcpInvalid("that database URL doesn't read: fetch it as notion-search or notion-fetch gave it")
	}
	// A full-page database's page is the database, and so is the page one
	// left as it turned inline.
	if containerID := mcpPageAddressDatabaseContainer(app, rawID); containerID != "" {
		return fetchMCPDatabaseContainer(ctx, app, containerID)
	}
	// A block by its own ID, as an ancestor-path names one (mcp_fetch_block.go).
	if block, found := mcpBlockNamed(app, rawID); found {
		return fetchMCPBlock(ctx, app, block, includeDiscussions)
	}
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPFetchOutput{}, err
	}
	// Its words mark the discussions left on them either way; asked, the
	// discussions are named before it too (mcp_fetch_block_discussions.go).
	var discussions *mcpFetchedDiscussions
	names := (func(threads, blockID string) string)(nil)
	if includeDiscussions {
		if discussions, err = mcpFetchDiscussions(app, authority, rawID); err != nil {
			return nil, notionMCPFetchOutput{}, err
		}
		names = discussions.namer()
	} else if names, _, err = mcpWriteDiscussions(app, authority, rawID); err != nil {
		return nil, notionMCPFetchOutput{}, err
	}
	_, page, err := fetchMCPPage(ctx, app, mcpFetchInput{ID: rawID, Discussions: names})
	if errors.Is(err, errMCPCantRead) {
		// The page tools' refusal reads as a permission alone; fetch takes
		// any address, and one Leafen doesn't know is refused the same way
		// (2026-11-13: an agent heard it lacked access to a database fetch
		// had named).
		return nil, notionMCPFetchOutput{}, errMCPFetchNothing
	}
	if err != nil {
		return nil, notionMCPFetchOutput{}, err
	}
	record, err := app.FindRecordById(pagesCollection, page.ID)
	if err != nil {
		return nil, notionMCPFetchOutput{}, failedBecause(errMCPToolFailed, err)
	}
	icon, cover, err := mcpPagePresentationObjects(app, authority, record)
	if err != nil {
		return nil, notionMCPFetchOutput{}, failedBecause(errMCPToolFailed, err)
	}
	facts, err := loadMCPPageFacts(app, record, time.Now(), authority.clock())
	if err != nil {
		return nil, notionMCPFetchOutput{}, failedBecause(errMCPToolFailed, err)
	}
	ancestors, err := mcpPageAncestors(app, authority, record)
	if err != nil {
		return nil, notionMCPFetchOutput{}, failedBecause(errMCPToolFailed, err)
	}
	// A page in the trash, or under it, fetches as Notion's does
	// (mcp_fetch_trash.go).
	trash, err := mcpPageTrash(app, record)
	if err != nil {
		return nil, notionMCPFetchOutput{}, failedBecause(errMCPToolFailed, err)
	}
	shownAncestors := ancestors
	if trash.NoAncestors {
		shownAncestors = nil
	}
	// A page with a block above it shows none, a database page names them
	// by their words (mcp_fetch_ancestor_blocks.go).
	if !isDatabaseRowPageRecord(record) {
		shownAncestors = mcpAncestorsShownForPage(shownAncestors)
	} else if err := mcpAncestorBlockTitles(app, shownAncestors); err != nil {
		return nil, notionMCPFetchOutput{}, failedBecause(errMCPToolFailed, err)
	}
	if trash.Blank {
		page.Content, page.Truncated = "", false
	}
	// An ordinary page's one property is its title, as Markdown, as a title
	// cell's is (mcpPropertyMarkdown); a database page gives its values as
	// its data source's SQLite table holds them.
	properties := map[string]any{"title": page.Title}
	if spans := ordinaryPageTitleSpans(record); len(spans) > 0 || page.Title != "" {
		mentions := mcpPropertyMentions(app, authority, record.GetString("workspace"))
		properties["title"] = mcpPropertyMarkdown(page.Title, spans, mentions)
	}
	schema := ""
	if isDatabaseRowPageRecord(record) {
		row, err := mcpFetchedDatabasePage(app, authority, record)
		if err == nil {
			properties, err = mcpDatabasePageFetchProperties(app, authority, row)
		}
		// Its data source's table follows it, as Notion's fetch gives a
		// database page, but short (mcp_parent_schema_text.go).
		if err == nil && !isDatabaseTemplatePageRecord(record) {
			schema, err = mcpParentSchemaText(app, authority, row.DataSourceID)
		}
		if err != nil {
			return nil, notionMCPFetchOutput{}, failedBecause(errMCPToolFailed, err)
		}
	}
	// An emoji icon leads the page's title, as Notion's fetch gives it, a
	// mention in it reads as it shows (mcpTitleAsShown), and an untitled
	// page reads as the screen calls it, a row by its database's word for
	// its untitled rows (mcp_untitled_names.go).
	title, emoji := mcpTitleAsShown(app, authority, record.GetString("workspace"), record.Id, page.Title), ""
	title = mcpNamedOr(title, mcpUntitledPageName(app, authority.PrincipalID, record))
	if icon != nil && *icon != nil && (*icon)["type"] == "emoji" {
		emoji, _ = (*icon)["emoji"].(string)
		title = strings.TrimSpace(emoji + " " + title)
	}
	url := page.URL
	fullWidth := record.GetBool("full_width")
	text, err := mcpPageFetchText(mcpFetchedPage{
		URL: url, AsOf: personClockMillisOf(facts.LastEditedAt, authority.clock()), Emoji: emoji, Icon: icon, Deleted: trash.Deleted,
		Discussions: discussions.text(), Ancestors: shownAncestors, Properties: properties, Content: page.Content, Schema: schema,
		ValueNote: mcpFetchTextValueNote(properties),
	})
	if err != nil {
		return nil, notionMCPFetchOutput{}, failedBecause(errMCPToolFailed, err)
	}
	return nil, notionMCPFetchOutput{
		Metadata: notionMCPFetchMetadata{Type: "page"}, Title: title, URL: url, Text: text,
		Icon: icon, Cover: cover, Path: mcpAncestorPath(app, ancestors), FullWidth: &fullWidth,
		SmallText: mcpPageSmallText(record), Font: mcpPageFont(record),
		Verification: &facts.Verification, PageLastEditedAt: personClockMillisOf(facts.LastEditedAt, authority.clock()), Truncated: page.Truncated,
	}, nil
}

// mcpPageSmallText is a page's small text when it is on, as fetch reads it.
func mcpPageSmallText(record *core.Record) *bool {
	if !record.GetBool("small_text") {
		return nil
	}
	on := true
	return &on
}

// mcpPageFont is a page's font when it isn't the default, as fetch reads it.
func mcpPageFont(record *core.Record) *string {
	font := record.GetString("font")
	if font == "" || font == pageFontDefault {
		return nil
	}
	return &font
}

// mcpPageAddressDatabaseContainer is the database a page's address names: a
// full-page database's page's or a wiki's (mcpFullPageDatabaseContainer),
// and the page's a full-page database left as it turned inline (a
// signpost, database_page_signposts.go), as in Notion a full-page database's
// address stays its own through the turn (seen 2026-11-13). "" for any other
// page.
func mcpPageAddressDatabaseContainer(app core.App, pageID string) string {
	if containerID := mcpFullPageDatabaseContainer(app, pageID); containerID != "" {
		return containerID
	}
	if place, signpost, err := loadDatabasePagePlace(app, pageID); err == nil && signpost {
		return place.ContainerID
	}
	return ""
}

// mcpFullPageDatabaseContainer is the database a page is: a full-page
// database's page's, or a wiki's, which Notion reads as a database by its
// page's ID (notion_wiki_identity.go); "" for any other page.
func mcpFullPageDatabaseContainer(app core.App, pageID string) string {
	record, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return ""
	}
	if !isFullPageDatabase(record) {
		wikiContainerID, _ := wikiDatabaseOfPage(app, record.Id)
		return wikiContainerID
	}
	containerID := ""
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_containers}}
		WHERE [[page_id]] = {:pageId} AND [[kind]] = 'source' AND [[parent_block_id]] = ''
		ORDER BY [[position]], [[id]] LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID}).Row(&containerID); err != nil {
		return ""
	}
	return containerID
}
