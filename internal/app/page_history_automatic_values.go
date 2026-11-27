package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database page's automatic properties — who made it and when, who last
// edited it and when — are its record's, not values its history keeps, so a
// version's preview reads them from the versions, as Notion's version
// preview shows them (seen 2026-11-18 at 1416 in two tabs minutes apart, a
// database page's and a wiki page's own preview and a database's rows in
// its preview): made as the page was; last edited as the latest version up
// to the one shown that changed the page's own record — its making, its
// title, icon or values, a restore — so a version that changed the page's
// body alone shows the edit before it (Notion's versions of three body edits
// showed the value edit or the making before them; one more, once, its own).

// pageHistoryAutomaticTypes are the properties a version reads so.
var pageHistoryAutomaticTypes = map[string]bool{
	graphValueTypeCreatedTime: true, graphValueTypeCreatedBy: true,
	graphValueTypeLastEditedTime: true, graphValueTypeLastEditedBy: true,
}

// pageHistoryAuthorship is a database page's maker and last editor, each
// with when.
type pageHistoryAuthorship struct {
	CreatedAt int64  `db:"created_at"`
	CreatedBy string `db:"created_by"`
	EditedAt  int64  `db:"edited_at"`
	EditedBy  string `db:"edited_by"`
}

// pageHistoryRecordEditSQL is the condition that a version, aliased
// [[version]], changed its page's own record: its first version, or one
// whose event is the page's state or a restore, an edit (pageHistoryEditVersion).
var pageHistoryRecordEditSQL = `(` + pageHistoryEditVersion("version") + `) AND ([[version]].[[source]]='` + pageHistoryVersionBaseline + `'
	OR EXISTS (SELECT 1 FROM {{leafen_page_history_events}} AS [[event]]
		WHERE [[event]].[[page_id]]=[[version]].[[page_id]] AND [[event]].[[version_cursor]]=[[version]].[[version_cursor]]
			AND [[event]].[[kind]] IN ('` + pageHistoryEventPage + `','` + pageHistoryEventMetadata + `','` + pageHistoryEventRestore + `')))`

// pageHistoryAuthorshipAt reads a database page's authorship at its version
// cursor when it is at least 0, else at a time: its maker as its row reads
// it (graphRowPayloadColumn); its last editor the latest version then that
// changed its record, else as its row reads it now.
func pageHistoryAuthorshipAt(app core.App, pageID string, cursor int, at int64) (pageHistoryAuthorship, error) {
	authorship := pageHistoryAuthorship{}
	err := app.DB().NewQuery(`
		SELECT COALESCE([[page]].[[created]], [[row]].[[created]], 0) AS [[created_at]],
			COALESCE((SELECT NULLIF([[imported]].[[created_source]], '') FROM {{leafen_notion_imported_authors}} [[imported]]
				WHERE [[imported]].[[page_id]] = [[row]].[[id]]), [[row_record]].[[created_by]], '') AS [[created_by]],
			COALESCE([[page]].[[updated]], [[row]].[[updated]], 0) AS [[edited_at]],
			COALESCE([[row_record]].[[last_edited_by]], '') AS [[edited_by]]
		` + graphRowPayloadFrom + `
		WHERE [[row]].[[id]]={:pageId} LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID}).One(&authorship)
	if err != nil {
		return authorship, err
	}
	bound, params := `[[version]].[[version_cursor]]<={:cursor}`, dbx.Params{"pageId": pageID, "cursor": cursor, "at": at}
	if cursor < 0 {
		bound = `[[version]].[[created_at]]<={:at}`
	}
	edit := struct {
		ActorID   string `db:"actor_id"`
		CreatedAt int64  `db:"created_at"`
	}{}
	err = app.DB().NewQuery(`
		SELECT [[version]].[[actor_id]], [[version]].[[created_at]] FROM {{leafen_page_history_versions}} AS [[version]]
		WHERE [[version]].[[page_id]]={:pageId} AND ` + bound + ` AND ` + pageHistoryRecordEditSQL + `
		ORDER BY [[version]].[[version_cursor]] DESC LIMIT 1
	`).Bind(params).One(&edit)
	if errors.Is(err, sql.ErrNoRows) {
		return authorship, nil
	}
	if err != nil {
		return authorship, err
	}
	authorship.EditedAt, authorship.EditedBy = edit.CreatedAt, edit.ActorID
	return authorship, nil
}

// fillPageHistoryAutomaticValues gives a version's values its automatic
// properties' as the version shows them (pageHistoryAuthorshipAt), for
// the properties among those given; values the version kept stay.
func fillPageHistoryAutomaticValues(
	app core.App, pageID string, cursor int, at int64, propertyTypes map[string]string, values map[string]pageHistoryRowValue,
) error {
	automatic := false
	for _, kind := range propertyTypes {
		automatic = automatic || pageHistoryAutomaticTypes[kind]
	}
	if !automatic {
		return nil
	}
	authorship, err := pageHistoryAuthorshipAt(app, pageID, cursor, at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	for propertyID, kind := range propertyTypes {
		var value any
		switch kind {
		case graphValueTypeCreatedTime:
			value = databaseRowPageTime(authorship.CreatedAt)
		case graphValueTypeCreatedBy:
			value = authorship.CreatedBy
		case graphValueTypeLastEditedTime:
			value = databaseRowPageTime(authorship.EditedAt)
		case graphValueTypeLastEditedBy:
			value = authorship.EditedBy
		default:
			continue
		}
		if value == "" {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		values[propertyID] = pageHistoryRowValue{Type: kind, Value: encoded}
	}
	return nil
}

// pageHistoryPageAutomaticTypes are a database page's database's automatic
// properties now, by their IDs, which its version preview draws.
func pageHistoryPageAutomaticTypes(app core.App, pageID string) (map[string]string, error) {
	stored := []struct {
		ID   string `db:"id"`
		Type string `db:"type"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[property]].[[id]], [[property]].[[type]] FROM {{leafen_database_properties}} AS [[property]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[data_source_id]]=[[property]].[[data_source_id]]
		WHERE [[page]].[[id]]={:pageId} AND [[page]].[[data_source_id]]!=''
			AND [[property]].[[type]] IN ({:createdTime},{:createdBy},{:editedTime},{:editedBy})
			AND ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + `
	`).Bind(dbx.Params{
		"pageId": pageID, "createdTime": graphValueTypeCreatedTime, "createdBy": graphValueTypeCreatedBy,
		"editedTime": graphValueTypeLastEditedTime, "editedBy": graphValueTypeLastEditedBy,
	}).All(&stored); err != nil {
		return nil, err
	}
	types := make(map[string]string, len(stored))
	for _, property := range stored {
		types[property.ID] = property.Type
	}
	return types, nil
}

