package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// inlineDatabaseLocation is what a database page's path shows between the
// page holding its database and the page, as Notion's paths do ("Page /
// Database / Row"): the inline database's title. A full-page database is
// its own page, already in the path, so it shows none; neither does one on a
// page the reader may not read, or one in the trash. UntitledRow is what the
// database's rows without a title are called (databaseUntitledRowName).
type inlineDatabaseLocation struct {
	Inline      bool
	Title       string
	UntitledRow string
}

// inlineDatabaseLocations reads each container's location once for a
// reader's results; keys are "containerPageId\x00dataSourceId".
type inlineDatabaseLocations struct {
	// ctx is the reader's own request, whose language names untitled rows.
	ctx         context.Context
	app         core.App
	principalID string
	workspaceID string
	read        map[string]inlineDatabaseLocation
}

func newInlineDatabaseLocations(app core.App, principalID, workspaceID string) *inlineDatabaseLocations {
	return &inlineDatabaseLocations{app: app, principalID: principalID, workspaceID: workspaceID, read: map[string]inlineDatabaseLocation{}}
}

// reading has the locations name untitled rows in the language of the
// reader's own request ctx carries.
func (locations *inlineDatabaseLocations) reading(ctx context.Context) *inlineDatabaseLocations {
	locations.ctx = ctx
	return locations
}

func (locations *inlineDatabaseLocations) of(containerPageID, dataSourceID string) (inlineDatabaseLocation, error) {
	if containerPageID == "" || dataSourceID == "" {
		return inlineDatabaseLocation{}, nil
	}
	key := containerPageID + "\x00" + dataSourceID
	if location, ok := locations.read[key]; ok {
		return location, nil
	}
	location, err := readInlineDatabaseLocation(locations.app, locations.principalID, locations.workspaceID, containerPageID, dataSourceID)
	if err != nil {
		return inlineDatabaseLocation{}, err
	}
	if location.UntitledRow, err = databaseUntitledRowNameFor(locations.ctx, locations.app, locations.principalID, dataSourceID); err != nil {
		return inlineDatabaseLocation{}, err
	}
	locations.read[key] = location
	return location, nil
}

func readInlineDatabaseLocation(
	app core.App, principalID, workspaceID, containerPageID, dataSourceID string,
) (inlineDatabaseLocation, error) {
	page, err := app.FindRecordById(pagesCollection, containerPageID)
	if errors.Is(err, sql.ErrNoRows) {
		return inlineDatabaseLocation{}, nil
	}
	if err != nil {
		return inlineDatabaseLocation{}, err
	}
	if page.GetString("kind") == pageKindDatabase || page.GetBool("in_trash") {
		return inlineDatabaseLocation{}, nil
	}
	readable, err := authorizePage(app, principalID, containerPageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return inlineDatabaseLocation{}, nil
	}
	if err != nil {
		return inlineDatabaseLocation{}, err
	}
	if readable.Workspace.WorkspaceID != workspaceID {
		return inlineDatabaseLocation{}, nil
	}
	source := struct {
		Title string `db:"title"`
		Spans string `db:"title_spans"`
	}{}
	err = app.DB().NewQuery(`
		SELECT COALESCE([[title]], '') AS [[title]], [[title_spans]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id} LIMIT 1
	`).Bind(dbx.Params{"id": dataSourceID}).One(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return inlineDatabaseLocation{}, nil
	}
	if err != nil {
		return inlineDatabaseLocation{}, err
	}
	// Its name as its reader reads it (page_name_mentions.go).
	title := databaseNameAsRead(readerCloser(app, principalID), source.Title, decodeStoredTextSpans(source.Spans))
	return inlineDatabaseLocation{Inline: true, Title: title}, nil
}
