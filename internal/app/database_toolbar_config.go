package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errDatabaseToolbarConfigNotFound        = errors.New(errorCodeDatabaseToolbarConfigNotFound)
	errDatabaseToolbarConfigInvalid         = errors.New(errorCodeDatabaseToolbarConfigInvalid)
	errDatabaseToolbarConfigRevisionChanged = errors.New(errorCodeDatabaseToolbarConfigRevisionChanged)
	errDatabaseToolbarConfigRequestReused   = errors.New(errorCodeDatabaseToolbarConfigRequestReused)
	errDatabaseToolbarConfigConflict        = errors.New(errorCodeDatabaseToolbarConfigConflict)
)

const databaseToolbarConfigMetadataQueryLimit = databaseToolbarConfigMetadataPageSize + 1

type databaseToolbarConfigAuthority struct {
	OwnerID      string `db:"owner"`
	WorkspaceID  string `db:"workspace_id"`
	PageID       string `db:"page_id"`
	DataSourceID string `db:"data_source_id"`
}

type databaseToolbarConfigCursor struct {
	PrincipalID              string `json:"principalId"`
	WorkspaceID              string `json:"workspaceId"`
	PageID                   string `json:"pageId"`
	DataSourceID             string `json:"dataSourceId"`
	WorkspaceContentRevision int    `json:"workspaceContentRevision"`
	Position                 string `json:"position"`
	TemplateID               string `json:"templateId"`
	Limit                    int    `json:"limit"`
}

type storedDatabaseToolbarTaskFeatures struct {
	Revision     int    `db:"authority_revision"`
	Subitems     string `db:"subitems_json"`
	Dependencies string `db:"dependencies_json"`
	Tasks        string `db:"tasks_json"`
}

type storedDatabaseToolbarTemplate struct {
	ID                string `db:"id"`
	Name              string `db:"name"`
	Icon              string `db:"icon"`
	Title             string `db:"title"`
	Properties        string `db:"properties_json"`
	PropertySpans     string `db:"property_spans_json"`
	IsDefault         bool   `db:"is_default"`
	Position          string `db:"position"`
	AuthorityRevision int    `db:"authority_revision"`
	DeletedAt         string `db:"deleted_at"`
	UpdatedAt         string `db:"updated_at"`
	// The template's repeat, when it has one.
	RepeatJSON         string `db:"repeat_json"`
	RepeatNextRunAt    int64  `db:"repeat_next_run_at"`
	RepeatPausedReason string `db:"repeat_paused_reason"`
	// Whether its page's body holds a block, for its icon.
	HasContent bool `db:"has_content"`
}

func registerDatabaseToolbarConfigRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.GET(apiRoot+databaseToolbarConfigMetadataPath, readDatabaseToolbarConfigMetadata)
	event.Router.GET(apiRoot+databaseToolbarConfigTemplateBodyPath, readDatabaseToolbarTemplateBody)
	event.Router.POST(apiRoot+databaseToolbarConfigMutatePath, mutateDatabaseToolbarConfig(ygo)).
		Bind(apis.BodyLimit(databaseToolbarConfigMaxRequestBytes))
	event.Router.POST(apiRoot+databaseToolbarTemplateRowCreatePath, ygo.createDatabaseToolbarTemplateRow).
		Bind(apis.BodyLimit(databaseToolbarConfigMaxRequestBytes))
}

