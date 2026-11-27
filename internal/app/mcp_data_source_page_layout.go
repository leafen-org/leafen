package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// notion-update-data-source's page_layout and fetch's <page-layout>, in the
// shape Notion's MCP gives them (notion://docs/database-page-layouts, read
// 2026-11-13), over Leafen's page layout (database_page_layout.go) and its
// properties' visibility, which Leafen keeps on each property as the row
// page's "속성 표시 여부" menu sets it (hideWhenEmpty, hideInPagePanel).
// Notion's relations module names no relations; Leafen's "관계형 그룹" gives
// its own as relations: [{property, display}], and a views module is a
// relation shown as a section of the page (Notion's "페이지 섹션으로 표시").

type notionMCPPageLayout struct {
	Main    []notionMCPPageLayoutModule `json:"main" jsonschema:"Main area modules in order: an optional cover first, then one title, the properties module, property, views and relations modules, discussions, and the editor (in the tab instead when the layout has one)"`
	Sidebar []notionMCPPageLayoutModule `json:"sidebar,omitempty" jsonschema:"Sidebar modules in order: properties and property modules only"`
	Tab     *notionMCPPageLayoutTab     `json:"tab,omitempty" jsonschema:"Puts page content in a tab next to database views; leave out for a simple layout"`
	Format  *notionMCPPageLayoutFormat  `json:"format,omitempty" jsonschema:"Page display options; left out, they stay as they are"`
}

type notionMCPPageLayoutTab struct {
	ID      string                      `json:"id,omitempty" jsonschema:"The tab ID fetch gave (view_tab_…); leave out for a new tab"`
	Modules []notionMCPPageLayoutModule `json:"modules" jsonschema:"Tab modules in order, the editor among them: editor, discussions, relations, properties and property modules"`
}

type notionMCPPageLayoutFormat struct {
	PropertyIcons string `json:"propertyIcons,omitempty" jsonschema:"show or hide: whether property icons show on the page"`
	PageFullWidth *bool  `json:"pageFullWidth,omitempty" jsonschema:"Whether pages open at full width"`
}

type notionMCPPageLayoutModule struct {
	Type             string                        `json:"type" jsonschema:"cover, title, properties, property, views, relations, discussions or editor; backlinks, page_sections and bottom_controls, which Leafen doesn't draw, are left out and named in warnings"`
	PinnedProperties []string                      `json:"pinnedProperties,omitempty" jsonschema:"title: property names to show under the title"`
	PropertyLabels   string                        `json:"propertyLabels,omitempty" jsonschema:"title: show or hide the pinned properties' names"`
	Properties       []notionMCPPageLayoutEntry    `json:"properties,omitempty" jsonschema:"properties: the property group's properties in order, each with its visibility; unlisted ones follow in their order and show. Leave out with sections, or both to keep the order, visibility and sections as they are"`
	Sections         []notionMCPPageLayoutSection  `json:"sections,omitempty" jsonschema:"properties: named sections in order; unlisted properties go in the last one and show"`
	Property         string                        `json:"property,omitempty" jsonschema:"property: the property this module shows"`
	Config           *notionMCPPageLayoutStyle     `json:"config,omitempty" jsonschema:"property: Leafen draws each property in one style, so this is refused"`
	Relation         string                        `json:"relation,omitempty" jsonschema:"views: a relation property whose related pages show as a section of the page"`
	Relations        []notionMCPPageLayoutRelation `json:"relations,omitempty" jsonschema:"relations (Leafen's): the relation properties of the relations group in order, each minimized to one line or shown as a section; left out, the group keeps the ones it has"`
}

type notionMCPPageLayoutEntry struct {
	Property   string `json:"property" jsonschema:"The property name"`
	Visibility string `json:"visibility,omitempty" jsonschema:"show (the default), hide_if_empty or hide: how pages look, not who can see the value"`
}

type notionMCPPageLayoutSection struct {
	Name       string                     `json:"name" jsonschema:"The section name"`
	Properties []notionMCPPageLayoutEntry `json:"properties" jsonschema:"The section's properties in order"`
}

type notionMCPPageLayoutStyle struct {
	Style string `json:"style" jsonschema:"Notion's display style"`
}

type notionMCPPageLayoutRelation struct {
	Property string `json:"property" jsonschema:"The relation property name"`
	Display  string `json:"display,omitempty" jsonschema:"minimized (the default) or section"`
}

