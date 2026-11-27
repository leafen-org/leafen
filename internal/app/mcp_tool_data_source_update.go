package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	mcpUpdateDataSourceMaximumStatements = 100
	mcpUpdateDataSourceMaximumBytes      = 64 << 10
)

const (
	mcpDataSourceStatementAdd    = "add"
	mcpDataSourceStatementRename = "rename"
	mcpDataSourceStatementAlter  = "alter"
	mcpDataSourceStatementDrop   = "drop"
)

type notionMCPUpdateDataSourceInput struct {
	DataSourceID string `json:"data_source_id" jsonschema:"collection:// data source URL or bare Leafen data source ID"`
	Title        string `json:"title,omitempty" jsonschema:"Optional non-empty replacement data source title"`
	Statements   string `json:"statements,omitempty" jsonschema:"Optional semicolon-separated statements with quoted names: ADD COLUMN \"Name\" <type> (a column as CREATE TABLE writes one: names in double quotes, a type's options and other text in single quotes, as ADD COLUMN \"Priority\" SELECT('High':red, 'Medium':yellow, 'Low')); DROP COLUMN \"Name\" (to Deleted properties); RENAME COLUMN \"Old\" TO \"New\"; ALTER COLUMN \"Name\" SET <type> to change its type or options. For SELECT, MULTI_SELECT and STATUS, SET lists the options to keep: one no page holds may be left out, and leaving out one pages hold is refused, naming it and how many pages hold it. ALTER COLUMN \"Name\" ADD OPTION 'New':blue adds one option without the others (a status's 'New':blue:in_progress), and ALTER COLUMN \"Name\" DROP OPTION 'Old' deletes one, clearing it from its pages. Existing options keep their id and can be recolored. STATUS options keep their group when the group is omitted"`
	// PageLayout is Notion's page_layout (mcp_data_source_page_layout.go).
	PageLayout *notionMCPPageLayout `json:"page_layout,omitempty" jsonschema:"Not with statements, which go in a call before it. Replaces the layout every page in the data source uses: pinned properties under the title, the property group's order, sections and each property's visibility, a sidebar, the relations group and a content tab. Fetch the data source first for its current layout (<page-layout>), then edit it; refer to properties by name. Example: {\"main\":[{\"type\":\"title\",\"pinnedProperties\":[\"Status\"]},{\"type\":\"properties\",\"properties\":[{\"property\":\"Notes\",\"visibility\":\"hide_if_empty\"}]},{\"type\":\"discussions\"},{\"type\":\"editor\"}],\"sidebar\":[{\"type\":\"property\",\"property\":\"Owner\"}]}"`
	// IsInline turns the database holding it (mcp_data_source_inline.go).
	IsInline *bool `json:"is_inline,omitempty" jsonschema:"true shows a full-page database inline in the page it is under; false turns an inline database into a full-page database right after where it was"`
}

type mcpDataSourceUpdateStatement struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	NewName string `json:"new_name,omitempty"`
	Type    string `json:"type,omitempty"`
	// An added column's options, number format and description.
	Column *mcpDatabaseSchemaProperty `json:"column,omitempty"`
	// An option added or dropped alone (mcp_data_source_option_statements.go).
	Option *mcpDataSourceOptionStatement `json:"option,omitempty"`
}

type mcpDataSourceUpdateIdentity struct {
	MutationID  string
	PropertyIDs []string
	// Renamed takes the name each renamed property was last asked for, when
	// the call's answer says the names it didn't give (mcpAskedRenamesIn).
	Renamed map[string]string
}

func registerMCPDataSourceUpdateTool(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-update-data-source", Title: "Update a Leafen data source",
		Description: "Update a data source's title, schema or page layout. statements add, drop (to the trash), rename and convert properties as semicolon-separated SQL DDL; for SELECT, MULTI_SELECT and STATUS, ALTER COLUMN … SET lists the options to keep — leaving out one pages hold is refused, naming it and how many pages hold it — while existing options keep their id and can be recolored; ALTER COLUMN … ADD OPTION 'New':color adds one alone, and ALTER COLUMN … DROP OPTION 'Old' deletes one, clearing it from its pages. " +
			"page_layout replaces the layout every page in the data source uses, in the form fetch gives it in <page-layout> (none means the original layout): fetch the data source first, edit that and give it back. Change properties in a call before the layout's: a call with both statements and page_layout is refused. " +
			"is_inline true shows a full-page database inline in the page it is under, false turns an inline one into a full-page database; the database keeps its address and data source. " +
			"Answers with its database as fetch gives it, the data source's schema and SQLite table as they are now, and warnings naming each two-way relation's other side it made.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, request *mcp.CallToolRequest, input notionMCPUpdateDataSourceInput) (*mcp.CallToolResult, notionMCPDatabaseAnswer, error) {
		return updateMCPDataSourceSayingNames(ctx, request, app, input)
	})
}

