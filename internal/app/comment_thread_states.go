package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A person's own state of a comment thread, as the "…" items of Notion's
// comments (seen 2026-09-30): "Turn off replies" keeps the thread's replies
// from notifying them personally — being mentioned still does — and "Mark
// as unread" marks the thread until they mark it read. Anyone who can read
// the comments may set theirs; it changes no comment and nobody else sees it.
//
// A thread is also unread to someone who follows it (comment_thread_following.go)
// while it holds a comment of someone else's they have not read, as Notion's
// (seen 2026-11-07, 2026-11-25): "Mark as read", replying, opening that
// comment's notice, closing its card beside a block once opened, or opening
// its folded replies under a page's title reads it (the web asks for "Mark as
// read" then); opening the page, its comment field or its discussion list,
// resting the pointer on its row there, or seeing it on the page does not, and
// reading the notice alone reads the thread only while they never read it
// otherwise. A reply under a page's title written while they did not have the
// page open is new only once they have been in its discussion past someone
// else's words, as opening the page while it shows read keeps
// (comment_readers_present.go). Each comment says whether it is new to the
// reader, for the thread's dot and the "New" line before the first such one.

type storedCommentThreadState struct {
	CommentID    string `db:"comment_id"`
	RepliesMuted bool   `db:"replies_muted"`
	MarkedUnread bool   `db:"marked_unread"`
	// When the person last read the thread or set its state: comments others
	// add after it are new to them (attachCommentThreadStates).
	UpdatedAt string `db:"updated_at"`
}

func registerCommentThreadStateRoutes(event *core.ServeEvent) {
	event.Router.Route(commentThreadStateSetMethod, apiRoot+commentThreadStateSetPath, commentsOfRowPage(
		refuseCommentWritesInTrash(setCommentThreadStateRoute), refuseCommentWritesInTrash(setDatabaseRowCommentThreadStateRoute),
	)).Bind(apis.BodyLimit(1024))
	event.Router.Route(databaseRowCommentThreadStateSetMethod, apiRoot+databaseRowCommentThreadStateSetPath, refuseCommentWritesInTrash(setDatabaseRowCommentThreadStateRoute)).
		Bind(apis.BodyLimit(1024))
}

func setCommentThreadStateRoute(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	input := commentThreadStateInput{}
	if event.BindBody(&input) != nil {
		return commentAPIError(event, errCommentInvalid, persistentWriteComment)
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	response, err := setCommentThreadStateForTarget(
		event.App, user.Id, nativePageCommentTarget(pageID), event.Request.PathValue("commentId"), input,
		func(txApp core.App) (commentAuthorization, error) {
			return authorizeCommentPage(txApp, user.Id, pageID, permissionWorkspaceRead, "")
		},
	)
	if err != nil {
		return commentAPIError(event, err, persistentWriteComment)
	}
	return event.JSON(http.StatusOK, response)
}

func setDatabaseRowCommentThreadStateRoute(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	input := commentThreadStateInput{}
	if event.BindBody(&input) != nil {
		return databaseRowCommentAPIError(event, errCommentInvalid, persistentWriteComment)
	}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	authorize := func(app core.App) (commentAuthorization, error) {
		authorization, _, err := authorizeDatabaseRowCommentTarget(app, user.Id, rowID, permissionWorkspaceRead)
		return authorization, err
	}
	current, err := authorize(event.App)
	if err != nil {
		return databaseRowCommentAPIError(event, err, persistentReadComment)
	}
	response, err := setCommentThreadStateForTarget(
		event.App, user.Id, current.Target, event.Request.PathValue("commentId"), input, authorize,
	)
	if err != nil {
		return databaseRowCommentAPIError(event, err, persistentWriteComment)
	}
	return event.JSON(http.StatusOK, response)
}

// setCommentThreadStateForTarget changes what the input names of the
// actor's state of a thread, given by its first comment, and answers with
// that comment as they see it. Asking for what is already so changes
// nothing and answers as a duplicate.
func setCommentThreadStateForTarget(
	app core.App,
	actorID string,
	target nativeCommentTarget,
	commentID string,
	input commentThreadStateInput,
	authorize func(core.App) (commentAuthorization, error),
) (commentMutationResponse, error) {
	commentID = strings.TrimSpace(commentID)
	if !validCommentIdentifier(commentID) || !validCommentIdentifier(target.ID) || !validCommentIdentifier(target.StoragePageID) {
		return commentMutationResponse{}, errCommentInvalid
	}
	// Each is a change of its own; one at least is sent.
	if input.RepliesMuted == nil && input.Unread == nil {
		return commentMutationResponse{}, commentInvalid("send at least one of repliesMuted and unread to change")
	}
	response := commentMutationResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorize(txApp)
		if err != nil {
			return err
		}
		if authorization.Target != target {
			return permissionDenied()
		}
		row, err := loadCommentForTarget(txApp, target, commentID)
		if err != nil {
			return err
		}
		// The state is the thread's, kept on its first comment; a reply names
		// its thread, as a notice of it opens it.
		if row.ParentID != "" {
			if row, err = loadCommentForTarget(txApp, target, row.ParentID); err != nil {
				return err
			}
		}
		changed, err := storeCommentThreadState(txApp, row.ID, actorID, input)
		if err != nil {
			return err
		}
		record, err := renderComment(row)
		if err != nil {
			return err
		}
		records := []commentRecord{record}
		if err := attachCommentDetails(txApp, actorID, records); err != nil {
			return err
		}
		response = commentMutationResponse{Comment: records[0], Duplicate: !changed}
		return nil
	})
	return response, err
}

