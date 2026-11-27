package app

import "strings"

// The keys a contract's free-form fields hold (a Record<string, unknown>, an
// any), as the screen writes them, so an agent reading leafen_operations
// reaches on its first call what a person does with a menu: a property's
// visibility on its row pages, a relation shown as a section, a task
// database and its dependencies, a template's values and repeat time, a
// database's page layout, an automation's trigger and steps, an app's
// action input, a form's answers and a comment's text. Keyed by record and
// field, lowercase: a record by its contract's name (GraphProperty), as what
// a handler reads is named too, or a handler's own type by its Go name
// (native_operation_field_shapes_more.go holds the rest).

var nativeOperationFieldShapes = map[string]string{
	"graphproperty.config":                                  `A property's settings. Every property: "hideWhenEmpty": true hides it on its row pages while its value is empty, and "hideInPagePanel": true always hides it there (the row page's property visibility "속성 표시 여부": both false or absent is "항상 표시", "비어있을 때 숨기기", "항상 숨기기"; the hidden ones sit under "속성 N개 더 보기"); "description": its description. Each type's own keys are in property_configs. Send the whole config back with your change: upsertProperty replaces it. notion-update-data-source's page_layout sets the visibility in Notion's form.`,
	"graphrowvalue.value":                                   `A cell's value by its type: title, text, url, email and phone a string (title and text with their formatting in spans); number a number; checkbox true or false; select and status an option's id (the property config's options), multi_select a list of option ids; person a list of user ids; relation a list of page ids; files a list of {"fileId": an uploaded file's ID, its size, type and hash taken from the file, with "name" to show it under another} or {"source": "external", "url", "name"}; date {"start": "YYYY-MM-DD" or "YYYY-MM-DDTHH:MM" with "timeZone", "end"}; place {"name", "address", "lat", "lon"}; verification {"state": "verified" or "unverified", "date": {"start", "end"}}; null clears it. notion-update-page writes values by name and option name instead.`,
	"graphview.config":                                      `A view's settings as the screen saves them (filters, sorts, grouping, shown properties, layout options). Change them with notion-update-view (its configure statements, ` + mcpViewDSLSpecURI + `), which writes this config; a content graph change sends the whole config back.`,
	"databasetoolbartaskfeaturewire.tasks":                  `The task database ("작업 데이터베이스로 전환"): {"enabled": true, "statusPropertyId": its status property, "assigneePropertyId": a person property, "dueDatePropertyId": a date property}, all three of this database (any that is missing goes in the operation's madeProperties, made in the same change, as the screen's "새로 만들기" does); a select as the status takes "statusWasSelect": true and, in madeProperties, itself as a status. {"enabled": false} turns it back, with that status as the select it was in madeProperties. Left out, the stored one stays.`,
	"databasetoolbartaskfeaturewire.dependencies":           `Dependencies ("종속성"): {"enabled": true, "shiftMode": "overlap" (moves later tasks only when dates overlap; the default), "maintain_spacing" or "none", "avoidWeekends": true (the default), "dateMode": "single" (the default) with "datePropertyId": a date property (left out, the database's first, as the screen picks), or "separate" with "startDatePropertyId" and "endDatePropertyId"}. Turned on, the server makes the 선행 작업/후속 작업 relation pair itself, and, for a database with no date property, a date named 날짜 (Date) before it, which datePropertyId then names. Send the stored value to keep it: what you send replaces it, null turns it off.`,
	"databasetoolbartaskfeaturewire.subitems":               `Sub-items ("하위 항목"): {"enabled": true, "titlePropertyId": the title property (the default), "showToggleOnTitle": false (the default: a table's sub-item toggles and indents stand in its first column; true puts them on the title)}. Turned on, the server makes the parent and sub-item relation pair itself. Send the stored value to keep it: what you send replaces it, null turns it off.`,
	"databasetoolbarconfigmutationoperation.taskfeatures":   `{"revision", "tasks", "dependencies", "subitems"}: read the stored taskFeatures from GET …/toolbar-config and send them back changed, with expectedRevision and taskFeatures.revision both its taskFeatures.revision (configurationRevision). Property ids are the ones GET /pages/{id}/content-graph names the properties by (items of kind property).`,
	"databasetoolbartemplaterepeatwire.time":                `A template\'s repeat ("복제 주기") time: "HH:MM" in timeZone, or "midnight" for the screen's "자정 전후" (once between midnight and 3am, the screen's default). frequency daily, weekly (weekdays 0–6, Sunday 0), monthly (monthDay 1–31, or monthWeek 1–5 with one weekday; 5 is the last) or yearly; interval how many of them apart; startsOn "YYYY-MM-DD".`,
	"databasetoolbarconfigmutationoperation.madeproperties": `With configureTaskFeatures alone, in a request of that one change: up to three properties (graphproperty: id, dataSourceId, type, name, position, config) its tasks' roles name — a new person, date or status property, or the select chosen for the status as a status (or, turning tasks off, back as the select) — made or retyped with the settings in one change, so a refused change leaves none.`,
	"databasetoolbarconfigmutationoperation.properties":     `A template's values by property id, each as a cell's value (graphrowvalue.value: an option's id, not its name), replacing all of them; its title goes in title. notion-update-page's update_properties on the template's id writes them by name instead.`,
	"databasetoolbarconfigmutationoperation.propertyspans":  `A template's text values' formatting by property id: the text spans of each, which must spell its value.`,
	"databasepagelayoutinput.layout":                        `A database's page layout ("레이아웃 사용자 지정"), as GET answers it (null: the original page, one properties, one comments and one content module): {"heading": {"pinned": up to 15 property ids under the title, "labels": show their names, "backlinks": "" (each page's own), "show", "hover" or "off"}, "page" and "panel": modules in order, each {"id", "kind": "properties" (the one property group), "property" with "propertyId", "relations" with "relations": [{"propertyId", "display": "minimized" or "section" (표시 옵션 › 최소화, 페이지 섹션으로 표시)}], "comments" or "content" (one, on the page)}, "panelOpen", "propertyGroup": [{"kind": "section", "id", "name"} or {"kind": "property", "propertyId"}] in order, "structure": "simple" or "tabbed" with "tabs": [{"id", "dataSourceId", "relationPropertyId", "name"}], "options": {"inlineComments": "default" or "minimal", "pageComments", "propertyIcons", "fullWidth", "templates"}}. Each property is placed once, the title nowhere. A property's visibility is its config's (graphproperty.config), not the layout's; notion-update-data-source's page_layout and leafen_database_page_layout set both.`,
}

