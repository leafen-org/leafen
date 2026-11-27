import { createElement, useEffect, useLayoutEffect, useReducer, useRef, type ReactElement } from "react";
import { leavingPageBoxes, leavingPageHeld, onLeavingPageGone } from "@/lib/pageLeftAfterPaint";

/*
 * The page left for a screen of another kind, drawn on as it was, hidden,
 * until that screen is painted (lib/pageLeftAfterPaint.ts): its own element is given
 * again, so nothing in it draws again, and what it put in the page's place
 * is hidden; the window's own styles drawn there stay.
 *
 * Hidden, its boxes stand: marked `data-page-left`, it is out of the page
 * area's flow, clipped to nothing and its contents skipped
 * (content-visibility: hidden, AppShell.module.css), so the screen's frame
 * neither lays it out nor takes its boxes down; they go with the page after
 * the paint. What is skipped takes no focus, no pointer and no find, as
 * display: none (focus in it moves to the document, as there). Each part
 * keeps the place and size it had as the press left it
 * (pageLeftAfterPaint.ts): a part grown or shrunk had its size watchers
 * measure the page inside it, which worked all of it out again in that
 * frame (2026-11-15: 330 ms at CPU ×2). Hidden so, the library opened from
 * a page of 15,500 elements showed its text 175 → 164 ms first, 129 → 116
 * second (NAS condition, CPU ×2).
 */

type Shown = { key: string; element: ReactElement; pageId: string | null; nodes: readonly Element[] };

const KEPT_TAGS = new Set(["STYLE", "SCRIPT", "TEMPLATE", "LINK"]);

/** Places the page left as the screen is put in, ahead of that screen's own layout effects. */
function LeavingPagePlace({ place }: { place: () => void }) {
  useLayoutEffect(place);
  return null;
}

/**
 * The page left, while the screen shown in its place (`key`, `screen`) is not
 * yet painted; null otherwise. Drawn before that screen, where it stood. It
 * stands only while it is still the page open (`openPageId`, the page
 * `screen` shows when it is one): one let go already goes at once. `place`
 * goes between the two: it hides the page before the screen measures itself,
 * which laid the page out with it (2026-11-15: the library's tabs measured
 * beside a page of 6,500 elements, a layout of 17,900 boxes in its first
 * frame, 17–30 ms at CPU ×2).
 */
export function useLeavingPage(key: string, screen: ReactElement, shownPageId: string | null, openPageId: string | null): { leaving: ReactElement | null; place: ReactElement } {
  const [, redraw] = useReducer((count: number) => count + 1, 0);
  const last = useRef<Shown | null>(null);
  const leaving = useRef<Omit<Shown, "pageId"> | null>(null);
  const hidden = useRef<{ element: HTMLElement; top: string; size: string }[]>([]);
  const previous = last.current;
  if (previous && previous.key !== key && previous.pageId && previous.pageId === openPageId && !shownPageId && leavingPageHeld()) {
    leaving.current = { key: previous.key, element: previous.element, nodes: previous.nodes };
  }
  // A screen gone back to before its page left is let go is the page again.
  if (leaving.current?.key === key) leaving.current = null;
  const held = leaving.current;

  useEffect(() => onLeavingPageGone(() => {
    if (!leaving.current) return;
    leaving.current = null;
    redraw();
  }), []);

  const place = () => {
    const main = document.querySelector("[data-app-main]");
    const nodes = main ? [...main.children] : [];
    if (held) {
      for (const node of held.nodes) {
        if (!(node instanceof HTMLElement) || !node.isConnected || KEPT_TAGS.has(node.tagName)) continue;
        if (hidden.current.some((entry) => entry.element === node)) continue;
        hidden.current.push({ element: node, top: node.style.insetBlockStart, size: node.style.blockSize });
        const box = leavingPageBoxes().get(node);
        if (box) {
          node.style.insetBlockStart = `${box.top}px`;
          node.style.blockSize = `${box.height}px`;
        }
        node.setAttribute("data-page-left", "");
      }
      // The page area lays the screen as it does without a page's scroll in it (pageScrollHost.ts).
      const screenScrolls = nodes.some((node) => !held.nodes.includes(node) && node.hasAttribute("data-page-scroll"));
      if (main instanceof HTMLElement && !screenScrolls) delete main.dataset.pageScrollHost;
    } else if (hidden.current.length) {
      // What stays after the page went, the page gone back to, shows again.
      for (const { element, top, size } of hidden.current) {
        if (!element.isConnected) continue;
        element.removeAttribute("data-page-left");
        element.style.insetBlockStart = top;
        element.style.blockSize = size;
        if (element.hasAttribute("data-page-scroll") && main instanceof HTMLElement) main.dataset.pageScrollHost = "true";
      }
      hidden.current = [];
    }
    last.current = { key, element: screen, pageId: shownPageId, nodes: held ? nodes.filter((node) => !held.nodes.includes(node)) : nodes };
  };

  return { leaving: held?.element ?? null, place: createElement(LeavingPagePlace, { key: "leaving-page-place", place }) };
}
