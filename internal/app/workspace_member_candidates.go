package app

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The people of a workspace's organization whom its owner can add back by
// name: those with no address they proved, whom adding by email can't
// reach — someone a name invitation brought in and who was removed. Notion
// has no account without an email; Leafen's name invitations make them.
// Only the owner, who adds members, reads it, and only while the server lets
// existing accounts be added; an app's account, a member already here and
// anyone outside the organization aren't offered. Two of the same name are
// told apart by when they joined.

type storedWorkspaceMemberCandidate struct {
	UserID      string  `db:"id"`
	DisplayName string  `db:"display_name"`
	Avatar      *string `db:"avatar"`
	Joined      string  `db:"joined"`
}

func readWorkspaceMemberCandidates(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	query := event.Request.URL.Query()
	name := strings.TrimSpace(query.Get("q"))
	if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength || name == "" ||
		utf8.RuneCountInString(name) > maxWorkspaceMemberCandidateQueryRunes || len(query) != 1 {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMemberListInvalid, "Check the member query.")
	}
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner)
	if isPermissionDenied(err) {
		return workspaceOwnersOnly(event, user.Id, workspaceID, "Only the workspace's owners look for people to add.", workspaceNotFound(event))
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceMemberListFailed, "Couldn't read the people.", err)
	}
	response := workspaceMemberCandidatesResponse{WorkspaceID: workspaceID, Candidates: []workspaceMemberCandidate{}}
	policy, err := loadWorkspaceMemberAddPolicy(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceMemberListFailed, "Couldn't read the people.", err)
	}
	if policy != administratorMemberAddPolicyEnabled {
		return event.JSON(http.StatusOK, response)
	}
	// A deactivated or suspended account can't be added; it isn't offered.
	// It is left out in the read, by the state that lets an account sign in
	// (administratorAccountCanAuthenticate): twice as many read and then
	// those left out, the names past them weren't offered while such
	// accounts filled the read (2026-11-23).
	pattern := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(sqliteLower(name)) + "%"
	stored := []storedWorkspaceMemberCandidate{}
	err = event.App.DB().NewQuery(`
		SELECT [[user]].[[id]], trim([[user]].[[name]]) AS [[display_name]],
			NULLIF([[user]].[[profile_avatar]],'') AS [[avatar]], [[user]].[[created]] AS [[joined]]
		FROM {{leafen_organization_memberships}} [[organization_member]]
		JOIN {{users}} [[user]] ON [[user]].[[id]] = [[organization_member]].[[user]]
		WHERE [[organization_member]].[[organization]] = {:organization} AND [[organization_member]].[[status]] = 'active'
			AND [[user]].[[id]] != {:owner} AND trim([[user]].[[name]]) != ''
			AND lower(trim([[user]].[[name]])) LIKE {:pattern} ESCAPE '\'
			AND NOT EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[member]]
				WHERE [[member]].[[workspace_id]] = {:workspaceId} AND [[member]].[[user_id]] = [[user]].[[id]])
			AND NOT EXISTS (SELECT 1 FROM {{leafen_account_emails}} [[address]]
				WHERE [[address]].[[user_id]] = [[user]].[[id]] AND ` + provenAccountEmailSQL("[[address]].") + `)
			AND NOT EXISTS (SELECT 1 FROM {{leafen_integration_accounts}} [[app_account]] WHERE [[app_account]].[[user_id]] = [[user]].[[id]])
			AND EXISTS (SELECT 1 FROM {{leafen_administrator_account_states}} [[account]]
				WHERE [[account]].[[user_id]] = [[user]].[[id]] AND [[account]].[[status]] = {:active})
		ORDER BY lower(trim([[user]].[[name]])), [[user]].[[created]], [[user]].[[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"organization": authorization.Organization.OrganizationID,
		"owner":        authorization.WorkspaceOwnerID,
		"workspaceId":  workspaceID,
		"pattern":      pattern,
		"active":       administratorAccountStatusActive,
		"limit":        maxWorkspaceMemberCandidates,
	}).All(&stored)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceMemberListFailed, "Couldn't read the people.", err)
	}
	for _, candidate := range stored {
		response.Candidates = append(response.Candidates, workspaceMemberCandidate{
			UserID: candidate.UserID, DisplayName: candidate.DisplayName, Avatar: candidate.Avatar,
			JoinedAt: memberCandidateJoinedAt(candidate.Joined),
		})
	}
	return event.JSON(http.StatusOK, response)
}

// memberCandidateJoinedAt gives when an account was made, as RFC 3339.
func memberCandidateJoinedAt(stored string) string {
	for _, layout := range []string{"2006-01-02 15:04:05.000Z", "2006-01-02 15:04:05Z", time.RFC3339Nano} {
		if at, err := time.Parse(layout, stored); err == nil {
			return at.UTC().Format(time.RFC3339)
		}
	}
	return ""
}
