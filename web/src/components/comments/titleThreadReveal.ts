import { useSyncExternalStore } from "react";
import { motionSafeScrollBehavior } from "@/lib/motion";

/**
 * A page's own thread pressed in "모든 토론" shows its place, as Notion's
 * (seen 2026-11-25 in Chrome at 1440, two tabs, the caret judged by one
 * letter typed after the press): under the page's title the thread opens its
 * replies and its reply row, the caret left where it was, and the page goes
 * back to its top (300px down, it went to 0). Pressing another opens that one
 * too, the first staying open, and closing the panel leaves them open — as a
 * thread's "↩" leaves its reply row (CommentCard).
 *
 * Notion's panel also opened its own first page thread each time (three times
 * in two tabs, whichever was pressed); a thread nobody pressed is not opened
 * here.
 */
const asks = new Map<string, number>();
const listeners = new Set<() => void>();

export function revealTitleThread(threadId: string) {
  asks.set(threadId, (asks.get(threadId) ?? 0) + 1);
  listeners.forEach((listener) => listener());
  const thread = document.querySelector<HTMLElement>(
    `[data-comments-mode="page-inline"] [data-comment-thread="${CSS.escape(threadId)}"]`);
  thread?.closest<HTMLElement>("[data-page-scroll]")?.scrollTo({ top: 0, behavior: motionSafeScrollBehavior() });
}

/** How many times the thread under a page's title has been asked to show its place; none counted elsewhere. */
export function useTitleThreadAsks(threadId: string, underTitle: boolean) {
  return useSyncExternalStore((listener) => {
    listeners.add(listener);
    return () => listeners.delete(listener);
  }, () => underTitle ? asks.get(threadId) ?? 0 : 0, () => 0);
}
