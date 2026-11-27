package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// noteDiscussionsSeenAfterOpen reads, as someone opens a page or a database
// row's own page themselves, the discussions there they have now been in
// (noteDiscussionsSeen), after the socket is let in: opening it waits on none
// of it.
func noteDiscussionsSeenAfterOpen(app core.App, userID, workspaceID, room string, rowPage bool) {
	go func() {
		if err := noteDiscussionsSeen(app, userID, workspaceID, room, rowPage); err != nil {
			app.Logger().Warn("leafen discussions seen on open failed", "error", err)
		}
	}()
}

// noteDiscussionsSeen reads to now, for someone who opens a page or a row's
// own page, the discussions under its title they follow that hold someone
// else's comment after where they read them to and still show read — a reply
// kept read while they were away (keepAwayRepliesRead): they have been in
// them past those words now, so a reply written while they are away again is
// new to them, with "New" before it, as Notion's (comment_readers_present.go).
// One that shows unread stays so: seeing it reads nothing (comment_thread_states.go).
func noteDiscussionsSeen(app core.App, userID, workspaceID, room string, rowPage bool) error {
	where := `[[page_id]]={:room} AND [[target_kind]]='page'`
	if rowPage {
		where = `[[target_kind]]='database_row' AND [[target_id]]={:room}`
	}
	rows := []storedComment{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],
			[[block_id]],[[parent_id]],[[author_id]],
			[[body_json]],[[notion_discussion_id]],[[resolved]],[[revision]],
			[[created_at]],[[updated_at]],[[edited_at]]
		FROM {{leafen_comments}}
		WHERE [[workspace_id]]={:workspaceId} AND ` + where + ` AND [[block_id]]=''
	`).Bind(dbx.Params{"workspaceId": workspaceID, "room": room}).All(&rows); err != nil {
		return err
	}
	// A row's property discussions are not under its title (titleDiscussion).
	onProperty := map[string]bool{}
	for _, row := range rows {
		if property, _ := commentPropertyAnchor(row.BodyJSON); row.ParentID == "" && property != "" {
			onProperty[row.ID] = true
		}
	}
	records := make([]commentRecord, 0, len(rows))
	others := false
	for _, row := range rows {
		if onProperty[row.ID] || onProperty[row.ParentID] {
			continue
		}
		record, err := renderComment(row)
		if err != nil {
			return err
		}
		records = append(records, record)
		others = others || row.AuthorID != userID
	}
	if !others {
		return nil
	}
	followed, err := commentThreadsFollowed(context.Background(), app, userID, records)
	if err != nil {
		return err
	}
	if err := attachCommentThreadStates(app, userID, records); err != nil {
		return err
	}
	readTo, err := commentThreadsReadTo(app, userID, records)
	if err != nil {
		return err
	}
	type look struct{ unread, unseen bool }
	threads := map[string]look{}
	for _, record := range records {
		thread := record.ID
		if record.ParentID != "" {
			thread = record.ParentID
		}
		seen := threads[thread]
		seen.unread = seen.unread || record.New || (record.ParentID == "" && record.Unread)
		if record.AuthorID != userID {
			created, createdErr := time.Parse(time.RFC3339Nano, record.CreatedAt)
			to, read := readTo[thread]
			seen.unseen = seen.unseen || !read || (createdErr == nil && created.After(to))
		}
		threads[thread] = seen
	}
	return app.RunInTransaction(func(txApp core.App) error {
		read := false
		for thread, seen := range threads {
			if _, follows := followed[thread]; !follows || seen.unread || !seen.unseen {
				continue
			}
			if _, err := storeCommentThreadState(txApp, thread, userID, commentThreadStateInput{Unread: &read}); err != nil {
				return err
			}
		}
		return nil
	})
}

// commentThreadsReadTo reads where the person read each thread of the
// records to; a thread they never read has none.
func commentThreadsReadTo(app core.App, userID string, records []commentRecord) (map[string]time.Time, error) {
	params := dbx.Params{"userId": userID}
	names := []string{}
	for _, record := range records {
		if record.ParentID == "" {
			name := fmt.Sprintf("thread%d", len(names))
			params[name] = record.ID
			names = append(names, "{:"+name+"}")
		}
	}
	readTo := map[string]time.Time{}
	if len(names) == 0 {
		return readTo, nil
	}
	states := []storedCommentThreadState{}
	if err := app.DB().NewQuery(`
		SELECT [[comment_id]],[[replies_muted]],[[marked_unread]],[[updated_at]] FROM {{leafen_comment_thread_states}}
		WHERE [[user_id]]={:userId} AND [[comment_id]] IN (` + strings.Join(names, ",") + `)
	`).Bind(params).All(&states); err != nil {
		return nil, err
	}
	for _, state := range states {
		if at, err := time.Parse(time.RFC3339Nano, state.UpdatedAt); err == nil {
			readTo[state.CommentID] = at
		}
	}
	return readTo, nil
}
