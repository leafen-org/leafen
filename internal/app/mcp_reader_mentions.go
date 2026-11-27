package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// What a reader reads names only the pages it may open: a person, the pages
// they read; an agent, those its person may open that its token reaches
// (mcp_token_tools.go), as its Markdown names the trash
// (mention_agent_trash_closer.go). A page's words keep each mention with its target's
// title as written, so every answer that carries words leaves a mention of a
// page or database its reader can't open unnamed. One it can't open is
// either closed — there, not the reader's to open — or missing: nothing
// answers to its ID (a page deleted for good, an ID an import or an API
// wrote). How an unnamed mention reads is decided here (unnamedMentionWords):
//   - on a person's screen, in marks the app draws as Notion draws such a
//     mention (seen 2026-11-09): a closed one an eye crossed out and
//     "사용 권한 없음", a missing one a page and "찾을 수 없음", each in its
//     own formatting (collaboration_reader_mentions.go);
//   - through the Public API or MCP, "Untitled", as Notion's API gives an
//     integration a mention of a page it can't reach and of one that isn't
//     there alike (seen 2026-11-09) — an agent's Markdown, comment or cell, a
//     search result's database page title — and, in Notion's rich text, a
//     page mention by its ID with no formatting (notion_block_rich_text.go);
//     or not at all: search keeps no mention's words
//     (person_mention_names.go). A page's Markdown names its mentions by
//     its own rule (notion_page_markdown_mentions.go).

// mentionReach is how what a page or database mention names stands to its
// reader.
type mentionReach uint8

const (
	// mentionOpens: the reader may open it.
	mentionOpens mentionReach = iota
	// mentionClosed: it is there, not the reader's to open.
	mentionClosed
	// mentionMissing: nothing answers to its ID.
	mentionMissing
)

// unnamedMentionWords is the words of a mention its reader can't open, for
// their screen (marks one for each UTF-16 unit of its words, so the text
// keeps its length) or for an API or MCP ("Untitled").
func unnamedMentionWords(reach mentionReach, written string, screen bool) string {
	switch {
	case !screen:
		return notionUnresolvedMentionTitle
	case reach == mentionMissing:
		return mentionMarks(missingMentionMark, written)
	default:
		return mentionMarks(closedMentionMark, written)
	}
}

// wordsReader is who reads: a person, or an agent through its token.
type wordsReader struct {
	principalID string
	// token is an agent's, nil for a person reading for themselves.
	token *mcpAuthority
	// opening is a person reading a page they opened, to whom a page opens
	// as it does from a link: in the trash too, and through a share search
	// keeps out of its results.
	opening bool
}

// personReader is a person reading for themselves.
func personReader(principalID string) wordsReader {
	return wordsReader{principalID: principalID}
}

// pageReader is a person reading a page they opened
// (collaboration_reader_mentions.go).
func pageReader(principalID string) wordsReader {
	return wordsReader{principalID: principalID, opening: true}
}

// mcpReader is an agent reading through its token.
func mcpReader(authority mcpAuthority) wordsReader {
	return wordsReader{principalID: authority.PrincipalID, token: &authority}
}

// opens is, of the pages, those the reader may open. To a person reading a
// page they opened, a page of a copy they are still making opens too, as it
// will once made: their copy's body read while it is made named its sub-pages
// closed, and they stayed hidden after it was done (2026-11-11).
func (reader wordsReader) opens(app core.App, pageIDs []string) (map[string]bool, error) {
	if reader.opening {
		open, err := openablePageIDs(app, reader.principalID, pageIDs)
		if err != nil {
			return open, err
		}
		closed := make([]string, 0, len(pageIDs))
		for _, pageID := range pageIDs {
			if !open[pageID] {
				closed = append(closed, pageID)
			}
		}
		toCome, err := pagesOfOwnCopyToCome(app, reader.principalID, closed)
		for pageID := range toCome {
			open[pageID] = true
		}
		return open, err
	}
	readable, err := readablePageIDs(app, reader.principalID, pageIDs)
	if err != nil || reader.token == nil {
		return readable, err
	}
	for pageID := range readable {
		if !mcpAuthorityReachesPageID(app, *reader.token, pageID) {
			delete(readable, pageID)
		}
	}
	return readable, nil
}

