package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A person's sidebar in a workspace, one list without tabs (the one-sidebar
// decision of 2026-10-29): Notion's sections as its 2026-09 redesign has
// them (seen 2026-09-29 and 2026-10-01): the order of its sections and, for each,
// how its pages sort (by hand, or by when they were last edited, which
// Notion starts with), how many it shows (5 to 100, 10 at first) and whether
// it was taken off the sidebar ("섹션 제거"; "최근 항목" starts off it until
// "섹션 추가" puts it on), and the tabs a person adds beside the home tab
// (Notion's "탭 추가", seen 2026-10-01), each with a name it may hide and the
// sections added or moved to it ("섹션 추가", "탭으로 이동"); a tab holds a
// section once. A tab may also hold up to ten database views as sections
// ("데이터베이스 보기", seen 2026-10-01): each names its database's page, data
// source and view, with a name of its own, whether its rows show their
// icons, where they open ("다음에서 열기", beside the page at first) and the
// template a new row starts from (none at first; seen 2026-10-02). A
// teamspace may be a section of its own too ("섹션 추가 › 팀스페이스 ›", seen
// 2026-11-11), which the teamspaces section then leaves out. Above its
// sections a tab keeps up to ten shortcuts ("단축키 추가"): a new page in a
// page (or at the top of the private pages), a page, a new database, the
// settings, the library or the reader's tasks, each with a name of its own,
// and a page or new page where it opens (a full page at first). The layout
// follows the person to every device: a change is kept only on the revision
// it was made on (keepSidebarLayout), and their other open screens are told
// to read it again; a workspace without one takes the
// default, and a section no tab names goes back to the home tab. The tabs
// are kept as the list was before it had none: completing a layout puts
// their sections and shortcuts after the home tab's.

const (
	sidebarLayoutMaxBodyBytes   = 32768
	sidebarSectionDefaultLimit  = 5
	sidebarFavoriteKeyMaxLength = 160
)

// sidebarSectionDefaults are a workspace's sections at first, in Notion's
// order: favorites, the teamspaces, private pages and shared pages, and the
// recent pages and the developer links, off the sidebar until added. Each
// shows five, as Notion's recent, private, teamspace and shared pages do
// (seen 2026-10-07).
var sidebarSectionDefaults = []sidebarLayoutSectionWire{
	{ID: sidebarSectionFavorites, Sort: sidebarSectionSortManual, Limit: sidebarSectionDefaultLimit},
	{ID: sidebarSectionTeamspaces, Sort: sidebarSectionSortManual, Limit: sidebarSectionDefaultLimit},
	{ID: sidebarSectionPrivate, Sort: sidebarSectionSortEdited, Limit: sidebarSectionDefaultLimit},
	{ID: sidebarSectionShared, Sort: sidebarSectionSortEdited, Limit: sidebarSectionDefaultLimit},
	{ID: sidebarSectionRecents, Sort: sidebarSectionSortEdited, Limit: sidebarSectionDefaultLimit, Hidden: true},
	{ID: sidebarSectionDeveloper, Sort: sidebarSectionSortManual, Limit: sidebarSectionDefaultLimit, Hidden: true},
}

var sidebarSectionLimits = map[int]bool{5: true, 10: true, 15: true, 20: true, 50: true, 100: true}

// The home tab is the one a person can't add; a tab they add has an id of
// its own.
const sidebarHomeTabID = "home"

var sidebarTabIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func registerSidebarLayoutRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+workspaceSidebarLayoutReadPath, readSidebarLayoutRoute)
	event.Router.PUT(apiRoot+workspaceSidebarLayoutSavePath, saveSidebarLayoutRoute).
		// Any layout the checks admit fits in the body, so its size is
		// refused below with the reason, not by the body limit.
		Bind(apis.BodyLimit(8 * sidebarLayoutMaxBodyBytes))
}

