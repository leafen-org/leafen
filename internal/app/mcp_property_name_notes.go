package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The answers that make or change a data source's properties say the names
// they didn't show (2026-11-24: an agent kept calling a relation's other side
// "메모", which the server had made "메모 1" beside a text "메모", as the
// answer didn't say). A two-way relation's other side is a property of
// another data source, which the answer's schema leaves out: notion-create-
// database and notion-update-data-source say, of each two-way relation they
// made or named anew, the name its other side has there. A name taken there
// gets the first free number, as a property's does when made in Leafen
// (distinctDatabasePropertyName) and as Notion's API names it and says in
// its answer (seen 2026-11-24, "메모" → "메모 1"); a rename into a name this
// data source has takes one too. Where a name isn't the one asked for, the
// note says why.

// mcpPairNameWait bounds how long an answer waits for the server to make the
// other sides a change asked for (database_relation_pair_runner.go), which it
// does right after; past it the answer gives the name each will take.
const mcpPairNameWait = 2 * time.Second

// mcpTwoWaySide is a two-way relation: the data source its other side is on,
// that side's property, and the name asked for it.
type mcpTwoWaySide struct {
	Target, PairID, Asked string
}

// mcpSourceProperties reads a data source's live properties: each one's
// name, and the two-way relations' other sides.
func mcpSourceProperties(app core.App, dataSourceID string) (map[string]string, map[string]mcpTwoWaySide, error) {
	rows := []struct {
		ID     string `db:"id"`
		Name   string `db:"name"`
		Type   string `db:"type"`
		Config string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:source} AND ` + databasePropertyNotTrashedSQL("[[id]]"),
	).Bind(dbx.Params{"source": dataSourceID}).All(&rows); err != nil {
		return nil, nil, err
	}
	names, sides := map[string]string{}, map[string]mcpTwoWaySide{}
	for _, row := range rows {
		names[row.ID] = row.Name
		config := map[string]any{}
		if row.Type != graphValueTypeRelation || json.Unmarshal([]byte(row.Config), &config) != nil {
			continue
		}
		if pair := databaseRelationPairOf(row.ID, dataSourceID, row.Type, config); pair != nil && !pair.selfPaired(row.ID) {
			asked, _ := config["reciprocalName"].(string)
			sides[row.ID] = mcpTwoWaySide{Target: pair.TargetDataSourceID, PairID: pair.PairPropertyID, Asked: strings.TrimSpace(asked)}
		}
	}
	return names, sides, nil
}

// mcpPropertyNameNotes says, of the two-way relations a call made or named
// anew, the name each other side has, and of the properties it renamed into
// a name taken, the name each took. beforeSides are the source's two-way
// relations as the call found them, none for one it made; renamed the names
// asked, by property.
func mcpPropertyNameNotes(
	ctx context.Context, app core.App, dataSourceID string,
	beforeSides map[string]mcpTwoWaySide, renamed map[string]string,
) []string {
	names, sides, err := mcpSourceProperties(app, dataSourceID)
	if err != nil {
		return nil
	}
	notes := []string{}
	for _, id := range mcpByName(names, renamed) {
		if name, asked := names[id], renamed[id]; name != asked {
			notes = append(notes, fmt.Sprintf("The property renamed %q is %q: %q is another property's name on this data source, so it took the first free number.", asked, name, asked))
		}
	}
	made := map[string]mcpTwoWaySide{}
	for id, side := range sides {
		if beforeSides[id] != side {
			made[id] = side
		}
	}
	mcpAwaitPairsMade(ctx, app, made)
	ownerID := ""
	_ = app.DB().NewQuery(`SELECT [[owner]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": dataSourceID}).Row(&ownerID)
	for _, id := range mcpByName(names, made) {
		side := made[id]
		asked := side.Asked
		if asked == "" {
			// None asked: this data source's name, as the server names it.
			if asked, err = databaseRelationPairDefaultName(app, ownerID, dataSourceID); err != nil {
				continue
			}
		}
		name, verb := "", "is"
		if err := app.DB().NewQuery(`SELECT [[name]] FROM {{leafen_database_properties}} WHERE [[id]] = {:id} AND [[data_source_id]] = {:target}`).
			Bind(dbx.Params{"id": side.PairID, "target": side.Target}).Row(&name); err != nil {
			if name, err = databaseRelationPairNameAsMade(app, ownerID, side.Target, side.PairID, asked); err != nil {
				continue
			}
			verb = "will be"
		}
		note := fmt.Sprintf("%q relates both ways: its other side, on %s, %s %q", names[id], mcpDataSourceURL(app, side.Target), verb, name)
		switch {
		case side.Asked != "" && name != asked:
			note += fmt.Sprintf(", as %q is another property's name there: a name taken gets the first free number.", asked)
		case name != asked:
			note += fmt.Sprintf(": none was given, and this data source's name %q is another property's name there, so it took the first free number.", asked)
		case side.Asked == "":
			note += ", this data source's name, as none was given."
		default:
			note += "."
		}
		notes = append(notes, note)
	}
	return notes
}

// mcpByName lists the properties of a map by their names, then IDs.
func mcpByName[T any](names map[string]string, of map[string]T) []string {
	ids := make([]string, 0, len(of))
	for id := range of {
		if _, live := names[id]; live {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if names[ids[i]] != names[ids[j]] {
			return names[ids[i]] < names[ids[j]]
		}
		return ids[i] < ids[j]
	})
	return ids
}

// mcpAwaitPairsMade waits, a while at most, until the server has made the
// other sides of the relations named (database_relation_pair_runner.go).
func mcpAwaitPairsMade(ctx context.Context, app core.App, sides map[string]mcpTwoWaySide) {
	pairs := []string{}
	for _, side := range sides {
		pairs = append(pairs, side.PairID)
	}
	if len(pairs) == 0 {
		return
	}
	encoded, err := json.Marshal(pairs)
	if err != nil {
		return
	}
	for until := time.Now().Add(mcpPairNameWait); time.Now().Before(until) && ctx.Err() == nil; time.Sleep(20 * time.Millisecond) {
		pending := 0
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_database_relation_pair_work}}
			WHERE [[status]] = 'pending' AND [[kind]] = {:kind}
				AND json_extract([[payload_json]], '$.pairPropertyId') IN (SELECT [[value]] FROM json_each({:pairs}))
		`).Bind(dbx.Params{"kind": databaseRelationPairEnsure, "pairs": string(encoded)}).Row(&pending); err != nil || pending == 0 {
			return
		}
	}
}

// updateMCPDataSourceSayingNames is notion-update-data-source's answer with
// the names it didn't show (mcpPropertyNameNotes).
func updateMCPDataSourceSayingNames(
	ctx context.Context, request *mcp.CallToolRequest, app core.App, input notionMCPUpdateDataSourceInput,
) (*mcp.CallToolResult, notionMCPDatabaseAnswer, error) {
	dataSourceID, _ := mcpLocalDataSourceID(app, input.DataSourceID)
	_, beforeSides, _ := mcpSourceProperties(app, dataSourceID)
	// The change says the name each property it renamed was last asked for,
	// one it adds and renames too.
	renamed := map[string]string{}
	result, answer, err := updateMCPDataSource(context.WithValue(ctx, mcpAskedRenamesKey{}, renamed), request, app, input)
	if err != nil || result != nil {
		return result, answer, err
	}
	answer.Warnings = append(answer.Warnings, mcpPropertyNameNotes(ctx, app, dataSourceID, beforeSides, renamed)...)
	return nil, answer, nil
}

// mcpAskedRenamesKey carries the map a data source change writes, by
// property, the name each it renamed was last asked for.
type mcpAskedRenamesKey struct{}

// mcpAskedRenamesIn is that map in ctx, or nil.
func mcpAskedRenamesIn(ctx context.Context) map[string]string {
	renamed, _ := ctx.Value(mcpAskedRenamesKey{}).(map[string]string)
	return renamed
}

// mcpCreatedDatabaseAnswer is notion-create-database's answer with the names
// it didn't show (mcpPropertyNameNotes).
func mcpCreatedDatabaseAnswer(ctx context.Context, app core.App, created notionMCPCreateDatabaseOutput) (*mcp.CallToolResult, notionMCPDatabaseAnswer, error) {
	answer, err := mcpDatabaseAnswer(ctx, app, created.ID, mcpCreatedDatabaseLead)
	if err != nil {
		return nil, answer, err
	}
	answer.Warnings = append(answer.Warnings, mcpPropertyNameNotes(ctx, app, created.DataSourceID, nil, nil)...)
	return nil, answer, nil
}
