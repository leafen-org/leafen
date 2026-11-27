/**
 * The teamspaces' names this window has read, by ID, for the places where a
 * page's location starts at its teamspace (Notion's path names the teamspace
 * over a teamspace's pages, seen 2026-10-31) — read where no teamspace list
 * is at hand, as a mention's card or a backlink's line.
 */
const names = new Map<string, string>();

export function rememberTeamspaceNames(teamspaces: readonly { id: string; name: string }[]) {
  for (const teamspace of teamspaces) names.set(teamspace.id, teamspace.name);
}

export function knownTeamspaceName(teamspaceId: string | null | undefined) {
  return teamspaceId ? names.get(teamspaceId) ?? "" : "";
}
