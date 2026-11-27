package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errMCPPageMissing      = errors.New(errorCodePageNotFound)
	errMCPCursorConflict   = errors.New(errorCodeCursorConflict)
	errMCPMutationIDReused = errors.New(errorCodeMutationIdentityReused)
	errMCPPageUpdateFailed = errors.New(errorCodePageUpdateFailed)
)

type notionMCPFetchInput struct {
	ID string `json:"id" jsonschema:"Notion page ID or URL"`
	// IncludeBlocks lists a page's blocks, which Markdown doesn't name.
	IncludeBlocks bool `json:"include_blocks,omitzero" jsonschema:"Also list the page's blocks with their IDs and addresses (blocks): a block's address links to it, and its ID names it to leafen_call, as to move it to another page"`
	// BlocksCursor lists on from where the last fetch's blocks stopped.
	BlocksCursor string `json:"blocks_cursor,omitempty" jsonschema:"With include_blocks, the blocks_next_cursor the last fetch gave, to list the blocks after those"`
	// IncludeDiscussions names a page's discussions (mcp_fetch_discussions.go).
	IncludeDiscussions bool `json:"include_discussions,omitzero" jsonschema:"Also name the page's discussions: a <page-discussions> with how many are open and the first few, and in its content each run of words one was left on in <span discussion-urls>, whose discussion:// addresses notion-get-comments takes"`
	IncludeFileURLs    bool `json:"include_file_urls,omitzero" jsonschema:"Also give a download address for each uploaded file the Markdown names by its notion-file-block:// address: references maps each to one, which expires in a few minutes"`
}

