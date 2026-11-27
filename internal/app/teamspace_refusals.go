package app

import (
	"strings"
	"unicode/utf8"
)

// What a refused teamspace change says to fix (request_refusal.go): the
// field and why after "Check the teamspace request.", and for a teamspace,
// member or request in a state the change can't start from, that state after
// "The teamspace changed. Check it again." A route keeps its status and code.

// teamspaceRefused refuses a teamspace change for the reason given.
func teamspaceRefused(format string, args ...any) error {
	return refuse(errTeamspaceInvalid, format, args...)
}

// teamspaceStateRefused refuses a teamspace change for the state given.
func teamspaceStateRefused(format string, args ...any) error {
	return refuse(errTeamspaceConflict, format, args...)
}

// Refusals more than one change or list says.
var (
	errTeamspaceIconNotHeld   = teamspaceRefused("icon names no image file of the workspace: upload it first, or send an emoji")
	errTeamspaceNotMember     = teamspaceStateRefused("principalId isn't in the teamspace: add_member adds them")
	errTeamspaceCursorInvalid = teamspaceRefused("the list's cursor isn't one it gave: leave it out to read from the start")
)

// teamspaceConflictMessage is a refused change's words: the state it can't
// start from, or that the teamspace changed since it was read.
func teamspaceConflictMessage(err error) string {
	if reason := refusalReason(err); reason != "" {
		return refusalMessage("The teamspace can't be changed this way now.", reason)
	}
	return "The teamspace changed. Check it again."
}

var teamspaceActions = []string{
	teamspaceActionCreate, teamspaceActionUpdate, teamspaceActionJoin, teamspaceActionRequest, teamspaceActionRespond,
	teamspaceActionAddMember, teamspaceActionUpdateMember, teamspaceActionRemoveMember, teamspaceActionLeave,
	teamspaceActionUpdateSettings, teamspaceActionSetDefault, teamspaceActionSetDefaults, teamspaceActionArchive,
	teamspaceActionRestore, teamspaceActionDelete,
}

func validateTeamspaceMutation(input teamspaceMutationInput) error {
	switch {
	case input.RequestID == "" || utf8.RuneCountInString(input.RequestID) > maxTeamspaceMutationRequestIDRunes:
		return teamspaceRefused("requestId is a new id of yours, 1–%d characters, so the same change sent again isn't made twice",
			maxTeamspaceMutationRequestIDRunes)
	case input.ExpectedRevision < 0:
		return teamspaceRefused("expectedRevision is the teamspace's revision as it was read, 0 or more")
	// Only a join carries an invite link's key.
	case input.InviteToken != "" && input.Action != teamspaceActionJoin:
		return teamspaceRefused("inviteToken goes only with action join")
	// Only a request to join says why.
	case input.JoinMessage != "" && input.Action != teamspaceActionRequest:
		return teamspaceRefused("joinMessage goes only with action request")
	case utf8.RuneCountInString(input.InviteToken) > workspaceInviteTokenRunes:
		return teamspaceRefused("inviteToken is at most %d characters, the invite link's key", workspaceInviteTokenRunes)
	}
	switch input.Action {
	case teamspaceActionCreate:
		if input.TeamspaceID != "" {
			return teamspaceRefused("teamspaceId is left out to create one: the new teamspace's id is given back")
		}
		if input.ExpectedRevision != 0 {
			return teamspaceRefused("expectedRevision is 0 to create a teamspace")
		}
		return teamspaceSettingsRefusal(input)
	case teamspaceActionUpdate:
		if err := teamspaceTargetRefusal(input); err != nil {
			return err
		}
		return teamspaceSettingsRefusal(input)
	case teamspaceActionJoin, teamspaceActionLeave, teamspaceActionArchive, teamspaceActionRestore, teamspaceActionDelete:
		return teamspaceTargetRefusal(input)
	case teamspaceActionRequest:
		if _, ok := teamspaceJoinMessage(input.JoinMessage); !ok {
			return teamspaceRefused("joinMessage is at most %d characters of text: why one asks to join, or left out", maxTeamspaceJoinMessageRunes)
		}
		return teamspaceTargetRefusal(input)
	case teamspaceActionRespond:
		if err := teamspaceTargetRefusal(input); err != nil {
			return err
		}
		switch {
		case input.JoinRequestID == "":
			return teamspaceRefused("joinRequestId is empty: it names the request to join to answer")
		case input.JoinResponse != teamspaceRequestApproved && input.JoinResponse != teamspaceRequestDenied:
			return teamspaceRefused("joinResponse is %s or %s, not %q", teamspaceRequestApproved, teamspaceRequestDenied, input.JoinResponse)
		}
	case teamspaceActionAddMember, teamspaceActionUpdateMember, teamspaceActionRemoveMember:
		if err := teamspaceTargetRefusal(input); err != nil {
			return err
		}
		switch {
		case input.PrincipalType != "user" && input.PrincipalType != "group":
			return teamspaceRefused("principalType is user or group, not %q", input.PrincipalType)
		case input.PrincipalID == "":
			return teamspaceRefused("principalId is empty: it names the person or group")
		case input.Action != teamspaceActionRemoveMember && !validTeamspaceMemberRole(input.MemberRole):
			return teamspaceRefused("memberRole is %s or %s, not %q", teamspaceMemberRoleOwner, teamspaceMemberRoleMember, input.MemberRole)
		}
	case teamspaceActionUpdateSettings:
		switch {
		case input.TeamspaceID != "":
			return teamspaceRefused("teamspaceId is left out: update_settings changes the workspace's teamspace settings")
		case input.OwnersOnlyCreate == nil:
			return teamspaceRefused("ownersOnlyCreate is true or false: whether only workspace owners make teamspaces")
		case input.ExpectedRevision < 1:
			return teamspaceRefused("expectedRevision is the workspace's teamspace settings revision as it was read, 1 or more")
		}
	case teamspaceActionSetDefault:
		if err := teamspaceTargetRefusal(input); err != nil {
			return err
		}
		if input.DefaultTeamspaceID != input.TeamspaceID {
			return teamspaceRefused("defaultTeamspaceId is the teamspaceId the change names")
		}
	case teamspaceActionSetDefaults:
		// The workspace's whole list, against its settings' revision.
		switch {
		case input.TeamspaceID != "" || input.DefaultTeamspaceID != "":
			return teamspaceRefused("teamspaceId and defaultTeamspaceId are left out: set_defaults names the whole list in defaultTeamspaceIds")
		case input.ExpectedRevision < 1:
			return teamspaceRefused("expectedRevision is the workspace's teamspace settings revision as it was read, 1 or more")
		}
		seen := make(map[string]struct{}, len(input.DefaultTeamspaceIDs))
		for index, id := range input.DefaultTeamspaceIDs {
			if id == "" {
				return teamspaceRefused("defaultTeamspaceIds[%d] is empty", index)
			}
			if _, again := seen[id]; again {
				return teamspaceRefused("defaultTeamspaceIds[%d] names %s a second time", index, id)
			}
			seen[id] = struct{}{}
		}
	default:
		return teamspaceRefused("action is one of %s, not %q", strings.Join(teamspaceActions, ", "), input.Action)
	}
	return nil
}

