package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A database's page layout, as Notion's "레이아웃 사용자 지정" (seen
// 2026-10-12, its help "Layouts"): one per data source, for every page in
// it. The heading pins up to 15 properties under the title; the page and the
// details panel hold modules in order — the one property group (with its
// sections and order), a property of its own, the page comments and the
// content; the structure is simple or tabbed (the content tab, with its
// own name, icon and description, and tabs of other data sources); and the options set inline comments, the page
// comments, property icons, full width and the templates. Anyone who may
// edit the database applies it; anyone who may read it reads it.

const (
	databasePageLayoutPath         = "/pages/{pageId}/data-sources/{dataSourceId}/page-layout"
	databasePageLayoutMaxBodyBytes = 512 << 10
	// A tab's view settings, as a database view's, its order apart.
	databasePageLayoutMaxViewBytes = 16 << 10
	databasePageLayoutMaxPinned    = 15
	databasePageLayoutMaxName      = 100
	// A content tab's description and icon, as a database view's.
	databasePageLayoutMaxDescription = 2000
	databasePageLayoutMaxIcon        = 256
	databasePageLayoutMaxID          = 64

	errorCodeDatabasePageLayoutInvalid  = "LEAFEN_DATABASE_PAGE_LAYOUT_INVALID"
	errorCodeDatabasePageLayoutConflict = "LEAFEN_DATABASE_PAGE_LAYOUT_CONFLICT"
	errorCodeDatabasePageLayoutNotFound = "LEAFEN_DATABASE_PAGE_LAYOUT_NOT_FOUND"
	errorCodeDatabasePageLayoutFailed   = "LEAFEN_DATABASE_PAGE_LAYOUT_FAILED"
)

var (
	errDatabasePageLayoutInvalid  = errors.New(errorCodeDatabasePageLayoutInvalid)
	errDatabasePageLayoutConflict = errors.New(errorCodeDatabasePageLayoutConflict)
	errDatabasePageLayoutNotFound = errors.New(errorCodeDatabasePageLayoutNotFound)
)

type databasePageLayout struct {
	Heading       databasePageLayoutHeading     `json:"heading"`
	Page          []databasePageLayoutModule    `json:"page"`
	Panel         []databasePageLayoutModule    `json:"panel"`
	PanelOpen     bool                          `json:"panelOpen"`
	PropertyGroup []databasePageLayoutGroupItem `json:"propertyGroup"`
	Structure     string                        `json:"structure"`
	Tabs          []databasePageLayoutTab       `json:"tabs"`
	ContentTab    *databasePageLayoutContentTab `json:"contentTab,omitempty"`
	Options       databasePageLayoutOptions     `json:"options"`
}

type databasePageLayoutHeading struct {
	Pinned []string `json:"pinned"`
	Labels bool     `json:"labels"`
	// Backlinks is "show", "hover" or "off"; empty follows each page's own.
	Backlinks string `json:"backlinks"`
}

type databasePageLayoutModule struct {
	ID string `json:"id"`
	// Kind is "properties" (the property group), "property", "relations"
	// (Notion's "관계형 그룹", seen 2026-11-06), "comments" or "content".
	Kind       string `json:"kind"`
	PropertyID string `json:"propertyId,omitempty"`
	// Relations are a relations group's relation properties in order, each
	// shown minimized (one line) or as a section of the page.
	Relations []databasePageLayoutRelation `json:"relations,omitempty"`
}

type databasePageLayoutRelation struct {
	PropertyID string `json:"propertyId"`
	Display    string `json:"display"`
}

// A property group lists sections and properties in order; a property the
// list leaves out (one made later) comes at its end.
type databasePageLayoutGroupItem struct {
	Kind       string `json:"kind"`
	ID         string `json:"id,omitempty"`
	Name       string `json:"name,omitempty"`
	PropertyID string `json:"propertyId,omitempty"`
}

// A tab shows a data source's pages: those the open page relates to through
// one of its relation properties, or all of them (Notion's "보기 추가"), in a
// view of their own — its layout and settings, as a database view's.
type databasePageLayoutTab struct {
	ID                 string                     `json:"id"`
	DataSourceID       string                     `json:"dataSourceId"`
	RelationPropertyID string                     `json:"relationPropertyId,omitempty"`
	Name               string                     `json:"name,omitempty"`
	View               *databasePageLayoutTabView `json:"view,omitempty"`
}

type databasePageLayoutTabView struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

// databasePageLayoutTabViewTypes are the layouts a tab shows its pages in;
// a form or a dashboard isn't a list of them.
var databasePageLayoutTabViewTypes = []string{
	graphViewTypeTable, graphViewTypeBoard, graphViewTypeList, graphViewTypeGallery, graphViewTypeCalendar,
	graphViewTypeTimeline, graphViewTypeChart, graphViewTypeMap, graphViewTypeFeed,
}

type databasePageLayoutOptions struct {
	InlineComments string `json:"inlineComments"`
	PageComments   bool   `json:"pageComments"`
	PropertyIcons  bool   `json:"propertyIcons"`
	FullWidth      bool   `json:"fullWidth"`
	Templates      bool   `json:"templates"`
}

type databasePageLayoutResponse struct {
	DataSourceID string              `json:"dataSourceId"`
	Revision     int                 `json:"revision"`
	Layout       *databasePageLayout `json:"layout"`
	UpdatedBy    string              `json:"updatedBy,omitempty"`
	UpdatedAt    int64               `json:"updatedAt,omitempty"`
}

type databasePageLayoutInput struct {
	ExpectedRevision *int            `json:"expectedRevision"`
	Layout           json.RawMessage `json:"layout"`
}

type storedDatabasePageLayout struct {
	Layout    string `db:"layout"`
	Revision  int    `db:"revision"`
	UpdatedBy string `db:"updated_by"`
	UpdatedAt int64  `db:"updated_at"`
}

func registerDatabasePageLayoutRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+databasePageLayoutPath, readDatabasePageLayout)
	event.Router.PUT(apiRoot+databasePageLayoutPath, func(request *core.RequestEvent) error {
		return writeDatabasePageLayout(request, false)
	}).Bind(apis.BodyLimit(databasePageLayoutMaxBodyBytes))
	event.Router.DELETE(apiRoot+databasePageLayoutPath, func(request *core.RequestEvent) error {
		return writeDatabasePageLayout(request, true)
	}).Bind(apis.BodyLimit(databasePageLayoutMaxBodyBytes))
}

