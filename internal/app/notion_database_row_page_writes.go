package app

import (
	"context"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A database page's property values are written the same way whoever asks:
// the Public API reads them from its request and MCP from its SQLite values,
// and both hand them to the row page planner inside the change, where the
// data source's current properties decide them.

// notionDatabaseRowPageProperties reads a request's property values as the
// Public API's property objects, against the row page or data source as it is
// inside the change.
type notionDatabaseRowPageProperties func(core.App, notionDatabaseRowPageAuthorization) (notionRequestProperties, error)

// writeNotionDatabaseRowPageValues writes a row page's property values as one
// content graph change; afterApply, when given, runs in its transaction and
// writes the row's own page alone (its look, archive, lock or style).
func writeNotionDatabaseRowPageValues(
	ctx context.Context,
	ygo *pocketBaseYgoPersistence,
	identity notionOAuthBearerIdentity,
	authority databaseRowPageAuthority,
	mutationID string,
	properties notionDatabaseRowPageProperties,
	afterApply func(core.App) error,
) error {
	// The sub-pages hanging on the cells it writes follow them, as a
	// person's or an integration's write leaves them (fieldAnchorsKept).
	cells := []pageFieldAnchor{}
	_, err := ygo.applyContentGraphMutationContext(
		ctx, identity.PrincipalID, authority.ContainerPageID,
		graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				current, err := authorizeNotionDatabaseRowPageCurrent(
					txApp, identity, authority.RowID, permissionWorkspaceWrite,
				)
				if err != nil || current.Authority.ContainerPageID != authority.ContainerPageID {
					return pageAuthorization{}, err
				}
				return current.Page, nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, err := authorizeNotionDatabaseRowPageCurrent(
					txApp, identity, authority.RowID, permissionWorkspaceWrite,
				)
				if err != nil || current.Authority.ContainerPageID != authority.ContainerPageID {
					return contentGraphMutationPreparation{}, err
				}
				requested, err := properties(txApp, current)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				// Properties that name no value to write (a formula's, a
				// created time's) end the change; the caller writes the rest.
				if requested.count() == 0 {
					return contentGraphMutationPreparation{}, errNotionDatabaseRowPageValuesKept
				}
				values, optionOperations, err := planNotionDatabaseRowPageRequestValues(
					txApp, current, requested, false,
				)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				if len(values) == 0 {
					return contentGraphMutationPreparation{}, errNotionDatabaseRowPageProjectionInvalid
				}
				row := graphOperation{
					Kind: graphOperationKindUpsertRow,
					Row: &graphDatabaseRow{
						ID:              current.Authority.RowID,
						DataSourceID:    current.Authority.DataSourceID,
						SubitemParentID: optionalDatabaseRowParent(current.Authority.SubitemParentID),
						Position:        current.Authority.Position, Values: values,
					},
				}
				cells = rowCellFieldAnchors([]graphOperation{row})
				return contentGraphMutationPreparation{Operations: append(optionOperations, row)}, nil
			},
			// Its steps settle the sub-pages on the cells it writes, pages of
			// their own, and write the row's own page, which a read of the row
			// takes in: the change says the row (StepsKeepGraph).
			StepsKeepGraph: true,
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				if err := settleFieldAnchoredPages(
					txApp, identity.PrincipalID, mutationID+":cells", cells, fieldAnchorsKept(txApp),
				); err != nil || afterApply == nil {
					return err
				}
				return afterApply(txApp)
			},
		},
	)
	return err
}

// notionDatabaseRowPageCreation is a new row page in a data source: its
// values, from a template when one applies and then the request, its icon
// and cover, and its body.
type notionDatabaseRowPageCreation struct {
	Parent       notionDatabaseRowPageAuthorization
	RowID        string
	MutationID   string
	Template     notionPageTemplateRequest
	Content      notionPageCreateContentPlan
	Presentation notionPagePresentationRequest
	Properties   notionDatabaseRowPageProperties
	// InTrash is an item made in a data source in the trash, or under a
	// page in it: in the trash with it, as Notion makes one (seen
	// 2026-11-11).
	InTrash bool
}

