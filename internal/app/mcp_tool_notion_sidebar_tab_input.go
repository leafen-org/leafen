package app

import (
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// How update-sidebar-tabs reads the tab it is sent into Leafen's sidebar
// list (mcp_tool_notion_sidebar_tabs.go): each section and shortcut by the
// id get-sidebar-tabs gave it, or as a new one from the catalogs, checked as
// the screen's sidebar checks what it adds — a view, page, database or
// template the person can open in that workspace, a teamspace they are in —
// each refused with why.

// mcpSidebarReader reads a Notion tab's items into a layout's.
type mcpSidebarReader struct {
	app         core.App
	authority   mcpAuthority
	workspaceID string
	current     sidebarLayoutWire
	warnings    []string
}

func notionMCPSidebarAbsentItem(id string) error {
	return mcpInvalid("Leafen's sidebar has no %s: Leafen holds no agents, AI chats, meeting notes, calendar, skills or marketplace; leave it out", id)
}

// item reads a shortcut or section given as a string or an object.
func (reader *mcpSidebarReader) item(raw any) (string, map[string]any, error) {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value), nil, nil
	case map[string]any:
		kind, _ := value["type"].(string)
		return strings.TrimSpace(kind), value, nil
	}
	return "", nil, mcpInvalid("each shortcut and section is an id or an object from the catalog")
}

func (reader *mcpSidebarReader) sections(given []any) ([]sidebarLayoutSectionWire, error) {
	existing := map[string]sidebarLayoutSectionWire{}
	for _, section := range reader.current.Sections {
		existing[sidebarLayoutSectionKey(section)] = section
		if section.View != nil {
			existing[section.View.Key] = section
		}
	}
	builtIn := func(id string) (sidebarLayoutSectionWire, bool) {
		for _, section := range sidebarSectionDefaults {
			if section.ID == id {
				if kept, found := existing[id]; found {
					section.Sort, section.Limit = kept.Sort, kept.Limit
				}
				section.Hidden = false
				return section, true
			}
		}
		return sidebarLayoutSectionWire{}, false
	}
	sections, seen, views := []sidebarLayoutSectionWire{}, map[string]bool{}, 0
	for _, raw := range given {
		kind, object, err := reader.item(raw)
		if err != nil {
			return nil, err
		}
		if slices.Contains(notionMCPSidebarAbsent, kind) {
			return nil, notionMCPSidebarAbsentItem(kind)
		}
		var section sidebarLayoutSectionWire
		switch {
		case kind == notionMCPViewSection && object != nil:
			if section, err = reader.viewSection(object, existing); err != nil {
				return nil, err
			}
			views++
		case kind == "teamspace" && object != nil:
			if section, err = reader.teamspaceSection(object); err != nil {
				return nil, err
			}
		default:
			found := false
			if section, found = builtIn(kind); !found {
				if kept, ok := existing[kind]; ok && kept.View != nil && object == nil {
					section, found = kept, true
					views++
				}
			}
			if !found {
				return nil, mcpInvalid("%q isn't a section Leafen's sidebar has: use one from sectionCatalog", kind)
			}
			if object != nil {
				if limit, ok := object["itemLimit"].(float64); ok {
					section.Limit = int(limit)
				}
			}
		}
		section.Hidden = false
		if !sidebarSectionLimits[section.Limit] {
			return nil, mcpInvalid("a section shows 5, 10, 15, 20, 50 or 100 items: itemLimit is one of them")
		}
		key := sidebarLayoutSectionKey(section)
		if seen[key] {
			return nil, mcpInvalid("the list holds each section once: %s is there twice", key)
		}
		seen[key] = true
		sections = append(sections, section)
	}
	if views > sidebarViewsPerTab {
		return nil, mcpInvalid("the sidebar holds at most %d database view sections", sidebarViewsPerTab)
	}
	// A built-in section left out is taken off the sidebar, keeping its sort
	// and count for when it is put back.
	for _, section := range sidebarSectionDefaults {
		if !seen[section.ID] {
			if kept, found := existing[section.ID]; found {
				section.Sort, section.Limit = kept.Sort, kept.Limit
			}
			section.Hidden = true
			sections = append(sections, section)
		}
	}
	return sections, nil
}

