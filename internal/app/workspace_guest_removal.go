package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's owner takes guests out of it, as Notion's 설정 → 사람 →
// 게스트 "워크스페이스에서 제거" does (seen 2026-11-09): every page of the
// workspace shared with them stops being shared — by their account, by an
// address they proved, or by the address itself, pages in the trash too, so
// a restored page doesn't bring them back — and the visits of theirs waiting
// to be announced go with them. They stay in the organization as a guest and
// open a page again only once it is shared with them afresh. The shares go
// at once, as a share removed from a page's share window does: the access
// revision the deletes raise closes what they have open.

var errWorkspaceGuestRemoveInvalid = errors.New(errorCodeWorkspaceGuestRemoveInvalid)

// pageShareNamesAccountSQL says a share (an unaliased row of
// leafen_page_permissions) names an account given by a parameter: by its id,
// or by an address it verified and proved (account_email_identity.go), as a
// page shared with such an address opens to it.
func pageShareNamesAccountSQL(accountParam string) string {
	account := `{:` + accountParam + `}`
	return `(([[principal_type]] = 'user' AND [[principal_id]] = ` + account + `)
		OR ([[principal_type]] = 'email' AND lower(trim([[principal_id]])) IN (
			SELECT lower(trim([[email]].[[email]])) FROM {{leafen_account_emails}} [[email]]
			WHERE [[email]].[[user_id]] = ` + account + ` AND ` + provenAccountEmailSQL("[[email]].") + `)))`
}

func removeWorkspaceGuests(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := workspaceGuestRemoveInput{}
	if err := event.BindBody(&input); err != nil || workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceGuestRemoveInvalid, "Check the guests to remove.")
	}
	keys, valid := normalizedWorkspaceGuestKeys(input.Guests)
	if !valid {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceGuestRemoveInvalid, "Check the guests to remove.")
	}
	response := workspaceGuestRemoveResponse{WorkspaceID: workspaceID}
	change := func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceOwner)
		if err != nil {
			return err
		}
		response.Removed, err = withdrawWorkspaceGuests(txApp, authorization, user.Id, keys)
		return err
	}
	// A key's call asks its person only for a removal that would be made
	// (user_key_acting_ask.go).
	err := rehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := askBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if isPermissionDenied(err) {
		return workspaceOwnersOnly(event, user.Id, workspaceID, "Only the workspace's owners remove its guests.", workspaceNotFound(event))
	}
	if err != nil {
		if storageError, ok := persistentWriteStorageAPIError(event, err, persistentWriteWorkspaceMemberRevoke, "guest-removal"); ok {
			return storageError
		}
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceGuestRemoveFailed,
			"Couldn't remove the guests.", "workspace_guest_removal", "withdraw", errorCodeWorkspaceGuestRemoveFailed, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

// normalizedWorkspaceGuestKeys are the guests asked for, each once, as the
// directory names them; there is at least one and at most a directory window.
func normalizedWorkspaceGuestKeys(raw []string) ([]string, bool) {
	if len(raw) == 0 || len(raw) > maxWorkspaceGuestRemovals {
		return nil, false
	}
	seen := map[string]bool{}
	keys := make([]string, 0, len(raw))
	for _, value := range raw {
		key := normalizedWorkspaceGuestKey(value)
		if key == "" || len(key) > 512 {
			return nil, false
		}
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys, true
}

// withdrawWorkspaceGuests takes the workspace's page shares from those of the
// keys who are its guests now, as the directory finds them
// (workspaceGuestSharesSQL), and says how many there were: a key that names
// a member, the owner or no one takes nothing.
func withdrawWorkspaceGuests(txApp core.App, authorization workspaceAuthorization, actorID string, keys []string) (int, error) {
	encoded, err := json.Marshal(keys)
	if err != nil {
		return 0, err
	}
	guests := []struct {
		Key       string  `db:"guest_key"`
		AccountID *string `db:"account_id"`
	}{}
	if err := txApp.DB().NewQuery(workspaceGuestSharesSQL + `
		SELECT [[guest_key]], MAX([[account_id]]) AS [[account_id]] FROM [[guest_share]]
		WHERE [[guest_key]] IN (SELECT [[value]] FROM json_each({:keys}))
		GROUP BY [[guest_key]] ORDER BY [[guest_key]]
	`).Bind(dbx.Params{"workspaceId": authorization.WorkspaceID, "keys": string(encoded)}).All(&guests); err != nil {
		return 0, err
	}
	now := time.Now().UTC().UnixMilli()
	for _, guest := range guests {
		params := dbx.Params{"workspaceId": authorization.WorkspaceID}
		statements := []string{}
		targetType, targetID := "user", ""
		if guest.AccountID != nil {
			params["account"], targetID = *guest.AccountID, *guest.AccountID
			statements = append(statements,
				`DELETE FROM {{leafen_page_permissions}} WHERE [[workspace_id]] = {:workspaceId} AND `+pageShareNamesAccountSQL("account"),
				`DELETE FROM {{leafen_page_share_access_pending}} WHERE [[workspace_id]] = {:workspaceId} AND [[principal_id]] = {:account}`)
		} else {
			address, isEmail := strings.CutPrefix(guest.Key, "email:")
			if !isEmail || address == "" {
				return 0, errWorkspaceGuestRemoveInvalid
			}
			params["address"], targetType, targetID = address, "email", address
			statements = append(statements, `DELETE FROM {{leafen_page_permissions}} WHERE [[workspace_id]] = {:workspaceId}
				AND [[principal_type]] = 'email' AND lower(trim([[principal_id]])) = {:address}`)
		}
		for _, statement := range statements {
			if _, err := txApp.DB().NewQuery(statement).Bind(params).Execute(); err != nil {
				return 0, err
			}
		}
		if err := recordOrganizationAuditEvent(txApp, organizationAuditEvent{
			OrganizationID: authorization.Organization.OrganizationID, WorkspaceID: authorization.WorkspaceID,
			ActorID: actorID, Action: "workspace_guest.remove", TargetType: targetType, TargetID: targetID, OccurredAt: now,
		}); err != nil {
			return 0, err
		}
	}
	return len(guests), nil
}