func updateMCPDataSource(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	app core.App,
	input notionMCPUpdateDataSourceInput,
) (*mcp.CallToolResult, notionMCPDatabaseAnswer, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPDatabaseAnswer{}, err
	}
	dataSourceID, ok := mcpLocalDataSourceID(app, input.DataSourceID)
	// A new title keeps the spaces at its ends, as a database's name does
	// through Notion's API and its Markdown (seen 2026-11-13); one of spaces
	// alone names nothing.
	title := input.Title
	if strings.TrimSpace(title) == "" {
		title = ""
	}
	// A statement that doesn't read says which and how it is written.
	statements, statementsErr := parseMCPDataSourceUpdateStatements(input.Statements)
	if ok && statementsErr != nil {
		return nil, notionMCPDatabaseAnswer{}, statementsErr
	}
	if !ok || (title == "" && len(statements) == 0 && input.PageLayout == nil && input.IsInline == nil) ||
		(input.Title != "" && title == "") || utf8.RuneCountInString(title) > maxGraphTitleRunes {
		return nil, notionMCPDatabaseAnswer{}, mcpInvalid("data_source_id as notion-fetch gives it (collection://…), and a title of at most %d characters, statements (ADD COLUMN, DROP COLUMN, RENAME COLUMN, ALTER COLUMN … SET …), a page_layout or is_inline to change", maxGraphTitleRunes)
	}
	location, err := loadMCPDataSourceLocation(app, dataSourceID)
	if err != nil {
		return nil, notionMCPDatabaseAnswer{}, mcpDataSourceQueryError(err)
	}
	page, err := authorizePageBody(app, authority.PrincipalID, location.PageID, permissionWorkspaceRead)
	if err != nil || page.Workspace.WorkspaceID != location.WorkspaceID ||
		page.Workspace.OwnerID != location.OwnerID {
		return nil, notionMCPDatabaseAnswer{}, mcpRefusedUnlessStorage(errMCPCantRead, err)
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
		WorkspaceID: location.WorkspaceID,
	}, permissionWorkspaceWrite)
	if err != nil || workspace.WorkspaceID != location.WorkspaceID {
		return nil, notionMCPDatabaseAnswer{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return nil, notionMCPDatabaseAnswer{}, errMCPToolFailed
	}
	if err := mcpDataSourceUpdateFits(app, authority, location, dataSourceID, input, statements); err != nil {
		return nil, notionMCPDatabaseAnswer{}, err
	}
	warnings := []string(nil)
	if title != "" || len(statements) > 0 {
		if _, err := applyMCPDataSourceStatements(
			ctx, app, persistence, authority, workspace, location, dataSourceID, title, statements,
		); err != nil {
			return nil, notionMCPDatabaseAnswer{}, err
		}
	}
	// The layout comes without statements (errMCPStatementsWithLayout).
	if input.PageLayout != nil {
		if warnings, err = applyMCPDataSourcePageLayout(ctx, app, persistence, authority, location, *input.PageLayout); err != nil {
			return nil, notionMCPDatabaseAnswer{}, err
		}
	}
	// Turned last, the other changes made where it stood.
	if input.IsInline != nil {
		if err := turnMCPDataSourceDatabase(ctx, app, persistence, authority, location, dataSourceID, *input.IsInline); err != nil {
			return nil, notionMCPDatabaseAnswer{}, err
		}
	}
	// The answer is the database the data source is in, as Notion's.
	answer := notionMCPDatabaseAnswer{}
	if containerID := mcpOwningDatabaseContainer(app, dataSourceID); containerID != "" {
		if answer, err = mcpDatabaseAnswer(ctx, app, containerID, mcpUpdatedDataSourceLead); err != nil {
			return nil, notionMCPDatabaseAnswer{}, err
		}
	} else {
		read, err := readMCPDataSource(ctx, app, dataSourceID)
		if err != nil {
			return nil, notionMCPDatabaseAnswer{}, err
		}
		answer.Result = mcpUpdatedDataSourceLead + read.Fetch.Text
	}
	answer.Warnings = warnings
	return nil, answer, nil
}

