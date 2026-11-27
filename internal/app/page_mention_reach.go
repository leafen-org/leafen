package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Whether the people an "@" menu offers on a page would hear of being
// mentioned there, as Notion's menu marks someone who can't open the page
// and, once they are picked, asks to invite them (seen 2026-11-07: "페이지에
// 초대하시겠습니까?", its "초대" sharing the page with them to edit). The
// answer is the mention notice's own: someone the page doesn't open to isn't
// told (page_person_mention_notifications.go). Whether the asker may share
// the page decides whether they are asked at all.
//
// The menu's own invite — "'〈typed〉' 초대…", or "초대" with nothing typed —
// asks someone not yet here in; Notion's offers it to the workspace's owner
// alone (seen 2026-11-24 on synthetic pages: its owner had it; a guest with
// edit, full or comment access and a member with edit or full access, where
// members may invite guests, had none). The server says whether the asker
// is offered it (pageMentionInvites); a menu asks with no one named for that
// alone. A person picker with no one left to list offers its "사용자 초대 ›"
// to whoever may share the page, a guest with full access too (Notion's,
// seen 2026-11-24; PeoplePickerEmpty), and asks the same way.

const (
	pageMentionReachPath   = "/pages/{pageId}/mention-reach"
	pageMentionReachMaxIDs = 50
)

type pageMentionReachResponse struct {
	// The people asked of whom the page doesn't open to.
	Unreachable []string `json:"unreachable"`
	// Whether the asker may share the page (full access).
	CanShare bool `json:"canShare"`
	// Whether the menu offers the asker to invite someone not yet here.
	CanInvite bool `json:"canInvite"`
	// The people asked who are guests of the page's workspace, and those who
	// are its restricted members, whom the menu marks "게스트" and "제한됨"
	// beside their names as Notion's "@" menu does (seen 2026-11-07, 2026-11-11).
	Guests     []string `json:"guests"`
	Restricted []string `json:"restricted"`
}

func registerPageMentionReachRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+pageMentionReachPath, readPageMentionReach)
}

func readPageMentionReach(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	ids := event.Request.URL.Query()["userId"]
	if pageID == "" || len(ids) > pageMentionReachMaxIDs {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Name at most 50 people.")
	}
	// Only someone who can mention there asks — a comment's "@" needs comment
	// access, the page's own edit — so a reader alone learns nothing of who
	// else the page opens to.
	authorization, err := authorizeMentionAsker(event.App, user.Id, pageID)
	if err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
		}
		return err
	}
	response := pageMentionReachResponse{Unreachable: []string{}, Guests: []string{}, Restricted: []string{}}
	workspaceID := authorization.Workspace.WorkspaceID
	invitees, err := loadPeopleValueInvitees(event.App, workspaceID, ids)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	asked := []string{}
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		asked = append(asked, id)
		// Someone invited by an address is reached as the account that proved
		// it, or, before one has, by the page's shares with the address.
		target := id
		if invitee, found := invitees[id]; found && invitee.AccountID != "" {
			target = invitee.AccountID
		} else if found {
			if reached, err := pageOpensToAddress(event.App, workspaceID, pageID, invitee.Email); err != nil {
				return err
			} else if !reached {
				response.Unreachable = append(response.Unreachable, id)
			}
			continue
		}
		if _, err := authorizeCommentNotificationTarget(event.App, target, pageID); err != nil {
			if !isPermissionDenied(err) {
				return err
			}
			response.Unreachable = append(response.Unreachable, id)
		}
	}
	if response.CanShare, response.CanInvite, err = pageMentionInvites(event.App, user.Id, pageID); err != nil {
		return err
	}
	if response.Guests, response.Restricted, err = pageMentionPeopleMarks(
		event.App, workspaceID, asked,
	); err != nil {
		return err
	}
	return event.JSON(http.StatusOK, response)
}

