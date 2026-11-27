package app

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errNotificationInboxInvalid = errors.New("notification Inbox request is invalid")
)

type storedNotificationInboxRecord struct {
	ID           string `db:"id"`
	ActivityKey  string `db:"activity_key"`
	Kind         string `db:"kind"`
	PageID       string `db:"page_id"`
	BlockID      string `db:"block_id"`
	CommentID    string `db:"comment_id"`
	ActorID      string `db:"actor_id"`
	Title        string `db:"title"`
	Preview      string `db:"preview"`
	PreviewSpans string `db:"preview_spans"`
	Target       string `db:"target"`
	OccurredAt   int64  `db:"occurred_at"`
	ReadAt       int64  `db:"read_at"`
	ArchivedAt   int64  `db:"archived_at"`
	Personal     bool   `db:"personal"`
	// ActorConnection names the connection that wrote it (noticeConnection).
	ActorConnection string `db:"actor_connection"`
}

// Inbox scopes: personal notifications, as Notion's inbox shows by default,
// or every workspace update.
const (
	notificationInboxScopePersonal = "personal"
	notificationInboxScopeAll      = "all"
)

func readNotificationInboxSnapshot(
	app core.App,
	principalID string,
	workspaceID string,
	rawLimit string,
) (notificationInboxResponse, error) {
	return readNotificationInboxScopedSnapshot(app, principalID, workspaceID, rawLimit, notificationInboxScopeAll)
}

// Inbox states, as Notion's inbox filter: the notifications still in the
// inbox, only the unread ones there, or the archived ones.
const (
	notificationInboxStateInbox    = "inbox"
	notificationInboxStateUnread   = "unread"
	notificationInboxStateArchived = "archived"
)

func readNotificationInboxScopedSnapshot(
	app core.App,
	principalID string,
	workspaceID string,
	rawLimit string,
	scope string,
) (notificationInboxResponse, error) {
	return readNotificationInboxFilteredSnapshot(app, principalID, workspaceID, rawLimit, scope, "")
}

func readNotificationInboxFilteredSnapshot(
	app core.App,
	principalID string,
	workspaceID string,
	rawLimit string,
	scope string,
	state string,
) (notificationInboxResponse, error) {
	return readNotificationInboxPage(app, principalID, workspaceID, rawLimit, scope, state, "")
}

// readNotificationInboxPage reads one page of a tab, after the cursor a page
// before it ended at.
func readNotificationInboxPage(
	app core.App,
	principalID string,
	workspaceID string,
	rawLimit string,
	scope string,
	state string,
	rawCursor string,
) (notificationInboxResponse, error) {
	limit, err := notificationInboxLimit(rawLimit)
	if scope == "" {
		scope = notificationInboxScopeAll
	}
	if state == "" {
		state = notificationInboxStateInbox
	}
	cursor, cursorErr := parseNotificationInboxCursor(rawCursor)
	switch {
	case err != nil:
		return notificationInboxResponse{}, err
	case cursorErr != nil:
		return notificationInboxResponse{}, cursorErr
	case strings.TrimSpace(principalID) == "" || strings.TrimSpace(workspaceID) == "":
		return notificationInboxResponse{}, refuse(errNotificationInboxInvalid, "the workspace ID in the path is empty")
	case scope != notificationInboxScopeAll && scope != notificationInboxScopePersonal:
		return notificationInboxResponse{}, refuse(errNotificationInboxInvalid, "scope is %s or %s, not %q", notificationInboxScopeAll, notificationInboxScopePersonal, scope)
	case state != notificationInboxStateInbox && state != notificationInboxStateUnread && state != notificationInboxStateArchived:
		return notificationInboxResponse{}, refuse(errNotificationInboxInvalid, "state is %s, %s or %s, not %q",
			notificationInboxStateInbox, notificationInboxStateUnread, notificationInboxStateArchived, state)
	}
	response := notificationInboxResponse{}
	err = runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		var readErr error
		response, readErr = readNotificationInboxFilteredSnapshotInTransaction(
			txApp, principalID, workspaceID, limit, scope == notificationInboxScopePersonal, state, cursor,
		)
		return readErr
	})
	return response, err
}

func readNotificationInboxSnapshotInTransaction(
	app core.App,
	principalID string,
	workspaceID string,
	limit int,
) (notificationInboxResponse, error) {
	return readNotificationInboxScopedSnapshotInTransaction(app, principalID, workspaceID, limit, false)
}

func readNotificationInboxScopedSnapshotInTransaction(
	app core.App,
	principalID string,
	workspaceID string,
	limit int,
	personalOnly bool,
) (notificationInboxResponse, error) {
	return readNotificationInboxFilteredSnapshotInTransaction(
		app, principalID, workspaceID, limit, personalOnly, notificationInboxStateInbox, notificationInboxCursor{},
	)
}