func readSidebarLayoutRoute(event *core.RequestEvent) error {
	user, workspaceID, ok, err := sidebarLayoutRequest(event)
	if !ok {
		return err
	}
	stored := struct {
		Layout    string `db:"layout"`
		UpdatedAt int64  `db:"updated_at"`
	}{}
	readErr := event.App.DB().NewQuery(`
		SELECT [[layout]], [[updated_at]] FROM {{leafen_sidebar_layouts}}
		WHERE [[owner_id]] = {:ownerId} AND [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"ownerId": user.Id, "workspaceId": workspaceID}).One(&stored)
	if readErr != nil && !errors.Is(readErr, sql.ErrNoRows) {
		return sidebarLayoutFailure(event, readErr)
	}
	if stored.Layout == "" {
		// Until they keep a layout of their own, a person's sidebar starts
		// with the workspace's default tab presets, at revision 0.
		presets, err := loadSidebarPresets(event.App, workspaceID)
		if err != nil {
			return sidebarLayoutFailure(event, err)
		}
		return event.JSON(http.StatusOK, sidebarLayoutFromPresets(presets))
	}
	layout := sidebarLayoutWire{}
	_ = json.Unmarshal([]byte(stored.Layout), &layout)
	layout = completeSidebarLayout(layout)
	layout.Revision = stored.UpdatedAt
	return event.JSON(http.StatusOK, layout)
}

func saveSidebarLayoutRoute(event *core.RequestEvent) error {
	user, workspaceID, ok, err := sidebarLayoutRequest(event)
	if !ok {
		return err
	}
	input := sidebarLayoutWire{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil || !validSidebarLayout(input) {
		return apiError(event, http.StatusBadRequest, errorCodeSidebarLayoutInvalid, "Check the sidebar layout.")
	}
	// The revision is the row's, not the layout's own: it isn't kept in it.
	layout := completeSidebarLayout(input)
	encoded, err := json.Marshal(layout)
	if err != nil {
		return sidebarLayoutFailure(event, err)
	}
	// What the store keeps is bounded: the layout is read with every page, so
	// past its size it is refused, saying so.
	if len(encoded) > sidebarLayoutMaxBodyBytes {
		return apiError(event, http.StatusBadRequest, errorCodeSidebarLayoutTooLarge,
			"The sidebar layout is too large to keep (32 KB). Remove shortcuts or tabs you no longer use.")
	}
	revision, kept, err := keepSidebarLayout(event.App, user.Id, workspaceID, string(encoded), input.Revision)
	if err != nil {
		return sidebarLayoutFailure(event, err)
	}
	if !kept {
		return apiError(event, http.StatusConflict, errorCodeSidebarLayoutRevisionConflict,
			"The sidebar layout changed since it was read (on another screen or device, or by an agent). "+
				"Read it again, make the change on what it is now and send it with the revision it came with.")
	}
	// The person's other screens read it again (workspace_page_list_wait.go).
	noteSidebarLayoutChanges(workspaceID, user.Id, revision)
	layout.Revision = revision
	return event.JSON(http.StatusOK, layout)
}

// keepSidebarLayout replaces a person's layout only if it is still at the
// revision the change was made on — its updated_at, 0 while they keep none
// — so a change made on a layout read before another screen, device or
// agent changed it is refused rather than undoing that change unseen. The
// new revision is past the old one even if the clock went back.
func keepSidebarLayout(app core.App, ownerID, workspaceID, encoded string, base int64) (int64, bool, error) {
	revision := max(time.Now().UnixMilli(), base+1)
	params := dbx.Params{"ownerId": ownerID, "workspaceId": workspaceID, "layout": encoded, "base": base, "revision": revision}
	updated, err := app.DB().NewQuery(`
		UPDATE {{leafen_sidebar_layouts}} SET [[layout]] = {:layout}, [[updated_at]] = {:revision}
		WHERE [[owner_id]] = {:ownerId} AND [[workspace_id]] = {:workspaceId} AND [[updated_at]] = {:base}
	`).Bind(params).Execute()
	if err != nil {
		return 0, false, err
	}
	if count, err := updated.RowsAffected(); err != nil || count == 1 {
		return revision, err == nil, err
	}
	if base != 0 {
		return 0, false, nil
	}
	inserted, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_sidebar_layouts}} ([[owner_id]], [[workspace_id]], [[layout]], [[updated_at]])
		VALUES ({:ownerId}, {:workspaceId}, {:layout}, {:revision})
		ON CONFLICT ([[owner_id]], [[workspace_id]]) DO NOTHING
	`).Bind(params).Execute()
	if err != nil {
		return 0, false, err
	}
	count, err := inserted.RowsAffected()
	return revision, err == nil && count == 1, err
}

