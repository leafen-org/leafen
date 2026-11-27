package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	mcpCreateDatabaseMaximumProperties  = 100
	mcpCreateDatabaseMaximumSchemaBytes = 64 << 10
)

// notionMCPCreateDatabaseParent is Notion's parent, its kind named or not
// (seen 2026-11-13: {"page_id": …, "type": "page_id"} taken), as
// create-pages' and move-pages' parents take it.
type notionMCPCreateDatabaseParent struct {
	Type   string `json:"type,omitempty" jsonschema:"page_id, or left out"`
	PageID string `json:"page_id,omitempty" jsonschema:"The page the database goes under"`
}

type notionMCPCreateDatabaseInput struct {
	Parent       notionMCPCreateDatabaseParent `json:"parent,omitempty" jsonschema:"The page the database goes under; left out, a private database at the top of the workspace (on a connection reaching several, the one workspace_id names)"`
	Title        string                        `json:"title,omitempty" jsonschema:"Database title; left out, the table's name in the schema, or no title, which screens show in their reader's language"`
	DatabaseType string                        `json:"database_type,omitempty" jsonschema:"tasks or projects: the database with the properties Notion gives that type, instead of a schema; skills isn't one Leafen has"`
	Schema       string                        `json:"schema,omitempty" jsonschema:"CREATE TABLE schema with double-quoted property names and their types, a type's options and other text in single quotes; without a TITLE column, \"Name\" is added. SELECT('opt':color, ...) and MULTI_SELECT('opt':color, ...) take options, STATUS('opt':color:group, ...) a status's (groups to_do, in_progress, complete), NUMBER FORMAT 'dollar' a format, FORMULA('expression') a formula, RELATION('ds') a relation (RELATION('ds', DUAL) or RELATION('ds', DUAL 'Name') two-way), ROLLUP('relation', 'property', 'function') a rollup, UNIQUE_ID PREFIX 'PR' a prefix (2-10 characters: a letter, then letters, digits or hyphens), and any column COMMENT 'description text'. Colors: default, gray, brown, orange, yellow, green, blue, purple, pink, red."`
}

// notionMCPCreatedDatabaseProperty is a property as an MCP answer names
// it: by the ID the Public API gives it (notion_property_public_id.go).
type notionMCPCreatedDatabaseProperty struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// notionMCPCreateDatabaseOutput is what Leafen's own callers read of a
// database made (an app's setup steps, app_platform_data_runner.go); the
// tool answers as Notion's does (notionMCPDatabaseAnswer).
type notionMCPCreateDatabaseOutput struct {
	Object        string                             `json:"object"`
	ID            string                             `json:"id"`
	Title         string                             `json:"title"`
	URL           string                             `json:"url"`
	ParentPageID  string                             `json:"parent_page_id"`
	PageID        string                             `json:"page_id"`
	BlockID       string                             `json:"block_id"`
	DataSourceID  string                             `json:"data_source_id"`
	DataSourceURL string                             `json:"data_source_url"`
	ViewID        string                             `json:"view_id"`
	Properties    []notionMCPCreatedDatabaseProperty `json:"properties"`
	Duplicate     bool                               `json:"duplicate"`
}

type mcpDatabaseSchemaProperty struct {
	Name string `json:"name"`
	Type string `json:"type"`
	// What Notion's DDL may add: a select's options, a number's format and
	// any column's description.
	Options      []mcpDatabaseSchemaOption `json:"options,omitempty"`
	NumberFormat string                    `json:"numberFormat,omitempty"`
	Description  string                    `json:"description,omitempty"`
	// FORMULA('expression'), RELATION('ds'[, DUAL ['name']]),
	// ROLLUP('relation', 'property', 'function') and UNIQUE_ID PREFIX 'PR'.
	Formula        string `json:"formula,omitempty"`
	Relation       string `json:"relation,omitempty"`
	Dual           bool   `json:"dual,omitzero"`
	SyncedName     string `json:"syncedName,omitempty"`
	RollupRelation string `json:"rollupRelation,omitempty"`
	RollupTarget   string `json:"rollupTarget,omitempty"`
	RollupFunction string `json:"rollupFunction,omitempty"`
	IDPrefix       string `json:"idPrefix,omitempty"`
	// What a relation's or rollup's names resolve to.
	relationSource, pairID, rollupRelationID, rollupTargetID string
	// translate puts a new status's first names in its maker's language.
	translate func(string) string
}

