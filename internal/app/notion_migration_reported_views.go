package app

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A property a migration's plan sends to the report doesn't come: it goes to
// Deleted properties only so the report says where its values are. So the
// views the run made stop naming it, as they would never have named it had
// the import not brought it — their conditions and sorts on it, its column
// in their lists, widths and wraps, what they group or date by — while every
// other condition stays (the rest of an "and" or "or" group, which stands
// without it). Each view it left is reported against that view
// (view_property_unresolved), so the person reads which views lost what.

// clearNotionMigrationReportedProperty takes a reported property out of the
// views of its database.
func clearNotionMigrationReportedProperty(app core.App, job storedJob, payload notionImportJobPayload, step notionMigrationStep) error {
	dataSourceID, err := notionImportMappedID(app, job.ID, step.Database.SourceID, "data_source")
	if err != nil || dataSourceID == "" {
		return err
	}
	propertyID, err := notionImportMappedID(app, job.ID, notionPropertySourceID(step.Database.SourceID, step.SourceID), "property")
	if err != nil || propertyID == "" {
		return err
	}
	// One Leafen kept as a property (another needs it) stays in the views.
	var deleted int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_property_trash}} WHERE [[property_id]]={:id}`).
		Bind(dbx.Params{"id": propertyID}).Row(&deleted); err != nil || deleted == 0 {
		return err
	}
	views := []struct {
		ID     string `db:"id"`
		PageID string `db:"page_id"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]],[[page_id]] FROM {{leafen_database_views}}
		WHERE [[data_source_id]]={:dataSourceId} ORDER BY [[page_id]],[[id]]`).
		Bind(dbx.Params{"dataSourceId": dataSourceID}).All(&views); err != nil {
		return err
	}
	operations, changed := map[string][]graphOperation{}, []string{}
	left := []notionMCPDataSourceProperty(nil)
	for _, stored := range views {
		view, found, err := loadDatabaseViewRecord(app, stored.PageID, stored.ID)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if left == nil {
			if left, err = databaseSourceLiveProperties(app, dataSourceID, []string{propertyID}, nil); err != nil {
				return err
			}
		}
		config, cleared := notionMigrationViewWithoutProperty(view, propertyID, left)
		if !cleared {
			continue
		}
		encoded, err := json.Marshal(config)
		if err != nil {
			return err
		}
		operations[stored.PageID] = append(operations[stored.PageID], graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: stored.ID, DataSourceID: view.DataSourceID, ContainerID: view.ContainerID, Type: view.Type,
			Name: view.Name, Position: view.Position, Config: encoded,
		}})
		if notionMigrationViewUsesProperty(view.Config, propertyID) {
			changed = append(changed, stored.ID)
		}
	}
	batches := map[string]*structuralMutationBatch{}
	for _, pageID := range slices.Sorted(maps.Keys(operations)) {
		batch, err := loadStructuralMutationBatch(app, pageID, batches)
		if err != nil {
			return err
		}
		if err := applyNotionGraphOperationsWith(app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner,
			operations[pageID], notionGraphApplyReferences{}); err != nil {
			return err
		}
	}
	if err := saveStructuralMutationBatches(app, job.Owner, batches); err != nil {
		return err
	}
	for _, viewID := range changed {
		sources := []string{}
		if err := app.DB().NewQuery(`SELECT [[source_id]] FROM {{leafen_notion_import_mappings}}
			WHERE [[job_id]]={:jobId} AND [[kind]]='view' AND [[native_id]]={:viewId}`).
			Bind(dbx.Params{"jobId": job.ID, "viewId": viewID}).Column(&sources); err != nil {
			return err
		}
		for _, source := range sources {
			if err := recordNotionImportViewIssue(app, job.ID, source, notionImportIssueViewPropertyUnresolved); err != nil {
				return err
			}
		}
	}
	return nil
}

// notionMigrationViewUsesProperty says whether a view did anything with a
// property — a condition, a sort, a grouping or dating by it, or its column
// shown — which its leaving changes; one it only listed hidden, as Notion's
// views list every property, changes nothing the person sees.
func notionMigrationViewUsesProperty(config map[string]any, propertyID string) bool {
	for key, value := range config {
		switch key {
		case "propertyOrder", "hiddenProperties", "propertyWidths", "wrappedColumns":
			continue
		}
		if _, changed := notionMigrationWithoutProperty(map[string]any{key: value}, propertyID); changed {
			return true
		}
		if named, _ := value.(string); strings.TrimSpace(named) == propertyID {
			return true
		}
	}
	order, _ := config["propertyOrder"].([]any)
	hidden, _ := config["hiddenProperties"].([]any)
	return slices.Contains(order, any(propertyID)) && !slices.Contains(hidden, any(propertyID))
}

// notionMigrationViewWithoutProperty is a view's settings once a property is
// gone from them, and whether any named it. What a view groups or dates by
// falls back as when the property is deleted for good
// (databaseViewConfigWithoutProperties).
func notionMigrationViewWithoutProperty(
	view databaseViewAuthority, propertyID string, left []notionMCPDataSourceProperty,
) (map[string]any, bool) {
	config := databaseViewConfigWithoutProperties(view, []string{propertyID}, left)
	before, _ := json.Marshal(view.Config)
	after, _ := json.Marshal(config)
	cleared := string(before) != string(after)
	for key, value := range view.Config {
		if named, _ := value.(string); strings.TrimSpace(named) == propertyID {
			cleared = true
			if _, kept := config[key]; kept {
				delete(config, key)
			}
		}
	}
	for key, value := range config {
		next, changed := notionMigrationWithoutProperty(value, propertyID)
		if !changed {
			continue
		}
		cleared = true
		if next == nil {
			delete(config, key)
		} else {
			config[key] = next
		}
	}
	return config, cleared
}

// notionMigrationWithoutProperty takes a property out of one setting: a
// list's entry naming it (an id, or an object with its propertyId), a map's
// key, a condition group left empty. Nil when nothing is left of it.
func notionMigrationWithoutProperty(value any, propertyID string) (any, bool) {
	switch typed := value.(type) {
	case []any:
		kept, changed := make([]any, 0, len(typed)), false
		for _, item := range typed {
			if named, _ := item.(string); named == propertyID {
				changed = true
				continue
			}
			if object, ok := item.(map[string]any); ok {
				if named, _ := object["propertyId"].(string); named == propertyID {
					changed = true
					continue
				}
				next, inner := notionMigrationWithoutProperty(object, propertyID)
				if inner {
					changed = true
					if next == nil {
						continue
					}
					item = next
				}
			}
			kept = append(kept, item)
		}
		return kept, changed
	case map[string]any:
		next, changed := maps.Clone(typed), false
		if _, named := next[propertyID]; named {
			delete(next, propertyID)
			changed = true
		}
		if named, _ := next["propertyId"].(string); named == propertyID {
			return nil, true
		}
		for key, inner := range next {
			if _, nested := inner.([]any); !nested {
				if _, object := inner.(map[string]any); !object {
					continue
				}
			}
			cleared, innerChanged := notionMigrationWithoutProperty(inner, propertyID)
			if innerChanged {
				changed = true
				if cleared == nil {
					delete(next, key)
				} else {
					next[key] = cleared
				}
			}
		}
		// A condition group with nothing left goes.
		if changed && notionMigrationEmptyFilterGroup(next) {
			return nil, true
		}
		return next, changed
	}
	return value, false
}

func notionMigrationEmptyFilterGroup(group map[string]any) bool {
	if _, conjunction := group["conjunction"]; !conjunction {
		return false
	}
	filters, _ := group["filters"].([]any)
	groups, _ := group["groups"].([]any)
	return len(filters) == 0 && len(groups) == 0
}