// sidebarLayoutRequest is the signed-in person and the workspace they read,
// a member's or a guest's; ok false when it answered instead.
func sidebarLayoutRequest(event *core.RequestEvent) (*core.Record, string, bool, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return nil, "", false, apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if workspaceID == "" {
		return nil, "", false, apiError(event, http.StatusBadRequest, errorCodeSidebarLayoutInvalid, "Check the sidebar layout.")
	}
	if _, err := authorizeWorkspaceReader(event.App, user.Id, workspaceID); err != nil {
		if isPermissionDenied(err) {
			return nil, "", false, apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return nil, "", false, sidebarLayoutFailure(event, err)
	}
	return user, workspaceID, true, nil
}

func sidebarLayoutFailure(event *core.RequestEvent, err error) error {
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeSidebarLayoutFailed,
		"Couldn't keep the sidebar layout.", "sidebar_layout", "storage", errorCodeSidebarLayoutFailed, err)
}

// validSidebarLayout takes each known section, database view and teamspace
// once in a tab, with a known sort and count, and at most ten tabs with their own ids and names of at
// most forty characters.
func validSidebarLayout(layout sidebarLayoutWire) bool {
	if len(layout.Tabs) > sidebarTabsLimit || !validSidebarTabName(layout.HomeName) {
		return false
	}
	validSections := func(sections []sidebarLayoutSectionWire) bool {
		seen := map[string]bool{}
		views := 0
		for _, section := range sections {
			key := sidebarLayoutSectionKey(section)
			switch section.ID {
			case sidebarSectionView:
				if section.View == nil || !validSidebarLayoutView(*section.View) || section.TeamspaceID != "" {
					return false
				}
				views++
			case sidebarSectionTeamspace:
				// One teamspace's pages ("섹션 추가 › 팀스페이스 ›", Notion's,
				// seen 2026-11-11), naming the teamspace by its id.
				id := section.TeamspaceID
				if id == "" || len(id) > maxWorkspacePermissionIdentifierLength || strings.TrimSpace(id) != id ||
					!utf8.ValidString(id) || section.View != nil {
					return false
				}
			default:
				known := false
				for _, candidate := range sidebarSectionDefaults {
					known = known || candidate.ID == section.ID
				}
				if !known || section.View != nil || section.TeamspaceID != "" {
					return false
				}
			}
			if seen[key] || !sidebarSectionLimits[section.Limit] ||
				(section.Sort != sidebarSectionSortManual && section.Sort != sidebarSectionSortEdited) {
				return false
			}
			seen[key] = true
		}
		return views <= sidebarViewsPerTab
	}
	if !validSections(layout.Sections) || !validSidebarShortcuts(layout.Shortcuts) {
		return false
	}
	// The reader's own order of the teamspaces they're in, each once, as many
	// as they are in; the layout's size bounds the whole.
	ordered := map[string]bool{}
	for _, id := range layout.TeamspaceOrder {
		if id == "" || len(id) > maxWorkspacePermissionIdentifierLength || ordered[id] {
			return false
		}
		ordered[id] = true
	}
	// The reader's own order of their favorites (Notion's: a new one comes
	// first, and a drag in the section moves only it there, seen
	// 2026-11-06), each once: a page's id, or a database's or a form's key.
	favorites := map[string]bool{}
	for _, key := range layout.FavoriteOrder {
		if key == "" || len(key) > sidebarFavoriteKeyMaxLength || favorites[key] {
			return false
		}
		favorites[key] = true
	}
	tabIDs := map[string]bool{}
	for _, tab := range layout.Tabs {
		if !sidebarTabIDPattern.MatchString(tab.ID) || tab.ID == sidebarHomeTabID || tabIDs[tab.ID] ||
			!validSidebarTabName(tab.Name) || !validSections(tab.Sections) || !validSidebarShortcuts(tab.Shortcuts) {
			return false
		}
		tabIDs[tab.ID] = true
	}
	return true
}

