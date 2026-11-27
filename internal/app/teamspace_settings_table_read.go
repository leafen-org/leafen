package app

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

// readTeamspaceSnapshotState reads the active teamspaces someone sees — those
// they are in and, but for a restricted member, the open and closed ones —
// for the teamspace window and connected tools.
func readTeamspaceSnapshotState(
	ctx context.Context, app core.App, principalID, workspaceID, rawCursor string,
) (teamspaceSnapshotResponse, error) {
	return readTeamspaceSnapshotFor(ctx, app, principalID, workspaceID, rawCursor, false)
}

// readTeamspaceTableSnapshotState reads 설정 › 팀스페이스's table and the
// window it opens on a row: for the workspace's owner every active
// teamspace, a private one they are not in too, as Notion's Enterprise
// workspace owner sees in that table (its help: "Workspace owners on the
// Enterprise Plan can see, join and make themselves owner of any
// teamspace"; its code, 2026-11-18) and joins from its "…"
// (privilegedTeamspaceSelfMembership); for anyone else what they see
// anyway. Such a teamspace's decision still says it is not visible to them:
// the sidebar, search, move targets and a page's path keep to the
// teamspaces someone is in or may see (decideTeamspaceAccess), and its
// pages stay closed to them until they join.
func readTeamspaceTableSnapshotState(
	ctx context.Context, app core.App, principalID, workspaceID, rawCursor string,
) (teamspaceSnapshotResponse, error) {
	return readTeamspaceSnapshotFor(ctx, app, principalID, workspaceID, rawCursor, true)
}
