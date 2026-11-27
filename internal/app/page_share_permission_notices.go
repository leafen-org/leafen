package app

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Someone whose share of a page changes hears of it among their workspace's
// updates, as Notion's: "〈누가〉 〈page〉의 권한을 업데이트했습니다." with their
// name and the role under it (seen 2026-11-16 in the second account of the
// free test workspace, Korean, and in an audit's English tab, "updated
// permission for"): a share given a member of the page's workspace, its role
// on the added band (twice); a role changed either way, "〈old〉 → 〈new〉"
// (a raise and a lowering); a share taken away, name and role struck through
// (twice). One person's changes to someone's share of a page made close
// together come as one line, from the role before the first to the last
// (pagePermissionNoticeMerge). They see it though the page is closed to them
// now, its title the one it had then; whoever changed it has the same line
// in their own updates (upsertPagePermissionNotice). The notice travels as
// Leafen's own system notice with "page-permission:<page>" where a page would be named
// and the roles at its key's end,
// "page-permission:<page>:<time>:<old role>:<new role>" (no old role when
// the share was given, no new one when it went), its person's id after them
// on the line of whoever changed it; no email tells it. A share
// given anyone is also an invitation in their inbox, "〈누가〉 다음에 초대됨
// 〈page〉" with its email (page_share_invite_notifications.go): the second
// account, a member of the test workspace, had one for each page shared
// with it there (seen 2026-11-16 22:25 and 22:27 in two tabs, four pages
// over a day), as guests do. Shares to a group or to the workspace, and teamspace
// and workspace roles, tell no one this way: Notion's told the second
// account nothing when its teamspace role and its workspace role were raised
// and lowered (seen 2026-11-16, an hour on); a page's access for everyone in
// the workspace changing, and a group's share, were not seen telling a full
// member. Whoever changes a page's access for everyone in its workspace has
// its line in their own updates (tellGeneralAccessChange).

const pagePermissionNoticePrefix = "page-permission:"

// Such a notice is a line of the person's workspace updates, not of their
// inbox, as Notion's: a share's role changed came up in the second
// account's '모든 워크스페이스 업데이트' at once, with no unread mark, and in
// none of '모든 알림', '읽지 않음' or the inbox's count, half an hour on
// (seen 2026-11-16 20:28 and again 22:25–22:27 in another tab, in the free
// test workspace); its earlier changes and takings away were there too, and
// the owner who changed it saw "본인 〈page〉의 권한을 업데이트했습니다." in
// their own, as Leafen's lists it. notificationInInboxSQL keeps either out
// of every inbox tab, its counts and an agent's inbox;
// workspace_activity_permission_notices.go lists them among the updates.
func notificationInInboxSQL(notification string) string {
	return notification + "[[page_id]] NOT LIKE '" + pagePermissionNoticePrefix + "%'"
}

// pagePermissionNoticeShown: such a notice is the person's own, whatever
// they see of the page now (systemNoticeSubjectVisible).
func pagePermissionNoticeShown(pageID string) bool {
	return strings.HasPrefix(pageID, pagePermissionNoticePrefix)
}

// pageShareRoleRank orders a share's roles, the widest last; 0 for none.
func pageShareRoleRank(role string) int {
	switch role {
	case "view":
		return 1
	case "comment":
		return 2
	case "edit":
		return 3
	case "full_access":
		return 4
	}
	return 0
}

// pagePermissionNotice is a page's permission line, which no activity email
// tells: a share given emails its invitation.
func pagePermissionNotice(activityKey string) bool {
	return strings.HasPrefix(activityKey, pagePermissionNoticePrefix)
}

// updateSharedPermissionTelling changes a share's role and tells its person
// when the role is another than the one it had (before, read just ahead).
func updateSharedPermissionTelling(
	app core.App, authorization pageAuthorization, state storedPublicMutationState, input publicMutationInput,
	before *storedPublicMutationPermission, now time.Time,
) error {
	if err := updatePublicMutationPermission(app, state, input, now); err != nil {
		return err
	}
	if before == nil || pageShareRoleRank(input.Role) == pageShareRoleRank(before.Role) {
		return nil
	}
	return notifyPageSharePermissionChange(app, authorization, *before, input.Role, now)
}

