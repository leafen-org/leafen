package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	mcpCreatePagesMaximumItems        = 100
	mcpCreatePagesMaximumContentRunes = 2_000_000
)

type notionMCPCreatePagesParent struct {
	Type         string `json:"type,omitempty" jsonschema:"page_id, database_id or data_source_id"`
	PageID       string `json:"page_id,omitempty" jsonschema:"Parent page ID"`
	DatabaseID   string `json:"database_id,omitempty" jsonschema:"Parent database, when it has one data source; its pages take properties as SQLite values"`
	DataSourceID string `json:"data_source_id,omitempty" jsonschema:"Parent data source (collection:// URL or ID); its pages take properties as SQLite values"`
	TeamspaceID  string `json:"teamspace_id,omitempty" jsonschema:"A team from notion-get-teams: the pages go at its top (a workspace's top: leave parent out and give workspace_id)"`
}

type notionMCPCreatePagesInput struct {
	Parent       notionMCPCreatePagesParent      `json:"parent,omitempty" jsonschema:"Parent page, database, data source or team; left out, the pages are private pages at the workspace's top (on a connection reaching several, the one workspace_id names)"`
	Pages        []notionMCPCreatePageDefinition `json:"pages" jsonschema:"One to 100 pages created atomically under the parent"`
	AllowAsync   *bool                           `json:"allow_async,omitempty" jsonschema:"Leafen creates the pages within the call and answers with them, so true and false answer the same"`
	CreationMode string                          `json:"creation_mode,omitempty" jsonschema:"draft makes private pages at the workspace's top, as leaving the parent out does; not with a parent"`
	Position     *notionMCPInsertPosition        `json:"position,omitempty" jsonschema:"Where the pages go in a page parent's content, in the order given: start or end (the default) for its top or bottom; after_content or before_content with an anchor to put them after the block where the anchor ends or before the block where it starts, at that block's level. Only for a page_id parent"`
}

// notionMCPCreatedPage is a page create-pages made, as Notion's answer names
// it (seen 2026-11-11): its ID, its address and the properties it was given.
type notionMCPCreatedPage struct {
	ID  string `json:"id"`
	URL string `json:"url"`
	// Properties are the values given, a list written as JSON text as a list,
	// as Notion's tool answers (seen 2026-11-04).
	Properties map[string]any `json:"properties"`
	// Duplicate says a retried call found the page made before.
	Duplicate bool `json:"-"`
}

// mcpCreatedPageProperties are a new page's properties as its tool answers.
func mcpCreatedPageProperties(given map[string]any) map[string]any {
	properties := make(map[string]any, len(given))
	for name, value := range given {
		if text, isText := value.(string); isText && strings.HasPrefix(strings.TrimSpace(text), "[") {
			var list []any
			if json.Unmarshal([]byte(text), &list) == nil {
				value = list
			}
		}
		properties[name] = value
	}
	return properties
}

// mcpCreatedUntitledLeftOut leaves an untitled page's title out of its
// answer, as Notion's ("properties":{} for "title":"", seen 2026-11-14).
func mcpCreatedUntitledLeftOut(properties map[string]any) map[string]any {
	if title, named := properties["title"]; named && (title == nil || title == "") {
		delete(properties, "title")
	}
	return properties
}

// mcpCreatedRowProperties are a new row's properties as its tool answers:
// those given, and its title, "" when none was, as Notion's answers a row
// (seen 2026-11-14: {"메모":"…","이름":""}).
func mcpCreatedRowProperties(app core.App, dataSourceID string, given map[string]any) map[string]any {
	properties := mcpCreatedPageProperties(given)
	var name string
	if app.DB().NewQuery(`SELECT [[name]] FROM {{leafen_database_properties}} WHERE [[data_source_id]] = {:sourceId} AND [[type]] = 'title' LIMIT 1`).
		Bind(dbx.Params{"sourceId": dataSourceID}).Row(&name) != nil || name == "" {
		return properties
	}
	_, named := properties[name]
	_, alias := properties["title"]
	if !named && !alias {
		properties[name] = ""
	}
	return properties
}

