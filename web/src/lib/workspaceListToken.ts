/**
 * The page list's refresh token of the workspace this tab follows
 * (useWorkspaceChangeFollow): the list read's, or the one a page change of
 * this tab answered with when the list it was made on was still current
 * (page_metadata_list_token.go). The change check asks with it, so this tab's
 * own change, which its list already shows, isn't read back with the whole
 * list, its teamspaces and the open page (2026-11-13: one page lock sent 12 to
 * 19 requests; Notion's sends one).
 */

type Followed = { workspaceId: string; token: string };

let followed: Followed | null = null;

/** The list read's token for the workspace followed; a newer read's replaces one adopted. */
export function followListToken(workspaceId: string, token: string | undefined) {
  followed = token ? { workspaceId, token } : null;
}

/** Stops following the workspace's list. */
export function unfollowListToken(workspaceId: string) {
  if (followed?.workspaceId === workspaceId) followed = null;
}

/** The token the followed workspace's list is current at, if any. */
export function listToken(workspaceId: string): string | undefined {
  return followed?.workspaceId === workspaceId ? followed.token : undefined;
}

/**
 * Sends a page change with the followed list's token, and takes the token
 * of the list after it when the server answers with one — unless the list
 * was read again meanwhile.
 */
export async function sendKeepingList<Answer extends { workspaceRefreshToken?: string }>(
  send: (token: string | undefined) => Promise<Answer>,
): Promise<Answer> {
  const sent = followed;
  const answer = await send(sent?.token);
  if (sent && answer.workspaceRefreshToken && followed === sent) {
    followed = { workspaceId: sent.workspaceId, token: answer.workspaceRefreshToken };
  }
  return answer;
}
