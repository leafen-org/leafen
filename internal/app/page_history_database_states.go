package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's history keeps the databases the page holds, as Notion's version
// history shows and restores them: for each data source, its name and live
// properties when the page holds it, and its views on the page, a linked
// database's too. Rows keep their own history. A change to the page records
// in its version the data sources it left different, and a checkpoint keeps
// them all. Changes made before the history kept databases are not known: a
// version from then reads the earliest state known, so restoring it undoes
// only the changes made since.

type pageHistoryDatabaseSource struct {
	Title string `json:"title"`
	// Its name's formatting as stored (JSON spans), kept as text so states compare.
	TitleSpans  string `json:"titleSpans,omitempty"`
	Icon        string `json:"icon,omitempty"`
	IconType    string `json:"iconType,omitempty"`
	Description string `json:"description,omitempty"`
	// Its formatting as stored (JSON spans), kept as text so states compare.
	DescriptionSpans  string `json:"descriptionSpans,omitempty"`
	DescriptionHidden bool   `json:"descriptionHidden,omitzero"`
	Cover             string `json:"cover,omitempty"`
	CoverPosition     int    `json:"coverPosition,omitempty"`
}

type pageHistoryDatabaseProperty struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Name     string          `json:"name"`
	Position string          `json:"position"`
	Config   json.RawMessage `json:"config"`
}

type pageHistoryDatabaseView struct {
	ID          string          `json:"id"`
	ContainerID string          `json:"containerId,omitempty"`
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Position    string          `json:"position"`
	Config      json.RawMessage `json:"config"`
}

// pageHistoryDatabaseState is one data source as a page holds it.
type pageHistoryDatabaseState struct {
	// Source is set when the page holds the data source itself.
	Source     *pageHistoryDatabaseSource    `json:"source,omitempty"`
	Properties []pageHistoryDatabaseProperty `json:"properties,omitempty"`
	Views      []pageHistoryDatabaseView     `json:"views,omitempty"`
}

// pageHistoryDatabaseChanges are the data sources a change left different,
// each with its state after it, or nil when the page no longer holds it.
type pageHistoryDatabaseChanges map[string]*pageHistoryDatabaseState

func pageHistoryDatabaseJSON(text string) json.RawMessage {
	if json.Valid([]byte(text)) {
		return json.RawMessage(text)
	}
	encoded, _ := json.Marshal(text)
	return encoded
}

