package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionMCPGetCommentsInput struct {
	PageID string `json:"page_id" jsonschema:"Page ID"`
	// IncludeAllBlocks is kept for callers that send it: the list holds
	// block and inline discussions either way, as Notion's does.
	IncludeAllBlocks bool   `json:"include_all_blocks,omitzero" jsonschema:"Kept for older callers: block and inline discussions are listed either way"`
	IncludeResolved  bool   `json:"include_resolved,omitzero" jsonschema:"Include resolved discussions"`
	DiscussionID     string `json:"discussion_id,omitempty" jsonschema:"Specific discussion ID or discussion:// URL"`
	// StartCursor goes on where the last answer stopped (mcp_tool_comments_window.go).
	StartCursor string `json:"start_cursor,omitempty" jsonschema:"The next_cursor the last answer gave, to list the discussions after those it wrote"`
}

type notionMCPCreateCommentInput struct {
	PageID                string           `json:"page_id" jsonschema:"Page ID"`
	SelectionWithEllipsis string           `json:"selection_with_ellipsis,omitempty" jsonschema:"Unique page content selection, optionally using an ellipsis"`
	DiscussionID          string           `json:"discussion_id,omitempty" jsonschema:"Existing discussion ID or discussion:// URL to reply to"`
	ReplyToCommentID      string           `json:"reply_to_comment_id,omitempty" jsonschema:"The ID of a comment to reply to, as notion-get-comments gives it"`
	Markdown              *string          `json:"markdown,omitempty" jsonschema:"Notion-flavored inline Markdown comment body"`
	RichText              []map[string]any `json:"rich_text,omitempty" jsonschema:"Instead of markdown, Notion rich text: a list of items such as {\"type\": \"text\", \"text\": {\"content\": \"words\", \"link\": {\"url\": \"https://…\"}}, \"annotations\": {\"bold\": true}}, {\"type\": \"mention\", \"mention\": {\"user\" or \"page\" or \"database\": {\"id\": \"…\"}} or {\"date\": {\"start\": \"2026-11-27\"}}} or {\"type\": \"equation\", \"equation\": {\"expression\": \"E=mc^2\"}}"`
}

// notionMCPComment is a comment as get-comments and fetch write it
// (mcp_tool_comment_projection.go).
type notionMCPComment struct {
	ID          string
	CreatedTime string
	// URL opens its discussion; UserURL names its author as user://ID, with
	// their email where the reader's directory gives it.
	URL, UserURL, Email string
	ByYou               bool
	// Body is its text as inline "v2" page Markdown.
	Body string
	// Files and Images count the files attached to it: pictures, and the rest.
	Files, Images int
	// Its files' download addresses, and its reactions (mcp_tool_comment_reactions_read.go).
	FileURLs                 []string
	Reactions, YourReactions string
}

// notionMCPDiscussion is a discussion: on the page, on a block, or on
// words of a block (inline), which its first comment quotes.
type notionMCPDiscussion struct {
	DiscussionID string
	RootID       string
	BlockID      string
	Resolved     bool
	Context      string
	TextContext  string
	// Kind is suggested_edit for a pending suggested edit's thread, whose
	// address notion-get-suggested-edit reads.
	Kind     string
	Comments []notionMCPComment
}

// notionMCPGetCommentsOutput is get-comments' answer, as Notion's (seen
// 2026-11-11): the discussions as text, none when there are none.
type notionMCPGetCommentsOutput struct {
	// SuggestedEditsStatus says suggested edits are listed, as Notion's
	// answer says whether they could be (available).
	SuggestedEditsStatus string `json:"suggested_edits_status,omitempty"`
	Text                 string `json:"text,omitempty"`
	// NextCursor, Leafen's own, names where the discussions go on when not
	// all of them fit in one answer (mcp_tool_comments_window.go).
	NextCursor string `json:"next_cursor,omitempty" jsonschema:"More discussions follow those written: call again with this as start_cursor"`
}

// notionMCPCreateCommentOutput is create-comment's answer, as Notion's (seen
// 2026-11-11): the new comment and the discussion it is in, by their IDs;
// and Leafen's warnings, what reading its markdown changed or kept as text
// (mcpInlineMarkdownSpans).
type notionMCPCreateCommentOutput struct {
	Result   notionMCPCreateCommentResult `json:"result"`
	Warnings []mcpMarkdownWarning         `json:"warnings,omitempty" jsonschema:"What reading the markdown changed or kept as text, as notion-update-page says it of a page's (markdown_parser_warning, its category the kind)"`
}

