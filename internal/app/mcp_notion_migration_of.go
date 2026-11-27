package app

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A migration tool given a migration_id alone reads that migration in the
// workspace that holds it, of those this connection reaches (2026-11-22: a
// person moving Notion into a second workspace had status, given the number
// start gave, refused twice with "a migration of this workspace" — the tools
// looked in the connection's default workspace only, while status without a
// number read every workspace). A move takes hours and another conversation
// or agent goes on with the number alone. Given workspace_id, a tool looks
// there only, as before; a refusal says what to do.

// mcpNotionMigrationIDAsked is what a migration tool says when it can't read
// the migration migration_id names.
const mcpNotionMigrationIDAsked = "give migration_id as leafen_notion_migration_start gave it, or call leafen_notion_migration_status " +
	"without migration_id for the person's latest migration, with their others in other_migrations"

// mcpNotionMigrationAuthority is the authority of a migration tool's call and
// the workspace it works in: workspace_id, or left out, the one of those this
// connection reaches that holds migration_id — the connection's default when
// there is no migration_id.
func mcpNotionMigrationAuthority(
	ctx context.Context, app core.App, workspaceID, migrationID string,
) (mcpAuthority, workspaceAuthorization, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return mcpAuthority{}, workspaceAuthorization{}, err
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if migrationID = strings.TrimSpace(migrationID); migrationID != "" {
		held, err := mcpNotionMigrationHeldIn(app, authority, workspaceID, migrationID)
		if err != nil {
			return authority, workspaceAuthorization{}, err
		}
		workspaceID = held
	}
	workspace, err := mcpNotionMigrationWorkspace(app, authority, workspaceID)
	return authority, workspace, err
}

// mcpNotionMigrationHeldIn is the workspace of the person's migration, or the
// refusal saying what to do when the call can't reach it.
func mcpNotionMigrationHeldIn(app core.App, authority mcpAuthority, workspaceID, migrationID string) (string, error) {
	var held string
	err := app.DB().Select("workspace_id").From("leafen_notion_migrations").
		Where(dbx.HashExp{"id": migrationID, "owner": authority.owner()}).Row(&held)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", mcpInvalid("the person has no migration %s: %s", migrationID, mcpNotionMigrationIDAsked)
	case err != nil:
		return "", failedBecause(errMCPToolFailed, err)
	case !slices.Contains(authority.WorkspaceIDs, held):
		return "", mcpInvalid("migration %s moves into a workspace of the person's this connection wasn't given: ask them to connect you "+
			"from that workspace's menu, %s, then call again", migrationID,
			mcpScreen(mcpScreenLanguage(app, authority.owner()), "connectAgent"))
	case workspaceID != "" && workspaceID != held:
		return "", mcpInvalid("migration %s moves into workspace %s, not workspace_id: give that one, or leave workspace_id out "+
			"and Leafen reads it where it is", migrationID, held)
	}
	return held, nil
}

// The scope guard holds sources to read and the others to content; each
// needs write in the workspace, which a migration fills.
func mcpNotionMigrationWorkspace(app core.App, authority mcpAuthority, workspaceID string) (workspaceAuthorization, error) {
	return selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: workspaceID, AllowImplicit: true}, permissionWorkspaceWrite)
}