// viewSection reads a database view section: an existing one by its id, its
// view by a view:// address or an address with v=.
func (reader *mcpSidebarReader) viewSection(object map[string]any, existing map[string]sidebarLayoutSectionWire) (sidebarLayoutSectionWire, error) {
	address, _ := object["viewUrl"].(string)
	viewID, ok := parseMCPViewIdentifier(address)
	if parsed, err := url.Parse(strings.TrimSpace(address)); err == nil && parsed.Query().Get("v") != "" {
		viewID, ok = parsed.Query().Get("v"), true
	}
	if !ok {
		return sidebarLayoutSectionWire{}, mcpInvalid("a database view section needs viewUrl: a view:// address or an address with v=")
	}
	location, err := loadMCPViewLocation(reader.app, viewID)
	if err != nil || location.WorkspaceID != reader.workspaceID || !reader.readable(location.PageID) {
		return sidebarLayoutSectionWire{}, mcpDenied("the view %s isn't one you can open in this workspace", address)
	}
	section := sidebarLayoutSectionWire{ID: sidebarSectionView, Sort: sidebarSectionSortManual, Limit: sidebarSectionDefaultLimit}
	view := sidebarLayoutViewWire{Key: "view-" + security.RandomString(16), ShowIcons: true}
	if id, _ := object["id"].(string); id != "" {
		kept, found := existing[id]
		if !found || kept.View == nil {
			return sidebarLayoutSectionWire{}, mcpInvalid("no database view section has the id %q: omit id for a new one", id)
		}
		section.Sort, view.Key = kept.Sort, kept.View.Key
	}
	view.PageID, view.DataSourceID, view.ViewID = location.PageID, location.DataSourceID, location.ID
	if limit, ok := object["itemLimit"].(float64); ok {
		section.Limit = int(limit)
	}
	view.Name, _ = object["customName"].(string)
	if hide, ok := object["hidePageIcon"].(bool); ok {
		view.ShowIcons = !hide
	}
	if view.OpenIn, err = notionMCPReadOpenMode(object); err != nil {
		return sidebarLayoutSectionWire{}, err
	}
	if view.TemplateID, err = reader.template(object); err != nil {
		return sidebarLayoutSectionWire{}, err
	}
	if !validSidebarLayoutView(view) {
		return sidebarLayoutSectionWire{}, mcpInvalid("customName is at most %d characters", sidebarViewNameRunes)
	}
	section.View = &view
	return section, nil
}

func (reader *mcpSidebarReader) teamspaceSection(object map[string]any) (sidebarLayoutSectionWire, error) {
	address, _ := object["teamspaceUrl"].(string)
	id := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(address), notionMCPTeamspaceURL))
	owner, found, err := mcpTeamspaceOwner(reader.app, id)
	if err != nil {
		return sidebarLayoutSectionWire{}, failedBecause(errMCPToolFailed, err)
	}
	if id == "" || !found || owner != reader.workspaceID {
		return sidebarLayoutSectionWire{}, mcpDenied("%q isn't a teamspace of this workspace: notion-get-teams lists them", address)
	}
	if _, _, _, err := authorizeTeamspace(reader.app, reader.authority.PrincipalID, owner, id, false); err != nil {
		return sidebarLayoutSectionWire{}, mcpDenied("the person isn't in the teamspace %s: join it first", address)
	}
	return sidebarLayoutSectionWire{ID: sidebarSectionTeamspace, TeamspaceID: id, Sort: sidebarSectionSortManual, Limit: sidebarSectionDefaultLimit}, nil
}

