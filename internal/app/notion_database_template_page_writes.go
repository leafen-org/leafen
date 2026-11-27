package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A database template takes a connection's change as a page of its data
// source does, as Notion's templates do (its MCP's notion-update-page and its
// API's page update write a template page): its values read as a database
// page's are and kept as the template's own, with its title, as its editor
// keeps them (database_toolbar_config_template_mutation.go); its icon the
// template's; its cover and style its page record's, as its editor's "..."
// menu sets them (database_row_page_presentation.go). Options its values add
// come with them, as a database page's do.

// databaseTemplatePageWrite is a change of a template page: its values, read
// from the request against the template as a page of its data source (nil
// writes none), its icon and cover, and its style.
type databaseTemplatePageWrite struct {
	Properties   notionDatabaseRowPageProperties
	Presentation notionPagePresentationRequest
	Font         *string
	SmallText    *bool
	FullWidth    *bool
}

// authorizeNotionDatabaseTemplatePage reads a template as a page of its data
// source for a connection, with the role the template's editor needs to
// read or to change it.
func authorizeNotionDatabaseTemplatePage(
	app core.App, identity notionOAuthBearerIdentity, templateID string, role pageAccessRole,
) (notionDatabaseRowPageAuthorization, error) {
	templatePage, err := authorizeDatabaseTemplateBody(app, identity.PrincipalID, templateID, role)
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, err
	}
	if templatePage.Workspace.WorkspaceID != identity.WorkspaceID || !notionOAuthIdentityAllowsPage(app, identity, templatePage) {
		return notionDatabaseRowPageAuthorization{}, permissionDenied()
	}
	authority, err := authorizeDatabaseTemplatePage(app, identity.PrincipalID, templateID, role)
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, err
	}
	container, err := loadNotionDataSourceParentAuthority(app, identity.WorkspaceID, authority.DataSourceID)
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, err
	}
	// Its values name pages and people from its database's page, as a
	// database page's do.
	page, err := authorizePageBody(app, identity.PrincipalID, container.PageID, permissionWorkspaceRead)
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, err
	}
	return notionDatabaseRowPageAuthorization{Identity: identity, Authority: authority, Container: container, Page: page}, nil
}

// writeDatabaseTemplatePage applies a change of a template page as one
// change: with the options its values add as a content graph change of its
// database's page, else alone.
func writeDatabaseTemplatePage(
	ctx context.Context, app core.App, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	templateID string, write databaseTemplatePageWrite,
) error {
	if err := validateNotionPagePresentationReferences(app, identity.PrincipalID, identity.WorkspaceID, &write.Presentation); err != nil {
		return err
	}
	plan := func(txApp core.App) (notionDatabaseRowPageAuthorization, map[string]graphRowValue, []graphOperation, error) {
		current, err := authorizeNotionDatabaseTemplatePage(txApp, identity, templateID, pageAccessRoleEdit)
		if err != nil || write.Properties == nil {
			return current, nil, nil, err
		}
		requested, err := write.Properties(txApp, current)
		if err != nil {
			return current, nil, nil, err
		}
		values, options, err := planNotionDatabaseRowPageRequestValues(txApp, current, requested, false)
		return current, values, options, err
	}
	current, _, options, err := plan(app)
	if err != nil {
		return err
	}
	apply := func(txApp core.App) error {
		defer namePageHistoryConnection(txApp, ctx)()
		current, values, _, err := plan(txApp)
		if err != nil {
			return err
		}
		return applyDatabaseTemplatePageWrite(txApp, current, values, write)
	}
	if len(options) == 0 {
		return app.RunInTransaction(apply)
	}
	if ygo == nil {
		return errDatabaseRowPageConflict
	}
	_, err = ygo.applyContentGraphMutationContext(
		ctx, identity.PrincipalID, current.Container.PageID,
		graphMutationInput{MutationID: "template-page-values:" + newDiagnosticReference()},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				if _, err := authorizeNotionDatabaseTemplatePage(txApp, identity, templateID, pageAccessRoleEdit); err != nil {
					return pageAuthorization{}, err
				}
				return authorizePageBody(txApp, identity.PrincipalID, current.Container.PageID, permissionWorkspaceWrite)
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				_, _, options, err := plan(txApp)
				return contentGraphMutationPreparation{Operations: options}, err
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				return apply(txApp)
			},
		},
	)
	return err
}

