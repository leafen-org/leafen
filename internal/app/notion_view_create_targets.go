package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// notionViewCreateTargets are the database and data source a new view
// names: no database for a widget, whose dashboard is found after.
type notionViewCreateTargets struct {
	database, source notionObjectIdentity
}

// findNotionViewCreateTargets finds what a new view names before anything
// its settings say, as Notion does (seen 2026-11-12: an unknown database
// before an unknown data source, both before a chart with no CHART
// directive): one the token can't find is answered as Notion's "Could not
// find database with ID" or "data_source", the ID as notionBodyIDShown
// names it. ok is false when the request was answered.
func findNotionViewCreateTargets(
	event *core.RequestEvent, workspaceID string, input notionViewCreateRequest,
) (notionViewCreateTargets, bool, error) {
	targets := notionViewCreateTargets{}
	missing := func(kind, id string) error {
		noteNotionAskedObject(event, kind, notionBodyIDShown(id))
		return notionCompatibilityError(event, http.StatusNotFound, notionErrorObjectNotFound, "There's no "+kind+" with the requested ID.")
	}
	if input.DatabaseID != nil {
		database, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityDatabase, *input.DatabaseID)
		if err != nil || database.Workspace != workspaceID {
			return targets, false, missing("database", *input.DatabaseID)
		}
		targets.database = database
	}
	source, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityDataSource, input.DataSourceID)
	if err != nil || source.Workspace != workspaceID {
		return targets, false, missing("data_source", input.DataSourceID)
	}
	if trashed, err := databaseSourceIsTrashed(event.App, source.InternalID); err != nil || trashed {
		if err != nil {
			return targets, false, notionViewReadError(event, err)
		}
		return targets, false, notionTrashedDataSourceNotFound(event, notionBodyIDShown(input.DataSourceID))
	}
	targets.source = source
	// A database the token may not read is one it can't find.
	if input.DatabaseID != nil {
		noteNotionAskedObject(event, "database", notionBodyIDShown(*input.DatabaseID))
	}
	return targets, true, nil
}
