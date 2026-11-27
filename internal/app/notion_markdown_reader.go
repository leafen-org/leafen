package app

import "github.com/pocketbase/pocketbase/core"

// notionMarkdownReader is who reads a page's Markdown: where its links point,
// whether it names things by their Leafen IDs, as the MCP tools do, and which
// pages it may open.
type notionMarkdownReader struct {
	Origin      string
	InternalIDs bool
	// PrincipalID is the person reading, whose download addresses name the
	// page's files, or with StableFiles their stable addresses, as the MCP
	// tools name them (notion_page_markdown_file_blocks.go).
	PrincipalID string
	StableFiles bool
	// Readable is whether the reader may open a page; nil opens every page.
	Readable func(app core.App, pageID string) (bool, error)
	// Sources gives a block written as unknown its address when it has one
	// (a web bookmark's, an embed's), for the server administrator's reading
	// of a page, who decides on content Notion's Markdown doesn't write.
	Sources bool
	// APIForm reads the page as the public API's Markdown does at its
	// default version, "v1" (notion_page_markdown_api_form.go), and Version2
	// as its "v2" and Notion's MCP write it (notion_page_markdown_version2.go);
	// without either, as Leafen's page Markdown — an export's, an
	// administrator's and an import's reading of a page.
	APIForm  bool
	Version2 bool
	// PeopleEmails writes a mentioned person's email in their mention's
	// address, and NoPeople writes no one's name, as the reader may read
	// people (notionIdentityReadsPeople; notion_markdown_person_mentions.go).
	PeopleEmails bool
	NoPeople     bool
	// Discussions names the threads runs of words in a block were left on, for an MCP fetch and write
	// and the API (mcp_write_discussions.go), and Threads reads its names back as those threads; nil, none.
	Discussions func(threads, blockID string) string
	Threads     func(string) string
	// LinksMentioned is a writer that makes the links it writes to the
	// workspace's pages and databases their mentions after reading
	// (notionMarkdownSitePages.LinksMentioned).
	LinksMentioned bool
	// NamesUntitled names a mention of a page without a title as an
	// agent's answers call it (mcp_untitled_names.go), as an MCP tool's
	// reader does; any other leaves its words empty.
	NamesUntitled bool
}
