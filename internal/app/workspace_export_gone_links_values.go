package app

import (
	"encoding/json"
	"errors"
	"slices"

	"github.com/pocketbase/dbx"
)

// The parts of an archive that name a database out of sight through a
// database that stays (workspace_export_gone_links.go): its database's
// entry, a trashed tree holding it, a view's settings and a row's values.

// container is what a database's entry keeps: none when its database leaves
// the archive, or its members and views in sight.
func (links *workspaceExportGoneLinks) container(value map[string]any) (bool, error) {
	blockID, _ := value["blockId"].(string)
	if dropped, err := links.databaseDropped(blockID); err != nil || dropped {
		return false, err
	}
	ids := workspaceExportStringList(value["dataSourceIds"])
	if err := links.load(ids); err != nil {
		return false, err
	}
	kept := slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return !links.inSight[id] })
	if len(kept) == 0 {
		return len(ids) == 0, nil
	}
	// A linked database links from the database its first member in sight
	// comes from, as its block does once those out of sight leave it
	// (withDatabaseContainerMembers), in the 30 days and after the purge took
	// their memberships.
	if value["kind"] == databaseContainerKindLinked {
		sources := []string{}
		if err := links.app.DB().NewQuery(`
			SELECT [[source_container_id]] FROM {{leafen_database_container_sources}}
			WHERE [[container_id]] = {:id} AND [[data_source_id]] = {:source}
		`).Bind(dbx.Params{"id": blockID, "source": kept[0]}).Column(&sources); err != nil {
			return false, err
		}
		if len(sources) == 1 && sources[0] != "" {
			value["sourceContainerId"] = sources[0]
		}
	}
	if len(kept) == len(ids) {
		return true, nil
	}
	views := []string{}
	for _, viewID := range workspaceExportStringList(value["viewIds"]) {
		shown, err := links.viewOfKeptMember(viewID)
		if err != nil {
			return false, err
		}
		if shown {
			views = append(views, viewID)
		}
	}
	var sources, shown any
	err := errors.Join(workspaceExportAsJSON(kept, &sources), workspaceExportAsJSON(views, &shown))
	value["dataSourceIds"], value["viewIds"] = sources, shown
	return true, err
}

// trashedBlocks is what a trashed tree keeps: its blocks but the databases
// that leave the archive, each next block taking the place of one gone, and
// the views of those; none when nothing is left.
func (links *workspaceExportGoneLinks) trashedBlocks(value map[string]any) (bool, error) {
	snapshot, _ := value["snapshot"].(map[string]any)
	blocks, _ := snapshot["blocks"].([]any)
	removed := map[string]string{}
	kept := make([]any, 0, len(blocks))
	for _, raw := range blocks {
		block, _ := raw.(map[string]any)
		if block == nil || block["type"] != graphBlockTypeInlineDatabase {
			kept = append(kept, raw)
			continue
		}
		id, _ := block["id"].(string)
		content, encoded := block["content"].(map[string]any)
		if text, isText := block["content"].(string); isText {
			if json.Unmarshal([]byte(text), &content) != nil {
				kept = append(kept, raw)
				continue
			}
		}
		next, keep, err := links.database(id, content)
		if err != nil {
			return false, err
		}
		if !keep {
			removed[id], _ = block["afterId"].(string)
			continue
		}
		if next != nil {
			if encoded {
				block["content"] = next
			} else {
				text, err := json.Marshal(next)
				if err != nil {
					return false, err
				}
				block["content"] = string(text)
			}
		}
		kept = append(kept, block)
	}
	if len(removed) == 0 {
		return true, nil
	}
	if len(kept) == 0 {
		return false, nil
	}
	for _, raw := range kept {
		if block, _ := raw.(map[string]any); block != nil {
			after, _ := block["afterId"].(string)
			for previous, gone := removed[after]; gone; previous, gone = removed[after] {
				after = previous
			}
			block["afterId"] = after
		}
	}
	if views, _ := snapshot["views"].([]any); views != nil {
		snapshot["views"] = slices.DeleteFunc(views, func(raw any) bool {
			view, _ := raw.(map[string]any)
			containerID, _ := view["containerId"].(string)
			_, gone := removed[containerID]
			return gone
		})
	}
	snapshot["blocks"], value["blockCount"] = kept, len(kept)
	return true, nil
}