type notionMCPCreatePagesOutput struct {
	Pages []notionMCPCreatedPage `json:"pages"`
	// Warnings are what reading each page's Markdown changed or kept as
	// text, by the page's index (mcpMarkdownWarning).
	Warnings []mcpMarkdownWarning `json:"warnings,omitempty" jsonschema:"What reading a page's content changed or kept as text (markdown_parser_warning, its category the kind), by the page's index in pages"`
}

// said adds the warnings reading the Markdown of the page at index gave.
func (output *notionMCPCreatePagesOutput) said(index int, warnings []mcpMarkdownWarning) {
	for _, warning := range warnings {
		at := index
		warning.Index = &at
		output.Warnings = append(output.Warnings, warning)
	}
}

func createMCPPages(
	ctx context.Context,
	request *mcp.CallToolRequest,
	app core.App,
	input notionMCPCreatePagesInput,
) (*mcp.CallToolResult, notionMCPCreatePagesOutput, error) {
	// A first line "# X" is the page's title line, as Notion's
	// (notion_page_markdown_leading_title.go).
	rows := strings.TrimSpace(input.Parent.DatabaseID) != "" || strings.TrimSpace(input.Parent.DataSourceID) != ""
	for index := range input.Pages {
		definition := &input.Pages[index]
		title, rest, found := notionMarkdownLeadingTitle(definition.Content)
		if !found {
			continue
		}
		definition.Content = rest
		if given, named := definition.Properties["title"]; !rows && (!named || given == nil || given == "") {
			definition.Properties = maps.Clone(definition.Properties)
			if definition.Properties == nil {
				definition.Properties = map[string]any{}
			}
			definition.Properties["title"] = title
		}
	}
	return createMCPPagesIn(ctx, request, app, input)
}

