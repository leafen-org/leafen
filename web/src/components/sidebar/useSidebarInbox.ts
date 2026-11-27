import { useCallback, useEffect, useState } from "react";
import { api } from "@/api";
import { useSidebarUnreadRefresh } from "@/components/useSidebarUnreadRefresh";
import {
  INBOX_WIDTH, inboxPinned, inboxWidth, setInboxDock, storeInboxPinned, storeInboxWidth,
} from "@/components/updates/inboxDock";
import { setDocumentUnread } from "@/lib/documentChrome";
import { matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";
import type { Session } from "@/types";

const SIDEBAR_UNREAD_BOUNDS = { maxLanes: 1 } as const;

/**
 * The sidebar's inbox: whether it is open (and still drawn while it slides
 * away), how many personal notifications are unread for the badge,
 * whether it is pinned beside the page and how wide it is.
 */
export function useSidebarInbox({ session, workspaceId, mobile, collapsed }: {
  session: Session;
  workspaceId: string | undefined;
  mobile: boolean;
  collapsed: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [rendered, setRendered] = useState(false);
  const [exiting, setExiting] = useState(false);
  const [unread, setUnread] = useState(0);
  const [pinned, setPinnedValue] = useState(() => inboxPinned(session.user.id));
  const [width, setWidthValue] = useState(() => inboxWidth(session.user.id));

  const readUnreadNotifications = useCallback(async ({ workspaceId: id }: { workspaceId: string }) => {
    const result = await api.listNotifications(session, id, 1, undefined, "personal");
    return { complete: result.complete, unreadCount: result.unreadCount };
  }, [session]);

  useSidebarUnreadRefresh({
    bounds: SIDEBAR_UNREAD_BOUNDS,
    onUnreadChange: setUnread,
    readNotifications: readUnreadNotifications,
    updatesOpen: open,
    workspaceId,
    shareKey: session.user.id,
  });

  useEffect(() => {
    setOpen(false);
    setUnread(0);
  }, [workspaceId]);

  // ⌘⌥U opens or closes it from anywhere, as Notion's tip names (seen 2026-10-28).
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (!matchesKeyboardShortcut("openInbox", event)) return;
      event.preventDefault();
      setOpen((current) => !current);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  // The window's title leads with the count, as Notion's.
  useEffect(() => { setDocumentUnread(unread); }, [unread]);
  useEffect(() => () => setDocumentUnread(0), []);

  useEffect(() => {
    if (open) {
      setRendered(true);
      setExiting(false);
    } else if (rendered) {
      setExiting(true);
    }
  }, [open, rendered]);

  useEffect(() => {
    if (!exiting) return;
    const reducedMotion = typeof window.matchMedia === "function"
      && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const timer = window.setTimeout(() => {
      setRendered(false);
      setExiting(false);
    }, reducedMotion ? 0 : 210);
    return () => window.clearTimeout(timer);
  }, [exiting]);

  // Pinned and open, the page moves over for it.
  const docked = pinned && open && !mobile && !collapsed;
  useEffect(() => {
    setInboxDock({ docked, width });
  }, [docked, width]);
  useEffect(() => () => setInboxDock({ docked: false, width: INBOX_WIDTH }), []);

  const setPinned = useCallback((next: boolean) => {
    storeInboxPinned(session.user.id, next);
    setPinnedValue(next);
  }, [session.user.id]);

  // Dragging changes it at once; the width is kept when the drag ends.
  const setWidth = useCallback((next: number, keep: boolean) => {
    setWidthValue(next);
    if (keep) storeInboxWidth(session.user.id, next);
  }, [session.user.id]);

  return { open, setOpen, rendered, exiting, unread, active: open || rendered, pinned, setPinned, width, setWidth };
}
