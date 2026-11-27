package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Notion's MCP fetch answers with the kind of what it read, its title and
// URL, and a text that wraps it (seen 2026-09-30): a page's text names its
// URL and when it was read, then the pages, databases and data source above
// it, its properties as JSON, its icon and its content, or that it has none;
// a database's names its data sources and views; a data source's gives its
// configurable state and SQLite table; a view's gives its configuration.
// Leafen's says those heads in its own words (mcp_notion_tool_words.go). The
// page's content is page Markdown as the public API's "v2" writes it
// (mcpMarkdownReader). Leafen names a page by its address on the server the
// connection reached (/p/ID), the one a person opens, as Notion's names its
// own; a database by its page's address and its block (#block-ID); data
// sources by collection://ID and views by view://ID.
// Page Markdown links pages by /pages/ID, which writes read back as links to
// them, and which the app opens too.

const mcpPageURLPrefix = "/pages/"

// mcpDatabaseLinkFragment leads the block a database's address names.
const mcpDatabaseLinkFragment = "#block-"

type notionMCPFetchMetadata struct {
	Type string `json:"type"`
}

type notionMCPFetchOutput struct {
	Metadata notionMCPFetchMetadata `json:"metadata"`
	Title    string                 `json:"title"`
	URL      string                 `json:"url"`
	Text     string                 `json:"text"`
	// WorkspaceID is a data source's workspace (mcp_tool_data_source_fetch.go).
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"The workspace holding the data source, to name as workspace_id"`
	// Icon and Cover are a page's, as the Public API gives them; null when
	// the page has none.
	Cover *map[string]any `json:"cover,omitempty"`
	Icon  *map[string]any `json:"icon,omitempty"`
	// Path, Verification and PageLastEditedAt are a page's facts
	// (mcp_page_facts.go); a top-level page has no path. A database gives
	// its verification once it is verified.
	// FullWidth is a page's width, as Notion's fetch gives it; SmallText and
	// Font its text, once update_properties set them away from the default.
	FullWidth *bool   `json:"full_width,omitempty"`
	SmallText *bool   `json:"small_text,omitempty"`
	Font      *string `json:"font,omitempty"`
	// Truncated says a page's Markdown stops before its end (mcpFetchOutput).
	Truncated bool `json:"truncated,omitempty" jsonschema:"The page's content stops before its end: it holds more blocks than one read gives"`
	// Blocks are a page's blocks when asked for (mcp_fetch_blocks.go), a
	// window at a time: BlocksNextCursor, while more follow, lists on.
	Blocks           []notionMCPFetchedBlock `json:"blocks,omitempty"`
	BlocksNextCursor string                  `json:"blocks_next_cursor,omitempty" jsonschema:"More of the page's blocks follow: fetch again with include_blocks and this as blocks_cursor"`
	Path             *string                 `json:"path,omitempty"`
	Verification     *map[string]any         `json:"verification,omitempty"`
	PageLastEditedAt string                  `json:"page_last_edited_at,omitempty"`
	// References gives each stored file the text names a download
	// address, when include_file_urls asks (mcp_fetch_file_references.go).
	References map[string]string `json:"references,omitempty"`
	// FileVersions are its stored files' versions (mcp_fetch_file_versions.go).
	FileVersions []mcpFetchedFileVersion `json:"file_versions,omitempty" jsonschema:"Each stored file the read holds — a file, image, PDF, video or audio block its text names, by block_id, or a file in a database page's files property, by page_id, property and file_id — with base_version, which leafen_replace_file takes with the same names to replace it while it is still that version"`
}

// mcpFetchAncestor is one of what stands above a page or database: a page,
// a database or a data source.
type mcpFetchAncestor struct {
	Kind  string
	URL   string
	Title string
	// <ancestor-path> names a database by its own name, as Notion's by its
	// block's title (mcp_fetch_ancestors.go): OwnTitle where it keeps one, else
	// none for one in a page (Inline) or its title. Its path reads its title.
	Inline   bool
	OwnTitle string
	// Untitled is what it is called while it has no name, which its path
	// and its line say then (mcp_untitled_names.go).
	Untitled string
	// What the walk stood on, which the app's path draws (page_path_route.go):
	// a page's record, or the page holding a database or data source; the
	// source; a database's container (for a data source, the one it is in);
	// a block's ID, type and page (mcp_fetch_ancestor_blocks.go), no path's.
	Page                                         *core.Record
	Source                                       mcpDataSourceLocation
	ContainerID, BlockID, BlockType, BlockPageID string
}

const (
	mcpFetchAncestorPage       = "page"
	mcpFetchAncestorDatabase   = "database"
	mcpFetchAncestorDataSource = "data-source"
)

// mcpFetchedPage is what a page's fetch text holds.
type mcpFetchedPage struct {
	URL        string
	AsOf       string
	Emoji      string
	Icon       *map[string]any
	Ancestors  []mcpFetchAncestor
	Properties map[string]any
	Content    string
	// Discussions is the page's <page-discussions>, which leads what stands
	// above it, "" for none (mcp_fetch_discussions.go); Schema the SQLite
	// table of a database page's data source, short, which follows the page
	// (mcpParentSchemaText), after ValueNote, how its text values read
	// while one reads escaped (mcpFetchTextValueNote).
	Discussions string
	Schema      string
	ValueNote   string
	// Deleted marks a page in the trash, as Notion's fetch does
	// (mcp_fetch_trash.go).
	Deleted bool
}

// mcpFetchedDatabase is what a database's fetch text holds: its data sources'
// texts and its views' configurations.
type mcpFetchedDatabase struct {
	URL    string
	Title  string
	Inline bool
	// Deleted is a database in the trash, its sources all in the trash where
	// they stand, or under a page in the trash, as Notion's fetch marks it
	// (seen 2026-11-11: `inline="true" deleted`; mcp_fetch_trash.go).
	Deleted     bool
	Locked      bool
	Ancestors   []mcpFetchAncestor
	DataSources []string
	Views       []mcpFetchedView
	// SourcesOnly is the database a create or update answer gives, which
	// names its data sources but not its views, as Notion's (seen
	// 2026-11-14: "Created database: <database …>…</data-sources>\n</database>").
	SourcesOnly bool
}

type mcpFetchedView struct {
	URL    string
	Config map[string]any
}

// mcpPageURL is a page's address on the server the connection reached: a
// person opens it and fetch reads it (2026-10-16: results named pages by
// /pages/ID, which the app didn't open). A UUID stands in it without its
// dashes (notionMentionID), as a database's address does and as Notion's
// MCP writes every page's, a database page's too, while their IDs keep the
// dashes (seen 2026-11-16: create-pages answered id with dashes and url
// /p/ and 32 digits; its SQL's url and relation columns alike) — one form
// for every address an agent reads.
func mcpPageURL(authority mcpAuthority, pageID string) string {
	address, err := mcpServerURL(authority.Resource, "/p/"+notionMentionID(pageID))
	if err != nil {
		return "/p/" + notionMentionID(pageID)
	}
	return address
}

// mcpDatabaseAddress is a database's address as fetch, search and the
// database tools give it, as Notion's: its own ID, which the screen opens,
// without dashes as the page's Markdown names it (databaseAppAddress) — one
// database, one address, full-page or in a page (seen 2026-11-13: Notion's
// fetch of an inline database and of the page holding it both name it /p/
// and 32 digits; its collection:// and view:// keep their dashes). An
// address naming the page it is on at its block (mcpDatabaseLinkFragment),
// or a full-page one's page, still reads as it.
func mcpDatabaseAddress(authority mcpAuthority, containerID string) string {
	origin, err := mcpServerURL(authority.Resource, "")
	if err != nil {
		origin = ""
	}
	return databaseAppAddress(origin, containerID)
}

// mcpDatabaseShownTitle is a database's or source's title as fetch names it:
// one without a name reads as a new database, in the reader's language where
// the server has it, as Notion's (seen 2026-11-05).
func mcpDatabaseShownTitle(app core.App, principalID, title string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	if mcpReaderReadsKorean(app, principalID) {
		return "새 데이터베이스"
	}
	return notionDatabaseShownTitle(title)
}

// mcpPageShownTitle is a page's title as fetch names it at its top: one
// without a title reads as a new page, in the reader's language where the
// server has it, as Notion's (seen 2026-11-13: "새 페이지", its title
// property ""). A list leaves an untitled page's title out instead
// (notionMCPPageListItem), and the trash tools give it as "", as Notion's.
func mcpPageShownTitle(app core.App, principalID, title string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	if mcpReaderReadsKorean(app, principalID) {
		return "새 페이지"
	}
	return notionUntitledPageMentionTitle
}

// mcpReaderReadsKorean is whether the connection's person reads Leafen in
// Korean (mcpReaderLanguage).
func mcpReaderReadsKorean(app core.App, principalID string) bool {
	return mcpReaderLanguage(app, principalID) == accountLanguageKo
}

// mcpDatabaseLinkID is the block an address names after #block-, which is
// the database's when the address is a database's.
func mcpDatabaseLinkID(raw string) (string, bool) {
	_, block, found := strings.Cut(strings.TrimSpace(raw), mcpDatabaseLinkFragment)
	if !found || block == "" || strings.ContainsAny(block, "/?#&") {
		return "", false
	}
	return block, true
}

// mcpPageAddress is a page's address. A template has its own, /p/ and its
// ID, as fetch's page_templates lists it, which the screen opens whole there,
// as Notion's does (StandaloneRowPage.tsx), and every tool reads as the
// template; an older address of one, its database's page
// with ?tp=, still reads as it (mcpReferencePart). It named the database's
// page first, and an agent took that page for the template (2026-11-23,
// in daily use: a ledger copied whole into a row). A full-page database's
// page is its database, at the database's address (mcpDatabaseAddress).
func mcpPageAddress(app core.App, authority mcpAuthority, pageID string) string {
	if containerID := mcpFullPageDatabaseContainer(app, pageID); containerID != "" {
		return mcpDatabaseAddress(authority, containerID)
	}
	return mcpPageURL(authority, pageID)
}

// mcpTemplateAddressParam is the parameter that opens a template in its
// database's page (the web's useTemplateAddress.ts), read in a template's
// older address.
const mcpTemplateAddressParam = "tp"

// mcpFetchJSON writes JSON as Notion's fetch text does: without escaping
// <, > and &, object keys in order.
func mcpFetchJSON(value any) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}

// mcpFetchIconJSON writes an icon as Notion's iconMetadata does, its type
// first; null without one.
func mcpFetchIconJSON(icon *map[string]any) (string, error) {
	if icon == nil || *icon == nil {
		return "null", nil
	}
	keys := make([]string, 0, len(*icon))
	for key := range *icon {
		if key != "type" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if _, typed := (*icon)["type"]; typed {
		keys = append([]string{"type"}, keys...)
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		name, err := mcpFetchJSON(key)
		if err != nil {
			return "", err
		}
		value, err := mcpFetchJSON((*icon)[key])
		if err != nil {
			return "", err
		}
		parts = append(parts, name+":"+value)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func mcpFetchAttribute(value string) string {
	return strings.NewReplacer(`&`, "&amp;", `"`, "&quot;", `<`, "&lt;", `>`, "&gt;").Replace(value)
}

