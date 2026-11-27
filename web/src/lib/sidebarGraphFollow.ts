import { useEffect, useState } from "react";

/*
 * The pages whose content the sidebar draws beside the open page — a
 * database view section's rows and heading, a favorite database's views
 * unfolded, a database menu's views (sidebar/sidebarHolderGraph.ts) — have no
 * room open here, so a change to them made on another screen or device,
 * through the API or by an agent, isn't heard as the open page's is
 * (2026-11-18: a view section showed a row's old name until the address
 * changed). The workspace's change socket (useWorkspaceChangeFollow.ts) says
 * which pages are drawn so, and hears when one of them changed
 * (workspace_change_graphs.go); each drawing then reads its page again. The
 * pages holding the databases of database pages mentioned on screen are
 * followed so too, whose rows renamed, sent to the trash or back their
 * mentions say (editor/unlistedMentionedPages.ts).
 */

// The server follows this many at most (workspaceChangeGraphsMax).
const FOLLOWED_GRAPHS_MAX = 64;

const drawn = new Map<string, number>();
const drawnListeners = new Set<() => void>();
const changeListeners = new Map<string, Set<() => void>>();
let telling = false;

// Many drawings come and go in one render: the socket is told once after it.
function tellDrawn() {
  if (telling) return;
  telling = true;
  queueMicrotask(() => {
    telling = false;
    for (const listener of [...drawnListeners]) listener();
  });
}

/** A page's content drawn in the sidebar, until the returned stop. */
export function drawGraphBeside(pageId: string): () => void {
  const count = drawn.get(pageId) ?? 0;
  drawn.set(pageId, count + 1);
  if (!count) tellDrawn();
  let stopped = false;
  return () => {
    if (stopped) return;
    stopped = true;
    const left = (drawn.get(pageId) ?? 1) - 1;
    if (left > 0) drawn.set(pageId, left);
    else {
      drawn.delete(pageId);
      tellDrawn();
    }
  };
}

/** The pages drawn so, as the change socket says them. */
export function graphsDrawnBeside(): string[] {
  return [...drawn.keys()].slice(0, FOLLOWED_GRAPHS_MAX);
}

/** Hears the pages drawn so change. */
export function onGraphsDrawnBesideChange(listener: () => void): () => void {
  drawnListeners.add(listener);
  return () => { drawnListeners.delete(listener); };
}

/** The pages the server said changed, or all drawn when what changed while none listened can't be told. */
export function announceGraphsChanged(pageIds: readonly unknown[]) {
  for (const pageId of pageIds) {
    if (typeof pageId !== "string") continue;
    for (const listener of [...changeListeners.get(pageId) ?? []]) listener();
  }
}

/** Hears a page drawn so changed elsewhere. */
export function onGraphChanged(pageId: string, listener: () => void): () => void {
  let set = changeListeners.get(pageId);
  if (!set) changeListeners.set(pageId, set = new Set());
  set.add(listener);
  return () => {
    set.delete(listener);
    if (!set.size) changeListeners.delete(pageId);
  };
}

/**
 * Follows the pages, drawn beside the open page (drawGraphBeside), and
 * counts the changes heard of each: what a screen read of them — a mentioned
 * row's name, icon and trash (editor/unlistedMentionedPages.ts), a sidebar
 * shortcut's database name (SidebarDatabaseShortcut.tsx) — is read again as
 * its page's count changes, a page changed on another screen, through the API
 * or by an agent; the socket opening again tells of them all, as what changed
 * while it was closed isn't known.
 */
export function useGraphChangesHeard(pageIds: readonly string[]): ReadonlyMap<string, number> {
  const [heard, setHeard] = useState<ReadonlyMap<string, number>>(() => new Map());
  const key = pageIds.join("\u0000");
  useEffect(() => {
    const stops = key ? key.split("\u0000").flatMap((pageId) => [drawGraphBeside(pageId), onGraphChanged(pageId, () => {
      setHeard((was) => new Map(was).set(pageId, (was.get(pageId) ?? 0) + 1));
    })]) : [];
    return () => { for (const stop of stops) stop(); };
  }, [key]);
  return heard;
}
