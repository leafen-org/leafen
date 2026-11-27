package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A template applied to a database page that already exists, as Notion's
// Public API page update (template, erase_content) and its MCP
// apply_template do (seen 2026-09-30): the template's values fill the
// properties the page leaves empty and its icon a page without one, the
// page keeps its title and its values, and the template's body follows the
// page's own. erase_content takes the page's body away first. Notion
// finishes applying after it answers; Leafen answers with it applied.

type notionDatabaseRowPageTemplateApplication struct {
	Template notionPageTemplateRequest
	Erase    bool
	// Properties are the request's own values, which win over the
	// template's.
	Properties notionRequestProperties
	// Synced is how the template's synced block originals come: new ones
	// for the Public API and MCP, copies of them for the app, as Notion's
	// (seen 2026-11-14).
	Synced rowPageBodySynced
	// WithoutBody leaves the template's body out: the app's row made on a
	// device that sent the body itself (database_row_template_apply.go).
	WithoutBody bool
}

// errNotionDatabaseRowPageValuesKept ends a values change that has nothing
// to write: the page already holds every value the template has.
var errNotionDatabaseRowPageValuesKept = errors.New("database page values kept")

// applyNotionDatabaseRowPageTemplate applies a template to a database page,
// or erases its body, as one request asks: its values and icon first, where
// the request's own values are checked, then its body.
func applyNotionDatabaseRowPageTemplate(
	ctx context.Context,
	app core.App,
	ygo *pocketBaseYgoPersistence,
	identity notionOAuthBearerIdentity,
	rowID string,
	application notionDatabaseRowPageTemplateApplication,
) error {
	current, err := authorizeNotionDatabaseRowPageCurrent(app, identity, rowID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	// A template_id naming another page the connection reaches makes that
	// page the template (notion_page_template_copy.go).
	source, fromPage, err := notionPageCreateTemplateSource(app, current, application.Template)
	if err != nil {
		return err
	}
	applies := application.Template.applies() && !fromPage
	use := databaseTemplateUse{}
	body, views := []newRowPageBlock(nil), map[string][]graphDatabaseView{}
	var databases []rowPageOwnDatabase
	var subPages []rowPageSubPageCopy
	templateID := ""
	if applies {
		stored, _, err := notionPageCreateTemplate(app, current, application.Template)
		if err != nil {
			return err
		}
		if use, err = newDatabaseTemplateUse(app, identity.PrincipalID, notionTemplateZone(identity, application.Template.Timezone)); err != nil {
			return err
		}
		templateID = stored.ID
		if !application.WithoutBody {
			body, views, databases, subPages, err = databaseTemplateBodyForExistingPage(app, stored, current.Authority, use, application.Synced)
			if err != nil {
				return err
			}
		}
	}
	mutationID := "notion-public-row-template:" + newDiagnosticReference()
	if applies || fromPage || application.Properties.count() > 0 {
		err := writeNotionDatabaseRowPageTemplateValues(
			ctx, ygo, identity, current.Authority, mutationID+":values", application, use, source, fromPage,
		)
		if err != nil && !errors.Is(err, errNotionDatabaseRowPageValuesKept) {
			return err
		}
	}
	if application.Erase {
		if err := eraseNotionDatabaseRowPageBody(ctx, app, ygo, identity, current.Authority, mutationID+":erase"); err != nil {
			return err
		}
	}
	if fromPage {
		return copyNotionPageTemplateBody(ctx, app, identity.PrincipalID, source, rowID)
	}
	if applies && len(body) == 0 && len(databases) == 0 && len(subPages) == 0 && !application.WithoutBody {
		if filled, err := rowPageHasBlocks(app, rowID); err != nil {
			return err
		} else if !filled {
			body = databaseTemplateEmptyLine()
		}
	}
	if err := appendNotionDatabaseRowPageBody(ctx, app, ygo, identity, current.Authority, mutationID, body, views); err != nil {
		return err
	}
	// Its sub-pages and own databases follow, copied into their places
	// (database_row_page_sub_page_copies.go, database_template_body_databases.go).
	return queueRowPageOwnDatabaseCopies(app, identity.PrincipalID, templateID, rowID, databases, subPages)
}

// writeNotionDatabaseRowPageTemplateValues writes the request's values and,
// in the properties the page leaves empty, the template's, and gives the page
// the template's look: its icon and cover where the page has none, its style.
// The template is the page source names when fromPage.
func writeNotionDatabaseRowPageTemplateValues(
	ctx context.Context,
	ygo *pocketBaseYgoPersistence,
	identity notionOAuthBearerIdentity,
	authority databaseRowPageAuthority,
	mutationID string,
	application notionDatabaseRowPageTemplateApplication,
	use databaseTemplateUse,
	source notionPageTemplateSource,
	fromPage bool,
) error {
	dataSourceTemplate := application.Template.applies() && !fromPage
	var stored *storedDatabaseToolbarTemplate
	_, err := ygo.applyContentGraphMutationContext(
		ctx, identity.PrincipalID, authority.ContainerPageID,
		graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				current, err := authorizeNotionDatabaseRowPageCurrent(txApp, identity, authority.RowID, permissionWorkspaceWrite)
				if err != nil || current.Authority.ContainerPageID != authority.ContainerPageID {
					return pageAuthorization{}, err
				}
				return current.Page, nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, err := authorizeNotionDatabaseRowPageCurrent(txApp, identity, authority.RowID, permissionWorkspaceWrite)
				if err != nil || current.Authority.ContainerPageID != authority.ContainerPageID {
					return contentGraphMutationPreparation{}, err
				}
				values := map[string]graphRowValue{}
				if application.Template.applies() {
					var offered map[string]graphRowValue
					if fromPage {
						if offered, err = notionPageTemplateRowValues(
							txApp, identity.PrincipalID, source, current.Authority.DataSourceID,
						); err != nil {
							return contentGraphMutationPreparation{}, err
						}
					} else {
						template, templateAuthority, err := notionPageCreateTemplate(txApp, current, application.Template)
						if err != nil {
							return contentGraphMutationPreparation{}, err
						}
						stored = &template
						if offered, err = databaseToolbarTemplateRowValues(txApp, templateAuthority, template, use); err != nil {
							return contentGraphMutationPreparation{}, refusedUnlessStorage(errDatabaseRowPageConflict, err)
						}
					}
					existing, err := loadDatabaseRowPageGraphValues(txApp, current.Authority)
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					for propertyID, value := range offered {
						if held, found := existing[propertyID]; found && !databaseRowPageMoveValueEmpty(held.Value) {
							continue
						}
						if databaseRowPageMoveValueEmpty(value.Value) {
							continue
						}
						values[propertyID] = value
					}
				}
				properties, err := notionRowRequestValues(txApp, current, application.Properties)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				requested, optionOperations, err := planNotionDatabaseRowPageRequestValues(
					txApp, current, properties, false,
				)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				for propertyID, value := range requested {
					values[propertyID] = value
				}
				if len(values) == 0 {
					return contentGraphMutationPreparation{}, errNotionDatabaseRowPageValuesKept
				}
				return contentGraphMutationPreparation{Operations: append(optionOperations, graphOperation{
					Kind: graphOperationKindUpsertRow,
					Row: &graphDatabaseRow{
						ID:              current.Authority.RowID,
						DataSourceID:    current.Authority.DataSourceID,
						SubitemParentID: optionalDatabaseRowParent(current.Authority.SubitemParentID),
						Position:        current.Authority.Position, Values: values,
					},
				})}, nil
			},
		},
	)
	if err != nil && !errors.Is(err, errNotionDatabaseRowPageValuesKept) {
		return err
	}
	if !fromPage && !dataSourceTemplate {
		return err
	}
	// The look is written after its values (ygo_persistence_follow_ups.go) and
	// told as a look the page is given elsewhere is: the views and peeks open
	// on it draw it as they draw its values and title, without reading again
	// (2026-11-25: a row filled from its template showed the default icon in
	// another tab's open table, and in the tab whose row went late, until
	// something else read it again).
	return ygo.runFollowUp(func(txApp core.App) error {
		if lookErr := writeTemplateRowLook(txApp, identity, authority.RowID, application.Template, source, fromPage, stored); lookErr != nil {
			return lookErr
		}
		return announceDatabaseRowPageLook(txApp, authority.RowID)
	})
}