// mcpAncestorPathText lists what stands above, nearest first: parent-…, then
// ancestor-2-…; a data source is named by name=, the rest by title=.
func mcpAncestorPathText(ancestors []mcpFetchAncestor) string {
	if len(ancestors) == 0 {
		return "<ancestor-path></ancestor-path>"
	}
	lines := []string{"<ancestor-path>"}
	for index, ancestor := range ancestors {
		tag := "parent-" + ancestor.Kind
		if index > 0 {
			tag = fmt.Sprintf("ancestor-%d-%s", index+1, ancestor.Kind)
		}
		// A step without a name of its own says what the path calls it
		// (mcpAncestorStepName); a block's says its words.
		label, title := "title", mcpAncestorStepName(ancestor)
		switch {
		case ancestor.Kind == mcpFetchAncestorDataSource:
			label = "name"
		case ancestor.OwnTitle != "":
			title = ancestor.OwnTitle
		}
		lines = append(lines, fmt.Sprintf(`<%s url="%s" %s="%s"/>`,
			tag, mcpFetchAttribute(ancestor.URL), label, mcpFetchAttribute(title)))
	}
	return strings.Join(append(lines, "</ancestor-path>"), "\n")
}

// mcpAncestorStepName is what a step of an <ancestor-path> is called while
// it has no name of its own: a database in a page, or one whose block keeps
// none, what its path calls it, by its source (Notion's says title="",
// seen 2026-11-21), and an untitled page, database or data source what the
// screen calls it (mcp_untitled_names.go); a block its words.
func mcpAncestorStepName(ancestor mcpFetchAncestor) string {
	if ancestor.BlockID != "" {
		return ancestor.Title
	}
	return mcpNamedOr(ancestor.Title, ancestor.Untitled)
}

