package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's sidebar tab presets, as Notion's settings "Sidebar" holds
// them (seen 2026-10-11 and 2026-11-09): tabs its owners set up for its
// members, each an icon and a name with the sections and the shortcuts a tab
// holds — pages and database views among them, as Notion's, whose editor
// says when not every member opens one. The sidebar is one list without tabs
// (the one-sidebar decision of 2026-10-29), so a "Default" preset's sections
// and shortcuts are in a new member's sidebar after its own, and an
// "Optional" one's a member may add while customizing it. The home tab is
// Leafen's own preset, always first. A person's sidebar takes the default
// presets until they keep a layout of their own.

const (
	workspaceSidebarPresetsPath = "/workspaces/{workspaceId}/sidebar-presets"
	sidebarPresetsLimit         = 10
	sidebarPresetTabPrefix      = "preset-"
)

type sidebarPresetWire struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// A built-in icon, "name:color"; none shows the preset's own.
	Icon      string                      `json:"icon,omitempty"`
	Optional  bool                        `json:"optional"`
	Sections  []sidebarLayoutSectionWire  `json:"sections"`
	Shortcuts []sidebarLayoutShortcutWire `json:"shortcuts,omitempty"`
	// Who made it, or copied it from another; the server keeps it.
	CreatedBy string `json:"createdBy"`
}

type sidebarPresetsResponse struct {
	Presets []sidebarPresetWire `json:"presets"`
	// Whether the reader may change them: the workspace's owners.
	CanEdit bool `json:"canEdit"`
	// The revision they are at (their row's updated_at, 0 while the
	// workspace keeps none); a save names the one its change was made on.
	Revision int64 `json:"revision"`
}

var errSidebarPresetsRevisionConflict = errors.New(errorCodeSidebarPresetsRevisionConflict)

func registerSidebarPresetRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+workspaceSidebarPresetsPath, readSidebarPresetsRoute)
	event.Router.PUT(apiRoot+workspaceSidebarPresetsPath, saveSidebarPresetsRoute).
		Bind(apis.BodyLimit(sidebarLayoutMaxBodyBytes))
}

func readSidebarPresetsRoute(event *core.RequestEvent) error {
	user, workspaceID, ok, err := sidebarLayoutRequest(event)
	if !ok {
		return err
	}
	presets, err := loadSidebarPresets(event.App, workspaceID)
	if err != nil {
		return sidebarLayoutFailure(event, err)
	}
	revision, err := sidebarPresetsRevision(event.App, workspaceID)
	if err != nil {
		return sidebarLayoutFailure(event, err)
	}
	_, ownerErr := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner)
	return event.JSON(http.StatusOK, sidebarPresetsResponse{Presets: presets, CanEdit: ownerErr == nil, Revision: revision})
}

func saveSidebarPresetsRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner); err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return sidebarLayoutFailure(event, err)
	}
	input := sidebarPresetsResponse{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil || !validSidebarPresets(input.Presets) {
		return apiError(event, http.StatusBadRequest, errorCodeSidebarLayoutInvalid, "Check the sidebar presets.")
	}
	var saved []sidebarPresetWire
	var revision int64
	err := event.App.RunInTransaction(func(txApp core.App) error {
		// They are replaced whole, so a save made on presets read before another
		// owner, settings window or agent saved is refused rather than undoing
		// that change unseen, as a sidebar layout's (keepSidebarLayout).
		base, err := sidebarPresetsRevision(txApp, workspaceID)
		if err != nil {
			return err
		}
		if base != input.Revision {
			return errSidebarPresetsRevisionConflict
		}
		revision = max(time.Now().UnixMilli(), base+1)
		current, err := loadSidebarPresets(txApp, workspaceID)
		if err != nil {
			return err
		}
		makers := map[string]string{}
		for _, preset := range current {
			makers[preset.ID] = preset.CreatedBy
		}
		saved = make([]sidebarPresetWire, 0, len(input.Presets))
		for _, preset := range input.Presets {
			preset.Name = strings.TrimSpace(preset.Name)
			preset.CreatedBy = makers[preset.ID]
			if preset.CreatedBy == "" {
				preset.CreatedBy = user.Id
			}
			if preset.Sections == nil {
				preset.Sections = []sidebarLayoutSectionWire{}
			}
			saved = append(saved, preset)
		}
		encoded, err := json.Marshal(saved)
		if err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_sidebar_presets}} ([[workspace_id]], [[presets]], [[updated_at]])
			VALUES ({:workspaceId}, {:presets}, {:now})
			ON CONFLICT ([[workspace_id]]) DO UPDATE SET
				[[presets]] = excluded.[[presets]], [[updated_at]] = excluded.[[updated_at]]
		`).Bind(dbx.Params{"workspaceId": workspaceID, "presets": string(encoded), "now": revision}).Execute()
		return err
	})
	if errors.Is(err, errSidebarPresetsRevisionConflict) {
		return apiError(event, http.StatusConflict, errorCodeSidebarPresetsRevisionConflict,
			"The sidebar presets changed since they were read (by another owner, settings window or agent). "+
				"Read them again (GET gives their revision), make the change on what they are now and send all of them with that revision.")
	}
	if err != nil {
		return sidebarLayoutFailure(event, err)
	}
	return event.JSON(http.StatusOK, sidebarPresetsResponse{Presets: saved, CanEdit: true, Revision: revision})
}

// sidebarPresetsRevision is the revision a workspace's presets are at: their
// row's updated_at, 0 while it keeps none.
func sidebarPresetsRevision(app core.App, workspaceID string) (int64, error) {
	var revision int64
	err := app.DB().NewQuery(`
		SELECT [[updated_at]] FROM {{leafen_workspace_sidebar_presets}} WHERE [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return revision, err
}

// loadSidebarPresets reads a workspace's presets in their order, none when it
// has none or what it keeps no longer reads as presets.
func loadSidebarPresets(app core.App, workspaceID string) ([]sidebarPresetWire, error) {
	stored := ""
	err := app.DB().NewQuery(`
		SELECT [[presets]] FROM {{leafen_workspace_sidebar_presets}} WHERE [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return []sidebarPresetWire{}, nil
	}
	if err != nil {
		return nil, err
	}
	presets := []sidebarPresetWire{}
	if json.Unmarshal([]byte(stored), &presets) != nil || !validSidebarPresets(presets) {
		return []sidebarPresetWire{}, nil
	}
	return presets, nil
}

// validSidebarPresets takes at most ten presets with their own ids, names of
// one to forty characters and a built-in icon or none, each holding what a
// tab may hold.
func validSidebarPresets(presets []sidebarPresetWire) bool {
	if len(presets) > sidebarPresetsLimit {
		return false
	}
	ids := map[string]bool{}
	for _, preset := range presets {
		name := strings.TrimSpace(preset.Name)
		if !sidebarTabIDPattern.MatchString(preset.ID) || ids[preset.ID] || name == "" || !validSidebarTabName(name) ||
			len(sidebarPresetTabPrefix+preset.ID) > 64 || !utf8.ValidString(preset.CreatedBy) ||
			len(preset.CreatedBy) > maxWorkspacePermissionIdentifierLength || preset.Icon != "" && !validNativeIconValue(preset.Icon) {
			return false
		}
		tab := sidebarLayoutTabWire{ID: "preset", Name: name, Sections: preset.Sections, Shortcuts: preset.Shortcuts}
		if !validSidebarLayout(sidebarLayoutWire{Tabs: []sidebarLayoutTabWire{tab}}) {
			return false
		}
		ids[preset.ID] = true
	}
	return true
}

// sidebarLayoutFromPresets is the layout a person starts with: the default
// one, with the sections and shortcuts of each of the workspace's default
// presets after its own, in their order (completeSidebarLayout puts a tab's
// there).
func sidebarLayoutFromPresets(presets []sidebarPresetWire) sidebarLayoutWire {
	layout := sidebarLayoutWire{Sections: append([]sidebarLayoutSectionWire{}, sidebarSectionDefaults...), Tabs: []sidebarLayoutTabWire{}}
	for _, preset := range presets {
		if preset.Optional || len(layout.Tabs) >= sidebarTabsLimit {
			continue
		}
		layout.Tabs = append(layout.Tabs, sidebarLayoutTabWire{
			ID: sidebarPresetTabPrefix + preset.ID, Name: preset.Name, Sections: preset.Sections, Shortcuts: preset.Shortcuts,
		})
	}
	return completeSidebarLayout(layout)
}