// validSidebarShortcuts takes at most ten shortcuts, each key once: a new
// page names the page it goes in (none for the top of the private pages), a
// page names itself, a database's new page names the page holding the
// database, its data source and the view the page is added to, and may name
// the template it starts from, and the others name none; a page or a new
// page may say where it opens.
func validSidebarShortcuts(shortcuts []sidebarLayoutShortcutWire) bool {
	if len(shortcuts) > sidebarShortcutsPerTab {
		return false
	}
	keys := map[string]bool{}
	for _, shortcut := range shortcuts {
		if !sidebarTabIDPattern.MatchString(shortcut.Key) || keys[shortcut.Key] || !utf8.ValidString(shortcut.Name) ||
			utf8.RuneCountInString(shortcut.Name) > sidebarViewNameRunes || len(shortcut.PageID) > maxGraphIdentifierLength ||
			strings.TrimSpace(shortcut.PageID) != shortcut.PageID || !validSidebarOpenMode(shortcut.OpenIn) {
			return false
		}
		if shortcut.Kind != sidebarShortcutNewDatabasePage && (shortcut.DataSourceID != "" || shortcut.ViewID != "" || shortcut.TemplateID != "") {
			return false
		}
		switch shortcut.Kind {
		case sidebarShortcutNewPage:
		case sidebarShortcutPage, sidebarShortcutApp:
			// An app's names its slug here; Name keeps what it is called.
			if shortcut.PageID == "" {
				return false
			}
		case sidebarShortcutNewDatabasePage:
			if !validSidebarLayoutView(sidebarLayoutViewWire{Key: shortcut.Key, PageID: shortcut.PageID, DataSourceID: shortcut.DataSourceID,
				ViewID: shortcut.ViewID, Name: shortcut.Name, OpenIn: shortcut.OpenIn, TemplateID: shortcut.TemplateID}) {
				return false
			}
		case sidebarShortcutSettings, sidebarShortcutLibrary, sidebarShortcutDocuments, sidebarShortcutMyTasks, sidebarShortcutNewDatabase:
			if shortcut.PageID != "" || shortcut.OpenIn != "" {
				return false
			}
		default:
			return false
		}
		keys[shortcut.Key] = true
	}
	return true
}

// validSidebarOpenMode is where a page opens: beside the open page, over its
// center or as a full page; none takes the default.
func validSidebarOpenMode(mode string) bool {
	switch mode {
	case "", sidebarOpenSide, sidebarOpenCenter, sidebarOpenFull:
		return true
	}
	return false
}

// validSidebarLayoutView names a database view by ids of the content graph
// and a name of at most a hundred characters, with where its rows open and
// the template a new row starts from, if any.
func validSidebarLayoutView(view sidebarLayoutViewWire) bool {
	for _, id := range []string{view.PageID, view.DataSourceID, view.ViewID} {
		if id == "" || len(id) > maxGraphIdentifierLength || !utf8.ValidString(id) || strings.TrimSpace(id) != id {
			return false
		}
	}
	if len(view.TemplateID) > maxGraphIdentifierLength || !utf8.ValidString(view.TemplateID) ||
		strings.TrimSpace(view.TemplateID) != view.TemplateID {
		return false
	}
	return sidebarTabIDPattern.MatchString(view.Key) && utf8.ValidString(view.Name) &&
		utf8.RuneCountInString(view.Name) <= sidebarViewNameRunes && validSidebarOpenMode(view.OpenIn)
}