func (reader *mcpSidebarReader) shortcuts(given []any) ([]sidebarLayoutShortcutWire, error) {
	existing := map[string]sidebarLayoutShortcutWire{}
	for _, shortcut := range reader.current.Shortcuts {
		existing[shortcut.Key] = shortcut
	}
	kinds := map[string]string{"new_page": sidebarShortcutNewPage}
	for kind, name := range notionMCPShortcutKinds {
		kinds[name] = kind
	}
	shortcuts, seen := []sidebarLayoutShortcutWire{}, map[string]bool{}
	for _, raw := range given {
		kind, object, err := reader.item(raw)
		if err != nil {
			return nil, err
		}
		if slices.Contains(notionMCPSidebarAbsent, kind) {
			return nil, notionMCPSidebarAbsentItem(kind)
		}
		shortcut := sidebarLayoutShortcutWire{Key: "shortcut-" + security.RandomString(16)}
		id := kind
		if object != nil {
			id, _ = object["id"].(string)
		}
		if kept, found := existing[id]; found && id != "" {
			shortcut = kept
			if object == nil {
				shortcuts = append(shortcuts, kept)
				seen[kept.Key] = true
				continue
			}
		} else if object != nil && id != "" {
			return nil, mcpInvalid("no shortcut has the id %q: omit id for a new one", id)
		}
		switch {
		case object == nil && kinds[kind] != "":
			shortcut.Kind = kinds[kind]
		case object == nil:
			return nil, mcpInvalid("%q isn't a shortcut Leafen's sidebar has: use one from shortcutCatalog, or an id get-sidebar-tabs gave", kind)
		default:
			if err := reader.shortcutObject(&shortcut, kind, object); err != nil {
				return nil, err
			}
		}
		if seen[shortcut.Key] {
			return nil, mcpInvalid("the list holds each shortcut once: %s is there twice", shortcut.Key)
		}
		seen[shortcut.Key] = true
		shortcuts = append(shortcuts, shortcut)
	}
	if len(shortcuts) > sidebarShortcutsPerTab {
		return nil, mcpInvalid("the sidebar holds at most %d shortcuts", sidebarShortcutsPerTab)
	}
	return shortcuts, nil
}

// shortcutObject reads a shortcut's object form onto it.
func (reader *mcpSidebarReader) shortcutObject(shortcut *sidebarLayoutShortcutWire, kind string, object map[string]any) error {
	label, _ := object["label"].(string)
	if utf8.RuneCountInString(label) > sidebarViewNameRunes {
		return mcpInvalid("a shortcut's label is at most %d characters", sidebarViewNameRunes)
	}
	shortcut.Name = label
	openIn, err := notionMCPReadOpenMode(object)
	if err != nil {
		return err
	}
	if icon, _ := object["icon"].(string); icon != "" {
		reader.warnings = append(reader.warnings, "Leafen's shortcuts show no icon of their own, so the icon was left out")
	}
	pageURL, _ := object["pageUrl"].(string)
	source, _ := object["dataSourceUrl"].(string)
	shortcut.PageID, shortcut.DataSourceID, shortcut.ViewID, shortcut.TemplateID, shortcut.OpenIn = "", "", "", "", openIn
	switch kind {
	case "new_page":
		if source != "" {
			return reader.newRowShortcut(shortcut, source, object)
		}
		shortcut.Kind = sidebarShortcutNewPage
		if pageURL != "" {
			if shortcut.PageID = mcpLocalID(reader.app, pageURL); !reader.readable(shortcut.PageID) {
				return mcpDenied("%s isn't a page you can open in this workspace", pageURL)
			}
		}
	case "page_link":
		shortcut.Kind = sidebarShortcutPage
		if shortcut.PageID = mcpLocalID(reader.app, pageURL); pageURL == "" || !reader.readable(shortcut.PageID) {
			return mcpDenied("pageUrl %q isn't a page you can open in this workspace", pageURL)
		}
		if viewID, _ := object["viewId"].(string); viewID != "" {
			reader.warnings = append(reader.warnings, "Leafen's page shortcut opens the database's last view, so viewId was left out")
		}
	case "app":
		if shortcut.Kind != sidebarShortcutApp {
			return mcpInvalid("an app shortcut is kept by its id from get-sidebar-tabs")
		}
		if app, _ := object["app"].(string); strings.TrimSpace(app) != "" {
			shortcut.PageID = strings.TrimSpace(app)
		} else {
			shortcut.PageID = reader.current.Shortcuts[slices.IndexFunc(reader.current.Shortcuts, func(kept sidebarLayoutShortcutWire) bool {
				return kept.Key == shortcut.Key
			})].PageID
		}
	default:
		known := ""
		for leafenKind, name := range notionMCPShortcutKinds {
			if name == kind {
				known = leafenKind
			}
		}
		if known == "" {
			return mcpInvalid("%q isn't a shortcut Leafen's sidebar has: use one from shortcutCatalog", kind)
		}
		if panel, _ := object["panel"].(string); panel != "" {
			reader.warnings = append(reader.warnings, "Leafen's settings shortcut opens the settings, not one panel, so panel was left out")
		}
		shortcut.Kind, shortcut.OpenIn = known, ""
	}
	return nil
}

