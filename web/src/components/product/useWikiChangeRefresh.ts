import { useEffect, useRef } from "react";
import { WIKI_CHANGED_EVENT } from "@/lib/wikiChangeEvents";
import { readAfterOwnWrites } from "@/lib/ownWritesInFlight";

/**
 * Reads the pages, and the open page's content, again after a page turns
 * into a wiki or back.
 */
export function useWikiChangeRefresh(
  refresh: () => Promise<unknown>,
  reread: (pageId: string) => Promise<unknown>,
  selectedPageId: string | null,
) {
  const current = useRef({ refresh, reread, selectedPageId });
  current.current = { refresh, reread, selectedPageId };
  useEffect(() => {
    // After this tab's own changes on their way have landed (lib/ownWritesInFlight.ts).
    const onWikiChanged = readAfterOwnWrites(() => {
      const { refresh: readPages, reread: readContent, selectedPageId: pageId } = current.current;
      return readPages()
        .then(() => (pageId ? readContent(pageId) : undefined))
        .catch(() => undefined);
    });
    window.addEventListener(WIKI_CHANGED_EVENT, onWikiChanged);
    return () => window.removeEventListener(WIKI_CHANGED_EVENT, onWikiChanged);
  }, []);
}
