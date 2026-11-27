package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A two-way relation is a pair of relation properties, each naming the
// other's data source in relationDatabaseId and the other property in
// relatedPropertyId. A relation to its own database turned two-way makes a
// second property there, the two keeping each other up to date. Without it, a
// relation made on the screen (databaseRelationSymmetricKey) pairs with
// itself, as Notion's does (seen 2026-11-12), and the change itself relates
// the other row back (database_relation_pair_mirror.go); one made through
// Notion's API, or before that mark, relates one way, as Notion's API one
// does (seen 2026-10-13 and 2026-11-12). The side a person changes is
// authoritative for that change: turning the pair on (or naming the other
// side through reciprocalName) ensures the other property, turning it off or
// pointing the relation elsewhere deletes it, and adding or removing a
// related row adds or removes this row on that row's side.

// databaseRelationSymmetricKey marks a relation to its own database, with no
// second property, that relates both rows: relating A to B relates B to A at
// the end of its pages, and unrelating it unrelates it back, as Notion's made
// on its screen do (seen 2026-11-12: one made from "+", one made a relation by
// "유형 변경", one whose related property was turned off). Notion's API makes
// one without it (single_property): that one relates one way.
const databaseRelationSymmetricKey = "relationSymmetric"

const (
	databaseRelationPairEnsure   = "ensure"
	databaseRelationPairBackfill = "backfill"
	databaseRelationPairLinks    = "links"
	databaseRelationPairDelete   = "delete"
)

