package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A reply in a discussion under a page's title, written while the reader did
// not have the page open, is new to them only once they have been in that
// discussion past someone else's words — had the page open while it held
// someone else's comment, or read it so — as Notion's (seen 2026-11-25 in
// Chrome at 1440, new tabs 20 seconds to 6 minutes later, a guest replying on
// a Galaxy A90): a reply written while the reader was away showed read (ten
// times, one after they had opened the page holding only their own comment),
// and so did two replies written while away with the page not opened between
// (twice); once they had opened the page holding such a reply, the next one
// written while away was unread with "New" before it (four times, in two
// workers' rounds); one written while they had the page open was unread there
// and in new tabs later (three times). A block's discussion was unread either
// way (five times). How long ago it was written or when its push came did not
// decide it. A database row's own discussions are held so too (titleDiscussion).
// So each reply keeps who had its page's room open as it was written, its
// writer aside (present_user_ids_json; migration 863), and opening the page
// reads the discussions there that the reader follows and that show read
// (noteDiscussionsSeen), which is where they have been in them.

// Migration 863 keeps, on a comment, who had its page open as it was written.
// Comments written before it say no one, as one written to an empty room does.

func ensureNativeCommentReadersPresentSchema(app core.App) error {
	if _, err := app.DB().NewQuery(`ALTER TABLE {{leafen_comments}}
		ADD COLUMN [[present_user_ids_json]] TEXT NOT NULL DEFAULT '[]'`).Execute(); err != nil {
		return err
	}
	return validateNativeCommentReadersPresentSchema(app)
}

