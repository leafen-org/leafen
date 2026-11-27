package app

import "time"

type storedTeamspace struct {
	ID                    string `db:"id"`
	WorkspaceID           string `db:"workspace_id"`
	Name                  string `db:"name"`
	Icon                  string `db:"icon"`
	Description           string `db:"description"`
	Access                string `db:"access"`
	MemberPageRole        string `db:"member_page_role"`
	OpenPageRole          string `db:"open_page_role"`
	OutsideAccess         bool   `db:"outside_access"` // the rest of the workspace has OpenPageRole (migration 389)
	MembersCanInvite      bool   `db:"members_can_invite"`
	MembersCanEditSidebar bool   `db:"members_can_edit_sidebar"`
	DisablePublishing     bool   `db:"disable_publishing"`
	DisableGuests         bool   `db:"disable_guests"`
	DisableExport         bool   `db:"disable_export"`
	ArchivedAt            int64  `db:"archived_at"`
	Revision              int    `db:"revision"`
	CreatedBy             string `db:"created_by"`
	UpdatedBy             string `db:"updated_by"`
	CreatedAt             int64  `db:"created_at"`
	UpdatedAt             int64  `db:"updated_at"`
}

type storedTeamspaceSettings struct {
	WorkspaceID         string `db:"workspace_id"`
	DefaultTeamspaceIDs string `db:"default_teamspace_ids"` // a JSON list (teamspace_default_list_schema.go)
	OwnersOnlyCreate    bool   `db:"owners_only_create"`
	Revision            int    `db:"revision"`
	UpdatedBy           string `db:"updated_by"`
	UpdatedAt           int64  `db:"updated_at"`
}

type storedTeamspaceMember struct {
	ID                string `db:"id"`
	WorkspaceID       string `db:"workspace_id"`
	TeamspaceID       string `db:"teamspace_id"`
	PrincipalType     string `db:"principal_type"`
	PrincipalID       string `db:"principal_id"`
	WorkspaceMemberID string `db:"workspace_member_id"`
	Role              string `db:"role"`
	Revision          int    `db:"revision"`
	CreatedBy         string `db:"created_by"`
	CreatedAt         int64  `db:"created_at"`
	UpdatedAt         int64  `db:"updated_at"`
}

type storedTeamspaceJoinRequest struct {
	ID                string `db:"id"`
	WorkspaceID       string `db:"workspace_id"`
	TeamspaceID       string `db:"teamspace_id"`
	UserID            string `db:"user_id"`
	WorkspaceMemberID string `db:"workspace_member_id"`
	Status            string `db:"status"`
	Revision          int    `db:"revision"`
	CreatedBy         string `db:"created_by"`
	DecidedBy         string `db:"decided_by"`
	DecidedAt         int64  `db:"decided_at"`
	CreatedAt         int64  `db:"created_at"`
	UpdatedAt         int64  `db:"updated_at"`
	// Why they ask, when they said (Notion's "메시지(선택사항)").
	Message string `db:"message"`
}

type teamspaceAuthority struct {
	Workspace workspaceAuthorization
	GroupIDs  map[string]struct{}
	// Each of the person's groups' names by its id, as a teamspace they are
	// in through one names it.
	GroupNames map[string]string
}

type teamspaceCursor struct {
	Version        int    `json:"version"`
	Revision       int    `json:"revision"`
	AuxRevision    int    `json:"auxRevision,omitzero"`
	PersonRevision int64  `json:"personRevision,omitzero"`
	Updated        int64  `json:"updated"`
	ID             string `json:"id"`
}

func projectTeamspace(value storedTeamspace) teamspaceWireRecord {
	archived := ""
	if value.ArchivedAt > 0 {
		archived = time.UnixMilli(value.ArchivedAt).UTC().Format(time.RFC3339Nano)
	}
	return teamspaceWireRecord{
		SchemaVersion: teamspaceSchemaVersion, ID: value.ID, WorkspaceID: value.WorkspaceID,
		Name: value.Name, Icon: value.Icon, Description: value.Description, Access: value.Access,
		MemberPageRole: value.MemberPageRole, OpenPageRole: teamspaceOutsideRole(value),
		MembersCanInvite: teamspaceMembersInvite(value), MembersCanEditSidebar: value.MembersCanEditSidebar,
		DisablePublishing: value.DisablePublishing, DisableGuests: value.DisableGuests, DisableExport: value.DisableExport,
		ArchivedAt: archived, Revision: value.Revision, CreatedBy: value.CreatedBy,
		UpdatedBy: value.UpdatedBy, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func projectTeamspaceSettings(value storedTeamspaceSettings) teamspaceSettingsWire {
	return teamspaceSettingsWire{
		SchemaVersion: teamspaceSchemaVersion, WorkspaceID: value.WorkspaceID,
		DefaultTeamspaceIDs: value.defaultIDs(), OwnersOnlyCreate: value.OwnersOnlyCreate,
		Revision: value.Revision, UpdatedBy: value.UpdatedBy, UpdatedAt: value.UpdatedAt,
	}
}

func projectTeamspaceMember(value storedTeamspaceMember) teamspaceMemberWire {
	return teamspaceMemberWire{
		ID: value.ID, WorkspaceID: value.WorkspaceID, TeamspaceID: value.TeamspaceID,
		PrincipalType: value.PrincipalType, PrincipalID: value.PrincipalID,
		WorkspaceMemberID: value.WorkspaceMemberID, Role: value.Role, Revision: value.Revision,
		CreatedBy: value.CreatedBy, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func projectTeamspaceJoinRequest(value storedTeamspaceJoinRequest) teamspaceJoinRequestWire {
	return teamspaceJoinRequestWire{
		ID: value.ID, WorkspaceID: value.WorkspaceID, TeamspaceID: value.TeamspaceID,
		UserID: value.UserID, WorkspaceMemberID: value.WorkspaceMemberID, Status: value.Status,
		Revision: value.Revision, CreatedBy: value.CreatedBy, DecidedBy: value.DecidedBy,
		DecidedAt: value.DecidedAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, Message: value.Message,
	}
}

// teamspaceOutsideRole is the role the rest of the workspace has in a
// teamspace's pages, "none" when it has none, as Notion's "그 외 〈작업공간〉의
// 멤버" says.
func teamspaceOutsideRole(value storedTeamspace) string {
	if value.Access == teamspaceAccessPrivate || !value.OutsideAccess {
		return teamspacePageRoleNone
	}
	return value.OpenPageRole
}

// storedOutsideRole is what a teamspace keeps for the role given to the rest
// of its workspace: the role, and whether they have it. "None" keeps the role
// it had, so choosing a role again offers it.
func storedOutsideRole(access, outsideRole, previous string) (string, bool) {
	if outsideRole == teamspacePageRoleNone || access == teamspaceAccessPrivate {
		if !validTeamspacePageRole(previous) {
			previous = teamspacePageRoleView
		}
		if outsideRole != teamspacePageRoleNone {
			previous = outsideRole
		}
		return previous, false
	}
	return outsideRole, true
}

// pageSecurityChoice is a teamspace's page security switch after a change:
// the one given, or the one it had.
func pageSecurityChoice(given *bool, had bool) bool {
	if given == nil {
		return had
	}
	return *given
}