type databaseRelationPairLink struct {
	RowID  string   `json:"rowId"`
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

// databaseRelationPairPayload is the property P on source S whose pair Q
// lives on target T; the work changes T's side.
type databaseRelationPairPayload struct {
	SourceDataSourceID string `json:"sourceDataSourceId"`
	PropertyID         string `json:"propertyId"`
	TargetDataSourceID string `json:"targetDataSourceId"`
	PairPropertyID     string `json:"pairPropertyId"`
	// Unpair keeps the other property as a one-way relation with its values.
	Unpair bool                       `json:"unpair,omitzero"`
	Name   string                     `json:"name,omitempty"`
	Links  []databaseRelationPairLink `json:"links,omitempty"`
	Cursor string                     `json:"cursor,omitempty"`
	// PageID and PropertyIDs are the page whose tables of the target hide
	// the properties made in it (database_new_property_hiding.go).
	PageID      string   `json:"pageId,omitempty"`
	PropertyIDs []string `json:"propertyIds,omitempty"`
}

type databaseRelationPairWork struct {
	Kind    string
	Payload databaseRelationPairPayload
}

type databaseRelationPairProperty struct {
	ID           string         `db:"id"`
	DataSourceID string         `db:"data_source_id"`
	Type         string         `db:"type"`
	ConfigJSON   string         `db:"config"`
	Config       map[string]any `db:"-"`
}

type databaseRelationPair struct {
	TargetDataSourceID string
	PairPropertyID     string
}

func databaseRelationPairOf(propertyID, dataSourceID, valueType string, config map[string]any) *databaseRelationPair {
	if valueType != graphValueTypeRelation {
		return nil
	}
	target, _ := config["relationDatabaseId"].(string)
	pair, _ := config["relatedPropertyId"].(string)
	if target == "" {
		return nil
	}
	if pair == "" || pair == propertyID {
		// Only a symmetric relation to its own database pairs with itself.
		if symmetric, _ := config[databaseRelationSymmetricKey].(bool); symmetric && target == dataSourceID {
			return &databaseRelationPair{TargetDataSourceID: target, PairPropertyID: propertyID}
		}
		return nil
	}
	return &databaseRelationPair{TargetDataSourceID: target, PairPropertyID: pair}
}

// selfPaired reports a symmetric relation to its own database, which pairs
// with itself and has no other side to make or delete.
func (pair databaseRelationPair) selfPaired(propertyID string) bool {
	return pair.PairPropertyID == propertyID
}

// planDatabaseRelationPairWork reads the state a mutation is about to
// change and returns the work its two-way relations need on the other side.
func planDatabaseRelationPairWork(
	app core.App, ownerID string, operations []graphOperation,
) ([]databaseRelationPairWork, error) {
	propertyIDs := map[string]struct{}{}
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertProperty:
			propertyIDs[operation.Property.ID] = struct{}{}
		case graphOperationKindUpsertRow:
			for propertyID := range operation.Row.Values {
				propertyIDs[propertyID] = struct{}{}
			}
		}
	}
	if len(propertyIDs) == 0 {
		return nil, nil
	}
	stored, err := loadDatabaseRelationPairProperties(app, ownerID, workspaceExportSortedKeys(propertyIDs))
	if err != nil {
		return nil, err
	}
	effective := map[string]databaseRelationPairProperty{}
	for id, property := range stored {
		effective[id] = property
	}
	work := []databaseRelationPairWork{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertProperty {
			continue
		}
		property := operation.Property
		config := map[string]any{}
		if len(property.Config) > 0 && json.Unmarshal(property.Config, &config) != nil {
			config = map[string]any{}
		}
		next := databaseRelationPairProperty{ID: property.ID, DataSourceID: property.DataSourceID, Type: property.Type, Config: config}
		effective[property.ID] = next
		newPair := databaseRelationPairOf(property.ID, next.DataSourceID, next.Type, next.Config)
		oldName := ""
		samePair := false
		if old, exists := stored[property.ID]; exists {
			oldName, _ = old.Config["reciprocalName"].(string)
			oldPair := databaseRelationPairOf(property.ID, old.DataSourceID, old.Type, old.Config)
			samePair = oldPair != nil && newPair != nil && *oldPair == *newPair
			if oldPair != nil && !samePair && !oldPair.selfPaired(property.ID) {
				// Made another type it only ends the sync, as Notion's (seen 2026-11-04).
				work = append(work, databaseRelationPairWork{Kind: databaseRelationPairDelete, Payload: databaseRelationPairPayload{
					SourceDataSourceID: old.DataSourceID, PropertyID: property.ID,
					TargetDataSourceID: oldPair.TargetDataSourceID, PairPropertyID: oldPair.PairPropertyID,
					Unpair: next.Type != graphValueTypeRelation,
				}})
				oldName = ""
			}
		}
		if newPair != nil && !newPair.selfPaired(property.ID) {
			// The other side keeps a name given there unless this side
			// names it anew.
			name, _ := config["reciprocalName"].(string)
			if name == oldName {
				name = ""
			}
			work = append(work, databaseRelationPairWork{Kind: databaseRelationPairEnsure, Payload: databaseRelationPairPayload{
				SourceDataSourceID: next.DataSourceID, PropertyID: property.ID,
				TargetDataSourceID: newPair.TargetDataSourceID, PairPropertyID: newPair.PairPropertyID, Name: name,
			}})
		}
	}
	links := map[string]*databaseRelationPairWork{}
	linkOrder := []string{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow {
			continue
		}
		row := operation.Row
		for _, propertyID := range workspaceExportSortedKeys(row.Values) {
			property, exists := effective[propertyID]
			if !exists || property.DataSourceID != row.DataSourceID {
				continue
			}
			pair := databaseRelationPairOf(propertyID, property.DataSourceID, property.Type, property.Config)
			// The change itself relates rows back within one data source.
			if pair == nil || pair.TargetDataSourceID == property.DataSourceID {
				continue
			}
			next := []string{}
			if value := row.Values[propertyID].Value; !graphRowValueIsCleared(value) && json.Unmarshal(value, &next) != nil {
				continue
			}
			previous, err := loadDatabaseRelationPairTargets(app, ownerID, row.ID, propertyID)
			if err != nil {
				return nil, err
			}
			add, remove := databaseRelationPairDiff(previous, next)
			if len(add) == 0 && len(remove) == 0 {
				continue
			}
			entry, exists := links[propertyID]
			if !exists {
				entry = &databaseRelationPairWork{Kind: databaseRelationPairLinks, Payload: databaseRelationPairPayload{
					SourceDataSourceID: property.DataSourceID, PropertyID: propertyID,
					TargetDataSourceID: pair.TargetDataSourceID, PairPropertyID: pair.PairPropertyID,
				}}
				links[propertyID] = entry
				linkOrder = append(linkOrder, propertyID)
			}
			entry.Payload.Links = append(entry.Payload.Links, databaseRelationPairLink{RowID: row.ID, Add: add, Remove: remove})
		}
	}
	for _, propertyID := range linkOrder {
		work = append(work, *links[propertyID])
	}
	return work, nil
}

