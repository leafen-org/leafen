package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where an MCP answer says where something is — a path, the steps of an
// <ancestor-path>, a search result's (notion-search's and leafen_search's)
// or a catalog's name — a page, database
// or data source without a title is called what the screen calls it, in
// its reader's language, never "" and never left out (user 2026-11-26, an
// agent's real use: "A /  / C" read as a gap, where Notion's screen shows
// an untitled one by its word): a page "새 페이지" ("New page"), a row by its
// database's word for its untitled rows ("신규 작업", a task database's
// "새 작업"), a database "새 데이터베이스" ("New database") — a view block's
// page "제목 없음", one of several sources by them listed — and a data
// source among others "새 데이터 소스" ("New data source"), as fetch heads
// each one (mcpPageShownTitle,
// mcpDatabaseShownTitle, mcpDataSourceShownTitle) and as the web names them
// (web/src/lib/pageTitle.ts untitledPageDisplayTitle). Notion's path
// leaves an untitled one out, its <ancestor-path> says title="" and its
// search calls an untitled page "Untitled" in every language (seen
// 2026-11-14 and 2026-11-21), a difference on purpose. Nothing reads a path
// or a step's title back — every tool takes a page by its ID or address —
// and a page's Markdown keeps an untitled sub-page's tag empty, as a tag's
// title written back renames its page (notion_page_markdown_references.go).

// mcpUntitledPageName is what an answer calls a page while it has no
// title: a row by its database's word for its untitled rows, a full-page
// database by its kind, any other page a new page.
func mcpUntitledPageName(app core.App, principalID string, page *core.Record) string {
	return mcpUntitledPageNameIn(app, mcpReaderLanguage(app, principalID), page)
}

// mcpUntitledPageNameIn is mcpUntitledPageName in the language given, for
// words a person reads in theirs: an action waiting for them names its page
// so (mcp_action_approval_words.go), where its question read "Untitled" on
// a Korean screen (2026-11-26).
func mcpUntitledPageNameIn(app core.App, language string, page *core.Record) string {
	if page != nil && isDatabaseRowPageRecord(page) {
		if name := mcpDatabaseUntitledWord(app, page.GetString("data_source_id"), language); name != "" {
			return name
		}
	}
	kind := pageNameKind{Kind: pageKindPage}
	if page != nil {
		if read, err := pageNameKindOf(app, page); err == nil {
			kind = read
		}
	}
	return untitledNotificationPageTitleOf(language, kind)
}

// mcpUntitledDatabaseName is what an answer calls a database while it has
// no name: a full-page one by its page's kind (a view block's "제목 없음"),
// one in a page a new database.
func mcpUntitledDatabaseName(app core.App, principalID string, holder *core.Record, inline bool) string {
	if holder != nil && !inline {
		return mcpUntitledPageName(app, principalID, holder)
	}
	return untitledNotificationPageTitleOf(mcpReaderLanguage(app, principalID), pageNameKind{Kind: pageKindDatabase})
}

// mcpNamedOr is a title, or while it is blank what its untitled one is
// called.
func mcpNamedOr(title, untitled string) string {
	if strings.TrimSpace(title) == "" && untitled != "" {
		return untitled
	}
	return title
}

// mcpDatabaseUntitledWord is a data source's word for its pages without a
// title in the language given (databaseUntitledRowNameIn: the name it keeps
// for them, else its untitled default template's, else its kind's — a task
// database's "새 작업"), or "".
func mcpDatabaseUntitledWord(app core.App, dataSourceID, language string) string {
	if name, err := databaseUntitledRowNameIn(app, dataSourceID, language); err == nil && strings.TrimSpace(name) != "" {
		return name
	}
	return ""
}

// mcpUntitledTemplateName is what an answer calls a database's template
// with neither a title nor the name it was made with — in fetch's
// page_templates and among the templates a template_id may name — as the
// screen's template menu calls it (web/src/components/database/
// untitledRowName.ts useTemplateDisplayName) and the template's own fetch
// heads it: by its database's word for its untitled pages, else a new page
// in the reader's language ("새 페이지", "New page"), where it read
// "Untitled" in every language (2026-11-26). No tool takes a template by its
// name: template_id is its ID or its address.
func mcpUntitledTemplateName(app core.App, principalID, templateID string) string {
	sourceID := ""
	_ = app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_templates}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": templateID}).Row(&sourceID)
	language := mcpReaderLanguage(app, principalID)
	if name := mcpDatabaseUntitledWord(app, sourceID, language); name != "" {
		return name
	}
	return untitledNotificationPageTitleOf(language, pageNameKind{Kind: pageKindPage})
}

// mcpUntitledDataSourceNameIn is what a data source without a name is
// called in the language given, as fetch heads it (mcpDataSourceShownTitle):
// one among its database's several a new data source, any other a new
// database.
func mcpUntitledDataSourceNameIn(app core.App, language string, location mcpDataSourceLocation) string {
	words, known := notionSourceListWords[language]
	if !known {
		words = notionSourceListWords[accountLanguageEn]
	}
	if mcpDataSourceAmongSeveral(app, location) {
		return words.untitled
	}
	return words.database
}

// mcpNamedOrCalled is mcpNamedOr working out what an untitled one is called
// only while its title is blank.
func mcpNamedOrCalled(title string, untitled func() string) string {
	if strings.TrimSpace(title) == "" {
		return untitled()
	}
	return title
}
