import { useSyncExternalStore } from "react";
import type { SuggestionDecision } from "./commentPatches";

// Suggestions settled from here that the page still draws as suggested: the
// server changes the page's text as it settles one, which reaches the page
// through the room 132–177 ms after the press under the NAS condition, and
// the blocks it keeps or takes away come with the page's next read after its
// answer, about 350 ms after the press (2026-11-25). Meanwhile the page draws
// them as settled (SettlingSuggestionMarks). One the server refuses is drawn
// as suggested again at once.

/** How long a settled suggestion is drawn so after the server's answer while the page still draws it as suggested. */
const DRAWN_WAIT_MS = 5_000;

/** A suggestion settling from here: how, and the blocks a removal suggests away. */
export type SuggestionSettling = { decision: SuggestionDecision; removed: readonly string[] };
type PageSettling = ReadonlyMap<string, SuggestionSettling>;

const NONE: PageSettling = new Map();
const settling = new Map<string, PageSettling>();
const listeners = new Set<() => void>();

function mark(pageId: string, suggestionId: string, entry: SuggestionSettling | null) {
  const next = new Map(settling.get(pageId) ?? NONE);
  if (entry) next.set(suggestionId, entry);
  else next.delete(suggestionId);
  if (next.size) settling.set(pageId, next);
  else settling.delete(pageId);
  for (const listener of listeners) listener();
}

/** The page's own elements, by the attribute its blocks carry (BlockFrame). */
export function settlingPage(pageId: string) {
  return `[data-page-id="${CSS.escape(pageId)}"]`;
}

/**
 * What the page draws of a suggestion until it draws it settled: its words,
 * the blocks it suggests in, the lines that end in its "⏎"
 * (NativeTiptapTextView) and, accepted, the blocks it suggests away.
 */
function drawnOfSuggestion(pageId: string, suggestionId: string, { decision, removed }: SuggestionSettling) {
  const page = settlingPage(pageId), id = CSS.escape(suggestionId);
  return [`${page} [data-suggestion-id="${id}"]`, `${page}[data-suggested-by="${id}"]`, `${page} [data-suggested-break-by="${id}"]`,
    ...decision === "accept" ? removed.map((blockId) => `${page}[data-block-id="${CSS.escape(blockId)}"]`) : []].join(", ");
}

/** Settles once the page draws nothing of the suggestion as suggested, looked at each frame, or after a while. */
function afterSettledDrawn(pageId: string, suggestionId: string, entry: SuggestionSettling) {
  if (typeof document === "undefined" || typeof requestAnimationFrame !== "function" || typeof CSS === "undefined") return Promise.resolve();
  const drawn = drawnOfSuggestion(pageId, suggestionId, entry);
  const until = performance.now() + DRAWN_WAIT_MS;
  return new Promise<void>((resolve) => {
    const look = () => {
      if (!document.querySelector(drawn) || performance.now() > until) resolve();
      else requestAnimationFrame(look);
    };
    requestAnimationFrame(look);
  });
}

/**
 * Draws a suggestion's words and blocks on the page as accepted or rejected
 * from now: `landed` once the server settled it — they stay so until the
 * page draws what the server made of them —, `refused` when it didn't.
 */
export function showSuggestionSettling(pageId: string, suggestionId: string, entry: SuggestionSettling) {
  mark(pageId, suggestionId, entry);
  let ended = false;
  const end = () => {
    if (ended) return;
    ended = true;
    mark(pageId, suggestionId, null);
  };
  return {
    landed: () => void afterSettledDrawn(pageId, suggestionId, entry).then(end),
    refused: end,
  };
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** The suggestions on the page settled from here that it is still to draw as the server settled them. */
export function useSettlingSuggestions(pageId: string): PageSettling {
  const read = () => settling.get(pageId) ?? NONE;
  return useSyncExternalStore(subscribe, read, read);
}