// notificationInboxWindow is how many notifications one read takes from the
// table while filling a page of a tab or finding the newest one shown; a
// tab reads on to its end however many there are (2026-10-13: only the
// newest 2,000 were read, so older ones, the archive past them, and the
// unread count and badges beyond them were lost).
const notificationInboxWindow = 500

// notificationInboxCursor is where a page of a tab ended: the last
// notification it listed, newest first.
type notificationInboxCursor struct {
	OccurredAt int64
	ID         string
}

func parseNotificationInboxCursor(raw string) (notificationInboxCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return notificationInboxCursor{}, nil
	}
	at, id, found := strings.Cut(raw, ".")
	occurredAt, err := strconv.ParseInt(at, 10, 64)
	if !found || err != nil || occurredAt <= 0 || id == "" || len(id) > 256 {
		return notificationInboxCursor{}, refuse(errNotificationInboxInvalid, "cursor isn't one this list gave: leave it out to read from the start")
	}
	return notificationInboxCursor{OccurredAt: occurredAt, ID: id}, nil
}

func (cursor notificationInboxCursor) String() string {
	return strconv.FormatInt(cursor.OccurredAt, 10) + "." + cursor.ID
}

// notificationInboxStateCondition is the SQL a tab keeps its notifications by (notificationInInboxSQL).
func notificationInboxStateCondition(state string) string {
	switch state {
	case notificationInboxStateArchived:
		return "[[archived_at]] != 0 AND " + notificationInInboxSQL("")
	case notificationInboxStateUnread:
		return "[[archived_at]] = 0 AND [[read_at]] = 0 AND " + notificationInInboxSQL("")
	}
	return "[[archived_at]] = 0 AND " + notificationInInboxSQL("")
}

// notificationInboxWindowAfter reads the next window of a tab's
// notifications after the cursor, newest first, someone else's doing only.
func notificationInboxWindowAfter(
	app core.App, principalID, workspaceID string, personalOnly bool, state string, cursor notificationInboxCursor,
) ([]storedNotificationInboxRecord, error) {
	rows := []storedNotificationInboxRecord{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[activity_key]], [[kind]], [[page_id]], [[block_id]],
			[[comment_id]], [[actor_id]], [[title]], [[preview]], [[preview_spans]], [[target]],
			[[occurred_at]], [[read_at]], [[archived_at]], [[personal]], [[actor_connection]]
		FROM {{leafen_notifications}}
		INDEXED BY [[idx_leafen_notifications_user_activity]]
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:userId}
			AND ` + notificationShownSQL("") + `
			AND ({:personalOnly} = 0 OR [[personal]] = 1)
			AND ` + notificationInboxStateCondition(state) + `
			AND ` + notificationGroupedAwaySQL() + `
			AND ({:cursorAt} = 0 OR [[occurred_at]] < {:cursorAt}
				OR ([[occurred_at]] = {:cursorAt} AND [[id]] < {:cursorId}))
		ORDER BY [[occurred_at]] DESC, [[id]] DESC
		LIMIT {:window}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "userId": principalID, "personalOnly": personalOnly,
		"cursorAt": cursor.OccurredAt, "cursorId": cursor.ID, "window": notificationInboxWindow,
	}).All(&rows)
	return rows, err
}

// notificationShownSQL keeps, of a person's notifications, what someone
// else did and the reminders Leafen sends them — their own reminder too,
// which names them as its actor (Notion's inbox shows it) — and an
// automation's messages, an unnamed one's naming its maker as its actor, not
// what they did themselves. notification prefixes the table's columns.
//
// A mention in a page's text shows while the mention is there, as Notion's
// (2026-10-27): deleted, its notification leaves the inbox and its counts;
// undone, it comes back as it was, read or not, without being sent again.
// A comment's notification shows while its comment is there, and a
// comment's mention while the comment still names the person (2026-10-28: a
// deleted comment's mention stayed in the inbox).
func notificationShownSQL(notification string) string {
	return "((" + notification + "[[actor_id]] != {:userId} OR " + notification + "[[kind]] = 'reminder' OR " +
		notification + "[[activity_key]] LIKE 'automation-notification:%') AND " + pageMentionShownSQL(notification) +
		" AND " + commentShownSQL(notification) + ")"
}

