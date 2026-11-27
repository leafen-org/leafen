// The page area (AppShell's main) holding a page's scroll as a child of its
// own, told so by the scroll as it is put in and taken out: the page area
// then lays its children in a column, the scroll taking what the bars over
// it leave (AppShell.module.css). A ":has()" on the page area did it, which
// Chrome worked out again at each element put in anywhere in the page — each
// new line of a long page (2026-11-14).
const held = new WeakMap<HTMLElement, number>();

/**
 * A ref for a page's scroll ([data-page-scroll]): marks the page area it
 * stands in, while it does (React 19's ref cleanup). Keep it stable
 * (useCallback): a new one each render takes the mark off and puts it back.
 */
export function holdPageScrollHost(scroll: HTMLElement | null): (() => void) | undefined {
  const host = scroll?.parentElement;
  if (!host?.hasAttribute("data-app-main")) return undefined;
  held.set(host, (held.get(host) ?? 0) + 1);
  host.dataset.pageScrollHost = "true";
  return () => {
    const left = (held.get(host) ?? 1) - 1;
    held.set(host, left);
    if (left <= 0) delete host.dataset.pageScrollHost;
  };
}

const feeds = new WeakMap<HTMLElement, number>();

/**
 * A ref for a feed view's body: marks the page's scroll it stands in
 * ([data-page-scroll-feed]) while it does, for the view settings and
 * automations beside it to narrow that column only (their modules). A
 * ":has([data-view-type=feed])" on the scroll did it, which Chrome looked
 * for again through the whole page at each element put in (2026-11-14).
 * Module-level, so stable as a ref.
 */
export function holdFeedInPageScroll(view: HTMLElement | null): (() => void) | undefined {
  const scroll = view?.parentElement?.closest<HTMLElement>("[data-page-scroll]");
  if (!scroll) return undefined;
  feeds.set(scroll, (feeds.get(scroll) ?? 0) + 1);
  scroll.dataset.pageScrollFeed = "true";
  return () => {
    const left = (feeds.get(scroll) ?? 1) - 1;
    feeds.set(scroll, left);
    if (left <= 0) delete scroll.dataset.pageScrollFeed;
  };
}
