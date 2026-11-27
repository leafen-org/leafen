import { type Dispatch, type SetStateAction, useEffect } from "react";
import { PUBLIC_CONTENT_CHANGED_EVENT } from "@/components/page/pageShareEvents";
import { readAfterOwnWrites } from "@/lib/ownWritesInFlight";
import type { Bootstrap, Page } from "@/types";

/**
 * Keeps the signed-in product's page list in step with what changed beside
 * it (AuthenticatedProduct): a page's sharing changed from here reads the list
 * again, since who has a page decides where the sidebar shows it; and the open
 * page's icon follows whether its body holds anything, as it gains its first
 * block or loses its last.
 */
export function useProductPageListFollow({ refreshPages, selectedPage, openPageHasContent, setData }: {
  refreshPages: () => Promise<unknown>;
  selectedPage: Page | null | undefined;
  /** Whether the open page's body holds anything, once it is known. */
  openPageHasContent: boolean | undefined;
  setData: Dispatch<SetStateAction<Bootstrap>>;
}) {
  useEffect(() => {
    // After this tab's own changes on their way have landed (lib/ownWritesInFlight.ts).
    const read = readAfterOwnWrites(() => refreshPages().catch(() => undefined));
    window.addEventListener(PUBLIC_CONTENT_CHANGED_EVENT, read);
    return () => window.removeEventListener(PUBLIC_CONTENT_CHANGED_EVENT, read);
  }, [refreshPages]);
  useEffect(() => {
    if (!selectedPage || openPageHasContent === undefined || selectedPage.hasContent === openPageHasContent) return;
    setData((current) => ({
      ...current,
      pages: current.pages.map((page) => page.id === selectedPage.id ? { ...page, hasContent: openPageHasContent } : page),
    }));
  }, [openPageHasContent, selectedPage, setData]);
}
