package app

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionApplyRowIdentities struct {
	rowIDs       map[string]string
	referenceSet *graphReferenceCache
}

func loadNotionApplyRowIdentities(
	app core.App,
	sources []storedNotionSource,
	mappings *notionApplyMappings,
) (*notionApplyRowIdentities, error) {
	return loadNotionApplyRowIdentitiesWithGenerator(
		app, sources, mappings, newPageID,
	)
}

func loadNotionApplyRowIdentitiesWithGenerator(
	app core.App,
	sources []storedNotionSource,
	mappings *notionApplyMappings,
	nextID func() string,
) (*notionApplyRowIdentities, error) {
	registry := &notionApplyRowIdentities{
		rowIDs: map[string]string{},
		referenceSet: &graphReferenceCache{
			references:  map[string]graphEntityReference{},
			completeIDs: map[string]struct{}{},
		},
	}
	for _, source := range sources {
		if source.Kind != "row" {
			continue
		}
		if _, found := mappings.nativeID(source.SourceID, "row"); found {
			continue
		}
		rowID, err := normalizeGraphID(nextID())
		if err != nil {
			return nil, err
		}
		if _, duplicate := registry.referenceSet.completeIDs[rowID]; duplicate {
			return nil, errors.New(errorCodeNotionImportCreateFailed)
		}
		registry.rowIDs[source.SourceID] = rowID
		registry.referenceSet.completeIDs[rowID] = struct{}{}
	}
	if len(registry.rowIDs) == 0 {
		return registry, nil
	}
	rowIDs := make([]string, 0, len(registry.referenceSet.completeIDs))
	for rowID := range registry.referenceSet.completeIDs {
		rowIDs = append(rowIDs, rowID)
	}
	sort.Strings(rowIDs)
	encoded, err := json.Marshal(rowIDs)
	if err != nil {
		return nil, err
	}
	stored := []graphEntityReference{}
	err = app.DB().NewQuery(`
		SELECT [[rows]].[[id]], [[rows]].[[owner]], [[rows]].[[page_id]],
			'' AS [[parent_id]], [[rows]].[[data_source_id]], '' AS [[type]], '' AS [[config]]
		FROM json_each({:rowIds}) AS [[requested]]
		CROSS JOIN {{leafen_database_rows}} AS [[rows]]
			ON [[rows]].[[id]] = [[requested]].[[value]]
	`).Bind(dbx.Params{"rowIds": string(encoded)}).All(&stored)
	if err != nil {
		return nil, err
	}
	if len(stored) != 0 {
		return nil, errors.New(errorCodeNotionImportCreateFailed)
	}
	return registry, nil
}

func (identities *notionApplyRowIdentities) nativeID(sourceID string) (string, bool) {
	if identities == nil {
		return "", false
	}
	rowID, found := identities.rowIDs[sourceID]
	return rowID, found
}

func (identities *notionApplyRowIdentities) cache() *graphReferenceCache {
	if identities == nil {
		return nil
	}
	return identities.referenceSet
}
