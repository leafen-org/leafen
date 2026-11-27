import { pointerAt } from "./pointerHover";

// What a comment or a feed card shows under the pointer — its buttons — Notion
// takes away while the reaction picker opened from inside it is open, and keeps
// away once the picker closes with the pointer still on it, by Escape or by a
// press beside the picker (seen 2026-11-26 in Chrome at 1440, dpr 2, in two
// tabs at different times, judged after the closing: under a page's title, on
// a reply, by a reaction's "+", in a card beside the page). A comment's come
// back with the pointer's next move on it; a feed card's only once the pointer
// has left the card and come in again — moves inside it bring none (the same
// two tabs). The browser's own :hover comes back at once there, so the host
// says so by `data-hover-rest` meanwhile, which its styles read.

type Until = "move" | "leave";

const hosts = new Map<string, Until>();
const resting = new WeakMap<Element, (event: PointerEvent) => void>();

/** Says the elements of this class rest their hover look after a layer opened
 *  from inside them closes, until the pointer moves on them again ("move") or
 *  leaves them ("leave"). */
export function hoverRestsAfterLayer(className: string, until: Until) {
  for (const name of className.split(/\s+/u)) if (name) hosts.set(name, until);
}

function within(element: Element, point: { x: number; y: number }) {
  const box = element.getBoundingClientRect();
  return point.x >= box.left && point.x < box.right && point.y >= box.top && point.y < box.bottom;
}

/** The nearest element around `element` that rests (hoverRestsAfterLayer), and until when. */
function hostAround(element: Element | null | undefined): { host: Element; until: Until } | null {
  for (let at: Element | null = element ?? null; at; at = at.parentElement) {
    for (const name of at.classList) {
      const until = hosts.get(name);
      if (until) return { host: at, until };
    }
  }
  return null;
}

/** A layer opened from `opener` closed: the nearest host around it that rests
 *  (hoverRestsAfterLayer) does so, where the pointer is still on it. */
export function restHoverAround(opener: Element | null | undefined) {
  if (typeof window === "undefined" || !opener?.isConnected) return;
  const found = hostAround(opener), at = pointerAt();
  if (!found || !at || !within(found.host, at)) return;
  const rest = found.host, kind = found.until;
  const previous = resting.get(rest);
  if (previous) window.removeEventListener("pointermove", previous, true);
  rest.setAttribute("data-hover-rest", "");
  // A move to the same point is the page moving under a still pointer, not the pointer's.
  const wake = (event: PointerEvent) => {
    if (event.clientX === at.x && event.clientY === at.y) return;
    if (kind === "leave" && rest.isConnected && within(rest, { x: event.clientX, y: event.clientY })) return;
    rest.removeAttribute("data-hover-rest");
    resting.delete(rest);
    window.removeEventListener("pointermove", wake, true);
  };
  resting.set(rest, wake);
  window.addEventListener("pointermove", wake, { capture: true, passive: true });
}

/**
 * A comment's "⋯" menu closed by Escape: the comment under the pointer — the
 * one it was opened from, or another its menu lay over — keeps its buttons
 * away until the pointer moves, as Notion's (seen 2026-11-26 in Chrome at
 * 1440 in two tabs at different times: a resting margin card's menu closed
 * by Escape with the pointer on the reply under it left the reply's buttons
 * away; a 6px move brought them). Its menu still drawn, the host is the first
 * one under the pointer beneath it.
 */
export function restHoverUnderPointer() {
  const at = pointerAt();
  if (typeof document === "undefined" || !at) return;
  restHoverAround(document.elementsFromPoint(at.x, at.y).find((element) => hostAround(element)));
}