// closes is how the mentions the reader reads stand to it: a page or
// database (through the page holding it) it may not open is unnamed. It
// keeps what it learned for the one answer it serves. To a person reading a
// page they opened, a page of their copy still being made is missing in a
// mention and opens as a sub-page (ownCopyToComeCloser).
func (reader wordsReader) closes(app core.App) searchMentionCloser {
	if reader.opening && reader.token == nil {
		return ownCopyToComeCloser(app, reader.principalID)
	}
	// An agent's words name a mention as its page Markdown does
	// (mention_agent_trash_closer.go).
	if reader.token != nil {
		authority := *reader.token
		return agentTrashClosed(app, memoMentionCloser(func(pageID string) bool {
			open, err := mcpAgentOpens(app, authority, []string{pageID})
			return err != nil || !open[pageID]
		}, app))
	}
	// A database in the trash itself reads closed, as a page there does.
	return trashedDatabasesClosed(app, memoMentionCloser(func(pageID string) bool {
		open, err := reader.opens(app, []string{pageID})
		return err != nil || !open[pageID]
	}, app))
}

// mcpAuthorityOpensPageID says whether an agent's reading may name a page:
// its person reads it and its token reaches it.
func mcpAuthorityOpensPageID(app core.App, authority mcpAuthority, pageID string) bool {
	if pageID == "" {
		return false
	}
	open, err := mcpReader(authority).opens(app, []string{pageID})
	return err == nil && open[pageID]
}

// mcpReaderCloses is how the mentions an agent reads stand to it.
func mcpReaderCloses(app core.App, authority mcpAuthority) searchMentionCloser {
	return mcpReader(authority).closes(app)
}

// memoMentionCloser makes a reader's closer from whether a page is closed
// to it, a database standing as the page holding it does, keeping what it
// learned for the one answer it serves. A page nothing answers to — or a
// database with no page holding it — is missing to every reader.
func memoMentionCloser(pageClosed func(pageID string) bool, app core.App) searchMentionCloser {
	known := map[string]mentionReach{}
	return func(mention, targetID string) mentionReach {
		key := mention + ":" + targetID
		if reach, seen := known[key]; seen {
			return reach
		}
		pageID := targetID
		if mention == "database" {
			pageID = mcpDatabaseHolderPageID(app, targetID)
		}
		reach := mentionOpens
		switch {
		case pageID == "":
			reach = mentionMissing
		case pageClosed(pageID):
			reach = mentionClosed
			if !pageRecordExists(app, pageID) {
				reach = mentionMissing
			}
		}
		known[key] = reach
		return reach
	}
}

// pageRecordExists reports a page, a database page among them, by its ID;
// one it fails to look for counts as there. A page deleted for good is gone
// for every reader at once (page_deletion_hold.go), its 30 days held or not,
// so a mention of it, or of a page under it, is "찾을 수 없음" too.
func pageRecordExists(app core.App, pageID string) bool {
	workspace := ""
	if err := app.DB().NewQuery(`SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&workspace); err != nil {
		return !errors.Is(err, sql.ErrNoRows)
	}
	shown := 0
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_pages}} [[page]] WHERE [[page]].[[id]] = {:id} AND ` +
		pageNotHeldForDeletionSQL("[[page]]", "{:workspace}")).
		Bind(dbx.Params{"id": pageID, "workspace": workspace}).Row(&shown)
	return err != nil || shown > 0
}

