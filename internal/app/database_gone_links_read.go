package app

import (
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// goneDatabaseLinksRead takes out of a window of a page's graph what names a
// database out of sight (database_gone_links_live.go): a database's members
// out of sight and their views, its chosen view then its first one left; an
// empty link keeps its views, waiting for a source.
type goneDatabaseLinksRead struct {
	*goneDatabaseLinks
	pageID     string
	emptyLinks map[string]bool
}

func withoutGoneDatabaseLinksRead(app core.App, pageID string, items []graphReadItem) ([]graphReadItem, error) {
	ids, viewIDs := []string{}, []string{}
	for _, item := range items {
		switch item.Kind {
		case graphReadKindBlock:
			if content, ok := graphReadDatabaseContent(item); ok {
				ids = append(ids, content.memberDataSourceIDs()...)
			}
		case graphReadKindView:
			id, _ := item.Value["dataSourceId"].(string)
			ids = append(ids, id)
		case graphReadKindDatabaseContainer:
			ids = append(ids, graphReadStrings(item.Value["dataSourceIds"])...)
			viewIDs = append(viewIDs, graphReadStrings(item.Value["viewIds"])...)
		}
	}
	if len(ids) == 0 {
		return items, nil
	}
	workspaceID, err := pageWorkspace(app, pageID)
	if err != nil {
		return nil, err
	}
	read := &goneDatabaseLinksRead{goneDatabaseLinks: newGoneDatabaseLinks(app, workspaceID), pageID: pageID, emptyLinks: map[string]bool{}}
	if err := read.load(ids); err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(ids, read.gone) {
		return items, nil
	}
	viewSources, err := read.viewSources(viewIDs)
	if err != nil {
		return nil, err
	}
	kept := items[:0]
	for _, item := range items {
		keep, err := read.keep(item, viewSources)
		if err != nil {
			return nil, err
		}
		if keep {
			kept = append(kept, item)
		}
	}
	return kept, nil
}

func (read *goneDatabaseLinksRead) keep(item graphReadItem, viewSources map[string]string) (bool, error) {
	switch item.Kind {
	case graphReadKindBlock:
		content, ok := graphReadDatabaseContent(item)
		if !ok {
			return true, nil
		}
		id, _ := item.Value["id"].(string)
		next, err := read.database(id, content, item.Value["content"])
		if next != nil {
			item.Value["content"] = next
		}
		return true, err
	case graphReadKindView:
		dataSourceID, _ := item.Value["dataSourceId"].(string)
		if !read.gone(dataSourceID) {
			return true, nil
		}
		containerID, _ := item.Value["containerId"].(string)
		empty, err := read.emptyLink(containerID)
		if err != nil || !empty {
			return false, err
		}
		config, _ := item.Value["config"].(map[string]any)
		if config == nil {
			config = map[string]any{}
		}
		config["linkPending"] = true
		item.Value["config"] = config
		return true, nil
	case graphReadKindDatabaseContainer:
		id, _ := item.Value["id"].(string)
		empty, err := read.emptyLink(id)
		if err != nil || empty {
			return true, err
		}
		item.Value["dataSourceIds"] = slices.DeleteFunc(graphReadStrings(item.Value["dataSourceIds"]), read.gone)
		item.Value["viewIds"] = slices.DeleteFunc(graphReadStrings(item.Value["viewIds"]), func(viewID string) bool {
			return read.gone(viewSources[viewID])
		})
	}
	return true, nil
}

// database is a database block's content without its members out of sight
// (nil when it keeps them all, or is an empty link).
func (read *goneDatabaseLinksRead) database(blockID string, content databaseContainerBlockContent, raw any) (map[string]any, error) {
	own, linked, changed, err := read.remaining(content)
	if err != nil || !changed {
		return nil, err
	}
	if len(own) == 0 && len(linked) == 0 {
		read.emptyLinks[blockID] = true
		return nil, nil
	}
	read.emptyLinks[blockID] = false
	stored, _ := raw.(map[string]any)
	next := withDatabaseContainerMembers(stored, own, linked)
	viewID, err := read.shownView(blockID, content.ViewID, own)
	if err != nil {
		return nil, err
	}
	next["viewId"] = viewID
	return next, nil
}

// shownView is the view a database shows: the one it chose while its source
// is in sight, else its first one left, as its tabs order them.
func (read *goneDatabaseLinksRead) shownView(blockID, chosen string, own []string) (string, error) {
	encoded, err := json.Marshal(own)
	if err != nil {
		return "", err
	}
	views := []struct {
		ID           string `db:"id"`
		DataSourceID string `db:"data_source_id"`
	}{}
	if err := read.app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]] FROM {{leafen_database_views}}
		WHERE [[page_id]] = {:pageId} AND ([[container_id]] = {:blockId}
			OR [[container_id]] = '' AND [[data_source_id]] IN (SELECT [[value]] FROM json_each({:own})))
		ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"pageId": read.pageID, "blockId": blockID, "own": string(encoded)}).All(&views); err != nil {
		return "", err
	}
	sources := make([]string, len(views))
	for index, view := range views {
		sources[index] = view.DataSourceID
	}
	if err := read.load(sources); err != nil {
		return "", err
	}
	first := ""
	for _, view := range views {
		if read.gone(view.DataSourceID) {
			continue
		}
		if view.ID == chosen {
			return chosen, nil
		}
		if first == "" {
			first = view.ID
		}
	}
	return first, nil
}