type notionMCPCreateCommentResult struct {
	Status       string `json:"status"`
	ID           string `json:"id"`
	DiscussionID string `json:"discussion_id"`
}

type mcpCommentTargetAuthorization struct {
	Authorization commentAuthorization
	PageID        string
	// CommentID is the comment a reply's or a change's ID names, its own ID.
	CommentID string
}

func registerMCPCommentTools(
	server *mcp.Server,
	app core.App,
	readOnly *mcp.ToolAnnotations,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-get-comments", Title: "Get Leafen comments",
		Description: "List a page's discussions — on the page, on its blocks and on words in them — as Notion's get-comments writes them: <discussions> in the page's order, each comment with its address, its author (user://), its time, whether you wrote it (by-you), its reactions (reactions, and reacted-by-you for yours) and its text as inline Markdown. include_resolved adds resolved ones; discussion_id names one. An answer holds as many discussions as fit, shown-count of total-count; next_cursor, when more follow, goes on as start_cursor.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPGetCommentsInput) (*mcp.CallToolResult, notionMCPGetCommentsOutput, error) {
		result, output, err := getMCPComments(ctx, app, input)
		return result, output, mcpNamedRefusal(ctx, app, err, mcpPage(input.PageID))
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-create-comment", Title: "Create Leafen comment",
		Description: "Add a comment, as Notion's create-comment does: given the page alone, to the page's open discussion (a new one when it has none); with selection_with_ellipsis — looked for in the page as notion-fetch writes it, else among its words —, on the words it names within one block, or on the block where the match starts when it runs past that block; with discussion_id or reply_to_comment_id (a comment's id from notion-get-comments), as a reply in that discussion, wherever it is. markdown is inline Notion-flavored Markdown, read as a page's text is: marks, links, $math$, <mention url=\"…\" /> and <date start=\"…\" />; a file made by notion-create-file-upload or notion-create-attachment is attached by its suggested_markdown on a line of its own, at most three. rich_text takes text, equations and mentions of users, pages, databases, dates and custom emoji.",
	}, func(ctx context.Context, request *mcp.CallToolRequest, input notionMCPCreateCommentInput) (*mcp.CallToolResult, notionMCPCreateCommentOutput, error) {
		result, output, err := createMCPComment(ctx, request, app, input)
		return result, output, mcpNamedRefusal(ctx, app, err, mcpPage(input.PageID))
	})
}

