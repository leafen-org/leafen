package app

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The workspace's updates list the reader's own permission lines among its
// pages' changes, as Notion's '모든 워크스페이스 업데이트' does (seen
// 2026-11-16 in the free test workspace: "〈누가〉 〈page〉의 권한을 업데이트했습니다."
// with the second account's name and "읽기 허용 → 댓글 허용" under it, at its
// time among the other lines, with no unread mark): someone's share of a
// page given them, changed or taken away (page_share_permission_notices.go),
// which their inbox no longer holds (notificationInInboxSQL). It is theirs
// whatever they see of the page now, its title the one it had then. Whoever
// made the change has the line in their own updates too, "본인 …" with the
// share's person under it (upsertPagePermissionNotice).

type storedWorkspacePermissionNotice struct {
	ID          string `db:"id"`
	ActivityKey string `db:"activity_key"`
	PageID      string `db:"page_id"`
	ActorID     string `db:"actor_id"`
	Title       string `db:"title"`
	OccurredAt  int64  `db:"occurred_at"`
	CreatedAt   int64  `db:"created_at"`
	// Seq is the order the notice was first written in (its rowid).
	Seq int64 `db:"seq"`
	// The notice leading the line this one is under, "" for a line's own
	// (page_share_permission_groups.go).
	Lead string `db:"block_id"`
}

