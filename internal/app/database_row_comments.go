package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Database rows keep their own discussions (a feed post's comments). They
// share the page comment authority: the row's container page authorizes
// them and its comment revision orders their continuation. A page's comment
// paths given a row's page id answer as these (commentsOfRowPage).
func registerDatabaseRowCommentRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.Route(databaseRowCommentListMethod, apiRoot+databaseRowCommentListPath, listDatabaseRowComments)
	event.Router.Route(databaseRowCommentCreateMethod, apiRoot+databaseRowCommentCreatePath, refuseCommentWritesInTrash(func(event *core.RequestEvent) error {
		return createDatabaseRowComment(event, ygo)
	})).Bind(apis.BodyLimit(commentBodyBytes + 8192))
	event.Router.Route(databaseRowCommentUpdateMethod, apiRoot+databaseRowCommentUpdatePath, refuseCommentWritesInTrash(updateDatabaseRowComment)).
		Bind(apis.BodyLimit(commentBodyBytes + 8192))
	event.Router.Route(databaseRowCommentDeleteMethod, apiRoot+databaseRowCommentDeletePath, refuseCommentWritesInTrash(deleteDatabaseRowComment)).
		Bind(apis.BodyLimit(8192))
}

// databaseRowCommentMutationTarget authorizes a change to one of a row's
// comments: the row must be readable, and its page must take comments. When
// it answers the request itself, ok is false and the handler returns its
// result.
func databaseRowCommentMutationTarget(
	event *core.RequestEvent, actorID, rowID string,
) (target nativeCommentTarget, authorize func(core.App) (commentAuthorization, error), ok bool, err error) {
	if _, _, err := authorizeDatabaseRowCommentTarget(event.App, actorID, rowID, permissionWorkspaceRead); err != nil {
		return nativeCommentTarget{}, nil, false, databaseRowCommentAPIError(event, err, persistentReadComment)
	}
	authorize = func(app core.App) (commentAuthorization, error) {
		authorization, _, err := authorizeDatabaseRowCommentTarget(app, actorID, rowID, permissionWorkspaceWrite)
		return authorization, err
	}
	current, err := authorize(event.App)
	if err != nil {
		return nativeCommentTarget{}, nil, false, databaseRowCommentAPIError(event, err, persistentWriteComment)
	}
	return current.Target, authorize, true, nil
}

// updateDatabaseRowComment edits or resolves a row comment, as a page
// comment: its author or an editor of the row's page may.
func updateDatabaseRowComment(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	input := commentUpdateInput{}
	if event.BindBody(&input) != nil {
		return databaseRowCommentAPIError(event, errCommentInvalid, persistentWriteComment)
	}
	target, authorize, ok, err := databaseRowCommentMutationTarget(event, user.Id, strings.TrimSpace(event.Request.PathValue("rowId")))
	if !ok {
		return err
	}
	response, err := updateCommentRecordForTarget(
		event.App, user.Id, target, strings.TrimSpace(event.Request.PathValue("commentId")), input, authorize,
	)
	if err != nil {
		return databaseRowCommentAPIError(event, err, persistentWriteComment)
	}
	return event.JSON(http.StatusOK, response)
}

// deleteDatabaseRowComment starts deleting a row comment and its replies;
// the page holding the row reads and continues the deletion.
func deleteDatabaseRowComment(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	input := commentDeleteInput{}
	if event.BindBody(&input) != nil {
		return databaseRowCommentAPIError(event, errCommentInvalid, persistentWriteComment)
	}
	target, authorize, ok, err := databaseRowCommentMutationTarget(event, user.Id, strings.TrimSpace(event.Request.PathValue("rowId")))
	if !ok {
		return err
	}
	response, err := deleteCommentRecordForTarget(
		event.App, user.Id, target, target.Kind+":"+target.ID,
		strings.TrimSpace(event.Request.PathValue("commentId")), input, authorize,
	)
	if err != nil {
		return databaseRowCommentAPIError(event, err, persistentWriteComment)
	}
	status := http.StatusOK
	if !response.Duplicate && response.Deletion.Status == commentDeletionStatusPending {
		status = http.StatusAccepted
	}
	return event.JSON(status, response)
}

// databaseRowCommentAPIError answers a row the actor cannot read as missing,
// so a row's existence does not leak.
func databaseRowCommentAPIError(event *core.RequestEvent, err error, operation persistentWriteStorageOperation) error {
	if errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err) && operation == persistentReadComment {
		return apiError(event, http.StatusNotFound, errorCodeDatabaseRowPageNotFound, "Row not found.")
	}
	return commentAPIError(event, err, operation)
}

