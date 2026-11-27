package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type preparedDatabaseStarter struct {
	Operations   []graphOperation
	Bodies       map[string][]newRowPageBlock
	Templates    []databaseToolbarConfigMutationOperation
	DataSourceID string
	ViewID       string
	TaskRoles    json.RawMessage
	PageLayout   json.RawMessage
	// The template it is made from, kept on its data source (database_starter_origin.go).
	Origin string
	// What its pages without a title are called (data_source_untitled_name.go).
	UntitledName string
	// RowIcons are its example rows' icons, which their pages hold.
	RowIcons []databaseStarterRowIcon
}

// databaseStarterRowIcon is an example row's icon, as Notion's starters give
// their example pages one (seen 2026-11-12: the docs hub's star, calendar and
// people, the task tracker's book and notepad).
type databaseStarterRowIcon struct {
	RowID, Icon, IconType string
}

// prepareDatabaseStarter readies a starter for a page, its example dates
// from now, on the clock of the person who starts it (shiftDatabaseStarterDates).
func prepareDatabaseStarter(template databaseStarterTemplate, pageID string, input databaseStarterApplyInput, labels map[string]string, now time.Time) (preparedDatabaseStarter, error) {
	result := preparedDatabaseStarter{Bodies: map[string][]newRowPageBlock{}, Origin: template.Key}
	selectedFeatures := map[string]bool{}
	for _, key := range input.Features {
		selectedFeatures[key] = true
	}
	removed := map[string]bool{}
	for _, feature := range template.Features {
		if !selectedFeatures[feature.Key] {
			for _, id := range feature.Properties {
				removed[id] = true
			}
		}
	}
	selectedViews := map[string]bool{}
	for _, key := range input.Views {
		selectedViews[key] = true
	}
	allowedViews := map[string]bool{}
	for _, view := range template.ViewChoices {
		if selectedViews[view.Key] {
			allowedViews[view.ID] = true
			for _, id := range view.RetainedProperties {
				delete(removed, id)
			}
		}
	}
	properties := template.Properties[:0]
	for _, property := range template.Properties {
		if !removed[property.ID] {
			properties = append(properties, property)
		}
	}
	template.Properties = properties
	views := template.Views[:0]
	for _, view := range template.Views {
		if allowedViews[view.ID] {
			config := map[string]any{}
			if json.Unmarshal(view.Config, &config) != nil {
				return result, databaseStarterInvalid()
			}
			view.Config, _ = json.Marshal(pruneDatabaseStarterConfig(config, removed))
			view.Position = fmt.Sprintf("a%08d", len(views))
			views = append(views, view)
		}
	}
	template.Views = views
	if len(views) == 0 || len(template.Sources) != 1 || len(template.Blocks) != 1 {
		return result, databaseStarterInvalid()
	}
	for index := range template.Rows {
		for id := range template.Rows[index].Values {
			if removed[id] {
				delete(template.Rows[index].Values, id)
			}
		}
	}
	for index := range template.Templates {
		values := map[string]any{}
		if json.Unmarshal(template.Templates[index].Properties, &values) != nil {
			return result, databaseStarterInvalid()
		}
		template.Templates[index].Properties, _ = json.Marshal(pruneDatabaseStarterConfig(values, removed))
	}
	if input.TaskDatabase {
		if err := prepareDatabaseStarterTaskDatabase(&template); err != nil {
			return result, err
		}
	}
	ids := map[string]string{"page": pageID}
	add := func(id string) { ids[id] = databaseStarterIdentity(pageID, input.MutationID, id) }
	for _, v := range template.Blocks {
		add(v.ID)
		// Put in place of a database, the template's database is that
		// database's block, so one opened alone stays where it is.
		if input.ReplaceBlockID != "" && input.ReplaceDataSourceID == "" {
			ids[v.ID] = input.ReplaceBlockID
		}
	}
	for _, v := range template.Sources {
		add(v.ID)
	}
	for _, v := range template.Properties {
		add(v.ID)
	}
	for _, v := range template.Views {
		add(v.ID)
	}
	for _, v := range template.Rows {
		add(v.ID)
	}
	for _, v := range template.RowBodies {
		add(v.ID)
	}
	for _, v := range template.Templates {
		add(v.ID)
		blocks := []graphBlock{}
		if json.Unmarshal(v.Blocks, &blocks) != nil {
			return result, databaseStarterInvalid()
		}
		for _, block := range blocks {
			add(block.ID)
		}
	}
	pageLayout, err := prepareDatabaseStarterPageLayout(template.PageLayout, ids)
	if err != nil {
		return result, err
	}
	result.PageLayout, template.PageLayout = pageLayout, nil
	content := map[string]any{}
	if json.Unmarshal(template.Blocks[0].Content, &content) != nil {
		return result, databaseStarterInvalid()
	}
	content["viewId"] = views[0].ID
	template.Blocks[0].Content, _ = json.Marshal(content)
	// Its item name, and the templates named with it, read as an item name.
	template.UntitledName = databaseStarterItemName(template.UntitledName, labels)
	for index := range template.Templates {
		template.Templates[index].Name = databaseStarterItemName(template.Templates[index].Name, labels)
	}
	encoded, err := json.Marshal(template)
	if err != nil {
		return result, err
	}
	var raw any
	if err = json.Unmarshal(encoded, &raw); err != nil {
		return result, err
	}
	encoded, err = json.Marshal(remapDatabaseStarterValue(raw, ids, labels))
	if err != nil {
		return result, err
	}
	var remapped databaseStarterTemplate
	if err = json.Unmarshal(encoded, &remapped); err != nil {
		return result, err
	}
	template = remapped
	if err = orderDatabaseStarterGroups(&template); err != nil {
		return result, err
	}
	shiftDatabaseStarterDates(template.Rows, now)
	if input.TaskDatabase || template.TaskDatabase {
		result.TaskRoles = databaseStarterTaskRoles(template)
	}
	result.DataSourceID, result.UntitledName = template.Sources[0].ID, template.UntitledName
	result.ViewID = template.Views[0].ID
	for index := range template.Blocks {
		block := template.Blocks[index]
		result.Operations = append(result.Operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &block})
	}
	for index := range template.Sources {
		source := template.Sources[index]
		source.PageID = pageID
		result.Operations = append(result.Operations, graphOperation{Kind: graphOperationKindUpsertDataSource, DataSource: &source})
	}
	for index := range template.Properties {
		property := template.Properties[index]
		result.Operations = append(result.Operations, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &property})
	}
	for index := range template.Views {
		view := template.Views[index]
		result.Operations = append(result.Operations, graphOperation{Kind: graphOperationKindUpsertView, View: &view})
	}
	for index := range template.Rows {
		row := template.Rows[index]
		if row.Icon != "" {
			result.RowIcons = append(result.RowIcons, databaseStarterRowIcon{RowID: row.ID, Icon: row.Icon, IconType: row.IconType})
		}
		result.Operations = append(result.Operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: &row})
	}
	for _, block := range template.RowBodies {
		result.Bodies[block.RowID] = append(result.Bodies[block.RowID], newRowPageBlock{ID: block.ID, ParentID: block.ParentID, Position: block.Position, Type: block.Type, Content: block.Content})
	}
	for _, template := range template.Templates {
		id, name, title, icon, position, isDefault := template.ID, template.Name, template.Title, template.Icon, template.Position, template.IsDefault
		operation := databaseToolbarConfigMutationOperation{Kind: "createTemplate", TemplateID: &id, Name: &name, Title: &title, Icon: &icon, Position: &position, IsDefault: &isDefault, Properties: template.Properties, PropertySpans: template.PropertySpans, Blocks: template.Blocks}
		if !validDatabaseToolbarTemplateCreate(operation) {
			return result, databaseStarterInvalid()
		}
		result.Templates = append(result.Templates, operation)
	}
	return result, nil
}