func readDatabasePageLayout(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID, dataSourceID := databasePageLayoutTarget(event)
	var response databasePageLayoutResponse
	err := runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		if _, err := authorizeDatabasePageLayout(txApp, user.Id, pageID, dataSourceID, pageAccessRoleView); err != nil {
			return err
		}
		var err error
		response, err = loadDatabasePageLayout(txApp, dataSourceID)
		return err
	})
	if err != nil {
		return databasePageLayoutError(event, err, "read")
	}
	return event.JSON(http.StatusOK, response)
}

func writeDatabasePageLayout(event *core.RequestEvent, reset bool) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID, dataSourceID := databasePageLayoutTarget(event)
	input := databasePageLayoutInput{}
	if err := event.BindBody(&input); err != nil {
		return databasePageLayoutError(event, bodyRefusal(errDatabasePageLayoutInvalid, "page layout change", err), "input")
	}
	if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 {
		return databasePageLayoutError(event, refuse(errDatabasePageLayoutInvalid, "expectedRevision is the layout's revision as GET reads it, 0 or more"), "input")
	}
	if !reset && len(input.Layout) == 0 {
		return databasePageLayoutError(event, refuse(errDatabasePageLayoutInvalid, "layout is empty: send the whole layout, or DELETE to go back to the default"), "input")
	}
	var response databasePageLayoutResponse
	err := event.App.RunInTransaction(func(txApp core.App) error {
		workspaceID, err := authorizeDatabasePageLayout(txApp, user.Id, pageID, dataSourceID, pageAccessRoleEdit)
		if err != nil {
			return err
		}
		current, err := loadDatabasePageLayout(txApp, dataSourceID)
		if err != nil {
			return err
		}
		if current.Revision != *input.ExpectedRevision {
			return errDatabasePageLayoutConflict
		}
		now := time.Now().UTC().UnixMilli()
		if reset {
			if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_database_page_layouts}} WHERE [[data_source_id]] = {:dataSourceId}`).
				Bind(dbx.Params{"dataSourceId": dataSourceID}).Execute(); err != nil {
				return err
			}
			response = databasePageLayoutResponse{DataSourceID: dataSourceID}
			return nil
		}
		layout, err := parseDatabasePageLayout(txApp, input.Layout, pageID, dataSourceID, workspaceID)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(layout)
		if err != nil {
			return err
		}
		revision := current.Revision + 1
		if err := storeDatabasePageLayout(txApp, dataSourceID, workspaceID, encoded, revision, user.Id, now); err != nil {
			return err
		}
		response = databasePageLayoutResponse{
			DataSourceID: dataSourceID, Revision: revision, Layout: &layout, UpdatedBy: user.Id, UpdatedAt: now,
		}
		return nil
	})
	if err != nil {
		return databasePageLayoutError(event, err, "write")
	}
	return event.JSON(http.StatusOK, response)
}

func storeDatabasePageLayout(app core.App, dataSourceID, workspaceID string, encoded []byte, revision int, actor string, now int64) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_database_page_layouts}}
			([[data_source_id]],[[workspace_id]],[[layout]],[[revision]],[[updated_by]],[[updated_at]])
		VALUES ({:dataSourceId},{:workspaceId},{:layout},{:revision},{:actor},{:now})
		ON CONFLICT ([[data_source_id]]) DO UPDATE SET [[layout]]=excluded.[[layout]],
			[[revision]]=excluded.[[revision]],[[updated_by]]=excluded.[[updated_by]],[[updated_at]]=excluded.[[updated_at]]
	`).Bind(dbx.Params{
		"dataSourceId": dataSourceID, "workspaceId": workspaceID, "layout": string(encoded),
		"revision": revision, "actor": actor, "now": now,
	}).Execute()
	return err
}

