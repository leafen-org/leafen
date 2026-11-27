package app

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A data source's templates are the row templates the editor keeps for it,
// in their order, without deleted ones. A template is a page of its data
// source, so its public ID is the one its page answers with; the ID derived
// from its workspace and native ID, which earlier lists gave, still names it.
// A cursor is resolved within the one source's templates.

type notionDataSourceTemplate struct {
	ID        string `db:"id"`
	Name      string `db:"name"`
	IsDefault bool   `db:"is_default"`
}

func serveNotionDataSourceTemplates(event *core.RequestEvent, publicID string) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:read", permissionWorkspaceRead)
	if err != nil || !ok {
		return err
	}
	identity, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityDataSource, publicID)
	if errors.Is(err, errInvalidNotionObjectIdentity) ||
		err == nil && identity.Workspace != workspace.Identity.WorkspaceID {
		return notionPageNotFound(event)
	}
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	// Its templates read with it in the trash too (notion_trash_reads.go).
	authorization, err := authorizeNotionDataSourceParentReach(
		event.App, workspace.Identity, identity.InternalID, permissionWorkspaceRead, true,
	)
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	query := event.Request.URL.Query()
	pageSize := 100
	if raw := notionQueryDigits(query.Get("page_size")); raw != "" {
		if pageSize, err = strconv.Atoi(raw); err != nil || pageSize < 1 || pageSize > 100 {
			return notionPageValidationError(event, "page_size must be between 1 and 100.")
		}
	}
	templates, err := loadNotionDataSourceTemplates(event.App, authorization.Container)
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	if name := foldSearchText(strings.TrimSpace(query.Get("name"))); name != "" {
		templates = slices.DeleteFunc(templates, func(template notionDataSourceTemplate) bool {
			return !strings.Contains(foldSearchText(template.Name), name)
		})
	}
	workspaceID := authorization.Container.WorkspaceID
	publicIDs := make([]string, len(templates))
	for index, template := range templates {
		if publicIDs[index], err = notionTemplatePagePublicID(event.App, workspaceID, template.ID); err != nil {
			return notionDataSourceReadError(event, err)
		}
	}
	offset := 0
	if cursor := strings.ToLower(strings.TrimSpace(query.Get("start_cursor"))); cursor != "" {
		offset = slices.IndexFunc(templates, func(template notionDataSourceTemplate) bool {
			return notionDataSourceTemplateNamed(workspaceID, template.ID, cursor)
		})
		if offset < 0 {
			return notionStartCursorRefused(event, query.Get("start_cursor"))
		}
	}
	end := min(offset+pageSize, len(templates))
	results := make([]notionDataSourceTemplateItem, 0, end-offset)
	for index, template := range templates[offset:end] {
		results = append(results, notionDataSourceTemplateItem{
			ID: publicIDs[offset+index], Name: template.Name, IsDefault: template.IsDefault,
		})
	}
	var next any
	if end < len(templates) {
		next = publicIDs[end]
	}
	// The list names no request_id, as Notion's doesn't (seen 2026-11-12);
	// a refusal does.
	notionAnswerWithoutRequestID(event)
	return writeNotionJSON(event, http.StatusOK, notionDataSourceTemplateList{
		Templates: results, HasMore: next != nil, NextCursor: next,
	})
}

// notionDataSourceTemplateList is a page of a source's templates in Notion's
// order (seen 2026-11-12: templates, has_more, next_cursor), each template's
// as its reference lists them.
type notionDataSourceTemplateList struct {
	Templates  []notionDataSourceTemplateItem `json:"templates"`
	HasMore    bool                           `json:"has_more"`
	NextCursor any                            `json:"next_cursor"`
}

type notionDataSourceTemplateItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

// notionDataSourceTemplateNamed reports whether a public ID names a template:
// the ID its page answers with (rowPageOwnPublicID), or the ones earlier
// lists gave — the derived one, then its page's row identity.
func notionDataSourceTemplateNamed(workspaceID, templateID, publicID string) bool {
	publicID = strings.ToLower(strings.TrimSpace(publicID))
	if publicID == "" {
		return false
	}
	if notionDataSourceTemplateID(workspaceID, templateID) == publicID {
		return true
	}
	normalized, ok := normalizeNotionUUIDRequest(publicID)
	if !ok {
		return false
	}
	if own, named := rowPageOwnPublicID(templateID); named && normalized == own {
		return true
	}
	pageID, err := notionCompatibleObjectID(notionObjectIdentityRowPage, workspaceID, templateID)
	return err == nil && normalized == pageID
}

// notionDataSourceTemplateID derives the public UUIDv4-shaped ID earlier
// template lists gave.
func notionDataSourceTemplateID(workspaceID, templateID string) string {
	digest := sha256.Sum256([]byte("leafen:notion-template-identity:v1\x00" + workspaceID + "\x00" + templateID))
	digest[6] = digest[6]&0x0f | 0x40
	digest[8] = digest[8]&0x3f | 0x80
	var encoded [16]byte
	copy(encoded[:], digest[:16])
	return formatNotionUserUUID(encoded)
}

