import { createContext, useCallback, useContext } from "react";
import { pagePeekHref, peekModeOf } from "@/lib/pagePeek";
import { pushRoute } from "@/lib/router";

/** How a row's page in a view's peek opens a related page: in that peek, in its own way. */
export const RelatedPageOpenContext = createContext<((pageId: string) => void) | null>(null);

/**
 * Opens a relation's page pressed on a row's page as Notion's does (seen
 * 2026-11-12): in a peek over the page, in the middle — whatever way the
 * related database opens its pages — and, from a page in a peek, in that
 * peek in its own way, beside or in the middle, the page shown there giving
 * way to it.
 */
export function useRelatedPageOpener() {
  const inViewPeek = useContext(RelatedPageOpenContext);
  return useCallback((pageId: string) => {
    if (inViewPeek) return inViewPeek(pageId);
    const params = new URL(window.location.href).searchParams;
    const mode = params.get("p") && peekModeOf(params.get("pm")) === "side" ? "side" : "center";
    pushRoute(pagePeekHref(pageId, mode));
  }, [inViewPeek]);
}