// loadPageHistoryDatabaseStates reads the databases a page holds now.
func loadPageHistoryDatabaseStates(
	app core.App, owner, pageID string,
) (map[string]pageHistoryDatabaseState, error) {
	params := dbx.Params{"owner": owner, "pageId": pageID}
	sources := []struct {
		ID                string `db:"id"`
		Title             string `db:"title"`
		TitleSpans        string `db:"title_spans"`
		Icon              string `db:"icon"`
		IconType          string `db:"icon_type"`
		Description       string `db:"description"`
		DescriptionSpans  string `db:"description_spans"`
		DescriptionHidden bool   `db:"description_hidden"`
		Cover             string `db:"cover"`
		CoverPosition     int    `db:"cover_position"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[title]],[[title_spans]],[[icon]],[[icon_type]],[[description]],[[description_spans]],[[description_hidden]],[[cover]],[[cover_position]]
		FROM {{leafen_data_sources}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} ORDER BY [[id]]
	`).Bind(params).All(&sources); err != nil {
		return nil, err
	}
	type record struct {
		ID           string `db:"id"`
		DataSourceID string `db:"data_source_id"`
		ContainerID  string `db:"container_id"`
		Type         string `db:"type"`
		Name         string `db:"name"`
		Position     string `db:"position"`
		Config       string `db:"config"`
	}
	properties := []record{}
	if err := app.DB().NewQuery(`
		SELECT [[property]].[[id]],[[property]].[[data_source_id]],[[property]].[[type]],
			[[property]].[[name]],[[property]].[[position]],[[property]].[[config]]
		FROM {{leafen_database_properties}} AS [[property]]
		WHERE [[property]].[[owner]]={:owner} AND [[property]].[[page_id]]={:pageId}
			AND ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + `
		ORDER BY [[property]].[[data_source_id]],[[property]].[[position]],[[property]].[[id]]
	`).Bind(params).All(&properties); err != nil {
		return nil, err
	}
	views := []record{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[data_source_id]],[[container_id]],[[type]],[[name]],[[position]],[[config]]
		FROM {{leafen_database_views}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId}
		ORDER BY [[data_source_id]],[[position]],[[id]]
	`).Bind(params).All(&views); err != nil {
		return nil, err
	}
	states := map[string]pageHistoryDatabaseState{}
	for _, source := range sources {
		states[source.ID] = pageHistoryDatabaseState{Source: &pageHistoryDatabaseSource{
			Title: source.Title, TitleSpans: source.TitleSpans, Icon: source.Icon, IconType: source.IconType,
			Description: source.Description, DescriptionSpans: source.DescriptionSpans, DescriptionHidden: source.DescriptionHidden,
			Cover: source.Cover, CoverPosition: source.CoverPosition,
		}}
	}
	for _, property := range properties {
		state := states[property.DataSourceID]
		state.Properties = append(state.Properties, pageHistoryDatabaseProperty{
			ID: property.ID, Type: property.Type, Name: property.Name,
			Position: property.Position, Config: pageHistoryDatabaseJSON(property.Config),
		})
		states[property.DataSourceID] = state
	}
	for _, view := range views {
		state := states[view.DataSourceID]
		state.Views = append(state.Views, pageHistoryDatabaseView{
			ID: view.ID, ContainerID: view.ContainerID, Type: view.Type, Name: view.Name,
			Position: view.Position, Config: pageHistoryDatabaseJSON(view.Config),
		})
		states[view.DataSourceID] = state
	}
	return states, nil
}

func pageHistoryDatabaseOwner(app core.App, pageID string) (string, error) {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return "", err
	}
	workspace, err := app.FindRecordById(workspacesCollection, page.GetString("workspace"))
	if err != nil {
		return "", err
	}
	return workspace.GetString("owner"), nil
}

// pageHistoryDatabasesNow is a page's databases as a checkpoint keeps them.
func pageHistoryDatabasesNow(app core.App, pageID string) (string, error) {
	owner, err := pageHistoryDatabaseOwner(app, pageID)
	if err != nil {
		return "", err
	}
	states, err := loadPageHistoryDatabaseStates(app, owner, pageID)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(states)
	return string(encoded), err
}

// pageHistoryCheckpointDatabases is what a checkpoint knows of its page's
// databases. One kept before databases were reads the earliest state known:
// the next checkpoint's that has them, or else the page's now, as nothing
// was recorded to change them since.
func pageHistoryCheckpointDatabases(
	app core.App, checkpoint pageHistoryCheckpoint,
) (map[string]pageHistoryDatabaseState, error) {
	encoded := checkpoint.DatabasesJSON
	if encoded == "" {
		err := app.DB().NewQuery(`
			SELECT [[databases_json]] FROM {{leafen_page_history_checkpoints}}
			WHERE [[page_id]]={:pageId} AND [[version_cursor]]>{:versionCursor} AND [[databases_json]]!=''
			ORDER BY [[version_cursor]] LIMIT 1
		`).Bind(dbx.Params{
			"pageId": checkpoint.PageID, "versionCursor": checkpoint.VersionCursor,
		}).Row(&encoded)
		if err == nil {
			encoded, err = unpackPageHistoryText(encoded)
		}
		if errors.Is(err, sql.ErrNoRows) {
			encoded, err = pageHistoryDatabasesNow(app, checkpoint.PageID)
		}
		if err != nil {
			return nil, err
		}
	}
	states := map[string]pageHistoryDatabaseState{}
	if err := json.Unmarshal([]byte(encoded), &states); err != nil {
		return nil, errors.Join(errors.New(errorCodePageHistoryFailed), err)
	}
	return states, nil
}