func authorizeMCPCommentPage(
	app core.App, authority mcpAuthority, pageID string, requirement permissionRequirement,
) (commentAuthorization, error) {
	authorization, err := authorizeCommentPage(app, authority.PrincipalID, pageID, requirement, "")
	if err != nil {
		if isPermissionDenied(err) {
			return commentAuthorization{}, mcpDenied("this account can't comment on that page, or can't open it")
		}
		return commentAuthorization{}, mcpCommentError(err)
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
		WorkspaceID: authorization.Page.Workspace.WorkspaceID,
	}, permissionWorkspaceRead)
	if err != nil || workspace.WorkspaceID != authorization.Page.Workspace.WorkspaceID {
		return commentAuthorization{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	if !mcpAuthorityReachesComment(app, authority, authorization) {
		return commentAuthorization{}, errMCPOutsideTokenPages
	}
	if err := mcpCommentTargetInTrash(app, authorization.Target, requirement); err != nil {
		return commentAuthorization{}, err
	}
	return authorization, nil
}

// mcpAuthorityReachesComment says whether a token narrowed to chosen pages
// reaches what a comment is on: the page, or a database's row under them.
func mcpAuthorityReachesComment(app core.App, authority mcpAuthority, authorization commentAuthorization) bool {
	return mcpAuthorityReachesPage(app, authority, authorization.Page.Page) ||
		authorization.Target.Kind == commentTargetDatabaseRow && mcpAuthorityReachesPageID(app, authority, authorization.Target.ID)
}

func authorizeMCPCommentTargetCurrent(
	app core.App,
	authority mcpAuthority,
	target nativeCommentTarget,
	requirement permissionRequirement,
) (commentAuthorization, error) {
	var authorization commentAuthorization
	var err error
	if target.Kind == commentTargetDatabaseRow {
		authorization, _, err = authorizeDatabaseRowCommentTarget(
			app, authority.PrincipalID, target.ID, requirement,
		)
	} else {
		authorization, err = authorizeCommentPage(
			app, authority.PrincipalID, target.ID, requirement, "",
		)
	}
	if err != nil {
		if isPermissionDenied(err) || errors.Is(err, errDatabaseRowPageNotFound) {
			return commentAuthorization{}, mcpDenied("this account can't comment on that item, or it is gone")
		}
		return commentAuthorization{}, mcpCommentError(err)
	}
	if authorization.Target != target {
		return commentAuthorization{}, mcpDenied("that discussion isn't on this page")
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
		WorkspaceID: authorization.Page.Workspace.WorkspaceID,
	}, permissionWorkspaceRead)
	if err != nil || workspace.WorkspaceID != authorization.Page.Workspace.WorkspaceID {
		return commentAuthorization{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	if !mcpAuthorityReachesComment(app, authority, authorization) {
		return commentAuthorization{}, errMCPOutsideTokenPages
	}
	if err := mcpCommentTargetInTrash(app, authorization.Target, requirement); err != nil {
		return commentAuthorization{}, err
	}
	return authorization, nil
}

func authorizeMCPCommentTarget(
	app core.App,
	authority mcpAuthority,
	pageID string,
	requirement permissionRequirement,
) (mcpCommentTargetAuthorization, error) {
	// The public API's IDs and a page's address name it as the tools' do (mcpLocalID).
	pageID = mcpLocalID(app, strings.TrimSpace(pageID))
	if !validCommentIdentifier(pageID) {
		return mcpCommentTargetAuthorization{}, mcpInvalid("page_id is a page ID as Leafen's tools give it")
	}
	// A database page takes comments as its database's row, as in the app.
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return mcpCommentTargetAuthorization{}, failedBecause(errMCPToolFailed, err)
	}
	target := nativePageCommentTarget(pageID)
	if page == nil || isDatabaseRowPageRecord(page) {
		rowID := pageID
		if page == nil {
			if identity, err := resolveNotionObjectIdentity(
				app, notionObjectIdentityRowPage, pageID,
			); err == nil {
				rowID = identity.InternalID
			} else if !errors.Is(err, errInvalidNotionObjectIdentity) {
				return mcpCommentTargetAuthorization{}, failedBecause(errMCPToolFailed, err)
			}
		}
		row, err := authorizeDatabaseRowPage(
			app, authority.PrincipalID, rowID, permissionWorkspaceRead,
		)
		if err != nil {
			if isPermissionDenied(err) || errors.Is(err, errDatabaseRowPageNotFound) {
				return mcpCommentTargetAuthorization{}, mcpDenied("this account can't comment on that item, or it is gone")
			}
			return mcpCommentTargetAuthorization{}, failedBecause(errMCPToolFailed, err)
		}
		target = nativeCommentTarget{
			Kind: commentTargetDatabaseRow, ID: row.RowID, StoragePageID: row.ContainerPageID,
		}
	}
	authorization, err := authorizeMCPCommentTargetCurrent(app, authority, target, requirement)
	if err != nil {
		return mcpCommentTargetAuthorization{}, err
	}
	return mcpCommentTargetAuthorization{Authorization: authorization, PageID: pageID}, nil
}

func getMCPComments(
	ctx context.Context,
	app core.App,
	input notionMCPGetCommentsInput,
) (*mcp.CallToolResult, notionMCPGetCommentsOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPGetCommentsOutput{}, err
	}
	target, err := authorizeMCPCommentTarget(
		app, authority, input.PageID, permissionWorkspaceRead,
	)
	if err != nil {
		return nil, notionMCPGetCommentsOutput{}, err
	}
	records, err := listAllCommentRecordsForTarget(
		app, target.Authorization.Target, mcpCommentReadAuthorizer(authority, target.Authorization.Target),
	)
	if err != nil {
		return nil, notionMCPGetCommentsOutput{}, mcpCommentError(err)
	}
	// One discussion asked for by its ID is written from the start.
	window := &mcpDiscussionWindow{after: strings.TrimSpace(input.StartCursor)}
	if strings.TrimSpace(input.DiscussionID) != "" {
		window.after = ""
	}
	discussions, err := notionMCPDiscussions(app, authority, target, records, input, window)
	if err != nil {
		return nil, notionMCPGetCommentsOutput{}, err
	}
	return nil, notionMCPGetCommentsOutput{
		SuggestedEditsStatus: "available", Text: window.text(discussions), NextCursor: window.nextCursor(),
	}, nil
}

