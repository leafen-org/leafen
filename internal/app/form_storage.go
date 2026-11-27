package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type storedFormLink struct {
	ID           string `db:"id"`
	Owner        string `db:"owner"`
	WorkspaceID  string `db:"workspace_id"`
	PageID       string `db:"page_id"`
	DataSourceID string `db:"data_source_id"`
	ViewID       string `db:"view_id"`
	TokenDigest  string `db:"token_digest"`
	Audience     string `db:"audience"`
	Enabled      bool   `db:"enabled"`
	// SubmitterAccess is what a signed-in respondent may do on the page
	// their response creates.
	SubmitterAccess string `db:"submitter_access"`
	CreatedBy       string `db:"created_by"`
	CreatedAt       string `db:"created_at"`
	UpdatedAt       string `db:"updated_at"`
}

type preparedFormConfiguration struct {
	Source formSource
	Link   storedFormLink
}

func normalizeFormConfigureRequest(input formConfigureRequest) (formConfigureRequest, error) {
	var err error
	for _, field := range []struct {
		name  string
		value *string
	}{{"mutationId", &input.MutationID}, {"pageId", &input.PageID}, {"dataSourceId", &input.DataSourceID}, {"viewId", &input.ViewID}} {
		if *field.value, err = formIdentifierField(field.name, *field.value); err != nil {
			return formConfigureRequest{}, err
		}
	}
	switch input.Audience {
	case formAudienceNone, formAudienceWorkspace, formAudienceWeb:
	default:
		return formConfigureRequest{}, formInvalid("audience is %s, %s or %s", formAudienceNone, formAudienceWorkspace, formAudienceWeb)
	}
	// No setting keeps the one the form has.
	if _, valid := formSubmitterAccessRole(input.SubmitterAccess); input.SubmitterAccess != "" && !valid {
		return formConfigureRequest{}, formInvalid("submitterAccess is %s, %s, %s, %s or %s, or left out to keep the form's",
			formSubmitterAccessNone, formSubmitterAccessReader, formSubmitterAccessCommentOnly, formSubmitterAccessReadAndWrite,
			formSubmitterAccessEditor)
	}
	return input, nil
}

func loadStoredFormLinkByView(
	app core.App,
	owner, pageID, viewID string,
) (storedFormLink, bool, error) {
	rows := []storedFormLink{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[workspace_id]], [[page_id]], [[data_source_id]], [[view_id]],
			[[token_digest]], [[audience]], [[enabled]], [[submitter_access]], [[created_by]], [[created_at]], [[updated_at]]
		FROM {{leafen_form_links}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[view_id]] = {:viewId}
		LIMIT 2
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "viewId": viewID}).All(&rows)
	if err != nil {
		return storedFormLink{}, false, err
	}
	if len(rows) > 1 {
		return storedFormLink{}, false, errFormConflict
	}
	if len(rows) == 0 {
		return storedFormLink{}, false, nil
	}
	return rows[0], true, nil
}

func prepareFormConfiguration(
	app core.App,
	principalID string,
	input formConfigureRequest,
) (preparedFormConfiguration, error) {
	prepared := preparedFormConfiguration{}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		authorization, err := authorizePageBody(txApp, principalID, input.PageID, permissionWorkspaceWrite)
		if isPermissionDenied(err) {
			return errFormNotFound
		}
		if err != nil {
			return err
		}
		prepared.Source, err = loadFormSource(
			txApp, authorization.Workspace.OwnerID, input.PageID,
			input.DataSourceID, input.ViewID, input.Audience, &input.Form,
		)
		if err != nil {
			return err
		}
		link, found, err := loadStoredFormLinkByView(
			txApp, authorization.Workspace.OwnerID, input.PageID, input.ViewID,
		)
		if err != nil {
			return err
		}
		if found && (link.WorkspaceID != authorization.Workspace.WorkspaceID ||
			link.DataSourceID != input.DataSourceID || link.CreatedBy == "") {
			return errFormConflict
		}
		if !found {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			link = storedFormLink{
				ID: core.GenerateDefaultRandomId(), Owner: authorization.Workspace.OwnerID,
				WorkspaceID: authorization.Workspace.WorkspaceID, PageID: input.PageID,
				DataSourceID: input.DataSourceID, ViewID: input.ViewID,
				CreatedBy: principalID, CreatedAt: now,
			}
		}
		if input.SubmitterAccess != "" {
			link.SubmitterAccess = input.SubmitterAccess
		}
		prepared.Link = link
		return nil
	})
	return prepared, err
}

