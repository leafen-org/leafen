import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { usePageIconImageUrl } from "@/components/page/PageIcon";
import { mentionSelector, type MentionIconPage } from "./pageMentionIconCss";

// What the mentions on screen name, read as the screen changes, and the
// picture icons drawn before them: for the pages', databases' and data
// sources' mentions (PageMentionIconStyles.tsx, DataSourceMentionStyles.tsx)
// and a published page's (PublicMentionIconStyles.tsx).
const MENTION_SCAN_DELAY_MS = 200;
// How long the view stays still after a scroll or a resize before the
// mentions nearest it are taken again (useMentionWindow).
const MENTION_VIEW_DELAY_MS = 200;

/** The pages or databases mentioned on screen, read again as the screen changes. */
export function useMentionedIds(selector: string, read: (element: HTMLElement) => string | undefined) {
  const [ids, setIds] = useState<ReadonlySet<string>>(() => new Set());
  useEffect(() => {
    let timer = 0;
    const scan = () => {
      const found = new Set(Array.from(document.querySelectorAll<HTMLElement>(selector), (element) => read(element) ?? "").filter(Boolean));
      setIds((current) => current.size === found.size && [...found].every((id) => current.has(id)) ? current : found);
    };
    const observer = new MutationObserver(() => {
      window.clearTimeout(timer);
      timer = window.setTimeout(scan, MENTION_SCAN_DELAY_MS);
    });
    observer.observe(document.body, { childList: true, subtree: true });
    scan();
    return () => {
      window.clearTimeout(timer);
      observer.disconnect();
    };
  }, [read, selector]);
  return ids;
}

/**
 * A mentioned page's image or custom emoji icon, once its address is known:
 * 16px in the mark's 20, rounded 1, as Notion's (measured 2026-11-01) — or a
 * mentioned database's, `selector` its mentions' slot.
 */
export function MentionImageIconRule({ page, selector = mentionSelector(page.id) }: { page: MentionIconPage; selector?: string }) {
  const url = usePageIconImageUrl(page);
  if (!url) return null;
  const safe = url.replace(/["\\\n\r()]/gu, (character) => encodeURIComponent(character));
  return (
    <style data-page-mention-icon={page.id}>
      {`${selector}{content:"";box-sizing:border-box;padding:0.125em;background:url("${safe}") center/cover no-repeat content-box;border-radius:calc(0.125em + 1px);-webkit-mask:none;mask:none}`}
    </style>
  );
}

// The view's moves, counted once it stays still after a scroll (of the page
// or anything in it) or a resize, while a screen has more mentions to read
// than it reads at a time.
let viewMoves = 0;
let viewTimer = 0;
const viewListeners = new Set<() => void>();

function viewMoved() {
  window.clearTimeout(viewTimer);
  viewTimer = window.setTimeout(() => {
    viewMoves += 1;
    for (const listener of [...viewListeners]) listener();
  }, MENTION_VIEW_DELAY_MS);
}

function followView(listener: () => void) {
  if (viewListeners.size === 0) {
    document.addEventListener("scroll", viewMoved, { capture: true, passive: true });
    window.addEventListener("resize", viewMoved);
  }
  viewListeners.add(listener);
  return () => {
    viewListeners.delete(listener);
    if (viewListeners.size > 0) return;
    window.clearTimeout(viewTimer);
    document.removeEventListener("scroll", viewMoved, { capture: true });
    window.removeEventListener("resize", viewMoved);
  };
}

const followNothing = () => () => {};

/** How far from the view an element is drawn, 0 within it; one not drawn (in a folded toggle) as far as can be. */
function distanceFromView(element: HTMLElement, height: number) {
  if (element.getClientRects().length === 0) return Number.POSITIVE_INFINITY;
  const { top, bottom } = element.getBoundingClientRect();
  return bottom < 0 ? -bottom : top > height ? top - height : 0;
}

/** The IDs, those whose mentions (`selector`, `read`) are drawn nearest the view first, the rest as they were. */
function nearestToView(ids: readonly string[], selector: string, read: (element: HTMLElement) => string | undefined) {
  const wanted = new Set(ids);
  const height = window.innerHeight;
  const distance = new Map<string, number>();
  for (const element of document.querySelectorAll<HTMLElement>(selector)) {
    const id = read(element);
    if (!id || !wanted.has(id)) continue;
    distance.set(id, Math.min(distance.get(id) ?? Number.POSITIVE_INFINITY, distanceFromView(element, height)));
  }
  return ids.map((id, index) => ({ id, index, away: distance.get(id) ?? Number.POSITIVE_INFINITY }))
    .sort((one, other) => one.away - other.away || one.index - other.index).map(({ id }) => id);
}

/**
 * Of the IDs the mentions on screen name (`ids`), those a screen reads names,
 * icons or places for, `limit` at most, as each is a preview of its own for
 * the server to read: all of them while there are no more; else those whose
 * mentions (`selector`, `read`) are drawn nearest the view, taken again once
 * the view stays still after a scroll. A page mentioning more read the first
 * it drew alone, and a mention further down kept the name it was written
 * with and no icon however far one scrolled, a page opened to the reader
 * again "사용 권한 없음" (2026-11-25). The same set while it holds the same IDs.
 */
export function useMentionWindow(ids: readonly string[], selector: string, read: (element: HTMLElement) => string | undefined, limit: number) {
  const over = ids.length > limit;
  const moves = useSyncExternalStore(over ? followView : followNothing, () => (over ? viewMoves : 0));
  const taken = useRef<ReadonlySet<string>>(new Set());
  return useMemo(() => {
    const next = over ? nearestToView(ids, selector, read).slice(0, limit) : ids;
    if (next.length !== taken.current.size || next.some((id) => !taken.current.has(id))) taken.current = new Set(next);
    return taken.current;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- a move of the view takes them again
  }, [ids, limit, moves, over, read, selector]);
}

/** The IDs the mentions on screen (`selector`, `read`) name as drawn now. */
export function mentionedNow(selector: string, read: (element: HTMLElement) => string | undefined): ReadonlySet<string> {
  return new Set(Array.from(document.querySelectorAll<HTMLElement>(selector), (element) => read(element) ?? "").filter(Boolean));
}

/**
 * What a screen read now of the mentions nearest the view (`taken`, `now`),
 * and after it what it read of those further, while they are drawn
 * (`drawn`, mentionedNow — the IDs a screen follows lag behind it, and a
 * mention redrawn closed or open leaves one set for another a moment): one
 * coming nearer again keeps what it showed until it is read again.
 */
export function keptFurther<T>(was: ReadonlyMap<string, T>, now: ReadonlyMap<string, T>, taken: ReadonlySet<string>, drawn: ReadonlySet<string>) {
  const next = new Map(now);
  for (const [id, value] of was) if (!taken.has(id) && !next.has(id) && drawn.has(id)) next.set(id, value);
  return next;
}
