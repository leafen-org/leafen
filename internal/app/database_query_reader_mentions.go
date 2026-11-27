package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A query reads a title or text cell as whoever queries reads it. A person's
// cell keeps a page or database mention with its target's title as written
// (mcp_reader_mentions.go), so a filter, a search or a sort matching those
// words would let someone who can't open the target learn its title by
// trying words against their own view of the cell, and a formula or rollup
// reading them would show it. To whoever queries, a mention of a page or
// database they can't open has the words they read it by wherever the query
// reads cells — its filters, search and sorts, and the formulas and rollups
// it computes, over the row and its related rows alike (unnamedMentionWords):
// on a person's screen its marks, as the app's own view reads the rows it
// holds when it filters, sorts and computes them itself; through the Public
// API or MCP, and in what a person's automation writes, "Untitled". Whoever
// may open the target matches and reads it by its title as before. The rows
// a query answers with keep their cells as stored, each reader's answer
// marking such a mention its own way (queryRowsAsRead, mcpRowsAsRead, the
// API's cells), so a change written back keeps what the cell holds for
// everyone else (content_graph_writer_mentions.go). A cell the API writes
// keeps no name for such a mention at all.
//
// The same reading decides whose related rows a query reads: a Public API
// connection or an agent reads a related database's rows only on a page its
// grant or token reaches and its person reads, and a relation to any other
// page as empty, as its row page shows them, so its filters, sorts,
// formulas and rollups never pick or order rows by what it can't read
// (database_query_related_sources.go).

// databaseQueryMentions is whose reading a query reads its cells' mentions
// and its related rows by: by default its person's own, as their screen
// reads them; a Public API connection's or an agent's reaches only the pages
// it was given.
type databaseQueryMentions struct {
	// key names a connection's or an agent's reading apart in the query's
	// hash, so the rows and order kept for one reading are never another's;
	// "" for a person's own.
	key string
	// opens is, of the pages, those the reading may open; nil for the
	// principal's own.
	opens func(app core.App, pageIDs []string) (map[string]bool, error)
	// reaches is whether the reading reaches a related page its person may
	// read; nil, every one.
	reaches func(app core.App, page pageAuthorization) bool
	// screen reads an unnamed mention by its marks, as a person's screen
	// does; otherwise "Untitled".
	screen bool
	// request is the person's own request a screen's reading is for, whose
	// language its search reads dates in (database_query_search_dates.go).
	request context.Context
	// trash reads a data source in the trash too, as an agent's reading
	// does: Notion's MCP reads one's rows (mcp_fetch_trash.go).
	trash bool
	// agentTrash is an agent's reading, which names a mention as its page
	// Markdown does: a page in the trash itself, and a database there or
	// under a page there, closed (agentTrashClosed).
	agentTrash bool
	// newest gives a query without sorts its rows newest first by their
	// created times, whatever order a view keeps them in, as a Public API
	// connection's reading does (database_query_linked.go).
	newest bool
	// uncomputed reads the rows' own values alone, none of their formulas
	// and rollups, as a SQL query's snapshot does
	// (database_sql_query_snapshot.go).
	uncomputed bool
}

// reachesRelatedPage is whether the query reads the rows of a related page
// its person may read.
func (mentions databaseQueryMentions) reachesRelatedPage(app core.App, page pageAuthorization) bool {
	return mentions.reaches == nil || mentions.reaches(app, page)
}

// screenQueryMentions is a person's reading on their screen: the app's view
// of a database.
var screenQueryMentions = databaseQueryMentions{key: "screen", screen: true}