// prepareDatabaseStarterTaskDatabase makes the empty task database Notion's
// "새 개인 페이지 작업 데이터베이스" does (seen 2026-11-05, 2026-11-07): no example
// rows; no tracker either — its icon the grey ☑ My tasks' own tab shows, no
// description, its properties without the tracker's hints (no ⓘ on 담당자),
// its one view a plain table "모든 작업" with no icon of its own, its status a
// 39px checkbox column before the title, the title 248, 담당자 158 and 마감일
// 151 wide, and the title column counting its pages ("개수 0").
// taskDatabaseIcon is a new task database's icon, a picture of Leafen's
// drawing in the web build: a check in a box, #A6A299 in light and dark
// alike, as Notion's empty task database shows its own fixed picture rather
// than a coloured icon (seen 2026-11-15: its title, top bar, sidebar, "내 작업"
// source rows and source window). A database made before keeps its icon.
const taskDatabaseIcon = "/icons/v1/task-database.svg"

func prepareDatabaseStarterTaskDatabase(template *databaseStarterTemplate) error {
	// Notion's empty task database has no item name: its untitled pages read
	// "새 작업" (seen 2026-11-11).
	template.Rows, template.RowBodies, template.Templates, template.PageLayout, template.UntitledName = nil, nil, nil, nil, ""
	icon, iconType := taskDatabaseIcon, "image"
	for index := range template.Sources {
		source := &template.Sources[index]
		source.Icon, source.IconType = &icon, &iconType
		source.Description, source.DescriptionHidden, source.DescriptionSpans = nil, nil, nil
	}
	roles := map[string]string{}
	for index, property := range template.Properties {
		roles[property.Type] = property.ID
		config := map[string]any{}
		if len(property.Config) > 0 && json.Unmarshal(property.Config, &config) != nil {
			return databaseStarterInvalid()
		}
		delete(config, "description")
		template.Properties[index].Config, _ = json.Marshal(config)
	}
	status, title, person, date := roles[graphValueTypeStatus], roles[graphValueTypeTitle], roles[graphValueTypePerson], roles[graphValueTypeDate]
	for index := range template.Views {
		config := map[string]any{}
		if json.Unmarshal(template.Views[index].Config, &config) != nil {
			return databaseStarterInvalid()
		}
		delete(config, "icon")
		config["propertyOrder"] = []string{status, title, person, date}
		config["statusCheckboxColumns"] = []string{status}
		config["propertyWidths"] = map[string]int{status: 39, title: 248, person: 158, date: 151}
		config["calculations"] = map[string]string{title: "count_all"}
		template.Views[index].Config, _ = json.Marshal(config)
	}
	return nil
}