func createMCPComment(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	app core.App,
	input notionMCPCreateCommentInput,
) (*mcp.CallToolResult, notionMCPCreateCommentOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPCreateCommentOutput{}, err
	}
	selection := strings.TrimSpace(input.SelectionWithEllipsis)
	discussionInput := strings.TrimSpace(input.DiscussionID)
	replyInput := strings.TrimSpace(input.ReplyToCommentID)
	// One way of placing it, refused as Notion's refuses two (seen 2026-11-11).
	switch {
	case replyInput != "" && discussionInput != "":
		return nil, notionMCPCreateCommentOutput{}, mcpInvalid("%s", notionFaultOnlyOne("reply_to_comment_id", "discussion_id"))
	case replyInput != "" && selection != "":
		return nil, notionMCPCreateCommentOutput{}, mcpInvalid("%s", notionFaultOnlyOne("reply_to_comment_id", "selection_with_ellipsis"))
	case discussionInput != "" && selection != "":
		return nil, notionMCPCreateCommentOutput{}, mcpInvalid("%s", notionFaultOnlyOne("discussion_id", "selection_with_ellipsis"))
	case utf8.RuneCountInString(selection) > 4096:
		return nil, notionMCPCreateCommentOutput{}, mcpInvalid("selection_with_ellipsis has at most 4096 characters")
	}
	var target mcpCommentTargetAuthorization
	replyTo, replyKind := replyInput, "comment"
	if discussionInput != "" {
		replyTo, replyKind = discussionInput, "discussion"
		if _, _, id, err := parseMCPDiscussionID(app, authority, discussionInput); err == nil {
			replyTo = id
		}
	}
	if replyTo != "" {
		target, err = authorizeMCPReplyTarget(app, authority, replyTo, replyKind)
	} else {
		target, err = authorizeMCPCommentTarget(app, authority, input.PageID, permissionWorkspaceWrite)
	}
	if err != nil {
		return nil, notionMCPCreateCommentOutput{}, err
	}

	// Where it goes is read for it alone (mcp_tool_comment_place.go).
	blockID, parentID, quote := "", "", ""
	var quoteRange *mcpCommentQuoteRange
	switch {
	case replyTo != "":
		root, found, err := mcpCommentReplyRoot(app, authority, target.Authorization.Target, target.CommentID)
		if err != nil {
			return nil, notionMCPCreateCommentOutput{}, mcpCommentError(err)
		}
		if !found {
			return nil, notionMCPCreateCommentOutput{}, mcpReplyTargetMissing(replyTo, replyKind)
		}
		blockID, parentID = root.BlockID, root.ID
	case selection != "":
		// As given: spaces before or after its words count.
		blockID, quote, quoteRange, err = findMCPCommentSelectionBlockForTarget(
			app, target.Authorization.Target, input.SelectionWithEllipsis, mcpReaderCloses(app, authority),
			mcpSelectionPageOf(authority, target.Authorization.Page.Workspace),
		)
		if err != nil {
			return nil, notionMCPCreateCommentOutput{}, mcpCommentError(err)
		}
	default:
		// The page's open discussion takes it, as Notion's does.
		root, found, err := mcpTargetOpenPageDiscussion(app, authority, target.Authorization.Target)
		if err != nil {
			return nil, notionMCPCreateCommentOutput{}, mcpCommentError(err)
		}
		if found {
			parentID = root.ID
		}
	}
	said := &mcpMarkdownSaid{}
	body, err := mcpCommentBody(
		app, authority, target.Authorization.Page,
		input.Markdown, input.RichText, quote, quoteRange, said,
	)
	if err != nil {
		return nil, notionMCPCreateCommentOutput{}, mcpReasoned(err)
	}
	commentID, requestID := mcpCommentMutationIDs(authority)
	response, err := createConnectedComment(
		ctx, app, authority.PrincipalID, target.Authorization.Target,
		commentCreateInput{
			ID: commentID, RequestID: requestID, BlockID: blockID,
			ParentID: parentID, Body: body,
		},
		func(txApp core.App) (commentAuthorization, error) {
			return authorizeMCPCommentTargetCurrent(
				txApp, authority, target.Authorization.Target, permissionWorkspaceWrite,
			)
		},
	)
	if err != nil {
		return nil, notionMCPCreateCommentOutput{}, mcpCommentError(err)
	}
	// A thread left on text marks that text, as a person's does.
	if persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence); ok && quoteRange != nil && !response.Duplicate {
		if err := persistence.anchorCommentText(ctx, authority.PrincipalID, response.Comment, func(txApp core.App) (commentAuthorization, error) {
			return authorizeMCPCommentTargetCurrent(txApp, authority, target.Authorization.Target, permissionWorkspaceWrite)
		}); err != nil {
			return nil, notionMCPCreateCommentOutput{}, mcpCommentError(err)
		}
	}
	// The discussion is named by its own ID, apart from its first
	// comment's, as Notion's create-comment gives it (seen 2026-11-13).
	rootID := response.Comment.ID
	if parentID != "" {
		rootID = parentID
	}
	return nil, notionMCPCreateCommentOutput{Result: notionMCPCreateCommentResult{
		Status: "success", ID: mcpCommentID(app, response.Comment.WorkspaceID, response.Comment.ID),
		DiscussionID: mcpDiscussionKey(app, response.Comment.WorkspaceID, rootID),
	}, Warnings: said.answer()}, nil
}

