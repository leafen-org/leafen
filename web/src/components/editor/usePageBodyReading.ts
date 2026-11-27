import { type RefObject, useLayoutEffect, useRef } from "react";
import { pageBusyCopyingDatabases, usePageBusy } from "@/lib/pageBusy";
import { usePageFilling } from "@/lib/pageCopyProgress";
import { pageTitleFocusAsked } from "@/lib/pageTitleFocus";
import readingStyles from "./PageBodyReading.module.css";

/**
 * A page opened before its body is read shows no head either until both draw
 * in one frame, as Notion's (PageBodyReading.module.css): while its own body
 * is first read, its page (the element holding its head) wears the reading
 * class. It holds only for the page's first read in this view — a head once
 * shown stays, though its body is read again — and not while the body is
 * being filled with a copy's databases (its head shows, as it did). A page
 * whose title has the caret or is asked for it (one just made) shows its head
 * at once: a hidden title takes no caret and loses the one it has (2026-11-15:
 * a new page's title took the caret as it opened, then lost it as its head
 * hid, and the first key went nowhere). A body inside another (a synced
 * block's) and a database page's, whose head is its database's name, leave
 * their page's head as it is.
 */
export function usePageBodyReading(pageId: string, loading: boolean, rootRef: RefObject<HTMLElement | null>) {
  const filling = usePageFilling(pageId);
  const busy = usePageBusy(pageId);
  // The page whose head has been shown here.
  const shownFor = useRef<string | null>(null);
  const reading = loading && !filling && busy !== pageBusyCopyingDatabases && shownFor.current !== pageId;
  useLayoutEffect(() => {
    const root = rootRef.current;
    const doc = root?.closest("[data-page-search-root]");
    const head = root && doc && !root.parentElement?.closest("[data-editor-page]") ? doc.querySelector(":scope > [data-page-header-root]") : null;
    if (!reading || !doc || !head || head.contains(document.activeElement) || pageTitleFocusAsked(pageId)) {
      shownFor.current = pageId;
      return undefined;
    }
    doc.classList.add(readingStyles.reading);
    return () => doc.classList.remove(readingStyles.reading);
  }, [pageId, reading, rootRef]);
}
