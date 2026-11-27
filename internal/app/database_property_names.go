package app

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// As in Notion, the properties of a data source have different names: a
// property made, renamed or moved with a name another property of its source
// has takes the first free "<name> 1", "<name> 2", …, a renamed property's
// own current name counting as taken, and the properties it meets keep
// theirs. Deleted properties don't hold their names, and a property restored
// with a name taken meanwhile takes a number. Properties that shared a name
// before this rule are told apart the same way, the first in the source's
// order keeping the name, and the formulas that read them name them by their
// new names.

// distinctDatabasePropertyName is the first of name, "name 1", "name 2", …
// that isn't taken.
func distinctDatabasePropertyName(name string, taken map[string]bool) string {
	if !taken[name] {
		return name
	}
	for number := 1; ; number++ {
		suffix := " " + strconv.Itoa(number)
		base := []rune(name)
		if limit := maxGraphIdentifierLength - utf8.RuneCountInString(suffix); len(base) > limit {
			base = base[:limit]
		}
		if candidate := strings.TrimSpace(string(base)) + suffix; !taken[candidate] {
			return candidate
		}
	}
}

// distinguishDatabasePropertyNames gives each property the operations make,
// rename or move a name no other property of its source has. One the change
// sends to Deleted properties (withPropertiesTrashed) holds no name either.
func distinguishDatabasePropertyNames(
	ctx context.Context, app core.App, pageID string, operations []graphOperation,
) ([]graphOperation, error) {
	upserts := false
	for _, operation := range operations {
		upserts = upserts || operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil
	}
	if !upserts {
		return operations, nil
	}
	current, err := loadDatabasePropertyRecords(app, pageID)
	if err != nil {
		return nil, err
	}
	trashed, err := databasePropertyTrashedIDs(app, pageID)
	if err != nil {
		return nil, err
	}
	leaving := propertiesTrashedBy(ctx)
	type named struct{ source, name string }
	final := map[string]named{}
	for id, entity := range current {
		if !trashed[id] && !leaving[id] {
			final[id] = named{entity.DataSourceID, entity.Name}
		}
	}
	changed := map[string]bool{}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil:
			property := operation.Property
			final[property.ID] = named{property.DataSourceID, property.Name}
			entity, exists := current[property.ID]
			changed[property.ID] = changed[property.ID] || !exists || entity.Name != property.Name ||
				entity.DataSourceID != property.DataSourceID
		case operation.Kind == graphOperationKindDeleteProperty && operation.PropertyDeletion != nil:
			delete(final, operation.PropertyDeletion.ID)
		}
	}
	taken := map[string]map[string]bool{}
	for id, property := range final {
		if taken[property.source] == nil {
			taken[property.source] = map[string]bool{}
		}
		if !changed[id] {
			taken[property.source][property.name] = true
		}
	}
	distinct := append([]graphOperation(nil), operations...)
	for index, operation := range distinct {
		property := operation.Property
		if operation.Kind != graphOperationKindUpsertProperty || property == nil || !changed[property.ID] {
			continue
		}
		if _, present := final[property.ID]; !present {
			continue
		}
		names := taken[property.DataSourceID]
		candidates := names
		if entity, exists := current[property.ID]; exists && entity.Name != property.Name && !names[entity.Name] {
			candidates = map[string]bool{entity.Name: true}
			for name := range names {
				candidates[name] = true
			}
		}
		name := distinctDatabasePropertyName(property.Name, candidates)
		names[name] = true
		if name != property.Name {
			renamed := *property
			renamed.Name = name
			distinct[index].Property = &renamed
		}
	}
	return distinct, nil
}

func databasePropertyTrashedIDs(app core.App, pageID string) (map[string]bool, error) {
	ids := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[trash]].[[property_id]] FROM {{leafen_database_property_trash}} AS [[trash]]
		JOIN {{leafen_database_properties}} AS [[property]] ON [[property]].[[id]] = [[trash]].[[property_id]]
		WHERE [[property]].[[page_id]] = {:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).Column(&ids); err != nil {
		return nil, err
	}
	trashed := make(map[string]bool, len(ids))
	for _, id := range ids {
		trashed[id] = true
	}
	return trashed, nil
}

// distinguishDatabaseSourcePropertyNames gives the live properties of a
// source that share a name their own names: the restored property alone when
// one is named, else each but the first in the source's order.
func distinguishDatabaseSourcePropertyNames(app core.App, sourceID, restoredID string) error {
	rows := []struct {
		ID       string `db:"id"`
		Type     string `db:"type"`
		Name     string `db:"name"`
		Position string `db:"position"`
		Config   string `db:"config"`
		Trashed  bool   `db:"trashed"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[property]].[[id]], [[property]].[[type]], [[property]].[[name]], [[property]].[[position]],
			[[property]].[[config]], NOT ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + ` AS [[trashed]]
		FROM {{leafen_database_properties}} AS [[property]]
		WHERE [[property]].[[data_source_id]] = {:sourceId}
		ORDER BY [[property]].[[position]], [[property]].[[id]]
	`).Bind(dbx.Params{"sourceId": sourceID}).All(&rows); err != nil {
		return err
	}
	before := []databaseFormulaReferenceProperty{}
	configs := map[string]map[string]any{}
	taken := map[string]bool{}
	for _, row := range rows {
		config, err := databaseRecordConfig(row.Config)
		if err != nil {
			return err
		}
		configs[row.ID] = config
		if !row.Trashed {
			before = append(before, databaseFormulaReferenceProperty{
				ID: row.ID, DataSourceID: sourceID, Type: row.Type, Name: row.Name, Position: row.Position,
			})
			if row.ID != restoredID {
				taken[row.Name] = true
			}
		}
	}
	after := append([]databaseFormulaReferenceProperty(nil), before...)
	if restoredID != "" {
		// Until now no formula could read the restored property by name.
		live := []databaseFormulaReferenceProperty{}
		for _, property := range before {
			if property.ID != restoredID {
				live = append(live, property)
			}
		}
		before = live
	}
	kept := map[string]bool{}
	for index, property := range after {
		keeps := !kept[property.Name]
		if restoredID != "" {
			keeps = property.ID != restoredID || !taken[property.Name]
		}
		if keeps {
			kept[property.Name] = true
			continue
		}
		name := distinctDatabasePropertyName(property.Name, taken)
		taken[name] = true
		after[index].Name = name
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_database_properties}} SET [[name]] = {:name} WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": property.ID, "name": name}).Execute(); err != nil {
			return err
		}
	}
	beforeNames := newDatabaseFormulaReferenceNames(before, nil)
	afterNames := newDatabaseFormulaReferenceNames(after, nil)
	if err := rewriteDatabaseAutomationDefinitionReferences(app, sourceID, beforeNames, afterNames); err != nil {
		return err
	}
	for _, row := range rows {
		stored := configs[row.ID]
		copied := map[string]any{}
		for key, value := range stored {
			copied[key] = value
		}
		config, changed := rewriteDatabaseFormulaReferenceConfig(databaseFormulaReferenceProperty{
			ID: row.ID, DataSourceID: sourceID, Type: row.Type, Name: row.Name, Position: row.Position,
			Config: copied, Stored: true,
		}, stored, beforeNames, afterNames)
		if !changed {
			continue
		}
		encoded, err := json.Marshal(config)
		if err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_database_properties}} SET [[config]] = {:config} WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": row.ID, "config": string(encoded)}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