// mcpPageLayoutPlan is a page_layout read against a data source: the layout
// to store, the visibility changes its properties take, and the Notion
// modules Leafen leaves out.
type mcpPageLayoutPlan struct {
	Layout     databasePageLayout
	Revision   int
	Operations []graphOperation
	LeftOut    []string
}

// mcpPageLayoutProperties reads a data source's properties as its layout
// names them.
func mcpPageLayoutProperties(app core.App, location mcpDataSourceLocation) ([]storedMCPDataSourceProperty, error) {
	stored := []storedMCPDataSourceProperty{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[type]], [[position]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId} AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"pageId": location.PageID, "dataSourceId": location.ID}).All(&stored)
	return stored, err
}

// mcpPropertyVisibility is a property's visibility as Notion names it.
func mcpPropertyVisibility(config map[string]any) string {
	if hidden, _ := config["hideInPagePanel"].(bool); hidden {
		return "hide"
	}
	if empty, _ := config["hideWhenEmpty"].(bool); empty {
		return "hide_if_empty"
	}
	return "show"
}

// planMCPPageLayout reads a page_layout against a data source's properties
// and current layout. A properties module with neither properties nor
// sections keeps the group's order, sections and visibility.
func planMCPPageLayout(app core.App, location mcpDataSourceLocation, input notionMCPPageLayout) (mcpPageLayoutPlan, error) {
	stored, err := mcpPageLayoutProperties(app, location)
	if err != nil {
		return mcpPageLayoutPlan{}, err
	}
	currentResponse, err := loadDatabasePageLayout(app, location.ID)
	if err != nil {
		return mcpPageLayoutPlan{}, err
	}
	current := defaultDatabasePageLayout()
	if currentResponse.Layout != nil {
		current = *currentResponse.Layout
	}
	byName := map[string]storedMCPDataSourceProperty{}
	for _, property := range stored {
		byName[property.Name] = property
	}
	named := func(name string) (storedMCPDataSourceProperty, error) {
		property, found := byName[name]
		if !found {
			return property, mcpInvalid("page_layout names %q, which isn't one of the data source's properties: name each by its exact name", name)
		}
		if property.Type == graphValueTypeTitle {
			return property, mcpInvalid("page_layout lists the title %q: the title module shows it, so leave it out of the other modules", name)
		}
		return property, nil
	}
	layout := current
	layout.Heading = databasePageLayoutHeading{Pinned: []string{}, Labels: true, Backlinks: current.Heading.Backlinks}
	layout.Page, layout.Panel = []databasePageLayoutModule{}, []databasePageLayoutModule{}
	plan := mcpPageLayoutPlan{Revision: currentResponse.Revision}
	main := input.Main
	if len(main) > 0 && main[0].Type == "cover" {
		main = main[1:]
	}
	if len(main) == 0 || main[0].Type != "title" {
		return plan, mcpInvalid("page_layout.main starts with a title module, after the cover if it has one")
	}
	title := main[0]
	if title.PropertyLabels != "" && title.PropertyLabels != "show" && title.PropertyLabels != "hide" {
		return plan, mcpInvalid("the title's propertyLabels is show or hide")
	}
	layout.Heading.Labels = title.PropertyLabels != "hide"
	for _, name := range title.PinnedProperties {
		property, err := named(name)
		if err != nil {
			return plan, err
		}
		layout.Heading.Pinned = append(layout.Heading.Pinned, property.ID)
	}
	var group *notionMCPPageLayoutModule
	// A relations module that names none keeps the group's relations, which
	// a relation the layout places elsewhere leaves.
	relations, relationsAt, kept := []databasePageLayoutRelation{}, -1, map[string]bool{}
	editor, editorInMain := false, false
	place := func(region string, module notionMCPPageLayoutModule) error {
		target := &layout.Page
		if region == "sidebar" {
			target = &layout.Panel
		}
		allowed := map[string][]string{
			"main":    {"properties", "property", "views", "relations", "discussions", "editor", "backlinks", "page_sections", "bottom_controls"},
			"sidebar": {"properties", "property"},
			"tab":     {"editor", "discussions", "relations", "properties", "property"},
		}[region]
		if !oneOf(module.Type, allowed...) {
			return mcpInvalid("page_layout's %s takes only %s modules, not %q", region, strings.Join(allowed, ", "), module.Type)
		}
		switch module.Type {
		case "properties":
			if group != nil {
				return mcpInvalid("Leafen pages have one property group: give one properties module")
			}
			if module.Properties != nil && module.Sections != nil {
				return mcpInvalid("a properties module takes properties or sections, not both")
			}
			group = &module
			*target = append(*target, databasePageLayoutModule{ID: "properties", Kind: "properties"})
		case "property":
			if module.Config != nil {
				return mcpInvalid("Leafen draws property %q in its one style: leave the module's config out", module.Property)
			}
			property, err := named(module.Property)
			if err != nil {
				return err
			}
			*target = append(*target, databasePageLayoutModule{ID: "property-" + property.ID, Kind: "property", PropertyID: property.ID})
		case "views", "relations":
			entries, keeping := []notionMCPPageLayoutRelation{{Property: module.Relation, Display: "section"}}, false
			if module.Type == "relations" {
				entries, keeping = module.Relations, module.Relations == nil
				if keeping {
					// The group keeps the relations it has, as Notion's
					// relations module names none.
					for _, had := range databasePageLayoutRelations(current) {
						if property, found := mcpPropertyByID(stored, had.PropertyID); found {
							entries = append(entries, notionMCPPageLayoutRelation{Property: property.Name, Display: had.Display})
						}
					}
				}
			}
			for _, entry := range entries {
				property, err := named(entry.Property)
				if err != nil {
					return err
				}
				if property.Type != graphValueTypeRelation {
					return mcpInvalid("%q isn't a relation property: a %s module takes relations", entry.Property, module.Type)
				}
				display := entry.Display
				if display == "" {
					display = "minimized"
				}
				if display != "minimized" && display != "section" {
					return mcpInvalid("a relation's display is minimized or section")
				}
				placed := databasePageLayoutRelation{PropertyID: property.ID, Display: display}
				at := slices.IndexFunc(relations, func(relation databasePageLayoutRelation) bool { return relation.PropertyID == property.ID })
				switch {
				case at < 0:
					relations, kept[property.ID] = append(relations, placed), keeping
				case kept[property.ID] && !keeping:
					relations[at], kept[property.ID] = placed, false
				case !keeping:
					return mcpInvalid("%q is placed twice in page_layout: list each property once", entry.Property)
				}
			}
			if relationsAt < 0 {
				relationsAt = len(*target)
				*target = append(*target, databasePageLayoutModule{ID: "relations", Kind: "relations"})
			}
		case "discussions":
			*target = append(*target, databasePageLayoutModule{ID: "comments", Kind: "comments"})
		case "editor":
			if editor {
				return mcpInvalid("page_layout has one editor module")
			}
			editor, editorInMain = true, region == "main"
			*target = append(*target, databasePageLayoutModule{ID: "content", Kind: "content"})
		default:
			plan.LeftOut = append(plan.LeftOut, module.Type)
		}
		return nil
	}
	for _, module := range main[1:] {
		if module.Type == "cover" || module.Type == "title" {
			return plan, mcpInvalid("page_layout.main has one cover, first, and one title after it")
		}
		if err := place("main", module); err != nil {
			return plan, err
		}
	}
	if input.Tab != nil {
		if editorInMain {
			return plan, mcpInvalid("a layout with a tab has its editor in the tab, not in main")
		}
		for _, module := range input.Tab.Modules {
			if err := place("tab", module); err != nil {
				return plan, err
			}
		}
	}
	for _, module := range input.Sidebar {
		if err := place("sidebar", module); err != nil {
			return plan, err
		}
	}
	if !editor {
		return plan, mcpInvalid("page_layout needs an editor module: in main, or in the tab when it has one")
	}
	if group == nil {
		return plan, mcpInvalid("Leafen pages keep one property group: add a {\"type\":\"properties\"} module")
	}
	elsewhere := map[string]bool{}
	for _, id := range layout.Heading.Pinned {
		elsewhere[id] = true
	}
	for _, module := range append(append([]databasePageLayoutModule{}, layout.Page...), layout.Panel...) {
		elsewhere[module.PropertyID] = true
	}
	for _, section := range append([]notionMCPPageLayoutSection{{Properties: group.Properties}}, group.Sections...) {
		for _, entry := range section.Properties {
			if property, found := byName[entry.Property]; found {
				elsewhere[property.ID] = true
			}
		}
	}
	relations = slices.DeleteFunc(relations, func(relation databasePageLayoutRelation) bool {
		return kept[relation.PropertyID] && elsewhere[relation.PropertyID]
	})
	// A relations group of none isn't drawn, as Notion draws none.
	if relationsAt >= 0 {
		for _, modules := range []*[]databasePageLayoutModule{&layout.Page, &layout.Panel} {
			for index := range *modules {
				if (*modules)[index].Kind == "relations" {
					if len(relations) == 0 {
						*modules = append((*modules)[:index], (*modules)[index+1:]...)
					} else {
						(*modules)[index].Relations = relations
					}
					break
				}
			}
		}
	}
	placed := map[string]bool{}
	for _, id := range layout.Heading.Pinned {
		placed[id] = true
	}
	for _, module := range append(append([]databasePageLayoutModule{}, layout.Page...), layout.Panel...) {
		placed[module.PropertyID] = true
		for _, relation := range module.Relations {
			placed[relation.PropertyID] = true
		}
	}
	visibility := map[string]string{}
	switch {
	case group.Properties == nil && group.Sections == nil:
		layout.PropertyGroup = []databasePageLayoutGroupItem{}
		for _, item := range current.PropertyGroup {
			if item.Kind == "section" || !placed[item.PropertyID] {
				layout.PropertyGroup = append(layout.PropertyGroup, item)
			}
		}
	default:
		layout.PropertyGroup = []databasePageLayoutGroupItem{}
		sections := group.Sections
		if group.Properties != nil {
			sections = []notionMCPPageLayoutSection{{Properties: group.Properties}}
		}
		sectionIDs := map[string]string{}
		for _, item := range current.PropertyGroup {
			if item.Kind == "section" {
				sectionIDs[item.Name] = item.ID
			}
		}
		for index, section := range sections {
			if group.Sections != nil {
				if strings.TrimSpace(section.Name) == "" {
					return plan, mcpInvalid("each section has a name")
				}
				id := sectionIDs[section.Name]
				if id == "" {
					id = fmt.Sprintf("section-%d", index+1)
				}
				delete(sectionIDs, section.Name)
				layout.PropertyGroup = append(layout.PropertyGroup, databasePageLayoutGroupItem{Kind: "section", ID: id, Name: section.Name})
			}
			for _, entry := range section.Properties {
				property, err := named(entry.Property)
				if err != nil {
					return plan, err
				}
				if entry.Visibility != "" && !oneOf(entry.Visibility, "show", "hide_if_empty", "hide") {
					return plan, mcpInvalid("a property's visibility is show, hide_if_empty or hide")
				}
				if placed[property.ID] {
					return plan, mcpInvalid("%q is placed twice in page_layout: list each property once", entry.Property)
				}
				placed[property.ID] = true
				visibility[property.ID] = entry.Visibility
				layout.PropertyGroup = append(layout.PropertyGroup, databasePageLayoutGroupItem{Kind: "property", PropertyID: property.ID})
			}
		}
		// The properties it doesn't list follow, in their order, and show.
		for _, property := range stored {
			if property.Type != graphValueTypeTitle && !placed[property.ID] {
				visibility[property.ID] = "show"
			}
		}
	}
	for _, property := range stored {
		want, changes := visibility[property.ID]
		if !changes {
			continue
		}
		config := map[string]any{}
		if json.Unmarshal([]byte(property.Config), &config) != nil {
			return plan, graphStructureAuthorityError()
		}
		if want == "" {
			want = "show"
		}
		if mcpPropertyVisibility(config) == want {
			continue
		}
		config["hideInPagePanel"], config["hideWhenEmpty"] = want == "hide", want == "hide_if_empty"
		encoded, err := json.Marshal(config)
		if err != nil {
			return plan, err
		}
		property.Config = string(encoded)
		plan.Operations = append(plan.Operations, mcpDataSourcePropertyOperation(location.ID, property))
	}
	layout.Structure = "simple"
	if input.Tab != nil {
		layout.Structure = "tabbed"
	}
	if layout.Structure == "simple" || current.Structure != "tabbed" {
		layout.Tabs = []databasePageLayoutTab{}
	}
	if len(current.Panel) == 0 {
		layout.PanelOpen = len(layout.Panel) > 0
	}
	if format := input.Format; format != nil {
		if format.PropertyIcons != "" && format.PropertyIcons != "show" && format.PropertyIcons != "hide" {
			return plan, mcpInvalid("format.propertyIcons is show or hide")
		}
		if format.PropertyIcons != "" {
			layout.Options.PropertyIcons = format.PropertyIcons == "show"
		}
		if format.PageFullWidth != nil {
			layout.Options.FullWidth = *format.PageFullWidth
		}
	}
	plan.Layout = layout
	return plan, nil
}