// notionIdentityQueryMentions is a Public API connection's reading: the
// pages its person reads and its grant reaches, as notionIdentityCloses
// reads them, and the related pages its grant reaches
// (notionOAuthIdentityAllowsPage).
func notionIdentityQueryMentions(identity notionOAuthBearerIdentity) databaseQueryMentions {
	reached := []string{}
	if identity.PageScoped {
		for id, kind := range identity.ResourceIDs {
			reached = append(reached, kind+":"+id)
		}
	}
	return databaseQueryMentions{
		key: databaseQueryMentionsKey("api", identity.ClientID, identity.GrantID, reached),
		opens: func(app core.App, pageIDs []string) (map[string]bool, error) {
			open := make(map[string]bool, len(pageIDs))
			for _, pageID := range pageIDs {
				_, allowed, err := authorizeNotionPageMentionTarget(app, identity, "", identity.WorkspaceID, pageID)
				open[pageID] = err == nil && allowed
			}
			return open, nil
		},
		reaches: func(app core.App, page pageAuthorization) bool {
			return notionOAuthIdentityAllowsPage(app, identity, page)
		},
		newest: true,
	}
}

// mcpQueryMentions is an agent's reading: the pages its person may open that
// its token reaches, as its words name them (mcpReaderCloses), related pages
// too.
func mcpQueryMentions(authority mcpAuthority) databaseQueryMentions {
	return databaseQueryMentions{
		key: databaseQueryMentionsKey("mcp", authority.ConnectionID, "", mcpReachKey(authority)),
		opens: func(app core.App, pageIDs []string) (map[string]bool, error) {
			return mcpAgentOpens(app, authority, pageIDs)
		},
		reaches: func(app core.App, page pageAuthorization) bool {
			return mcpAuthorityReachesPage(app, authority, page.Page)
		},
		trash: true, agentTrash: true,
	}
}

func databaseQueryMentionsKey(kind, connection, grant string, reached []string) string {
	slices.Sort(reached)
	digest := sha256.Sum256([]byte(strings.Join(append([]string{kind, connection, grant}, reached...), "\x00")))
	return kind + ":" + hex.EncodeToString(digest[:12])
}

// withMentionsHash is a query's hash with the reading its cells and related
// rows are read by, a person's own too: a query's job kept from before they
// were read so picks no row by what a reading may not read.
func (mentions databaseQueryMentions) withMentionsHash(queryHash string) string {
	return databaseQueryHashWithHiddenRows(queryHash, []string{"reading", mentions.key})
}

// reading is the reading for the query's principal; nil, for no principal,
// reads every mention as written.
func (mentions databaseQueryMentions) reading(app core.App, principalID string) *databaseQueryMentionReading {
	opens := mentions.opens
	if opens == nil {
		if principalID == "" {
			return nil
		}
		opens = pageReader(principalID).opens
	}
	return &databaseQueryMentionReading{
		app: app, open: map[string]bool{}, screen: mentions.screen, agentTrash: mentions.agentTrash,
		opens: func(pageIDs []string) (map[string]bool, error) { return opens(app, pageIDs) },
	}
}

// databaseQueryMentionReading is what one query learned of the pages its
// cells mention: whether its reading may open each, asked a window of rows
// at once (warm) or a page at a time.
type databaseQueryMentionReading struct {
	app    core.App
	opens  func(pageIDs []string) (map[string]bool, error)
	open   map[string]bool
	closes searchMentionCloser
	screen bool
	// agentTrash: see databaseQueryMentions.
	agentTrash bool
}

// closer is how the mentions stand to the reading (memoMentionCloser).
func (reading *databaseQueryMentionReading) closer() searchMentionCloser {
	if reading.closes == nil {
		reading.closes = memoMentionCloser(func(pageID string) bool {
			open, asked := reading.open[pageID]
			if !asked {
				found, err := reading.opens([]string{pageID})
				open = err == nil && found[pageID]
				reading.open[pageID] = open
			}
			return !open
		}, reading.app)
		if reading.agentTrash {
			reading.closes = agentTrashClosed(reading.app, reading.closes)
		}
	}
	return reading.closes
}

// spansAsRead is spans with each mention the reading can't open in the
// words it reads it by, their words, and whether any was.
func (reading *databaseQueryMentionReading) spansAsRead(spans []graphTextSpan) ([]graphTextSpan, string, bool) {
	var read []graphTextSpan
	var words strings.Builder
	for index, span := range spans {
		if reach := searchMentionReach(reading.closer(), span.Mention, span.PageID, span.DatabaseID); reach != mentionOpens {
			if read == nil {
				read = slices.Clone(spans)
			}
			read[index].Text = unnamedMentionWords(reach, span.Text, reading.screen)
		}
	}
	if read == nil {
		return spans, "", false
	}
	for _, span := range read {
		words.WriteString(span.Text)
	}
	return read, words.String(), true
}

