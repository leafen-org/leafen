package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// One person's changes to several shares of a page made close together are
// one line in their own updates, as Notion's: "본인 〈page〉의 권한을
// 업데이트했습니다." with each share's person and roles under it, in the order
// they were first changed (seen 2026-11-16 in a page's updates, a person's
// share given and, two minutes on, everyone in the workspace's; and
// 2026-11-17 in another tab, everyone in the workspace on the added band over
// a guest's share given a minute later). A change joins the line while the
// line's last change is within pagePermissionNoticeMerge. Each share keeps a
// notice of its own, so its own later changes join it and a role put back
// takes it away as before (mergePagePermissionNotice); the line's first
// notice leads it, the others name it in their block_id — no block is ever
// one of these notices' — and the feed lists the lead with the rest under it
// (readWorkspacePermissionGroupMembers).

// pagePermissionGroupLead is the lead notice of the line in whoever changed
// a page's shares' own updates that a change made now joins: its last change
// within pagePermissionNoticeMerge. "" for none.
func pagePermissionGroupLead(app core.App, workspaceID, pageID, actorID string, now time.Time) (string, error) {
	var lead string
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_notifications}}
		WHERE [[workspace_id]]={:workspace} AND [[user_id]]={:actor} AND [[actor_id]]={:actor}
			AND [[page_id]]={:page} AND [[kind]]='system' AND [[archived_at]]=0 AND [[block_id]]=''
			AND [[occurred_at]]>={:since} AND [[activity_key]] NOT LIKE '%:` + pagePermissionRestricted + `:%'
			AND [[activity_key]] NOT LIKE '%:` + pagePermissionRestored + `'
		ORDER BY [[occurred_at]] DESC LIMIT 1`).Bind(dbx.Params{
		"workspace": workspaceID, "actor": actorID, "page": pagePermissionNoticePrefix + pageID,
		"since": now.Add(-pagePermissionNoticeMerge).UnixMilli(),
	}).Row(&lead)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return lead, err
}

// touchPagePermissionGroup brings the line holding a notice up to time at,
// unread: every notice of it, so the line's last change is each one's.
func touchPagePermissionGroup(app core.App, noticeID, lead string, at int64) error {
	if lead == "" {
		lead = noticeID
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notifications}} SET [[occurred_at]]={:at}, [[read_at]]=0, [[updated_at]]={:at}
		WHERE [[id]]={:lead} OR ([[block_id]]={:lead} AND [[page_id]] LIKE '` + pagePermissionNoticePrefix + `%')`).
		Bind(dbx.Params{"lead": lead, "at": at}).Execute()
	return err
}

// leavePagePermissionGroup takes a notice going away out of its line: one
// that leads others hands the line to the first of them.
func leavePagePermissionGroup(app core.App, noticeID string) error {
	members := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_notifications}}
		WHERE [[block_id]]={:lead} AND [[page_id]] LIKE '` + pagePermissionNoticePrefix + `%'
		ORDER BY [[created_at]], [[id]]`).Bind(dbx.Params{"lead": noticeID}).Column(&members); err != nil || len(members) == 0 {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notifications}} SET [[block_id]]=CASE WHEN [[id]]={:next} THEN '' ELSE {:next} END
		WHERE [[block_id]]={:lead} AND [[page_id]] LIKE '` + pagePermissionNoticePrefix + `%'`).
		Bind(dbx.Params{"lead": noticeID, "next": members[0]}).Execute()
	return err
}

// readWorkspacePermissionGroupMembers reads the notices under the lines a
// feed turn shows, by their lead, each line's in the order first written.
func readWorkspacePermissionGroupMembers(app core.App, leads []storedWorkspacePermissionNotice) (map[string][]storedWorkspacePermissionNotice, error) {
	members := map[string][]storedWorkspacePermissionNotice{}
	if len(leads) == 0 {
		return members, nil
	}
	ids := make([]string, 0, len(leads))
	for _, lead := range leads {
		ids = append(ids, lead.ID)
	}
	params, placeholders := pageDuplicationIDParams("lead", ids)
	rows := []storedWorkspacePermissionNotice{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[activity_key]],[[page_id]],[[actor_id]],[[title]],[[occurred_at]],[[created_at]],[[block_id]]
		FROM {{leafen_notifications}}
		WHERE [[block_id]] IN (` + joinSQL(placeholders) + `) AND [[page_id]] LIKE '` + pagePermissionNoticePrefix + `%'
		ORDER BY [[created_at]], [[id]]`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		members[row.Lead] = append(members[row.Lead], row)
	}
	return members, nil
}