// orderDatabaseStarterGroups gives a view grouped by a select or tags that
// keeps no order of its own its groups by name, in the language it is made
// in, the group for no value among them as "uncategorized" — as a view
// grouped from its settings starts (web boardOptionGroupOrder) and as
// Notion's starters show them (docs hub "카테고리별", seen 2026-11-12:
// 카테고리 없음, 고객 리서치, 전략 문서, 제안, the options being 제안, 고객
// 리서치, 전략 문서, 계획 중). A view whose order the template gives (the
// brainstorm board's, as Notion's keeps it) or that sorts its groups keeps it.
func orderDatabaseStarterGroups(template *databaseStarterTemplate) error {
	options := map[string][][2]string{}
	for _, property := range template.Properties {
		if property.Type != graphValueTypeSelect && property.Type != graphValueTypeMultiSelect {
			continue
		}
		config := struct {
			Options []struct{ ID, Name string } `json:"options"`
		}{}
		if json.Unmarshal(property.Config, &config) != nil {
			return databaseStarterInvalid()
		}
		named := [][2]string{{"uncategorized", "__none"}}
		for _, option := range config.Options {
			named = append(named, [2]string{option.Name, option.ID})
		}
		sort.SliceStable(named, func(left, right int) bool { return named[left][0] < named[right][0] })
		options[property.ID] = named
	}
	for index := range template.Views {
		config := map[string]any{}
		if json.Unmarshal(template.Views[index].Config, &config) != nil {
			return databaseStarterInvalid()
		}
		groupBy, _ := config["groupBy"].(string)
		named, grouped := options[groupBy]
		_, ordered := config["groupOrder"]
		_, sorted := config["groupSort"]
		if !grouped || ordered || sorted {
			continue
		}
		order := make([]string, 0, len(named))
		for _, option := range named {
			order = append(order, option[1])
		}
		config["groupOrder"] = order
		template.Views[index].Config, _ = json.Marshal(config)
	}
	return nil
}

func databaseStarterTaskRoles(template databaseStarterTemplate) json.RawMessage {
	roles := databaseTaskRoles{Enabled: true}
	for _, property := range template.Properties {
		switch property.Type {
		case graphValueTypeStatus:
			roles.StatusPropertyID = property.ID
		case graphValueTypePerson:
			roles.AssigneePropertyID = property.ID
		case graphValueTypeDate:
			roles.DueDatePropertyID = property.ID
		}
	}
	encoded, _ := json.Marshal(roles)
	return encoded
}

// prepareDatabaseStarterPageLayout makes a starter's page layout this
// database's: its properties by their new ids, those the person left out
// dropped. It isn't remapped with the rest, whose "page" key is the page's id.
func prepareDatabaseStarterPageLayout(raw json.RawMessage, ids map[string]string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	layout := databasePageLayout{}
	if json.Unmarshal(raw, &layout) != nil || len(layout.Tabs) > 0 {
		return nil, databaseStarterInvalid()
	}
	pinned := []string{}
	for _, id := range layout.Heading.Pinned {
		if next, ok := ids[id]; ok {
			pinned = append(pinned, next)
		}
	}
	layout.Heading.Pinned = pinned
	group := []databasePageLayoutGroupItem{}
	for _, item := range layout.PropertyGroup {
		if item.Kind == "property" {
			next, ok := ids[item.PropertyID]
			if !ok {
				continue
			}
			item.PropertyID = next
		}
		group = append(group, item)
	}
	layout.PropertyGroup = group
	modules := func(list []databasePageLayoutModule) []databasePageLayoutModule {
		kept := []databasePageLayoutModule{}
		for _, module := range list {
			if module.Kind == "property" {
				next, ok := ids[module.PropertyID]
				if !ok {
					continue
				}
				module.PropertyID = next
			}
			kept = append(kept, module)
		}
		return kept
	}
	layout.Page, layout.Panel = modules(layout.Page), modules(layout.Panel)
	return json.Marshal(layout)
}