// leftOutProperties are a data source's properties an archive leaves out:
// its relations to a source out of sight, and the rollups through them or of
// a property left out where they reach. Other exports keep them all.
func (links *workspaceExportGoneLinks) leftOutProperties(dataSourceID string) (map[string]bool, error) {
	if left, known := links.leftOut[dataSourceID]; known {
		return left, nil
	}
	left := map[string]bool{}
	links.leftOut[dataSourceID] = left
	if !links.archive {
		return left, nil
	}
	properties := []struct {
		ID     string `db:"id"`
		Type   string `db:"type"`
		Config string `db:"config"`
	}{}
	if err := links.app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:id} AND [[type]] IN ({:relation}, {:rollup})
		ORDER BY [[type]], [[id]]
	`).Bind(dbx.Params{
		"id": dataSourceID, "relation": graphValueTypeRelation, "rollup": graphValueTypeRollup,
	}).All(&properties); err != nil || len(properties) == 0 {
		return left, err
	}
	targets := map[string]string{}
	for _, property := range properties {
		if property.Type == graphValueTypeRelation {
			targets[property.ID] = graphRelationTarget(property.Type, json.RawMessage(property.Config))
		}
	}
	if err := links.load(workspaceExportSortedValues(targets)); err != nil {
		return nil, err
	}
	for id, target := range targets {
		if target != "" && !links.inSight[target] {
			left[id] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, property := range properties {
			if property.Type != graphValueTypeRollup || left[property.ID] {
				continue
			}
			config, _ := decodeDatabaseComputedConfig(property.Config)
			relationID, targetID, _ := config.rollupFields()
			through := left[relationID]
			if target := targets[relationID]; !through && target != "" && targetID != "" {
				reached, err := links.leftOutProperties(target)
				if err != nil {
					return nil, err
				}
				through = reached[targetID]
			}
			if through {
				left[property.ID], changed = true, true
			}
		}
	}
	return left, nil
}

// cells takes out of a row's or a template's values those of the properties
// left out, and out of its relations the rows the archive doesn't hold.
func (links *workspaceExportGoneLinks) cells(dataSourceID string, values map[string]any) error {
	if len(values) == 0 {
		return nil
	}
	left, err := links.leftOutProperties(dataSourceID)
	if err != nil {
		return err
	}
	targets := []string{}
	for propertyID, raw := range values {
		if left[propertyID] {
			delete(values, propertyID)
			continue
		}
		if cell, _ := raw.(map[string]any); cell != nil && cell["type"] == graphValueTypeRelation {
			targets = append(targets, workspaceExportStringList(cell["value"])...)
		}
	}
	if err := links.loadRows(targets); err != nil || len(targets) == 0 {
		return err
	}
	for _, raw := range values {
		if cell, _ := raw.(map[string]any); cell != nil && cell["type"] == graphValueTypeRelation {
			if rows, isList := cell["value"].([]any); isList {
				cell["value"] = slices.DeleteFunc(rows, func(row any) bool {
					id, _ := row.(string)
					return !links.rowCarried[id]
				})
			}
		}
	}
	return nil
}

// loadRows learns which rows the archive holds of those named: the ones in
// the workspace outside every deletion's fence, in the trash or not.
func (links *workspaceExportGoneLinks) loadRows(ids []string) error {
	unknown := []string{}
	for _, id := range ids {
		if _, known := links.rowCarried[id]; !known {
			unknown = append(unknown, id)
			links.rowCarried[id] = false
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	encoded, err := json.Marshal(unknown)
	if err != nil {
		return err
	}
	found := []string{}
	// The rows are found by id; the workspace is read after (+), or SQLite,
	// which keeps no statistics, reads the workspace's every row.
	if err := links.app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_rows}}
		WHERE +[[workspace_id]] = {:workspaceId} AND [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND ` + pageOutsideDeletionFenceSQL("[[id]]", "{:workspaceId}") + `
	`).Bind(dbx.Params{"workspaceId": links.workspaceID, "ids": string(encoded)}).Column(&found); err != nil {
		return err
	}
	for _, id := range found {
		links.rowCarried[id] = true
	}
	return nil
}

// viewConfig takes out of a view's settings the properties left out: a
// group or date it shows by, as their deletion for good does
// (withDatabaseViewsOffProperties), and its filters, sorts and lists.
func (links *workspaceExportGoneLinks) viewConfig(value map[string]any, dataSourceID string) error {
	left, err := links.leftOutProperties(dataSourceID)
	config, _ := value["config"].(map[string]any)
	if err != nil || len(left) == 0 || config == nil {
		return err
	}
	gone := workspaceExportSortedKeys(left)
	remaining, err := databaseSourceLiveProperties(links.app, dataSourceID, gone, nil)
	if err != nil {
		return err
	}
	viewType, _ := value["type"].(string)
	config = databaseViewConfigWithoutProperties(
		databaseViewAuthority{DataSourceID: dataSourceID, Type: viewType, Config: config}, gone, remaining,
	)
	value["config"] = workspaceExportWithoutProperties("", config, left)
	return nil
}

// workspaceExportWithoutProperties drops from settings every mention of the
// properties: a field naming one, an entry of a list, a key of a map keyed
// by them, and a filter, sort or column on one.
func workspaceExportWithoutProperties(field string, value any, left map[string]bool) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			named, isName := child.(string)
			if isName && left[named] && (pageDuplicationDatabaseReferenceKind(key) == "property" || databaseViewPropertyFields[key]) {
				delete(typed, key)
				continue
			}
			if databaseViewPropertyKeyedFields[key] {
				if keyed, _ := child.(map[string]any); keyed != nil {
					for property := range left {
						delete(keyed, property)
					}
				}
			}
			typed[key] = workspaceExportWithoutProperties(key, child, left)
		}
		return typed
	case []any:
		return slices.DeleteFunc(typed, func(child any) bool {
			if named, isName := child.(string); isName {
				return left[named] && databaseViewPropertyFields[field]
			}
			entry, _ := child.(map[string]any)
			property, _ := entry["propertyId"].(string)
			if column, _ := entry["id"].(string); field == databaseViewTimelineColumns && left[column] {
				return true
			}
			if left[property] {
				return true
			}
			workspaceExportWithoutProperties(field, child, left)
			return false
		})
	}
	return value
}

// workspaceExportAsJSON reads a value as an export's window holds it, in
// the shapes JSON decodes to.
func workspaceExportAsJSON(value, into any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, into)
}

func workspaceExportStringList(value any) []string {
	list, _ := value.([]any)
	result := make([]string, 0, len(list))
	for _, item := range list {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func workspaceExportSortedValues(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for _, key := range workspaceExportSortedKeys(values) {
		if values[key] != "" {
			result = append(result, values[key])
		}
	}
	return result
}
