import { api } from "@/api";
import type { Page, Session, Workspace } from "@/types";
import { personValue, storePersonValue } from "./browserStorage";

// Where the product opens for a person, decided here alone: what this
// browser remembers of them, and what their account keeps where it doesn't.
//
// This browser remembers the workspace the person last used on it, as
// Notion's app does on its device (2026-10-16: the first one opened every
// time). A browser that has never seen them — a new device, an agent's
// sign-in in a browser of its own — remembers nothing, and opened the
// first workspace in the list, its Home, whichever the person used last
// (2026-11-27, an agent's "AI로 로그인" on an account with four
// workspaces): it opens where the account last was instead, the page of
// the person's newest visit in any of their workspaces, which the server
// keeps for the account (page_visits.go). In the workspace it opens, the
// start page is the newest visit there (useStartPage.ts).

/** The workspace a person last used on this browser. */
const LAST_WORKSPACE_KEY = "last-workspace";

// Whom the last choice found nothing to go by: their start page asks the
// account where they were (takeUnrememberedLanding).
let unremembered: string | null = null;

/** Keeps the workspace the person uses now as the one this browser opens with. */
export function rememberWorkspace(userId: string, workspaceId: string) {
  storePersonValue(LAST_WORKSPACE_KEY, workspaceId, { userId });
}

/**
 * The workspace the list opens on: one asked for (a profile switch's, an
 * address's), the one shown already, the address's page's, the one this
 * browser remembers, else the first — noted as chosen without memory when
 * there was any other to choose.
 */
export function landingWorkspace(input: {
  userId: string;
  workspaces: readonly Workspace[];
  asked: string | null;
  current: string | null;
  pageWorkspace: string | null;
}): string | null {
  const listed = (id: string | null) => Boolean(id && input.workspaces.some((workspace) => workspace.id === id));
  if (input.asked) return input.asked;
  if (listed(input.current)) return input.current;
  if (input.pageWorkspace) return input.pageWorkspace;
  const remembered = personValue(LAST_WORKSPACE_KEY, { userId: input.userId });
  if (listed(remembered)) return remembered;
  unremembered = input.workspaces.length > 1 ? input.userId : null;
  return input.workspaces[0]?.id ?? null;
}

/** Whether the list opened on its first workspace for want of anything remembered; asked once. */
export function takeUnrememberedLanding(userId: string): boolean {
  const taken = unremembered === userId;
  unremembered = null;
  return taken;
}

/**
 * The page of the person's newest visit in any of their workspaces, from
 * the server's visits — each workspace's newest first, a page the list
 * can't open (`usable`) passed over; none when nothing could be read.
 */
export async function accountLastPage(
  session: Session,
  workspaces: readonly Workspace[],
  usable: (pageId: string, workspaceId: string) => Page | null,
): Promise<Page | null> {
  const reads = await Promise.all(workspaces.map((workspace) => api.readPageVisits(session, workspace.id)
    .then(({ visits }) => ({ workspaceId: workspace.id, visits }), () => null)));
  let newest: { page: Page; at: number } | null = null;
  for (const read of reads) {
    for (const visit of read?.visits ?? []) {
      const page = usable(visit.pageId, read!.workspaceId);
      if (!page) continue;
      const at = Date.parse(visit.visitedAt);
      if (!newest || at > newest.at) newest = { page, at };
      break;
    }
  }
  return newest?.page ?? null;
}
