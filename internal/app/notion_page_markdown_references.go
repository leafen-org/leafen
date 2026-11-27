package app

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's Markdown names its sub-pages and databases with <page> and
// <database> tags, as Notion's enhanced Markdown does: a tag holds the page's
// or database's address and title, and a sub-page the reader can't open stays
// an unknown block. Markdown written back keeps each sub-page or database its
// tag names where the tag now stands, and a tag's new title renames its page
// or database. A <page> tag without an address makes a sub-page, a
// <database inline="true"> tag an inline database and a <database> tag a
// full-page database, where the tag stands, and a <page> tag naming a page
// elsewhere mentions it. Leaving one out deletes it only when deleting
// content is allowed. A linked database keeps its source's title.

// notionMarkdownReferenceBlock is the type a tag line parses to until the
// Markdown's plan resolves it to the block it names.
const notionMarkdownReferenceBlock = "leafen:markdown-reference"

// notionMarkdownReference is a sub-page or database a page's Markdown names
// with a tag, and the block that holds it in the page's body.
type notionMarkdownReference struct {
	BlockID  string
	ParentID string
	Type     string
	Content  json.RawMessage
	Tag      string
	Title    string
	// Spans are its title's formatting and mentions, none for plain words,
	// and Written its title as its tag holds it for the reader
	// (notion_page_markdown_reference_titles.go).
	Spans   []graphTextSpan
	Written string
	// ID, URL and DataSourceID name it as the reader does.
	ID           string
	URL          string
	DataSourceID string
	Inline       bool
	// SourceID is its first data source's Leafen ID, and OwnSource whether
	// that source is the page's own rather than linked from another page.
	SourceID  string
	OwnSource bool
	// Several is a database of two or more sources of its own, named apart
	// from them (databaseNameFrom): its tag's title is its own name, which
	// a new title renames, its sources keeping theirs.
	Several bool
	// Linked is an inline database showing a source of another block, and
	// OwnTitle the name it gives itself, which the public API's Markdown
	// writes as Notion's does (seen 2026-11-13: a linked view's tag with no
	// title and no data-source-url).
	Linked   bool
	OwnTitle string
	// Deleted is a database whose own sources are all in the trash where
	// they stand (database_source_trash_in_place_record.go).
	Deleted bool
	// Wiki is a wiki's database, whose tag says so (wikiAttribute).
	Wiki bool
}

// notionMarkdownTag is a <page> or <database> tag line.
type notionMarkdownTag struct {
	Tag           string `json:"tag"`
	URL           string `json:"url,omitempty"`
	DataSourceURL string `json:"dataSourceUrl,omitempty"`
	Inline        string `json:"inline,omitempty"`
	// Title is the words its title reads as, Spans its formatting and
	// mentions as read, none for plain words, and Written its title as
	// written.
	Title   string          `json:"title"`
	Spans   []graphTextSpan `json:"spans,omitempty"`
	Written string          `json:"written,omitempty"`
	// Trimmed is a page's title in "v1", which drops the spaces at its
	// ends (notionMarkdownReferenceLine).
	Trimmed bool `json:"trimmed,omitempty"`
	// Line is an <unknown/> line as written.
	Line string `json:"line,omitempty"`
}

// notionMarkdownRename is a sub-page a tag gives a new title, with the first
// data source of a full-page database of one source, or an inline database
// of several sources by its block (DatabaseBlockID), which takes a name of
// its own: its words, and its formatting and mentions, none for plain words.
type notionMarkdownRename struct {
	PageID          string
	Title           string
	Spans           []graphTextSpan
	SourceID        string
	DatabaseBlockID string
	// Trimmed drops the spaces at its spans' ends (notionMarkdownTag).
	Trimmed bool
}

