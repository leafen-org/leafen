package app

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// notionCreatedDatabaseViewName is the first view of a database a program
// makes — through the API or MCP — as Notion names it, in English whatever
// the reader's language (seen 2026-10-10).
const notionCreatedDatabaseViewName = "Default view"

// notionUnnamedSourceViewName is the view of a source a program adds to a
// database without a name, as Notion names it (notion_data_source_create.go).
const notionUnnamedSourceViewName = "New database"

// notionCreatedViewConfig is that view's first settings: it keeps its own row
// order, so rows a program adds show newest first, as Notion's do (seen
// 2026-11-04), like every other view made since (the web's viewRowOrder.ts).
func notionCreatedViewConfig() json.RawMessage {
	return json.RawMessage(`{"rowOrder":[]}`)
}

// A new database is a database block with its first source and a table view.
// An inline one goes at the end of its parent page; a full-page one is a new
// full-page database, under a page or at the workspace top level, whose body
// is the database and which takes its title, icon and cover.

type notionDatabaseCreateRequest struct {
	Parent            notionPageParentRequest `json:"parent"`
	Title             json.RawMessage         `json:"title,omitempty"`
	Description       json.RawMessage         `json:"description,omitempty"`
	IsInline          *bool                   `json:"is_inline,omitempty"`
	Icon              json.RawMessage         `json:"icon,omitempty"`
	Cover             json.RawMessage         `json:"cover,omitempty"`
	InitialDataSource *struct {
		Properties map[string]json.RawMessage `json:"properties"`
	} `json:"initial_data_source,omitempty"`
	DatabaseType *string `json:"database_type,omitempty"`
}

