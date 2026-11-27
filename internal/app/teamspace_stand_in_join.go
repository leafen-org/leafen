package app

import "github.com/pocketbase/pocketbase/core"

// A teamspace no owner answers — none at all, only restricted ones, or ones
// whose accounts can't act now — has the workspace's owner standing in for
// them (managesTeamspace), and its join requests reach them alone
// (teamspaceJoinRequestApprovers). Their own request would reach no one, so
// the workspace's owner outside it joins it at once instead, as they may add
// anyone to it: its settings window's head and a page path's teamspace
// window offer "참여하기" where they offered "요청" (TeamspaceJoinButton),
// and a request is refused.

// standInJoins makes the decision of the workspace's owner, outside an
// active teamspace they stand in for, one that joins rather than asks.
func standInJoins(decision *teamspaceAccessDecisionWire) {
	decision.CanJoin, decision.CanRequest = true, false
}

// letStandInJoin decides it for one teamspace, asking who answers it only for
// the workspace's owner outside an active one they can't already join.
func letStandInJoin(
	app core.App, authority teamspaceAuthority, teamspace storedTeamspace, decision *teamspaceAccessDecisionWire,
) error {
	if authority.Workspace.Role != workspaceRoleOwner || decision.Joined || decision.CanJoin || teamspace.ArchivedAt != 0 {
		return nil
	}
	manages, err := managesTeamspace(app, authority, teamspace.ID, *decision)
	if manages {
		standInJoins(decision)
	}
	return err
}

// letStandInsJoin decides it for a list of active teamspaces by the owners
// who answer each, as their summaries count them.
func letStandInsJoin(
	authority teamspaceAuthority, decisions []teamspaceAccessDecisionWire, summaries []teamspaceSummaryWire,
) {
	if authority.Workspace.Role != workspaceRoleOwner {
		return
	}
	answering := make(map[string]int, len(summaries))
	for _, summary := range summaries {
		answering[summary.TeamspaceID] = summary.AnsweringOwnerCount
	}
	for index := range decisions {
		if !decisions[index].Joined && answering[decisions[index].TeamspaceID] == 0 {
			standInJoins(&decisions[index])
		}
	}
}
