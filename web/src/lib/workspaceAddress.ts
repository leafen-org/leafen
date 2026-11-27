// A link Leafen gives a person to a screen of one workspace names it
// (person_links.go on the server): ?workspace= its domain or else its name, a
// hyphen and its ID — the words for the eye, the ID to open it by. The app
// opens the workspace it names, whichever it showed (2026-11-22: a Notion
// move's import screen opened the person's other workspace, without the run),
// and the address then goes on without it. Its ID or domain alone names it too.

const WORKSPACE_PARAM = "workspace";

/** The person's workspace an address's search names, or null. */
export function workspaceAskedBy(search: string, workspaces: readonly { id: string; domain?: string | null }[]): string | null {
  const asked = new URLSearchParams(search).get(WORKSPACE_PARAM)?.trim();
  if (!asked) return null;
  const named = workspaces.find(({ id }) => asked === id || asked.endsWith(`-${id}`))
    ?? workspaces.find(({ domain }) => Boolean(domain) && asked === domain);
  return named?.id ?? null;
}

/** The address as it is, without the workspace it named. */
export function addressWithoutWorkspace(location: { href: string }): string {
  const url = new URL(location.href);
  url.searchParams.delete(WORKSPACE_PARAM);
  return `${url.pathname}${url.search}${url.hash}`;
}