func authorizeDatabaseToolbarConfig(
	app core.App,
	principalID, pageID, dataSourceID string,
	requirement permissionRequirement,
) (databaseToolbarConfigAuthority, error) {
	authority := databaseToolbarConfigAuthority{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[owner]], [[source]].[[workspace_id]], [[source]].[[page_id]],
			[[source]].[[id]] AS [[data_source_id]]
		FROM {{leafen_data_sources}} AS [[source]]
		WHERE [[source]].[[id]]={:dataSourceId} AND [[source]].[[page_id]]={:pageId}
			AND ` + databaseSourceNotTrashedSQL("[[source]].[[id]]") + `
	`).Bind(dbx.Params{"dataSourceId": dataSourceID, "pageId": pageID}).One(&authority)
	if errors.Is(err, sql.ErrNoRows) {
		return databaseToolbarConfigAuthority{}, errDatabaseToolbarConfigNotFound
	}
	if err != nil {
		return databaseToolbarConfigAuthority{}, err
	}
	page, err := authorizePageBody(app, principalID, pageID, requirement)
	if isPermissionDenied(err) {
		return databaseToolbarConfigAuthority{}, errDatabaseToolbarConfigNotFound
	}
	if err != nil {
		return databaseToolbarConfigAuthority{}, err
	}
	if page.Workspace.OwnerID != authority.OwnerID || page.Workspace.WorkspaceID != authority.WorkspaceID {
		return databaseToolbarConfigAuthority{}, errDatabaseToolbarConfigNotFound
	}
	return authority, nil
}

func databaseToolbarConfigIdentity(event *core.RequestEvent) (string, string, string) {
	return strings.TrimSpace(event.Request.PathValue("pageId")),
		strings.TrimSpace(event.Request.PathValue("dataSourceId")),
		strings.TrimSpace(event.Request.PathValue("templateId"))
}

// databaseToolbarConfigActor is the person signed in or through their key; ok false when
// it answered 401 instead, err then that answer's write.
func databaseToolbarConfigActor(event *core.RequestEvent) (*core.Record, bool, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return nil, false, apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	return user, true, nil
}

func writeDatabaseToolbarConfigError(event *core.RequestEvent, err error) error {
	if isDLPPolicyDecisionError(err) {
		return writeDLPPolicyError(event, err, "database-toolbar-config")
	}
	if storageErr, ok := persistentWriteStorageAPIError(
		event, err, persistentWriteDatabaseToolbarConfig, "transaction",
	); ok {
		return storageErr
	}
	switch {
	case errors.Is(err, errDatabaseToolbarConfigNotFound), isPermissionDenied(err):
		return apiError(event, http.StatusNotFound, errorCodeDatabaseToolbarConfigNotFound, "Database settings not found.")
	case errors.As(err, new(*databaseTaskFeaturesError)):
		// Task settings a screen would never send say what they lack.
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseToolbarConfigInvalid, "Check the task settings: "+err.Error()+".")
	case errors.Is(err, errDatabaseToolbarConfigInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseToolbarConfigInvalid, refusalMessage("Check the database settings request.", refusalReason(err)))
	case errors.Is(err, errDatabaseToolbarConfigRevisionChanged):
		message := "The database settings changed. Load them again from the start."
		if reason := refusalReason(err); reason != "" {
			message = refusalMessage("The database settings can't be changed this way now.", reason)
		}
		return apiError(event, http.StatusConflict, errorCodeDatabaseToolbarConfigRevisionChanged, message)
	case errors.Is(err, errDatabaseToolbarConfigRequestReused):
		return apiError(event, http.StatusConflict, errorCodeDatabaseToolbarConfigRequestReused, "This request ID was used for a different settings change.")
	case errors.Is(err, errDatabaseToolbarConfigConflict):
		return apiError(event, http.StatusConflict, errorCodeDatabaseToolbarConfigConflict, databaseToolbarConflictMessage(err))
	case errors.Is(err, errLegalHoldPreventsPermanentDelete):
		return apiError(event, http.StatusPreconditionFailed, errorCodeLegalHoldPreventsPermanentDelete,
			"Release the active legal hold, then turn sprints off.")
	default:
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeDatabaseToolbarConfigFailed,
			"Couldn't process the database settings.", "database_toolbar_config", "request",
			errorCodeDatabaseToolbarConfigFailed, err)
	}
}

func parseDatabaseToolbarConfigLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return databaseToolbarConfigMetadataPageSize, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > databaseToolbarConfigMetadataPageSize {
		return 0, databaseToolbarRefused("limit is a number from 1 to %d, not %q", databaseToolbarConfigMetadataPageSize, raw)
	}
	return limit, nil
}

func readDatabaseToolbarConfigMetadata(event *core.RequestEvent) error {
	user, ok, err := databaseToolbarConfigActor(event)
	if !ok {
		return err
	}
	pageID, dataSourceID, _ := databaseToolbarConfigIdentity(event)
	limit, parseErr := parseDatabaseToolbarConfigLimit(event.Request.URL.Query().Get("limit"))
	if pageID == "" || dataSourceID == "" {
		return writeDatabaseToolbarConfigError(event, databaseToolbarRefused("the page and data source IDs in the path are empty"))
	}
	if parseErr != nil {
		return writeDatabaseToolbarConfigError(event, parseErr)
	}
	response, loadErr := loadDatabaseToolbarConfigMetadata(
		event.App, user.Id, pageID, dataSourceID,
		event.Request.URL.Query().Get("cursor"), limit,
	)
	if loadErr != nil {
		return writeDatabaseToolbarConfigError(event, loadErr)
	}
	return event.JSON(http.StatusOK, response)
}

func loadDatabaseToolbarConfigMetadata(
	app core.App,
	principalID, pageID, dataSourceID, rawCursor string,
	limit int,
) (databaseToolbarConfigMetadataResponse, error) {
	response := databaseToolbarConfigMetadataResponse{}
	if limit < 1 || limit+1 > databaseToolbarConfigMetadataQueryLimit {
		return response, errDatabaseToolbarConfigInvalid
	}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		authority, err := authorizeDatabaseToolbarConfig(
			txApp, principalID, pageID, dataSourceID, permissionWorkspaceRead,
		)
		if err != nil {
			return err
		}
		var WorkspaceContentRevision int
		if err := txApp.DB().NewQuery(`SELECT [[revision]] FROM {{leafen_workspace_content_revisions}}
			WHERE [[workspace_id]]={:workspaceId}`).Bind(dbx.Params{
			"workspaceId": authority.WorkspaceID,
		}).Row(&WorkspaceContentRevision); err != nil {
			return err
		}
		cursor, err := decodeDatabaseToolbarConfigCursor(
			rawCursor, principalID, authority, WorkspaceContentRevision, limit,
		)
		if err != nil {
			return err
		}
		task, err := loadDatabaseToolbarTaskFeatures(txApp, authority)
		if err != nil {
			return err
		}
		templates, err := loadDatabaseToolbarTemplateMetadata(txApp, authority, cursor, limit)
		if err != nil {
			return err
		}
		hasMore := len(templates) > limit
		if hasMore {
			templates = templates[:limit]
		}
		SnapshotIdentity := databaseToolbarSnapshotIdentity(
			authority, WorkspaceContentRevision, task.Revision,
		)
		response = databaseToolbarConfigMetadataResponse{
			WorkspaceID: authority.WorkspaceID, PageID: pageID, DataSourceID: dataSourceID,
			WorkspaceContentRevision: WorkspaceContentRevision,
			ConfigurationRevision:    task.Revision, SnapshotIdentity: SnapshotIdentity,
			TaskFeatures: databaseToolbarTaskFeatureWire{
				Revision: task.Revision, Subitems: json.RawMessage(task.Subitems),
				Dependencies: json.RawMessage(task.Dependencies), Tasks: json.RawMessage(task.Tasks),
			},
			Templates: databaseToolbarTemplateMetadataWires(templates), HasMore: hasMore,
		}
		if hasMore && len(templates) > 0 {
			last := templates[len(templates)-1]
			next, err := encodeDatabaseToolbarConfigCursor(databaseToolbarConfigCursor{
				PrincipalID: principalID, WorkspaceID: authority.WorkspaceID,
				PageID: pageID, DataSourceID: dataSourceID,
				WorkspaceContentRevision: WorkspaceContentRevision,
				Position:                 last.Position, TemplateID: last.ID, Limit: limit,
			})
			if err != nil {
				return err
			}
			response.NextCursor = &next
		}
		return nil
	})
	return response, err
}

func loadDatabaseToolbarTaskFeatures(
	app core.App,
	authority databaseToolbarConfigAuthority,
) (storedDatabaseToolbarTaskFeatures, error) {
	task := storedDatabaseToolbarTaskFeatures{Subitems: "null", Dependencies: "null", Tasks: "null"}
	// Dependencies name the relation pair they are bound to, which turning
	// them off removes or keeps; the binding owns the pair, not the settings.
	err := app.DB().NewQuery(`SELECT [[features]].[[authority_revision]], [[features]].[[subitems_json]],
			CASE WHEN json_type([[features]].[[dependencies_json]]) = 'object' AND [[binding]].[[data_source_id]] IS NOT NULL
				THEN json_set([[features]].[[dependencies_json]],
					'$.successorPropertyId', [[binding]].[[successor_property_id]],
					'$.predecessorPropertyId', [[binding]].[[predecessor_property_id]])
				ELSE [[features]].[[dependencies_json]] END AS [[dependencies_json]],
			[[features]].[[tasks_json]]
		FROM {{leafen_database_task_features}} AS [[features]]
		LEFT JOIN {{leafen_database_dependency_bindings}} AS [[binding]]
			ON [[binding]].[[data_source_id]] = [[features]].[[data_source_id]]
		WHERE [[features]].[[data_source_id]]={:dataSourceId}
			AND [[features]].[[owner]]={:owner} AND [[features]].[[workspace_id]]={:workspaceId}
			AND [[features]].[[page_id]]={:pageId}`).
		Bind(dbx.Params{"dataSourceId": authority.DataSourceID, "owner": authority.OwnerID,
			"workspaceId": authority.WorkspaceID, "pageId": authority.PageID}).One(&task)
	if errors.Is(err, sql.ErrNoRows) {
		return task, nil
	}
	return task, err
}

func loadDatabaseToolbarTemplateMetadata(
	app core.App,
	authority databaseToolbarConfigAuthority,
	cursor databaseToolbarConfigCursor,
	limit int,
) ([]storedDatabaseToolbarTemplate, error) {
	templates := []storedDatabaseToolbarTemplate{}
	err := app.DB().NewQuery(`SELECT [[template]].[[id]], [[template]].[[name]], [[template]].[[icon]],
		[[template]].[[title]], [[template]].[[properties_json]], [[template]].[[property_spans_json]],
		[[template]].[[is_default]],
		[[template]].[[position]], COALESCE([[state]].[[authority_revision]],1) AS [[authority_revision]],
		COALESCE([[state]].[[deleted_at]],'') AS [[deleted_at]], [[template]].[[updated_at]],
		COALESCE([[state]].[[repeat_json]],'') AS [[repeat_json]],
		COALESCE([[state]].[[repeat_next_run_at]],0) AS [[repeat_next_run_at]],
		COALESCE([[state]].[[repeat_paused_reason]],'') AS [[repeat_paused_reason]],
		EXISTS (
			SELECT 1 FROM {{leafen_blocks}} [[content_block]] INDEXED BY [[idx_leafen_blocks_page_order]]
			WHERE [[content_block]].[[owner]] = [[template]].[[owner]] AND [[content_block]].[[page_id]] = [[template]].[[id]]
		) AS [[has_content]]
		FROM {{leafen_database_templates}} [[template]] INDEXED BY [[idx_leafen_database_templates_toolbar_window]]
		LEFT JOIN {{leafen_database_toolbar_template_state}} [[state]]
			ON [[state]].[[template_id]]=[[template]].[[id]]
		WHERE [[template]].[[owner]]={:owner} AND [[template]].[[workspace_id]]={:workspaceId}
			AND [[template]].[[page_id]]={:pageId} AND [[template]].[[data_source_id]]={:dataSourceId}
			AND ([[template]].[[position]]>{:position}
				OR ([[template]].[[position]]={:position} AND [[template]].[[id]]>{:templateId}))
		ORDER BY [[template]].[[position]], [[template]].[[id]] LIMIT {:limit}`).Bind(dbx.Params{
		"owner": authority.OwnerID, "workspaceId": authority.WorkspaceID,
		"pageId": authority.PageID, "dataSourceId": authority.DataSourceID,
		"position": cursor.Position, "templateId": cursor.TemplateID,
		"limit": limit + 1,
	}).All(&templates)
	return templates, err
}

func databaseToolbarTemplateMetadataWires(
	templates []storedDatabaseToolbarTemplate,
) []databaseToolbarTemplateMetadataWire {
	wires := make([]databaseToolbarTemplateMetadataWire, 0, len(templates))
	for _, template := range templates {
		repeat, repeatNextRunAt, repeatPausedReason := databaseTemplateRepeatMetadata(template)
		var spans json.RawMessage
		if template.PropertySpans != "" && template.PropertySpans != "{}" {
			spans = json.RawMessage(template.PropertySpans)
		}
		wires = append(wires, databaseToolbarTemplateMetadataWire{
			ID: template.ID, Name: template.Name, Icon: template.Icon, Title: template.Title,
			Properties: json.RawMessage(template.Properties), PropertySpans: spans, IsDefault: template.IsDefault,
			Position: template.Position, AuthorityRevision: template.AuthorityRevision,
			Deleted: template.DeletedAt != "", UpdatedAt: template.UpdatedAt,
			Repeat: repeat, RepeatNextRunAt: repeatNextRunAt, RepeatPausedReason: repeatPausedReason,
			HasContent: template.HasContent,
		})
	}
	return wires
}

func databaseToolbarSnapshotIdentity(
	authority databaseToolbarConfigAuthority,
	workspaceRevision, configurationRevision int,
) string {
	value := strings.Join([]string{authority.WorkspaceID, authority.PageID, authority.DataSourceID,
		strconv.Itoa(workspaceRevision), strconv.Itoa(configurationRevision)}, "\x00")
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func decodeDatabaseToolbarConfigCursor(
	raw, principalID string,
	authority databaseToolbarConfigAuthority,
	revision, limit int,
) (databaseToolbarConfigCursor, error) {
	empty := databaseToolbarConfigCursor{PrincipalID: principalID, WorkspaceID: authority.WorkspaceID,
		PageID: authority.PageID, DataSourceID: authority.DataSourceID,
		WorkspaceContentRevision: revision, Limit: limit}
	if strings.TrimSpace(raw) == "" {
		return empty, nil
	}
	cursorRefused := databaseToolbarRefused("cursor isn't one this list gave for this database, person and limit: leave it out to read from the start")
	if len(raw) > databaseToolbarConfigCursorBytes {
		return databaseToolbarConfigCursor{}, cursorRefused
	}
	encoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(encoded) > databaseToolbarConfigCursorBytes || json.Unmarshal(encoded, &empty) != nil {
		return databaseToolbarConfigCursor{}, cursorRefused
	}
	if empty.PrincipalID != principalID || empty.WorkspaceID != authority.WorkspaceID ||
		empty.PageID != authority.PageID || empty.DataSourceID != authority.DataSourceID || empty.Limit != limit {
		return databaseToolbarConfigCursor{}, cursorRefused
	}
	if empty.WorkspaceContentRevision != revision {
		return databaseToolbarConfigCursor{}, errDatabaseToolbarConfigRevisionChanged
	}
	return empty, nil
}

func encodeDatabaseToolbarConfigCursor(cursor databaseToolbarConfigCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil || len(encoded) > databaseToolbarConfigCursorBytes {
		return "", errDatabaseToolbarConfigInvalid
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func readDatabaseToolbarTemplateBody(event *core.RequestEvent) error {
	user, ok, err := databaseToolbarConfigActor(event)
	if !ok {
		return err
	}
	pageID, dataSourceID, templateID := databaseToolbarConfigIdentity(event)
	expected, parseErr := strconv.Atoi(event.Request.URL.Query().Get("revision"))
	if pageID == "" || dataSourceID == "" || templateID == "" {
		return writeDatabaseToolbarConfigError(event, databaseToolbarRefused("the page, data source and template IDs in the path are empty"))
	}
	if parseErr != nil || expected < 1 {
		return writeDatabaseToolbarConfigError(event, databaseToolbarRefused("revision is the template's revision as the settings list it, 1 or more"))
	}
	response, loadErr := loadDatabaseToolbarTemplateBody(event.App, user.Id, pageID, dataSourceID, templateID, expected)
	if loadErr != nil {
		return writeDatabaseToolbarConfigError(event, loadErr)
	}
	return event.JSON(http.StatusOK, response)
}

func loadDatabaseToolbarTemplateBody(
	app core.App,
	principalID, pageID, dataSourceID, templateID string,
	expectedRevision int,
) (databaseToolbarTemplateBodyResponse, error) {
	response := databaseToolbarTemplateBodyResponse{}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		authority, err := authorizeDatabaseToolbarConfig(
			txApp, principalID, pageID, dataSourceID, permissionWorkspaceRead,
		)
		if err != nil {
			return err
		}
		template := storedDatabaseToolbarTemplate{}
		err = txApp.DB().NewQuery(`SELECT [[template]].[[id]],
			COALESCE([[state]].[[authority_revision]],1) AS [[authority_revision]],
			COALESCE([[state]].[[deleted_at]],'') AS [[deleted_at]]
			FROM {{leafen_database_templates}} [[template]]
			LEFT JOIN {{leafen_database_toolbar_template_state}} [[state]]
				ON [[state]].[[template_id]]=[[template]].[[id]]
			WHERE [[template]].[[id]]={:templateId} AND [[template]].[[owner]]={:owner}
				AND [[template]].[[workspace_id]]={:workspaceId} AND [[template]].[[page_id]]={:pageId}
				AND [[template]].[[data_source_id]]={:dataSourceId}`).Bind(dbx.Params{
			"templateId": templateID, "owner": authority.OwnerID,
			"workspaceId": authority.WorkspaceID, "pageId": pageID, "dataSourceId": dataSourceID,
		}).One(&template)
		if errors.Is(err, sql.ErrNoRows) || template.DeletedAt != "" {
			return errDatabaseToolbarConfigNotFound
		}
		if err != nil {
			return err
		}
		if template.AuthorityRevision != expectedRevision {
			return errDatabaseToolbarConfigRevisionChanged
		}
		// The body is the template page's.
		blocks, err := loadDatabaseTemplatePageBody(txApp, templateID, false)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(blocks)
		if err != nil {
			return err
		}
		if len(encoded) > databaseToolbarConfigMaxTemplateBodyBytes {
			return errDatabaseToolbarConfigConflict
		}
		response = databaseToolbarTemplateBodyResponse{
			PageID: pageID, DataSourceID: dataSourceID, TemplateID: templateID,
			AuthorityRevision: template.AuthorityRevision,
			SnapshotIdentity:  databaseToolbarSnapshotIdentity(authority, expectedRevision, template.AuthorityRevision),
			Blocks:            json.RawMessage(encoded),
		}
		return nil
	})
	return response, err
}
