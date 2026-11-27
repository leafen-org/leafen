import { useEffect, useMemo } from "react";
import type { Session } from "@/types";
import { notifyPageLinkCopied } from "./pageLinkCopyNotice";
import { useProductToast } from "./productToast";

/** Cancel feedback and its action when its page/session leaves the surface. */
export function usePageLinkCopyNotice(session: Session | undefined, pageId: string | undefined, openShare?: () => void) {
  const { notify } = useProductToast();
  const scope = useMemo(() => ({ active: true, pending: null as AbortController | null }), [session, pageId]);
  useEffect(() => {
    scope.active = true;
    return () => { scope.active = false; scope.pending?.abort(); };
  }, [scope]);
  return () => {
    if (!scope.active || !pageId) return;
    scope.pending?.abort();
    const controller = new AbortController();
    scope.pending = controller;
    void notifyPageLinkCopied(session, pageId, notify, openShare, controller.signal);
  };
}
