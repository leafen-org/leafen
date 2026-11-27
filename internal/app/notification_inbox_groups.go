package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's comment notifications go on one line of the inbox, as Notion's
// (seen 2026-11-11): one that comes while the one before it on the same page
// is still unread and in the inbox joins it, the newest standing for them all
// with its comment's words and, over two discussions or more, "토론 N건". A
// line read or archived before the next comment came stays as it is; the next
// begins a new line, unless it is a reply in the same discussion that came
// soon after the comment before it (notificationGroupReadReplyWindow): that
// one takes the read line back, unread again. Its unread count is the lines',
// so a reply to an unread line moves no badge. Opening, reading, archiving or
// bringing back a line does so to every notification on it.

// notificationGroupReadReplyWindow is how soon after a read line's comment a
// reply in the same discussion still joins it, in milliseconds. Notion's
// (2026-11-16, a guest reading a member's comments in Chrome, each line read
// within a minute of its comment): a reply 2½ minutes after the comment
// joined its read line; replies 4½ and 9 minutes after began a new line, as
// did another discussion's mention on the page 5 minutes after (2026-11-15:
// a reply 1–2 minutes after joined; 2026-11-12: a later one did not). Where
// between 2½ and 4½ minutes it ends was not measured.
const notificationGroupReadReplyWindow = 3 * 60 * 1000

// notificationGroupReadBatch is how many of a line's notifications one read
// takes. A line reaches back over all of them, a batch after another, so a
// change to it and its "토론 N건" count every one however many there are.
const notificationGroupReadBatch = 500

// notificationIDListSQL matches [[id]] against a list bound as one JSON value
// ({:<name>}, from notificationIDList), so a line of any size goes in one query.
func notificationIDListSQL(name string) string {
	return "[[id]] IN (SELECT [[value]] FROM json_each({:" + name + "}))"
}

// notificationIDList binds a list of ids for notificationIDListSQL.
func notificationIDList(ids []string) string {
	if len(ids) == 0 {
		return "[]"
	}
	encoded, _ := json.Marshal(ids)
	return string(encoded)
}

// notificationCommentKindSQL keeps a comment's notifications, a mention in one too.
func notificationCommentKindSQL(notification string) string {
	return "(" + notification + "[[comment_id]] != '' AND " + notification + "[[kind]] IN ('comment','mention'))"
}

// notificationLogicalPageSQL is the page a notification opens, a database item
// rather than the page holding its database: its target up to its fragment.
func notificationLogicalPageSQL(notification string) string {
	return "substr(" + notification + "[[target]], 1, instr(" + notification + "[[target]] || '#', '#') - 1)"
}

// notificationGroupedAwaySQL leaves out the notifications a newer one stands
// for. It takes {:workspaceId}, {:userId} and {:personalOnly}, as the lists do.
func notificationGroupedAwaySQL() string {
	member := "[[member]]."
	window := " OVER (PARTITION BY " + member + "[[page_id]], " + notificationLogicalPageSQL(member) + ", " +
		member + "[[archived_at]] = 0 ORDER BY " + member + "[[occurred_at]], " + member + "[[id]])"
	discussion := notificationDiscussionSQL(member)
	return "[[id]] NOT IN (SELECT [[grouped]].[[id]] FROM (SELECT " + member + "[[id]] AS [[id]], " +
		member + "[[read_at]] AS [[read_at]], " + member + "[[archived_at]] AS [[archived_at]], " +
		member + "[[occurred_at]] AS [[occurred_at]], " + discussion + " AS [[discussion]], " +
		"LEAD(" + member + "[[occurred_at]])" + window + " AS [[next_at]], " +
		"LEAD(" + discussion + ")" + window + " AS [[next_discussion]] " +
		"FROM {{leafen_notifications}} [[member]] WHERE " + member + "[[workspace_id]] = {:workspaceId} AND " +
		member + "[[user_id]] = {:userId} AND " + notificationCommentKindSQL(member) + " AND ({:personalOnly} = 0 OR " +
		member + "[[personal]] = 1) AND " + notificationShownSQL(member) + ") [[grouped]] " +
		"WHERE [[grouped]].[[next_at]] IS NOT NULL AND ([[grouped]].[[read_at]] = 0 OR [[grouped]].[[read_at]] >= [[grouped]].[[next_at]] " +
		"OR ([[grouped]].[[discussion]] = [[grouped]].[[next_discussion]] AND [[grouped]].[[next_at]] - [[grouped]].[[occurred_at]] <= " +
		strconv.Itoa(notificationGroupReadReplyWindow) + ")) " +
		"AND ([[grouped]].[[archived_at]] = 0 OR [[grouped]].[[archived_at]] >= [[grouped]].[[next_at]]))"
}