// mcpAncestorPath is a page's path: the titles of the pages and databases
// above it from the top down, joined by " / "; nil for a top-level page.
// Each reads as Notion's search keeps it (seen 2026-11-22: "… / w2322 제목
// 언급 ‣ 끝", mcpTitleAsIndexed), and an untitled one by what it is called
// (mcp_untitled_names.go), where Notion's path leaves it out (seen
// 2026-11-14: "〈test page〉 / 〈page〉" under an untitled page or database).
func mcpAncestorPath(app core.App, ancestors []mcpFetchAncestor) *string {
	titles := []string{}
	for index := len(ancestors) - 1; index >= 0; index-- {
		ancestor := ancestors[index]
		if ancestor.Kind == mcpFetchAncestorDataSource || ancestor.BlockID != "" {
			continue
		}
		if title := mcpNamedOr(mcpAncestorTitleAsIndexed(app, ancestor), ancestor.Untitled); strings.TrimSpace(title) != "" {
			titles = append(titles, title)
		}
	}
	if len(titles) == 0 {
		return nil
	}
	path := strings.Join(titles, " / ")
	return &path
}

func mcpPageFetchText(page mcpFetchedPage) (string, error) {
	properties, err := mcpFetchJSON(page.Properties)
	if err != nil {
		return "", err
	}
	icon, err := mcpFetchIconJSON(page.Icon)
	if err != nil {
		return "", err
	}
	emoji := ""
	if page.Emoji != "" {
		emoji = ` icon="` + mcpFetchAttribute(page.Emoji) + `"`
	}
	// A page without content says so in place of its content, as Notion's
	// does (seen 2026-11-11).
	content := "<content>\n" + strings.TrimRight(page.Content, "\n") + "\n</content>"
	if strings.TrimSpace(page.Content) == "" {
		content = mcpFetchBlankPage
	}
	return mcpFetchPageHead(page.URL, page.AsOf) + fmt.Sprintf(
		"<page url=\"%s\"%s%s>\n%s%s\n<properties>\n%s\n</properties>\n<iconMetadata>%s</iconMetadata>\n"+
			"%s\n</page>%s%s",
		mcpFetchAttribute(page.URL), emoji, mcpDeletedAttribute(page.Deleted),
		page.Discussions, mcpAncestorPathText(page.Ancestors), properties, icon, content, page.ValueNote, page.Schema), nil
}

