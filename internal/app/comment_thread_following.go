package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Whom a discussion is for: the people who follow it. As Notion's (seen
// 2026-11-25 in Chrome at 1440 and on a Galaxy A90 in its app, a member and a
// guest alike), a discussion someone follows is one they wrote in or are
// mentioned in, or one on a page whose "알림 받기" they set to every comment —
// the page they made is so from the start (followCreatedPageComments). Its
// unread mark goes by that as it is now: a member who never wrote in a
// guest's new discussion under the title saw it read, setting the page's bell
// to "모든 댓글" turned it unread at once, and setting it back to "답글과
// @멘션" read it again; a reply in a discussion the reader had written in
// was unread to them under either setting. A block's discussion followed
// only through a bell holds as new only what came after that bell last turned
// to follow (commentThreadFollow). A comment's notice goes by the same rule
// as it is written (commentNotificationPersonal).

// commentThreadFollow is how the person follows a thread now. A block's
// discussion followed only through a bell — the page's, its row page's or a
// database's "알림 받기" — holds as new to them only the comments made after
// that bell last turned to follow (newAfter, Unix milliseconds), as Notion's
// (seen 2026-11-25 in Chrome at 1440, two tabs at different times, a guest
// writing on a Galaxy A90): a guest's new block discussions written under
// "답글과 @멘션" stayed read once the bell turned to "모든 댓글" (four, on three
// pages), and an unread one stayed read after the bell went to "답글과 @멘션"
// and back (twice), while a discussion under the title turned unread and read
// again with the bell. Zero bounds nothing: one written in or mentioned in,
// or one under the title.
type commentThreadFollow struct{ newAfter int64 }

// commentThreadsFollowed says which of the threads, by their first comments
// among the records, the person follows now, and how.
func commentThreadsFollowed(ctx context.Context, app core.App, actorID string, records []commentRecord) (map[string]commentThreadFollow, error) {
	followed := map[string]commentThreadFollow{}
	// The threads by their page, which each comment of a thread shares.
	type pageKey struct{ workspaceID, pageID string }
	threads := map[pageKey][]string{}
	seen := map[string]bool{}
	for _, record := range records {
		thread := record.ID
		if record.ParentID != "" {
			thread = record.ParentID
		}
		if seen[thread] {
			continue
		}
		seen[thread] = true
		key := pageKey{record.WorkspaceID, record.PageID}
		threads[key] = append(threads[key], thread)
	}
	for key, ids := range threads {
		if err := followedThreadsOnPage(ctx, app, actorID, key.workspaceID, key.pageID, ids, followed); err != nil {
			return nil, err
		}
	}
	return followed, nil
}

