package app

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// recordDatabaseOptionTypeChange keeps a status made a select and back as
// Notion keeps it (seen 2026-11-07: status to select and back took back its
// 할 일·진행 중·완료 groups and where its options stood). The two read their
// cells alike, so the cells stay as they are; the change is kept as a
// conversion with no cells to convert, which a change back finds as any
// type's does. A status taken back keeps the options it has now: those it had
// go back to their groups, those added since to its first; and it has no
// default, as Notion's (seen 2026-11-08: default_option null, so its rows
// that set none, empty as a select, stay empty). A status asked for with the
// very groups it had is the change undone (⌘Z), which puts it back as it
// was, its default too, as Notion's undo does (seen 2026-11-12: a row that
// read its default "시작 전" still does after a reload); a change back from
// the type menu or the API makes new groups. Asked for asAsked (the API's
// and MCP's writes), a status takes the settings asked for, a new status's,
// not those it had, as Notion's API and MCP give it (seen 2026-11-14: a
// status made a select and back has Notion's three options again and its
// first as default).
func recordDatabaseOptionTypeChange(
	app core.App, owner, workspaceID, pageID, actorID string, stored databasePropertyAuthority, property graphProperty, asAsked bool,
) (json.RawMessage, error) {
	returned, found, err := markDatabasePropertyConversionReturn(app, property.ID, stored.Type, property.Type)
	if err != nil {
		return nil, err
	}
	fromConfig, err := json.Marshal(stored.Config)
	if err != nil || string(fromConfig) == "null" {
		fromConfig = []byte("{}")
	}
	restoreOf := sql.NullString{}
	if found {
		restoreOf = sql.NullString{String: returned.ID, Valid: true}
	}
	now := time.Now().UTC()
	if _, err := app.DB().NewQuery(`INSERT INTO {{leafen_database_property_conversions}}
		([[id]],[[owner]],[[workspace_id]],[[page_id]],[[data_source_id]],[[property_id]],[[actor_id]],
		 [[from_type]],[[from_config]],[[to_type]],[[status]],[[created_at]],[[completed_at]],[[expires_at]],[[restore_of]])
		VALUES ({:id},{:owner},{:workspaceId},{:pageId},{:dataSourceId},{:propertyId},{:actorId},
		 {:fromType},{:fromConfig},{:toType},'completed',{:now},{:now},{:expires},{:restoreOf})`,
	).Bind(dbx.Params{
		"id": uuid.NewString(), "owner": owner, "workspaceId": workspaceID, "pageId": pageID,
		"dataSourceId": property.DataSourceID, "propertyId": property.ID, "actorId": actorID,
		"fromType": stored.Type, "fromConfig": string(fromConfig), "toType": property.Type,
		"now": now.UnixMilli(), "expires": now.Add(recoverableBlockTrashRetention).UnixMilli(), "restoreOf": restoreOf,
	}).Execute(); err != nil {
		return nil, err
	}
	if property.Type != graphValueTypeStatus {
		return nil, nil
	}
	if found && !asAsked {
		if taken, err := databaseStatusSettingsTakenBack(returned.FromConfig, property.Config); err != nil || taken != nil {
			return taken, err
		}
	}
	// A select made a status for the first time takes Notion's groups and
	// options, its own among them (databaseStatusConfigMadeFrom).
	return databaseStatusConfigMadeFrom(stored.Type, property.Config, newStatusWords(app, actorID))
}

// databaseStatusSettingsTakenBack gives a status the settings it had before
// (its groups), with the options asked for now in the groups they stood in
// then and no default; nil when it had no groups to take back.
func databaseStatusSettingsTakenBack(before string, requested json.RawMessage) (json.RawMessage, error) {
	old, next := map[string]any{}, map[string]any{}
	if json.Unmarshal([]byte(before), &old) != nil || (len(requested) > 0 && json.Unmarshal(requested, &next) != nil) {
		return nil, nil
	}
	groups, _ := old["statusGroups"].([]any)
	if len(groups) == 0 || databaseStatusGroupsSame(groups, next["statusGroups"]) {
		return nil, nil
	}
	options, asked := next["options"].([]any)
	if !asked {
		options, _ = old["options"].([]any)
	}
	groupOf := map[string]int{}
	for index, group := range groups {
		fields, _ := group.(map[string]any)
		ids, _ := fields["optionIds"].([]any)
		for _, id := range ids {
			if text, ok := id.(string); ok {
				groupOf[text] = index
			}
		}
	}
	members := make([][]any, len(groups))
	for _, option := range options {
		fields, ok := option.(map[string]any)
		if !ok {
			continue
		}
		id, _ := fields["id"].(string)
		index := groupOf[id]
		members[index] = append(members[index], id)
		if group, _ := groups[index].(map[string]any); group != nil {
			if name, _ := group["name"].(string); name != "" {
				fields["group"] = name
			}
		}
	}
	taken := make([]any, len(groups))
	for index, group := range groups {
		fields := map[string]any{}
		if stored, ok := group.(map[string]any); ok {
			for key, value := range stored {
				fields[key] = value
			}
		}
		fields["optionIds"] = append([]any{}, members[index]...)
		taken[index] = fields
	}
	merged := map[string]any{}
	for key, value := range next {
		merged[key] = value
	}
	for key, value := range old {
		merged[key] = value
	}
	merged["options"] = options
	merged["statusGroups"] = taken
	merged["defaultOptionId"] = ""
	return json.Marshal(merged)
}

// databaseStatusGroupsSame reports settings that name the groups a status had,
// each by its ID in the order it stood.
func databaseStatusGroupsSame(before []any, requested any) bool {
	asked, _ := requested.([]any)
	if len(asked) != len(before) {
		return false
	}
	for index := range before {
		was, _ := before[index].(map[string]any)
		now, _ := asked[index].(map[string]any)
		id, _ := was["id"].(string)
		if id == "" || now == nil || now["id"] != id {
			return false
		}
	}
	return true
}