// createMCPPagesIn makes pages for an agent, or for an app's run.
func createMCPPagesIn(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	app core.App,
	input notionMCPCreatePagesInput,
) (*mcp.CallToolResult, notionMCPCreatePagesOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPCreatePagesOutput{}, err
	}
	for _, definition := range input.Pages {
		if definition.IsSkill != nil && *definition.IsSkill {
			return nil, notionMCPCreatePagesOutput{}, mcpSkillPageUnsupported()
		}
	}
	// The public API's IDs name the parent as the tools' do (mcpLocalID).
	for _, id := range []*string{&input.Parent.PageID, &input.Parent.DatabaseID, &input.Parent.DataSourceID} {
		if strings.TrimSpace(*id) != "" {
			*id = mcpLocalID(app, *id)
		}
	}
	parents := 0
	teamspaceID := strings.TrimSpace(input.Parent.TeamspaceID)
	for _, named := range []string{input.Parent.PageID, input.Parent.DatabaseID, input.Parent.DataSourceID, teamspaceID} {
		if strings.TrimSpace(named) != "" {
			parents++
		}
	}
	draft := strings.TrimSpace(input.CreationMode)
	if parents > 1 || draft != "" && (draft != "draft" || parents != 0) ||
		len(input.Pages) == 0 || len(input.Pages) > mcpCreatePagesMaximumItems {
		return nil, notionMCPCreatePagesOutput{}, mcpInvalid("pages lists from 1 to %d pages, under at most one parent (page_id, database_id, data_source_id or teamspace_id); creation_mode draft takes no parent", mcpCreatePagesMaximumItems)
	}
	if parents == 1 && strings.TrimSpace(input.Parent.PageID) == "" && teamspaceID == "" {
		dataSourceID, err := mcpCreatePagesDataSource(app, input.Parent)
		if err != nil {
			return nil, notionMCPCreatePagesOutput{}, err
		}
		// A token narrowed to chosen pages adds rows only to a database under them.
		if authority.PagesNarrowed {
			location, err := loadMCPDataSourceLocation(app, dataSourceID)
			if err != nil || !mcpAuthorityReachesPageID(app, authority, location.PageID) {
				return nil, notionMCPCreatePagesOutput{}, mcpRefusedUnlessStorage(errMCPOutsideTokenPages, err)
			}
		}
		output, err := createMCPDatabasePagesBringing(ctx, app, authority, dataSourceID, input.Pages)
		for index := range output.Pages {
			output.Pages[index].Properties = mcpCreatedRowProperties(app, dataSourceID, definitionProperties(input.Pages, index))
		}
		return nil, output, err
	}
	parentID := strings.TrimSpace(input.Parent.PageID)
	if len(parentID) > maxWorkspacePermissionIdentifierLength {
		return nil, notionMCPCreatePagesOutput{}, mcpInvalid("parent.page_id is a page ID as Leafen's tools give it")
	}
	// Pages without a parent are private pages at the top of the one
	// workspace the connection reaches, as Notion's drafts are; the
	// workspace stands where a parent would for their Markdown.
	parent := pageAuthorization{}
	if parentID == "" && authority.PagesNarrowed {
		return nil, notionMCPCreatePagesOutput{}, mcpDenied("this token reaches only the pages chosen on its page: give one of them, or a page under one, as parent.page_id")
	}
	if parentID == "" {
		workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{TeamspaceID: teamspaceID, AllowImplicit: teamspaceID == ""}, permissionWorkspaceWrite)
		if errors.Is(err, errMCPWorkspaceRequired) {
			return nil, notionMCPCreatePagesOutput{}, mcpWorkspaceNeeded(authority, " or a parent in one")
		}
		if err != nil {
			return nil, notionMCPCreatePagesOutput{}, err
		}
		// A workspace from notion-get-teams' workspaces, given as the team,
		// takes the pages at its top as leaving the parent out does.
		if teamspaceID == workspace.WorkspaceID {
			teamspaceID = ""
		}
		parent = pageAuthorization{Workspace: workspace}
	} else {
		parent, err = authorizePage(app, authority.PrincipalID, parentID, permissionWorkspaceRead)
		if err != nil {
			return nil, notionMCPCreatePagesOutput{}, mcpPageCreationError(err, pageCreationApplied)
		}
		if !mcpAuthorityReachesPage(app, authority, parent.Page) {
			return nil, notionMCPCreatePagesOutput{}, errMCPOutsideTokenPages
		}
		workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
			WorkspaceID: parent.Workspace.WorkspaceID,
		}, permissionWorkspaceWrite)
		if err != nil || workspace.WorkspaceID != parent.Workspace.WorkspaceID {
			return nil, notionMCPCreatePagesOutput{}, mcpDenied("the parent is outside the workspaces this connection was given, or this account can't add pages there")
		}
	}
	mcpBringMarkdownUploads(app, authority, parent.Workspace.WorkspaceID, mcpCreatePagesUploads(input.Pages)...)
	seed := mcpCallSeed()
	admissions := make([]pageCreationAdmissionInput, len(input.Pages))
	bodies := make([][]newRowPageBlock, len(input.Pages))
	// A body that makes sub-pages or databases is written as notion-update-page
	// writes it, right after the pages are made, its links made mentions as
	// here (mcpCreatedBodyInput).
	written := make([]bool, len(input.Pages))
	presentations := make([]*notionPagePresentationRequest, len(input.Pages))
	said, moves := make([][]mcpMarkdownWarning, len(input.Pages)), make([]mcpCreatePageMoves, len(input.Pages))
	totalContentRunes := 0
	for index, definition := range input.Pages {
		if strings.TrimSpace(definition.TemplateID) != "" {
			return nil, notionMCPCreatePagesOutput{}, fmt.Errorf(
				"%w: templates belong to data sources; name the data source as the parent", errMCPToolInvalid,
			)
		}
		// A page given no title is made without one, as Notion's (seen
		// 2026-11-13: its title "", shown as 새 페이지).
		titleValue, named := definition.Properties["title"]
		given, titleOK := titleValue.(string)
		if titleValue == nil {
			given, titleOK = "", true
		}
		// The title reads as an MCP write's inline Markdown, what that says told first.
		title, fits := mcpPageTitleMarkdown(given, mcpValueSitePages(app, authority, parent))
		said[index] = mcpMarkdownWarnings(nil, title.Said)
		contentRunes := utf8.RuneCountInString(definition.Content)
		others := len(definition.Properties)
		if named {
			others--
		}
		if !titleOK || others > 0 || !fits ||
			contentRunes > mcpCreatePagesMaximumContentRunes {
			return nil, notionMCPCreatePagesOutput{}, mcpCreatePagesItemRefusal(mcpInvalid("a page under a page takes one property at most, its title (properties.title, at most %d characters; left out, the page has none), and content of at most %d characters", maxGraphTitleRunes, mcpCreatePagesMaximumContentRunes), len(input.Pages), index)
		}
		totalContentRunes += contentRunes
		if totalContentRunes > mcpCreatePagesMaximumContentRunes {
			return nil, notionMCPCreatePagesOutput{}, mcpInvalid("the pages' content together is at most %d characters: make them in more calls", mcpCreatePagesMaximumContentRunes)
		}
		if title, err = title.prepared(app, authority, parent); err != nil {
			return nil, notionMCPCreatePagesOutput{}, mcpCreatePagesItemRefusal(mcpGraphReason(errMCPToolInvalid, err), len(input.Pages), index)
		}
		// The Markdown is the page's first body; the request is still told
		// apart by it.
		admissions[index] = normalizePageCreationAdmissionInput(pageCreationAdmissionInput{
			RequestID:   mcpPageCreationRequestID(authority.ConnectionID, seed, index),
			WorkspaceID: parent.Workspace.WorkspaceID, TeamspaceID: teamspaceID, ParentID: parentID,
			Title: title.Words, TitleSpans: title.Spans, Content: definition.Content, ContentInBody: true,
		})
		if !validPageCreationAdmissionInput(admissions[index]) {
			return nil, notionMCPCreatePagesOutput{}, mcpCreatePagesItemRefusal(mcpInvalid("the page's place or title didn't read: its title at most %d characters", maxGraphTitleRunes), len(input.Pages), index)
		}
		presentation, set, err := mcpPagePresentation(app, parent.Workspace.WorkspaceID, definition.Icon, definition.Cover)
		if err != nil {
			return nil, notionMCPCreatePagesOutput{}, mcpCreatePagesItemRefusal(mcpReasoned(err), len(input.Pages), index)
		}
		if set {
			presentations[index] = &presentation
		}
		if strings.TrimSpace(definition.Content) == "" {
			continue
		}
		// It reads as notion-update-page reads Markdown, the public API's
		// "v2" (mcpPageMarkdownChange), the pages it moves in found first (mcpCreatePageMovesOf),
		// and what that says is told by page, a refusal too (mcpCreatePageContentRefusal).
		origin, _ := mcpServerURL(authority.Resource, "")
		if moves[index], err = mcpCreatePageMovesOf(app, authority, parent.Workspace.WorkspaceID, parentID, definition, origin); err != nil {
			return nil, notionMCPCreatePagesOutput{}, mcpCreatePagesItemRefusal(err, len(input.Pages), index)
		}
		content, warnings, err := notionMarkdownVersion2Input(moves[index].content)
		if refused := mcpCreatePageContentRefusal(moves[index].content, err, origin, len(input.Pages), index); refused != nil {
			return nil, notionMCPCreatePagesOutput{}, refused
		}
		said[index] = append(append(said[index], mcpMarkdownWarnings(nil, warnings)...), moves[index].left...)
		preserve := definition.PreserveInternalLinks != nil && *definition.PreserveInternalLinks
		site := notionMarkdownSitePagesAt(origin, true)
		if site != nil {
			site.LinksMentioned = !preserve
		}
		plan, err := notionMarkdownReplacementPlan(content, &notionPageMarkdownProjection{SitePages: site, References: moves[index].references}, false)
		if errors.Is(err, errNotionMarkdownToggleOpen) {
			return nil, notionMCPCreatePagesOutput{}, mcpCreatePagesItemRefusal(mcpInvalid(err.Error()), len(input.Pages), index)
		}
		// The plan says why it refuses: too much for one write (with how to
		// write it in parts), Markdown that doesn't parse (with where its
		// forms are), or a tag in a place it can't be.
		if errors.Is(err, errNotionMarkdownTooLarge) {
			return nil, notionMCPCreatePagesOutput{}, mcpCreatePagesItemRefusal(mcpInvalid("the page's content is more than one write takes (4 MB, or %d block changes): create the page with part of it, then add the rest with notion-update-page's insert_content", maxGraphMutationOperations), len(input.Pages), index)
		}
		if errors.Is(err, errNotionMarkdownUnparsed) {
			return nil, notionMCPCreatePagesOutput{}, mcpCreatePagesItemRefusal(mcpInvalid("the page's content isn't Markdown Leafen reads: "+notionPageMarkdownSpecURI+" gives the forms it takes"), len(input.Pages), index)
		}
		if unknown := (notionMarkdownNewUnknownError{}); errors.As(err, &unknown) {
			return nil, notionMCPCreatePagesOutput{}, mcpCreatePageUnknownRefusal(unknown, origin, len(input.Pages), index)
		}
		if err != nil {
			return nil, notionMCPCreatePagesOutput{}, mcpCreatePagesItemRefusal(mcpInvalid("%s", err.Error()), len(input.Pages), index)
		}
		if notionMarkdownMakesObjects(plan) {
			written[index] = true
			continue
		}
		// Code of nothing on a page made with Markdown has no text, as the
		// public API's create gives it (notionPageCreateEmptyCode).
		plan.TextUpdates = notionPageCreateEmptyCode(plan)
		// Its files pass the graph's one check as it is placed (content_graph_given_body_files.go).
		files := notionMarkdownFiles{WorkspaceID: parent.Workspace.WorkspaceID, Upload: mcpMarkdownUploads(authority)}
		err = files.place(app, plan.Operations)
		if err == nil {
			err = checkGivenPageBodyFiles(app, files.WorkspaceID, plan.Operations)
		}
		if err != nil {
			requestError := &graphRequestError{}
			if errors.As(err, &requestError) {
				return nil, notionMCPCreatePagesOutput{}, mcpCreatePagesItemRefusal(fmt.Errorf("%w: %s", errMCPToolInvalid, requestError.message), len(input.Pages), index)
			}
			return nil, notionMCPCreatePagesOutput{}, mcpCreatePagesItemRefusal(mcpReasoned(err), len(input.Pages), index)
		}
		readable := mcpLinkTargetReadable(app, authority, parent.Workspace.OwnerID, parent.Workspace.WorkspaceID)
		if err := mentionMCPInternalLinks(readable, mcpSitePages(authority), plan.Operations, plan.TextUpdates, preserve); err != nil {
			return nil, notionMCPCreatePagesOutput{}, failedBecause(errMCPToolFailed, err)
		}
		// Its mentions are checked, and its :name: resolved, as the
		// Markdown's writes to a page are.
		prepared, err := narrowedMarkdownMentionInput(authority.PrincipalID, mcpMentionReaches(authority))(
			app, parent, nil, graphMutationInput{Operations: plan.Operations, TextUpdates: plan.TextUpdates},
		)
		if err != nil {
			requestError := &graphRequestError{}
			if errors.As(err, &requestError) {
				return nil, notionMCPCreatePagesOutput{}, mcpGraphReason(errMCPToolInvalid, err)
			}
			return nil, notionMCPCreatePagesOutput{}, failedBecause(errMCPToolFailed, err)
		}
		if bodies[index], err = markdownPageBlocks(plan.Operations, prepared.TextUpdates); err != nil {
			return nil, notionMCPCreatePagesOutput{}, mcpInvalid("the page's content didn't make blocks: simplify it, or write it with notion-update-page after")
		}
	}
	output := notionMCPCreatePagesOutput{Pages: make([]notionMCPCreatedPage, 0, len(admissions))}
	for index := range said {
		output.said(index, said[index])
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		defer namePageHistoryConnection(txApp, ctx)()
		now := time.Now().UTC().UnixMilli()
		for index, admissionInput := range admissions {
			response, status, err := admitPageCreationTransactionWithLock(
				txApp, authority.PrincipalID, admissionInput, pageLockIgnored,
			)
			if err != nil {
				return err
			}
			if status != pageCreationApplied {
				return &mcpPageCreationFailure{status: status}
			}
			if !response.Duplicate && len(bodies[index]) > 0 {
				page, err := txApp.FindRecordById(pagesCollection, response.Page.ID)
				if err != nil {
					return err
				}
				if err := writeNewPageBody(txApp, page, authority.PrincipalID, bodies[index], now); err != nil {
					return err
				}
			}
			if !response.Duplicate && presentations[index] != nil {
				if err := applyMCPPageMetadata(
					ctx, txApp, authority.PrincipalID, parent.Workspace.WorkspaceID, response.Page.ID, nil, *presentations[index],
				); err != nil {
					return err
				}
			}
			output.Pages = append(output.Pages, notionMCPCreatedPage{
				ID: response.Page.ID, URL: mcpPageURL(authority, response.Page.ID), Duplicate: response.Duplicate,
				Properties: mcpCreatedUntitledLeftOut(mcpCreatedPageProperties(definitionProperties(input.Pages, index))),
			})
		}
		return nil
	})
	if err != nil {
		failure := &mcpPageCreationFailure{}
		if errors.As(err, &failure) {
			return nil, notionMCPCreatePagesOutput{}, mcpPageCreationError(nil, failure.status)
		}
		return nil, notionMCPCreatePagesOutput{}, mcpPageCreationError(err, pageCreationApplied)
	}
	// Its warnings were said as its Markdown was first read.
	for index, definition := range input.Pages {
		if !written[index] || output.Pages[index].Duplicate {
			continue
		}
		if _, err := writeMCPCreatedBody(ctx, app, output.Pages[index].ID, definition, moves[index]); err != nil {
			return nil, notionMCPCreatePagesOutput{}, err
		}
	}
	// A page made under a wiki's page, or a page of a wiki, is the wiki's.
	if persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence); ok {
		for _, page := range output.Pages {
			if _, err := persistence.adoptPageTreeIntoWiki(ctx, authority.PrincipalID, page.ID); err != nil {
				return nil, notionMCPCreatePagesOutput{}, mcpPageCreationError(err, pageCreationApplied)
			}
		}
	}
	return nil, output, nil
}