type leafenMCPFetchInput struct {
	ID          string `json:"id" jsonschema:"Leafen page ID or address (/p/…)"`
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"The workspace holding the page, to check it is there; may be left out, the page's ID names it"`
}

type mcpFetchInput struct {
	ID          string
	WorkspaceID string
	// Discussions names the runs of words open discussions were left on in
	// the content (mcpFetchedDiscussions); nil names none.
	Discussions func(threads, blockID string) string
}

type notionMCPUpdatePageInput struct {
	PageID                 string                        `json:"page_id" jsonschema:"Notion page ID"`
	Command                string                        `json:"command" jsonschema:"Page mutation command: replace_content, insert_content, update_content, update_properties, apply_template or update_verification"`
	NewString              string                        `json:"new_str,omitempty" jsonschema:"Required for replace_content: the complete replacement content, in Leafen page Markdown (notion://docs/notion-flavored-markdown-spec); \"\" empties the page. A long one can come from a file: new_str_file_upload_id"`
	NewStrFileUploadID     string                        `json:"new_str_file_upload_id,omitempty" jsonschema:"For replace_content in place of new_str, for content too long to write in the call (tens of thousands of characters): the file_upload_id of a .md file this connection uploaded — notion-create-file-upload gives upload_url and file_upload_id, and the file goes to upload_url (curl -F file=@body.md <upload_url>). Its text is the replacement content, read and checked as new_str is"`
	Content                string                        `json:"content,omitempty" jsonschema:"Required for insert_content: the content to insert, in Leafen page Markdown (notion://docs/notion-flavored-markdown-spec)"`
	Position               *notionMCPInsertPosition      `json:"position,omitempty" jsonschema:"Where insert_content puts the content; omitted appends"`
	ContentUpdates         []notionMarkdownContentUpdate `json:"content_updates,omitempty" jsonschema:"Required for update_content: search-and-replace operations, made in turn; each old_str must match the page content exactly, once unless replace_all_matches is true. One that matches nothing is left out and named in warnings; when none matches, the update is refused"`
	AllowDeletingContent   bool                          `json:"allow_deleting_content,omitzero" jsonschema:"Let replace_content and update_content remove sub-pages, databases and other content the new Markdown doesn't keep"`
	Properties             map[string]any                `json:"properties,omitempty" jsonschema:"For update_properties: an ordinary page takes only title, in inline Markdown; a database page takes its properties as SQLite values by name (a title or text in inline Markdown, date:{name}:start/end/is_datetime (a time without an offset is in the person's time zone) and Leafen's date:{name}:deadline — days before it to notify, as \"7,3,1,0\", \"\" to clear — place:{name}:…, __YES__/__NO__ for checkboxes, lists for multi-select, relations and people, null to clear)"`
	Icon                   *string                       `json:"icon,omitempty" jsonschema:"An emoji, a custom emoji by name (:name:), a Notion icon identifier as fetch gives it (icons/pizza_blue), or an external image URL; none removes the icon. Can be set alongside any command"`
	Cover                  *string                       `json:"cover,omitempty" jsonschema:"An external image URL for the page cover; none removes it. Can be set alongside any command"`
	IsSkill                *bool                         `json:"is_skill,omitempty" jsonschema:"Leafen does not make pages Skills: true is refused as unsupported_feature, false changes nothing"`
	AllowAsync             *bool                         `json:"allow_async,omitempty" jsonschema:"Leafen finishes a page update within the call, so true and false answer the same"`
	VerificationStatus     string                        `json:"verification_status,omitempty" jsonschema:"For update_verification, on any page: verified or unverified; the verifier is the connection's person. A wiki's page, or a database page with a Verification property, takes it as that property's value; any other page as its own verification"`
	VerificationExpiryDays *int                          `json:"verification_expiry_days,omitempty" jsonschema:"For update_verification with verified: days from now until the verification expires, as the screen's 7, 30 and 90 days; leave it and verification_expires_on out for no expiry"`
	VerificationExpiresOn  string                        `json:"verification_expires_on,omitempty" jsonschema:"For update_verification with verified, instead of verification_expiry_days: the last day it stays verified, YYYY-MM-DD, tomorrow or later; it ends at that day's end in the connection person's time zone, as the screen's date picker sets it"`
	FullWidth              *bool                         `json:"full_width,omitempty" jsonschema:"With update_properties, set the page to full width or the default width"`
	SmallText              *bool                         `json:"small_text,omitempty" jsonschema:"With update_properties, set the page's small text on or off; fetch gives small_text while it is on"`
	Font                   *string                       `json:"font,omitempty" jsonschema:"With update_properties, the page's font: default, serif or mono; fetch gives font when it isn't default"`
	TemplateID             string                        `json:"template_id,omitempty" jsonschema:"For apply_template: one of the database page's data source templates, as fetch lists them; its body goes after the page's own and its values fill the properties the page leaves empty"`
}

type leafenMCPUpdatePageInput struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"The workspace holding the page, to check it is there; may be left out, the page's ID names it"`
	PageID      string `json:"page_id" jsonschema:"Leafen page ID or address (/p/…)"`
	Command     string `json:"command" jsonschema:"replace_content, or trash to put the page in the trash and restore to take it back out"`
	NewString   string `json:"new_str,omitempty" jsonschema:"Required for replace_content: the complete replacement content, in Leafen page Markdown (notion://docs/notion-flavored-markdown-spec); \"\" empties the page. A long one can come from a file: new_str_file_upload_id"`
	// NewStrFileUploadID names an uploaded file whose text is new_str (mcp_page_body_upload.go).
	NewStrFileUploadID string `json:"new_str_file_upload_id,omitempty" jsonschema:"For replace_content in place of new_str, for content too long to write in the call (tens of thousands of characters): the file_upload_id of a .md file this connection uploaded — notion-create-file-upload gives upload_url and file_upload_id, and the file goes to upload_url (curl -F file=@body.md <upload_url>). Its text is the replacement content, read and checked as new_str is, base_cursor too"`
	MutationID         string `json:"mutation_id,omitempty" jsonschema:"Optional durable idempotency identity"`
	BaseCursor         *int   `json:"base_cursor,omitempty" jsonschema:"Optional: the cursor leafen_fetch or a write's answer gave, as given; the write is refused if the page changed since — its body and what it shows of other pages, title, icon, cover or a database page's values"`
	// AllowDeletingContent lets replace_content remove what Markdown can't hold.
	AllowDeletingContent bool `json:"allow_deleting_content,omitzero" jsonschema:"Let replace_content remove sub-pages, databases and other content Markdown can't hold"`
}