func mcpViewElement(view mcpFetchedView) (string, error) {
	config, err := mcpFetchJSON(view.Config)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("<view url=\"{{%s}}\">\n%s\n</view>", mcpFetchAttribute(view.URL), config), nil
}

func mcpViewFetchText(view mcpFetchedView) (string, error) {
	element, err := mcpViewElement(view)
	if err != nil {
		return "", err
	}
	return mcpFetchViewHead(view.URL) + element, nil
}

// mcpLockedAttribute marks a locked database's tag, as Notion's fetch does
// (seen 2026-10-01: `<database … locked>`).
func mcpLockedAttribute(locked bool) string {
	if locked {
		return " locked"
	}
	return ""
}

func mcpDeletedAttribute(deleted bool) string {
	if deleted {
		return " deleted"
	}
	return ""
}

func mcpDatabaseFetchText(database mcpFetchedDatabase) (string, error) {
	views := make([]string, 0, len(database.Views))
	for _, view := range database.Views {
		element, err := mcpViewElement(view)
		if err != nil {
			return "", err
		}
		views = append(views, element)
	}
	lines := []string{
		fmt.Sprintf(`<database url="{{%s}}" inline="%t"%s%s>`, mcpFetchAttribute(database.URL), database.Inline,
			mcpDeletedAttribute(database.Deleted), mcpLockedAttribute(database.Locked)),
		mcpFetchTitleLine("Database", database.Title),
		mcpAncestorPathText(database.Ancestors),
	}
	lines = append(append(lines, mcpFetchDataSourcesLines...), "<data-sources>")
	lines = append(lines, database.DataSources...)
	lines = append(lines, "</data-sources>")
	if !database.SourcesOnly {
		lines = append(append(lines, mcpFetchViewsLines...), "<views>")
		lines = append(lines, views...)
		lines = append(lines, "</views>")
	}
	return strings.Join(append(lines, "</database>"), "\n"), nil
}
