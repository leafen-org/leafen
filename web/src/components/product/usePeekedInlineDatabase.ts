import { useEffect, useSyncExternalStore } from "react";
import { api } from "@/api";
import { rememberInlineDatabaseAddress, subscribeInlineDatabaseAddresses } from "@/lib/inlineDatabaseAddress";
import { peekedInlineDatabase } from "@/lib/pagePeek";
import type { Session } from "@/types";

/**
 * The inline database an address's peek shows alone (peekedInlineDatabase),
 * with the page holding it, followed as that page comes to be known: by the
 * press that opened it, the page under it drawing it, or the server's answer
 * (PeekedInlineDatabaseAsk).
 */
export function usePeekedInlineDatabase(search: URLSearchParams) {
  const shown = useSyncExternalStore(subscribeInlineDatabaseAddresses, () => {
    const peeked = peekedInlineDatabase(search);
    return peeked ? `${peeked.pageId}\u0000${peeked.blockId}` : "";
  }, () => "");
  if (!shown) return null;
  const [pageId, blockId] = shown.split("\u0000");
  return { pageId, blockId };
}

/**
 * What an address's peek shows while what it names is read as a database
 * page and isn't one: the server is asked where that ID opens, and an inline
 * database's page holding it is kept, its peek then showing it alone
 * (usePeekedInlineDatabase), as a peek's address loaded anew over another page
 * does (page_links.go answers its holder). Shows nothing.
 */
export function PeekedInlineDatabaseAsk({ session, id }: { session: Session; id: string }) {
  useEffect(() => {
    const controller = new AbortController();
    api.resolvePageLink(session, id, controller.signal).then(({ holderPageId }) => {
      if (!controller.signal.aborted && holderPageId) rememberInlineDatabaseAddress(id, holderPageId, true);
    }, () => undefined);
    return () => controller.abort();
  }, [id, session]);
  return null;
}