// warmMentions asks at once whether the pages the rows' title and text
// cells mention open to whoever queries, so a query over many rows reads
// them in a few lookups rather than one for each page.
func (context databaseQueryContext) warmMentions(rows map[string]map[string]graphRowValue) {
	reading := context.Mentions
	if reading == nil {
		return
	}
	asked := map[string]bool{}
	pageIDs := []string{}
	for _, values := range rows {
		for _, value := range values {
			for _, span := range value.Spans {
				if span.Mention != "page" || span.PageID == "" || asked[span.PageID] {
					continue
				}
				if _, known := reading.open[span.PageID]; !known {
					asked[span.PageID] = true
					pageIDs = append(pageIDs, span.PageID)
				}
			}
		}
	}
	for start := 0; start < len(pageIDs); start += databaseQueryEvaluationBatch {
		window := pageIDs[start:min(len(pageIDs), start+databaseQueryEvaluationBatch)]
		found, err := reading.opens(window)
		if err != nil {
			return
		}
		for _, pageID := range window {
			reading.open[pageID] = found[pageID]
		}
	}
}

// databaseQueryWindowCells are a job's window of rows' cells and their
// related rows', to warm the mentions of at once.
func databaseQueryWindowCells(rows []storedDatabaseQueryRow, related map[string]map[string]graphRowValue) map[string]map[string]graphRowValue {
	cells := maps.Clone(related)
	if cells == nil {
		cells = make(map[string]map[string]graphRowValue, len(rows))
	}
	for _, row := range rows {
		cells[row.ID] = row.Values
	}
	return cells
}

// rowAsRead is a row as the query reads it for whoever queries: in a title
// or text cell, a page or database mention they can't open has the words
// they read it by, in the cell's value and its spans, and a relation to a
// source their reading doesn't read is empty (ClosedRelations). The row
// given is left as it is.
func (context databaseQueryContext) rowAsRead(row storedDatabaseQueryRow) storedDatabaseQueryRow {
	var values map[string]graphRowValue
	for propertyID := range context.ClosedRelations {
		if _, held := row.Values[propertyID]; held {
			if values == nil {
				values = maps.Clone(row.Values)
			}
			delete(values, propertyID)
		}
	}
	for propertyID, value := range row.Values {
		if context.Mentions == nil {
			break
		}
		if len(value.Value) == 0 || value.Value[0] != '"' || !querySpansHoldMention(value.Spans) {
			continue
		}
		spans, words, hid := context.Mentions.spansAsRead(value.Spans)
		if !hid {
			continue
		}
		encoded, err := json.Marshal(words)
		if err != nil {
			continue
		}
		if values == nil {
			values = maps.Clone(row.Values)
		}
		values[propertyID] = graphRowValue{Type: value.Type, Value: encoded, Spans: spans}
	}
	if values != nil {
		row.Values = values
	}
	return row
}

// pageTitlesAsRead puts database pages' titles, as their pages keep them,
// as whoever queries reads them: a mention in a page's title cell they can't
// open has the words they read it by.
func (context databaseQueryContext) pageTitlesAsRead(app core.App, titles map[string]string) error {
	if context.Mentions == nil || len(titles) == 0 {
		return nil
	}
	ids := make([]string, 0, len(titles))
	for id := range titles {
		ids = append(ids, id)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	cells := []struct {
		RowID string `db:"row_id"`
		Spans string `db:"spans"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[row_id]], [[spans]] FROM {{leafen_database_row_values}}
		WHERE [[row_id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND [[type]] = 'title' AND [[spans]] LIKE '%"mention"%'
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&cells); err != nil {
		return err
	}
	for _, cell := range cells {
		if _, words, hid := context.Mentions.spansAsRead(databaseStoredSpans(cell.Spans)); hid {
			titles[cell.RowID] = words
		}
	}
	return nil
}