func validateNativeCommentReadersPresentSchema(app core.App) error {
	var columns int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_comments')
		WHERE [[name]] = 'present_user_ids_json' AND [[notnull]] = 1`).Row(&columns); err != nil {
		return err
	}
	if columns != 1 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}

// peopleInRoom says who has a room open now themselves: a socket of theirs
// admitted to it under a sign-in, not under a connection's key.
func (a *ygoRoomAuthorization) peopleInRoom(room string) map[string]bool {
	a.recheck.mu.Lock()
	defer a.recheck.mu.Unlock()
	people := map[string]bool{}
	for _, socket := range a.recheck.sockets {
		if socket.admitted && socket.room == room && !strings.HasPrefix(socket.access.SessionID, "key:") {
			people[socket.access.UserID] = true
		}
	}
	return people
}

// noteReadersPresent keeps, on a new reply to a page's or a database row's
// own discussion, who has that page — the row's own page for a row — open as
// it is written, its writer aside. With no one there the comment's own "no
// one" stands.
func noteReadersPresent(app core.App, row storedComment) error {
	room := row.PageID
	if row.TargetKind == commentTargetDatabaseRow {
		room = row.TargetID
	} else if row.TargetKind != commentTargetPage {
		return nil
	}
	if row.ParentID == "" || room == "" {
		return nil
	}
	rooms, _ := app.Store().Get(ygoRoomAuthorizationStoreKey).(*ygoRoomAuthorization)
	if rooms == nil {
		return nil
	}
	people := rooms.peopleInRoom(room)
	delete(people, row.AuthorID)
	if len(people) == 0 {
		return nil
	}
	present, err := json.Marshal(slices.Sorted(maps.Keys(people)))
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_comments}} SET [[present_user_ids_json]]={:present} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"present": string(present), "id": row.ID}).Execute()
	return err
}

// titleThreadComment is a comment of a discussion as the away rule reads it.
type titleThreadComment struct {
	ID        string `db:"id"`
	ParentID  string `db:"parent_id"`
	PageID    string `db:"page_id"`
	BlockID   string `db:"block_id"`
	AuthorID  string `db:"author_id"`
	BodyJSON  string `db:"body_json"`
	CreatedAt string `db:"created_at"`
	Present   string `db:"present_user_ids_json"`
}

// titleDiscussion says whether a discussion, by its first comment, is one
// under a page's or a database row's title: on no block, and not on one of
// the row's properties (comment_property_anchors.go). A row's is held as a
// page's is, as Notion's row page (seen 2026-11-25 in Chrome at 1440, two
// tabs at different times: a lone reply written away read, and after the row
// page was opened holding it, the next one unread with "New" before it).
func titleDiscussion(root titleThreadComment) bool {
	property, _ := commentPropertyAnchor(root.BodyJSON)
	return root.BlockID == "" && property == ""
}

// keepAwayRepliesRead takes "new" off the replies among the records, in
// discussions under a page's or a row's title, that the actor did not have the
// page open for as they were written while they have not been in the
// discussion past someone else's comment: their thread state reads it to a
// time after one. read is the actor's thread states (attachCommentThreadStates).
func keepAwayRepliesRead(app core.App, actorID string, records []commentRecord, read map[string]storedCommentThreadState) error {
	threads := map[string][]string{}
	for _, record := range records {
		if record.New && record.ParentID != "" && !slices.Contains(threads[record.WorkspaceID], record.ParentID) {
			threads[record.WorkspaceID] = append(threads[record.WorkspaceID], record.ParentID)
		}
	}
	kept := map[string]bool{}
	for workspaceID, ids := range threads {
		byThread, err := titleThreadComments(app, workspaceID, ids)
		if err != nil {
			return err
		}
		for thread, comments := range byThread {
			if beenPastOthers(actorID, comments, read[thread]) {
				continue
			}
			for _, comment := range comments {
				present := []string{}
				if comment.ParentID != "" && (json.Unmarshal([]byte(comment.Present), &present) != nil || !slices.Contains(present, actorID)) {
					kept[comment.ID] = true
				}
			}
		}
	}
	for index := range records {
		if kept[records[index].ID] {
			records[index].New = false
		}
	}
	return nil
}

// titleThreadComments reads the comments of those of the threads, by their
// first comments, that are discussions under a title, by thread: the first
// comments by their ids, then their replies where they are stored.
func titleThreadComments(app core.App, workspaceID string, threadIDs []string) (map[string][]titleThreadComment, error) {
	const columns = `[[id]],[[parent_id]],[[page_id]],[[block_id]],[[author_id]],[[body_json]],[[created_at]],[[present_user_ids_json]]`
	params := dbx.Params{"workspaceId": workspaceID}
	names := make([]string, 0, len(threadIDs))
	for index, id := range threadIDs {
		name := fmt.Sprintf("thread%d", index)
		params[name] = id
		names = append(names, "{:"+name+"}")
	}
	roots := []titleThreadComment{}
	if err := app.DB().NewQuery(`SELECT ` + columns + ` FROM {{leafen_comments}}
		WHERE [[workspace_id]]={:workspaceId} AND [[id]] IN (` + strings.Join(names, ",") + `) AND [[parent_id]]=''
	`).Bind(params).All(&roots); err != nil {
		return nil, err
	}
	byThread := map[string][]titleThreadComment{}
	byPage := map[string][]string{}
	for _, root := range roots {
		if titleDiscussion(root) {
			byThread[root.ID] = []titleThreadComment{root}
			byPage[root.PageID] = append(byPage[root.PageID], root.ID)
		}
	}
	for pageID, ids := range byPage {
		params := dbx.Params{"workspaceId": workspaceID, "pageId": pageID}
		names := make([]string, 0, len(ids))
		for index, id := range ids {
			name := fmt.Sprintf("thread%d", index)
			params[name] = id
			names = append(names, "{:"+name+"}")
		}
		replies := []titleThreadComment{}
		if err := app.DB().NewQuery(`SELECT ` + columns + ` FROM {{leafen_comments}}
			WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId} AND [[parent_id]] IN (` + strings.Join(names, ",") + `)
		`).Bind(params).All(&replies); err != nil {
			return nil, err
		}
		for _, reply := range replies {
			byThread[reply.ParentID] = append(byThread[reply.ParentID], reply)
		}
	}
	return byThread, nil
}

// beenPastOthers says whether the actor's state of a discussion reads it to
// a time after one of someone else's comments in it.
func beenPastOthers(actorID string, comments []titleThreadComment, state storedCommentThreadState) bool {
	readTo, err := time.Parse(time.RFC3339Nano, state.UpdatedAt)
	if err != nil {
		return false
	}
	for _, comment := range comments {
		created, err := time.Parse(time.RFC3339Nano, comment.CreatedAt)
		if err == nil && comment.AuthorID != actorID && created.Before(readTo) {
			return true
		}
	}
	return false
}