// commentShownSQL holds a comment's notification to its comment (by the
// comments' primary key); a mention also to the comment's body still holding
// the person's mention (comment_notification_admission.go reads them as
// {"mention":"person","userId":…}).
func commentShownSQL(notification string) string {
	outer := notification
	if outer == "" {
		outer = "{{leafen_notifications}}."
	}
	return "(" + outer + "[[comment_id]] = '' OR EXISTS (SELECT 1 FROM {{leafen_comments}} [[shown_comment]] " +
		"WHERE [[shown_comment]].[[id]] = " + outer + "[[comment_id]] AND (" + outer + "[[activity_key]] NOT LIKE 'mention:%' " +
		"OR instr([[shown_comment]].[[body_json]], " + outer + "[[user_id]]) > 0)))"
}

// pageMentionShownSQL holds a page text mention's notification to the
// mention it tells of (page_person_mention_notifications.go), on its page or,
// noticed before database pages kept their own, on the page holding it.
func pageMentionShownSQL(notification string) string {
	outer := notification
	if outer == "" {
		outer = "{{leafen_notifications}}."
	}
	// Only a page text mention's key reads "mention:block:" (commentNotificationActivityKey);
	// a person property's "mention" (database-person:) holds to no text.
	return "(" + outer + "[[activity_key]] NOT LIKE 'mention:block:%' OR EXISTS (" +
		"SELECT 1 FROM {{leafen_page_person_mentions}} [[shown_mention]] " +
		"WHERE [[shown_mention]].[[page_id]] IN (" + outer + "[[page_id]], " +
		"(SELECT [[shown_block]].[[page_id]] FROM {{leafen_blocks}} [[shown_block]] WHERE [[shown_block]].[[id]] = " + outer + "[[block_id]])) " +
		"AND [[shown_mention]].[[block_id]] = " + outer + "[[block_id]] AND [[shown_mention]].[[user_id]] = " + outer + "[[user_id]]))"
}

// visibleNotificationInboxRows keeps a window's notifications the person
// still sees (shownNotificationSubjects).
func visibleNotificationInboxRows(
	app core.App, principalID, workspaceID string, rows []storedNotificationInboxRecord,
) ([]storedNotificationInboxRecord, error) {
	notices := make([]notificationSubjectRef, 0, len(rows))
	for _, row := range rows {
		notices = append(notices, notificationSubjectRef{PageID: row.PageID, Target: row.Target})
	}
	shown, err := shownNotificationSubjects(app, principalID, workspaceID, notices)
	if err != nil {
		return nil, err
	}
	kept := make([]storedNotificationInboxRecord, 0, len(rows))
	for index, row := range rows {
		if shown[index] {
			kept = append(kept, row)
		}
	}
	return kept, nil
}

// notificationUnreadCount is the unread count a person's inbox shows in a
// workspace, for badges and the workspace list.
func notificationUnreadCount(app core.App, principalID, workspaceID string, personalOnly bool) (int, error) {
	if _, err := authorizeCommentNotificationScope(app, principalID, workspaceID); err != nil {
		return 0, err
	}
	return notificationInboxCount(app, principalID, workspaceID, personalOnly, notificationInboxStateUnread)
}