// emptyLink says whether the database a block holds is a linked one with
// nothing left in sight.
func (read *goneDatabaseLinksRead) emptyLink(blockID string) (bool, error) {
	if empty, known := read.emptyLinks[blockID]; known || blockID == "" {
		return empty, nil
	}
	read.emptyLinks[blockID] = false
	stored := []string{}
	if err := read.app.DB().NewQuery(`
		SELECT [[content]] FROM {{leafen_blocks}}
		WHERE [[id]] = {:id} AND [[page_id]] = {:pageId} AND [[type]] = {:type} LIMIT 1
	`).Bind(dbx.Params{"id": blockID, "pageId": read.pageID, "type": graphBlockTypeInlineDatabase}).Column(&stored); err != nil {
		return false, err
	}
	if len(stored) == 0 {
		return false, nil
	}
	content, err := parseDatabaseContainerBlockContent(json.RawMessage(stored[0]))
	if err != nil {
		return false, nil
	}
	own, linked, changed, err := read.remaining(content)
	empty := changed && len(own) == 0 && len(linked) == 0
	read.emptyLinks[blockID] = empty
	return empty, err
}

// viewSources is the data source of each view.
func (read *goneDatabaseLinksRead) viewSources(viewIDs []string) (map[string]string, error) {
	sources := map[string]string{}
	if len(viewIDs) == 0 {
		return sources, nil
	}
	encoded, err := json.Marshal(viewIDs)
	if err != nil {
		return nil, err
	}
	views := []struct {
		ID           string `db:"id"`
		DataSourceID string `db:"data_source_id"`
	}{}
	if err := read.app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]] FROM {{leafen_database_views}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&views); err != nil {
		return nil, err
	}
	ids := make([]string, len(views))
	for index, view := range views {
		sources[view.ID] = view.DataSourceID
		ids[index] = view.DataSourceID
	}
	return sources, read.load(ids)
}

func graphReadDatabaseContent(item graphReadItem) (databaseContainerBlockContent, bool) {
	if item.Value["type"] != graphBlockTypeInlineDatabase {
		return databaseContainerBlockContent{}, false
	}
	encoded, err := json.Marshal(item.Value["content"])
	if err != nil {
		return databaseContainerBlockContent{}, false
	}
	content, err := parseDatabaseContainerBlockContent(encoded)
	return content, err == nil
}

func graphReadStrings(value any) []string {
	list, _ := value.([]any)
	strings := make([]string, 0, len(list))
	for _, entry := range list {
		if text, ok := entry.(string); ok {
			strings = append(strings, text)
		}
	}
	return strings
}