// followedThreadsOnPage marks, of one page's threads, those the person
// follows: by the page they are on, by having written in them, or by being
// mentioned in them. The page is the one the threads' records name — a
// database row's own for its discussion, which is stored with the page
// holding its database (renderComment).
func followedThreadsOnPage(
	ctx context.Context, app core.App, actorID, workspaceID, pageID string, threadIDs []string,
	followed map[string]commentThreadFollow,
) error {
	params := dbx.Params{"workspaceId": workspaceID, "pageId": pageID, "actorId": actorID}
	names := make([]string, 0, len(threadIDs))
	for index, id := range threadIDs {
		name := fmt.Sprintf("thread%d", index)
		params[name] = id
		names = append(names, "{:"+name+"}")
	}
	inThreads := strings.Join(names, ",")
	// What each thread is on, from its first comment, and the page its
	// comments are stored with.
	roots := []struct {
		ID         string `db:"id"`
		PageID     string `db:"page_id"`
		TargetKind string `db:"target_kind"`
		TargetID   string `db:"target_id"`
		BlockID    string `db:"block_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[page_id]],[[target_kind]],[[target_id]],[[block_id]] FROM {{leafen_comments}}
		WHERE [[workspace_id]]={:workspaceId} AND [[id]] IN (` + inThreads + `)
			AND (CASE WHEN [[target_id]]='' THEN [[page_id]] ELSE [[target_id]] END)={:pageId}
	`).WithContext(ctx).Bind(params).All(&roots); err != nil {
		return err
	}
	if len(roots) == 0 {
		return nil
	}
	params["storagePageId"] = roots[0].PageID
	// A page's or a row page's every comment, followed or not and since when, asked once.
	type bell struct {
		all   bool
		since int64
	}
	byTarget := map[string]bell{}
	for _, root := range roots {
		target := root.TargetKind + "\x00" + root.TargetID
		on, known := byTarget[target]
		if !known {
			var err error
			if on.all, on.since, err = everyCommentFollowedSince(ctx, app, root.TargetKind, root.TargetID, root.PageID, actorID); err != nil {
				return err
			}
			byTarget[target] = on
		}
		if on.all && root.BlockID != "" {
			followed[root.ID] = commentThreadFollow{newAfter: on.since}
		} else if on.all {
			followed[root.ID] = commentThreadFollow{}
		}
	}
	// The threads they wrote in, and those whose comments mention them.
	written := []struct {
		ID       string `db:"id"`
		ParentID string `db:"parent_id"`
		AuthorID string `db:"author_id"`
		BodyJSON string `db:"body_json"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[parent_id]],[[author_id]],
			CASE WHEN [[author_id]]={:actorId} THEN '' ELSE [[body_json]] END AS [[body_json]]
		FROM {{leafen_comments}}
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:storagePageId}
			AND ([[id]] IN (` + inThreads + `) OR [[parent_id]] IN (` + inThreads + `))
			AND ([[author_id]]={:actorId} OR instr([[body_json]], {:actorId}) > 0)
	`).WithContext(ctx).Bind(params).All(&written); err != nil {
		return err
	}
	for _, comment := range written {
		thread := comment.ID
		if comment.ParentID != "" {
			thread = comment.ParentID
		}
		if follow, ok := followed[thread]; ok && follow.newAfter == 0 {
			continue
		}
		if comment.AuthorID == actorID {
			followed[thread] = commentThreadFollow{}
			continue
		}
		projection, err := commentNotificationBodyProjection(json.RawMessage(comment.BodyJSON))
		if err != nil {
			continue
		}
		for _, userID := range projection.MentionRecipientIDs {
			if userID == actorID {
				followed[thread] = commentThreadFollow{}
				break
			}
		}
	}
	return nil
}

// followsEveryCommentOn reports whether the person follows every comment on
// what a comment is on: a page whose "알림 받기" they set to every comment;
// a database row page's every update or comment, or every comment of a
// database showing that row.
func followsEveryCommentOn(ctx context.Context, app core.App, targetKind, targetID, pageID, userID string) (bool, error) {
	follows, _, err := everyCommentFollowedSince(ctx, app, targetKind, targetID, pageID, userID)
	return follows, err
}

// everyCommentFollowedSince is followsEveryCommentOn with when that bell last
// turned to follow (Unix milliseconds): its row keeps the time it was made,
// and choosing again a level that follows every comment leaves it.
func everyCommentFollowedSince(ctx context.Context, app core.App, targetKind, targetID, pageID, userID string) (bool, int64, error) {
	switch targetKind {
	case commentTargetDatabaseRow:
		level, since, err := pageBellSince(ctx, app, targetID, userID)
		if err != nil || level != databaseRowNotificationRepliesAndMentions {
			return err == nil, since, err
		}
		return databaseRowCommentsFollowedSince(ctx, app, targetID, userID)
	case commentTargetPage, "":
		level, since, err := pageBellSince(ctx, app, pageID, userID)
		return level == pageNotificationAllComments, since, err
	}
	return false, 0, nil
}

// pageBellSince reads a page's or a row page's "알림 받기" for a person and
// when it was set; none is "답글과 @멘션", everyone's default.
func pageBellSince(ctx context.Context, app core.App, pageID, userID string) (string, int64, error) {
	row := struct {
		Level     string `db:"level"`
		UpdatedAt int64  `db:"updated_at"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[level]],[[updated_at]] FROM {{leafen_page_notification_preferences}}
		WHERE [[page_id]] = {:pageId} AND [[user_id]] = {:userId}
	`).WithContext(ctx).Bind(dbx.Params{"pageId": pageID, "userId": userID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return pageNotificationRepliesAndMentions, 0, nil
	}
	return row.Level, row.UpdatedAt, err
}