func currentFormViewSnapshot(app core.App, pageID string, source formSource) (databaseViewAuthority, bool) {
	current, exists, err := loadDatabaseViewRecord(app, pageID, source.View.ID)
	if err != nil || !exists {
		return databaseViewAuthority{}, false
	}
	return current, current.DataSourceID == source.View.DataSourceID &&
		current.Type == source.View.Type && current.Name == source.View.Name &&
		reflect.DeepEqual(current.Config, source.ViewConfig)
}

func saveFormLink(
	app core.App,
	link storedFormLink,
	tokenDigest, audience string,
) error {
	enabled := audience != formAudienceNone
	// Opening a form to anyone with its link publishes it to the web.
	if audience == formAudienceWeb && link.Audience != formAudienceWeb {
		workspace, err := app.FindRecordById(workspacesCollection, link.WorkspaceID)
		if err != nil {
			return err
		}
		if err := enforceSharingSecurityPublicSharing(app, workspace.GetString("organization")); err != nil {
			return err
		}
		if err := enforceTeamspacePublishing(app, link.PageID); err != nil {
			return err
		}
	}
	submitterAccess := link.SubmitterAccess
	if submitterAccess == "" {
		submitterAccess = formSubmitterAccessNone
	}
	if _, valid := formSubmitterAccessRole(submitterAccess); !valid {
		return errFormInvalid
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_form_links}}
		SET [[token_digest]] = {:tokenDigest}, [[audience]] = {:audience},
			[[enabled]] = {:enabled}, [[submitter_access]] = {:submitterAccess}, [[updated_at]] = {:updatedAt}
		WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
			AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:sourceId}
			AND [[view_id]] = {:viewId}
	`).Bind(dbx.Params{
		"id": link.ID, "owner": link.Owner, "workspaceId": link.WorkspaceID,
		"pageId": link.PageID, "sourceId": link.DataSourceID, "viewId": link.ViewID,
		"tokenDigest": tokenDigest, "audience": audience, "enabled": enabled,
		"submitterAccess": submitterAccess, "updatedAt": now,
	}).Execute()
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated == 1 {
		return nil
	}
	if updated != 0 {
		return errFormConflict
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_form_links}}
			([[id]], [[owner]], [[workspace_id]], [[page_id]], [[data_source_id]], [[view_id]],
			 [[token_digest]], [[audience]], [[enabled]], [[submitter_access]], [[created_by]], [[created_at]], [[updated_at]])
		VALUES
			({:id}, {:owner}, {:workspaceId}, {:pageId}, {:sourceId}, {:viewId},
			 {:tokenDigest}, {:audience}, {:enabled}, {:submitterAccess}, {:createdBy}, {:createdAt}, {:updatedAt})
	`).Bind(dbx.Params{
		"id": link.ID, "owner": link.Owner, "workspaceId": link.WorkspaceID,
		"pageId": link.PageID, "sourceId": link.DataSourceID, "viewId": link.ViewID,
		"tokenDigest": tokenDigest, "audience": audience, "enabled": enabled, "submitterAccess": submitterAccess,
		"createdBy": link.CreatedBy, "createdAt": link.CreatedAt, "updatedAt": now,
	}).Execute()
	return err
}

