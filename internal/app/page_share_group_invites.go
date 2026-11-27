package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A group invitation reaches current active members who belonged when the
// invitation was created. A persisted cursor bounds each turn, and stable
// invite IDs make replay safe. Revoked grants cannot send remaining invites.
func settlePageShareGroupInvite(app core.App, effect storedPublicMutationEffect, payload publicMutationEffectPayload) error {
	grant := struct {
		Message string `db:"invite_message"`
	}{}
	params := dbx.Params{"share": payload.InviteShareID, "workspace": effect.WorkspaceID, "page": effect.PageID,
		"group": payload.PrincipalID, "organization": payload.OrganizationID, "created": effect.CreatedAt, "after": effect.Cursor, "limit": pageShareAccessNoticeWindow}
	err := app.DB().NewQuery(`SELECT [[invite_message]] FROM {{leafen_page_permissions}} WHERE [[id]]={:share}
  AND [[workspace_id]]={:workspace} AND [[page_id]]={:page} AND [[principal_type]]='group' AND [[principal_id]]={:group}`).Bind(params).One(&grant)
	if errors.Is(err, sql.ErrNoRows) {
		return completePublicMutationEffect(app, effect.ID)
	}
	if err != nil {
		return err
	}
	members := []string{}
	if err := app.DB().NewQuery(`SELECT [[member]].[[user_id]] FROM {{leafen_workspace_permission_group_members}} [[member]]
  JOIN {{leafen_workspace_permission_groups}} [[group]] ON [[group]].[[id]]=[[member]].[[group_id]] AND [[group]].[[workspace_id]]={:workspace} AND [[group]].[[deleted_at]] IS NULL
  JOIN {{leafen_organization_memberships}} [[organization]] ON [[organization]].[[user]]=[[member]].[[user_id]] AND [[organization]].[[organization]]={:organization} AND [[organization]].[[status]]='active'
  WHERE [[member]].[[group_id]]={:group} AND [[member]].[[created_at]]<={:created} AND [[member]].[[user_id]]>{:after}
  ORDER BY [[member]].[[user_id]] LIMIT {:limit}`).Bind(params).Column(&members); err != nil {
		return err
	}
	for _, userID := range members {
		if _, err := pageAccessDespiteHold(authorizePageAccessRole(app, userID, effect.PageID, pageAccessRoleView)); isPermissionDenied(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := notifyPageShareInvite(app, effect.WorkspaceID, effect.PageID, payload.ActorID, userID, payload.InviteShareID, time.UnixMilli(effect.CreatedAt), grant.Message); err != nil {
			return err
		}
	}
	if len(members) < pageShareAccessNoticeWindow {
		return completePublicMutationEffect(app, effect.ID)
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_public_mutation_effects}} SET [[cursor]]={:cursor},[[updated_at]]={:now} WHERE [[id]]={:id} AND [[status]]='pending'`).Bind(dbx.Params{"cursor": members[len(members)-1], "now": time.Now().UTC().UnixMilli(), "id": effect.ID}).Execute()
	return err
}
