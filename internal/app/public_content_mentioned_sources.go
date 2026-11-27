package app

import "github.com/pocketbase/pocketbase/core"

// A published page's data source mention (data_source_mentions.go) reads as
// its database's mention does — the visitor reads it while the database's
// page opens to them, and it links that page — by the source's own name, as
// the app names it. Notion's published mention of a data source is not seen:
// publishing a page is outside what may be tried.

// loadPublicMentionedSources reads the data sources mentioned by their
// addresses among targets: each one's live database of the workspace
// (loadPublicMentionedDatabases) under the mention's address, without the
// database's icon, and the source's ID by the address, for its name
// (publicMentionedSourcesNamed).
func loadPublicMentionedSources(
	app core.App, authorization publicVisibilityAuthorization, targets []string,
) (map[string]storedPublicMentionedDatabase, map[string]string, error) {
	containers, sources, containerIDs := map[string]string{}, map[string]string{}, []string{}
	for _, target := range targets {
		dataSourceID, containerID, _, found, err := mentionedDataSource(app, target)
		if err != nil {
			return nil, nil, err
		}
		if found {
			containers[target], sources[target] = containerID, dataSourceID
			containerIDs = append(containerIDs, containerID)
		}
	}
	if len(containerIDs) == 0 {
		return nil, nil, nil
	}
	databases, err := loadPublicMentionedDatabases(app, authorization, containerIDs)
	if err != nil {
		return nil, nil, err
	}
	mentioned := map[string]storedPublicMentionedDatabase{}
	for address, containerID := range containers {
		if database, live := databases[containerID]; live {
			database.Icon, database.IconType = "", ""
			mentioned[address] = database
		}
	}
	return mentioned, sources, nil
}

// publicMentionedSourcesNamed names the open data source mentions by their
// sources' names as the visitor reads them, a mention in a name closed to
// them in marks, once the databases' names were read for them
// (publicMentionedDatabasesAsRead).
func publicMentionedSourcesNamed(
	app core.App, closer func() searchMentionCloser, open map[string]storedPublicMentionedDatabase, sources map[string]string,
) error {
	for address, dataSourceID := range sources {
		database, opened := open[address]
		if !opened {
			continue
		}
		name, err := mentionedDataSourceName(app, dataSourceID)
		if err != nil {
			return err
		}
		sourceNamesAsReadBy(app, closer, map[string]*string{dataSourceID: &name})
		database.Title = oneMarkPerMention(name)
		open[address] = database
	}
	return nil
}
