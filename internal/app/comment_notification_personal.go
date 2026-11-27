package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedCommentNotificationSource struct {
	WorkspaceID string `db:"workspace_id"`
	ParentID    string `db:"parent_id"`
	TargetKind  string `db:"target_kind"`
	TargetID    string `db:"target_id"`
}

// commentNotificationFollowersAfter lists, after `cursor` in id order and
// at most `limit` of them, who follows a comment: the others in its thread —
// those who wrote in it and those it mentions — and those following every
// comment on its page or every update of its database row page. Each source is read from where the last list ended, so
// every follower is reached however many there are; more says some remain.
func commentNotificationFollowersAfter(
	ctx context.Context, app core.App, event storedCommentNotificationEvent, cursor string, limit int,
) (followers []string, more bool, err error) {
	if event.SourceKind != "comment" {
		return nil, false, nil
	}
	comment := struct {
		PageID     string `db:"page_id"`
		ParentID   string `db:"parent_id"`
		TargetKind string `db:"target_kind"`
		TargetID   string `db:"target_id"`
	}{}
	err = app.DB().NewQuery(`
		SELECT [[page_id]], [[parent_id]], [[target_kind]], [[target_id]] FROM {{leafen_comments}}
		WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId}
	`).WithContext(ctx).Bind(dbx.Params{"id": event.SourceID, "workspaceId": event.WorkspaceID}).One(&comment)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	params := dbx.Params{
		"workspaceId": event.WorkspaceID, "pageId": comment.PageID, "parentId": comment.ParentID,
		"commentId": event.SourceID, "cursor": cursor, "limit": limit + 1,
	}
	found := map[string]struct{}{}
	read := func(query string, extra dbx.Params) error {
		ids := []string{}
		bound := dbx.Params{}
		for key, value := range params {
			bound[key] = value
		}
		for key, value := range extra {
			bound[key] = value
		}
		if err := app.DB().NewQuery(query).WithContext(ctx).Bind(bound).Column(&ids); err != nil {
			return err
		}
		for _, id := range ids {
			found[id] = struct{}{}
		}
		return nil
	}
	if comment.ParentID != "" {
		if err := read(`
			SELECT DISTINCT [[author_id]] FROM {{leafen_comments}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:pageId}
				AND ([[id]] = {:parentId} OR [[parent_id]] = {:parentId}) AND [[id]] != {:commentId}
				AND [[author_id]] > {:cursor}
			ORDER BY [[author_id]] LIMIT {:limit}
		`, nil); err != nil {
			return nil, false, err
		}
		mentioned, err := commentThreadMentionedPeople(ctx, app, event.WorkspaceID, comment.PageID, comment.ParentID, event.SourceID)
		if err != nil {
			return nil, false, err
		}
		for userID := range mentioned {
			if userID > cursor {
				found[userID] = struct{}{}
			}
		}
	}
	levels, followedPageID := []string{}, ""
	switch comment.TargetKind {
	case commentTargetPage, "":
		levels, followedPageID = []string{pageNotificationAllComments}, comment.PageID
	case commentTargetDatabaseRow:
		levels, followedPageID = []string{databaseRowNotificationAllUpdates, pageNotificationAllComments}, comment.TargetID
	}
	for _, level := range levels {
		if err := read(`
			SELECT [[user_id]] FROM {{leafen_page_notification_preferences}}
			INDEXED BY [[idx_leafen_page_notification_preferences_level]]
			WHERE [[page_id]] = {:followedPageId} AND [[level]] = {:level} AND [[user_id]] > {:cursor}
			ORDER BY [[user_id]] LIMIT {:limit}
		`, dbx.Params{"followedPageId": followedPageID, "level": level}); err != nil {
			return nil, false, err
		}
	}
	// A row's comments also reach those following every comment on a database showing it.
	if comment.TargetKind == commentTargetDatabaseRow {
		if err := read(databaseRowCommentFollowersSQL, dbx.Params{"rowId": comment.TargetID}); err != nil {
			return nil, false, err
		}
	}
	for id := range found {
		followers = append(followers, id)
	}
	sort.Strings(followers)
	if len(followers) > limit {
		return followers[:limit], true, nil
	}
	return followers, false, nil
}

// commentNotificationPersonal reports whether a comment notification is for
// this person rather than workspace activity: they were mentioned, the
// comment replies in a thread they wrote in or were mentioned in, or they follow every comment on
// the page or the updates of the database row page, which include comments.
// A reply in a thread whose replies they turned off is for them only when it
// mentions them.
func commentNotificationPersonal(
	ctx context.Context,
	app core.App,
	delivery storedCommentNotificationDelivery,
) (bool, error) {
	if delivery.DeliveryKind == "mention" {
		return true, nil
	}
	if delivery.SourceKind != "comment" {
		return false, nil
	}
	source := storedCommentNotificationSource{}
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]], [[parent_id]], [[target_kind]], [[target_id]] FROM {{leafen_comments}} WHERE [[id]] = {:id}
	`).WithContext(ctx).Bind(dbx.Params{"id": delivery.SourceID}).One(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if source.ParentID != "" {
		// The person turned off this thread's replies.
		muted, err := commentThreadRepliesMuted(ctx, app, source.ParentID, delivery.UserID)
		if err != nil || muted {
			return false, err
		}
		participated := 0
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_comments}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:pageId} AND [[author_id]] = {:userId}
				AND ([[id]] = {:parentId} OR [[parent_id]] = {:parentId}) AND [[id]] != {:commentId}
		`).WithContext(ctx).Bind(dbx.Params{
			"workspaceId": source.WorkspaceID, "pageId": delivery.PageID, "userId": delivery.UserID,
			"parentId": source.ParentID, "commentId": delivery.SourceID,
		}).Row(&participated); err != nil {
			return false, err
		}
		if participated > 0 {
			return true, nil
		}
		mentioned, err := commentThreadMentionedPeople(ctx, app, source.WorkspaceID, delivery.PageID, source.ParentID, delivery.SourceID)
		if err != nil {
			return false, err
		}
		if _, ok := mentioned[delivery.UserID]; ok {
			return true, nil
		}
	}
	return followsEveryCommentOn(ctx, app, source.TargetKind, source.TargetID, delivery.PageID, delivery.UserID)
}

// commentThreadMentionedPeople lists whom a thread's comments mention, but for
// one of them: as Notion's, a person mentioned in a discussion hears of the
// replies after it as its writers do (seen 2026-11-07, 2026-11-11).
func commentThreadMentionedPeople(
	ctx context.Context, app core.App, workspaceID, pageID, rootID, exceptID string,
) (map[string]struct{}, error) {
	bodies := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[body_json]] FROM {{leafen_comments}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:pageId}
			AND ([[id]] = {:rootId} OR [[parent_id]] = {:rootId}) AND [[id]] != {:exceptId}
			AND instr([[body_json]], '"person"') > 0
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "pageId": pageID, "rootId": rootID, "exceptId": exceptID,
	}).Column(&bodies); err != nil {
		return nil, err
	}
	mentioned := map[string]struct{}{}
	for _, body := range bodies {
		projection, err := commentNotificationBodyProjection(json.RawMessage(body))
		if err != nil {
			continue
		}
		for _, userID := range projection.MentionRecipientIDs {
			mentioned[userID] = struct{}{}
		}
	}
	return mentioned, nil
}