// databasePageLayoutRelations are a layout's relations group's relations.
func databasePageLayoutRelations(layout databasePageLayout) []databasePageLayoutRelation {
	for _, module := range append(append([]databasePageLayoutModule{}, layout.Page...), layout.Panel...) {
		if module.Kind == "relations" {
			return module.Relations
		}
	}
	return nil
}

func mcpPropertyByID(properties []storedMCPDataSourceProperty, id string) (storedMCPDataSourceProperty, bool) {
	for _, property := range properties {
		if property.ID == id {
			return property, true
		}
	}
	return storedMCPDataSourceProperty{}, false
}

// storeMCPPageLayout checks a planned layout as the layout editor's is
// checked and stores it at the next revision.
func storeMCPPageLayout(app core.App, location mcpDataSourceLocation, plan mcpPageLayoutPlan, actor string, now int64) error {
	raw, err := json.Marshal(plan.Layout)
	if err != nil {
		return err
	}
	layout, err := parseDatabasePageLayout(app, raw, location.PageID, location.ID, location.WorkspaceID)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(layout)
	if err != nil {
		return err
	}
	return storeDatabasePageLayout(app, location.ID, location.WorkspaceID, encoded, plan.Revision+1, actor, now)
}

// applyMCPDataSourcePageLayout applies a page_layout for someone who may
// edit the database: the properties' visibility as one content graph change
// with the layout stored in it, or the layout alone when no visibility
// changes. It names the Notion modules it left out.
func applyMCPDataSourcePageLayout(
	ctx context.Context, app core.App, persistence *pocketBaseYgoPersistence, authority mcpAuthority,
	location mcpDataSourceLocation, input notionMCPPageLayout,
) ([]string, error) {
	authorizeEdit := func(txApp core.App) error {
		if _, err := authorizeDatabasePageLayout(txApp, authority.PrincipalID, location.PageID, location.ID, pageAccessRoleEdit); err != nil {
			return mcpRefusedUnlessStorage(errMCPCantChange, err)
		}
		return nil
	}
	if err := authorizeEdit(app); err != nil {
		return nil, err
	}
	plan, err := planMCPPageLayout(app, location, input)
	if err != nil {
		return nil, mcpPageLayoutError(err)
	}
	now := time.Now().UTC().UnixMilli()
	if len(plan.Operations) == 0 {
		err = app.RunInTransaction(func(txApp core.App) error {
			if err := authorizeEdit(txApp); err != nil {
				return err
			}
			if plan, err = planMCPPageLayout(txApp, location, input); err != nil {
				return err
			}
			return storeMCPPageLayout(txApp, location, plan, authority.PrincipalID, now)
		})
	} else {
		_, err = persistence.applyContentGraphMutationContext(
			ctx, authority.PrincipalID, location.PageID,
			graphMutationInput{MutationID: "mcp-page-layout:" + newDiagnosticReference()},
			contentGraphMutationHooks{
				Authorize: func(txApp core.App) (pageAuthorization, error) {
					authorized, err := authorizePageBody(txApp, authority.PrincipalID, location.PageID, permissionWorkspaceWrite)
					if err != nil {
						return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPCantChange, err)
					}
					return authorized, authorizeEdit(txApp)
				},
				Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
					plan, err = planMCPPageLayout(txApp, location, input)
					return contentGraphMutationPreparation{Operations: plan.Operations}, err
				},
				AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
					return storeMCPPageLayout(txApp, location, plan, authority.PrincipalID, now)
				},
			},
		)
	}
	if err != nil {
		return nil, mcpPageLayoutError(err)
	}
	warnings := []string{}
	for _, module := range plan.LeftOut {
		warnings = append(warnings, "page_layout's "+module+" module is one Leafen pages don't draw: it was left out")
	}
	return warnings, nil
}

// mcpPageLayoutError names what stopped a page_layout.
func mcpPageLayoutError(err error) error {
	if errors.Is(err, errDatabasePageLayoutInvalid) {
		return mcpInvalid("the page_layout doesn't read: each property in one place by its name (not the title), " +
			"relations and views modules on relation properties, and at most 15 pinned properties")
	}
	if errors.Is(err, errMCPCantChange) {
		return err
	}
	return mcpDatabaseCreateError(err)
}