type mcpUpdatePageInput struct {
	// FullWidth, SmallText and Font set the page's appearance.
	FullWidth            *bool
	SmallText            *bool
	Font                 *string
	WorkspaceID          string
	PageID               string
	Command              string
	NewString            string
	MutationID           string
	BaseCursor           *int
	AllowDeletingContent bool
	// Content and Position are insert_content's, ContentUpdates
	// update_content's, Properties update_properties' (an ordinary page's
	// title or a database page's SQLite values) and Verification
	// update_verification's.
	Content        string
	Position       string
	ContentUpdates []notionMarkdownContentUpdate
	Properties     map[string]any
	Verification   mcpPageVerification
	// Icon and Cover, as Notion's MCP names them, change the page's
	// presentation alongside the command (mcp_page_presentation.go).
	Icon, Cover *string
	// TemplateID is apply_template's (mcp_database_pages.go).
	TemplateID string
	// CreatedBody is a body notion-create-pages gives a page it made, whose
	// links become mentions unless PreserveInternalLinks keeps them
	// (mcpCreatedBodyInput).
	CreatedBody, PreserveInternalLinks bool
}

// notionMCPPageOutput is notion-update-page's answer, as Notion's (seen
// 2026-11-11: {"page_id": …}): the page's ID, and what the update left out
// or changed.
type notionMCPPageOutput struct {
	PageID string `json:"page_id"`
	// Warnings are what an update left out or changed, as Notion's MCP says
	// them (mcpMarkdownWarning).
	Warnings []mcpMarkdownWarning `json:"warnings,omitempty" jsonschema:"What the update left out or changed: an update_content replacement whose old_str the page didn't hold (no_match), and what reading the Markdown changed or kept as text (markdown_parser_warning, its category the kind)"`
}

// mcpPageMutationOutput is a page write's answer to an agent: the page with
// its body as Markdown, the write's receipt, and whether that body stops
// before the page's end.
type mcpPageMutationOutput struct {
	Page      pageResponse            `json:"page"`
	Receipt   mutationReceiptResponse `json:"receipt"`
	Truncated bool                    `json:"truncated,omitempty" jsonschema:"The page's content stops before its end: the page holds more blocks than one read gives"`
	Warnings  []mcpMarkdownWarning    `json:"warnings,omitempty" jsonschema:"What the change left out or changed: an update_content replacement whose old_str the page didn't hold (no_match), and what reading the Markdown changed or kept as text (markdown_parser_warning, its category the kind)"`
}