func (state *pageHistoryMaterializedState) applyDatabaseChanges(changes pageHistoryDatabaseChanges) {
	for id, database := range changes {
		if database == nil {
			delete(state.Databases, id)
			continue
		}
		state.Databases[id] = *database
	}
}

// pageHistoryDatabaseCapture compares a page's databases around a change.
type pageHistoryDatabaseCapture struct {
	owner           string
	pageID          string
	before          map[string]pageHistoryDatabaseState
	activityTrashed map[string]bool
	// containerBlocks are the blocks holding the page's databases before
	// the change, which a change of its blocks alone leaves be
	// (content_graph_changed_blocks.go).
	containerBlocks map[string]struct{}
}

// beginPageHistoryDatabaseCapture reads a page's databases before a change.
// Every checkpoint records them.
func beginPageHistoryDatabaseCapture(
	app core.App, owner, pageID string,
) (*pageHistoryDatabaseCapture, error) {
	capture := &pageHistoryDatabaseCapture{owner: owner, pageID: pageID, before: map[string]pageHistoryDatabaseState{}, activityTrashed: map[string]bool{}}
	// A page with no database is told so in one statement (2026-10-21: its
	// four reads before each block written and three after, while the change
	// held the one write connection).
	if present, err := pageHistoryHasDatabases(app, owner, pageID); err != nil || !present {
		return capture, err
	}
	before, err := loadPageHistoryDatabaseStates(app, owner, pageID)
	if err != nil {
		return nil, err
	}
	trashed := []string{}
	if err := app.DB().NewQuery(`SELECT [[property]].[[id]] FROM {{leafen_database_properties}} [[property]]
		JOIN {{leafen_database_property_trash}} [[trash]] ON [[trash]].[[property_id]]=[[property]].[[id]]
		WHERE [[property]].[[owner]]={:owner} AND [[property]].[[page_id]]={:pageId}`).Bind(dbx.Params{"owner": owner, "pageId": pageID}).Column(&trashed); err != nil {
		return nil, err
	}
	capture.before = before
	if capture.containerBlocks, err = pageDatabaseContainerBlocks(app, owner, pageID); err != nil {
		return nil, err
	}
	for _, id := range trashed {
		capture.activityTrashed[id] = true
	}
	return capture, nil
}

// pageHistoryHasDatabases is whether a page has any database's source,
// property (trashed or not) or view, any of which a capture compares.
func pageHistoryHasDatabases(app core.App, owner, pageID string) (bool, error) {
	present := false
	err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_data_sources}} WHERE [[owner]]={:owner} AND [[page_id]]={:pageId})
			OR EXISTS(SELECT 1 FROM {{leafen_database_properties}} WHERE [[owner]]={:owner} AND [[page_id]]={:pageId})
			OR EXISTS(SELECT 1 FROM {{leafen_database_views}} WHERE [[owner]]={:owner} AND [[page_id]]={:pageId})
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID}).Row(&present)
	return present, err
}

// finish reads the databases after the change and keeps those it changed.
func (capture *pageHistoryDatabaseCapture) finish(app core.App) (pageHistoryDatabaseChanges, error) {
	if capture == nil {
		return nil, nil
	}
	after := map[string]pageHistoryDatabaseState{}
	present, err := pageHistoryHasDatabases(app, capture.owner, capture.pageID)
	if err != nil {
		return nil, err
	}
	if present {
		if after, err = loadPageHistoryDatabaseStates(app, capture.owner, capture.pageID); err != nil {
			return nil, err
		}
	}
	changes := pageHistoryDatabaseChanges{}
	for id, state := range after {
		previous, existed := capture.before[id]
		if existed {
			left, leftErr := json.Marshal(previous)
			right, rightErr := json.Marshal(state)
			if leftErr != nil || rightErr != nil {
				return nil, errors.Join(leftErr, rightErr)
			}
			if string(left) == string(right) {
				continue
			}
		}
		changed := state
		changes[id] = &changed
	}
	for id := range capture.before {
		if _, kept := after[id]; !kept {
			changes[id] = nil
		}
	}
	if len(changes) == 0 {
		return nil, nil
	}
	return changes, nil
}