// writeTemplateRowLook gives a page a template applied to it the template's
// look: the page source names when fromPage (applyNotionPageTemplateRowLook),
// else the database template stored names, read again when nil. The page
// keeps an icon and a cover of its own (applyDatabaseToolbarTemplateRowPresentation).
func writeTemplateRowLook(
	txApp core.App, identity notionOAuthBearerIdentity, rowID string, request notionPageTemplateRequest,
	source notionPageTemplateSource, fromPage bool, stored *storedDatabaseToolbarTemplate,
) error {
	if fromPage {
		return applyNotionPageTemplateRowLook(txApp, source, rowID)
	}
	current, err := authorizeNotionDatabaseRowPageCurrent(txApp, identity, rowID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	if stored == nil {
		template, _, templateErr := notionPageCreateTemplate(txApp, current, request)
		if templateErr != nil {
			return templateErr
		}
		stored = &template
	}
	look := *stored
	if current.Authority.Icon != "" {
		look.Icon = ""
	}
	if lookErr := applyDatabaseToolbarTemplateRowPresentation(txApp, look, rowID); lookErr != nil {
		return refusedUnlessStorage(errDatabaseRowPageConflict, lookErr)
	}
	return nil
}

// databaseTemplateBodyForExistingPage reads a template's body for a page
// that already has one: new block IDs, so a template applied twice adds its
// body twice, its template mentions given their value for this use and its
// synced block originals as synced says. Its own databases come apart, to be
// copied after the rest into their places, after the page's own blocks.
func databaseTemplateBodyForExistingPage(
	app core.App,
	template storedDatabaseToolbarTemplate,
	authority databaseRowPageAuthority,
	use databaseTemplateUse,
	synced rowPageBodySynced,
) ([]newRowPageBlock, map[string][]graphDatabaseView, []rowPageOwnDatabase, []rowPageSubPageCopy, error) {
	workspaceID := authority.WorkspaceID
	var size int
	if err := app.DB().NewQuery(`
		SELECT COALESCE((SELECT SUM(length([[content]])) FROM {{leafen_blocks}} WHERE [[page_id]] = {:templateId}), 0)
			+ COALESCE((SELECT length([[state]]) FROM {{leafen_ygo_document_states}} WHERE [[room]] = {:templateId}), 0)
	`).Bind(dbx.Params{"templateId": template.ID}).Row(&size); err != nil {
		return nil, nil, nil, nil, err
	}
	if size > databaseRowPageMaxBodyBytes {
		return nil, nil, nil, nil, notionDatabaseRowPageRequestInvalid("The template's body is larger than a database page takes.")
	}
	fresh, sources := map[string]string{}, map[string]string{}
	copied := func(id string) string {
		if _, known := fresh[id]; !known {
			fresh[id] = uuid.NewString()
			sources[fresh[id]] = id
		}
		return fresh[id]
	}
	body, err := readDatabaseRowPageBodyCopy(app, template.ID, workspaceID, authority.RowID, copied, use.resolve, synced)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	tail, err := databaseRowPageBodyTail(app, authority)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	topAfter := ""
	if tail != nil {
		topAfter = *tail
	}
	// A linked database comes with its views; a database of its own as a copy of it.
	body, databases := takeRowPageOwnDatabases(body, func(id string) string { return sources[id] }, topAfter)
	views, err := copiedLinkedDatabaseViews(app, template.ID, authority.RowID, authority.OwnerID, body,
		func(id string) string { return sources[id] }, copied)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	// Its sub-pages come as copies (database_row_page_sub_page_copies.go).
	target, err := app.FindRecordById(pagesCollection, authority.RowID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	subPages, err := rowPageBodySubPageCopies(app, template.ID, target, body, func(id string) string { return sources[id] })
	return body, views, databases, subPages, err
}

// appendNotionDatabaseRowPageBody puts blocks at the end of a database
// page's body, parents before their children, in changes of the text one
// change takes.
func appendNotionDatabaseRowPageBody(
	ctx context.Context,
	app core.App,
	ygo *pocketBaseYgoPersistence,
	identity notionOAuthBearerIdentity,
	authority databaseRowPageAuthority,
	mutationID string,
	body []newRowPageBlock,
	views map[string][]graphDatabaseView,
) error {
	if len(body) == 0 {
		return nil
	}
	children := map[string][]newRowPageBlock{}
	for _, block := range body {
		children[block.ParentID] = append(children[block.ParentID], block)
	}
	ordered := make([]newRowPageBlock, 0, len(body))
	// However deep the body nests; each block is placed once.
	visited := map[string]bool{}
	var visit func(parentID string)
	visit = func(parentID string) {
		if visited[parentID] {
			return
		}
		visited[parentID] = true
		for _, block := range children[parentID] {
			ordered = append(ordered, block)
			visit(block.ID)
		}
	}
	visit("")
	tail, err := databaseRowPageBodyTail(app, authority)
	if err != nil {
		return err
	}
	previous := map[string]*string{"": tail}
	operations := []graphOperation{}
	texts := []graphTextUpdate{}
	turn := 0
	flush := func() error {
		if len(operations) == 0 {
			return nil
		}
		_, err := ygo.applyContentGraphMutationContext(
			ctx, identity.PrincipalID, authority.RowID,
			graphMutationInput{
				MutationID: mutationID + ":body:" + strconv.Itoa(turn),
				Operations: operations, TextUpdates: texts,
			},
			contentGraphMutationHooks{Authorize: authorizeNotionPageMutation(identity, authority.RowID)},
		)
		operations, texts, turn = []graphOperation{}, []graphTextUpdate{}, turn+1
		return err
	}
	for _, block := range ordered {
		// A linked database's views come in its block's change.
		if len(operations)+1+len(views[block.ID]) > maxGraphMutationOperations {
			if err := flush(); err != nil {
				return err
			}
		}
		delete(block.Content, "text")
		content, err := json.Marshal(block.Content)
		if err != nil {
			return err
		}
		var parentID *string
		if block.ParentID != "" {
			parent := block.ParentID
			parentID = &parent
		}
		id := block.ID
		operations = append(operations, graphOperation{
			Kind: graphOperationKindUpsertBlock,
			Block: &graphBlock{
				ID: id, ParentID: parentID, AfterID: previous[block.ParentID],
				Position: "template-" + strconv.Itoa(len(operations)), Type: block.Type, Content: content,
			},
		})
		previous[block.ParentID] = &id
		if len(block.Spans) > 0 {
			texts = append(texts, graphTextUpdate{BlockID: id, Spans: block.Spans})
		}
		for index := range views[id] {
			operations = append(operations, graphOperation{Kind: graphOperationKindUpsertView, View: &views[id][index]})
		}
	}
	return flush()
}

// databaseRowPageBodyTail is the last block at the top of a database page's
// body, or nil when the body is empty.
func databaseRowPageBodyTail(app core.App, authority databaseRowPageAuthority) (*string, error) {
	tail := ""
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_blocks}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[parent_id]]=''
		ORDER BY [[position]] DESC,[[id]] DESC LIMIT 1
	`).Bind(dbx.Params{"owner": authority.OwnerID, "pageId": authority.RowID}).Row(&tail)
	if errors.Is(err, sql.ErrNoRows) || err == nil && tail == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tail, nil
}

// eraseNotionDatabaseRowPageBody takes away a database page's body, as the
// Public API's erase_content does for a page: its sub-pages and databases
// go to the trash with their blocks (eraseNotionPageContentThen).
func eraseNotionDatabaseRowPageBody(
	ctx context.Context,
	app core.App,
	ygo *pocketBaseYgoPersistence,
	identity notionOAuthBearerIdentity,
	authority databaseRowPageAuthority,
	mutationID string,
) error {
	roots := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_blocks}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[parent_id]]=''
		ORDER BY [[position]],[[id]]
	`).Bind(dbx.Params{"owner": authority.OwnerID, "pageId": authority.RowID}).Column(&roots); err != nil {
		return err
	}
	for start := 0; start < len(roots); start += maxGraphMutationOperations {
		end := min(start+maxGraphMutationOperations, len(roots))
		operations := make([]graphOperation, 0, end-start)
		for _, blockID := range roots[start:end] {
			operations = append(operations, graphOperation{
				Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: blockID},
			})
		}
		if _, err := ygo.applyContentGraphMutationContext(
			ctx, identity.PrincipalID, authority.RowID,
			graphMutationInput{MutationID: mutationID + ":" + strconv.Itoa(start), Operations: operations},
			// Page blocks follow their pages, as a page's erase sends them.
			contentGraphMutationHooks{Authorize: authorizeNotionPageMutation(identity, authority.RowID), PersonChange: true},
		); err != nil {
			return err
		}
	}
	return nil
}
