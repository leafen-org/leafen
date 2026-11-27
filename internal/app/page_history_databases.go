package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// pageHistoryVersionDatabases lists the databases a version holds, inline
// and full-page, in the page's order and by their names now, as Notion's
// restore names the databases it keeps as they are (seen 2026-09-30); an
// inline one with its table at the version's time. One the page doesn't hold
// now reads as changed, as restoring the databases brings it back (seen
// 2026-11-25: an inline and a full-page database moved to the trash since
// counted among the databases to restore). A linked view is named Linked:
// Notion's restore neither counts nor names it among the databases, and
// leaves it as it is now without them (seen twice the same day).
func pageHistoryVersionDatabases(
	app core.App, pageID string, versionCursor int, at int64,
) ([]pageHistoryDatabaseWire, error) {
	// The structure and databases alone: the text isn't read.
	state, err := materializePageHistoryParts(app, pageID, versionCursor, pageHistoryReplayStructure)
	if err != nil {
		return nil, err
	}
	metadata, err := structuralMetadata(state.Structural)
	if err != nil {
		return nil, err
	}
	// The page's blocks now, which a restore keeping its databases keeps.
	current, found, err := loadYgoStructuralDoc(app, pageID)
	if err != nil {
		return nil, err
	}
	now := map[string]structuralBlockMetadata{}
	if found {
		if now, err = structuralMetadata(current); err != nil {
			return nil, err
		}
	}
	databases := []pageHistoryDatabaseWire{}
	seen := map[string]struct{}{}
	// The page's databases as they are now, read for the first inline one.
	var nowStates map[string]pageHistoryDatabaseState
	var walk func(parentID string) error
	walk = func(parentID string) error {
		order, err := structuralOrder(state.Structural, parentID)
		if err != nil {
			return err
		}
		for _, id := range order {
			if _, done := seen[id]; done {
				continue
			}
			seen[id] = struct{}{}
			entry, found := metadata[id]
			if !found {
				continue
			}
			database, ok, err := pageHistoryDatabaseOf(app, id, entry)
			if err != nil {
				return err
			}
			_, held := now[id]
			if ok && !database.FullPage {
				sourceID, _ := entry.Content["dataSourceId"].(string)
				if err := fillPageHistoryDatabaseTable(app, &database, sourceID, state.Databases[sourceID], at); err != nil {
					return err
				}
				if database.Views, err = pageHistoryDatabaseViews(app, id); err != nil {
					return err
				}
				if nowStates == nil {
					owner, err := pageHistoryDatabaseOwner(app, pageID)
					if err != nil {
						return err
					}
					if nowStates, err = loadPageHistoryDatabaseStates(app, owner, pageID); err != nil {
						return err
					}
				}
				// Away from the page now, restoring it brings it back: it changes.
				if held {
					if database.Unchanged, err = pageHistoryDatabaseUnchanged(
						app, database, sourceID, state.Databases[sourceID], nowStates[sourceID],
					); err != nil {
						return err
					}
				}
			}
			if ok {
				databases = append(databases, database)
			}
			if err := walk(id); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(""); err != nil {
		return nil, err
	}
	// The databases the page holds now that the version doesn't, which the
	// restore keeps or, with the databases restored too, removes.
	if !found {
		return databases, nil
	}
	later, err := pageHistoryLaterDatabases(app, current, now, metadata)
	if err != nil {
		return nil, err
	}
	return append(databases, later...), nil
}

// pageHistoryDatabaseUnchanged is whether an inline database reads now as
// a version left it: its name, properties and views as the page keeps them,
// and its rows with their values, so restoring it would change nothing.
// Notion's restore asks about a page's databases only then (seen
// 2026-11-07: no option for a version whose table is the one now, the
// option for one whose rows read otherwise). A table with more rows than a
// preview reads is not known to be the same, and counts as changed; the
// rows now are read only as many as the version had and one more, as a
// row past those already tells it changed.
func pageHistoryDatabaseUnchanged(
	app core.App, then pageHistoryDatabaseWire, sourceID string, thenState, nowState pageHistoryDatabaseState,
) (bool, error) {
	if then.HasMoreRows {
		return false, nil
	}
	left, leftErr := json.Marshal(thenState)
	right, rightErr := json.Marshal(nowState)
	if leftErr != nil || rightErr != nil {
		return false, errors.Join(leftErr, rightErr)
	}
	if string(left) != string(right) {
		return false, nil
	}
	now := pageHistoryDatabaseWire{}
	if err := fillPageHistoryDatabaseTableRows(app, &now, sourceID, nowState, math.MaxInt64, len(then.Rows)); err != nil {
		return false, err
	}
	if now.HasMoreRows {
		return false, nil
	}
	left, leftErr = json.Marshal(then.Rows)
	right, rightErr = json.Marshal(now.Rows)
	if leftErr != nil || rightErr != nil {
		return false, errors.Join(leftErr, rightErr)
	}
	return string(left) == string(right), nil
}

func pageHistoryLaterDatabases(
	app core.App, current *crdt.Doc, now, then map[string]structuralBlockMetadata,
) ([]pageHistoryDatabaseWire, error) {
	order, err := structuralOrder(current, "")
	if err != nil {
		return nil, err
	}
	later := []pageHistoryDatabaseWire{}
	for _, id := range order {
		if _, there := then[id]; there {
			continue
		}
		database, ok, err := pageHistoryDatabaseOf(app, id, now[id])
		if err != nil {
			return nil, err
		}
		if ok {
			database.Later = true
			later = append(later, database)
		}
	}
	return later, nil
}

func pageHistoryDatabaseOf(
	app core.App, blockID string, entry structuralBlockMetadata,
) (pageHistoryDatabaseWire, bool, error) {
	switch entry.Type {
	case graphBlockTypeInlineDatabase:
		sourceID, _ := entry.Content["dataSourceId"].(string)
		title := ""
		err := app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"id": sourceID}).Row(&title)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return pageHistoryDatabaseWire{}, false, err
		}
		linked, _ := entry.Content["linkedDatabaseSource"].(bool)
		return pageHistoryDatabaseWire{BlockID: blockID, Title: title, Linked: linked}, true, nil
	case graphBlockTypeChildPage, graphBlockTypeChildDatabase:
		target, _ := entry.Content["childPageId"].(string)
		if target == "" {
			return pageHistoryDatabaseWire{}, false, nil
		}
		page, err := app.FindRecordById(pagesCollection, target)
		if errors.Is(err, sql.ErrNoRows) {
			return pageHistoryDatabaseWire{}, false, nil
		}
		if err != nil || !isFullPageDatabase(page) {
			return pageHistoryDatabaseWire{}, false, err
		}
		return pageHistoryDatabaseWire{BlockID: blockID, Title: page.GetString("title"), FullPage: true}, true, nil
	}
	return pageHistoryDatabaseWire{}, false, nil
}