func serveNotionDatabaseCreate(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	if ygo == nil {
		return notionRequestNotCompleted(event, nil)
	}
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:write", permissionWorkspaceWrite)
	if err != nil || !ok {
		return err
	}
	input := notionDatabaseCreateRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	if input.DatabaseType != nil && *input.DatabaseType == notionDatabaseTypeSkills {
		return notionCompatibilityError(event, http.StatusNotImplemented, notionMeetingNotesUnsupportedCode, notionDatabaseTypeSkillsReason)
	}
	identity := workspace.Identity
	inline := input.IsInline != nil && *input.IsInline
	// Its name's and description's mentions are named for the workspace.
	mentions := notionNameMentions(event.App, identity, pageAuthorization{Workspace: workspace.Workspace})
	title, titleSpans, keys, properties, err := notionDatabaseCreateSchema(input, mentions)
	order := notionRequestKeyOrder(event, "initial_data_source", "properties")
	description, descriptionSpans := "", []graphTextSpan(nil)
	if err == nil && input.Description != nil {
		description, descriptionSpans, err = notionDatabaseDescription(input.Description, mentions)
	}
	if err == nil && inline && input.Cover != nil {
		err = notionDatabaseRowPageRequestInvalid("An inline database has no cover; only a database on a page of its own does.")
	}
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	presentation, err := notionPagePresentation(input.Icon, input.Cover)
	if err != nil {
		return notionPageValidationError(event, err.Error())
	}
	if input.Parent.PageID != "" {
		noteNotionAskedObject(event, "page", input.Parent.PageID)
	}
	if notionPageParentKind(input.Parent) == "workspace" && notionInternalConnection(identity) {
		return notionPageValidationError(event, notionInternalDatabaseParentMessage)
	}
	parentID, err := authorizeNotionDatabaseParent(event.App, identity, input.Parent, !inline)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	blockID := uuid.NewString()
	dataSourceID := newDatabaseEntityID()
	viewID := newDatabaseEntityID()
	plan := func(app core.App, pageID string, afterID *string) ([]graphOperation, error) {
		authorization := notionDatabaseRowPageAuthorization{
			Identity: identity,
			Container: notionDataSourceParentAuthority{
				ContainerID: blockID, WorkspaceID: identity.WorkspaceID, PageID: pageID, DataSource: dataSourceID,
				Title: title,
			},
		}
		operations, err := planNotionDatabaseCreate(app, authorization, input.Icon, title, titleSpans, description, descriptionSpans, keys, order, properties, viewID, afterID)
		if err == nil && input.DatabaseType != nil {
			same := func(word string) string { return word }
			err = notionDatabaseTypeSchemas[*input.DatabaseType].shape(operations, same, true)
		}
		return operations, err
	}
	if inline {
		_, err = ygo.applyContentGraphMutationContext(
			event.Request.Context(), identity.PrincipalID, parentID,
			graphMutationInput{MutationID: "notion-public-database-create:" + newDiagnosticReference()},
			contentGraphMutationHooks{
				StructuralAppend: true,
				// Its Verification comes with its Owner, as a data source's
				// does (database_verification_properties.go).
				LinkVerificationProperties: true,
				Authorize:                  authorizeNotionPageMutation(identity, parentID),
				Prepare: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
					var afterID *string
					if order := doc.GetArray(blockOrderRoot("")); order.Len() > 0 {
						tail, ok := order.Get(order.Len() - 1).(string)
						if !ok || tail == "" {
							return contentGraphMutationPreparation{}, graphStructureAuthorityError()
						}
						afterID = &tail
					}
					operations, err := plan(txApp, parentID, afterID)
					return contentGraphMutationPreparation{Operations: operations}, err
				},
			},
		)
		if err == nil {
			err = recordConnectionStoredMark(event.App, identity, connectionAuthoredDatabase, blockID, true, "")
		}
		// Its first view is no one's, as Notion's API leaves it (seen 2026-11-11).
		if err == nil {
			err = clearDatabaseViewAuthors(event.App, viewID)
		}
		if err == nil && input.DatabaseType != nil {
			err = finishNotionDatabaseType(event.App, identity.PrincipalID, databaseToolbarConfigAuthority{
				OwnerID: workspace.Workspace.OwnerID, WorkspaceID: identity.WorkspaceID, PageID: parentID, DataSourceID: dataSourceID,
			}, *input.DatabaseType)
		}
		if err != nil {
			return notionDataSourceMutationError(event, err)
		}
		return serveNotionDatabaseObject(event, identity, blockID)
	}
	// The new page's ID is not known before it is created, and planning uses
	// it only to name the container, so the operations are planned first.
	operations, err := plan(event.App, "", nil)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	pageID, err := createNotionPageWithContent(
		event, ygo, identity, parentID, title, nil, presentation, nil,
		notionPageCreateContentPlan{Operations: operations, Kind: pageKindDatabase},
	)
	if err != nil {
		return notionPageCreateContentError(event, err)
	}
	if page, err := event.App.FindRecordById(pagesCollection, pageID); err == nil {
		err = recordConnectionPage(event.App, identity, page, true, "")
		if err == nil {
			err = grantConnectionTopLevelPage(event.App, identity, parentID, pageID)
		}
		if err == nil {
			err = recordConnectionStoredMark(event.App, identity, connectionAuthoredDatabase, blockID, true, "")
		}
		if err == nil {
			err = clearDatabaseViewAuthors(event.App, viewID)
		}
		if err == nil && input.DatabaseType != nil {
			err = finishNotionDatabaseType(event.App, identity.PrincipalID, databaseToolbarConfigAuthority{
				OwnerID: workspace.Workspace.OwnerID, WorkspaceID: identity.WorkspaceID, PageID: pageID, DataSourceID: dataSourceID,
			}, *input.DatabaseType)
		}
		if err != nil {
			return notionDataSourceMutationError(event, err)
		}
	}
	return serveNotionDatabaseObject(event, identity, blockID)
}

