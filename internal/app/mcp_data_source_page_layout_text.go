package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// mcpPageLayoutTabID names a tabbed layout's content tab, as Notion's fetch
// names its tab (view_tab_…); page_layout takes any back.
const mcpPageLayoutTabID = "view_tab_content"

// mcpPageLayoutText is a data source's page layout as Notion's fetch gives it
// (seen 2026-11-13): a <page-layout> of the JSON page_layout takes, none
// while its pages keep the original layout with every property shown.
// Properties the reader doesn't see aren't named.
func mcpPageLayoutText(app core.App, location mcpDataSourceLocation, properties []notionMCPDataSourceProperty) (string, error) {
	response, err := loadDatabasePageLayout(app, location.ID)
	if err != nil {
		return "", err
	}
	layout := defaultDatabasePageLayout()
	if response.Layout != nil {
		layout = *response.Layout
	}
	names, visibility, customized := map[string]string{}, map[string]string{}, response.Layout != nil
	for _, property := range properties {
		if normalizeNotionDatabaseRowPagePropertyType(property.Type) == graphValueTypeTitle {
			continue
		}
		names[property.ID] = property.Name
		visibility[property.ID] = mcpPropertyVisibility(property.Config)
		customized = customized || visibility[property.ID] != "show"
	}
	if !customized {
		return "", nil
	}
	title := notionMCPPageLayoutModule{Type: "title"}
	for _, id := range layout.Heading.Pinned {
		if name, found := names[id]; found {
			title.PinnedProperties = append(title.PinnedProperties, name)
		}
	}
	if !layout.Heading.Labels {
		title.PropertyLabels = "hide"
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
	modules := func(from []databasePageLayoutModule) []notionMCPPageLayoutModule {
		shown := []notionMCPPageLayoutModule{}
		for _, module := range from {
			switch module.Kind {
			case "properties":
				shown = append(shown, mcpPageLayoutGroupModule(layout.PropertyGroup, properties, names, visibility, placed))
			case "property":
				if name, found := names[module.PropertyID]; found {
					shown = append(shown, notionMCPPageLayoutModule{Type: "property", Property: name})
				}
			case "relations":
				group := notionMCPPageLayoutModule{Type: "relations", Relations: []notionMCPPageLayoutRelation{}}
				for _, relation := range module.Relations {
					if name, found := names[relation.PropertyID]; found {
						group.Relations = append(group.Relations, notionMCPPageLayoutRelation{Property: name, Display: relation.Display})
					}
				}
				shown = append(shown, group)
			case "comments":
				shown = append(shown, notionMCPPageLayoutModule{Type: "discussions"})
			case "content":
				shown = append(shown, notionMCPPageLayoutModule{Type: "editor"})
			}
		}
		return shown
	}
	page := modules(layout.Page)
	shown := notionMCPPageLayout{Main: []notionMCPPageLayoutModule{{Type: "cover"}, title}}
	if layout.Structure == "tabbed" {
		// The content and what follows it are the content tab's.
		split := len(page)
		for index, module := range page {
			if module.Type == "editor" {
				split = index
				break
			}
		}
		shown.Main = append(shown.Main, page[:split]...)
		shown.Tab = &notionMCPPageLayoutTab{ID: mcpPageLayoutTabID, Modules: page[split:]}
	} else {
		shown.Main = append(shown.Main, page...)
	}
	if sidebar := modules(layout.Panel); len(sidebar) > 0 {
		shown.Sidebar = sidebar
	}
	icons, fullWidth := "show", layout.Options.FullWidth
	if !layout.Options.PropertyIcons {
		icons = "hide"
	}
	shown.Format = &notionMCPPageLayoutFormat{PropertyIcons: icons, PageFullWidth: &fullWidth}
	encoded, err := mcpFetchJSON(shown)
	if err != nil {
		return "", err
	}
	return "<page-layout>\n" + encoded + "\n</page-layout>", nil
}

// mcpPageLayoutGroupModule is the property group as Notion's properties
// module names it: its properties in order, each with a visibility other
// than show; in sections when it has them, those before the first in it.
func mcpPageLayoutGroupModule(
	group []databasePageLayoutGroupItem, properties []notionMCPDataSourceProperty,
	names, visibility map[string]string, placed map[string]bool,
) notionMCPPageLayoutModule {
	entry := func(id string) notionMCPPageLayoutEntry {
		shown := notionMCPPageLayoutEntry{Property: names[id]}
		if visibility[id] != "show" {
			shown.Visibility = visibility[id]
		}
		return shown
	}
	sections := []notionMCPPageLayoutSection{}
	listed := []notionMCPPageLayoutEntry{}
	seen := map[string]bool{}
	for _, item := range group {
		switch {
		case item.Kind == "section":
			if len(sections) > 0 || len(listed) == 0 {
				sections = append(sections, notionMCPPageLayoutSection{Name: item.Name, Properties: []notionMCPPageLayoutEntry{}})
			} else {
				sections = append(sections, notionMCPPageLayoutSection{Name: item.Name, Properties: listed})
				listed = nil
			}
		case names[item.PropertyID] != "" && !placed[item.PropertyID] && !seen[item.PropertyID]:
			seen[item.PropertyID] = true
			if len(sections) > 0 {
				last := &sections[len(sections)-1]
				last.Properties = append(last.Properties, entry(item.PropertyID))
			} else {
				listed = append(listed, entry(item.PropertyID))
			}
		}
	}
	for _, property := range properties {
		if names[property.ID] == "" || placed[property.ID] || seen[property.ID] {
			continue
		}
		if len(sections) > 0 {
			last := &sections[len(sections)-1]
			last.Properties = append(last.Properties, entry(property.ID))
		} else {
			listed = append(listed, entry(property.ID))
		}
	}
	if len(sections) > 0 {
		return notionMCPPageLayoutModule{Type: "properties", Sections: sections}
	}
	return notionMCPPageLayoutModule{Type: "properties", Properties: listed}
}
