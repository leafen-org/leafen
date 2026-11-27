package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's comment tools beyond reading and writing one (seen 2026-11-13): a
// comment's words replaced, a comment deleted, an emoji reaction set, a
// discussion resolved or reopened — each as the comment's menu does it on
// the screen (updateCommentRecordForTarget, deleteCommentRecordForTarget,
// setCommentReactionForTarget), found by its ID alone as Notion's are. As
// Notion's tools say, the person edits and deletes only their own comments.

// mcpCommentDeletionTurns bounds the turns one call gives a deletion of a
// comment and its replies before it answers.
const mcpCommentDeletionTurns = 64

type notionMCPUpdateCommentInput struct {
	CommentID string `json:"comment_id" jsonschema:"The comment's id, as notion-get-comments or notion-create-comment gives it"`
	Markdown  string `json:"markdown" jsonschema:"The comment's whole new text, inline Markdown as notion-create-comment reads it: it replaces the text, so keep what should stay"`
}

type notionMCPDeleteCommentInput struct {
	CommentID string `json:"comment_id" jsonschema:"The comment's id, as notion-get-comments or notion-create-comment gives it"`
}

type notionMCPCommentReactionInput struct {
	CommentID string `json:"comment_id" jsonschema:"The comment's id, as notion-get-comments or notion-create-comment gives it"`
	Emoji     string `json:"emoji" jsonschema:"One emoji"`
	Reacted   bool   `json:"reacted" jsonschema:"true to add your reaction, false to take it back"`
}

type notionMCPDiscussionResolvedInput struct {
	DiscussionID string `json:"discussion_id" jsonschema:"The discussion's discussion:// address or ID, as notion-get-comments or notion-create-comment gives it"`
	Resolved     bool   `json:"resolved" jsonschema:"true to resolve the discussion, false to reopen it"`
}

type notionMCPCommentChangeOutput struct {
	Result notionMCPCommentChangeResult `json:"result"`
}

// notionMCPUpdateCommentOutput is update-comment's answer: the change, and
// what reading its markdown changed or kept as text, as create-comment's.
type notionMCPUpdateCommentOutput struct {
	Result   notionMCPCommentChangeResult `json:"result"`
	Warnings []mcpMarkdownWarning         `json:"warnings,omitempty" jsonschema:"What reading the markdown changed or kept as text, as notion-update-page says it of a page's (markdown_parser_warning, its category the kind)"`
}

type notionMCPCommentChangeResult struct {
	Status       string `json:"status"`
	ID           string `json:"id,omitempty"`
	DiscussionID string `json:"discussion_id,omitempty"`
	Emoji        string `json:"emoji,omitempty"`
	Reacted      *bool  `json:"reacted,omitempty"`
	Resolved     *bool  `json:"resolved,omitempty"`
}

func registerMCPCommentEditTools(server *mcp.Server, app core.App) {
	idempotent := &mcp.ToolAnnotations{IdempotentHint: true}
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-update-comment", Title: "Edit a Leafen comment",
		Description: "Replace the text of a comment you wrote with the whole corrected text, as inline Markdown (marks, links, mentions and dates as notion-create-comment reads them; no headings or lists). To fix a comment, edit it rather than deleting it and writing a new one. Only your own comments can be edited.",
		Annotations: idempotent,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPUpdateCommentInput) (*mcp.CallToolResult, notionMCPUpdateCommentOutput, error) {
		return updateMCPNotionComment(ctx, app, input)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-delete-comment", Title: "Delete a Leafen comment",
		Description: "Delete a comment you wrote, with its replies when it starts its discussion; the last comment of a discussion takes the discussion with it. Use it only when the person asks to delete the comment; to fix one, use notion-update-comment. Only your own comments can be deleted.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(true), IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPDeleteCommentInput) (*mcp.CallToolResult, notionMCPCommentChangeOutput, error) {
		return deleteMCPNotionComment(ctx, app, input)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-set-comment-reaction", Title: "React to a Leafen comment",
		Description: "Add or remove your emoji reaction on a comment; notion-get-comments shows a comment's reactions. Setting the same state twice changes nothing.",
		Annotations: idempotent,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPCommentReactionInput) (*mcp.CallToolResult, notionMCPCommentChangeOutput, error) {
		return reactMCPNotionComment(ctx, app, input)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-set-discussion-resolved", Title: "Resolve a Leafen discussion",
		Description: "Resolve or reopen a discussion. Use it only when a person asks to resolve that discussion — not because its feedback was addressed, a reply was written or related work is done.",
		Annotations: idempotent,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPDiscussionResolvedInput) (*mcp.CallToolResult, notionMCPCommentChangeOutput, error) {
		return resolveMCPNotionDiscussion(ctx, app, input)
	})
}

