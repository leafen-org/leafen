package app

import (
	"encoding/json"
	"strings"
)

// A workspace's default teamspaces, as Notion's "기본 팀스페이스": any number
// of them, or none, and everyone in the workspace but its restricted members
// is in each, and stays in one that stops being a default one
// (keepDefaultTeamspaceMembers). They are kept as a list on the workspace's teamspace settings
// (default_teamspace_ids); the one default teamspace kept before
// (default_teamspace_id) became the list's only one, and that column stays
// empty since.

// teamspaceDefaultSQL says a teamspace, given by an SQL expression for its
// id, is one of the default teamspaces of a workspace, given likewise. The
// alias names its settings row and list in the query.
func teamspaceDefaultSQL(workspaceID, teamspaceID, alias string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_teamspace_settings}} [[` + alias + `]], json_each([[` + alias + `]].[[default_teamspace_ids]]) [[` + alias + `_id]]
		WHERE [[` + alias + `]].[[workspace_id]] = ` + workspaceID + ` AND [[` + alias + `_id]].[[value]] = ` + teamspaceID + `)`
}

// defaultIDs is the settings' default teamspaces, in the order they were
// chosen.
func (settings storedTeamspaceSettings) defaultIDs() []string {
	ids := []string{}
	if strings.TrimSpace(settings.DefaultTeamspaceIDs) != "" {
		_ = json.Unmarshal([]byte(settings.DefaultTeamspaceIDs), &ids)
	}
	if ids == nil {
		ids = []string{}
	}
	return ids
}

// isDefault says a teamspace is one of the settings' default teamspaces.
func (settings storedTeamspaceSettings) isDefault(teamspaceID string) bool {
	for _, id := range settings.defaultIDs() {
		if id == teamspaceID {
			return true
		}
	}
	return false
}

// withoutDefault is the settings' default teamspaces but one.
func (settings storedTeamspaceSettings) withoutDefault(teamspaceID string) []string {
	kept := []string{}
	for _, id := range settings.defaultIDs() {
		if id != teamspaceID {
			kept = append(kept, id)
		}
	}
	return kept
}
