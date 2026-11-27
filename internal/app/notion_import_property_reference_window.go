package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionApplyPropertyReferences struct {
	propertyReferences map[string]graphEntityReference
}

func loadNotionApplyPropertyReferences(
	app core.App,
	sources []storedNotionSource,
	mappings *notionApplyMappings,
) (*notionApplyPropertyReferences, error) {
	requested := map[string]struct{}{}
	for _, source := range sources {
		if source.Kind != "row" {
			continue
		}
		if _, found := mappings.nativeID(source.SourceID, "row"); found {
			continue
		}
		dependencies, err := notionSourceDependencyMappingKeys(source)
		if err != nil {
			return nil, err
		}
		for _, dependency := range dependencies {
			if dependency.Kind != "property" {
				continue
			}
			if propertyID, found := mappings.nativeID(dependency.SourceID, "property"); found {
				requested[propertyID] = struct{}{}
			}
		}
	}
	registry := &notionApplyPropertyReferences{
		propertyReferences: map[string]graphEntityReference{},
	}
	if len(requested) == 0 {
		return registry, nil
	}
	propertyIDs := make([]string, 0, len(requested))
	for propertyID := range requested {
		propertyIDs = append(propertyIDs, propertyID)
	}
	sort.Strings(propertyIDs)
	encoded, err := json.Marshal(propertyIDs)
	if err != nil {
		return nil, err
	}
	stored := []graphEntityReference{}
	err = app.DB().NewQuery(`
		SELECT [[properties]].[[id]], [[properties]].[[owner]], [[properties]].[[page_id]],
			'' AS [[parent_id]], [[properties]].[[data_source_id]], [[properties]].[[type]], [[properties]].[[config]]
		FROM json_each({:propertyIds}) AS [[requested]]
		CROSS JOIN {{leafen_database_properties}} AS [[properties]]
			ON [[properties]].[[id]] = [[requested]].[[value]]
	`).Bind(dbx.Params{"propertyIds": string(encoded)}).All(&stored)
	if err != nil {
		return nil, err
	}
	for _, reference := range stored {
		registry.propertyReferences[reference.ID] = reference
	}
	for _, propertyID := range propertyIDs {
		if _, found := registry.propertyReferences[propertyID]; !found {
			return nil, sql.ErrNoRows
		}
	}
	return registry, nil
}

func (references *notionApplyPropertyReferences) insert(
	owner, pageID, dataSourceID, propertyID, propertyType, config string,
) error {
	if references == nil {
		return errors.New(errorCodeNotionImportCreateFailed)
	}
	next := graphEntityReference{
		ID: propertyID, Owner: owner, PageID: pageID, DataSourceID: dataSourceID,
		Type: propertyType, Config: config,
	}
	if current, found := references.propertyReferences[propertyID]; found {
		if current != next {
			return errors.New(errorCodeNotionImportCreateFailed)
		}
		return nil
	}
	references.propertyReferences[propertyID] = next
	return nil
}

// replace keeps a property's reference as a change of the turn left it.
func (references *notionApplyPropertyReferences) replace(reference graphEntityReference) {
	if references != nil {
		references.propertyReferences[reference.ID] = reference
	}
}

func (references *notionApplyPropertyReferences) references() map[string]graphEntityReference {
	if references == nil {
		return nil
	}
	return references.propertyReferences
}
