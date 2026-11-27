import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import { peekedPageId } from "@/lib/pagePeek";
import { routeInfoFromPath } from "@/lib/router";
import type { Session } from "@/types";

// A window opened afresh on the server's restored data — the one that had it
// restored, and any that takes it anew (ServerRestoredDialog) — comes back
// where it was while that is still there. A page the restore took away isn't
// shown as not found (2026-11-21: the window came back on "페이지 찾지
// 못함"): the window opens home, where the workspace's start page opens what
// is still there (useStartPage), and a page open over it that is gone closes.
// The app's tabs whose page isn't up do the same (desktop/window_tabs_restored.go).

async function gone(session: Session, pageId: string): Promise<boolean> {
  return api.readPage(session, pageId).then(
    () => false,
    (error: unknown) => error instanceof LeafenApiError && error.status === 404,
  );
}

/** Opens the window afresh on the restored data, where it was or at home. */
export async function openRestoredAfresh(session: Session): Promise<void> {
  const here = new URL(window.location.href);
  const route = routeInfoFromPath(here.pathname);
  const peeked = peekedPageId(here.searchParams.get("p"));
  const [page, peek] = await Promise.all([
    route.kind === "page" ? gone(session, route.pageId).catch(() => false) : false,
    peeked ? gone(session, peeked).catch(() => false) : false,
  ]);
  if (page) {
    window.location.replace("/");
    return;
  }
  if (peek) {
    here.searchParams.delete("p");
    here.searchParams.delete("pm");
    window.location.replace(here.toString());
    return;
  }
  window.location.reload();
}
