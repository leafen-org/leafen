package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The official database types name a database after the type and give it these
// properties, its status these options and its first view this order, as
// Notion's API and MCP do (seen 2026-11-05). Names go in the reader's language
// through MCP and in English through the API. A tasks database is Leafen's task
// database. Skills are Notion's agent feature, which Leafen has none of.
type notionDatabaseTypeSchema struct {
	title      string
	properties [][2]string
	statuses   []mcpDatabaseSchemaOption
	shown      []string
}

const notionDatabaseTypeTasks, notionDatabaseTypeProjects, notionDatabaseTypeSkills = "tasks", "projects", "skills"

// A database made from the meeting notes template is Notion's meeting notes
// database: its API type is "meeting_notes" (seen 2026-11-11, a database made
// from Notion's "회의록" template).
const notionDatabaseTypeMeetingNotes, databaseStarterMeetings = "meeting_notes", "meetings"

// A wiki's database and data source are of Notion's type "wiki".
const notionDatabaseTypeWiki = "wiki"

// The docs hub template's key, whose database Notion's API gives no type.
const databaseStarterDocs = "docs"

var notionDatabaseTypeSchemas = map[string]notionDatabaseTypeSchema{
	notionDatabaseTypeTasks: {
		title:      "Tasks",
		properties: [][2]string{{"Task name", "TITLE"}, {"Assignee", "PEOPLE"}, {"Status", "STATUS"}, {"Due", "DATE"}},
		statuses: []mcpDatabaseSchemaOption{
			{Name: "Not started", Color: "default", Group: "to_do"}, {Name: "In progress", Color: "blue", Group: "in_progress"},
			{Name: "Done", Color: "green", Group: "complete"}, {Name: "Archived", Color: "gray", Group: "complete"},
		},
		shown: []string{"Task name", "Assignee", "Due", "Status"},
	},
	"projects": {
		title:      "Projects",
		properties: [][2]string{{"Project name", "TITLE"}, {"Owner", "PEOPLE"}, {"Status", "STATUS"}, {"Dates", "DATE"}},
		statuses: []mcpDatabaseSchemaOption{
			{Name: "Planning", Color: "blue", Group: "in_progress"}, {Name: "In progress", Color: "yellow", Group: "in_progress"},
			{Name: "Paused", Color: "purple", Group: "in_progress"}, {Name: "Backlog", Color: "default", Group: "to_do"},
			{Name: "Done", Color: "green", Group: "complete"}, {Name: "Cancelled", Color: "red", Group: "complete"},
		},
		shown: []string{"Project name", "Dates", "Status", "Owner"},
	},
}

const notionDatabaseTypeSkillsReason = "Skills are instructions for Notion's own agents, and Leafen has no built-in agent."

// statement writes a type's properties as notion-create-database's schema.
func (schema notionDatabaseTypeSchema) statement(translate func(string) string) string {
	columns := make([]string, 0, len(schema.properties))
	for _, property := range schema.properties {
		kind := property[1]
		if kind == "STATUS" {
			options := make([]string, 0, len(schema.statuses))
			for _, option := range schema.statuses {
				options = append(options, fmt.Sprintf("'%s':%s:%s", translate(option.Name), option.Color, option.Group))
			}
			kind += "(" + strings.Join(options, ", ") + ")"
		}
		columns = append(columns, fmt.Sprintf("%q %s", translate(property[0]), kind))
	}
	return "CREATE TABLE (" + strings.Join(columns, ", ") + ")"
}

// requestProperties writes a type's properties as the public API's schema.
func (schema notionDatabaseTypeSchema) requestProperties() ([]string, map[string]json.RawMessage) {
	objects := map[string]string{"TITLE": `{"title":{}}`, "PEOPLE": `{"people":{}}`, "STATUS": `{"status":{}}`, "DATE": `{"date":{}}`}
	keys, properties := []string{}, map[string]json.RawMessage{}
	for _, property := range schema.properties {
		keys = append(keys, property[0])
		properties[property[0]] = json.RawMessage(objects[property[1]])
	}
	return keys, properties
}

// shape gives a new database's operations what its type asks of them: the
// status's options when it was made without them, and its first view's order.
func (schema notionDatabaseTypeSchema) shape(operations []graphOperation, translate func(string) string, withStatuses bool) error {
	ids := map[string]string{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertProperty || operation.Property == nil {
			continue
		}
		ids[operation.Property.Name] = operation.Property.ID
		if !withStatuses || operation.Property.Type != graphValueTypeStatus {
			continue
		}
		property := operation.Property
		options, groups := notionStatusNewConfig(schema.statuses, translate, func(kind string, index int) string {
			return property.ID + "-" + kind + fmt.Sprint(index)
		})
		encoded, err := json.Marshal(map[string]any{"options": options, "statusGroups": groups})
		if err != nil {
			return err
		}
		property.Config = encoded
	}
	order := make([]string, 0, len(schema.shown))
	for _, name := range schema.shown {
		order = append(order, ids[translate(name)])
	}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertView || operation.View == nil {
			continue
		}
		config := map[string]any{}
		if err := json.Unmarshal(operation.View.Config, &config); err != nil {
			return err
		}
		config["propertyOrder"] = order
		encoded, err := json.Marshal(config)
		if err != nil {
			return err
		}
		operation.View.Config = encoded
	}
	return nil
}