func validSidebarTabName(name string) bool {
	return utf8.ValidString(name) && utf8.RuneCountInString(name) <= sidebarTabNameRunes
}

// completeSidebarLayout is a layout as one list: the home tab's sections in
// their order, then each tab's it doesn't hold, with their shortcuts after
// its own, and each section none names, as at first. The conversations and
// the recent edits a layout kept from 2026-10-29 go (Leafen holds no
// agents), the rest of it as it was.
func completeSidebarLayout(layout sidebarLayoutWire) sidebarLayoutWire {
	layout.Sections = withoutRetiredSidebarSections(layout.Sections)
	for index := range layout.Tabs {
		layout.Tabs[index].Sections = withoutRetiredSidebarSections(layout.Tabs[index].Sections)
	}
	complete := sidebarLayoutWire{Sections: []sidebarLayoutSectionWire{}, Tabs: []sidebarLayoutTabWire{}}
	listed, views := map[string]int{}, 0
	// A tab's section joins the list once, after the home tab's, as many
	// database views as a list holds; one the home tab had taken off comes
	// back where the tab showed it.
	add := func(section sidebarLayoutSectionWire) {
		key := sidebarLayoutSectionKey(section)
		if at, found := listed[key]; found {
			complete.Sections[at].Hidden = complete.Sections[at].Hidden && section.Hidden
			return
		}
		if section.ID == sidebarSectionView {
			if views == sidebarViewsPerTab {
				return
			}
			views++
		}
		listed[key] = len(complete.Sections)
		complete.Sections = append(complete.Sections, section)
	}
	if validSidebarLayout(layout) {
		complete.HomeName, complete.HomeNameHidden = layout.HomeName, layout.HomeNameHidden
		complete.TeamspaceOrder = layout.TeamspaceOrder
		complete.FavoriteOrder = layout.FavoriteOrder
		shortcuts := map[string]bool{}
		addShortcuts := func(from []sidebarLayoutShortcutWire) {
			for _, shortcut := range from {
				if !shortcuts[shortcut.Key] && len(complete.Shortcuts) < sidebarShortcutsPerTab {
					shortcuts[shortcut.Key] = true
					complete.Shortcuts = append(complete.Shortcuts, shortcut)
				}
			}
		}
		addShortcuts(layout.Shortcuts)
		for _, section := range layout.Sections {
			add(section)
		}
		for _, tab := range layout.Tabs {
			addShortcuts(tab.Shortcuts)
			for _, section := range tab.Sections {
				add(section)
			}
		}
	}
	for _, section := range sidebarSectionDefaults {
		if _, found := listed[section.ID]; !found {
			complete.Sections = append(complete.Sections, section)
		}
	}
	return complete
}

// sidebarLayoutSectionKey is a section's key in a tab: a known section's id,
// a database view's "view:" and its key, or one teamspace's "teamspace:" and
// its id; a tab holds each once.
func sidebarLayoutSectionKey(section sidebarLayoutSectionWire) string {
	switch section.ID {
	case sidebarSectionView:
		if section.View != nil {
			return "view:" + section.View.Key
		}
	case sidebarSectionTeamspace:
		return "teamspace:" + section.TeamspaceID
	}
	return section.ID
}

// withoutRetiredSidebarSections leaves out the conversations and the recent
// edits.
func withoutRetiredSidebarSections(sections []sidebarLayoutSectionWire) []sidebarLayoutSectionWire {
	kept := []sidebarLayoutSectionWire{}
	for _, section := range sections {
		if section.ID != "chats" && section.ID != "edits" {
			kept = append(kept, section)
		}
	}
	return kept
}
