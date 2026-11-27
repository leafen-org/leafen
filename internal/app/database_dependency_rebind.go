package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Dependencies' "고급 설정" chooses the relation naming a row's successors,
// as Notion's (seen 2026-10-11): any two-way relation of the data source to
// itself whose other side is another property, that side naming the
// predecessors. Chosen, the binding moves to that pair at once — the pair it
// leaves stays the relations it is, values and all — and the links the
// chosen relation already holds become dependencies: under the binding's new
// data key, the rows it relates are linked, a window of rows a change.

const (
	databaseDependencyRebindRows      = 200
	databaseDependencyRebindKeyPrefix = "dependencies-rebind-"
)

var errDatabaseDependencyRelationInvalid = errors.New("the chosen relation is not a two-way relation of the source to itself")

// rebindDatabaseDependencyPair moves a data source's dependency binding to
// the relation its settings name as successors, when that is another one,
// and links what the relation holds.
func (p *pocketBaseYgoPersistence) rebindDatabaseDependencyPair(ctx context.Context, actorID, pageID, sourceID string) error {
	state := struct {
		Requested   string `db:"requested"`
		Turn        int64  `db:"turn"`
		Owner       string `db:"owner"`
		Successor   string `db:"successor"`
		Predecessor string `db:"predecessor"`
		Revision    int    `db:"revision"`
		DataKey     string `db:"data_key"`
	}{}
	err := p.app.DB().NewQuery(`
		SELECT COALESCE(json_extract([[features]].[[dependencies_json]], '$.successorPropertyId'), '') AS [[requested]],
			[[features]].[[authority_revision]] AS [[turn]], [[binding]].[[owner]],
			[[binding]].[[successor_property_id]] AS [[successor]], [[binding]].[[predecessor_property_id]] AS [[predecessor]],
			[[binding]].[[revision]], [[binding]].[[data_key]]
		FROM {{leafen_database_task_features}} AS [[features]]
		JOIN {{leafen_database_dependency_bindings}} AS [[binding]]
			ON [[binding]].[[data_source_id]] = [[features]].[[data_source_id]] AND [[binding]].[[page_id]] = [[features]].[[page_id]]
		WHERE [[features]].[[data_source_id]] = {:source} AND [[features]].[[page_id]] = {:page}
			AND json_extract([[features]].[[dependencies_json]], '$.enabled') = 1
	`).Bind(dbx.Params{"source": sourceID, "page": pageID}).One(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// A binding a choice made finishes linking on any later change, should
	// its windows have stopped short.
	predecessor, binding := state.Predecessor, graphDependencyBinding{DataKey: state.DataKey}
	configure := []graphOperation(nil)
	if state.Requested != "" && state.Requested != state.Successor {
		predecessor, err = databaseDependencyRelationSide(p.app, sourceID, state.Requested)
		if err != nil {
			return err
		}
		binding = graphDependencyBinding{
			DataSourceID: sourceID, PredecessorPropertyID: predecessor, SuccessorPropertyID: state.Requested,
			DataKey: databaseDependencyRebindKeyPrefix + strconv.FormatInt(state.Turn, 10), ExpectedRevision: state.Revision,
		}
		configure = []graphOperation{{Kind: graphOperationKindConfigureDependency, DependencyBinding: &binding}}
	} else if !strings.HasPrefix(state.DataKey, databaseDependencyRebindKeyPrefix) {
		return nil
	}
	linked := map[string]bool{}
	for window := 0; ; window++ {
		rows, err := databaseDependencyUnlinkedRows(p.app, state.Owner, pageID, sourceID, predecessor, binding.DataKey)
		if err != nil {
			return err
		}
		if len(rows) == 0 && len(configure) == 0 {
			return nil
		}
		// A row linked once and listed again would be listed for ever.
		for _, rowID := range rows {
			if linked[rowID] {
				return errDatabaseDependencyRelationInvalid
			}
			linked[rowID] = true
		}
		writes := newDatabaseRowWrites(p.app, state.Owner, nil)
		for _, rowID := range rows {
			row, found, err := writes.row(sourceID, rowID)
			if err != nil {
				return err
			}
			if !found {
				continue
			}
			targets := []string{}
			if err := p.app.DB().NewQuery(`
				SELECT [[target_row_id]] FROM {{leafen_database_relation_items}}
				WHERE [[owner]] = {:owner} AND [[row_id]] = {:row} AND [[property_id]] = {:property}
				ORDER BY [[ordinal]]
			`).Bind(dbx.Params{"owner": state.Owner, "row": rowID, "property": predecessor}).Column(&targets); err != nil {
				return err
			}
			value, err := json.Marshal(targets)
			if err != nil {
				return err
			}
			row.Values[predecessor] = graphRowValue{Type: graphValueTypeRelation, Value: value}
		}
		operations := append(configure, writes.operations...)
		if len(operations) == 0 {
			return nil
		}
		mutationID := "dependency-rebind:" + sourceID + ":" + strconv.FormatInt(state.Turn, 10) + ":" + strconv.Itoa(window)
		digest := sha256.Sum256([]byte("leafen:dependency-rebind\x00" + pageID + "\x00" + mutationID + "\x00" + strings.Join(rows, "\x00")))
		if _, err := p.applyContentGraphMutationContext(ctx, actorID, pageID, graphMutationInput{MutationID: mutationID},
			contentGraphMutationHooks{
				SuppressAutomationEvents: true, SuppressNotifications: true,
				ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
					return hex.EncodeToString(digest[:]), nil
				},
				Prepare: func(core.App, pageAuthorization, *crdt.Doc) (contentGraphMutationPreparation, error) {
					return contentGraphMutationPreparation{Operations: operations}, nil
				},
			}); err != nil {
			return err
		}
		if len(configure) > 0 {
			binding.ExpectedRevision++
			configure = nil
		}
		if len(rows) < databaseDependencyRebindRows {
			return nil
		}
	}
}