func registerMCPPageTools(
	server *mcp.Server,
	app core.App,
	readOnly *mcp.ToolAnnotations,
	surface mcpToolSurface,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-fetch", Title: "Fetch Leafen content",
		Description: "Fetch one page, database or block by ID or address, collection:// data source, or view:// saved view from the fixed Notion-compatible connection scope. The answer names what was read in metadata.type and wraps it in text as Notion's fetch does: a page's ancestors, properties (a database page's as SQLite values, followed by its data source's SQLite table), icon and Markdown content, and with include_discussions its open discussions; a database's data sources and views; a data source's configurable state and SQLite table; a view's configuration; a block's words and the blocks it holds. " +
			"Pages use Leafen page Markdown; pass " + notionPageMarkdownSpecURI + " as id to read its specification. " +
			"Uploaded files are named by stable notion-file-block:// addresses: keep them as written when editing or copying Markdown; notion-get-file-download-urls, or include_file_urls (a references map), gives their download addresses.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPFetchInput) (*mcp.CallToolResult, notionMCPFetchOutput, error) {
		result, output, err := fetchMCPNotionObject(ctx, app, input.ID, input.IncludeDiscussions)
		if err == nil {
			err = mcpFetchFileFacts(ctx, app, &output, input.ID, input.IncludeFileURLs)
		}
		if err == nil && input.IncludeBlocks && output.Metadata.Type == "page" {
			output.Blocks, output.BlocksNextCursor, err = mcpFetchedPageBlocks(ctx, app, mcpLocalID(app, input.ID), input.BlocksCursor)
		}
		return result, output, err
	})
	updateAnnotations := &mcp.ToolAnnotations{IdempotentHint: true}
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-update-page", Title: "Update Leafen page",
		Description: notionPageMarkdownSpecRule + " Apply one durable Notion-compatible page update through the native page service. " +
			"The content is kept as given: a first-level heading on its first line stays a heading (only notion-create-pages reads one as the title). " +
			"A body too long to write in the call comes from an uploaded .md file: new_str_file_upload_id. " + notionPageGrowingRecordRule,
		Annotations: updateAnnotations,
	}, func(ctx context.Context, request *mcp.CallToolRequest, input notionMCPUpdatePageInput) (*mcp.CallToolResult, notionMCPPageOutput, error) {
		output, err := updateNotionMCPPage(ctx, app, input, mcpCallArguments(request))
		return nil, output, mcpNamedRefusal(ctx, app, err, mcpPage(input.PageID))
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-duplicate-page", Title: "Duplicate a Leafen page",
		Description: "Duplicate a page beside it. The answer names the copy (page_id, page_url), which fills in after it; notion-get-async-task with that page_id says when it is done.",
	}, func(ctx context.Context, request *mcp.CallToolRequest, input notionMCPDuplicatePageInput) (*mcp.CallToolResult, notionMCPDuplicatePageOutput, error) {
		result, output, err := duplicateMCPPage(ctx, request, app, input)
		return result, output, mcpNamedRefusal(ctx, app, err, mcpPage(input.PageID))
	})
	if surface == mcpToolSurfaceNative {
		mcp.AddTool(server, &mcp.Tool{
			Name: "leafen_fetch", Title: "Fetch Leafen page",
			Description: "Fetch one page of a workspace this connection was given, its body in Leafen page Markdown (" + notionPageMarkdownSpecURI + "), the words discussions were left on in <span discussion-urls>; keep those spans when writing it back, or the discussions lose their place.",
			Annotations: readOnly,
		}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPFetchInput) (*mcp.CallToolResult, mcpFetchOutput, error) {
			// A page's address, as a person opens it, names it as its ID does.
			local := mcpLocalID(app, input.ID)
			result, output, err := fetchMCPPage(ctx, app, mcpFetchInput{ID: local, WorkspaceID: input.WorkspaceID})
			if errors.Is(err, errMCPCantRead) && (strings.HasPrefix(local, mcpDataSourceURLPrefix) || mcpDatabaseContainerExistsToRead(app, local)) {
				return nil, mcpFetchOutput{}, mcpInvalid("that is a database's address: leafen_fetch reads pages, notion-fetch reads databases")
			}
			if err != nil {
				return result, output, mcpNamedRefusal(ctx, app, err, mcpPage(input.ID))
			}
			authority, err := mcpAuthorityFromContext(ctx)
			if err != nil {
				return nil, mcpFetchOutput{}, err
			}
			output.Notice = mcpMaterialNotice
			if output.Properties, err = mcpFetchedPageProperties(app, authority, output.ID); err != nil {
				return nil, mcpFetchOutput{}, failedBecause(errMCPToolFailed, err)
			}
			output.FileVersions, err = mcpFetchFileVersions(ctx, app, output.Content, output.ID)
			return result, output, err
		})
		mcp.AddTool(server, &mcp.Tool{
			Name: "leafen_update_page", Title: "Update Leafen page",
			Description: notionPageMarkdownSpecRule + " Apply one durable Leafen page update with replay control: replace its content, or put it in the trash and take it back out. " +
				"A body too long to write in the call comes from an uploaded .md file: new_str_file_upload_id.",
			Annotations: updateAnnotations,
		}, func(ctx context.Context, request *mcp.CallToolRequest, input leafenMCPUpdatePageInput) (*mcp.CallToolResult, mcpPageMutationOutput, error) {
			var result *mcp.CallToolResult
			var output mcpPageMutationOutput
			var err error
			if command := strings.TrimSpace(input.Command); command == "trash" || command == "restore" {
				result, output, err = trashMCPPage(ctx, app, input.WorkspaceID, mcpLocalID(app, input.PageID), command == "trash", input.MutationID, input.BaseCursor)
			} else if err = mcpPageCommandRefusal(mcpCallArguments(request), command); err == nil {
				newString, bodyErr := mcpPageBody(ctx, app, "new_str", input.NewString, mcpArgumentGiven(mcpCallArguments(request), "new_str"), input.NewStrFileUploadID)
				if bodyErr != nil {
					return nil, mcpPageMutationOutput{}, mcpNamedRefusal(ctx, app, bodyErr, mcpPage(input.PageID))
				}
				result, output, err = updateMCPPage(ctx, app, mcpUpdatePageInput{
					WorkspaceID: input.WorkspaceID, PageID: mcpLocalID(app, input.PageID),
					Command: input.Command, NewString: newString,
					MutationID: input.MutationID, BaseCursor: input.BaseCursor,
					AllowDeletingContent: input.AllowDeletingContent,
				})
			}
			return result, output, mcpNamedRefusal(ctx, app, err, mcpPage(input.PageID))
		})
	}
}