// storeCommentThreadState writes the actor's state of a thread and says
// whether anything changed. Its row stays once neither is set: its time is
// where they read the thread to, and marking it read moves that to now.
func storeCommentThreadState(app core.App, commentID, actorID string, input commentThreadStateInput) (bool, error) {
	params := dbx.Params{"commentId": commentID, "userId": actorID}
	current := storedCommentThreadState{}
	err := app.DB().NewQuery(`
		SELECT [[comment_id]],[[replies_muted]],[[marked_unread]],[[updated_at]] FROM {{leafen_comment_thread_states}}
		WHERE [[comment_id]]={:commentId} AND [[user_id]]={:userId}
	`).Bind(params).One(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	next := current
	if input.RepliesMuted != nil {
		next.RepliesMuted = *input.RepliesMuted
	}
	if input.Unread != nil {
		next.MarkedUnread = *input.Unread
	}
	reading := input.Unread != nil && !*input.Unread
	if next.RepliesMuted == current.RepliesMuted && next.MarkedUnread == current.MarkedUnread && !reading {
		return false, nil
	}
	params["muted"], params["unread"] = next.RepliesMuted, next.MarkedUnread
	// Turning replies off or on leaves where they read to.
	params["at"] = time.Now().UTC().Format(time.RFC3339Nano)
	if !reading && next.MarkedUnread == current.MarkedUnread && current.UpdatedAt != "" {
		params["at"] = current.UpdatedAt
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_comment_thread_states}} ([[comment_id]],[[user_id]],[[replies_muted]],[[marked_unread]],[[updated_at]])
		VALUES ({:commentId},{:userId},{:muted},{:unread},{:at})
		ON CONFLICT ([[comment_id]],[[user_id]]) DO UPDATE SET
			[[replies_muted]]=excluded.[[replies_muted]], [[marked_unread]]=excluded.[[marked_unread]],
			[[updated_at]]=excluded.[[updated_at]]
	`).Bind(params).Execute()
	return err == nil, err
}

// attachCommentThreadStates sets, on the first comments of threads among
// the records, the actor's own state of each thread, and on each comment
// someone else wrote whether it is new to the actor: they follow its thread
// now (commentThreadsFollowed) — a block's followed only through a bell, since
// before it was written — and it came after where they read the thread
// to — or, in a thread they never read, they were told of it and its notice
// (its comment or mention notice, found by the notice's own id) is unread —
// save a reply under a page's title written while they were away from a
// discussion they have not been in (keepAwayRepliesRead). A
// member is told of every comment on a page they reach, followed or not, so a
// page they come to follow shows what they never read as unread (Notion's,
// seen 2026-11-25); an imported comment told no one.
func attachCommentThreadStates(app core.App, actorID string, records []commentRecord) error {
	if actorID == "" || len(records) == 0 {
		return nil
	}
	params := dbx.Params{"userId": actorID}
	placeholders := []string{}
	seenThread := map[string]bool{}
	for _, record := range records {
		thread := record.ID
		if record.ParentID != "" {
			thread = record.ParentID
		}
		if seenThread[thread] {
			continue
		}
		seenThread[thread] = true
		key := fmt.Sprintf("thread%d", len(placeholders))
		params[key] = thread
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rows := []storedCommentThreadState{}
	if err := app.DB().NewQuery(`
		SELECT [[comment_id]],[[replies_muted]],[[marked_unread]],[[updated_at]] FROM {{leafen_comment_thread_states}}
		WHERE [[user_id]]={:userId} AND [[comment_id]] IN (` + strings.Join(placeholders, ",") + `)
	`).Bind(params).All(&rows); err != nil {
		return err
	}
	byThread := map[string]storedCommentThreadState{}
	for _, row := range rows {
		byThread[row.CommentID] = row
	}
	told, err := commentNoticesFor(app, actorID, records)
	if err != nil {
		return err
	}
	followed, err := commentThreadsFollowed(context.Background(), app, actorID, records)
	if err != nil {
		return err
	}
	for index := range records {
		record := &records[index]
		if record.ParentID == "" {
			state := byThread[record.ID]
			record.RepliesMuted, record.Unread = state.RepliesMuted, state.MarkedUnread
		}
		thread := record.ID
		if record.ParentID != "" {
			thread = record.ParentID
		}
		follow, follows := followed[thread]
		if record.AuthorID == actorID || !follows {
			continue
		}
		created, createdErr := time.Parse(time.RFC3339Nano, record.CreatedAt)
		if follow.newAfter > 0 && (createdErr != nil || !created.After(time.UnixMilli(follow.newAfter))) {
			continue
		}
		state, read := byThread[thread]
		if !read {
			notice, wasTold := told[record.ID]
			record.New = wasTold && notice.unread
			continue
		}
		readTo, err := time.Parse(time.RFC3339Nano, state.UpdatedAt)
		record.New = err == nil && createdErr == nil && created.After(readTo)
	}
	return keepAwayRepliesRead(app, actorID, records, byThread)
}

type commentNotice struct{ unread bool }

// commentNoticesFor finds, by their ids, the actor's notices of the records'
// comments — a reply's or a comment's own, or a mention of them in it — as
// each was first written (comment_notification_delivery.go).
func commentNoticesFor(app core.App, actorID string, records []commentRecord) (map[string]commentNotice, error) {
	params := dbx.Params{}
	placeholders := []string{}
	for _, record := range records {
		if record.AuthorID == actorID || record.WorkspaceID == "" {
			continue
		}
		for _, key := range []string{
			fmt.Sprintf("comment:%s:1", record.ID),
			fmt.Sprintf("mention:comment:%s:%s:1", record.ID, actorID),
		} {
			name := fmt.Sprintf("notice%d", len(placeholders))
			params[name] = commentNotificationID(record.WorkspaceID, actorID, key)
			placeholders = append(placeholders, "{:"+name+"}")
		}
	}
	told := map[string]commentNotice{}
	if len(placeholders) == 0 {
		return told, nil
	}
	found := []struct {
		CommentID string `db:"comment_id"`
		ReadAt    int64  `db:"read_at"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[comment_id]],[[read_at]] FROM {{leafen_notifications}} WHERE [[id]] IN (` + strings.Join(placeholders, ",") + `)
	`).Bind(params).All(&found); err != nil {
		return nil, err
	}
	for _, notice := range found {
		told[notice.CommentID] = commentNotice{unread: told[notice.CommentID].unread || notice.ReadAt == 0}
	}
	return told, nil
}

// attachCommentDetails sets what comment records carry besides the comment:
// its attachments' files, everyone's reactions and the actor's own state of
// each thread.
func attachCommentDetails(app core.App, actorID string, records []commentRecord) error {
	if len(records) == 0 {
		return nil
	}
	if err := attachCommentAttachments(app, records); err != nil {
		return err
	}
	if err := attachCommentReactions(app, records); err != nil {
		return err
	}
	return attachCommentThreadStates(app, actorID, records)
}

// attachCommentDetail sets one comment record's details.
func attachCommentDetail(app core.App, actorID string, record *commentRecord) error {
	records := []commentRecord{*record}
	if err := attachCommentDetails(app, actorID, records); err != nil {
		return err
	}
	*record = records[0]
	return nil
}

// commentThreadRepliesMuted reports whether the person turned off the
// notifications of a thread's replies.
func commentThreadRepliesMuted(ctx context.Context, app core.App, threadID, userID string) (bool, error) {
	muted := 0
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_comment_thread_states}}
		WHERE [[comment_id]]={:threadId} AND [[user_id]]={:userId} AND [[replies_muted]]=1
	`).WithContext(ctx).Bind(dbx.Params{"threadId": threadID, "userId": userID}).Row(&muted)
	return muted > 0, err
}