func mcpPageCreationRequestID(connectionID, seed string, index int) string {
	digest := sha256.Sum256([]byte(
		"leafen:mcp-page-creation:v1\x00" + connectionID + "\x00" + seed + "\x00" + strconv.Itoa(index),
	))
	return "mcp-page-create-" + hex.EncodeToString(digest[:])
}

// notionMarkdownMakesObjects tells a plan that makes more than blocks: sub-pages,
// databases, or their sources and views.
func notionMarkdownMakesObjects(plan notionMarkdownPlan) bool {
	if len(plan.FullPageDatabases) > 0 || len(plan.Renames) > 0 {
		return true
	}
	for _, operation := range plan.Operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil ||
			operation.Block.Type == graphBlockTypeChildPage || operation.Block.Type == graphBlockTypeInlineDatabase {
			return true
		}
		// A synced block reference's original is found as the body is written
		// (notionMarkdownSyncedReferences).
		if operation.Block.Type == graphBlockTypeSyncedBlock &&
			strings.Contains(string(operation.Block.Content), notionMarkdownSyncedAddressKey) {
			return true
		}
	}
	return false
}

// markdownPageBlocks turns the blocks a Markdown plan makes into a page's
// first body.
func markdownPageBlocks(operations []graphOperation, textUpdates []graphTextUpdate) ([]newRowPageBlock, error) {
	spans := make(map[string][]graphTextSpan, len(textUpdates))
	for _, update := range textUpdates {
		spans[update.BlockID] = update.Spans
	}
	blocks := []newRowPageBlock{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil {
			continue
		}
		content := map[string]any{}
		if len(operation.Block.Content) > 0 {
			if err := json.Unmarshal(operation.Block.Content, &content); err != nil {
				return nil, err
			}
		}
		parentID := ""
		if operation.Block.ParentID != nil {
			parentID = *operation.Block.ParentID
		}
		blocks = append(blocks, newRowPageBlock{
			ID: operation.Block.ID, ParentID: parentID, Position: operation.Block.Position,
			Type: operation.Block.Type, Content: content, Spans: spans[operation.Block.ID],
		})
	}
	return blocks, nil
}