// applyMCPDataSourceStatements changes a data source's title and schema as
// one content graph change; it reports a replayed one.
func applyMCPDataSourceStatements(
	ctx context.Context, app core.App, persistence *pocketBaseYgoPersistence, authority mcpAuthority,
	workspace workspaceAuthorization, location mcpDataSourceLocation, dataSourceID, title string,
	statements []mcpDataSourceUpdateStatement,
) (bool, error) {
	seed := mcpCallSeed()
	identity := mcpDataSourceUpdateIDs(authority.ConnectionID, seed, len(statements))
	identity.Renamed = mcpAskedRenamesIn(ctx)
	requestHash, err := mcpDataSourceUpdateRequestHash(dataSourceID, title, statements)
	if err != nil {
		return false, failedBecause(errMCPToolFailed, err)
	}
	dropped, changed := []string{}, 0
	expectedSources := 0
	if title != "" {
		expectedSources = 1
	}
	receipt, err := persistence.applyContentGraphMutationContext(
		withPropertiesTrashed(ctx, &dropped), authority.PrincipalID, location.PageID,
		graphMutationInput{MutationID: identity.MutationID},
		contentGraphMutationHooks{
			// A type changed takes the settings the statement names, as
			// Notion's MCP and API do (seen 2026-11-14): a select or status
			// just the options listed, a type it had before not its old
			// settings; the type menu's rule is the screen's.
			LinkVerificationProperties: true, SettingsAsAsked: true, CheckOperations: databaseOptionChangesChecked(true),
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				authorized, err := authorizePageBody(
					txApp, authority.PrincipalID, location.PageID, permissionWorkspaceWrite,
				)
				if err != nil || authorized.Workspace.WorkspaceID != location.WorkspaceID ||
					authorized.Workspace.OwnerID != location.OwnerID {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPCantChange, err)
				}
				current, err := selectMCPWorkspace(txApp, authority, mcpWorkspaceInput{
					WorkspaceID: location.WorkspaceID,
				}, permissionWorkspaceWrite)
				if err != nil || current.WorkspaceID != workspace.WorkspaceID {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
				}
				return authorized, nil
			},
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return requestHash, nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				operations, drops, err := mcpDataSourceUpdateOperations(
					txApp, authority.PrincipalID, location, identity, title, statements,
				)
				changed = mcpPropertyUpsertCount(operations)
				// Its title renames the source alone (full_page_database_own_name.go).
				if err == nil && title != "" {
					operations, err = keepFullPageDatabaseOwnName(txApp, location.OwnerID, location.PageID, location.ID, title, operations)
				}
				dropped = drops
				return contentGraphMutationPreparation{Operations: operations}, err
			},
			// A dropped property goes to Deleted properties, as the editor's
			// delete does, in the same change; the views on it keep naming it
			// (database_property_trash_views.go). Its cells go with it: the
			// change's other steps change nothing of the page.
			StepsChangeGraph: func() bool { return len(dropped) > 0 },
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				for _, propertyID := range dropped {
					target, err := loadDatabasePropertyTrashTarget(txApp, location.PageID, propertyID)
					if err != nil {
						return err
					}
					// The other of a dependency pair went with the first.
					if target.DeletedAt != 0 {
						continue
					}
					if kept, err := wikiPropertyKept(txApp, target.ID); err != nil || kept {
						if err == nil {
							err = errWikiPropertyKept
						}
						return err
					}
					if err := trashDatabaseProperty(txApp, target, authority.PrincipalID, time.Now().UTC()); err != nil {
						return err
					}
				}
				return nil
			},
		},
	)
	if err != nil {
		return false, mcpDatabaseCreateError(err)
	}
	// A new Verification property comes with its Owner, so there may be more;
	// a change replayed was counted when it was made.
	if receipt.Counts.DataSources != expectedSources || receipt.Counts.Properties < changed {
		return false, errMCPToolFailed
	}
	return receipt.Duplicate, nil
}

func parseMCPDataSourceIdentifier(raw string) (string, bool) {
	if id, ok := parseMCPDataSourceURL(raw); ok {
		return id, true
	}
	id := strings.TrimSpace(raw)
	return id, id != "" && len(id) <= maxGraphIdentifierLength &&
		!strings.ContainsAny(id, "/?#")
}

