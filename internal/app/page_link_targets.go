package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's link blocks point at a page, a database or a comment. The editor
// asks what each target is for the person reading: the page that holds it,
// its current title and icon and whether it is in the trash, or that it
// cannot open, when the target is out of their reach or gone. Nothing about
// a target the person cannot open is returned but that.

const maxPageLinkTargets = 64

type pageLinkTargetRequest struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type pageLinkTarget struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Available bool   `json:"available"`
	// Copying says the target is a page of a copy not yet made, which shows
	// when it is, as under Notion's copy (seen 2026-11-05).
	Copying bool `json:"copying,omitzero"`
	// InTrash says the page holding the target is in the trash: it still
	// opens, under the trash's banner, and the link says so, as Notion's
	// "휴지통에 있음" (seen 2026-11-06).
	InTrash  bool   `json:"inTrash,omitempty"`
	PageID   string `json:"pageId,omitempty"`
	BlockID  string `json:"blockId,omitempty"`
	Title    string `json:"title,omitempty"`
	Icon     string `json:"icon,omitempty"`
	IconType string `json:"iconType,omitempty"`
	// SourceTitles name a database without a name of several sources
	// (database_container_name.go).
	SourceTitles []databaseSourceTitle `json:"sourceTitles,omitempty"`
}

func registerPageLinkTargetRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/pages/{id}/link-targets", readPageLinkTargets)
}

func readPageLinkTargets(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := struct {
		Targets []pageLinkTargetRequest `json:"targets"`
	}{}
	if err := event.BindBody(&input); err != nil || len(input.Targets) == 0 || len(input.Targets) > maxPageLinkTargets {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the link targets.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	current, err := authorizePage(event.App, user.Id, pageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeGraphReadFailed,
			"Couldn't read the link targets.", "page_link_targets", "authorize", errorCodeGraphReadFailed, err)
	}
	targets := make([]pageLinkTarget, 0, len(input.Targets))
	for _, request := range input.Targets {
		request.ID = strings.TrimSpace(request.ID)
		if request.ID == "" || len(request.ID) > maxWorkspacePermissionIdentifierLength {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the link targets.")
		}
		var target pageLinkTarget
		switch request.Kind {
		case "page":
			target, err = resolvePageLinkPage(event.App, user.Id, current, request.ID)
		case "database":
			target, err = resolvePageLinkDatabase(event.App, user.Id, current, request.ID)
		case "comment":
			target, err = resolvePageLinkComment(event.App, user.Id, current, request.ID)
		default:
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the link targets.")
		}
		if err != nil {
			return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeGraphReadFailed,
				"Couldn't read the link targets.", "page_link_targets", "resolve", errorCodeGraphReadFailed, err)
		}
		target.Kind, target.ID = request.Kind, request.ID
		targets = append(targets, target)
	}
	return event.JSON(http.StatusOK, map[string]any{"targets": targets})
}

// pageLinkReadable reports whether the person can open a page of the same
// workspace, a workspace page or a database row page, and whether it is in
// the trash.
func pageLinkReadable(app core.App, principalID string, current pageAuthorization, pageID string) (bool, bool, error) {
	page, err := authorizePage(app, principalID, pageID, permissionWorkspaceRead)
	if err == nil {
		if page.Workspace.WorkspaceID != current.Workspace.WorkspaceID {
			return false, false, nil
		}
		trashed, err := pageInTrashPath(app, pageID)
		return err == nil, trashed, err
	}
	if !isPermissionDenied(err) {
		return false, false, err
	}
	row, err := authorizeDatabaseRowPage(app, principalID, pageID, permissionWorkspaceRead)
	if isPermissionDenied(err) || errors.Is(err, errDatabaseRowPageNotFound) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return row.WorkspaceID == current.Workspace.WorkspaceID, row.TrashedAt != 0, nil
}

