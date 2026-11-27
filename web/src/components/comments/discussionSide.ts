import { useEffect, useSyncExternalStore } from "react";
import { pushRoute, useSearchParams } from "@/lib/router";

/**
 * The one discussion open in its own view beside the page — by a page
 * title thread's "▯ 열기" — named by the address, as Notion's (seen
 * 2026-11-25 in Chrome at 1440, a page and a database row's page in a side
 * peek, two tabs): "?d=〈discussion〉&c=〈comment〉&dm=s" after what the address
 * already holds (a peek's "p"/"pm" stay before it). Opening it and closing it
 * by "»", "✕" or Escape each add a step to the tab's history, so the way back
 * closes it and opens it again, and a reload or the address opened afresh
 * opens it. A thread resolved — in the view or elsewhere — leaves the address as
 * it is and the view open on it (Notion's, seen 2026-11-25 in Chrome at 1440,
 * two tabs: "✓ 해결됨" by its time and a card to reopen it where its reply row
 * was, the address and the tab's history unchanged; a reload, the way back or
 * the address opened afresh open it so); a thread gone leaves the address and
 * opens nothing. Here a page's thread is named by its first comment, which is
 * both the discussion and the comment the view opens at.
 *
 * The page's title discussion draws the view (DiscussionSide) and claims it,
 * so the same page shown twice opens it once: the one whose thread was
 * pressed, or else the first one drawn.
 */
const DISCUSSION = "d", COMMENT = "c", MODE = "dm", SIDE = "s";

/** The thread an address opens in its own view, if any. */
export function discussionSideOf(search: string | URLSearchParams): string | null {
  const params = typeof search === "string" ? new URLSearchParams(search) : search;
  return params.get(MODE) === SIDE ? params.get(DISCUSSION) || params.get(COMMENT) || null : null;
}

/** The address here with the thread's view open, or with none (`null`). */
function sideAddress(threadId: string | null) {
  const url = new URL(window.location.href);
  for (const key of [DISCUSSION, COMMENT, MODE]) url.searchParams.delete(key);
  if (threadId) {
    url.searchParams.set(DISCUSSION, threadId);
    url.searchParams.set(COMMENT, threadId);
    url.searchParams.set(MODE, SIDE);
  }
  return `${url.pathname}${url.search}${url.hash}`;
}

type Claim = { threadId: string; owner: string };

let claim: Claim | null = null;
const listeners = new Set<() => void>();

function setClaim(next: Claim | null) {
  if (claim?.threadId === next?.threadId && claim?.owner === next?.owner) return;
  claim = next;
  listeners.forEach((listener) => listener());
}

export function openDiscussionSide(threadId: string, owner: string) {
  setClaim({ threadId, owner });
  if (discussionSideOf(window.location.search) !== threadId) pushRoute(sideAddress(threadId));
}

/** Closes the open view, a step of its own in the tab's history. */
export function closeDiscussionSide() {
  if (discussionSideOf(window.location.search) !== null) pushRoute(sideAddress(null));
}

/**
 * The thread among `threads` the address opens in its own view, once this
 * place (`owner`, a page's title discussion) holds it; none while the address
 * names none of them, or another place holds it.
 */
export function useDiscussionSideThread<T extends { id: string }>(threads: readonly T[], owner: string): T | null {
  const namedId = discussionSideOf(useSearchParams());
  const thread = namedId ? threads.find((candidate) => candidate.id === namedId) ?? null : null;
  const threadId = thread?.id ?? null;
  const held = useSyncExternalStore((listener) => {
    listeners.add(listener);
    return () => listeners.delete(listener);
  }, () => claim !== null && claim.threadId === threadId && claim.owner === owner, () => false);
  // Named and held by no place, the first place drawn with it takes it; a
  // place going lets it go for the next one drawn.
  useEffect(() => {
    if (threadId && claim?.threadId !== threadId) setClaim({ threadId, owner });
  }, [owner, threadId]);
  useEffect(() => () => { if (claim?.owner === owner) setClaim(null); }, [owner]);
  return held ? thread : null;
}
