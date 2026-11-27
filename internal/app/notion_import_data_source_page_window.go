package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionApplyDataSourcePages struct {
	sourceReferences map[string]graphEntityReference
}

func loadNotionApplyDataSourcePages(
	app core.App,
	sources []storedNotionSource,
	mappings *notionApplyMappings,
) (*notionApplyDataSourcePages, error) {
	requested := map[string]struct{}{}
	for _, source := range sources {
		// A database's other data source joins the first on its page.
		joins := source.Kind == "database" && source.ParentKind == notionImportPropertyIssueParentKind
		if source.Kind != "property" && source.Kind != "row" && !joins {
			continue
		}
		if kind := notionSourceMappingKind(source.Kind); kind != "" {
			if _, found := mappings.nativeID(source.SourceID, kind); found {
				continue
			}
		}
		if dataSourceID, found := mappings.nativeID(source.ParentSourceID, "data_source"); found {
			requested[dataSourceID] = struct{}{}
		}
	}
	registry := &notionApplyDataSourcePages{
		sourceReferences: map[string]graphEntityReference{},
	}
	if len(requested) == 0 {
		return registry, nil
	}
	dataSourceIDs := make([]string, 0, len(requested))
	for dataSourceID := range requested {
		dataSourceIDs = append(dataSourceIDs, dataSourceID)
	}
	sort.Strings(dataSourceIDs)
	encoded, err := json.Marshal(dataSourceIDs)
	if err != nil {
		return nil, err
	}
	stored := []graphEntityReference{}
	err = app.DB().NewQuery(`
		SELECT [[sources]].[[id]], [[sources]].[[owner]], [[sources]].[[page_id]],
			'' AS [[parent_id]], '' AS [[data_source_id]], '' AS [[type]], '' AS [[config]]
		FROM json_each({:dataSourceIds}) AS [[requested]]
		CROSS JOIN {{leafen_data_sources}} AS [[sources]]
			ON [[sources]].[[id]] = [[requested]].[[value]]
	`).Bind(dbx.Params{"dataSourceIds": string(encoded)}).All(&stored)
	if err != nil {
		return nil, err
	}
	for _, reference := range stored {
		registry.sourceReferences[reference.ID] = reference
	}
	for _, dataSourceID := range dataSourceIDs {
		if _, found := registry.sourceReferences[dataSourceID]; !found {
			return nil, sql.ErrNoRows
		}
	}
	return registry, nil
}

func (pages *notionApplyDataSourcePages) pageID(dataSourceID string) (string, bool) {
	if pages == nil {
		return "", false
	}
	reference, found := pages.sourceReferences[dataSourceID]
	return reference.PageID, found
}

func (pages *notionApplyDataSourcePages) insert(owner, pageID, dataSourceID string) error {
	if pages == nil {
		return errors.New(errorCodeNotionImportCreateFailed)
	}
	if current, found := pages.sourceReferences[dataSourceID]; found {
		if current.Owner != owner || current.PageID != pageID {
			return errors.New(errorCodeNotionImportCreateFailed)
		}
		return nil
	}
	pages.sourceReferences[dataSourceID] = graphEntityReference{
		ID: dataSourceID, Owner: owner, PageID: pageID,
	}
	return nil
}

func (pages *notionApplyDataSourcePages) references() map[string]graphEntityReference {
	if pages == nil {
		return nil
	}
	return pages.sourceReferences
}