type mcpDatabaseSchemaOption struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	// Group is the status group a status's option names.
	Group string `json:"group,omitempty"`
}

type mcpDatabaseCreateIDs struct {
	MutationID   string
	BlockID      string
	DataSourceID string
	ViewID       string
	PropertyIDs  []string
}

var mcpDatabaseSchemaPropertyTypes = map[string]string{
	"TITLE":            graphValueTypeTitle,
	"RICH_TEXT":        graphValueTypeRichText,
	"TEXT":             graphValueTypeRichText,
	"NUMBER":           graphValueTypeNumber,
	"SELECT":           graphValueTypeSelect,
	"MULTI_SELECT":     graphValueTypeMultiSelect,
	"STATUS":           graphValueTypeStatus,
	"DATE":             graphValueTypeDate,
	"PERSON":           graphValueTypePerson,
	"PEOPLE":           graphValueTypePerson,
	"CHECKBOX":         graphValueTypeCheckbox,
	"URL":              graphValueTypeURL,
	"EMAIL":            graphValueTypeEmail,
	"PHONE":            graphValueTypePhone,
	"PHONE_NUMBER":     graphValueTypePhone,
	"FILES":            graphValueTypeFiles,
	"CREATED_TIME":     graphValueTypeCreatedTime,
	"LAST_EDITED_TIME": graphValueTypeLastEditedTime,
	"CREATED_BY":       graphValueTypeCreatedBy,
	"LAST_EDITED_BY":   graphValueTypeLastEditedBy,
	"FORMULA":          graphValueTypeFormula,
	"RELATION":         graphValueTypeRelation,
	"ROLLUP":           graphValueTypeRollup,
	"UNIQUE_ID":        graphValueTypeUniqueID,
	"BUTTON":           graphValueTypeButton,
	"PLACE":            graphValueTypePlace,
	"VERIFICATION":     graphValueTypeVerification,
}

// mcpDatabaseSchemaArgumentTypes take what they need in parentheses after
// the type, so ALTER COLUMN SET can't change a column to them.
var mcpDatabaseSchemaArgumentTypes = map[string]bool{
	graphValueTypeFormula: true, graphValueTypeRelation: true, graphValueTypeRollup: true,
}

func registerMCPDatabaseCreateTool(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-create-database", Title: "Create a Leafen database",
		Description: "Create a full-page database, as a sub-page of the parent page, with its table view and property schema, atomically. <database inline=\"true\"> in page content makes an inline one. " +
			"Answers with the database as fetch gives it: its data source's schema and SQLite table, and its collection:// URL in the <data-source> tag, for notion-update-data-source and notion-query-data-sources, and warnings naming each two-way relation's other side.",
	}, func(ctx context.Context, request *mcp.CallToolRequest, input notionMCPCreateDatabaseInput) (*mcp.CallToolResult, notionMCPDatabaseAnswer, error) {
		result, created, err := createMCPDatabase(ctx, request, app, input)
		if err != nil || result != nil {
			return result, notionMCPDatabaseAnswer{}, mcpNamedRefusal(ctx, app, err, mcpPage(input.Parent.PageID))
		}
		return mcpCreatedDatabaseAnswer(ctx, app, created)
	})
}