// mcpCommentToChange is a comment found by its ID alone, with what it is on
// authorized for change by the connection's person.
func mcpCommentToChange(app core.App, authority mcpAuthority, raw, kind string) (storedComment, mcpCommentTargetAuthorization, error) {
	id := strings.TrimSpace(raw)
	if kind == "discussion" {
		if _, _, rootID, err := parseMCPDiscussionID(app, authority, raw); err == nil {
			id = rootID
		}
	}
	target, err := authorizeMCPReplyTarget(app, authority, id, kind)
	if err != nil {
		return storedComment{}, mcpCommentTargetAuthorization{}, err
	}
	row, err := loadCommentForTarget(app, target.Authorization.Target, target.CommentID)
	if err != nil {
		return storedComment{}, mcpCommentTargetAuthorization{}, mcpCommentError(err)
	}
	return row, target, nil
}

// mcpCommentAuthorize authorizes a comment change again inside its write.
func mcpCommentAuthorize(authority mcpAuthority, target mcpCommentTargetAuthorization) func(core.App) (commentAuthorization, error) {
	return func(txApp core.App) (commentAuthorization, error) {
		return authorizeMCPCommentTargetCurrent(txApp, authority, target.Authorization.Target, permissionWorkspaceWrite)
	}
}

// mcpCommentDiscussionOf is the address of a comment's discussion, by the ID
// each of its comments keeps — read before the comment is changed or deleted.
func mcpCommentDiscussionOf(app core.App, row storedComment, target mcpCommentTargetAuthorization) string {
	rootID := row.ID
	if row.ParentID != "" {
		rootID = row.ParentID
	}
	if row.NotionDiscussionID == "" {
		return mcpDiscussionID(app, row.WorkspaceID, target.PageID, row.BlockID, rootID)
	}
	return mcpDiscussionAddress(target.PageID, row.BlockID, row.NotionDiscussionID)
}

func updateMCPNotionComment(ctx context.Context, app core.App, input notionMCPUpdateCommentInput) (*mcp.CallToolResult, notionMCPUpdateCommentOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPUpdateCommentOutput{}, err
	}
	row, target, err := mcpCommentToChange(app, authority, input.CommentID, "comment")
	if err != nil {
		return nil, notionMCPUpdateCommentOutput{}, err
	}
	if row.AuthorID != authority.PrincipalID {
		return nil, notionMCPUpdateCommentOutput{}, mcpDenied("you can only edit your own comments: this one is someone else's")
	}
	said := &mcpMarkdownSaid{}
	written, err := mcpCommentBody(app, authority, target.Authorization.Page, &input.Markdown, nil, "", nil, said)
	if err != nil {
		return nil, notionMCPUpdateCommentOutput{}, mcpReasoned(err)
	}
	// The new words take the old ones' place; what the comment is on — the
	// words it quotes, the property it is on — stays.
	body := map[string]any{}
	replacement := map[string]any{}
	if json.Unmarshal([]byte(row.BodyJSON), &body) != nil || json.Unmarshal(written, &replacement) != nil {
		return nil, notionMCPUpdateCommentOutput{}, errMCPToolFailed
	}
	for _, field := range []string{"type", "rich", "attachmentIds"} {
		if value, ok := replacement[field]; ok {
			body[field] = value
		} else {
			delete(body, field)
		}
	}
	merged, err := json.Marshal(body)
	if err != nil {
		return nil, notionMCPUpdateCommentOutput{}, failedBecause(errMCPToolFailed, err)
	}
	_, requestID := mcpCommentMutationIDs(authority)
	response, err := updateCommentRecordForTarget(app, authority.PrincipalID, target.Authorization.Target, row.ID,
		commentUpdateInput{RequestID: requestID, ExpectedRevision: row.Revision, Body: merged}, mcpCommentAuthorize(authority, target))
	if err != nil {
		return nil, notionMCPUpdateCommentOutput{}, mcpCommentError(err)
	}
	return nil, notionMCPUpdateCommentOutput{Result: notionMCPCommentChangeResult{
		Status: "success", ID: mcpCommentID(app, row.WorkspaceID, response.Comment.ID), DiscussionID: mcpCommentDiscussionOf(app, row, target),
	}, Warnings: said.answer()}, nil
}