// teamspaceTargetRefusal is why a change of one teamspace doesn't name it as
// it was read, or nil.
func teamspaceTargetRefusal(input teamspaceMutationInput) error {
	switch {
	case input.TeamspaceID == "":
		return teamspaceRefused("teamspaceId is empty: it names the teamspace to %s", strings.ReplaceAll(input.Action, "_", " "))
	case input.ExpectedRevision < 1:
		return teamspaceRefused("expectedRevision is the teamspace's revision as it was read, 1 or more")
	}
	return nil
}

// teamspaceSettingsRefusal is why a teamspace's name, access or page roles
// can't be kept, or nil.
func teamspaceSettingsRefusal(input teamspaceMutationInput) error {
	pageRoles := strings.Join([]string{teamspacePageRoleView, teamspacePageRoleComment, teamspacePageRoleEdit, teamspacePageRoleFullAccess}, ", ")
	switch {
	case input.Name == "":
		return teamspaceRefused("name is empty: a teamspace has a name")
	case utf8.RuneCountInString(input.Name) > maxTeamspaceNameRunes:
		return teamspaceRefused("name is %d characters, over %d", utf8.RuneCountInString(input.Name), maxTeamspaceNameRunes)
	case utf8.RuneCountInString(input.Description) > maxTeamspaceDescriptionRunes:
		return teamspaceRefused("description is %d characters, over %d", utf8.RuneCountInString(input.Description), maxTeamspaceDescriptionRunes)
	case !validTeamspaceAccess(input.Access):
		return teamspaceRefused("access is %s, %s or %s, not %q", teamspaceAccessOpen, teamspaceAccessClosed, teamspaceAccessPrivate, input.Access)
	case !validTeamspacePageRole(input.MemberPageRole):
		return teamspaceRefused("memberPageRole is %s, not %q", pageRoles, input.MemberPageRole)
	case !validTeamspaceOutsideRole(input.OpenPageRole):
		return teamspaceRefused("openPageRole is %s or %s, not %q", pageRoles, teamspacePageRoleNone, input.OpenPageRole)
	case input.MembersCanInvite == nil:
		return teamspaceRefused("membersCanInvite is true or false")
	case input.MembersCanEditSidebar == nil:
		return teamspaceRefused("membersCanEditSidebar is true or false")
	}
	return nil
}
