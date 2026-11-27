import { useEffect } from "react";
import { pageHref } from "@/lib/navigation";
import { pushRoute } from "@/lib/router";
import { joinShareLink, takeShareLinkJoin } from "@/lib/shareLinkJoin";
import type { Session } from "@/types";

/** A web link's join kept while the person signed in goes on once they have. */
export function usePendingShareLinkJoin(session: Session) {
  useEffect(() => {
    const pending = takeShareLinkJoin();
    if (!pending) return;
    void joinShareLink(session, pending).then(
      (joined) => pushRoute(pageHref(joined.pageId)),
      // Not the organization's, or the link changed: the shared page says so.
      () => pushRoute(`/share/${encodeURIComponent(pending.token)}`),
    );
  }, [session]);
}
