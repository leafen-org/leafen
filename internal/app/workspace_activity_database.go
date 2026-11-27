package app

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"

	"github.com/pocketbase/pocketbase/core"
)

func recordWorkspaceDatabaseActivities(app core.App, page *core.Record, version storedPageHistoryVersion, origin string,
	before map[string]pageHistoryDatabaseState, after pageHistoryDatabaseChanges, restored map[string]bool, jobID, wikiHome string) error {
	// Lines of one time list as written, the last first (workspaceActivityOrder):
	// a database's own line, then its properties' and its views', each in
	// its order — a new database's "보기", "속성", "컬렉션" from the top, as
	// Notion's.
	for _, sourceID := range slices.Sorted(maps.Keys(after)) {
		now := after[sourceID]
		was := before[sourceID]
		var source *pageHistoryDatabaseSource
		if now != nil {
			source = now.Source
		}
		// A view's or a property's line names its database (workspaceActivityMetadata.DatabaseName).
		databaseName := ""
		if source != nil {
			databaseName = source.Title
		} else if was.Source != nil {
			databaseName = was.Source.Title
		}
		write := func(entity, id, action, name, previousName, kind string) error {
			metadata := workspaceActivityMetadata{OperationJobID: jobID, Name: name, PreviousName: previousName, Kind: kind, LocationID: page.Id, SourceID: sourceID}
			if entity != "dataSource" {
				metadata.DatabaseName = databaseName
			}
			return recordWorkspaceActivity(app, page, version.ActorID, origin, entity, id, action, metadata, version.CreatedAt)
		}
		if was.Source == nil && source != nil {
			if err := write("dataSource", sourceID, "create", source.Title, "", ""); err != nil {
				return err
			}
		} else if was.Source != nil && source == nil {
			if err := write("dataSource", sourceID, "delete", was.Source.Title, "", ""); err != nil {
				return err
			}
		} else if was.Source != nil && source != nil && !workspaceActivityEqual(was.Source, source) {
			action := "edit"
			previousName := ""
			if was.Source.Title != source.Title {
				action = "rename"
				previousName = was.Source.Title
			}
			if err := write("dataSource", sourceID, action, source.Title, previousName, ""); err != nil {
				return err
			}
		}
		properties := map[string]pageHistoryDatabaseProperty{}
		views := map[string]pageHistoryDatabaseView{}
		var nowProperties []pageHistoryDatabaseProperty
		var nowViews []pageHistoryDatabaseView
		if now != nil {
			nowProperties, nowViews = now.Properties, now.Views
			for _, property := range now.Properties {
				properties[property.ID] = property
			}
			for _, view := range now.Views {
				views[view.ID] = view
			}
		}
		oldProperties := map[string]pageHistoryDatabaseProperty{}
		for _, property := range was.Properties {
			oldProperties[property.ID] = property
		}
		for _, property := range nowProperties {
			id := property.ID
			old, exists := oldProperties[id]
			action, previousName := "create", ""
			if restored[id] {
				action = "restore"
			}
			if exists {
				if workspaceActivityEqual(old, property) {
					continue
				}
				action = "edit"
				if old.Name != property.Name {
					action = "rename"
					previousName = old.Name
				} else if old.Position != property.Position {
					action = "move"
				}
			}
			if err := write("property", id, action, property.Name, previousName, property.Type); err != nil {
				return err
			}
		}
		for _, property := range was.Properties {
			if _, remains := properties[property.ID]; !remains {
				if err := write("property", property.ID, "delete", property.Name, "", property.Type); err != nil {
					return err
				}
			}
		}
		// A wiki's Home, made with its database, is the first of the wiki's
		// views in its updates, as Notion's ("〈wiki〉에서 홈 보기를
		// 생성했습니다" under its other views, seen 2026-11-17 at 1416): no view
		// of the database, it is named in the reader's language by its kind.
		if sourceID == wikiHome {
			if err := write("view", wikiHomeViewID, "create", "", "", wikiHomeViewKind); err != nil {
				return err
			}
		}
		oldViews := map[string]pageHistoryDatabaseView{}
		for _, view := range was.Views {
			oldViews[view.ID] = view
		}
		for _, view := range nowViews {
			id := view.ID
			old, exists := oldViews[id]
			action, previousName := "create", ""
			if exists {
				if workspaceActivitySameView(old, view) {
					continue
				}
				action = "edit"
				if old.Name != view.Name {
					action = "rename"
					previousName = old.Name
				} else if old.Position != view.Position {
					action = "move"
				}
			}
			if err := write("view", id, action, view.Name, previousName, view.Type); err != nil {
				return err
			}
		}
		for _, view := range was.Views {
			if _, remains := views[view.ID]; !remains {
				if err := write("view", view.ID, "delete", view.Name, "", view.Type); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func workspaceActivityEqual(before, after any) bool {
	a, err := json.Marshal(before)
	if err != nil {
		return false
	}
	b, err := json.Marshal(after)
	return err == nil && string(a) == string(b)
}

// databaseViewRowOrderKey is a view's own order of its rows in its settings,
// which a row made, moved, copied or removed in the view changes. It is no
// edit of the view's settings: not in the workspace's updates
// (workspaceActivitySameView) nor in a view.updated event
// (databaseViewChangedFields), as Notion's view keeps the page order apart
// from the name, filter, sorts and configuration its API and events name
// (seen 2026-11-13: a row dragged or added in Notion's table moved the view's
// last_edited_time while its API object stayed the same).
const databaseViewRowOrderKey = "rowOrder"

// workspaceActivitySameView tells whether a view changed only where its rows
// stand: the view's own order of rows (`rowOrder`) changes as rows are made,
// moved, copied or removed in it, and Notion's updates list those as the
// rows' own lines, never as an edit of the view (seen 2026-11-13: a row made
// in a table or a card made on a board is the row's creation alone). Its
// settings — filters, sorts, columns, layout — still count. The page's
// history keeps the order (pageHistoryDatabaseView), so restoring a version
// still puts its rows back where they stood.
func workspaceActivitySameView(before, after pageHistoryDatabaseView) bool {
	settings := func(view pageHistoryDatabaseView) (pageHistoryDatabaseView, any) {
		var config map[string]any
		if json.Unmarshal(view.Config, &config) != nil || config == nil {
			return view, nil
		}
		delete(config, databaseViewRowOrderKey)
		view.Config = nil
		return view, config
	}
	left, leftConfig := settings(before)
	right, rightConfig := settings(after)
	return workspaceActivityEqual(left, right) && reflect.DeepEqual(leftConfig, rightConfig)
}