func notionPageOutputFromMutation(output mcpPageMutationOutput) notionMCPPageOutput {
	return notionMCPPageOutput{PageID: output.Page.ID, Warnings: output.Warnings}
}

func fetchMCPPage(
	ctx context.Context,
	app core.App,
	input mcpFetchInput,
) (*mcp.CallToolResult, mcpFetchOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, mcpFetchOutput{}, err
	}
	pageID := strings.TrimSpace(input.ID)
	if pageID == "" {
		return nil, mcpFetchOutput{}, mcpInvalid("id names the page: its ID or address")
	}
	authorization, err := authorizeMCPPageRead(app, authority.PrincipalID, pageID)
	if err != nil {
		return nil, mcpFetchOutput{}, mcpPageReadRefusal(app, authority, pageID, err)
	}
	if !mcpAuthorityReachesPage(app, authority, authorization.Page) {
		return nil, mcpFetchOutput{}, errMCPOutsideTokenPages
	}
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if !mcpInWorkspace(app, workspaceID, authorization.Workspace.WorkspaceID) {
		return nil, mcpFetchOutput{}, mcpDenied("that page isn't in workspace_id's workspace")
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
		WorkspaceID: authorization.Workspace.WorkspaceID,
	}, permissionWorkspaceRead)
	if err != nil || authorization.Workspace.WorkspaceID != workspace.WorkspaceID {
		return nil, mcpFetchOutput{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	page, err := mcpPageRecordResponse(app, authorization.Page)
	if err != nil {
		return nil, mcpFetchOutput{}, failedBecause(errMCPToolFailed, err)
	}
	content, truncated, cursor, err := mcpFetchedPageBody(ctx, app, authority, authorization.Page, authorization.Workspace.OwnerID, input.Discussions)
	if err != nil {
		return nil, mcpFetchOutput{}, failedBecause(errMCPToolFailed, err)
	}
	// A template without a title of its own reads as its name in the menu.
	if isDatabaseTemplatePageRecord(authorization.Page) {
		_ = app.DB().NewQuery(`
			SELECT COALESCE(NULLIF([[title]], ''), [[name]]) FROM {{leafen_database_templates}} WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": page.ID}).Row(&page.Title)
	}
	kind := "page"
	if isDatabaseRowPageRecord(authorization.Page) {
		kind = "row"
		// Its title cell's mention of a page closed to the agent reads
		// "Untitled" (mcp_reader_mentions.go).
		page.Title = mcpRowTitleAsRead(app, mcpReaderCloses(app, authority), page.ID, page.Title)
	}
	recordMCPReads(app, authority, workspace.WorkspaceID, []contentRead{{Kind: kind, ID: page.ID, How: contentReadOpen}})
	return nil, mcpFetchOutput{
		ID: page.ID, WorkspaceID: page.WorkspaceID, Title: page.Title,
		Content: content, URL: mcpPageAddress(app, authority, page.ID),
		Revision: page.Revision, Cursor: cursor, Truncated: truncated, InTrash: page.InTrash,
	}, nil
}

// mcpPageBodyMarkdown reads a page's body as Markdown for someone, as the
// Public API reads it.
func mcpPageBodyMarkdown(app core.App, reader notionMarkdownReader, ownerID, pageID string) (string, error) {
	markdown, _, err := mcpPageBodyMarkdownRead(app, reader, ownerID, pageID)
	return markdown, err
}

// mcpPageBodyMarkdownRead is the same, and whether the Markdown stops before
// the page's end, as the Public API's truncated says.
func mcpPageBodyMarkdownRead(app core.App, reader notionMarkdownReader, ownerID, pageID string) (string, bool, error) {
	projection := notionPageMarkdownProjection{}
	err := runNotionReadSnapshot(context.Background(), app, func(txApp core.App) error {
		var err error
		projection, err = projectNotionPageMarkdown(txApp, ownerID, pageID, pageID, reader)
		return err
	})
	return projection.Markdown, projection.Truncated, err
}

// updateMCPPage replaces a page's body with Markdown under the page's body
// permission, in a workspace the connection may write. A request replayed
// under its mutation ID answers as it did, and one that would leave the body
// as it is changes nothing.
func updateMCPPage(
	ctx context.Context,
	app core.App,
	input mcpUpdatePageInput,
) (*mcp.CallToolResult, mcpPageMutationOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, mcpPageMutationOutput{}, err
	}
	pageID := strings.TrimSpace(input.PageID)
	command := strings.TrimSpace(input.Command)
	input.Command = command
	if (command == "apply_template") != (strings.TrimSpace(input.TemplateID) != "") {
		return nil, mcpPageMutationOutput{}, mcpInvalid("apply_template goes with template_id, and template_id only with apply_template")
	}
	presenting := input.Icon != nil || input.Cover != nil || input.FullWidth != nil || input.SmallText != nil || input.Font != nil
	change, requestHash, changesMarkdown := mcpPageMarkdownChange(pageID, input)
	properties, verifying := command == "update_properties", command == "update_verification"
	applying := command == "apply_template"
	if pageID == "" || !changesMarkdown && !verifying && !applying && (!properties || !presenting && input.Properties == nil) ||
		!properties && len(input.Properties) > 0 || verifying != input.Verification.given() ||
		input.BaseCursor != nil && *input.BaseCursor < 0 {
		return nil, mcpPageMutationOutput{}, mcpInvalid("page_id names the page, and command is one the description lists, with what it needs: new_str or the content parts for content, properties (or icon, cover) for update_properties, verification for update_verification; base_cursor is 0 or more")
	}
	if input.Font != nil && !validPageFont(*input.Font) {
		return nil, mcpPageMutationOutput{}, mcpInvalid("font is default, serif or mono")
	}
	mutationID := strings.TrimSpace(input.MutationID)
	if mutationID == "" {
		// A call without an ID is a change of its own: content written
		// before is written again, and a body that already holds it stays.
		mutationID = "mcp-page-update:" + newDiagnosticReference()
	}
	if len([]rune(mutationID)) > 255 {
		return nil, mcpPageMutationOutput{}, mcpInvalid("mutation_id is at most 255 characters")
	}
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return nil, mcpPageMutationOutput{}, errMCPPageUpdateFailed
	}
	// A page's write, this one's or a synced block original's its Markdown
	// changes, is admitted as the page's own.
	authorizeWrite := func(pageID string) func(core.App) (pageAuthorization, error) {
		return func(txApp core.App) (pageAuthorization, error) {
			return authorizeMCPPageWrite(txApp, authority, input.WorkspaceID, pageID)
		}
	}
	authorize := authorizeWrite(pageID)
	if properties || verifying {
		return updateMCPPagePropertiesBringing(ctx, app, authority, pageID, mutationID, input, authorize)
	}
	if applying {
		return applyMCPPageTemplate(ctx, app, authority, pageID, mutationID, input, authorize)
	}
	// The replacements update_content left out are said beside the page,
	// then what reading the Markdown as "v2" said.
	matchWarnings := []notionMarkdownWarning{}
	prepare := narrowedMarkdownMentionInput(authority.PrincipalID, mcpMentionReaches(authority))
	reader, err := mcpWriteReader(app, authority, pageID)
	if err != nil {
		return nil, mcpPageMutationOutput{}, err
	}
	if input.CreatedBody {
		reader.LinksMentioned = !input.PreserveInternalLinks
		prepare = mcpCreatedBodyInput(authority, input.PreserveInternalLinks, prepare)
	}
	mcpBringPageUploads(app, authority, authorize, input) // uploads from other workspaces (mcp_file_attach.go)
	result, err := persistence.replacePageMarkdown(ctx, app, pageMarkdownReplacement{
		PrincipalID: authority.PrincipalID, PublicPageID: pageID, MutationID: mutationID,
		Reader: reader, Version2: true,
		PrepareInput: prepare,
		Uploads:      mcpMarkdownUploads(authority),
		// A base_cursor is the version fetch gave (mcp_page_version.go), which the page must still be at.
		RequestHash: requestHash, BaseVersion: input.BaseCursor,
		SkipUnchanged: input.BaseCursor == nil, Authorize: authorize,
		SyncedOriginal: func(_ core.App, pageID string) (string, func(core.App) (pageAuthorization, error), error) {
			return pageID, authorizeWrite(pageID), nil
		},
		Next: func(current string) (string, bool, error) {
			next, allowDeleting, warnings, err := applyNotionMarkdownChange(current, change)
			matchWarnings = warnings
			return next, allowDeleting, err
		},
	})
	if err != nil {
		return nil, mcpPageMutationOutput{}, mcpPageMarkdownError(err)
	}
	presented := mcpPageMutationOutput{}
	if presenting {
		if _, presented, err = updateMCPPageProperties(ctx, app, authority, pageID, mutationID, input, authorize); err != nil {
			return nil, mcpPageMutationOutput{}, err
		}
	}
	page, err := mcpPageResponse(app, pageID)
	if err == nil {
		page.Cursor, err = mcpWrittenPageCursor(app, pageID, result)
	}
	if err != nil {
		return nil, mcpPageMutationOutput{}, failedBecause(errMCPPageUpdateFailed, err)
	}
	// A replayed request reads the body its first answer made.
	page.Content = result.After.Markdown
	if result.Receipt.Duplicate && page.Content == "" {
		page.Content = result.Before.Markdown
	}
	return nil, mcpPageMutationOutput{
		Page: page, Warnings: append(mcpMarkdownWarnings(matchWarnings, result.Warnings), presented.Warnings...),
		Receipt: mutationReceiptResponse{
			MutationID: mutationID, Cursor: page.Cursor, Duplicate: result.Receipt.Duplicate,
		},
	}, nil
}

// mcpPageMarkdownError names what stopped a page's body replacement.
func mcpPageMarkdownError(err error) error {
	requestError := &graphRequestError{}
	markdownRequest, markdownPlan, markdownMatch := pageMarkdownRequestError{}, pageMarkdownPlanError{}, pageMarkdownMatchError{}
	switch {
	case errors.Is(err, errMCPScopeDenied), errors.Is(err, errMCPToolInvalid), errors.Is(err, errMCPPageMissing):
		// A refusal that says why already, as it was made.
		return err
	case isDLPPolicyDecisionError(err):
		return mcpDLPError(err)
	case errors.Is(err, errPageMarkdownUnsafe), errors.As(err, &markdownRequest), errors.As(err, &markdownPlan),
		errors.As(err, &markdownMatch):
		// The client reads why, as Notion's tools tell it, with their data (mcp_tool_error_data.go).
		return fmt.Errorf("%w: %w", errMCPToolInvalid, err)
	case errors.As(err, &requestError):
		switch {
		case requestError.code == errorCodeGraphMutationIDReused:
			return errMCPMutationIDReused
		case requestError.status == http.StatusNotFound:
			return errMCPPageMissing
		case requestError.status == http.StatusConflict:
			return errMCPCursorConflict
		case requestError.code == errorCodeGraphReferenceInvalid:
			// A file or mention the Markdown names that isn't there.
			return fmt.Errorf("%w: %s", errMCPToolInvalid, requestError.message)
		case requestError.status == http.StatusBadRequest:
			return mcpGraphReason(errMCPToolInvalid, err)
		}
	}
	return failedBecause(errMCPPageUpdateFailed, err)
}

// authorizeMCPPageWrite admits a write of a page's body under the page's body
// permission, in a workspace the connection may write (and workspace_id's,
// when given), the page out of the trash.
func authorizeMCPPageWrite(txApp core.App, authority mcpAuthority, workspaceID, pageID string) (pageAuthorization, error) {
	current, err := authorizePageBody(txApp, authority.PrincipalID, pageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return pageAuthorization{}, errMCPCantChange
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	if !mcpAuthorityReachesPage(txApp, authority, current.Page) {
		return pageAuthorization{}, errMCPOutsideTokenPages
	}
	if workspaceID := strings.TrimSpace(workspaceID); !mcpInWorkspace(txApp, workspaceID, current.Workspace.WorkspaceID) {
		return pageAuthorization{}, mcpDenied("that page isn't in workspace_id's workspace")
	}
	workspace, err := selectMCPWorkspace(txApp, authority, mcpWorkspaceInput{
		WorkspaceID: current.Workspace.WorkspaceID,
	}, permissionWorkspaceWrite)
	if err != nil || workspace.WorkspaceID != current.Workspace.WorkspaceID {
		return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	if trashed, err := pageInTrashPath(txApp, pageID); err != nil {
		return pageAuthorization{}, err
	} else if trashed {
		return pageAuthorization{}, graphNotFoundError()
	}
	return current, nil
}