// The unread count and the boundary for marking everything read always come
// from the notifications in the inbox; the items are the asked state's, a
// page of them after the cursor.
func readNotificationInboxFilteredSnapshotInTransaction(
	app core.App,
	principalID string,
	workspaceID string,
	limit int,
	personalOnly bool,
	state string,
	cursor notificationInboxCursor,
) (notificationInboxResponse, error) {
	if limit < 1 || limit > commentNotificationInboxMaxSize {
		return notificationInboxResponse{}, errNotificationInboxInvalid
	}
	if _, err := authorizeCommentNotificationScope(app, principalID, workspaceID); err != nil {
		return notificationInboxResponse{}, err
	}
	listed := []storedNotificationInboxRecord{}
	for after := cursor; len(listed) <= limit; {
		rows, err := notificationInboxWindowAfter(app, principalID, workspaceID, personalOnly, state, after)
		if err != nil {
			return notificationInboxResponse{}, err
		}
		kept, err := visibleNotificationInboxRows(app, principalID, workspaceID, rows)
		if err != nil {
			return notificationInboxResponse{}, err
		}
		listed = append(listed, kept...)
		if len(rows) < notificationInboxWindow {
			break
		}
		last := rows[len(rows)-1]
		after = notificationInboxCursor{OccurredAt: last.OccurredAt, ID: last.ID}
	}
	response := notificationInboxResponse{
		WorkspaceID: workspaceID,
		Items:       []notificationInboxRecord{}, ActorProfiles: []commentActorProfile{},
		Complete: true,
	}
	if len(listed) > limit {
		listed = listed[:limit]
		response.HasMore = true
		last := listed[len(listed)-1]
		response.NextCursor = notificationInboxCursor{OccurredAt: last.OccurredAt, ID: last.ID}.String()
	}
	var err error
	if response.UnreadCount, err = notificationInboxCount(
		app, principalID, workspaceID, personalOnly, notificationInboxStateUnread,
	); err != nil {
		return notificationInboxResponse{}, err
	}
	if response.Total, err = notificationInboxCount(app, principalID, workspaceID, personalOnly, state); err != nil {
		return notificationInboxResponse{}, err
	}
	// Marking everything read reaches the newest notification shown in the
	// inbox.
	for after := (notificationInboxCursor{}); ; {
		rows, err := notificationInboxWindowAfter(app, principalID, workspaceID, personalOnly, notificationInboxStateInbox, after)
		if err != nil {
			return notificationInboxResponse{}, err
		}
		kept, err := visibleNotificationInboxRows(app, principalID, workspaceID, rows)
		if err != nil {
			return notificationInboxResponse{}, err
		}
		if len(kept) > 0 {
			response.Through = &notificationInboxBoundary{
				OccurredAt: notificationInboxTime(kept[0].OccurredAt), ID: kept[0].ID,
			}
			break
		}
		if len(rows) < notificationInboxWindow {
			break
		}
		last := rows[len(rows)-1]
		after = notificationInboxCursor{OccurredAt: last.OccurredAt, ID: last.ID}
	}
	actorIDs := make([]string, 0, len(listed))
	for _, row := range listed {
		response.Items = append(response.Items, notificationInboxRecord{
			ID: row.ID, ActivityKey: row.ActivityKey, Kind: row.Kind,
			PageID: row.PageID, BlockID: row.BlockID, CommentID: row.CommentID,
			ActorID: row.ActorID, Title: row.Title, Preview: row.Preview, PreviewSpans: notificationPreviewSpans(row.PreviewSpans),
			Target: row.Target, OccurredAt: notificationInboxTime(row.OccurredAt),
			ReadAt: notificationInboxTime(row.ReadAt), Personal: row.Personal,
			ActorConnection: noticeKeptConnection(row.ActorConnection),
		})
		actorIDs = append(actorIDs, row.ActorID)
	}
	response.ActorProfiles, err = readCanonicalActorProfiles(app, actorIDs)
	if err != nil {
		return notificationInboxResponse{}, err
	}
	notificationInboxItemsNow(app, principalID, response.Items)
	if err := markNotificationGroups(app, principalID, workspaceID, personalOnly, listed, response.Items); err != nil {
		return notificationInboxResponse{}, err
	}
	if err := markResolvedNotificationThreads(app, workspaceID, response.Items); err != nil {
		return notificationInboxResponse{}, err
	}
	return response, nil
}

// markResolvedNotificationThreads marks the notices whose comment's
// discussion is resolved now, which Notion's inbox says under the notice
// ("✓ 해결", seen 2026-11-06). A reply's discussion is its thread's root.
func markResolvedNotificationThreads(app core.App, workspaceID string, items []notificationInboxRecord) error {
	ids := make([]any, 0, len(items))
	for _, item := range items {
		if item.CommentID != "" {
			ids = append(ids, item.CommentID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows := []struct {
		ID string `db:"id"`
	}{}
	if err := app.DB().Select("comment.id").
		From(commentsTable+" comment").
		InnerJoin(commentsTable+" root", dbx.NewExp(
			"[[root.id]] = CASE WHEN [[comment.parent_id]] = '' THEN [[comment.id]] ELSE [[comment.parent_id]] END")).
		Where(dbx.HashExp{"comment.workspace_id": workspaceID, "root.resolved": 1}).
		AndWhere(dbx.In("comment.id", ids...)).
		All(&rows); err != nil {
		return err
	}
	resolved := make(map[string]bool, len(rows))
	for _, row := range rows {
		resolved[row.ID] = true
	}
	for index := range items {
		items[index].Resolved = resolved[items[index].CommentID]
	}
	return nil
}

func notificationInboxLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return commentNotificationInboxDefaultSize, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > commentNotificationInboxMaxSize {
		return 0, refuse(errNotificationInboxInvalid, "limit is a number from 1 to %d, not %q", commentNotificationInboxMaxSize, raw)
	}
	return value, nil
}

func notificationInboxTime(milliseconds int64) string {
	if milliseconds <= 0 {
		return ""
	}
	return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339Nano)
}

// notificationPreviewSpans is a preview's mentions as stored, none when it has none.
func notificationPreviewSpans(stored string) json.RawMessage {
	if stored == "" || !json.Valid([]byte(stored)) {
		return nil
	}
	return json.RawMessage(stored)
}