func mcpCommentMutationIDs(authority mcpAuthority) (string, string) {
	digest := sha256.Sum256([]byte("leafen:mcp-comment:v1\x00" + authority.ConnectionID + "\x00" + mcpCallSeed()))
	hexDigest := hex.EncodeToString(digest[:])
	// A new comment's ID is a UUID, as the screen's (newId) and Notion's are.
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(hexDigest)).String(), "mcp-comment-request-" + hexDigest
}

func mcpCommentError(err error) error {
	switch {
	case err == nil:
		return nil
	case isPermissionDenied(err):
		return mcpDenied("this account can't comment there")
	case errors.Is(err, errCommentInvalid), errors.Is(err, errCommentParentScope),
		errors.Is(err, errCommentBlockScope):
		return fmt.Errorf("%w: %s", errMCPToolInvalid, notionCommentRefusalReason(err))
	case errors.Is(err, errCommentNotFound):
		return mcpInvalid("that comment or discussion is gone: notion-get-comments lists what is there")
	default:
		return mcpReasoned(err)
	}
}

// mcpDiscussionRootID is the first comment of the discussion an ID names
// in the connection's workspaces: a discussion's own ID read back, or a
// comment's ID as it is (addresses given before named a discussion by its
// first comment, and a reply may name any of its comments).
func mcpDiscussionRootID(app core.App, authority mcpAuthority, id string) string {
	if len(authority.WorkspaceIDs) == 0 {
		return id
	}
	workspaces := make([]any, 0, len(authority.WorkspaceIDs))
	for _, workspaceID := range authority.WorkspaceIDs {
		workspaces = append(workspaces, workspaceID)
	}
	rootID := ""
	if err := app.DB().Select("id").From("leafen_comments").
		Where(dbx.And(dbx.In("workspace_id", workspaces...), dbx.HashExp{"notion_discussion_id": id, "parent_id": ""})).
		OrderBy("created_at", "id").Limit(1).Row(&rootID); err != nil || rootID == "" {
		return id
	}
	return rootID
}

// parseMCPDiscussionID reads a discussion's address, or its bare ID, as its
// page, its block and its first comment (mcpDiscussionRootID).
func parseMCPDiscussionID(app core.App, authority mcpAuthority, raw string) (string, string, string, error) {
	pageID, blockID, id, err := parseMCPDiscussionAddress(raw)
	if err != nil {
		return "", "", "", err
	}
	return pageID, blockID, mcpDiscussionRootID(app, authority, id), nil
}

func parseMCPDiscussionAddress(raw string) (string, string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", mcpInvalid("discussion_id is a discussion://… address or ID, as notion-get-comments gives it")
	}
	if !strings.HasPrefix(raw, "discussion://") {
		if !validCommentIdentifier(raw) {
			return "", "", "", mcpInvalid("discussion_id is a discussion://… address or ID, as notion-get-comments gives it")
		}
		return "", "", raw, nil
	}
	parsed, err := url.Parse(raw)
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if err != nil || parsed.Host == "" || len(parts) != 2 ||
		!validCommentIdentifier(parts[1]) {
		return "", "", "", mcpInvalid("discussion_id is a discussion://… address or ID, as notion-get-comments gives it")
	}
	// "page" named the page's own in addresses given before.
	blockID := parts[0]
	if blockID == "page" || blockID == parsed.Host {
		blockID = ""
	}
	return parsed.Host, blockID, parts[1], nil
}