func deleteMCPNotionComment(ctx context.Context, app core.App, input notionMCPDeleteCommentInput) (*mcp.CallToolResult, notionMCPCommentChangeOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPCommentChangeOutput{}, err
	}
	row, target, err := mcpCommentToChange(app, authority, input.CommentID, "comment")
	if err != nil {
		return nil, notionMCPCommentChangeOutput{}, err
	}
	if row.AuthorID != authority.PrincipalID {
		return nil, notionMCPCommentChangeOutput{}, mcpDenied("you can only delete your own comments: this one is someone else's")
	}
	// Named before it goes, as the API names a deleted one.
	publicID := mcpCommentID(app, row.WorkspaceID, row.ID)
	_, requestID := mcpCommentMutationIDs(authority)
	commentTarget := target.Authorization.Target
	response, err := deleteCommentRecordForTarget(app, authority.PrincipalID, commentTarget, commentTarget.Kind+":"+commentTarget.ID, row.ID,
		commentDeleteInput{RequestID: requestID, ExpectedRevision: row.Revision}, mcpCommentAuthorize(authority, target))
	for turn := 0; err == nil && response.Deletion.Status == commentDeletionStatusPending && turn < mcpCommentDeletionTurns; turn++ {
		response, err = continueCommentDeletion(app, authority.PrincipalID, commentTarget.StoragePageID, response.Deletion.ID)
	}
	if err != nil {
		return nil, notionMCPCommentChangeOutput{}, mcpCommentError(err)
	}
	status := "success"
	if response.Deletion.Status == commentDeletionStatusPending {
		status = "in_progress"
	}
	return nil, notionMCPCommentChangeOutput{Result: notionMCPCommentChangeResult{
		Status: status, ID: publicID, DiscussionID: mcpCommentDiscussionOf(app, row, target),
	}}, nil
}

func reactMCPNotionComment(ctx context.Context, app core.App, input notionMCPCommentReactionInput) (*mcp.CallToolResult, notionMCPCommentChangeOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPCommentChangeOutput{}, err
	}
	row, target, err := mcpCommentToChange(app, authority, input.CommentID, "comment")
	if err != nil {
		return nil, notionMCPCommentChangeOutput{}, err
	}
	emoji := strings.TrimSpace(input.Emoji)
	if _, err := setCommentReactionForTarget(app, authority.PrincipalID, target.Authorization.Target, row.ID,
		commentReactionInput{Emoji: emoji, Reacted: input.Reacted}, mcpCommentAuthorize(authority, target)); err != nil {
		return nil, notionMCPCommentChangeOutput{}, mcpCommentError(err)
	}
	return nil, notionMCPCommentChangeOutput{Result: notionMCPCommentChangeResult{
		Status: "success", ID: mcpCommentID(app, row.WorkspaceID, row.ID), Emoji: emoji, Reacted: &input.Reacted,
	}}, nil
}

func resolveMCPNotionDiscussion(ctx context.Context, app core.App, input notionMCPDiscussionResolvedInput) (*mcp.CallToolResult, notionMCPCommentChangeOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPCommentChangeOutput{}, err
	}
	row, target, err := mcpCommentToChange(app, authority, input.DiscussionID, "discussion")
	if err != nil {
		return nil, notionMCPCommentChangeOutput{}, err
	}
	if row.ParentID != "" {
		return nil, notionMCPCommentChangeOutput{}, mcpInvalid("that is a reply, not a discussion: give the discussion's discussion:// address from notion-get-comments")
	}
	discussion := mcpCommentDiscussionOf(app, row, target)
	if row.Resolved != input.Resolved {
		_, requestID := mcpCommentMutationIDs(authority)
		resolved := input.Resolved
		if _, err := updateCommentRecordForTarget(app, authority.PrincipalID, target.Authorization.Target, row.ID,
			commentUpdateInput{RequestID: requestID, ExpectedRevision: row.Revision, Resolved: &resolved},
			mcpCommentAuthorize(authority, target)); err != nil {
			return nil, notionMCPCommentChangeOutput{}, mcpCommentError(err)
		}
	}
	return nil, notionMCPCommentChangeOutput{Result: notionMCPCommentChangeResult{
		Status: "success", DiscussionID: discussion, Resolved: &input.Resolved,
	}}, nil
}
