package app

import (
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A new comment, made in one transaction with its receipt, as a request
// names it (insertCommentRow writes the row itself).

func createCommentRecord(
	app core.App, actorID, pageID string, input commentCreateInput,
) (commentMutationResponse, error) {
	target := nativePageCommentTarget(pageID)
	return createCommentRecordForTarget(
		app, actorID, target, input,
		func(txApp core.App) (commentAuthorization, error) {
			return authorizeCommentPage(
				txApp, actorID, pageID, permissionWorkspaceWrite, "",
			)
		},
	)
}

func createCommentRecordForTarget(
	app core.App,
	actorID string,
	target nativeCommentTarget,
	input commentCreateInput,
	authorize func(core.App) (commentAuthorization, error),
) (commentMutationResponse, error) {
	return createCommentRecordAs(app, actorID, target, input, authorize, false)
}

// createCommentRecordAs is createCommentRecordForTarget; byPerson says the
// person wrote it themselves, here or through a connection, rather than a
// move from Notion or a send writing it for them — only then does it change
// what they follow (followFirstTitleDiscussion).
func createCommentRecordAs(
	app core.App,
	actorID string,
	target nativeCommentTarget,
	input commentCreateInput,
	authorize func(core.App) (commentAuthorization, error),
	byPerson bool,
) (commentMutationResponse, error) {
	input.ID, input.RequestID = strings.TrimSpace(input.ID), strings.TrimSpace(input.RequestID)
	input.BlockID, input.ParentID = strings.TrimSpace(input.BlockID), strings.TrimSpace(input.ParentID)
	if !validCommentIdentifier(target.ID) || !validCommentIdentifier(target.StoragePageID) ||
		(target.Kind != commentTargetPage && target.Kind != commentTargetDatabaseRow) {
		return commentMutationResponse{}, errCommentInvalid
	}
	if problem := firstNonempty(commentNewIDsProblem(input.ID, input.RequestID), commentBodyRefusalProblem(input.Body)); problem != "" {
		return commentMutationResponse{}, commentInvalid("%s", problem)
	}
	if err := commentBodyProblem(input.Body); err != nil {
		return commentMutationResponse{}, err
	}
	// Suggestion threads start only with their suggestion.
	if _, suggestion := pageSuggestionThread(storedComment{
		ParentID: input.ParentID, BodyJSON: string(input.Body),
	}); suggestion {
		return commentMutationResponse{}, commentInvalid("an edit suggestion starts with POST /pages/{pageId}/suggestions, not as a comment")
	}
	hash, err := commentRequestHash("create", target.Kind+":"+target.ID, input.ID, input)
	if err != nil {
		return commentMutationResponse{}, err
	}
	response := commentMutationResponse{}
	err = app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorize(txApp)
		if err != nil {
			return err
		}
		if authorization.Target != target {
			return permissionDenied()
		}
		if replay, err := readCommentReceipt(txApp, actorID, input.RequestID, hash, &response); err != nil || replay {
			response.Duplicate = replay
			return err
		}
		// A comment goes through the organization's DLP policy as page
		// content does (2026-10-16: comments weren't checked).
		if err := enforceOrganizationDLPContent(txApp, authorization.Page.Workspace.Organization.OrganizationID, input.Body); err != nil {
			return err
		}
		now := time.Now().UTC()
		row, err := insertCommentRow(txApp, authorization, target, actorID, input, now)
		if err != nil {
			return err
		}
		// Who has the page open as a reply comes (comment_readers_present.go).
		if err := noteReadersPresent(txApp, row); err != nil {
			return err
		}
		if byPerson {
			if err := followFirstTitleDiscussion(txApp, target, row, now); err != nil {
				return err
			}
		}
		// Replying reads the thread to now, as Notion's (comment_thread_states.go).
		if row.ParentID != "" {
			read := false
			if _, err := storeCommentThreadState(txApp, row.ParentID, actorID, commentThreadStateInput{Unread: &read}); err != nil {
				return err
			}
		}
		record, err := renderComment(row)
		if err != nil {
			return err
		}
		// A new comment shows its attachments' files at once.
		records := []commentRecord{record}
		if err := attachCommentAttachments(txApp, records); err != nil {
			return err
		}
		response.Comment = records[0]
		return insertCommentReceipt(
			txApp, actorID, row.WorkspaceID, target.StoragePageID,
			input.RequestID, hash, "create", response,
		)
	})
	return response, err
}