func configureForm(
	persistence *pocketBaseYgoPersistence,
	app core.App,
	principalID string,
	input formConfigureRequest,
) (formConfigureResponse, error) {
	input, err := normalizeFormConfigureRequest(input)
	if err != nil {
		return formConfigureResponse{}, err
	}
	prepared, err := prepareFormConfiguration(app, principalID, input)
	if err != nil {
		return formConfigureResponse{}, err
	}
	token, err := formToken(app, prepared.Link.ID)
	if err != nil {
		return formConfigureResponse{}, err
	}
	prepared.Link.TokenDigest = formTokenDigest(token)
	prepared.Link.Audience = input.Audience
	prepared.Link.Enabled = input.Audience != formAudienceNone

	nextConfig := make(map[string]any, len(prepared.Source.ViewConfig)+4)
	for key, value := range prepared.Source.ViewConfig {
		nextConfig[key] = value
	}
	nextConfig["type"] = graphViewTypeForm
	nextConfig["leafenForm"] = prepared.Source.Form
	nextConfig["leafenFormAudience"] = input.Audience
	nextConfig["leafenFormSubmitterAccess"] = formSubmitterAccessOrNone(prepared.Link.SubmitterAccess)
	encodedConfig, err := json.Marshal(nextConfig)
	if err != nil {
		return formConfigureResponse{}, err
	}
	operation := graphOperation{
		Kind: graphOperationKindUpsertView,
		View: &graphDatabaseView{
			ID: prepared.Source.View.ID, DataSourceID: prepared.Source.View.DataSourceID,
			Type: prepared.Source.View.Type, Name: prepared.Source.View.Name,
			Position: prepared.Source.View.Position, Config: encodedConfig,
		},
	}
	receipt, err := persistence.applyContentGraphMutation(
		principalID, input.PageID,
		graphMutationInput{MutationID: input.MutationID, Operations: []graphOperation{operation}},
		contentGraphMutationHooks{
			EnforcePageLock: true,
			BeforeApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				current, matches := currentFormViewSnapshot(txApp, input.PageID, prepared.Source)
				if !matches {
					return errFormConflict
				}
				// The rewrite keeps the database that owns the view.
				operation.View.Position = current.Position
				operation.View.ContainerID = current.ContainerID
				return nil
			},
			AfterApply: func(txApp core.App, authorization pageAuthorization, _ *crdt.Doc) error {
				if authorization.Workspace.OwnerID != prepared.Link.Owner ||
					authorization.Workspace.WorkspaceID != prepared.Link.WorkspaceID {
					return errFormForbidden
				}
				return saveFormLink(txApp, prepared.Link, prepared.Link.TokenDigest, input.Audience)
			},
		},
	)
	if err != nil {
		return formConfigureResponse{}, err
	}
	result, err := readConfiguredForm(app, principalID, input.PageID, input.ViewID)
	if err != nil {
		return formConfigureResponse{}, err
	}
	result.Replayed = receipt.Duplicate
	return result, nil
}

func readConfiguredForm(
	app core.App,
	principalID, pageID, viewID string,
) (formConfigureResponse, error) {
	response := formConfigureResponse{}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		authorization, err := authorizePageBody(txApp, principalID, pageID, permissionWorkspaceWrite)
		if isPermissionDenied(err) {
			return errFormNotFound
		}
		if err != nil {
			return err
		}
		link, found, err := loadStoredFormLinkByView(txApp, authorization.Workspace.OwnerID, pageID, viewID)
		if err != nil || !found {
			if err != nil {
				return err
			}
			return errFormConflict
		}
		source, err := loadFormSource(
			txApp, link.Owner, link.PageID, link.DataSourceID, link.ViewID, link.Audience, nil,
		)
		if err != nil {
			return err
		}
		revision, err := formDefinitionRevision(link, source)
		if err != nil {
			return err
		}
		token, err := formToken(txApp, link.ID)
		if err != nil {
			return err
		}
		if !strings.EqualFold(link.TokenDigest, formTokenDigest(token)) {
			return errFormConflict
		}
		response = formConfigureResponse{
			Form: source.Form,
			FormLink: formLinkResponse{
				ID: link.ID, PageID: link.PageID, DataSourceID: link.DataSourceID,
				ViewID: link.ViewID, Token: token, Audience: link.Audience, Enabled: link.Enabled,
				SubmitterAccess: formSubmitterAccessOrNone(link.SubmitterAccess),
			},
			Revision: revision,
		}
		return nil
	})
	return response, err
}

func formSubmitterAccessOrNone(access string) string {
	if access == "" {
		return formSubmitterAccessNone
	}
	return access
}