func databasePageLayoutTarget(event *core.RequestEvent) (string, string) {
	return strings.TrimSpace(event.Request.PathValue("pageId")), strings.TrimSpace(event.Request.PathValue("dataSourceId"))
}

// authorizeDatabasePageLayout checks the person's role on the database's
// page and that the data source is that database's; it returns the
// workspace.
func authorizeDatabasePageLayout(app core.App, principalID, pageID, dataSourceID string, minimum pageAccessRole) (string, error) {
	if pageID == "" || dataSourceID == "" || len(pageID) > databasePageLayoutMaxID || len(dataSourceID) > databasePageLayoutMaxID {
		return "", errDatabasePageLayoutNotFound
	}
	authorization, err := authorizePageAccessRole(app, principalID, pageID, minimum)
	if err != nil {
		return "", err
	}
	var workspaceID string
	err = app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id} AND [[page_id]] = {:page}`).
		Bind(dbx.Params{"id": dataSourceID, "page": pageID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && workspaceID != authorization.Page.GetString("workspace") {
		return "", errDatabasePageLayoutNotFound
	}
	return workspaceID, err
}

func loadDatabasePageLayout(app core.App, dataSourceID string) (databasePageLayoutResponse, error) {
	stored := storedDatabasePageLayout{}
	err := app.DB().NewQuery(`SELECT [[layout]],[[revision]],[[updated_by]],[[updated_at]] FROM {{leafen_database_page_layouts}}
		WHERE [[data_source_id]] = {:dataSourceId}`).Bind(dbx.Params{"dataSourceId": dataSourceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		// A Sprints database starts from Notion's sprint layout (database_sprint_database.go).
		sprint, sprintErr := databaseSprintDefaultPageLayout(app, dataSourceID)
		return databasePageLayoutResponse{DataSourceID: dataSourceID, Layout: sprint}, sprintErr
	}
	if err != nil {
		return databasePageLayoutResponse{}, err
	}
	layout := databasePageLayout{}
	if err := json.Unmarshal([]byte(stored.Layout), &layout); err != nil {
		return databasePageLayoutResponse{}, err
	}
	return databasePageLayoutResponse{
		DataSourceID: dataSourceID, Revision: stored.Revision, Layout: &layout,
		UpdatedBy: stored.UpdatedBy, UpdatedAt: stored.UpdatedAt,
	}, nil
}

// parseDatabasePageLayout reads a layout strictly and checks it against the
// data source's properties: the one property group, the one content module
// on the page, each property placed once, the title nowhere but the heading.
// A refusal names the field (layout.page[1].propertyId …) and why.
func parseDatabasePageLayout(app core.App, raw json.RawMessage, pageID, dataSourceID, workspaceID string) (databasePageLayout, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	layout := databasePageLayout{}
	if err := decoder.Decode(&layout); err != nil {
		return layout, refuse(errDatabasePageLayoutInvalid, "%s", databasePageLayoutDecodeReason(err))
	}
	rows := []struct {
		ID   string `db:"id"`
		Type string `db:"type"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[type]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:source} AND [[page_id]] = {:page} AND ` + databasePropertyNotTrashedSQL("[[id]]")).
		Bind(dbx.Params{"source": dataSourceID, "page": pageID}).All(&rows); err != nil {
		return layout, err
	}
	properties := make(map[string]string, len(rows))
	for _, row := range rows {
		properties[row.ID] = row.Type
	}
	placed := map[string]string{}
	// place is why a property can't stand at field, or "".
	place := func(field, id string) string {
		kind, found := properties[id]
		switch {
		case !found:
			return fmt.Sprintf("%s names no property of the data source", field)
		case kind == "title":
			return fmt.Sprintf("%s is the title, which stays in the heading", field)
		case placed[id] != "":
			return fmt.Sprintf("%s is placed at %s already: each property stands in one place", field, placed[id])
		}
		placed[id] = field
		return ""
	}
	heading := layout.Heading
	if len(heading.Pinned) > databasePageLayoutMaxPinned {
		return layout, databasePageLayoutRefused("heading.pinned holds %d properties, over %d", len(heading.Pinned), databasePageLayoutMaxPinned)
	}
	if !oneOf(heading.Backlinks, "", "show", "hover", "off") {
		return layout, databasePageLayoutRefused("heading.backlinks is show, hover or off, not %q", heading.Backlinks)
	}
	for index, id := range heading.Pinned {
		if reason := place(fmt.Sprintf("heading.pinned[%d]", index), id); reason != "" {
			return layout, databasePageLayoutRefused("%s", reason)
		}
	}
	counts := map[string]int{}
	moduleIDs := map[string]bool{}
	for index, module := range append(append([]databasePageLayoutModule{}, layout.Page...), layout.Panel...) {
		inPanel := index >= len(layout.Page)
		at := fmt.Sprintf("page[%d]", index)
		if inPanel {
			at = fmt.Sprintf("panel[%d]", index-len(layout.Page))
		}
		if !layoutID(module.ID) {
			return layout, databasePageLayoutRefused("%s.id is 1–%d characters", at, databasePageLayoutMaxID)
		}
		if moduleIDs[module.ID] {
			return layout, databasePageLayoutRefused("%s.id is another module's too", at)
		}
		moduleIDs[module.ID] = true
		counts[module.Kind]++
		switch module.Kind {
		case "properties", "comments":
			if module.PropertyID != "" || len(module.Relations) > 0 {
				return layout, databasePageLayoutRefused("%s: propertyId and relations don't go with kind %s", at, module.Kind)
			}
		case "relations":
			if module.PropertyID != "" || len(module.Relations) == 0 {
				return layout, databasePageLayoutRefused("%s.relations names the relation properties it shows, and no propertyId", at)
			}
			for relationIndex, relation := range module.Relations {
				field := fmt.Sprintf("%s.relations[%d]", at, relationIndex)
				if properties[relation.PropertyID] != graphValueTypeRelation {
					return layout, databasePageLayoutRefused("%s.propertyId names no relation property of the data source", field)
				}
				if !oneOf(relation.Display, "minimized", "section") {
					return layout, databasePageLayoutRefused("%s.display is minimized or section, not %q", field, relation.Display)
				}
				if reason := place(field+".propertyId", relation.PropertyID); reason != "" {
					return layout, databasePageLayoutRefused("%s", reason)
				}
			}
		case "content":
			if module.PropertyID != "" {
				return layout, databasePageLayoutRefused("%s: propertyId doesn't go with kind content", at)
			}
			if inPanel {
				return layout, databasePageLayoutRefused("%s: the content module stands on the page, not in the panel", at)
			}
		case "property":
			if reason := place(at+".propertyId", module.PropertyID); reason != "" {
				return layout, databasePageLayoutRefused("%s", reason)
			}
		default:
			return layout, databasePageLayoutRefused("%s.kind is properties, comments, relations, content or property, not %q", at, module.Kind)
		}
	}
	switch {
	case counts["properties"] != 1:
		return layout, databasePageLayoutRefused("page and panel hold one properties module, not %d", counts["properties"])
	case counts["content"] != 1:
		return layout, databasePageLayoutRefused("page holds one content module, not %d", counts["content"])
	case counts["comments"] > 1 || counts["relations"] > 1:
		return layout, databasePageLayoutRefused("page and panel hold at most one comments and one relations module")
	}
	sections := map[string]bool{}
	for index, item := range layout.PropertyGroup {
		at := fmt.Sprintf("propertyGroup[%d]", index)
		switch item.Kind {
		case "section":
			switch {
			case !layoutID(item.ID) || sections[item.ID]:
				return layout, databasePageLayoutRefused("%s.id is a section's own id, 1–%d characters", at, databasePageLayoutMaxID)
			case item.PropertyID != "":
				return layout, databasePageLayoutRefused("%s: propertyId doesn't go with kind section", at)
			case utf8.RuneCountInString(item.Name) > databasePageLayoutMaxName:
				return layout, databasePageLayoutRefused("%s.name is at most %d characters", at, databasePageLayoutMaxName)
			}
			sections[item.ID] = true
		case "property":
			if item.ID != "" || item.Name != "" {
				return layout, databasePageLayoutRefused("%s: id and name go only with kind section", at)
			}
			if reason := place(at+".propertyId", item.PropertyID); reason != "" {
				return layout, databasePageLayoutRefused("%s", reason)
			}
		default:
			return layout, databasePageLayoutRefused("%s.kind is section or property, not %q", at, item.Kind)
		}
	}
	if !oneOf(layout.Structure, "simple", "tabbed") {
		return layout, databasePageLayoutRefused("structure is simple or tabbed, not %q", layout.Structure)
	}
	if layout.Structure == "simple" && len(layout.Tabs) > 0 {
		return layout, databasePageLayoutRefused("tabs go only with structure tabbed")
	}
	tabs := map[string]bool{}
	for index, tab := range layout.Tabs {
		at := fmt.Sprintf("tabs[%d]", index)
		switch {
		case !layoutID(tab.ID) || tabs[tab.ID]:
			return layout, databasePageLayoutRefused("%s.id is a tab's own id, 1–%d characters", at, databasePageLayoutMaxID)
		case tab.DataSourceID == "" || len(tab.DataSourceID) > databasePageLayoutMaxID:
			return layout, databasePageLayoutRefused("%s.dataSourceId names the data source the tab shows, 1–%d characters", at, databasePageLayoutMaxID)
		case len(tab.RelationPropertyID) > databasePageLayoutMaxID:
			return layout, databasePageLayoutRefused("%s.relationPropertyId is at most %d characters", at, databasePageLayoutMaxID)
		case utf8.RuneCountInString(tab.Name) > databasePageLayoutMaxName:
			return layout, databasePageLayoutRefused("%s.name is at most %d characters", at, databasePageLayoutMaxName)
		}
		tabs[tab.ID] = true
		if tab.View != nil {
			if !oneOf(tab.View.Type, databasePageLayoutTabViewTypes...) {
				return layout, databasePageLayoutRefused("%s.view.type is %s, not %q", at, strings.Join(databasePageLayoutTabViewTypes, ", "), tab.View.Type)
			}
			if databasePageLayoutViewSettingsBytes(tab.View.Config) > databasePageLayoutMaxViewBytes {
				return layout, databasePageLayoutRefused("%s.view.config is over %d bytes, its rowOrder apart", at, databasePageLayoutMaxViewBytes)
			}
			config, err := normalizeJSONObject(tab.View.Config)
			if err != nil {
				return layout, databasePageLayoutRefused("%s.view.config is an object", at)
			}
			tab.View.Config = config
		}
		var found int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_data_sources}} WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspace}`).
			Bind(dbx.Params{"id": tab.DataSourceID, "workspace": workspaceID}).Row(&found); err != nil {
			return layout, err
		}
		if found != 1 {
			return layout, databasePageLayoutRefused("%s.dataSourceId names no data source of the workspace", at)
		}
		// A relation tab's property is this data source's relation to the tab's.
		if tab.RelationPropertyID != "" {
			if properties[tab.RelationPropertyID] != "relation" {
				return layout, databasePageLayoutRefused("%s.relationPropertyId names no relation property of the data source", at)
			}
			var related int
			if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_relation_property_targets}}
				WHERE [[page_id]] = {:page} AND [[property_id]] = {:property} AND [[source_id]] = {:source} AND [[target_source_id]] = {:target}`).
				Bind(dbx.Params{"page": pageID, "property": tab.RelationPropertyID, "source": dataSourceID, "target": tab.DataSourceID}).
				Row(&related); err != nil {
				return layout, err
			}
			if related != 1 {
				return layout, databasePageLayoutRefused("%s.relationPropertyId relates to another data source than dataSourceId", at)
			}
		}
	}
	if err := layout.ContentTab.kept(len(layout.Tabs)); err != nil {
		return layout, err
	}
	if !oneOf(layout.Options.InlineComments, "default", "minimal") {
		return layout, databasePageLayoutRefused("options.inlineComments is default or minimal, not %q", layout.Options.InlineComments)
	}
	if layout.Heading.Pinned == nil {
		layout.Heading.Pinned = []string{}
	}
	if layout.Panel == nil {
		layout.Panel = []databasePageLayoutModule{}
	}
	if layout.PropertyGroup == nil {
		layout.PropertyGroup = []databasePageLayoutGroupItem{}
	}
	if layout.Tabs == nil {
		layout.Tabs = []databasePageLayoutTab{}
	}
	return layout, nil
}

// databasePageLayoutDecodeReason is why a layout doesn't read: a field it
// doesn't have, a value of the wrong kind, or no JSON object.
func databasePageLayoutDecodeReason(err error) string {
	if field, unknown := strings.CutPrefix(err.Error(), "json: unknown field "); unknown {
		return "layout has no field " + field
	}
	var wrongKind *json.UnmarshalTypeError
	if errors.As(err, &wrongKind) && wrongKind.Field != "" {
		return "layout." + bodyRefusalReason("page layout", err)
	}
	return "layout is the page layout as an object, as GET reads it"
}

// databasePageLayoutRefused refuses a page layout for the reason given,
// the field named under layout.
func databasePageLayoutRefused(format string, args ...any) error {
	return refuse(errDatabasePageLayoutInvalid, "layout."+format, args...)
}

func layoutID(id string) bool {
	return id != "" && len(id) <= databasePageLayoutMaxID
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func databasePageLayoutError(event *core.RequestEvent, err error, stage string) error {
	switch {
	case isPermissionDenied(err) || errors.Is(err, errDatabasePageLayoutNotFound):
		return apiError(event, http.StatusNotFound, errorCodeDatabasePageLayoutNotFound, "Database not found.")
	case errors.Is(err, errDatabasePageLayoutInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeDatabasePageLayoutInvalid, refusalMessage("Check the page layout.", refusalReason(err)))
	case errors.Is(err, errDatabasePageLayoutConflict):
		return apiError(event, http.StatusConflict, errorCodeDatabasePageLayoutConflict, "The page layout changed. Load it again.")
	default:
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeDatabasePageLayoutFailed,
			"Couldn't change the page layout.", "database_page_layout", stage, errorCodeDatabasePageLayoutFailed, err)
	}
}