// newRowShortcut reads a new page shortcut into a database: its data source,
// the view new pages go in (its first, when none is given) and its template.
func (reader *mcpSidebarReader) newRowShortcut(shortcut *sidebarLayoutShortcutWire, source string, object map[string]any) error {
	dataSourceID, ok := parseMCPDataSourceURL(mcpLocalID(reader.app, source))
	if !ok {
		dataSourceID = mcpLocalID(reader.app, source)
	}
	location, err := loadMCPDataSourceLocation(reader.app, dataSourceID)
	if err != nil || location.WorkspaceID != reader.workspaceID || !reader.readable(location.PageID) {
		return mcpDenied("%s isn't a database you can open in this workspace", source)
	}
	viewID, _ := object["viewId"].(string)
	if viewID == "" {
		_ = reader.app.DB().NewQuery(`
			SELECT [[id]] FROM {{leafen_database_views}} WHERE [[data_source_id]] = {:source}
			ORDER BY [[position]], [[id]] LIMIT 1
		`).Bind(map[string]any{"source": location.ID}).Row(&viewID)
	}
	view, err := loadMCPViewLocation(reader.app, viewID)
	if err != nil || view.DataSourceID != location.ID {
		return mcpInvalid("viewId isn't a view of %s", source)
	}
	shortcut.Kind, shortcut.PageID, shortcut.DataSourceID, shortcut.ViewID = sidebarShortcutNewDatabasePage, view.PageID, location.ID, view.ID
	shortcut.TemplateID, err = reader.template(object)
	return err
}

// template reads templateUrl: a template page's address, or "empty" for none.
func (reader *mcpSidebarReader) template(object map[string]any) (string, error) {
	address, _ := object["templateUrl"].(string)
	if address = strings.TrimSpace(address); address == "" || address == "empty" {
		return "", nil
	}
	id := mcpLocalID(reader.app, address)
	page, err := reader.app.FindRecordById(pagesCollection, id)
	if err != nil || !isDatabaseTemplatePageRecord(page) || page.GetString("workspace") != reader.workspaceID {
		return "", mcpInvalid("templateUrl %q isn't a database template in this workspace", address)
	}
	return id, nil
}

func (reader *mcpSidebarReader) readable(pageID string) bool {
	_, _, open := mcpListedPage(reader.app, reader.authority, pageID)
	return open
}

func notionMCPReadOpenMode(object map[string]any) (string, error) {
	mode, _ := object["openIn"].(string)
	if mode == "" {
		return "", nil
	}
	for leafen, notion := range notionMCPOpenModes {
		if notion == mode {
			return leafen, nil
		}
	}
	return "", mcpInvalid("openIn is side_peek, center_peek or full_page")
}