// loadNotionDataSourceTemplates reads a source's live templates in order. A
// template's name is its page's title, as in Notion; a template whose title is
// empty goes by the name it was made with.
func loadNotionDataSourceTemplates(
	app core.App, container notionDataSourceParentAuthority,
) ([]notionDataSourceTemplate, error) {
	templates := []notionDataSourceTemplate{}
	err := app.DB().NewQuery(`
		SELECT [[template]].[[id]],
			COALESCE(NULLIF(TRIM([[template]].[[title]]), ''), [[template]].[[name]]) AS [[name]],
			[[template]].[[is_default]]
		FROM {{leafen_database_templates}} AS [[template]]
		LEFT JOIN {{leafen_database_toolbar_template_state}} AS [[state]] ON [[state]].[[template_id]] = [[template]].[[id]]
		WHERE [[template]].[[owner]] = {:owner} AND [[template]].[[workspace_id]] = {:workspaceId}
			AND [[template]].[[page_id]] = {:pageId} AND [[template]].[[data_source_id]] = {:dataSourceId}
			AND COALESCE([[state]].[[deleted_at]], '') = ''
		ORDER BY [[template]].[[position]], [[template]].[[id]]
	`).Bind(dbx.Params{
		"owner": container.OwnerID, "workspaceId": container.WorkspaceID,
		"pageId": container.PageID, "dataSourceId": container.DataSource,
	}).All(&templates)
	return templates, err
}

// notionPageCreateTemplate finds the template a page create names among its
// data source's live templates, with the authority that reads it. Whoever
// makes the page, or changes it, was let write it already — an editor of the
// database's page, a row shared with them to edit
// (authorizeNotionDatabaseRowPageCurrent, authorizeNotionDataSourceItemParent)
// — so the template is only read from the database they read, as a row made
// in its views reads it.
func notionPageCreateTemplate(
	app core.App, authorization notionDatabaseRowPageAuthorization, request notionPageTemplateRequest,
) (storedDatabaseToolbarTemplate, databaseToolbarConfigAuthority, error) {
	container := authorization.Container
	authority, err := authorizeDatabaseToolbarConfig(
		app, authorization.Identity.PrincipalID, container.PageID, container.DataSource, permissionWorkspaceRead,
	)
	if err != nil {
		return storedDatabaseToolbarTemplate{}, authority, refusedUnlessStorage(errDatabaseRowPageNotFound, err)
	}
	templates, err := loadNotionDataSourceTemplates(app, container)
	if err != nil {
		return storedDatabaseToolbarTemplate{}, authority, err
	}
	templateID := ""
	for _, template := range templates {
		if request.Type == "default" && template.IsDefault ||
			request.Type == "template_id" && notionDataSourceTemplateNamed(container.WorkspaceID, template.ID, request.TemplateID) {
			templateID = template.ID
			break
		}
	}
	if templateID == "" {
		if request.Type == "default" {
			return storedDatabaseToolbarTemplate{}, authority, notionDatabaseRowPageRequestInvalid(
				"This data source has no default template: give a template_id, or use type 'none'.")
		}
		return storedDatabaseToolbarTemplate{}, authority, notionPageTemplateMissing(app, authorization.Identity, request.TemplateID)
	}
	stored, err := databaseToolbarTemplateForMutation(app, authority, templateID)
	if err != nil || stored.DeletedAt != "" {
		return storedDatabaseToolbarTemplate{}, authority, refusedUnlessStorage(errDatabaseRowPageConflict, err)
	}
	return stored, authority, nil
}

// notionPageCreateTemplateSource finds the page a template_id names when it
// names none of the data source's live templates but a page, a database page
// or a database the connection reaches, which a page made or updated in the
// data source takes as its template (notion_page_template_copy.go).
func notionPageCreateTemplateSource(
	app core.App, authorization notionDatabaseRowPageAuthorization, request notionPageTemplateRequest,
) (notionPageTemplateSource, bool, error) {
	if request.Type != "template_id" {
		return notionPageTemplateSource{}, false, nil
	}
	container := authorization.Container
	templates, err := loadNotionDataSourceTemplates(app, container)
	if err != nil {
		return notionPageTemplateSource{}, false, err
	}
	for _, template := range templates {
		if notionDataSourceTemplateNamed(container.WorkspaceID, template.ID, request.TemplateID) {
			return notionPageTemplateSource{}, false, nil
		}
	}
	source, reached := resolveNotionPageTemplateSource(app, authorization.Identity, strings.TrimSpace(request.TemplateID))
	return source, reached, nil
}

// notionPageTemplateMissing answers a template_id that names nothing the
// connection reaches: a page Notion can't find (seen 2026-11-14: 404 "Could
// not find page with ID: …", the ID dashed when written without dashes,
// otherwise as written). One it reaches is the page's template
// (notionPageCreateTemplateSource).
func notionPageTemplateMissing(app core.App, identity notionOAuthBearerIdentity, templateID string) error {
	id := strings.TrimSpace(templateID)
	if notionPageTemplateReached(app, identity, id) {
		return errDatabaseRowPageConflict
	}
	if dashed, ok := normalizeNotionUUIDRequest(id); ok {
		id = dashed
	}
	return &notionDatabaseRowPageRequestError{message: "template not found", missingPage: id}
}

// notionPageTemplateReached tells a template_id naming a page, a database
// page or a database the connection reads.
func notionPageTemplateReached(app core.App, identity notionOAuthBearerIdentity, publicID string) bool {
	_, reached := resolveNotionPageTemplateSource(app, identity, publicID)
	return reached
}