func mcpDataSourceUpdateIDs(
	connectionID, seed string,
	statementCount int,
) mcpDataSourceUpdateIdentity {
	identity := func(kind string, index int) string {
		digest := sha256.Sum256([]byte(fmt.Sprintf(
			"leafen:mcp-data-source-update:v1\x00%s\x00%s\x00%s\x00%d",
			connectionID, seed, kind, index,
		)))
		return "mcp-ds-update-" + kind + "-" + hex.EncodeToString(digest[:16])
	}
	propertyIDs := make([]string, statementCount)
	for index := range propertyIDs {
		propertyIDs[index] = mcpEntityID(fmt.Sprintf(
			"leafen:mcp-data-source-update:v1\x00%s\x00%s\x00property\x00%d", connectionID, seed, index,
		))
	}
	return mcpDataSourceUpdateIdentity{
		MutationID: identity("mutation", 0), PropertyIDs: propertyIDs,
	}
}

func mcpDataSourceUpdateRequestHash(
	dataSourceID, title string,
	statements []mcpDataSourceUpdateStatement,
) (string, error) {
	payload, err := json.Marshal(struct {
		DataSourceID string                         `json:"data_source_id"`
		Title        string                         `json:"title,omitempty"`
		Statements   []mcpDataSourceUpdateStatement `json:"statements"`
	}{DataSourceID: dataSourceID, Title: title, Statements: statements})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

// mcpPropertyUpsertCount is how many properties the operations write.
func mcpPropertyUpsertCount(operations []graphOperation) int {
	count := 0
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertProperty {
			count++
		}
	}
	return count
}