func createMCPDatabase(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	app core.App,
	input notionMCPCreateDatabaseInput,
) (*mcp.CallToolResult, notionMCPCreateDatabaseOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPCreateDatabaseOutput{}, err
	}
	parentID := mcpLocalID(app, input.Parent.PageID)
	// Its title keeps the spaces at its ends, as a database's name does
	// through Notion's API and its Markdown (seen 2026-11-13); one of spaces
	// alone is none.
	title := input.Title
	if strings.TrimSpace(title) == "" {
		title = ""
	}
	schema := input.Schema
	// Notion's takes one or the other (seen 2026-11-05).
	databaseType := strings.TrimSpace(input.DatabaseType)
	if (strings.TrimSpace(schema) == "") == (databaseType == "") {
		return nil, notionMCPCreateDatabaseOutput{}, mcpInvalid("give exactly one of schema, a CREATE TABLE statement as CREATE TABLE (\"Name\" TITLE), or database_type")
	}
	if databaseType == notionDatabaseTypeSkills {
		body, err := json.Marshal(notionMCPUnsupportedResult{
			Code: notionMeetingNotesUnsupportedCode, Message: "This official Notion MCP capability is not enabled in Leafen.",
			Reason: notionDatabaseTypeSkillsReason, Alternative: "Keep instructions in ordinary pages, or make a database with a schema.",
		})
		if err != nil {
			return nil, notionMCPCreateDatabaseOutput{}, failedBecause(errMCPToolFailed, err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}, IsError: true}, notionMCPCreateDatabaseOutput{}, nil
	}
	typed, isTyped := notionDatabaseTypeSchemas[databaseType]
	translate := mcpPersonWords(app, authority.PrincipalID)
	if databaseType != "" {
		if !isTyped {
			return nil, notionMCPCreateDatabaseOutput{}, mcpInvalid("database_type is tasks or projects")
		}
		schema = typed.statement(translate)
		if title == "" {
			title = translate(typed.title)
		}
	}
	properties, table, err := parseMCPDatabaseSchema(schema)
	if err != nil {
		return nil, notionMCPCreateDatabaseOutput{}, err
	}
	if title == "" {
		title = table
	}
	parentType := strings.TrimSpace(input.Parent.Type)
	if len(parentID) > maxWorkspacePermissionIdentifierLength || parentType != "" && (parentType != "page_id" || parentID == "") {
		return nil, notionMCPCreateDatabaseOutput{}, mcpInvalid("parent.page_id names the page the database goes under, its type page_id or left out; without a parent, the database is a private one at the top")
	}
	if utf8.RuneCountInString(title) > maxGraphTitleRunes {
		return nil, notionMCPCreateDatabaseOutput{}, mcpInvalid("a database's title is at most %d characters", maxGraphTitleRunes)
	}
	// Without a parent, a private database at the top of the one workspace
	// the connection reaches, as Notion's.
	parent := pageAuthorization{}
	var workspace workspaceAuthorization
	if parentID == "" {
		workspace, err = selectMCPWorkspace(app, authority, mcpWorkspaceInput{AllowImplicit: true}, permissionWorkspaceWrite)
		if errors.Is(err, errMCPWorkspaceRequired) {
			return nil, notionMCPCreateDatabaseOutput{}, mcpWorkspaceNeeded(authority, " or a parent page in one")
		}
		if err != nil {
			return nil, notionMCPCreateDatabaseOutput{}, err
		}
		parent = pageAuthorization{Workspace: workspace}
	} else {
		parent, err = authorizePageBody(app, authority.PrincipalID, parentID, permissionWorkspaceRead)
		if err != nil {
			return nil, notionMCPCreateDatabaseOutput{}, mcpDatabaseCreateError(err)
		}
		workspace, err = selectMCPWorkspace(app, authority, mcpWorkspaceInput{
			WorkspaceID: parent.Workspace.WorkspaceID,
		}, permissionWorkspaceWrite)
		if err != nil || workspace.WorkspaceID != parent.Workspace.WorkspaceID {
			return nil, notionMCPCreateDatabaseOutput{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
		}
	}
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return nil, notionMCPCreateDatabaseOutput{}, errMCPToolFailed
	}
	seed := mcpCallSeed()
	ids := mcpDatabaseCreateIdentity(authority.ConnectionID, seed, len(properties))
	requestHash, err := mcpDatabaseCreateRequestHash(parentID, title, properties)
	if err != nil {
		return nil, notionMCPCreateDatabaseOutput{}, failedBecause(errMCPToolFailed, err)
	}
	pageID := newPageID()
	receipt, err := persistence.applyContentGraphMutationContext(
		ctx, authority.PrincipalID, pageID,
		graphMutationInput{MutationID: ids.MutationID},
		contentGraphMutationHooks{
			LinkVerificationProperties: true, CheckOperations: databaseOptionChangesChecked(true),
			StructuralAppend: true, StepsChangeGraph: func() bool { return isTyped }, // only a typed one's finish changes its page
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				current, err := selectMCPWorkspace(txApp, authority, mcpWorkspaceInput{
					WorkspaceID: workspace.WorkspaceID,
				}, permissionWorkspaceWrite)
				if err != nil || current.WorkspaceID != workspace.WorkspaceID {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
				}
				created, status, err := admitPageCreationTransactionWithLock(txApp, authority.PrincipalID, pageCreationAdmissionInput{
					RequestID: ids.MutationID, WorkspaceID: workspace.WorkspaceID, ParentID: parentID,
					Title: title, ContentInBody: true, PreparedPageID: pageID, Kind: pageKindDatabase,
				}, pageLockIgnored)
				if err != nil {
					return pageAuthorization{}, err
				}
				if status != pageCreationApplied && !created.Duplicate {
					return pageAuthorization{}, mcpPageCreationError(nil, status)
				}
				return authorizePage(txApp, authority.PrincipalID, pageID, permissionWorkspaceWrite)
			},
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return requestHash, nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				if err := resolveMCPDatabaseSchemaReferences(txApp, authority.PrincipalID, workspace.WorkspaceID, ids.DataSourceID,
					properties, ids.PropertyIDs, nil); err != nil {
					return contentGraphMutationPreparation{}, err
				}
				operations, err := mcpDatabaseCreateOperations(doc, ids, title, properties)
				if err == nil && isTyped {
					err = typed.shape(operations, translate, false)
				}
				return contentGraphMutationPreparation{Operations: operations}, err
			},
			AfterApply: func(txApp core.App, auth pageAuthorization, _ *crdt.Doc) error {
				if !isTyped {
					return nil
				}
				return finishNotionDatabaseType(txApp, authority.PrincipalID, databaseToolbarConfigAuthority{
					OwnerID: auth.Workspace.OwnerID, WorkspaceID: auth.Workspace.WorkspaceID, PageID: pageID, DataSourceID: ids.DataSourceID,
				}, databaseType)
			},
		},
	)
	if err != nil {
		return nil, notionMCPCreateDatabaseOutput{}, mcpDatabaseCreateError(err)
	}
	// A Verification property comes with its Owner, so there may be more.
	if receipt.Counts.Blocks != 1 || receipt.Counts.DataSources != 1 ||
		receipt.Counts.Views != 1 || receipt.Counts.Properties < len(properties) {
		return nil, notionMCPCreateDatabaseOutput{}, errMCPToolFailed
	}
	// A database made under a wiki's page is the wiki's.
	if _, err := persistence.adoptPageTreeIntoWiki(ctx, authority.PrincipalID, pageID); err != nil {
		return nil, notionMCPCreateDatabaseOutput{}, mcpDatabaseCreateError(err)
	}
	publicIDs, err := loadNotionPropertyIDs(app, ids.DataSourceID)
	if err != nil {
		return nil, notionMCPCreateDatabaseOutput{}, mcpDatabaseCreateError(err)
	}
	outputProperties := make([]notionMCPCreatedDatabaseProperty, len(properties))
	for index, property := range properties {
		outputProperties[index] = notionMCPCreatedDatabaseProperty{
			ID: publicIDs.publicID(ids.PropertyIDs[index]), Name: property.Name, Type: property.Type,
		}
	}
	return nil, notionMCPCreateDatabaseOutput{
		Object: "database", ID: ids.BlockID, Title: mcpDatabaseShownTitle(app, authority.PrincipalID, title),
		URL: mcpPageAddress(app, authority, pageID), ParentPageID: parentID, PageID: pageID,
		BlockID: ids.BlockID, DataSourceID: ids.DataSourceID,
		DataSourceURL: mcpDataSourceURL(app, ids.DataSourceID),
		ViewID:        ids.ViewID, Properties: outputProperties,
		Duplicate: receipt.Duplicate,
	}, nil
}