func listDatabaseRowComments(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	response, err := listDatabaseRowCommentRecords(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("rowId")),
		event.Request.URL.Query().Get("limit"), event.Request.URL.Query().Get("cursor"),
	)
	if err != nil {
		return databaseRowCommentAPIError(event, err, persistentReadComment)
	}
	// What names a page closed to this reader (content_graph_reader_mentions.go).
	commentsAsRead(event.App, user.Id, response.Items)
	return event.JSON(http.StatusOK, response)
}

func createDatabaseRowComment(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	input := commentCreateInput{}
	if event.BindBody(&input) != nil {
		return databaseRowCommentAPIError(event, errCommentInvalid, persistentWriteComment)
	}
	if err := refuseConnectionCommentPart(input.Body); err != nil {
		return databaseRowCommentAPIError(event, err, persistentWriteComment)
	}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	authorize := func(app core.App) (commentAuthorization, error) {
		authorization, _, err := authorizeDatabaseRowCommentTarget(app, user.Id, rowID, permissionWorkspaceWrite)
		return authorization, err
	}
	if _, _, err := authorizeDatabaseRowCommentTarget(event.App, user.Id, rowID, permissionWorkspaceRead); err != nil {
		return databaseRowCommentAPIError(event, err, persistentReadComment)
	}
	current, err := authorize(event.App)
	if err != nil {
		return databaseRowCommentAPIError(event, err, persistentWriteComment)
	}
	response, err := createConnectedComment(event.Request.Context(), event.App, user.Id, current.Target, input, authorize)
	if err != nil {
		return databaseRowCommentAPIError(event, err, persistentWriteComment)
	}
	anchorNewCommentText(event, ygo, user.Id, response, authorize)
	anchorNewCommentCell(event, ygo, response, authorize)
	status := http.StatusCreated
	if response.Duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

// listDatabaseRowCommentRecords pages one row's comments oldest first with
// their authors' profiles, like a page's comment list.
func listDatabaseRowCommentRecords(
	app core.App, actorID, rowID, rawLimit, rawCursor string,
) (commentListResponse, error) {
	limit := commentListPageSize
	if rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > commentListPageSize {
			return commentListResponse{}, errCommentInvalid
		}
		limit = parsed
	}
	cursor, err := decodeCommentCursor(rawCursor)
	if err != nil {
		return commentListResponse{}, err
	}
	response := commentListResponse{Items: []commentRecord{}, ActorProfiles: []commentActorProfile{}}
	err = runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		authorization, _, err := authorizeDatabaseRowCommentTarget(txApp, actorID, rowID, permissionWorkspaceRead)
		if err != nil {
			return err
		}
		target := authorization.Target
		revision, err := readCommentPageRevision(txApp, target.StoragePageID)
		if err != nil {
			return err
		}
		if rawCursor != "" && cursor.SourceRevision != revision {
			return errCommentSnapshotChanged
		}
		rows := []storedComment{}
		err = txApp.DB().NewQuery(`
			SELECT [[id]],[[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],
				[[block_id]],[[parent_id]],[[author_id]],
				[[body_json]],[[notion_discussion_id]],[[resolved]],[[revision]],
				[[created_at]],[[updated_at]],[[edited_at]]
			FROM {{leafen_comments}}
			WHERE [[workspace_id]]={:workspaceId} AND [[target_kind]]={:targetKind} AND [[target_id]]={:targetId}
				AND ({:afterCreated}='' OR [[created_at]]>{:afterCreated}
					OR ([[created_at]]={:afterCreated} AND [[id]]>{:afterId}))
			ORDER BY [[created_at]],[[id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"workspaceId": authorization.Page.Workspace.WorkspaceID, "targetKind": target.Kind, "targetId": target.ID,
			"afterCreated": cursor.AfterCreated, "afterId": cursor.AfterID, "limit": limit + 1,
		}).All(&rows)
		if err != nil {
			return err
		}
		response.SourceRevision = revision
		response.HasMore = len(rows) > limit
		if rawCursor == "" {
			if response.PageReactions, err = loadPageReactions(txApp, target); err != nil {
				return err
			}
		}
		if response.HasMore {
			rows = rows[:limit]
		}
		actorIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			record, err := renderComment(row)
			if err != nil {
				return err
			}
			response.Items = append(response.Items, record)
			actorIDs = append(actorIDs, record.AuthorID)
		}
		if err := attachCommentListDetails(txApp, actorID, rows, response.Items); err != nil {
			return err
		}
		if response.ActorProfiles, err = readCanonicalActorProfiles(txApp, actorIDs); err != nil {
			return err
		}
		if response.HasMore {
			last := rows[len(rows)-1]
			next, err := encodeCommentCursor(commentListCursor{
				SourceRevision: revision, AfterCreated: last.CreatedAt, AfterID: last.ID,
			})
			if err != nil {
				return err
			}
			response.NextCursor = &next
		}
		return nil
	})
	return response, err
}