// mcpDatabaseHolderPageID is the page holding a database, "" for none; a
// data source's mention, named by the source's address, stands as the page
// holding the source's database does (data_source_mentions.go).
func mcpDatabaseHolderPageID(app core.App, databaseID string) string {
	if pageID, found := mentionedDataSourceHolderPage(app, databaseID); found {
		return pageID
	}
	pageID := ""
	_ = app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_database_containers}}
		WHERE [[id]] = {:id} AND [[kind]] = 'source' LIMIT 1
	`).Bind(dbx.Params{"id": databaseID}).Row(&pageID)
	return pageID
}

// mcpCellAsRead is a title or text cell as an agent reads it: its words
// again from its spans when one of its mentions is closed to the agent.
func mcpCellAsRead(closed searchMentionCloser, property storedNotionDatabaseRowPageProperty) storedNotionDatabaseRowPageProperty {
	if !property.Spans.Valid || !strings.Contains(property.Spans.String, `"mention"`) {
		return property
	}
	if words, hid := spansAsRead(closed, databaseStoredSpans(property.Spans.String)); hid {
		if encoded, err := json.Marshal(words); err == nil {
			property.Value.String, property.Value.Valid = string(encoded), true
		}
	}
	return property
}

// mcpRowsAsRead puts queried rows' cells as an agent reads them, before any
// answer is made of them: a mention closed to it reads "Untitled", in a
// title or text cell's words and spans alike, and a relation names only the
// rows it may open (mcpRelationsAsRead).
func mcpRowsAsRead(app core.App, authority mcpAuthority, workspaceID string, rows []databaseQueryRow) {
	closed := mcpReaderCloses(app, authority)
	// The related rows its relation cells name, judged below, read at once
	// (database_relation_row_prime.go).
	related := []string{}
	for _, row := range rows {
		for _, value := range row.Values {
			if value.Type == graphValueTypeRelation {
				related = append(related, graphRelationRowIDs(value.Value)...)
			}
		}
	}
	if err := primeRelationRowsRemembered(app, related); err != nil {
		app.Logger().Warn("leafen could not read an answer's related rows at once", "error", err)
	}
	relations := mcpRelationsAsRead(app, authority, workspaceID)
	for _, row := range rows {
		for propertyID, value := range row.Values {
			if value.Type == graphValueTypeRelation {
				if read, changed := relations(value.Value); changed {
					value.Value = read
					row.Values[propertyID] = value
				}
				continue
			}
			words, hid := spansAsRead(closed, value.Spans)
			if !hid {
				continue
			}
			if encoded, err := json.Marshal(words); err == nil {
				spans := slices.Clone(value.Spans)
				for index := range spans {
					spans[index].Text = searchMentionWords(closed, spans[index].Mention, spans[index].PageID, spans[index].DatabaseID, spans[index].Text)
				}
				value.Value, value.Spans = encoded, spans
				row.Values[propertyID] = value
			}
		}
	}
}

// mcpRelationsAsRead is a relation cell as an agent reads it: the rows it
// may open (notionRelationRowOpen), one in the trash among them, as Notion's
// MCP fetch gives a trashed related page (seen 2026-11-10); a row it can't
// open stays in the cell when it writes it
// (database_relation_closed_targets.go). It keeps what it learned for the
// one answer it serves.
func mcpRelationsAsRead(app core.App, authority mcpAuthority, workspaceID string) func(json.RawMessage) (json.RawMessage, bool) {
	shown := mcpRelationRowOpener(app, authority, workspaceID)
	return func(value json.RawMessage) (json.RawMessage, bool) {
		ids := graphRelationRowIDs(value)
		kept := make([]string, 0, len(ids))
		for _, id := range ids {
			if open, err := shown(id); err == nil && open {
				kept = append(kept, id)
			}
		}
		if len(kept) == len(ids) {
			return value, false
		}
		encoded, err := json.Marshal(kept)
		if err != nil {
			return value, false
		}
		return encoded, true
	}
}

// mcpRowTitleAsRead is a database page's title as an agent reads it: its
// title cell's words, or a full-page database's name's (page_name_mentions.go),
// a mention closed to the agent "Untitled".
func mcpRowTitleAsRead(app core.App, closed searchMentionCloser, rowID, title string) string {
	spans, mentions := pageNameSpans(app, rowID)
	if !mentions {
		return title
	}
	if words, hid := spansAsRead(closed, spans); hid {
		return words
	}
	return title
}

// rowTitleSpans is a database page's title cell's spans, and whether they
// hold a mention.
func rowTitleSpans(app core.App, rowID string) ([]graphTextSpan, bool) {
	spans := ""
	if err := app.DB().NewQuery(`
		SELECT [[value]].[[spans]] FROM {{leafen_database_row_values}} [[value]]
		JOIN {{leafen_database_properties}} [[property]] ON [[property]].[[id]] = [[value]].[[property_id]]
		WHERE [[value]].[[row_id]] = {:rowId} AND [[property]].[[type]] = 'title' LIMIT 1
	`).Bind(dbx.Params{"rowId": rowID}).Row(&spans); err != nil || !strings.Contains(spans, `"mention"`) {
		return nil, false
	}
	return databaseStoredSpans(spans), true
}

// mcpMentionReaches narrows the pages an agent's writing may mention to
// those its token reaches; nil, for a token not narrowed, leaves every page
// its person reads.
func mcpMentionReaches(authority mcpAuthority) func(core.App, pageAuthorization) bool {
	if !authority.PagesNarrowed {
		return nil
	}
	return func(app core.App, target pageAuthorization) bool {
		return mcpAuthorityReachesPage(app, authority, target.Page)
	}
}

// mcpTextHoldsClosed reports a block's text — its live text, or the spans
// its row keeps — holding a mention closed to the agent; a missing one names
// nothing it may not read.
func mcpTextHoldsClosed(deltas []crdt.Delta, content string, closed searchMentionCloser) bool {
	if closed == nil {
		return false
	}
	for _, delta := range deltas {
		mention, _ := delta.Attributes["mention"].(string)
		pageID, _ := delta.Attributes["pageId"].(string)
		databaseID, _ := delta.Attributes["databaseId"].(string)
		if searchMentionReach(closed, mention, pageID, databaseID) == mentionClosed {
			return true
		}
	}
	if !strings.Contains(content, `"mention"`) {
		return false
	}
	stored := struct {
		Rich []graphTextSpan `json:"rich"`
	}{}
	_ = json.Unmarshal([]byte(content), &stored)
	for _, span := range stored.Rich {
		if searchMentionReach(closed, span.Mention, span.PageID, span.DatabaseID) == mentionClosed {
			return true
		}
	}
	return false
}
