package app

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// pagePermissionRestricted is the role a permission line names when a page
// took away access its pages above gave: Notion's line of whoever removed
// someone a sub-page inherited, cutting its sharing from its parent's,
// "본인이 〈page〉에 대한 사용 권한을 제한함" with the person under it and
// "〈role〉 → 사용 권한 없음" in the default color (seen 2026-11-16 and
// 2026-11-17 in a page's updates, in two tabs; its time and place as a
// page's line has them, "지금 · 〈top〉 / 〈parent〉"). Such a line is a line of
// its own, joining no other.
const pagePermissionRestricted = "none"

// pagePermissionRestored ends the key of each share's notice on the line of
// whoever linked a page's sharing to its parent's again, as Notion's: "본인이
// 〈page〉의 상속된 사용 권한을 복원함" with each person whose access the pages
// above widen under it, "〈role, 사용 권한 없음 for none〉 → 〈role above〉" in
// the default color (seen 2026-11-17 in a page's updates, in two tabs at two
// times — one line in the free test workspace, one made then by pressing
// '설정 복원' › '다시 연결', whose dialog listed the same change). The line
// joins no other, its shares under its first (page_share_permission_groups.go);
// the feed tells it by Kind (workspacePermissionActivity).
const pagePermissionRestored = "restored"

// pagePermissionLinePerson is whom a permission line names for a share's
// principal: a person, an address's account, or everyone in the workspace
// (tellGeneralAccessChange names them so); "" for a group, which names no one
// here.
func pagePermissionLinePerson(app core.App, principal sharePrincipalKey) (string, error) {
	switch principal.Type {
	case publicMutationPrincipalUser:
		return principal.ID, nil
	case publicMutationPrincipalEmail:
		return pageShareEmailAccount(app, principal.ID)
	case publicMutationPrincipalWorkspace:
		return pagePermissionWorkspacePerson, nil
	}
	return "", nil
}

// tellInheritedAccessRemoved writes, in the updates of whoever unlinked a
// page by removing someone it inherited (unlinkPageShareInheritance), the
// line restricting their access (seen once for everyone in the workspace,
// "읽기 허용 → 사용 권한 없음"). The person's own updates were not seen
// telling them, so theirs has none.
func tellInheritedAccessRemoved(
	app core.App, authorization pageAuthorization, principal sharePrincipalKey, role pageAccessRole, now time.Time,
) error {
	actorID := authorization.Workspace.PrincipalID
	person, err := pagePermissionLinePerson(app, principal)
	if err != nil {
		return err
	}
	from := publicMutationRoleFor(role)
	if actorID == "" || person == "" || person == actorID || from == "" {
		return nil
	}
	return upsertPagePermissionLine(app, authorization.Workspace.WorkspaceID, authorization.Page, actorID, actorID, person,
		strconv.FormatInt(now.UnixMilli(), 10), from, pagePermissionRestricted, now)
}

// tellInheritedAccessRestored writes, in the updates of whoever linked a
// page's sharing to its parent's again (relinkPageShareInheritance), the line
// restoring the access the pages above give: each share whose role there is
// wider than the page's own (own, read before the relink), from that role to
// theirs; shares the page keeps or that change nothing are left out, as
// Notion's relink dialog and line list only june lee, whose share had gone.
// People come before everyone in the workspace (their order between them not
// seen). As with the restricting line, the people's own updates have none.
func tellInheritedAccessRestored(
	app core.App, authorization pageAuthorization, inherited map[sharePrincipalKey]pageAccessRole,
	own []storedPageShareRole, now time.Time,
) error {
	actorID := authorization.Workspace.PrincipalID
	if actorID == "" {
		return nil
	}
	kept := map[sharePrincipalKey]pageAccessRole{}
	for _, row := range own {
		key := sharePrincipalKey{row.PrincipalType, row.PrincipalID}
		kept[key] = max(kept[key], pageAccessRoleFromTeamspaceRole(row.Role))
	}
	principals := make([]sharePrincipalKey, 0, len(inherited))
	for principal, role := range inherited {
		if role > kept[principal] {
			principals = append(principals, principal)
		}
	}
	slices.SortFunc(principals, func(a, b sharePrincipalKey) int {
		everyone := func(key sharePrincipalKey) bool { return key.Type == publicMutationPrincipalWorkspace }
		if everyone(a) != everyone(b) {
			if everyone(a) {
				return 1
			}
			return -1
		}
		return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.ID, b.ID))
	})
	lead, stamp := "", pagePermissionRestored+":"+strconv.FormatInt(now.UnixMilli(), 10)
	for _, principal := range principals {
		person, err := pagePermissionLinePerson(app, principal)
		if err != nil {
			return err
		}
		if person == "" || person == actorID {
			continue
		}
		from := publicMutationRoleFor(kept[principal])
		if from == "" {
			from = pagePermissionRestricted
		}
		id, err := writePagePermissionRestoredNotice(app, authorization, actorID, person, stamp,
			from, publicMutationRoleFor(inherited[principal]), lead, now)
		if err != nil {
			return err
		}
		if lead == "" {
			lead = id
		}
	}
	return nil
}

// writePagePermissionRestoredNotice writes one share's notice on whoever
// relinked a page's restoring line, under lead ("" for the line's first),
// and returns its id.
func writePagePermissionRestoredNotice(
	app core.App, authorization pageAuthorization, actorID, person, stamp, from, to, lead string, now time.Time,
) (string, error) {
	page := authorization.Page
	target, err := pageNotificationTarget(app, page.Id)
	if err != nil {
		return "", err
	}
	identity := []string{page.Id, actorID, stamp, person}
	id := appNoticeID(append([]string{"page-permission"}, identity...)...)
	at := now.UnixMilli()
	_, err = upsertProductNotifications(context.Background(), app, []productNotificationProjection{{
		ID:          id,
		EventID:     appNoticeID(append([]string{"page-permission-event"}, identity...)...),
		WorkspaceID: authorization.Workspace.WorkspaceID, UserID: actorID,
		ActivityKey: pagePermissionNoticeKey(page.Id, at, from, to, person) + ":" + pagePermissionRestored,
		Kind:        "system", PageID: pagePermissionNoticePrefix + page.Id, BlockID: lead, ActorID: actorID,
		Title: page.GetString("title"), Target: target, OccurredAt: at,
	}}, at)
	return id, err
}