var (
	notionMarkdownTagPattern = regexp.MustCompile(
		`^<(page|database)((?:\s+[A-Za-z][A-Za-z-]*="[^"]*")*)\s*>(.*)</(page|database)>$`)
	notionMarkdownAttributePattern = notionMarkdownAttributeAt
	notionMarkdownEscaped          = strings.NewReplacer(
		`\`, `\\`, `*`, `\*`, `~`, `\~`, "`", "\\`", `$`, `\$`, `[`, `\[`, `]`, `\]`,
		`<`, `\<`, `>`, `\>`, `{`, `\{`, `}`, `\}`, `|`, `\|`, `^`, `\^`,
	)
	// notionMarkdownAttributeEscaped is an attribute's value as "v2" and the
	// MCP write it, ' too (seen 2026-11-11: source="…a&#39;b&amp;c…" on a
	// bookmark, embed, file and the media).
	notionMarkdownAttributeEscaped = strings.NewReplacer(`&`, `&amp;`, `"`, `&quot;`, `<`, `&lt;`, `>`, `&gt;`, `'`, `&#39;`)
	// notionMarkdownAPIAttributeEscaped is one as the public API's "v1"
	// writes a mention's or a synced block's address, ' as it is.
	notionMarkdownAPIAttributeEscaped = strings.NewReplacer(`&`, `&amp;`, `"`, `&quot;`, `<`, `&lt;`, `>`, `&gt;`)
	// notionMarkdownVersion1AttributeEscaped leaves & as it is, as "v1"
	// writes an embed's address.
	notionMarkdownVersion1AttributeEscaped = strings.NewReplacer(`"`, `&quot;`, `<`, `&lt;`, `>`, `&gt;`)
)

// line is the reference's tag.
func (reference notionMarkdownReference) line() string {
	title := reference.writtenTitle()
	address := notionMarkdownAttributeEscaped.Replace(reference.URL)
	// Its title on a line of its own, as Notion's MCP reads it (seen
	// 2026-11-04); none, its tags on one line, as Notion's "v2" writes them
	// (seen 2026-11-13: "<page url=…></page>", "<database …></database>").
	apart := "\n" + title + "\n"
	if title == "" {
		apart = ""
	}
	if reference.Tag == "page" {
		return `<page url="` + address + `">` + apart + "</page>"
	}
	// A linked one is its view alone, as Notion's MCP writes it (seen
	// 2026-11-14: `<database url="…" inline="true"></database>`).
	source := ` data-source-url="collection://` + notionMarkdownAttributeEscaped.Replace(reference.DataSourceID) + `"`
	if reference.Linked {
		source, apart = "", ""
		if own := notionMarkdownEscaped.Replace(reference.OwnTitle); own != "" {
			apart = "\n" + own + "\n"
		}
	}
	return `<database url="` + address + `"` + source + ` inline="` +
		strconv.FormatBool(reference.Inline) + `"` + reference.wikiAttribute() + `>` + apart + "</database>"
}

// apiLine is the same reference as the public API's Markdown reads it.
func (reference notionMarkdownReference) apiLine() string {
	title := reference.writtenTitle()
	address := notionMarkdownAttributeEscaped.Replace(reference.URL)
	if reference.Tag == "page" {
		return `<page url="` + address + `">` + title + `</page>`
	}
	deleted := ""
	if reference.Deleted {
		// As Notion's (seen 2026-11-11).
		deleted = " deleted"
	}
	source := ` data-source-url="collection://` + notionMarkdownAttributeEscaped.Replace(reference.DataSourceID) + `"`
	if reference.Linked {
		source, title = "", notionMarkdownEscaped.Replace(reference.OwnTitle)
	}
	return `<database url="` + address + `" inline="` + strconv.FormatBool(reference.Inline) + `"` + deleted +
		source + reference.wikiAttribute() + `>` + title + `</database>`
}

// parseNotionMarkdownTag reads a line that is a <page> or <database> tag,
// its title as written.
func parseNotionMarkdownTag(line string) (notionMarkdownTag, bool) {
	match := notionMarkdownTagPattern.FindStringSubmatch(strings.TrimSpace(line))
	if match == nil || match[1] != match[4] {
		return notionMarkdownTag{}, false
	}
	tag := notionMarkdownTag{Tag: match[1], Title: unescapeNotionMarkdown(match[3]), Written: match[3]}
	for _, attribute := range notionMarkdownAttributePattern.FindAllStringSubmatch(match[2], -1) {
		value := strings.NewReplacer(`&quot;`, `"`, `&lt;`, `<`, `&gt;`, `>`, `&amp;`, `&`).Replace(attribute[2])
		switch attribute[1] {
		case "url":
			tag.URL = value
		case "data-source-url":
			tag.DataSourceURL = value
		case "inline":
			tag.Inline = value
		}
	}
	return tag, true
}

func unescapeNotionMarkdown(value string) string {
	var result strings.Builder
	escaped := false
	for _, character := range value {
		if !escaped && character == '\\' {
			escaped = true
			continue
		}
		escaped = false
		result.WriteRune(character)
	}
	return result.String()
}

// notionMarkdownReferenceLine parses a Markdown line that is a tag into a
// reference block for the Markdown's plan to resolve, its title read as the
// text of its Markdown ("v1"'s when version1 is set). A title keeps the
// spaces at its ends but a page's in "v1", as Notion's Markdown takes them
// (seen 2026-11-13: "v1" " 하위 끝 " names a page "하위 끝" and " 디비 끝 "
// a database " 디비 끝 "; "v2" "  v2쪽  " names a page so).
func notionMarkdownReferenceLine(line string, version1 bool) (string, json.RawMessage, bool) {
	tag, ok := parseNotionMarkdownTag(line)
	if !ok {
		return "", nil, false
	}
	if version1 && tag.Tag == "page" {
		tag.Written, tag.Trimmed = strings.TrimSpace(tag.Written), true
	}
	tag.Title, tag.Spans = notionMarkdownTagTitle(tag.Written, version1)
	content, err := json.Marshal(tag)
	if err != nil {
		return "", nil, false
	}
	return notionMarkdownReferenceBlock, content, true
}

// notionMarkdownKey is an ID as tags are matched by it: the last part of an
// address, without dashes or case.
func notionMarkdownKey(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "collection" {
		value = parsed.Path
	}
	value = strings.TrimPrefix(value, "collection://")
	if index := strings.LastIndex(value, "/"); index >= 0 {
		value = value[index+1:]
	}
	return strings.ToLower(strings.ReplaceAll(value, "-", ""))
}

// loadNotionMarkdownReferences reads the sub-pages and databases of a page's
// blocks that its reader may open, by block.
func loadNotionMarkdownReferences(
	app core.App,
	owner, workspaceID, pageID string,
	blocks []workspaceExportMarkdownBlock,
	reader notionMarkdownReader,
) (map[string]notionMarkdownReference, error) {
	readable := func(id string) (bool, error) {
		if reader.Readable == nil {
			return true, nil
		}
		return reader.Readable(app, id)
	}
	linkTo := func(id string) (string, error) {
		if reader.InternalIDs {
			return notionMentionURL(reader.Origin, id), nil
		}
		publicID, err := notionPagePublicID(app, id)
		return notionMentionURL(reader.Origin, publicID), err
	}
	name := func(kind, id string) (string, error) {
		if reader.InternalIDs && (kind != notionObjectIdentityDataSource || !reader.Version2) {
			return id, nil
		}
		if kind == "page" {
			return notionPagePublicID(app, id)
		}
		return ensureNotionObjectIdentity(app, kind, workspaceID, id)
	}
	notionMarkdownSubPagesAhead(app, blocks)
	references := map[string]notionMarkdownReference{}
	for _, block := range blocks {
		// A sub-page's block, or a wiki's link, is its page (notion_page_markdown_wiki_pages.go).
		reference, standsFor, err := notionMarkdownBlockReference(app, owner, block)
		if err != nil {
			return nil, err
		}
		switch {
		case standsFor != "":
			page, err := findRecordRemembered(app, pagesCollection, standsFor)
			if err != nil || page.GetBool("in_trash") {
				continue
			}
			if ok, err := readable(page.Id); err != nil || !ok {
				if err != nil {
					return nil, err
				}
				continue
			}
			if reference.URL, err = linkTo(page.Id); err != nil {
				return nil, err
			}
			reference.Tag, reference.Title = "page", page.GetString("title")
			reference.Spans = ordinaryPageTitleSpans(page)
			if reference.ID, err = name("page", page.Id); err != nil {
				return nil, err
			}
			// A full-page database, or a wiki, is its database (notionMarkdownPageDatabase).
			if database, wiki, found, err := notionMarkdownPageDatabase(app, owner, page, readable); err != nil {
				return nil, err
			} else if found {
				// Named by its page, as its child database block is
				// (loadFullPageDatabase): a second source keeps its own.
				reference.Tag, reference.Wiki = "database", wiki
				if reference.Deleted, err = databaseKeepsOnlySourcesTrashedInPlace(app, database.ContainerID); err != nil {
					return nil, err
				}
				if !wiki {
					reference.Spans = databasePageTitleSpans(app, owner, page.Id, reference.Title)
				}
				reference.SourceID = database.DataSourceID
				// One of several sources is named by its page alone (databaseNameFrom).
				names, err := databaseContainerNames(app, []string{database.ContainerID})
				if err != nil {
					return nil, err
				}
				reference.Several = names[database.ContainerID].Several
				if reference.ID, err = name(notionObjectIdentityDatabase, database.ContainerID); err != nil {
					return nil, err
				}
				if reference.DataSourceID, err = name(notionObjectIdentityDataSource, database.DataSourceID); err != nil {
					return nil, err
				}
				// Its address is its database's ID, as an inline one's
				// is, as Notion's API (seen 2026-11-11: the child_database
				// block's ID, not its page's); by Leafen's IDs — the MCP
				// tools' — its one address (databaseAppAddress).
				reference.URL = notionMentionURL(reader.Origin, reference.ID)
				if reader.InternalIDs {
					reference.URL = databaseAppAddress(reader.Origin, database.ContainerID)
				}
			}
		case block.Type == graphBlockTypeInlineDatabase:
			database, found, err := loadNotionMarkdownDatabase(app, owner, pageID, block.ID, readable)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			reference.Tag, reference.Inline = "database", true
			reference.Title, reference.Spans, reference.Several = notionMarkdownInlineDatabaseTitle(app, database)
			if reference.Deleted, err = databaseKeepsOnlySourcesTrashedInPlace(app, database.ContainerID); err != nil {
				return nil, err
			}
			reference.SourceID, reference.OwnSource = database.DataSourceID, database.SourcePageID == pageID
			if reference.Linked = database.Kind == databaseContainerKindLinked; reference.Linked {
				reference.OwnTitle = strings.TrimSpace(mapString(block.Content, "linkedTitle"))
			}
			if reference.URL, err = linkTo(pageID); err != nil {
				return nil, err
			}
			if reference.ID, err = name(notionObjectIdentityDatabase, database.ContainerID); err != nil {
				return nil, err
			}
			// One waiting for its source names none (notionMarkdownWaitingLink).
			if database.DataSourceID != "" {
				if reference.DataSourceID, err = name(notionObjectIdentityDataSource, database.DataSourceID); err != nil {
					return nil, err
				}
			}
			// An inline database's address is its own ID, as Notion's.
			reference.URL = notionMentionURL(reader.Origin, reference.ID)
		default:
			continue
		}
		references[block.ID] = reference
	}
	return references, nil
}

type notionMarkdownDatabase struct {
	ContainerID  string `db:"container_id"`
	Kind         string `db:"kind"`
	Title        string `db:"title"`
	DataSourceID string `db:"data_source_id"`
	SourcePageID string `db:"source_page_id"`
}

// loadNotionMarkdownDatabase reads the database a page's block holds, or the
// database a full-page database's page is when blockID is empty, titled by
// its first data source that names it (databaseNamingSources). A linked
// database whose source the reader can't open, and one in the trash, is none.
func loadNotionMarkdownDatabase(
	app core.App, owner, pageID, blockID string, readable func(string) (bool, error),
) (notionMarkdownDatabase, bool, error) {
	if waiting, found, err := notionMarkdownWaitingLink(app, owner, pageID, blockID); err != nil || found {
		return waiting, found, err
	}
	where := `[[container]].[[block_id]] = {:blockId} AND [[container]].[[kind]] IN ('source','linked')`
	if blockID == "" {
		where = `[[container]].[[kind]] = 'source' AND [[container]].[[parent_block_id]] = ''`
	}
	rows := []notionMarkdownDatabase{}
	if err := app.DB().NewQuery(`
		SELECT [[container]].[[id]] AS [[container_id]], [[container]].[[kind]], [[source]].[[title]], [[source]].[[id]] AS [[data_source_id]],
			[[source]].[[page_id]] AS [[source_page_id]]
		FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[container_id]] = [[container]].[[id]] AND [[membership]].[[owner]] = [[container]].[[owner]]
		JOIN {{leafen_data_sources}} AS [[source]]
			ON [[source]].[[id]] = [[membership]].[[data_source_id]] AND [[source]].[[owner]] = [[container]].[[owner]]
		WHERE [[container]].[[owner]] = {:owner} AND [[container]].[[page_id]] = {:pageId} AND ` + where + `
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
				WHERE [[trash]].[[container_id]] IN ([[container]].[[id]], [[container]].[[source_container_id]])
			)
		ORDER BY ` + databaseSourceGoneWithViewsSQL("[[container]].[[id]]", "[[membership]].[[data_source_id]]") + `,
			[[membership]].[[position]], [[source]].[[id]] LIMIT 1
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "blockId": blockID}).All(&rows); err != nil || len(rows) == 0 {
		return notionMarkdownDatabase{}, false, err
	}
	if rows[0].SourcePageID != pageID {
		if ok, err := readable(rows[0].SourcePageID); err != nil || !ok {
			return notionMarkdownDatabase{}, false, err
		}
	}
	return rows[0], true, nil
}

// notionIdentityMarkdownReader is a Public API connection as a page's
// Markdown reader: its links open pages on this server, it opens the
// sub-pages its connection may read, it reads the people its connection may
// (notionIdentityReadsPeople), and it reads the Markdown version its request
// names (notionMarkdownVersion2).
func notionIdentityMarkdownReader(
	event *core.RequestEvent, identity notionOAuthBearerIdentity, markdownVersion string,
) (notionMarkdownReader, error) {
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return notionMarkdownReader{}, err
	}
	return notionIdentityMarkdownReaderAt(event.App, origin, identity, markdownVersion), nil
}

// notionIdentityMarkdownReaderAt is notionIdentityMarkdownReader for this
// server at origin.
func notionIdentityMarkdownReaderAt(
	app core.App, origin string, identity notionOAuthBearerIdentity, markdownVersion string,
) notionMarkdownReader {
	version2 := markdownVersion == notionMarkdownVersion2
	people, emails := notionIdentityReadsPeople(app, identity)
	return notionMarkdownReader{Origin: origin, APIForm: !version2, Version2: version2, PrincipalID: identity.PrincipalID,
		PeopleEmails: emails, NoPeople: !people, Readable: func(app core.App, pageID string) (bool, error) {
			current, err := authorizePage(app, identity.PrincipalID, pageID, permissionWorkspaceRead)
			if isPermissionDenied(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			return current.Workspace.WorkspaceID == identity.WorkspaceID &&
				notionOAuthIdentityAllowsPage(app, identity, current), nil
		}}
}

// principalMarkdownReader is someone reading a page's Markdown by Leafen's
// IDs, as the MCP tools (mcpMarkdownReader) and an import do.
func principalMarkdownReader(principalID string) notionMarkdownReader {
	return notionMarkdownReader{InternalIDs: true, PrincipalID: principalID, Readable: func(app core.App, pageID string) (bool, error) {
		_, err := authorizePage(app, principalID, pageID, permissionWorkspaceRead)
		if isPermissionDenied(err) {
			return false, nil
		}
		return err == nil, err
	}}
}