// finishNotionDatabaseType records a new database's type on its source, and
// makes a tasks database Leafen's task database.
func finishNotionDatabaseType(app core.App, actorID string, authority databaseToolbarConfigAuthority, databaseType string) error {
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_data_sources}} SET [[database_type]]={:type} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"type": databaseType, "id": authority.DataSourceID}).Execute(); err != nil {
		return err
	}
	if databaseType != notionDatabaseTypeTasks {
		return nil
	}
	properties := []struct {
		ID   string `db:"id"`
		Type string `db:"type"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]],[[type]] FROM {{leafen_database_properties}} WHERE [[data_source_id]]={:id}`).
		Bind(dbx.Params{"id": authority.DataSourceID}).All(&properties); err != nil {
		return err
	}
	roles := databaseTaskRoles{Enabled: true}
	for _, property := range properties {
		switch property.Type {
		case graphValueTypeStatus:
			roles.StatusPropertyID = property.ID
		case graphValueTypePerson:
			roles.AssigneePropertyID = property.ID
		case graphValueTypeDate:
			roles.DueDatePropertyID = property.ID
		}
	}
	encoded, err := json.Marshal(roles)
	if err != nil {
		return err
	}
	revision := 0
	state := databaseToolbarMutationState{ActorID: actorID}
	return configureDatabaseToolbarTaskFeatures(app, authority, databaseToolbarConfigMutationOperation{
		ExpectedRevision: &revision,
		TaskFeatures:     &databaseToolbarTaskFeatureWire{Subitems: json.RawMessage(`{}`), Dependencies: json.RawMessage(`{}`), Tasks: encoded},
	}, &state)
}

// markProjectDatabases tells a read which of its data sources were made as
// projects, through the API or from the projects template (Notion's board
// from that template reads "+ 새 프로젝트", seen 2026-11-11), which as
// meeting notes, from that template (Notion's reads "+ 새 회의" and calls an
// untitled row "신규 회의", seen 2026-11-11), and which from the docs hub
// template (Notion's table reads "+ 새 문서", seen 2026-11-11): their new
// rows are called so on screen. A task database is marked too (Notion's reads
// "+ 새 작업"), as its settings say, so a copy kept for reading offline,
// where those settings aren't read, calls its rows so as well (2026-11-11:
// offline it read "+ 새 페이지").
func markProjectDatabases(app core.App, items []graphReadItem) error {
	for _, item := range items {
		if item.Kind != graphReadKindDataSource {
			continue
		}
		source, _ := item.Value["id"].(string)
		stored, origin, tasks := "", "", 0
		err := app.DB().NewQuery(`
			SELECT [[source]].[[database_type]], [[source]].[[starter_template]],
				COALESCE(json_extract([[features]].[[tasks_json]], '$.enabled'), 0)
			FROM {{leafen_data_sources}} AS [[source]]
			LEFT JOIN {{leafen_database_task_features}} AS [[features]] ON [[features]].[[data_source_id]] = [[source]].[[id]]
			WHERE [[source]].[[id]]={:id}`).
			Bind(dbx.Params{"id": source}).Row(&stored, &origin, &tasks)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if tasks == 1 {
			item.Value["taskDatabase"] = true
		}
		if stored == notionDatabaseTypeProjects || origin == notionDatabaseTypeProjects {
			item.Value["projectDatabase"] = true
		}
		if stored == notionDatabaseTypeMeetingNotes || origin == databaseStarterMeetings {
			item.Value["meetingDatabase"] = true
		}
		if origin == databaseStarterDocs {
			item.Value["docsDatabase"] = true
		}
	}
	return nil
}

// notionStoredDatabaseType is a data source's official type, or none: a
// wiki's for a wiki's (seen 2026-11-19: "wiki" on the wiki's database and
// data source), what it was made as, meeting notes for one made from that
// template, or tasks for a task database however it was made.
func notionStoredDatabaseType(app core.App, dataSourceID string) any {
	stored, origin, wiki := "", "", false
	if err := app.DB().NewQuery(`SELECT [[database_type]], [[starter_template]], [[wiki]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": dataSourceID}).Row(&stored, &origin, &wiki); err == nil && wiki {
		return notionDatabaseTypeWiki
	} else if err == nil && stored != "" {
		return stored
	} else if err == nil && origin == databaseStarterMeetings {
		return notionDatabaseTypeMeetingNotes
	}
	if task, err := databaseIsTaskDatabase(app, dataSourceID); err == nil && task {
		return notionDatabaseTypeTasks
	}
	return nil
}