// createNotionDatabaseRowPage makes a row page at the end of its data source
// as one content graph change, with the options its values add.
func createNotionDatabaseRowPage(
	ctx context.Context,
	ygo *pocketBaseYgoPersistence,
	creation notionDatabaseRowPageCreation,
) error {
	parent, template, rowID := creation.Parent, creation.Template, creation.RowID
	// A template_id naming another page the connection reaches makes that
	// page the template (notion_page_template_copy.go).
	source, fromPage, err := notionPageCreateTemplateSource(ygo.app, parent, template)
	if err != nil {
		return err
	}
	dataSourceTemplate := template.applies() && !fromPage
	var applied *storedDatabaseToolbarTemplate
	// One use of the template gives its values and its body their value.
	var use databaseTemplateUse
	_, err = ygo.applyContentGraphMutationContext(
		ctx, parent.Identity.PrincipalID, parent.Container.PageID,
		graphMutationInput{MutationID: creation.MutationID},
		contentGraphMutationHooks{
			AllowTrashedPage: creation.InTrash,
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				current, err := authorizeNotionDataSourceItemParentReach(
					txApp, parent.Identity, parent.Container.DataSource, creation.InTrash,
				)
				if err != nil || current.Container.PageID != parent.Container.PageID {
					return pageAuthorization{}, err
				}
				return current.Page, nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, err := authorizeNotionDataSourceItemParentReach(
					txApp, parent.Identity, parent.Container.DataSource, creation.InTrash,
				)
				if err != nil || current.Container.PageID != parent.Container.PageID {
					return contentGraphMutationPreparation{}, err
				}
				// A template's values come first and the request's properties
				// replace them.
				values := map[string]graphRowValue{}
				if fromPage {
					if values, err = notionPageTemplateRowValues(
						txApp, parent.Identity.PrincipalID, source, current.Container.DataSource,
					); err != nil {
						return contentGraphMutationPreparation{}, err
					}
				}
				if dataSourceTemplate {
					stored, authority, err := notionPageCreateTemplate(txApp, current, template)
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					if use, err = newDatabaseTemplateUse(txApp, parent.Identity.PrincipalID, notionTemplateZone(parent.Identity, template.Timezone)); err != nil {
						return contentGraphMutationPreparation{}, err
					}
					if values, err = databaseToolbarTemplateRowValues(txApp, authority, stored, use); err != nil {
						return contentGraphMutationPreparation{}, refusedUnlessStorage(errDatabaseRowPageConflict, err)
					}
					applied = &stored
				}
				properties, err := creation.Properties(txApp, current)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				requested, optionOperations, err := planNotionDatabaseRowPageRequestValues(
					txApp, current, properties, !dataSourceTemplate,
				)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				for propertyID, value := range requested {
					values[propertyID] = value
				}
				// The page goes after the database's last, as one added in its
				// table does.
				position, err := databaseRowPositionAtEnd(
					txApp, current.Page.Workspace.OwnerID, current.Container.PageID, current.Container.DataSource,
				)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				// The options the values add come with them, as Notion adds them.
				return contentGraphMutationPreparation{Operations: append(optionOperations, graphOperation{
					Kind: graphOperationKindUpsertRow,
					Row: &graphDatabaseRow{
						ID: rowID, DataSourceID: current.Container.DataSource,
						Position: position, Values: values,
					},
				})}, nil
			},
			// Its steps note the mentions to tell of and give the new row's own
			// page its look, which a read of the row takes in, its body and
			// its history, that page's own: its change says the row
			// (StepsKeepGraph).
			StepsKeepGraph: true,
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				if !dataSourceTemplate {
					if err := expectPagePersonMentions(txApp, rowID, time.Now().UTC().UnixMilli()); err != nil {
						return err
					}
				}
				if applied != nil {
					if err := applyDatabaseToolbarTemplateRowPresentation(txApp, *applied, rowID); err != nil {
						return refusedUnlessStorage(errDatabaseRowPageConflict, err)
					}
				}
				// The request's own icon and cover replace the template page's.
				if err := applyNotionPageTemplateRowLook(txApp, source, rowID); err != nil {
					return err
				}
				if err := applyNotionDatabaseRowPagePresentation(
					txApp, rowID, parent.Container.WorkspaceID, parent.Container.PageID,
					parent.Identity.PrincipalID, creation.Presentation,
				); err != nil {
					return err
				}
				// A template the Public API or MCP applies takes its synced block
				// originals as new ones, as Notion's (seen 2026-11-14).
				if applied != nil {
					if err := applyDatabaseToolbarTemplateRowBody(
						txApp, *applied, rowID, parent.Container.WorkspaceID, use, rowPageBodySyncedNew,
					); err != nil {
						return err
					}
				}
				if len(creation.Content.Operations) > 0 || creation.Content.ContentRefused != nil {
					current, err := authorizeNotionDatabaseRowPageCurrent(
						txApp, parent.Identity, rowID, permissionWorkspaceWrite,
					)
					if err != nil {
						return err
					}
					if err := applyNotionDatabaseRowPageCreateContent(
						txApp, current, creation.Content, parent.Identity.PrincipalID,
					); err != nil {
						return err
					}
				}
				// A database page made through a connection starts its history
				// naming it, as a page made so does (ensureCreatedPageHistoryBaseline),
				// so its first version and its menus' last edit say who made it.
				return ensurePageHistoryBaselineAs(txApp, rowID, pageHistoryConnectionOf(txApp))
			},
		},
	)
	if err != nil {
		return err
	}
	// A page made in a wiki sits in the body of the wiki's page at its end,
	// as one a person adds to the wiki's view does (wiki_page_links.go), and
	// as Notion's API makes one (notion_page_body_wiki_items.go).
	if err := ygo.keepWikiPageLinks(ctx, parent.Identity.PrincipalID, []string{rowID}); err != nil {
		ygo.app.Logger().Warn("leafen wiki page link not kept", "page", rowID, "error", err)
	}
	if !fromPage {
		return nil
	}
	// The template page's body follows once the page is there.
	return copyNotionPageTemplateBody(ctx, ygo.app, parent.Identity.PrincipalID, source, rowID)
}