// nativeOperationFieldShapeAliases are fields that hold what another's does.
var nativeOperationFieldShapeAliases = map[string]string{
	"databaserowpageproperty.config":                    "graphproperty.config",
	"pagehistorydatabaseproperty.config":                "graphproperty.config",
	"workspacecsvmergenewproperty.config":               "graphproperty.config",
	"databasetoolbartemplatemetadatawire.properties":    "databasetoolbarconfigmutationoperation.properties",
	"databasetoolbartemplatemetadatawire.propertyspans": "databasetoolbarconfigmutationoperation.propertyspans",
}

// nativeOperationFieldShape is what a record's field holds, or "".
func nativeOperationFieldShape(record, field string) string {
	key := strings.ToLower(record + "." + field)
	if alias, found := nativeOperationFieldShapeAliases[key]; found {
		key = alias
	}
	if shape := nativeOperationFieldShapes[key]; shape != "" {
		return shape
	}
	return nativeOperationMoreFieldShapes[key]
}

// graphPropertyConfigShapes are each property type's own config keys, as the
// screen writes them, for a content graph change that makes or changes one.
var graphPropertyConfigShapes = map[string]string{
	graphValueTypeSelect:      `{"options": [{"id", "name", "color": a Notion color name, "description"}]}`,
	graphValueTypeMultiSelect: `{"options": [{"id", "name", "color", "description"}]}`,
	graphValueTypeStatus:      `{"options": [{"id", "name", "color", "group": its group's name}], "statusGroups": [{"id", "name", "color", "optionIds"}] (to do, in progress, complete), "defaultOptionId": the default, "" for none}`,
	graphValueTypeNumber:      `{"numberFormat": Notion's format name ("number", "comma" for number_with_commas, "percent", a currency such as "dollar" or "won"), "decimalPlaces": 0–5, "numberDisplay": "bar" or "ring", "numberDisplayColor", "numberDivideBy" (100), "numberShowValue"}`,
	graphValueTypeDate:        `{"dateFormat": "full", "short", "month_day_year", "day_month_year", "year_month_day" or "relative", "timeFormat": "12", "24" or "hidden"}`,
	graphValueTypePerson:      `{"notify": "users_and_groups", "users" or "none", "defaultCreator": true shows the row's creator while it names no one}`,
	graphValueTypeRelation:    `{"relationDatabaseId": the related data source, "relatedPropertyId": its counterpart on the related data source (two-way), "limit": 1 for one page}`,
	graphValueTypeRollup:      `{"rollupRelationPropertyId", "rollupTargetPropertyId", "rollupFunction": Notion's function name}`,
	graphValueTypeFormula:     `{"formula": the expression}`,
	graphValueTypeURL:         `{"showFullUrl": true shows its links whole everywhere (Notion's "Show full URL"), false short}`,
	graphValueTypeUniqueID:    `{"idPrefix": the prefix, unique among the workspace's data sources}`,
	graphValueTypeButton:      `{"button": its action document, {"version": 1, "actions": its steps}}`,
}
