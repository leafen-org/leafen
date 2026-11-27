import { useEffect, useRef } from "react";
import { ROUTE_CHANGE_EVENT, type RouteMove } from "@/lib/router";

/**
 * A phone's search opens over what is there — the drawer, the inbox in it, a
 * page — and its "취소하기" or shade goes back there, the drawer and the
 * inbox open as they were, as the Notion Android app's search goes back where
 * it was opened (the user's hand test 2026-11-27 on a Galaxy Fold's outer
 * screen; phones follow the app, the phone basis decision of 2026-11-21 — no
 * longer iPhone Safari's, whose "취소하기" shut the drawer, seen 2026-11-14).
 *
 * What a result opens takes them along: the inbox stands in the drawer, and
 * goes as the drawer does when the app moves to another place (AppShell: a new
 * address, or a press to the one shown), rather than staying open over the
 * page opened, its foot's search and new page hidden under it. Search closes
 * before or after the place it opens moves, so the move, not the close, says so.
 */
export function usePhoneInboxGoesWithDrawer(phone: boolean, pathname: string, closeInbox: () => void) {
  const close = useRef(closeInbox);
  close.current = closeInbox;
  const at = useRef(pathname);
  useEffect(() => {
    if (at.current === pathname) return;
    at.current = pathname;
    if (phone) close.current();
  }, [phone, pathname]);
  useEffect(() => {
    if (!phone) return undefined;
    const moved = (event: Event) => {
      if ((event as CustomEvent<RouteMove>).detail?.replace !== true) close.current();
    };
    window.addEventListener(ROUTE_CHANGE_EVENT, moved);
    return () => window.removeEventListener(ROUTE_CHANGE_EVENT, moved);
  }, [phone]);
}
