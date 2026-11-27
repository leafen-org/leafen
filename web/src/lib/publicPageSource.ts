import { createContext, useContext, useEffect, useState } from "react";
import {
  appendPublicContentPageWindow,
  type PublicContentPageDocument,
} from "./publicContentDocument";

// A published page opens its database pages in place, as Notion's public
// pages do: the share link or site it was opened from reads the database
// page, window by window, for the peek to show.

export type PublicPageSource = {
  /** Reads one window of a page the source shows. */
  read: (pageId: string, graphCursor: string | undefined, signal: AbortSignal) => Promise<PublicContentPageDocument>;
  /** A published site shows its pages with static read-only semantics. */
  site: boolean;
};

export const PublicPageSourceContext = createContext<PublicPageSource | null>(null);

export function usePublicPageSource() {
  return useContext(PublicPageSourceContext);
}

type PublicPageDocumentState = {
  pageId: string;
  document?: PublicContentPageDocument;
  failed?: boolean;
};

/** A page of the published source, shown as its windows arrive. */
export function usePublicPageDocument(pageId: string): PublicPageDocumentState {
  const source = usePublicPageSource();
  const [state, setState] = useState<PublicPageDocumentState>({ pageId });
  useEffect(() => {
    if (!source) return;
    const controller = new AbortController();
    setState({ pageId });
    void (async () => {
      let document: PublicContentPageDocument | undefined;
      let cursor: string | undefined;
      try {
        do {
          const window = await source.read(pageId, cursor, controller.signal);
          document = document && cursor ? appendPublicContentPageWindow(document, window, cursor) : window;
          if (controller.signal.aborted) return;
          setState({ pageId, document });
          cursor = document.graphHasMore && document.graphNextCursor ? document.graphNextCursor : undefined;
        } while (cursor);
      } catch {
        if (!controller.signal.aborted) setState((current) => ({ ...current, failed: true }));
      }
    })();
    return () => controller.abort();
  }, [pageId, source]);
  return state.pageId === pageId ? state : { pageId };
}
