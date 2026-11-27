package app

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A database page's empty body lists its database's templates, as Notion's
// (seen 2026-10-12: "…또는 템플릿을 선택하세요"), and the one chosen applies to
// the page as the Public API and MCP apply one (notion_database_row_page_template.go):
// its values fill the properties the page leaves empty, its icon a page
// without one, and its body follows the page's own, each synced block
// original in it a copy of it, as Notion's app applies one (seen 2026-11-14).
// withoutBody asks for the rest alone: a row made on a device from a template
// is filled as the change that makes it is taken, its body copied on the
// device coming along (content_graph_row_templates.go).

const databaseRowTemplateApplyPath = "/database-rows/{rowId}/apply-template"

func registerDatabaseRowTemplateApplyRoute(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.POST(apiRoot+databaseRowTemplateApplyPath, ygo.applyDatabaseRowTemplate).Bind(apis.BodyLimit(1024))
}

func (ygo *pocketBaseYgoPersistence) applyDatabaseRowTemplate(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := struct {
		TemplateID  string `json:"templateId"`
		TimeZone    string `json:"timeZone"`
		WithoutBody bool   `json:"withoutBody"`
	}{}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	if err := event.BindBody(&input); err != nil || rowID == "" || strings.TrimSpace(input.TemplateID) == "" ||
		len(input.TemplateID) > maxGraphIdentifierLength || len(input.TimeZone) > 64 {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Choose one of the database's templates.")
	}
	if err := ygo.fillDatabaseRowFromTemplate(event.Request.Context(), event.App, user.Id, rowID, input.TemplateID, input.TimeZone, input.WithoutBody); err != nil {
		if errors.Is(err, errDatabaseRowTemplateNotFound) {
			return apiError(event, http.StatusNotFound, errorCodeTemplateNotFound, "Template not found.")
		}
		return databaseRowPageValuesError(event, err)
	}
	// Its body's own databases come after the rest (database_row_page_database_copies.go):
	// the page says so while they are copied in.
	if copying, err := rowPageDatabasesCopying(event.App, rowID); err == nil && copying {
		return event.JSON(http.StatusOK, map[string]bool{"copyingDatabases": true})
	}
	return event.NoContent(http.StatusNoContent)
}

var errDatabaseRowTemplateNotFound = errors.New("database row template not found")

// fillDatabaseRowFromTemplate applies one of the row's database's templates,
// named by its Leafen ID, as the person: its body too, unless withoutBody.
func (ygo *pocketBaseYgoPersistence) fillDatabaseRowFromTemplate(
	ctx context.Context, app core.App, principalID, rowID, leafenTemplateID, timeZone string, withoutBody bool,
) error {
	authority, _, err := databaseRowPageMutationAuthorization(app, principalID, rowID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	identity := notionOAuthBearerIdentity{PrincipalID: principalID, ClientID: "leafen:app", WorkspaceID: authority.WorkspaceID,
		Scopes: append([]string{}, notionOAuthDefaultScopes...)}
	current, err := authorizeNotionDatabaseRowPageCurrent(app, identity, rowID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	// The page names a template by its Leafen ID; applying goes by the ID
	// the Public API gives it.
	templates, err := loadNotionDataSourceTemplates(app, current.Container)
	if err != nil {
		return err
	}
	templateID := ""
	for _, template := range templates {
		if template.ID == strings.TrimSpace(leafenTemplateID) {
			templateID = notionDataSourceTemplateID(current.Container.WorkspaceID, template.ID)
		}
	}
	if templateID == "" {
		return errDatabaseRowTemplateNotFound
	}
	return applyNotionDatabaseRowPageTemplate(ctx, app, ygo, identity, rowID,
		notionDatabaseRowPageTemplateApplication{Template: notionPageTemplateRequest{
			Type: "template_id", TemplateID: templateID, Timezone: strings.TrimSpace(timeZone),
		}, Synced: rowPageBodySyncedAsCopies, WithoutBody: withoutBody})
}
