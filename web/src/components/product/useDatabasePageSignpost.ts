import { useEffect, useState } from "react";
import { useHeldSignIn } from "@/lib/heldSignIn";
import type { Page, Session } from "@/types";
import { askPageLink, followPageLink } from "./usePageLinkRoute";

/**
 * The page a full-page database left as it turned inline leads to the
 * database, as in Notion its ID stays the database's: opening it opens the
 * database where it is now, as its address does — alone while it is inline,
 * its page once it is a page again (Notion's, seen 2026-11-13) — the view
 * the address names with it. Reports whether the page is leading there, so
 * it isn't shown meanwhile; a page whose database can't be reached shows as
 * it is. It is asked once for the person, as an address is
 * (usePageLinkRoute): their sign-in handed over or renewed asks nothing again.
 */
export function useDatabasePageSignpost(session: Session, page: Page | null) {
  const signpostId = page?.inTrash && page.movedDatabase ? page.id : null;
  const [unreachable, setUnreachable] = useState<string | null>(null);
  const signIn = useHeldSignIn(session);
  useEffect(() => {
    if (!signpostId) return;
    const controller = new AbortController();
    askPageLink(signIn(), signpostId, controller.signal).then((answer) => {
      if (!controller.signal.aborted && !followPageLink(answer)) setUnreachable(signpostId);
    }, () => {
      if (!controller.signal.aborted) setUnreachable(signpostId);
    });
    return () => controller.abort();
  }, [signIn, signpostId]);
  return signpostId !== null && unreachable !== signpostId;
}