// tellShareGivenToMember writes the permission line of a share given a
// member of the page's workspace, by the share's id, beside its invitation;
// anyone else has the invitation alone, and whoever shared it the line in
// their own updates, as Notion's names a guest given a share there (seen
// 2026-11-16 and 2026-11-17 in pages' updates, the second account a guest of
// the workspace: "june lee / 전체 허용" on the added band). A share with a
// message has none (Notion's member share with one not seen).
func tellShareGivenToMember(app core.App, workspaceID string, page *core.Record, actorID, userID, shareID string, now time.Time, message string) error {
	if strings.TrimSpace(message) != "" {
		return nil
	}
	member := true
	if _, err := authorizeWorkspace(app, userID, workspaceID, permissionWorkspaceRead); isPermissionDenied(err) {
		member = false
	} else if err != nil {
		return err
	}
	var role string
	if err := app.DB().NewQuery(`SELECT [[role]] FROM {{leafen_page_permissions}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": shareID}).Row(&role); err != nil {
		return err
	}
	if member {
		return upsertPagePermissionNotice(app, workspaceID, page, actorID, userID, shareID, "", role, now)
	}
	if actorID == "" || actorID == userID {
		return nil
	}
	return upsertPagePermissionLine(app, workspaceID, page, actorID, actorID, userID, shareID, "", role, now)
}

// removeSharedPermissionTelling takes a share away and tells its person.
func removeSharedPermissionTelling(
	app core.App, authorization pageAuthorization, state storedPublicMutationState, input publicMutationInput, now time.Time,
) error {
	before, err := loadPublicMutationPermission(app, state, input.PermissionID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := removePageSharePermission(app, authorization, state, input); err != nil || before.ID == "" {
		return err
	}
	return notifyPageSharePermissionChange(app, authorization, before, "", now)
}

// notifyPageSharePermissionChange tells the account a share names that it now
// gives role ("" taken away) instead of the share's own; a share with the
// whole workspace, whoever changed it (tellGeneralAccessChange).
func notifyPageSharePermissionChange(
	app core.App, authorization pageAuthorization, share storedPublicMutationPermission, role string, now time.Time,
) error {
	userID := ""
	switch share.PrincipalType {
	case publicMutationPrincipalWorkspace:
		return tellGeneralAccessChange(app, authorization, share.Role, role, now)
	case publicMutationPrincipalUser:
		userID = share.PrincipalID
	case publicMutationPrincipalEmail:
		account, err := pageShareEmailAccount(app, share.PrincipalID)
		if err != nil {
			return err
		}
		userID = account
	}
	actorID := authorization.Workspace.PrincipalID
	if userID == "" || userID == actorID {
		return nil
	}
	stamp := strconv.FormatInt(now.UnixMilli(), 10)
	return upsertPagePermissionNotice(app, authorization.Workspace.WorkspaceID, authorization.Page, actorID, userID, stamp,
		share.Role, role, now)
}

// upsertPagePermissionNotice puts a page's permission line in the updates of
// the share's person and of whoever changed it, once for its key (a time, or
// a share given), or joins each to the line of the same person's change just
// before (mergePagePermissionNotice). Whoever changed it reads it as theirs —
// "본인 〈page〉의 권한을 업데이트했습니다." with the share's person and the
// roles under it, joined and gone as the person's own line is (seen
// 2026-11-16 in the first account's updates of the free test workspace at
// 22:2x and again at 23:19–23:20 in two tabs: a change, a second one 40 s
// later joined to it, and the role put back within the five minutes leaving
// no line; a share given a member, its role alone). A share taken away is
// written for them as for its person, Notion's line of it not seen yet. Their
// own line tells them nothing else: no email and no push.
func upsertPagePermissionNotice(app core.App, workspaceID string, page *core.Record, actorID, userID, key, from, to string, now time.Time) error {
	if err := upsertPagePermissionLine(app, workspaceID, page, actorID, userID, "", key, from, to, now); err != nil {
		return err
	}
	if actorID == "" || actorID == userID {
		return nil
	}
	return upsertPagePermissionLine(app, workspaceID, page, actorID, actorID, userID, key, from, to, now)
}

// upsertPagePermissionLine writes one holder's permission line: the share's
// own person's (person "") or, naming its person, the one who changed it,
// whose line of another share of the page changed just before takes it
// (page_share_permission_groups.go).
func upsertPagePermissionLine(app core.App, workspaceID string, page *core.Record, actorID, holderID, person, key, from, to string, now time.Time) error {
	restricted := to == pagePermissionRestricted
	if !restricted {
		if merged, err := mergePagePermissionNotice(app, workspaceID, page.Id, actorID, holderID, person, to, now); err != nil || merged {
			return err
		}
	}
	target, err := pageNotificationTarget(app, page.Id)
	if err != nil {
		return err
	}
	lead := ""
	if person != "" && holderID == actorID && !restricted {
		if lead, err = pagePermissionGroupLead(app, workspaceID, page.Id, actorID, now); err != nil {
			return err
		}
	}
	identity := []string{page.Id, holderID, key}
	if person != "" {
		identity = append(identity, person)
	}
	at := now.UnixMilli()
	_, err = upsertProductNotifications(context.Background(), app, []productNotificationProjection{{
		ID:          appNoticeID(append([]string{"page-permission"}, identity...)...),
		EventID:     appNoticeID(append([]string{"page-permission-event"}, identity...)...),
		WorkspaceID: workspaceID, UserID: holderID,
		ActivityKey: pagePermissionNoticeKey(page.Id, at, from, to, person),
		Kind:        "system", PageID: pagePermissionNoticePrefix + page.Id, BlockID: lead, ActorID: actorID,
		Title: page.GetString("title"), Target: target, OccurredAt: at, Personal: person == "",
	}}, at)
	if err != nil || lead == "" {
		return err
	}
	return touchPagePermissionGroup(app, lead, lead, at)
}

// pagePermissionNoticeKey is a permission line's key: its page, time and
// roles, and for the line of whoever changed it the share's person.
func pagePermissionNoticeKey(pageID string, at int64, from, to, person string) string {
	key := pagePermissionNoticePrefix + pageID + ":" + strconv.FormatInt(at, 10) + ":" + from + ":" + to
	if person != "" {
		key += ":" + person
	}
	return key
}

// pagePermissionNoticeMerge is how soon after one person's change of
// someone's share of a page their next change of it joins the same line, as
// Notion's five minutes (seen 2026-11-16 in the second account's updates of
// the free test workspace, a member's share of one page changed by its
// owner): changes 206 s and 276 s apart came as one line, from the role
// before the first to the last, its time the last's; 338 s, 362 s, 373 s
// and twelve minutes apart, as lines of their own. A role changed and put
// back within it (five seconds later) left no line.
const pagePermissionNoticeMerge = 5 * time.Minute

// mergePagePermissionNotice joins a change of a share of a page, to role
// `to` ("" taken away), to holderID's line of actorID's last change of it
// made within pagePermissionNoticeMerge — the share's person's own line, or
// with person the line of whoever changed it: the line, brought up unread,
// then tells one change, from the role before that first change (none for a
// share given) to `to`; where the two are the same nothing has changed and
// the line goes. False when there is no such line.
func mergePagePermissionNotice(app core.App, workspaceID, pageID, actorID, holderID, person, to string, now time.Time) (bool, error) {
	var last struct {
		ID          string `db:"id"`
		ActivityKey string `db:"activity_key"`
		Lead        string `db:"block_id"`
	}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[activity_key]], [[block_id]] FROM {{leafen_notifications}}
		WHERE [[workspace_id]] = {:workspace} AND [[user_id]] = {:user} AND [[occurred_at]] >= {:since}
			AND [[page_id]] = {:page} AND [[actor_id]] = {:actor} AND [[kind]] = 'system' AND [[archived_at]] = 0
			AND ({:person} = '' OR substr([[activity_key]], -length({:person}) - 1) = ':' || {:person})
		ORDER BY [[occurred_at]] DESC LIMIT 1`).Bind(dbx.Params{
		"workspace": workspaceID, "user": holderID, "since": now.Add(-pagePermissionNoticeMerge).UnixMilli(),
		"page": pagePermissionNoticePrefix + pageID, "actor": actorID, "person": person,
	}).One(&last)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	parts := strings.Split(strings.TrimPrefix(last.ActivityKey, pagePermissionNoticePrefix), ":")
	if len(parts) < 4 {
		return false, nil
	}
	from := parts[2]
	if from == to {
		if err := leavePagePermissionGroup(app, last.ID); err != nil {
			return false, err
		}
		_, err = app.DB().NewQuery(`DELETE FROM {{leafen_notifications}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": last.ID}).Execute()
	} else {
		at := now.UnixMilli()
		_, err = app.DB().NewQuery(`
			UPDATE {{leafen_notifications}} SET [[activity_key]] = {:key}, [[occurred_at]] = {:at}, [[read_at]] = 0, [[updated_at]] = {:at}
			WHERE [[id]] = {:id}`).Bind(dbx.Params{
			"id": last.ID, "at": at, "key": pagePermissionNoticeKey(pageID, at, from, to, person),
		}).Execute()
		if err == nil && person != "" {
			// Its line's last change is this one (page_share_permission_groups.go).
			err = touchPagePermissionGroup(app, last.ID, last.Lead, at)
		}
	}
	if err != nil {
		return false, err
	}
	noteWorkspaceNotices(map[[2]string]struct{}{{workspaceID, holderID}: {}})
	return true, nil
}