// mcpDatabaseSchemaForm is the schema's form, as a refusal says it.
const mcpDatabaseSchemaForm = `CREATE TABLE ("Name" TITLE, "Status" SELECT('To do':red, 'Done':green), "Due" DATE)`

// parseMCPDatabaseSchema reads a schema in Notion's form, and the name a
// table may be given before its columns, with a closing semicolon or not as
// SQL is written; it says what to change when it can't.
func parseMCPDatabaseSchema(raw string) ([]mcpDatabaseSchemaProperty, string, error) {
	if len(raw) == 0 || len(raw) > mcpCreateDatabaseMaximumSchemaBytes {
		return nil, "", mcpInvalid("the schema is empty or longer than %d bytes", mcpCreateDatabaseMaximumSchemaBytes)
	}
	schema := strings.TrimSuffix(strings.TrimSpace(raw), ";")
	const prefix = "CREATE TABLE"
	if len(schema) < len(prefix) || !strings.EqualFold(schema[:len(prefix)], prefix) {
		return nil, "", mcpInvalid("the schema starts with CREATE TABLE, as %s", mcpDatabaseSchemaForm)
	}
	body := strings.TrimSpace(schema[len(prefix):])
	// A table's name, quoted or not, is the database's title when none is given.
	table := ""
	if body != "" && body[0] == '"' {
		name, next, ok := readMCPDatabaseQuotedIdentifier(body, 0)
		if !ok {
			return nil, "", mcpInvalid("the table's name has no closing quote")
		}
		table, body = strings.TrimSpace(name), strings.TrimSpace(body[next:])
	} else if at := strings.IndexByte(body, '('); at > 0 {
		table, body = strings.TrimSpace(body[:at]), body[at:]
	}
	if len(body) < 3 || body[0] != '(' || body[len(body)-1] != ')' {
		return nil, "", mcpInvalid("the columns go in parentheses after CREATE TABLE, as %s", mcpDatabaseSchemaForm)
	}
	body = body[1 : len(body)-1]
	properties := make([]mcpDatabaseSchemaProperty, 0)
	seen := map[string]struct{}{}
	titleCount := 0
	for offset := 0; ; {
		offset = skipMCPDatabaseSchemaSpace(body, offset)
		name, next, ok := readMCPDatabaseQuotedIdentifier(body, offset)
		if !ok || strings.TrimSpace(name) != name || name == "" ||
			utf8.RuneCountInString(name) > maxGraphIdentifierLength {
			return nil, "", mcpInvalid("column %d: a column's name is in double quotes, without spaces around it, at most %d characters, as \"Name\"",
				len(properties)+1, maxGraphIdentifierLength)
		}
		offset = skipMCPDatabaseSchemaSpace(body, next)
		typeStart := offset
		for offset < len(body) && isMCPDatabaseSchemaTypeByte(body[offset]) {
			offset++
		}
		propertyType, supported := mcpDatabaseSchemaPropertyTypes[strings.ToUpper(body[typeStart:offset])]
		if typeStart == offset || !supported {
			return nil, "", mcpInvalid("column %q: its type is one of %s", name, mcpDatabaseSchemaTypeNames())
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, "", mcpInvalid("column %q is named twice", name)
		}
		seen[name] = struct{}{}
		if propertyType == graphValueTypeTitle {
			titleCount++
		}
		property := mcpDatabaseSchemaProperty{Name: name, Type: propertyType}
		// What doesn't read is said for the column's type
		// (mcpDatabaseSchemaModifierRefusal), as update-data-source says it.
		if offset, ok = parseMCPDatabaseSchemaModifiers(body, offset, &property); !ok {
			return nil, "", mcpInvalid("%s", mcpDatabaseSchemaModifierRefusal(property, body, offset))
		}
		properties = append(properties, property)
		if len(properties) > mcpCreateDatabaseMaximumProperties {
			return nil, "", mcpInvalid("a database takes at most %d columns", mcpCreateDatabaseMaximumProperties)
		}
		offset = skipMCPDatabaseSchemaSpace(body, offset)
		if offset == len(body) {
			break
		}
		if body[offset] != ',' {
			return nil, "", mcpInvalid("column %q: columns are separated by commas", name)
		}
		offset++
	}
	if titleCount > 1 {
		return nil, "", mcpInvalid("a database has one TITLE column; this schema has %d", titleCount)
	}
	// A schema without a title gets "Name", as Notion's does.
	if titleCount == 0 {
		if _, taken := seen["Name"]; taken {
			return nil, "", mcpInvalid(`a column named "Name" goes as the TITLE here, or give one column the type TITLE`)
		}
		properties = append([]mcpDatabaseSchemaProperty{{Name: "Name", Type: graphValueTypeTitle}}, properties...)
		if len(properties) > mcpCreateDatabaseMaximumProperties {
			return nil, "", mcpInvalid("a database takes at most %d columns", mcpCreateDatabaseMaximumProperties)
		}
	}
	return properties, table, nil
}