// notionDatabaseCreateSchema returns the new database's title and its first
// source's properties from initial_data_source, database_type, or a single
// Name title.
func notionDatabaseCreateSchema(
	input notionDatabaseCreateRequest, mentions func([]graphTextSpan) error,
) (string, []graphTextSpan, []string, map[string]json.RawMessage, error) {
	if input.InitialDataSource != nil && input.DatabaseType != nil {
		return "", nil, nil, nil, notionDatabaseRowPageRequestInvalid("`database_type` doesn't go with `initial_data_source`: a typed database comes with a schema of its own.")
	}
	title, titleSpans := "", []graphTextSpan(nil)
	// None, or only spaces, makes it unnamed, as Notion's (seen 2026-11-13:
	// [], "" and "   " each made a "New database").
	if input.Title != nil {
		var err error
		if title, titleSpans, err = notionDataSourceName(input.Title, mentions); err != nil {
			if requestError := (&notionDatabaseRowPageRequestError{}); errors.As(err, &requestError) &&
				requestError.message != notionTemplateMentionRefused {
				err = notionDatabaseRowPageRequestInvalid("The database title is invalid.")
			}
			return "", nil, nil, nil, err
		}
	}
	properties := map[string]json.RawMessage{"Name": json.RawMessage(`{"title":{}}`)}
	keys := []string{"Name"}
	switch {
	case input.DatabaseType != nil:
		schema, ok := notionDatabaseTypeSchemas[*input.DatabaseType]
		if !ok {
			return "", nil, nil, nil, notionDatabaseRowPageRequestInvalid("database_type must be tasks or projects.")
		}
		if title == "" {
			title = schema.title
		}
		keys, properties = schema.requestProperties()
	case input.InitialDataSource != nil && len(input.InitialDataSource.Properties) > 0:
		properties = input.InitialDataSource.Properties
		encoded, err := json.Marshal(properties)
		if err != nil || len(encoded) > maxNotionDataSourceSchemaBytes || len(properties) > maxNotionDataSourceSchemaProperties {
			return "", nil, nil, nil, notionDatabaseRowPageRequestInvalid("The data source schema exceeds the supported size.")
		}
		keys = notionNamedPropertyKeys(properties)
	}
	return title, titleSpans, keys, properties, nil
}

// planNotionDatabaseCreate plans the database block after afterID, its first
// source and that source's table view.
func planNotionDatabaseCreate(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	icon json.RawMessage,
	title string,
	titleSpans []graphTextSpan,
	description string,
	descriptionSpans []graphTextSpan,
	keys, order []string,
	properties map[string]json.RawMessage,
	viewID string,
	afterID *string,
) ([]graphOperation, error) {
	dataSourceID := authorization.Container.DataSource
	source := &graphDataSource{ID: dataSourceID, Title: title, TitleSpans: titleSpans}
	if description != "" {
		source.Description, source.DescriptionSpans = &description, descriptionSpans
	}
	if len(icon) != 0 {
		var err error
		if source.Icon, source.IconType, err = notionDataSourceIcon(app, authorization.Identity, icon); err != nil {
			return nil, err
		}
	}
	schema, err := planNotionDataSourceSchema(app, authorization, dataSourceID, keys, order, properties)
	if err != nil {
		return nil, err
	}
	content, err := json.Marshal(map[string]any{"dataSourceId": dataSourceID, "viewId": viewID})
	if err != nil {
		return nil, err
	}
	operations := []graphOperation{
		{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
			ID: authorization.Container.ContainerID, AfterID: afterID, Position: "p0000000000",
			Type: graphBlockTypeInlineDatabase, Content: content,
		}},
		{Kind: graphOperationKindUpsertDataSource, DataSource: source},
	}
	operations = append(operations, schema...)
	return append(operations, graphOperation{
		Kind: graphOperationKindUpsertView,
		View: &graphDatabaseView{
			ID: viewID, DataSourceID: dataSourceID, Type: graphViewTypeTable,
			Name: notionCreatedDatabaseViewName, Position: "a00000000", Config: notionCreatedViewConfig(),
		},
	}), nil
}