// databaseDependencyRelationSide returns the other side of a two-way
// relation of a data source to itself, the side naming predecessors when
// the relation names successors.
func databaseDependencyRelationSide(app core.App, sourceID, propertyID string) (string, error) {
	properties := []struct {
		ID     string `db:"id"`
		Type   string `db:"type"`
		Config string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:source} AND [[type]] = {:relation} AND ` + databasePropertyNotTrashedSQL("[[id]]"),
	).Bind(dbx.Params{"source": sourceID, "relation": graphValueTypeRelation}).All(&properties); err != nil {
		return "", err
	}
	related := map[string]string{}
	for _, property := range properties {
		config := map[string]any{}
		if json.Unmarshal([]byte(property.Config), &config) != nil {
			continue
		}
		if target, _ := config["relationDatabaseId"].(string); target != sourceID {
			continue
		}
		other, _ := config["relatedPropertyId"].(string)
		related[property.ID] = other
	}
	other := related[propertyID]
	if other == "" || other == propertyID || related[other] != propertyID {
		return "", errDatabaseDependencyRelationInvalid
	}
	return other, nil
}

// databaseDependencyUnlinkedRows lists a window of the source's live rows
// whose predecessors the relation names without a link under the key.
func databaseDependencyUnlinkedRows(app core.App, owner, pageID, sourceID, predecessor, dataKey string) ([]string, error) {
	rows := []string{}
	err := app.DB().NewQuery(`
		SELECT DISTINCT [[item]].[[row_id]] FROM {{leafen_database_relation_items}} AS [[item]]
		WHERE [[item]].[[owner]] = {:owner} AND [[item]].[[data_source_id]] = {:source} AND [[item]].[[property_id]] = {:property}
			AND [[item]].[[target_row_id]] <> [[item]].[[row_id]]
			-- A row relating to itself can't wait on itself; its other links stay relations.
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_relation_items}} AS [[own]]
				WHERE [[own]].[[row_id]] = [[item]].[[row_id]] AND [[own]].[[property_id]] = {:property}
					AND [[own]].[[target_row_id]] = [[own]].[[row_id]])
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_row_trash}} WHERE [[row_id]] = [[item]].[[row_id]])
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_dependency_edges}} AS [[edge]]
				WHERE [[edge]].[[owner]] = {:owner} AND [[edge]].[[page_id]] = {:page} AND [[edge]].[[data_source_id]] = {:source}
					AND [[edge]].[[data_key]] = {:key} AND [[edge]].[[successor_row_id]] = [[item]].[[row_id]]
					AND [[edge]].[[predecessor_row_id]] = [[item]].[[target_row_id]])
		ORDER BY [[item]].[[row_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": owner, "page": pageID, "source": sourceID, "property": predecessor, "key": dataKey,
		"limit": databaseDependencyRebindRows,
	}).Column(&rows)
	return rows, err
}