// mcpDatabaseSchemaTypeNames are the column types a schema takes.
func mcpDatabaseSchemaTypeNames() string {
	names := make([]string, 0, len(mcpDatabaseSchemaPropertyTypes))
	for name := range mcpDatabaseSchemaPropertyTypes {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func skipMCPDatabaseSchemaSpace(value string, offset int) int {
	for offset < len(value) {
		switch value[offset] {
		case ' ', '\t', '\r', '\n':
			offset++
		default:
			return offset
		}
	}
	return offset
}

func readMCPDatabaseQuotedIdentifier(value string, offset int) (string, int, bool) {
	if offset >= len(value) || value[offset] != '"' {
		return "", offset, false
	}
	offset++
	var name strings.Builder
	for offset < len(value) {
		if value[offset] != '"' {
			name.WriteByte(value[offset])
			offset++
			continue
		}
		if offset+1 < len(value) && value[offset+1] == '"' {
			name.WriteByte('"')
			offset += 2
			continue
		}
		return name.String(), offset + 1, true
	}
	return "", offset, false
}

func isMCPDatabaseSchemaTypeByte(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value == '_'
}

func mcpDatabaseCreateIdentity(connectionID, seed string, propertyCount int) mcpDatabaseCreateIDs {
	identity := func(kind string, index int) string {
		digest := sha256.Sum256([]byte(fmt.Sprintf(
			"leafen:mcp-database-create:v1\x00%s\x00%s\x00%s\x00%d",
			connectionID, seed, kind, index,
		)))
		return "mcp-db-" + kind + "-" + hex.EncodeToString(digest[:16])
	}
	// What people see is named as the screen names its own (mcpEntityID).
	entity := func(kind string, index int) string {
		return mcpEntityID(fmt.Sprintf(
			"leafen:mcp-database-create:v1\x00%s\x00%s\x00%s\x00%d", connectionID, seed, kind, index,
		))
	}
	properties := make([]string, propertyCount)
	for index := range properties {
		properties[index] = entity("property", index)
	}
	return mcpDatabaseCreateIDs{
		MutationID: identity("mutation", 0), BlockID: entity("block", 0),
		DataSourceID: entity("source", 0), ViewID: entity("view", 0),
		PropertyIDs: properties,
	}
}

func mcpDatabaseCreateRequestHash(
	parentID, title string,
	properties []mcpDatabaseSchemaProperty,
) (string, error) {
	payload, err := json.Marshal(struct {
		ParentID   string                      `json:"parent_id"`
		Title      string                      `json:"title"`
		Properties []mcpDatabaseSchemaProperty `json:"properties"`
	}{ParentID: parentID, Title: title, Properties: properties})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func mcpDatabaseCreateOperations(
	doc *crdt.Doc,
	ids mcpDatabaseCreateIDs,
	title string,
	properties []mcpDatabaseSchemaProperty,
) ([]graphOperation, error) {
	order := doc.GetArray(blockOrderRoot(""))
	var afterID *string
	if order.Len() > 0 {
		tail, ok := order.Get(order.Len() - 1).(string)
		if !ok || tail == "" {
			return nil, graphStructureAuthorityError()
		}
		afterID = &tail
	}
	content, err := json.Marshal(map[string]any{
		"dataSourceId": ids.DataSourceID, "viewId": ids.ViewID,
	})
	if err != nil {
		return nil, err
	}
	operations := []graphOperation{
		{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
			ID: ids.BlockID, AfterID: afterID, Position: "p0000000000",
			Type: graphBlockTypeInlineDatabase, Content: content,
		}},
		{Kind: graphOperationKindUpsertDataSource, DataSource: &graphDataSource{
			ID: ids.DataSourceID, Title: title,
		}},
		{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: ids.ViewID, DataSourceID: ids.DataSourceID, Type: graphViewTypeTable,
			Name: notionCreatedDatabaseViewName, Position: "p0000000000", Config: notionCreatedViewConfig(),
		}},
	}
	for index, property := range properties {
		operations = append(operations, graphOperation{
			Kind: graphOperationKindUpsertProperty,
			Property: &graphProperty{
				ID: ids.PropertyIDs[index], DataSourceID: ids.DataSourceID,
				Type: property.Type, Name: property.Name,
				Position: fmt.Sprintf("p%010d", index), Config: mcpDatabaseSchemaConfig(ids.PropertyIDs[index], property),
			},
		})
	}
	return operations, nil
}

func mcpDatabaseCreateError(err error) error {
	if err == nil {
		return nil
	}
	if isDLPPolicyDecisionError(err) {
		return mcpDLPError(err)
	}
	if errors.Is(err, errMCPScopeDenied) || errors.Is(err, errMCPToolInvalid) {
		return err
	}
	if isPermissionDenied(err) {
		return errMCPCantChange
	}
	var requestError *graphRequestError
	if errors.As(err, &requestError) {
		if requestError.status == http.StatusNotFound {
			return mcpGraphReason(errMCPScopeDenied, err)
		}
		// A change refused for what it asks, a prefix another data source
		// has among them (database_unique_id_rules.go), says why.
		if requestError.status == http.StatusBadRequest ||
			requestError.code == errorCodeGraphMutationIDReused || requestError.code == errorCodeGraphChangeInvalid {
			return mcpGraphReason(errMCPToolInvalid, err)
		}
		return failedBecause(errMCPToolFailed, err)
	}
	return failedBecause(errMCPToolFailed, err)
}