// notificationDiscussionSQL is the discussion a comment's notification is in:
// its comment's first, the comment itself when it begins one.
func notificationDiscussionSQL(notification string) string {
	return "(SELECT CASE WHEN [[discussed]].[[parent_id]] = '' THEN [[discussed]].[[id]] ELSE [[discussed]].[[parent_id]] END " +
		"FROM {{leafen_comments}} [[discussed]] WHERE [[discussed]].[[id]] = " + notification + "[[comment_id]])"
}

// joinsReadLine says whether an older notification read before a newer one
// came still goes on the newer one's line: a reply in its discussion soon after.
func (row notificationGroupRow) joinsReadLine(newer notificationGroupRow) bool {
	return row.Discussion != "" && row.Discussion == newer.Discussion &&
		newer.OccurredAt-row.OccurredAt <= notificationGroupReadReplyWindow
}

type notificationGroupRow struct {
	ID         string `db:"id"`
	Kind       string `db:"kind"`
	PageID     string `db:"page_id"`
	CommentID  string `db:"comment_id"`
	Target     string `db:"target"`
	OccurredAt int64  `db:"occurred_at"`
	ReadAt     int64  `db:"read_at"`
	ArchivedAt int64  `db:"archived_at"`
	Personal   bool   `db:"personal"`
	Discussion string `db:"discussion"`
}

func (row notificationGroupRow) comment() bool {
	return row.CommentID != "" && (row.Kind == "comment" || row.Kind == "mention")
}