func loadDatabaseRelationPairProperties(app core.App, ownerID string, ids []string) (map[string]databaseRelationPairProperty, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows := []databaseRelationPairProperty{}
	// The properties are found by id; the owner is read after (+), or
	// SQLite, which keeps no statistics, reads the owner's every property.
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]], [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE +[[owner]] = {:owner} AND [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"owner": ownerID, "ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	result := make(map[string]databaseRelationPairProperty, len(rows))
	for _, row := range rows {
		row.Config = map[string]any{}
		if row.ConfigJSON != "" {
			_ = json.Unmarshal([]byte(row.ConfigJSON), &row.Config)
		}
		result[row.ID] = row
	}
	return result, nil
}

func loadDatabaseRelationPairTargets(app core.App, ownerID, rowID, propertyID string) ([]string, error) {
	targets := []string{}
	err := app.DB().NewQuery(`
		SELECT [[target_row_id]] FROM {{leafen_database_relation_items}}
		WHERE [[owner]] = {:owner} AND [[row_id]] = {:rowId} AND [[property_id]] = {:propertyId}
		ORDER BY [[ordinal]]
	`).Bind(dbx.Params{"owner": ownerID, "rowId": rowID, "propertyId": propertyID}).Column(&targets)
	return targets, err
}

func databaseRelationPairDiff(previous, next []string) ([]string, []string) {
	before, after := map[string]struct{}{}, map[string]struct{}{}
	for _, id := range previous {
		before[id] = struct{}{}
	}
	add := []string{}
	for _, id := range next {
		if _, seen := after[id]; seen || id == "" {
			continue
		}
		after[id] = struct{}{}
		if _, existed := before[id]; !existed {
			add = append(add, id)
		}
	}
	remove := []string{}
	for _, id := range previous {
		if _, kept := after[id]; !kept {
			remove = append(remove, id)
			after[id] = struct{}{}
		}
	}
	return add, remove
}

// admitDatabaseRelationPairWork records work in the transaction that caused
// it and wakes the runner once that transaction commits.
func admitDatabaseRelationPairWork(
	app core.App, ownerID, workspaceID, actorID, origin string, work []databaseRelationPairWork,
) error {
	if len(work) == 0 {
		return nil
	}
	now := time.Now().UTC().UnixMilli()
	for index, item := range work {
		payload, err := json.Marshal(item.Payload)
		if err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(origin + "\x00" + strconv.Itoa(index) + "\x00" + item.Kind + "\x00" + string(payload)))
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_relation_pair_work}}
				([[id]], [[owner]], [[workspace_id]], [[actor_id]], [[kind]], [[payload_json]], [[status]],
				 [[attempts]], [[not_before]], [[created_at]], [[updated_at]], [[error_code]])
			VALUES ({:id}, {:owner}, {:workspaceId}, {:actorId}, {:kind}, {:payload}, 'pending', 0, {:now}, {:now}, {:now}, '')
			ON CONFLICT ([[id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": hex.EncodeToString(digest[:16]), "owner": ownerID, "workspaceId": workspaceID, "actorId": actorID,
			"kind": item.Kind, "payload": string(payload), "now": now + int64(index),
		}).Execute(); err != nil {
			return err
		}
	}
	notifyDatabaseRelationPairAfterCommit(app)
	return nil
}

// planDatabaseRelationPairDeletion returns the work a deletion needs on the
// other side: a deleted paired property deletes its pair, a deleted row
// leaves its related rows, and a deleted source deletes its pairs.
func planDatabaseRelationPairDeletion(app core.App, ownerID, targetKind, targetID string) ([]databaseRelationPairWork, error) {
	properties := []databaseRelationPairProperty{}
	query := `SELECT [[id]], [[data_source_id]], [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[owner]] = {:owner} AND [[type]] = 'relation' AND `
	switch targetKind {
	case databaseMutationTargetProperty:
		query += `[[id]] = {:target}`
	case databaseMutationTargetSource:
		query += `[[data_source_id]] = {:target}`
	case databaseMutationTargetRow:
		query += `[[data_source_id]] = (SELECT [[data_source_id]] FROM {{leafen_database_rows}}
			WHERE [[owner]] = {:owner} AND [[id]] = {:target})`
	default:
		return nil, nil
	}
	if err := app.DB().NewQuery(query).Bind(dbx.Params{"owner": ownerID, "target": targetID}).All(&properties); err != nil {
		return nil, err
	}
	work := []databaseRelationPairWork{}
	for _, property := range properties {
		config := map[string]any{}
		_ = json.Unmarshal([]byte(property.ConfigJSON), &config)
		pair := databaseRelationPairOf(property.ID, property.DataSourceID, property.Type, config)
		if pair == nil {
			continue
		}
		payload := databaseRelationPairPayload{
			SourceDataSourceID: property.DataSourceID, PropertyID: property.ID,
			TargetDataSourceID: pair.TargetDataSourceID, PairPropertyID: pair.PairPropertyID,
		}
		if targetKind != databaseMutationTargetRow {
			if pair.selfPaired(property.ID) {
				continue
			}
			work = append(work, databaseRelationPairWork{Kind: databaseRelationPairDelete, Payload: payload})
			continue
		}
		targets, err := loadDatabaseRelationPairTargets(app, ownerID, targetID, property.ID)
		if err != nil {
			return nil, err
		}
		if len(targets) > 0 {
			payload.Links = []databaseRelationPairLink{{RowID: targetID, Remove: targets}}
			work = append(work, databaseRelationPairWork{Kind: databaseRelationPairLinks, Payload: payload})
		}
	}
	return work, nil
}
