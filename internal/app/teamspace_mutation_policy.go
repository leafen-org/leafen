package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

func normalizeTeamspaceMutation(input *teamspaceMutationInput) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Action = strings.TrimSpace(input.Action)
	input.TeamspaceID = strings.TrimSpace(input.TeamspaceID)
	input.Name = strings.TrimSpace(input.Name)
	input.Icon = strings.TrimSpace(input.Icon)
	input.Description = strings.TrimSpace(input.Description)
	input.Access = strings.TrimSpace(input.Access)
	input.MemberPageRole = strings.TrimSpace(input.MemberPageRole)
	input.OpenPageRole = strings.TrimSpace(input.OpenPageRole)
	input.DefaultTeamspaceID = strings.TrimSpace(input.DefaultTeamspaceID)
	input.ReplacementDefaultTeamspaceID = strings.TrimSpace(input.ReplacementDefaultTeamspaceID)
	for index, id := range input.DefaultTeamspaceIDs {
		input.DefaultTeamspaceIDs[index] = strings.TrimSpace(id)
	}
	input.PrincipalType = strings.TrimSpace(input.PrincipalType)
	input.PrincipalID = strings.TrimSpace(input.PrincipalID)
	input.MemberRole = strings.TrimSpace(input.MemberRole)
	input.JoinRequestID = strings.TrimSpace(input.JoinRequestID)
	input.JoinResponse = strings.TrimSpace(input.JoinResponse)
	input.InviteToken = strings.TrimSpace(input.InviteToken)
}

func validTeamspaceAccess(value string) bool {
	return value == teamspaceAccessOpen || value == teamspaceAccessClosed || value == teamspaceAccessPrivate
}

func validTeamspacePageRole(value string) bool {
	switch value {
	case teamspacePageRoleView, teamspacePageRoleComment, teamspacePageRoleEdit, teamspacePageRoleFullAccess:
		return true
	default:
		return false
	}
}

// validTeamspaceOutsideRole is a role for the rest of the workspace, which
// may also be none.
func validTeamspaceOutsideRole(value string) bool {
	return value == teamspacePageRoleNone || validTeamspacePageRole(value)
}

func validTeamspaceMemberRole(value string) bool {
	return value == teamspaceMemberRoleOwner || value == teamspaceMemberRoleMember
}

func teamspaceMutationRequestHash(workspaceID string, input teamspaceMutationInput) (string, error) {
	payload, err := json.Marshal(struct {
		WorkspaceID string                 `json:"workspaceId"`
		Input       teamspaceMutationInput `json:"input"`
	}{WorkspaceID: workspaceID, Input: input})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}