// pageHistoryDatabaseViews names a database block's view tabs in their
// order, which a full-page database's preview shows over its table as
// Notion's does (seen 2026-11-05). They are its views now: the history keeps
// rows and properties, not views.
func pageHistoryDatabaseViews(app core.App, blockID string) ([]pageHistoryDatabaseViewWire, error) {
	stored := []struct {
		Name   string `db:"name"`
		Type   string `db:"type"`
		Config string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[view]].[[name]], [[view]].[[type]], [[view]].[[config]]
		FROM {{leafen_database_container_views}} AS [[membership]]
		JOIN {{leafen_database_views}} AS [[view]] ON [[view]].[[id]] = [[membership]].[[view_id]]
		WHERE [[membership]].[[container_id]] = {:containerId}
		ORDER BY [[membership]].[[position]], [[membership]].[[view_id]] LIMIT 64
	`).Bind(dbx.Params{"containerId": blockID}).All(&stored); err != nil {
		return nil, err
	}
	views := make([]pageHistoryDatabaseViewWire, 0, len(stored))
	for _, view := range stored {
		// A widget reads under its dashboard, not as a tab of its own.
		if dashboardWidgetOwner([]byte(view.Config)) != "" {
			continue
		}
		views = append(views, pageHistoryDatabaseViewWire{
			Name: view.Name, Type: view.Type, PropertyWidths: pageHistoryViewPropertyWidths(view.Config),
		})
	}
	return views, nil
}

// pageHistoryViewPropertyWidths reads the column widths a view was set to,
// which a version's table is drawn with as the view draws it.
func pageHistoryViewPropertyWidths(config string) map[string]float64 {
	parsed := struct {
		PropertyWidths map[string]any `json:"propertyWidths"`
	}{}
	if json.Unmarshal([]byte(config), &parsed) != nil || len(parsed.PropertyWidths) == 0 {
		return nil
	}
	widths := map[string]float64{}
	for id, value := range parsed.PropertyWidths {
		if width, ok := value.(float64); ok && width > 0 {
			widths[id] = width
		}
	}
	if len(widths) == 0 {
		return nil
	}
	return widths
}
