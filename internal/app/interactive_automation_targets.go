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

type interactiveAutomationTargetDatabase struct {
	DataSourceID string `json:"dataSourceId"`
	PageID       string `json:"pageId"`
	Title        string `json:"title"`
	// Icon is the database's own, as its sources list and template band show it.
	Icon       string          `json:"icon,omitempty"`
	IconType   string          `json:"iconType,omitempty"`
	Properties []graphProperty `json:"properties"`
	Tasks      bool            `json:"tasks,omitempty"`
	// UntitledRowName is what its rows without a title are called
	// (databaseUntitledRowName), as a relation's picker names them.
	UntitledRowName string `json:"untitledRowName,omitempty"`
	// Templates are what a "페이지 추가" card offers to add its page from.
	Templates []interactiveAutomationTargetTemplate `json:"templates"`
}

const interactiveAutomationTargetTemplateLimit = 100

type interactiveAutomationTargetTemplate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// readInteractiveAutomationTargetDatabase is a database of the workspace an
// action may add to or edit, wherever its page is — one that takes added
// rows (database_added_rows.go), as Notion's "페이지 추가 위치" and "페이지
// 편집 위치" find no People: its page, its name and its properties, for
// someone setting the action up who may change that page, as running it
// needs. Anything else reads as not found.
func readInteractiveAutomationTargetDatabase(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	dataSourceID := strings.TrimSpace(event.Request.PathValue("dataSourceId"))
	if workspaceID == "" || dataSourceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength ||
		len(dataSourceID) > maxGraphIdentifierLength {
		return apiError(event, http.StatusBadRequest, errorCodeInteractiveAutomationInvalid, "Check the database.")
	}
	notFound := func() error {
		return apiError(event, http.StatusNotFound, errorCodeInteractiveAutomationNotFound, "Database not found.")
	}
	failed := func(cause error) error {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the database.", cause)
	}
	stored := struct {
		Owner    string `db:"owner"`
		PageID   string `db:"page_id"`
		Title    string `db:"title"`
		Icon     string `db:"icon"`
		IconType string `db:"icon_type"`
	}{}
	err := event.App.DB().NewQuery(`
		SELECT [[source]].[[owner]], [[source]].[[page_id]], [[source]].[[title]],
			COALESCE([[source]].[[icon]], '') AS [[icon]], COALESCE(NULLIF([[source]].[[icon_type]], ''), 'none') AS [[icon_type]]
		FROM {{leafen_data_sources}} AS [[source]]
		WHERE [[source]].[[id]] = {:id} AND [[source]].[[workspace_id]] = {:workspaceId}
			AND ` + databaseSourceNotTrashedSQL("[[source]].[[id]]") + `
			AND ` + databaseSourceTakesAddedRowsSQL("{:id}"),
	).Bind(dbx.Params{"id": dataSourceID, "workspaceId": workspaceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return notFound()
	}
	if err != nil {
		return failed(err)
	}
	authorization, err := authorizePageBody(event.App, user.Id, stored.PageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return notFound()
	}
	if err != nil {
		return failed(err)
	}
	if authorization.Workspace.WorkspaceID != workspaceID || authorization.Workspace.OwnerID != stored.Owner {
		return notFound()
	}
	if trashed, err := pageInTrashPath(event.App, stored.PageID); err != nil {
		return failed(err)
	} else if trashed {
		return notFound()
	}
	properties := []struct {
		ID       string `db:"id"`
		Type     string `db:"type"`
		Name     string `db:"name"`
		Position string `db:"position"`
		Config   string `db:"config"`
	}{}
	if err := event.App.DB().NewQuery(`
		SELECT [[id]], [[type]], [[name]], [[position]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:id}
			AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": stored.Owner, "pageId": stored.PageID, "id": dataSourceID, "limit": maxDatabaseQueryProperties + 1,
	}).All(&properties); err != nil {
		return failed(err)
	}
	// A database is never read in part: past what a query reads at all, it
	// says so instead of leaving properties out.
	if len(properties) > maxDatabaseQueryProperties {
		return apiError(event, http.StatusConflict, errorCodeInteractiveAutomationUnsupported, "This database has too many properties to edit from an action.")
	}
	target := interactiveAutomationTargetDatabase{
		DataSourceID: dataSourceID, PageID: stored.PageID, Title: stored.Title, Icon: stored.Icon, IconType: stored.IconType,
		Properties: make([]graphProperty, 0, len(properties)),
	}
	// An untitled source is the client's "새 데이터베이스" or "새 데이터 소스",
	// as Notion's, not the page holding it; a full-page database's page is
	// the database itself.
	if strings.TrimSpace(target.Title) == "" && authorization.Page.GetString("kind") == "database" {
		target.Title = authorization.Page.GetString("title")
	}
	for _, property := range properties {
		target.Properties = append(target.Properties, graphProperty{
			ID: property.ID, DataSourceID: dataSourceID, Type: property.Type, Name: property.Name,
			Position: property.Position, Config: json.RawMessage(property.Config),
		})
	}
	if roles, err := loadDatabaseTaskRoles(event.App, dataSourceID); err != nil {
		return failed(err)
	} else {
		target.Tasks = roles.Enabled
	}
	if target.UntitledRowName, err = databaseUntitledRowName(event.App, dataSourceID); err != nil {
		return failed(err)
	}
	target.Templates = []interactiveAutomationTargetTemplate{}
	templates, err := loadDatabaseToolbarTemplateMetadata(event.App, databaseToolbarConfigAuthority{
		OwnerID: stored.Owner, WorkspaceID: workspaceID, PageID: stored.PageID, DataSourceID: dataSourceID,
	}, databaseToolbarConfigCursor{}, interactiveAutomationTargetTemplateLimit)
	if err != nil {
		return failed(err)
	}
	for _, template := range templates {
		// A template goes by its page's title, as the new-row menu names it.
		name := strings.TrimSpace(template.Title)
		if name == "" {
			name = template.Name
		}
		target.Templates = append(target.Templates, interactiveAutomationTargetTemplate{ID: template.ID, Name: name})
	}
	return event.JSON(http.StatusOK, target)
}

// interactiveAutomationDatabasePage is the page holding a database a run
// reads to find the pages it edits, checked as its writes there are.
func interactiveAutomationDatabasePage(app core.App, authorization pageAuthorization, databaseID string) (string, error) {
	var pageID string
	err := app.DB().NewQuery(`SELECT [[source]].[[page_id]] FROM {{leafen_data_sources}} AS [[source]]
		WHERE [[source]].[[owner]] = {:owner} AND [[source]].[[id]] = {:id}
			AND ` + databaseSourceNotTrashedSQL("[[source]].[[id]]")).Bind(dbx.Params{
		"owner": authorization.Workspace.OwnerID, "id": databaseID,
	}).Row(&pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errInteractiveAutomationNotFound
	}
	if err != nil {
		return "", err
	}
	if pageID != authorization.Page.Id {
		if err := authorizeInteractiveAutomationTargetPage(app, authorization, pageID); err != nil {
			return "", err
		}
	}
	return pageID, nil
}

// authorizeInteractiveAutomationTargetPage lets a run write a database on
// another page: one in the same workspace, not in the trash, whose body the
// person running may change. Anything else reads as not found.
func authorizeInteractiveAutomationTargetPage(app core.App, authorization pageAuthorization, pageID string) error {
	target, err := authorizePageBody(app, authorization.Workspace.PrincipalID, pageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return errInteractiveAutomationNotFound
	}
	if err != nil {
		return err
	}
	if target.Workspace.WorkspaceID != authorization.Workspace.WorkspaceID || target.Workspace.OwnerID != authorization.Workspace.OwnerID {
		return errInteractiveAutomationNotFound
	}
	if trashed, err := pageInTrashPath(app, pageID); err != nil {
		return err
	} else if trashed {
		return errInteractiveAutomationNotFound
	}
	return nil
}