func resolvePageLinkPage(app core.App, principalID string, current pageAuthorization, pageID string) (pageLinkTarget, error) {
	// A link to the page a full-page database left as it turned inline
	// leads to the database.
	place, signpost, err := loadDatabasePagePlace(app, pageID)
	if err != nil {
		return pageLinkTarget{}, err
	}
	if signpost {
		return resolvePageLinkDatabase(app, principalID, current, place.ContainerID)
	}
	readable, trashed, err := pageLinkReadable(app, principalID, current, pageID)
	if err != nil {
		return pageLinkTarget{}, err
	}
	if !readable {
		copying, err := pageDuplicationTargetToCome(app, pageID, current.Workspace.WorkspaceID)
		return pageLinkTarget{Copying: copying}, err
	}
	target := pageLinkTarget{Available: true, InTrash: trashed, PageID: pageID}
	if page, err := app.FindRecordById(pagesCollection, pageID); err == nil {
		// Its title as its reader reads it (page_name_mentions.go).
		target.Title, target.Icon, target.IconType = pageTitleAsRead(app, principalID, page), page.GetString("icon"), page.GetString("icon_type")
		return target, nil
	}
	title := ""
	if err := app.DB().NewQuery(`
		SELECT [[value]] FROM {{leafen_database_row_values}} WHERE [[row_id]] = {:rowId} AND [[type]] = 'title' LIMIT 1
	`).Bind(dbx.Params{"rowId": pageID}).Row(&title); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return pageLinkTarget{}, err
	}
	_ = json.Unmarshal([]byte(title), &target.Title)
	return target, nil
}

func resolvePageLinkDatabase(app core.App, principalID string, current pageAuthorization, containerID string) (pageLinkTarget, error) {
	container := struct {
		PageID  string `db:"page_id"`
		BlockID string `db:"block_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[page_id]], [[container]].[[block_id]] FROM {{leafen_database_containers}} AS [[container]]
		WHERE [[container]].[[id]] = {:containerId}
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
				WHERE [[trash]].[[container_id]] = [[container]].[[id]]
			)
	`).Bind(dbx.Params{"containerId": containerID}).One(&container)
	if errors.Is(err, sql.ErrNoRows) {
		return pageLinkTarget{}, nil
	}
	if err != nil {
		return pageLinkTarget{}, err
	}
	readable, trashed, err := pageLinkReadable(app, principalID, current, container.PageID)
	if err != nil || !readable {
		return pageLinkTarget{}, err
	}
	target := pageLinkTarget{Available: true, InTrash: trashed, PageID: container.PageID, BlockID: container.BlockID}
	source := struct {
		Icon     string `db:"icon"`
		IconType string `db:"icon_type"`
	}{}
	err = app.DB().NewQuery(`
		SELECT ` + dataSourceIconSQL("[[source]]") + ` AS [[icon]], ` + dataSourceIconTypeSQL("[[source]]") + ` AS [[icon_type]]
		FROM {{leafen_database_container_sources}} AS [[member]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[member]].[[data_source_id]]
		WHERE [[member]].[[container_id]] = {:containerId}
		ORDER BY [[member]].[[position]], [[member]].[[data_source_id]] LIMIT 1
	`).Bind(dbx.Params{"containerId": containerID}).One(&source)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return pageLinkTarget{}, err
	}
	// Its name as its reader reads it (database_container_name.go).
	titles, sources, err := databaseContainerNamesAsRead(app, readerCloser(app, principalID), []string{containerID})
	if err != nil {
		return pageLinkTarget{}, err
	}
	target.Title, target.SourceTitles, target.Icon, target.IconType = titles[containerID], sources[containerID], source.Icon, source.IconType
	return target, nil
}

func resolvePageLinkComment(app core.App, principalID string, current pageAuthorization, commentID string) (pageLinkTarget, error) {
	comment := struct {
		PageID  string `db:"page_id"`
		BlockID string `db:"block_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[page_id]], [[block_id]] FROM {{leafen_comments}} WHERE [[id]] = {:commentId}
	`).Bind(dbx.Params{"commentId": commentID}).One(&comment)
	if errors.Is(err, sql.ErrNoRows) {
		return pageLinkTarget{}, nil
	}
	if err != nil {
		return pageLinkTarget{}, err
	}
	readable, trashed, err := pageLinkReadable(app, principalID, current, comment.PageID)
	if err != nil || !readable {
		return pageLinkTarget{}, err
	}
	return pageLinkTarget{Available: true, InTrash: trashed, PageID: comment.PageID, BlockID: comment.BlockID}, nil
}