// mcpDataSourceUpdateOperations are the graph changes the statements make,
// and the properties they drop.
func mcpDataSourceUpdateOperations(
	app core.App,
	principalID string,
	location mcpDataSourceLocation,
	identity mcpDataSourceUpdateIdentity,
	title string,
	statements []mcpDataSourceUpdateStatement,
) ([]graphOperation, []string, error) {
	current, err := loadMCPDataSourceLocation(app, location.ID)
	if err != nil || current.OwnerID != location.OwnerID ||
		current.WorkspaceID != location.WorkspaceID || current.PageID != location.PageID {
		return nil, nil, mcpRefusedUnlessStorage(errMCPChangedMeanwhile, err)
	}
	stored := []storedMCPDataSourceProperty{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[type]], [[position]], [[config]]
		FROM {{leafen_database_properties}}
		WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
			AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
			AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": location.OwnerID, "workspaceId": location.WorkspaceID,
		"pageId": location.PageID, "dataSourceId": location.ID,
		"limit": maxDatabaseQueryProperties + 1,
	}).All(&stored); err != nil {
		return nil, nil, err
	}
	if len(stored) > maxDatabaseQueryProperties || len(stored)+len(statements) > maxDatabaseQueryProperties {
		return nil, nil, errMCPToolFailed
	}
	byName := make(map[string]storedMCPDataSourceProperty, len(stored)+len(statements))
	for _, property := range stored {
		if _, duplicate := byName[property.Name]; duplicate {
			return nil, nil, graphStructureAuthorityError()
		}
		byName[property.Name] = property
	}
	operations, drops := make([]graphOperation, 0, len(statements)+1), []string{}
	// New columns' relations and rollups resolve against what is here and
	// what comes before them.
	columns, columnIDs := []mcpDatabaseSchemaProperty{}, []string{}
	for index, statement := range statements {
		if statement.Kind == mcpDataSourceStatementAdd {
			column := mcpDatabaseSchemaProperty{Name: statement.Name, Type: statement.Type}
			if statement.Column != nil {
				column = *statement.Column
			}
			columns, columnIDs = append(columns, column), append(columnIDs, identity.PropertyIDs[index])
		}
	}
	if err := resolveMCPDatabaseSchemaReferences(app, principalID, location.WorkspaceID, location.ID, columns, columnIDs, stored); err != nil {
		return nil, nil, err
	}
	resolved := map[string]mcpDatabaseSchemaProperty{}
	for index, column := range columns {
		resolved[columnIDs[index]] = column
	}
	if title != "" {
		// The name comes plain, its formatting going even with the same
		// words, as Notion's MCP title does (seen 2026-11-09).
		operations = append(operations, graphOperation{
			Kind:       graphOperationKindUpsertDataSource,
			DataSource: &graphDataSource{ID: location.ID, Title: title, TitleSpans: []graphTextSpan{}},
		})
	}
	tail := ""
	for _, property := range stored {
		tail = max(tail, property.Position)
	}
	// The statements apply in order, each to the schema the ones before it
	// left, as SQL's DDL does and as Notion's description sets none apart:
	// a property several of them change — renamed then changed, added then
	// changed, changed twice — is one change, as the last leaves it.
	at := map[string]int{}
	put := func(property storedMCPDataSourceProperty) {
		byName[property.Name] = property
		if index, seen := at[property.ID]; seen {
			operations[index] = mcpDataSourcePropertyOperation(location.ID, property)
			return
		}
		at[property.ID] = len(operations)
		operations = append(operations, mcpDataSourcePropertyOperation(location.ID, property))
	}
	made := []string{}
	for index, statement := range statements {
		switch statement.Kind {
		case mcpDataSourceStatementAdd:
			// A column added by a name one has changes that one, as ALTER
			// COLUMN … SET does, as Notion's MCP does (seen 2026-11-14: text
			// "글A 1" made NUMBER, a select's options those listed, its IDs
			// kept).
			if property, exists := byName[statement.Name]; exists {
				if property, err = mcpAlteredDataSourceProperty(app, principalID, identity.PropertyIDs[index], property, statement); err != nil {
					return nil, nil, err
				}
				put(property)
				continue
			}
			position, err := databasePositionAfter(tail)
			if err != nil {
				return nil, nil, err
			}
			tail = position
			// A column added gets the settings CREATE TABLE would give it — a
			// bare STATUS Notion's three options and groups, as Notion's.
			property := storedMCPDataSourceProperty{
				ID: identity.PropertyIDs[index], Name: statement.Name, Type: statement.Type,
				Position: position, Config: string(mcpDatabaseSchemaConfig(identity.PropertyIDs[index], resolved[identity.PropertyIDs[index]])),
			}
			made = append(made, property.ID)
			put(property)
		case mcpDataSourceStatementRename:
			property, exists := byName[statement.Name]
			if !exists {
				return nil, nil, graphInputError(errorCodeGraphInvalid, "Database property not found.")
			}
			// A name another column has takes the first free number, its own
			// counting as taken, as Notion's MCP does (seen 2026-11-14:
			// "텍스트" to "글A" beside "글A" … "글A 3" became "글A 4") and
			// as the server names it (distinguishDatabasePropertyNames).
			name := statement.NewName
			if name != statement.Name {
				taken := map[string]bool{}
				for other := range byName {
					taken[other] = true
				}
				name = distinctDatabasePropertyName(name, taken)
			}
			delete(byName, statement.Name)
			property.Name = name
			if identity.Renamed != nil {
				identity.Renamed[property.ID] = statement.NewName
			}
			put(property)
		case mcpDataSourceStatementAlter:
			property, exists := byName[statement.Name]
			if !exists {
				return nil, nil, graphInputError(errorCodeGraphInvalid, "Database property not found.")
			}
			if property, err = mcpAlteredDataSourceProperty(app, principalID, identity.PropertyIDs[index], property, statement); err != nil {
				return nil, nil, err
			}
			put(property)
		case mcpDataSourceStatementDrop:
			// The title stays; the rest goes to the trash, where people can bring it back.
			property, exists := byName[statement.Name]
			if !exists {
				return nil, nil, graphInputError(errorCodeGraphInvalid, "Database property not found.")
			}
			if property.Type == graphValueTypeTitle {
				return nil, nil, graphInputError(errorCodeGraphInvalid, "The title property can't be dropped.")
			}
			delete(byName, statement.Name)
			drops = append(drops, property.ID)
		default:
			return nil, nil, graphInputError(errorCodeGraphInvalid, "Check the database change details.")
		}
	}
	hidden, err := databaseNewPropertiesHiddenInTables(app, location.PageID, location.ID, made)
	if err != nil {
		return nil, nil, err
	}
	return append(operations, hidden...), drops, nil
}

func mcpDataSourcePropertyOperation(
	dataSourceID string,
	stored storedMCPDataSourceProperty,
) graphOperation {
	return graphOperation{
		Kind: graphOperationKindUpsertProperty,
		Property: &graphProperty{
			ID: stored.ID, DataSourceID: dataSourceID, Type: stored.Type,
			Name: stored.Name, Position: stored.Position, Config: json.RawMessage(stored.Config),
		},
	}
}