// readWorkspacePermissionNotices reads the reader's permission lines of the
// workspace after the feed's cursor, newest first, as the feed orders its
// lines (workspaceActivityOrder); those of one page where the cursor walks its
// updates (TrashOf). A line is its lead notice; those under it come with it
// (readWorkspacePermissionGroupMembers). Notion's page updates list the
// page's permission lines among its edits, as its workspace updates do (seen
// 2026-11-16 and 2026-11-17 in two tabs, by whoever changed them).
func readWorkspacePermissionNotices(app core.App, userID, workspaceID string, cutoff int64, cursor workspaceActivityCursor, limit int) ([]storedWorkspacePermissionNotice, error) {
	notices := []storedWorkspacePermissionNotice{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[activity_key]],[[page_id]],[[actor_id]],[[title]],[[occurred_at]],[[created_at]],rowid AS [[seq]],[[block_id]]
		FROM {{leafen_notifications}} INDEXED BY [[idx_leafen_notifications_user_activity]]
		WHERE [[workspace_id]]={:workspace} AND [[user_id]]={:user} AND [[kind]]='system' AND [[block_id]]=''
			AND NOT (` + notificationInInboxSQL("") + `) AND [[occurred_at]]>={:cutoff}
			AND ({:page}='' OR [[page_id]]={:page})
			AND ({:time}=0 OR [[occurred_at]]<{:time} OR ([[occurred_at]]={:time} AND {:notice} AND rowid<{:seq}))
		ORDER BY [[occurred_at]] DESC,rowid DESC LIMIT {:limit}`).Bind(dbx.Params{
		"workspace": workspaceID, "user": userID, "cutoff": cutoff, "time": cursor.Time, "seq": cursor.Seq, "notice": cursor.Notice, "limit": limit,
		"page": pagePermissionNoticeOf(cursor.TrashOf),
	}).All(&notices)
	return notices, err
}

// pagePermissionNoticeOf is the page a page's permission notices name, "" for none.
func pagePermissionNoticeOf(pageID string) string {
	if pageID == "" {
		return ""
	}
	return pagePermissionNoticePrefix + pageID
}

// workspacePermissionActivity is a permission line as the feed carries it,
// its lead notice with those under it (members) in permissions, in the order
// first written, the first of them also in the line's own fields: the
// share's person in entityId — the reader, or on the line of whoever changed
// it the person its key names; "generalAccess" with none for everyone in the
// workspace — the roles before and after ("" given, "" taken away).
func workspacePermissionActivity(notice storedWorkspacePermissionNotice, members []storedWorkspacePermissionNotice, readerID string) workspaceActivityRecord {
	item := workspaceActivityRecord{ID: notice.ID, Action: "permission",
		PageID: strings.TrimPrefix(notice.PageID, pagePermissionNoticePrefix), ActorID: notice.ActorID, Name: notice.Title,
		OccurredAt: notificationInboxTime(notice.OccurredAt)}
	all := append([]storedWorkspacePermissionNotice{notice}, members...)
	slices.SortStableFunc(all, func(a, b storedWorkspacePermissionNotice) int {
		return cmp.Or(cmp.Compare(a.CreatedAt, b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	for _, each := range all {
		item.Permissions = append(item.Permissions, workspacePermissionChange(each, readerID))
	}
	first := item.Permissions[0]
	item.EntityType, item.EntityID, item.FromRole, item.ToRole = first.EntityType, first.EntityID, first.FromRole, first.ToRole
	// A line restoring what the pages above give says so (page_share_unlink_notices.go).
	if strings.HasSuffix(notice.ActivityKey, ":"+pagePermissionRestored) {
		item.Kind = pagePermissionRestored
	}
	return item
}

// workspacePermissionChange is one share's change on a permission line.
func workspacePermissionChange(notice storedWorkspacePermissionNotice, readerID string) workspaceActivityPermission {
	parts := strings.Split(strings.TrimPrefix(notice.ActivityKey, pagePermissionNoticePrefix), ":")
	change := workspaceActivityPermission{EntityType: "share", EntityID: readerID}
	if len(parts) >= 4 {
		change.FromRole, change.ToRole = parts[2], parts[3]
	}
	if len(parts) >= 5 && parts[4] != "" {
		change.EntityID = parts[4]
	}
	// A page's access for everyone in its workspace is no person's
	// (tellGeneralAccessChange): the app names them by the workspace.
	if change.EntityID == pagePermissionWorkspacePerson {
		change.EntityType, change.EntityID = "generalAccess", ""
	}
	return change
}

// workspacePermissionPlace is where a permission line's page is, after its
// time as a page's own line says it (workspace_activity_location.go):
// Notion's "본인 〈page〉의 권한을 업데이트했습니다. · 지금 · 〈위 page〉" (seen
// 2026-11-16 in a page's updates, two tabs at two times, and in the
// workspace's updates, "· 〈test page〉"). A page gone for good says none.
func workspacePermissionPlace(ctx context.Context, app core.App, readerID, pageID string, places workspaceActivityPlaces) ([]workspaceActivityPlace, error) {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return places.read(ctx, app, readerID, "page", page, page)
}

// workspaceActivityCandidate is one of a feed turn's lines before it is
// shown: a group of the activity table or a permission line.
type workspaceActivityCandidate struct {
	at     int64
	seq    int64
	id     string
	row    *storedWorkspaceActivity
	notice *storedWorkspacePermissionNotice
}

// workspaceActivityOrder orders the feed's lines, the newest first: by
// time, then a time's permission lines over its groups, then each as it was
// first written (its rowid), the last first — as Notion's updates list lines
// of one moment in the order they were written (seen 2026-11-17 by workers
// w1596 and w1606 in two tabs each: a new database's "보기", "속성",
// "컬렉션" lines from the top, written collection first). A group joined
// again keeps the place of its first writing. The feed's and the permission
// notices' queries keep the same order and cursor.
func workspaceActivityOrder(a, b workspaceActivityCandidate) int {
	if a.at != b.at {
		return cmp.Compare(b.at, a.at)
	}
	if (a.notice != nil) != (b.notice != nil) {
		if a.notice != nil {
			return -1
		}
		return 1
	}
	return cmp.Compare(b.seq, a.seq)
}

// mergeWorkspaceActivityCandidates orders a turn's groups and permission
// lines as one list. Where either source stopped short (rowsMore,
// noticesMore), nothing of the other older than its last is taken this turn:
// the next turn reads on from the cursor and finds it.
func mergeWorkspaceActivityCandidates(rows []storedWorkspaceActivity, rowsMore bool, notices []storedWorkspacePermissionNotice, noticesMore bool) []workspaceActivityCandidate {
	merged := make([]workspaceActivityCandidate, 0, len(rows)+len(notices))
	for index := range rows {
		merged = append(merged, workspaceActivityCandidate{at: rows[index].LastAt, seq: rows[index].Seq, id: rows[index].ID, row: &rows[index]})
	}
	for index := range notices {
		merged = append(merged, workspaceActivityCandidate{at: notices[index].OccurredAt, seq: notices[index].Seq, id: notices[index].ID, notice: &notices[index]})
	}
	slices.SortFunc(merged, workspaceActivityOrder)
	// Past the last line of a source that stopped short, the other's lines wait.
	var last *workspaceActivityCandidate
	if rowsMore && len(rows) > 0 {
		last = &workspaceActivityCandidate{at: rows[len(rows)-1].LastAt, seq: rows[len(rows)-1].Seq, row: &rows[len(rows)-1]}
	}
	if noticesMore && len(notices) > 0 {
		end := workspaceActivityCandidate{at: notices[len(notices)-1].OccurredAt, seq: notices[len(notices)-1].Seq, notice: &notices[len(notices)-1]}
		if last == nil || workspaceActivityOrder(end, *last) < 0 {
			last = &end
		}
	}
	if last != nil {
		merged = slices.DeleteFunc(merged, func(candidate workspaceActivityCandidate) bool {
			return workspaceActivityOrder(candidate, *last) > 0
		})
	}
	return merged
}
