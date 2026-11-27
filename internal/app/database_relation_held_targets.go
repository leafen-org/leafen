package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A relation keeps what it already holds. Another page's row the writer
// can't read stays in a relation value when the same property already
// relates it from a row or template of this page: an edit keeps what its
// cell held, and a copy or a row made from a template what its source held.
// Notion's duplicate keeps a relation to a page the person duplicating can't
// open, shown to them as "사용 권한 없음" (seen 2026-11-08). A row deleted for
// good that the property held is left out of what is written instead of
// refusing it. A row the property doesn't hold yet is still refused.

// keepHeldRelationTargets admits into `rows` the unreadable rows that each
// relation value naming them already holds, and takes the rows gone for good
// that a value already holds out of it, saying how many values it changed.
func keepHeldRelationTargets(
	app core.App, owner, pageID string, operations []graphOperation,
	rows, unreadable map[string]graphEntityReference, gone map[string]struct{},
) (int, error) {
	if len(unreadable) == 0 && len(gone) == 0 {
		return 0, nil
	}
	candidates := make(map[string]struct{}, len(unreadable)+len(gone))
	for id := range unreadable {
		candidates[id] = struct{}{}
	}
	for id := range gone {
		candidates[id] = struct{}{}
	}
	held, err := heldRelationTargets(app, owner, pageID, candidates)
	if err != nil {
		return 0, err
	}
	// An unreadable row comes in only where every value naming it held it.
	admitted, refused, changed := map[string]struct{}{}, map[string]struct{}{}, 0
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		for propertyID, value := range operation.Row.Values {
			if value.Type != graphValueTypeRelation {
				continue
			}
			ids := graphRelationRowIDs(value.Value)
			kept := make([]string, 0, len(ids))
			for _, id := range ids {
				_, isHeld := held[[2]string{propertyID, id}]
				if _, isGone := gone[id]; isGone && isHeld {
					continue
				}
				if _, hidden := unreadable[id]; hidden && isHeld {
					admitted[id] = struct{}{}
				} else if hidden {
					refused[id] = struct{}{}
				}
				kept = append(kept, id)
			}
			if len(kept) == len(ids) {
				continue
			}
			encoded, err := json.Marshal(kept)
			if err != nil {
				return 0, err
			}
			value.Value = encoded
			operation.Row.Values[propertyID] = value
			changed++
		}
	}
	for id := range admitted {
		if _, no := refused[id]; !no {
			rows[id] = unreadable[id]
		}
	}
	return changed, nil
}

// heldRelationTargets are the (property, row) pairs among `ids` that a
// relation of this page's rows, or of its databases' templates, already holds.
func heldRelationTargets(app core.App, owner, pageID string, ids map[string]struct{}) (map[[2]string]struct{}, error) {
	encoded, err := json.Marshal(workspaceExportSortedKeys(ids))
	if err != nil {
		return nil, err
	}
	pairs := []struct {
		PropertyID string `db:"property_id"`
		TargetID   string `db:"target_row_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[property_id]], [[target_row_id]] FROM {{leafen_database_relation_items}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[target_row_id]] IN (SELECT [[value]] FROM json_each({:ids}))
		UNION
		SELECT [[property]].[[key]], [[target]].[[value]]
		FROM {{leafen_database_templates}} AS [[template]],
			json_each([[template]].[[properties_json]]) AS [[property]],
			json_each([[property]].[[value]]) AS [[target]]
		WHERE [[template]].[[owner]] = {:owner} AND [[template]].[[page_id]] = {:pageId}
			AND [[property]].[[type]] = 'array' AND [[target]].[[type]] = 'text'
			AND [[target]].[[value]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "ids": string(encoded)}).All(&pairs); err != nil {
		return nil, err
	}
	held := make(map[[2]string]struct{}, len(pairs))
	for _, pair := range pairs {
		held[[2]string{pair.PropertyID, pair.TargetID}] = struct{}{}
	}
	return held, nil
}