// applyDatabaseTemplatePageWrite keeps a template page's values, icon,
// cover and style.
func applyDatabaseTemplatePageWrite(
	app core.App, current notionDatabaseRowPageAuthorization, values map[string]graphRowValue, write databaseTemplatePageWrite,
) error {
	actorID, templateID, workspaceID := current.Identity.PrincipalID, current.Authority.RowID, current.Authority.WorkspaceID
	if locked, err := databaseRowPageLockedFor(app, current.Authority, actorID); err != nil {
		return err
	} else if locked {
		return pageLockedError()
	}
	toolbar, err := authorizeDatabaseToolbarConfig(
		app, actorID, current.Container.PageID, current.Container.DataSource, permissionWorkspaceWrite,
	)
	if err != nil {
		return err
	}
	template, err := databaseToolbarTemplateForMutation(app, toolbar, templateID)
	if err != nil {
		return err
	}
	operation := databaseToolbarConfigMutationOperation{
		Kind: "updateTemplate", TemplateID: &templateID, ExpectedRevision: &template.AuthorityRevision,
	}
	if len(values) > 0 {
		properties, spans := map[string]json.RawMessage{}, map[string]json.RawMessage{}
		if json.Unmarshal([]byte(template.Properties), &properties) != nil ||
			template.PropertySpans != "" && json.Unmarshal([]byte(template.PropertySpans), &spans) != nil {
			return errDatabaseToolbarConfigConflict
		}
		for propertyID, value := range values {
			if value.Type == graphValueTypeTitle {
				title := ""
				if err := json.Unmarshal(value.Value, &title); err != nil {
					return err
				}
				operation.Title = &title
				continue
			}
			delete(spans, propertyID)
			if len(value.Value) == 0 || string(value.Value) == "null" {
				delete(properties, propertyID)
				continue
			}
			properties[propertyID] = value.Value
			if len(value.Spans) > 0 {
				encoded, err := json.Marshal(value.Spans)
				if err != nil {
					return err
				}
				spans[propertyID] = encoded
			}
		}
		if operation.Properties, err = json.Marshal(properties); err != nil {
			return err
		}
		if operation.PropertySpans, err = json.Marshal(spans); err != nil {
			return err
		}
	}
	if icon := write.Presentation; icon.Icon != nil && icon.IconType != nil {
		value, err := databaseTemplateIconOfPage(app, workspaceID, *icon.Icon, *icon.IconType)
		if err != nil {
			return err
		}
		operation.Icon = &value
	}
	if operation.Title != nil || len(operation.Properties) > 0 || operation.Icon != nil {
		if !validDatabaseToolbarTemplatePatch(operation) {
			return &notionDatabaseRowPageRequestError{message: "the template's title, values or icon are too long"}
		}
		state := databaseToolbarMutationState{ActorID: actorID}
		if err := applyDatabaseToolbarOperation(app, toolbar, operation, &state); err != nil {
			return err
		}
	}
	if cover := write.Presentation.Cover; cover != nil {
		found, err := setRowPageRecordCover(app, templateID, workspaceID, actorID, *cover, nil)
		if err != nil {
			return err
		}
		if !found {
			return errDatabaseRowPageConflict
		}
		if err := replaceProductFilePageReferences(app, workspaceID, templateID, "", "none", *cover); err != nil {
			return err
		}
	}
	return setDatabaseRowPageStyle(app, templateID, workspaceID, actorID, write.Font, write.SmallText, write.FullWidth)
}

// isDatabaseTemplateWriteRefusal reports a template change refused for the
// template's state: changed meanwhile, or gone.
func isDatabaseTemplateWriteRefusal(err error) bool {
	return errors.Is(err, errDatabaseToolbarConfigConflict) || errors.Is(err, errDatabaseToolbarConfigNotFound) ||
		errors.Is(err, errDatabaseRowPageConflict)
}
