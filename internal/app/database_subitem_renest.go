package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// renestDatabaseSubitems puts each row of a data source under the row its
// Parent item names once the sub-items' pair is another, as Notion's rows
// follow the relation its "속성" names: a row nested by the pair before whose
// Parent item is empty goes to the top, so the job moving old sub-items
// finds no parent to write in as a value no one gave. Only the rows move:
// a Parent item naming the row itself or a row in the trash stays as it is,
// the row at the top. A row whose Parent item names more than one page
// stays where it is: a row has one parent, and its relation is not cut down
// to one.
func (p *pocketBaseYgoPersistence) renestDatabaseSubitems(ctx context.Context, actorID, pageID, sourceID string) error {
	pairs, err := loadDatabaseSubitemPairs(p.app, []string{sourceID})
	if err != nil {
		return err
	}
	pair, paired := pairs[sourceID]
	if !paired {
		return nil
	}
	features := struct {
		Owner string `db:"owner"`
		Turn  int64  `db:"authority_revision"`
	}{}
	if err := p.app.DB().NewQuery(`SELECT [[owner]], [[authority_revision]] FROM {{leafen_database_task_features}}
		WHERE [[data_source_id]] = {:source} AND [[page_id]] = {:page}`).
		Bind(dbx.Params{"source": sourceID, "page": pageID}).One(&features); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	moves, err := databaseSubitemRenests(p.app, sourceID, pair)
	if err != nil || len(moves) == 0 {
		return err
	}
	// The same moves at the same settings are one change; a row back from the
	// trash makes others.
	named := sha256.New()
	for _, rowID := range workspaceExportSortedKeys(moves) {
		named.Write([]byte(rowID + "\x00" + moves[rowID].parent + "\x00"))
	}
	mutationID := "subitem-renest:" + sourceID + ":" + strconv.FormatInt(features.Turn, 10) + ":" + hex.EncodeToString(named.Sum(nil)[:8])
	digest := sha256.Sum256([]byte("leafen:subitem-renest\x00" + pageID + "\x00" + mutationID))
	_, err = p.applyContentGraphMutationContext(ctx, actorID, pageID,
		graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			SuppressAutomationEvents: true, SuppressNotifications: true, PreserveRowEditStamps: true,
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				moves, err := databaseSubitemRenests(txApp, sourceID, pair)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				writes := newDatabaseRowWrites(txApp, features.Owner, nil)
				for rowID, move := range moves {
					row, found, err := writes.row(sourceID, rowID)
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					if !found {
						continue
					}
					row.SubitemParentID = nil
					if move.parent != "" {
						parent := move.parent
						row.SubitemParentID = &parent
					}
					// Its Parent item as it already is, and the sub-item parent with it: a
					// dependency pair's Blocking leaves the change as links before the
					// sub-items read it, and the parent then says the same.
					row.Values[pair.Parent] = graphRowValue{Type: graphValueTypeRelation, Value: move.value}
				}
				return contentGraphMutationPreparation{Operations: writes.operations}, nil
			},
		})
	return err
}

// databaseSubitemRenest is where a row goes ("" for the top), its Parent
// item carried as it is.
type databaseSubitemRenest struct {
	parent string
	value  json.RawMessage
}

// databaseSubitemRenests names the live rows whose sub-item parent is not
// the one live row their Parent item names, with where each goes.
func databaseSubitemRenests(app core.App, sourceID string, pair databaseSubitemPair) (map[string]databaseSubitemRenest, error) {
	rows := []struct {
		ID     string `db:"id"`
		Parent string `db:"parent"`
		Value  string `db:"value"`
	}{}
	err := app.DB().NewQuery(`SELECT [[row]].[[id]], COALESCE([[row]].[[subitem_parent_id]], '') AS [[parent]],
			COALESCE((SELECT [[value]] FROM {{leafen_database_row_values}}
				WHERE [[row_id]] = [[row]].[[id]] AND [[property_id]] = {:property}), '[]') AS [[value]]
		FROM {{leafen_database_rows}} AS [[row]]
		WHERE ` + databaseSourceRowsSQL("[[row]]", "{:source}") + `
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_row_trash}} WHERE [[row_id]] = [[row]].[[id]])`).
		Bind(dbx.Params{"source": sourceID, "property": pair.Parent}).All(&rows)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]bool, len(rows))
	for _, row := range rows {
		ids[row.ID] = true
	}
	moves := map[string]databaseSubitemRenest{}
	for _, row := range rows {
		targets := []string{}
		if json.Unmarshal([]byte(row.Value), &targets) != nil || len(targets) > 1 {
			continue
		}
		next := ""
		if len(targets) == 1 && ids[targets[0]] && targets[0] != row.ID {
			next = targets[0]
		}
		if next != row.Parent {
			value, _ := json.Marshal(targets)
			moves[row.ID] = databaseSubitemRenest{parent: next, value: value}
		}
	}
	return moves, nil
}