// authorizeMentionAsker authorizes someone who may mention on a page with
// comment access: a database row's page by its own rule, as its share window
// and its comments decide it (authorizeRowPageRecord) — the page rule reads
// one for viewing alone, and a row page's "@" and person pickers ask here too.
func authorizeMentionAsker(app core.App, userID, pageID string) (pageAuthorization, error) {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return pageAuthorization{}, permissionDenied()
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	if isDatabaseRowPageRecord(page) {
		return authorizeRowPageRecord(app, userID, page, pageAccessRoleComment)
	}
	return authorizePageAccessRole(app, userID, pageID, pageAccessRoleComment)
}

// pageMentionInvites says whether someone may share a page, by the rule its
// share window changes it by (authorizePublicMutationManager), and whether
// the "@" menu offers them to invite someone not yet here: its workspace's
// owner, who may share it.
func pageMentionInvites(app core.App, actorID, pageID string) (share, invite bool, err error) {
	authorization, err := authorizePublicMutationManager(app, actorID, pageID)
	if errors.Is(err, errPublicMutationNotFound) || isPermissionDenied(err) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, authorization.Workspace.Role == workspaceRoleOwner, nil
}

// pageOpensToAddress says whether the page, or a page its sharing follows
// from above, is shared with the address.
func pageOpensToAddress(app core.App, workspaceID, pageID, email string) (bool, error) {
	chain, err := loadPageAccessAncestorIDs(app, workspaceID, pageID)
	if err != nil || len(chain) == 0 {
		if isPermissionDenied(err) {
			return false, nil
		}
		return false, err
	}
	encoded, err := json.Marshal(chain)
	if err != nil {
		return false, err
	}
	shared := false
	err = app.DB().NewQuery(`
		SELECT EXISTS (SELECT 1 FROM {{leafen_page_permissions}} [[share]]
			WHERE [[share]].[[workspace_id]] = {:workspaceId} AND [[share]].[[principal_type]] = 'email'
				AND [[share]].[[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
				AND lower(trim([[share]].[[principal_id]])) = {:email})
	`).Bind(dbx.Params{"workspaceId": workspaceID, "pages": string(encoded), "email": normalizePageShareEmail(email)}).Row(&shared)
	return shared, err
}

// pageMentionPeopleMarks sorts the people asked into a workspace's guests —
// neither its owner nor its members, as the share window counts them
// (publicMutationPrincipalColumnsSQL), nor an address it invited to become a
// member (people_value_invitee_membership.go) — and its restricted members,
// those invited as such among them.
func pageMentionPeopleMarks(app core.App, workspaceID string, ids []string) (guests, restricted []string, err error) {
	guests, restricted = []string{}, []string{}
	if len(ids) == 0 {
		return guests, restricted, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, nil, err
	}
	rows := []struct {
		UserID     string `db:"user_id"`
		Guest      bool   `db:"guest"`
		Restricted bool   `db:"restricted"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[asked]].[[value]] AS [[user_id]],
			NOT EXISTS (
				SELECT 1 FROM {{leafen_workspaces}} [[mark_workspace]]
				WHERE [[mark_workspace]].[[id]] = {:workspaceId} AND [[mark_workspace]].[[workspace_owner]] = [[asked]].[[value]]
				UNION ALL
				SELECT 1 FROM {{leafen_workspace_memberships}} [[mark_member]]
				WHERE [[mark_member]].[[workspace_id]] = {:workspaceId} AND [[mark_member]].[[user_id]] = [[asked]].[[value]]
			) AND NOT ` + inviteeMembershipInvitationSQL("{:workspaceId}", "[[asked]].[[value]]", false) + ` AS [[guest]],
			(` + workspaceRestrictedMemberSQL("{:workspaceId}", "[[asked]].[[value]]") + `
				OR ` + inviteeMembershipInvitationSQL("{:workspaceId}", "[[asked]].[[value]]", true) + `) AS [[restricted]]
		FROM json_each({:ids}) [[asked]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "ids": string(encoded)}).All(&rows); err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		if row.Guest {
			guests = append(guests, row.UserID)
		} else if row.Restricted {
			restricted = append(restricted, row.UserID)
		}
	}
	return guests, restricted, nil
}
