package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Home's "최근 편집": the pages the reader edited last in a workspace,
// newest first, while the reader can still read them and they are out of the
// trash, with every page above them.

// recentEditsLooked bounds the pages looked at for the ones the reader may
// read.
const recentEditsLooked = 100

// recentEditTitleSQL is a page's title as the list names it: a template by
// its name, as its database's menus name it (databaseTemplateNameSQL), as
// Notion's Home names a template the reader was in (seen 2026-11-11: "최근
// 방문" carries an edited template's card under its name).
var recentEditTitleSQL = `CASE WHEN COALESCE([[leafen_pages.` + databaseTemplatePageField + `]], FALSE)
	THEN COALESCE((SELECT ` + databaseTemplateNameSQL("[[template]]") + ` FROM {{leafen_database_templates}} [[template]]
		WHERE [[template]].[[id]] = [[leafen_pages.id]]), [[leafen_pages.title]])
	ELSE [[leafen_pages.title]] END AS [[title]]`

func registerRecentEditRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+workspaceRecentEditsReadPath, readRecentEditsRoute)
}

func readRecentEditsRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspaceReader(event.App, user.Id, workspaceID); err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return pageVisitFailure(event, err)
	}
	stored := []struct {
		ID           string `db:"id"`
		WorkspaceID  string `db:"workspace"`
		Title        string `db:"title"`
		Icon         string `db:"icon"`
		IconType     string `db:"icon_type"`
		Updated      string `db:"updated"`
		LastEditedBy string `db:"last_edited_by"`
		DataSourceID string `db:"data_source_id"`
		Template     bool   `db:"template"`
	}{}
	if err := event.App.DB().Select(
		"id", "workspace", recentEditTitleSQL, "COALESCE([[icon]], '') AS [[icon]]", "COALESCE([[icon_type]], '') AS [[icon_type]]",
		"updated", "last_edited_by", "COALESCE([[data_source_id]], '') AS [[data_source_id]]",
		"COALESCE([["+databaseTemplatePageField+"]], FALSE) AS [[template]]",
	).From("leafen_pages").
		Where(dbx.HashExp{"workspace": workspaceID, "last_edited_by": user.Id}).
		// The flags are read after the row (+): SQLite, which has no
		// statistics, took the (workspace, import) index and read the
		// workspace's every page rather than the reader's edits.
		AndWhere(dbx.NewExp("+[[in_trash]] = FALSE AND +[[notion_import_staging]] = FALSE")).
		OrderBy("updated DESC", "id").Limit(recentEditsLooked).All(&stored); err != nil {
		return pageVisitFailure(event, err)
	}
	// A page under a page in the trash, or a database row in the trash, went
	// there with it (loadPageTrashEffects): it leaves the list as a trashed
	// page does, and takes no place of the pages listed.
	editedIDs := make([]string, 0, len(stored))
	for _, row := range stored {
		editedIDs = append(editedIDs, row.ID)
	}
	trashed, err := loadPageTrashEffects(event.App, editedIDs)
	if err != nil {
		return pageVisitFailure(event, err)
	}
	result := recentEditsWire{Edits: []recentEditWire{}}
	names := map[string]string{}
	untitled := newDatabaseUntitledRowNames(event.Request.Context(), event.App, user.Id)
	for _, row := range stored {
		if len(result.Edits) == recentEditsListed {
			break
		}
		if _, inTrash := trashed[row.ID]; inTrash {
			continue
		}
		// A page the reader can't read leaves the list.
		if _, _, err := pageVisitWorkspace(event.App, user.Id, row.ID); err != nil {
			if isPermissionDenied(err) {
				continue
			}
			return pageVisitFailure(event, err)
		}
		if _, known := names[row.LastEditedBy]; !known {
			names[row.LastEditedBy] = userDisplayName(event.App, row.LastEditedBy)
		}
		edit := recentEditWire{
			PageID: row.ID, WorkspaceID: row.WorkspaceID, Title: row.Title, Icon: row.Icon, IconType: row.IconType,
			EditedAt: recentEditTime(row.Updated), EditorID: row.LastEditedBy, EditorName: names[row.LastEditedBy],
		}
		// A database row, or a template with neither a title nor a name of
		// its own, is called by its database's word for its untitled pages.
		if row.DataSourceID != "" {
			name, err := untitled.of(row.DataSourceID)
			if err != nil {
				return pageVisitFailure(event, err)
			}
			edit.UntitledName = name
		}
		result.Edits = append(result.Edits, edit)
	}
	// Each title as its reader reads it (page_name_mentions.go).
	titles := make(map[string]*string, len(result.Edits))
	for index := range result.Edits {
		titles[result.Edits[index].PageID] = &result.Edits[index].Title
	}
	pageTitlesAsRead(event.App, user.Id, titles, true)
	// and its spans, for a plain title to say its mentions (page_title_spans.go).
	spans := pageTitlesSpansAsRead(event.App, user.Id, titles)
	// and its kind, which names it while untitled where the reader's list
	// doesn't hold it (page_name_kind.go).
	ids := make([]string, 0, len(result.Edits))
	for _, edit := range result.Edits {
		ids = append(ids, edit.PageID)
	}
	kinds, err := loadPageNameKinds(event.App, ids)
	if err != nil {
		return pageVisitFailure(event, err)
	}
	for index := range result.Edits {
		result.Edits[index].TitleSpans = spans[result.Edits[index].PageID]
		if kind := kinds[result.Edits[index].PageID]; kind.Kind == pageKindDatabase {
			result.Edits[index].PageKind, result.Edits[index].ViewBlock, result.Edits[index].SourceTitles = kind.Kind, kind.ViewBlock, kind.SourceTitles
		}
	}
	return event.JSON(http.StatusOK, result)
}

// recentEditTime is when a page was last edited, as the page list says it.
func recentEditTime(updated string) string {
	at, err := types.ParseDateTime(updated)
	if err != nil || at.IsZero() {
		return ""
	}
	return databaseRowPageTime(at.Time().UnixMilli())
}
