import { useEffect, useRef } from "react";
import type { Page, Session } from "@/types";
import type { WikiPageMenuActions } from "./WikiPageMenu";

/** The product's page actions a wiki's page's menu works with, wherever the page is open. */
export type WikiPageMenuHost = {
  pages: Page[];
  session: Session;
  favorite(pageId: string): boolean;
  /** Its page menu's operations, for the page it opens on. */
  actions(page: Page): WikiPageMenuActions;
};

// The product's, while it is open (AuthenticatedPagePeek): a peek's page —
// a view's peek among them — is drawn apart from the product's own tree.
let host: (() => WikiPageMenuHost) | null = null;

/** Keeps the product's page actions where a wiki's page's menu in a peek finds them. */
export function usePublishWikiPageMenuHost(value: WikiPageMenuHost) {
  const latest = useRef(value);
  latest.current = value;
  useEffect(() => {
    const read = () => latest.current;
    host = read;
    return () => {
      if (host === read) host = null;
    };
  }, []);
}

/** The product's page actions as they are now, or none outside the product (a shared or public page). */
export function wikiPageMenuHost(): WikiPageMenuHost | null {
  return host?.() ?? null;
}