// notificationGroupBefore lists the notifications a line's newest stands for,
// newest first, without it: those before it on its page, each one still unread
// and in the inbox when the one after it came.
func notificationGroupBefore(
	app core.App, userID, workspaceID string, head notificationGroupRow, personalOnly bool,
) ([]notificationGroupRow, error) {
	if !head.comment() {
		return nil, nil
	}
	if head.Discussion == "" {
		if err := app.DB().NewQuery(`
			SELECT CASE WHEN [[parent_id]] = '' THEN [[id]] ELSE [[parent_id]] END FROM {{leafen_comments}} WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": head.CommentID}).Row(&head.Discussion); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	members := []notificationGroupRow{}
	next := head
	for {
		rows := []notificationGroupRow{}
		err := app.DB().NewQuery(`
			SELECT [[id]], [[kind]], [[page_id]], [[comment_id]], [[target]], [[occurred_at]], [[read_at]], [[archived_at]], [[personal]],
				COALESCE(` + notificationDiscussionSQL("{{leafen_notifications}}.") + `, '') AS [[discussion]]
			FROM {{leafen_notifications}}
			INDEXED BY [[idx_leafen_notifications_page_cleanup]]
			WHERE [[page_id]] = {:pageId} AND [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId}
				AND ` + notificationCommentKindSQL("") + `
				AND ` + notificationLogicalPageSQL("") + ` = {:logical}
				AND ([[archived_at]] = 0) = {:inInbox}
				AND ({:personalOnly} = 0 OR [[personal]] = 1)
				AND ` + notificationShownSQL("") + `
				AND ([[occurred_at]] < {:at} OR ([[occurred_at]] = {:at} AND [[id]] < {:id}))
			ORDER BY [[occurred_at]] DESC, [[id]] DESC
			LIMIT {:limit}
		`).Bind(dbx.Params{
			"pageId": head.PageID, "workspaceId": workspaceID, "userId": userID,
			"logical": notificationLogicalPage(head.Target), "inInbox": head.ArchivedAt == 0,
			"personalOnly": personalOnly, "at": next.OccurredAt, "id": next.ID, "limit": notificationGroupReadBatch,
		}).All(&rows)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if (row.ReadAt != 0 && row.ReadAt < next.OccurredAt && !row.joinsReadLine(next)) ||
				(row.ArchivedAt != 0 && row.ArchivedAt < next.OccurredAt) {
				return members, nil
			}
			members = append(members, row)
			next = row
		}
		if len(rows) < notificationGroupReadBatch {
			return members, nil
		}
	}
}

func notificationLogicalPage(target string) string {
	for index := 0; index < len(target); index++ {
		if target[index] == '#' {
			return target[:index]
		}
	}
	return target
}

// markNotificationGroups names, on each listed line, the notifications it
// stands for and how many discussions they are in.
func markNotificationGroups(
	app core.App, userID, workspaceID string, personalOnly bool,
	listed []storedNotificationInboxRecord, items []notificationInboxRecord,
) error {
	for index, record := range listed {
		head := notificationGroupRow{
			ID: record.ID, Kind: record.Kind, PageID: record.PageID, CommentID: record.CommentID, Target: record.Target,
			OccurredAt: record.OccurredAt, ReadAt: record.ReadAt, ArchivedAt: record.ArchivedAt, Personal: record.Personal,
		}
		members, err := notificationGroupBefore(app, userID, workspaceID, head, personalOnly)
		if err != nil || len(members) == 0 {
			if err != nil {
				return err
			}
			continue
		}
		groupIDs := make([]string, 0, len(members))
		commentIDs := make([]string, 0, len(members)+1)
		commentIDs = append(commentIDs, record.CommentID)
		for _, member := range members {
			groupIDs = append(groupIDs, member.ID)
			commentIDs = append(commentIDs, member.CommentID)
		}
		roots := []string{}
		if err := app.DB().NewQuery(`
			SELECT DISTINCT CASE WHEN [[parent_id]] = '' THEN [[id]] ELSE [[parent_id]] END
			FROM {{leafen_comments}}
			WHERE [[workspace_id]] = {:workspaceId} AND ` + notificationIDListSQL("comments") + `
		`).Bind(dbx.Params{"workspaceId": workspaceID, "comments": notificationIDList(commentIDs)}).Column(&roots); err != nil {
			return err
		}
		items[index].GroupIDs = groupIDs
		items[index].Discussions = len(roots)
	}
	return nil
}

// expandNotificationGroups adds to the asked notifications every one their
// lines stand for, so a change to a line reaches all of it.
func expandNotificationGroups(app core.App, userID, workspaceID string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return ids, nil
	}
	heads := []notificationGroupRow{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[kind]], [[page_id]], [[comment_id]], [[target]], [[occurred_at]], [[read_at]], [[archived_at]], [[personal]],
			COALESCE(` + notificationDiscussionSQL("{{leafen_notifications}}.") + `, '') AS [[discussion]]
		FROM {{leafen_notifications}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId} AND ` + notificationIDListSQL("ids") + `
	`).Bind(dbx.Params{"workspaceId": workspaceID, "userId": userID, "ids": notificationIDList(ids)}).All(&heads); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(ids))
	expanded := make([]string, 0, len(ids))
	add := func(id string) {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			expanded = append(expanded, id)
		}
	}
	for _, id := range ids {
		add(id)
	}
	for _, head := range heads {
		members, err := notificationGroupBefore(app, userID, workspaceID, head, head.Personal)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			add(member.ID)
		}
	}
	sort.Strings(expanded)
	return expanded, nil
}