// fillPageHistorySnapshotAutomaticValues gives a database page's version
// its database's automatic properties as the version shows them.
func fillPageHistorySnapshotAutomaticValues(app core.App, pageID string, cursor int, state *pageHistoryPageState) error {
	types, err := pageHistoryPageAutomaticTypes(app, pageID)
	if err != nil || len(types) == 0 {
		return err
	}
	if state.Values == nil {
		state.Values = map[string]pageHistoryRowValue{}
	}
	return fillPageHistoryAutomaticValues(app, pageID, cursor, 0, types, state.Values)
}

// The MCP version tools (view-version of a database or a row page,
// get-version-changes' rows) read a row's automatic values as its version
// preview shows them: who last edited it and when as the latest version by
// then that changed its record (pageHistoryAuthorshipAt), who made it and
// when as it was made; an edited row's changes leave them out, as the
// preview outlines none of them (pageHistoryChanges.ts).

// factsAt reads a row's facts as its version at a time shows them.
func (reader mcpVersionRowReader) factsAt(source mcpVersionSource, rowID string, at int64) (mcpDatabaseRowFacts, error) {
	facts, err := loadMCPDatabaseRowFacts(reader.app, []databaseQueryRow{{ID: rowID}})
	if err != nil {
		return mcpDatabaseRowFacts{}, err
	}
	fact := facts[rowID]
	fact.RowID, fact.DataSourceID = rowID, source.ID
	edits := slices.ContainsFunc(source.Properties, func(property notionMCPDataSourceProperty) bool {
		kind := normalizeNotionDatabaseRowPagePropertyType(property.Type)
		return kind == graphValueTypeLastEditedTime || kind == graphValueTypeLastEditedBy
	})
	if !edits {
		return fact, nil
	}
	authorship, err := pageHistoryAuthorshipAt(reader.app, rowID, -1, at)
	if errors.Is(err, sql.ErrNoRows) {
		return fact, nil
	}
	if err != nil {
		return fact, err
	}
	fact.UpdatedAt, fact.LastEditedBy = authorship.EditedAt, authorship.EditedBy
	return fact, nil
}

// withoutAutomatic is a row's values by column without its automatic
// properties'.
func (source mcpVersionSource) withoutAutomatic(values map[string]any) map[string]any {
	kept := maps.Clone(values)
	for _, property := range source.Properties {
		if pageHistoryAutomaticTypes[normalizeNotionDatabaseRowPagePropertyType(property.Type)] {
			delete(kept, mcpSQLiteColumnName(property.Name))
		}
	}
	return kept
}
