import { useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { setInteractionModality } from "react-aria";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ComposeIcon, FileBlank, Home, MailIcon, MenuIcon, TableIcon } from "@/icons/leafen";
import { shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import { onDesktopWindowEvent } from "@/lib/desktopMenuCommands";
import { inDesktopTab, retellDesktopSidebar } from "@/lib/desktopTabs";
import { SidebarActionMenu, type SidebarMenuRow } from "./SidebarActionMenu";
import { styles } from "./sidebarStyles";
import badge from "../CountBadge.module.css";
import { cappedCount } from "@/lib/countCap";

const SIDEBAR_TOP_ICON_SIZE = "regular";

/** What "새로" makes, each for those who may. */
export type SidebarNewChoices = {
  creating: boolean;
  onPage?: () => void;
  onDatabase?: () => void;
  /** A guest who may ask to become a member: "새로" opens no menu but the
   *  window that asks, its mark dimmed, as Notion's (seen 2026-11-12). */
  onAsk?: () => void;
};

/** The rows "새로" offers: a new page and database, those the reader may make. */
function sidebarNewRows(t: (key: string) => string, choices: SidebarNewChoices, close: (restoreFocus?: boolean) => void): SidebarMenuRow[] {
  const row = (key: string, label: string, icon: ReactNode, run?: () => void): SidebarMenuRow[] => (run ? [{
    key, label, icon, data: { "data-sidebar-new-choice": key }, run: () => { close(false); run(); },
  }] : []);
  return [
    ...row("page", t("newMenuPage"), <FileBlank size="regular" aria-hidden="true" />, choices.onPage),
    ...row("database", t("newMenuDatabase"), <TableIcon size="regular" aria-hidden="true" />, choices.onDatabase),
  ];
}

/**
 * "새로" and its menu, opened from a button the caller draws. At the
 * sidebar's top it is Notion's (seen 2026-10-31): 180px, 6px left of the
 * button, its first row "페이지" lit as it opens however it was opened, so
 * Enter makes a page and ↓ goes to the next (seen 2026-11-11 and 2026-11-13,
 * the desktop strip's too); Notion's "채팅" and "AI 노트" there are its AI's.
 */
export function useSidebarNewMenu(button: RefObject<HTMLButtonElement | null>, choices: SidebarNewChoices | undefined,
  place: { width: number; shift: number; highlightFirst?: boolean } = { width: 200, shift: 0 }) {
  const { t } = useTranslation("sidebar");
  const [open, setOpen] = useState(false);
  // Put away without a choice, the button takes the focus back, as Notion's
  // (2026-11-15, Chrome, no script before the action; judged by the screen
  // and one typed letter: pressed, then Escape — the button ringed, its tip
  // "신규..." shown, the letter landing nowhere).
  function close(restoreFocus = true) {
    setOpen(false);
    if (restoreFocus) window.requestAnimationFrame(() => button.current?.focus({ preventScroll: true }));
  }
  const ask = choices?.onAsk;
  const rows = choices && !ask ? sidebarNewRows(t, choices, close) : [];
  const menu = open && rows.length ? createPortal(<SidebarActionMenu button={button} label={t("newMenu")} width={place.width}
    shift={place.shift} highlightFirst={place.highlightFirst} backdropLabel={t("closeNewMenu")} close={close} rows={rows} />, document.body) : null;
  return {
    open, shown: rows.length > 0 || Boolean(ask), asks: Boolean(ask),
    // The button's popup: its menu, or the asking window.
    popup: ask ? "dialog" as const : "menu" as const,
    toggle: () => (ask ? ask() : open ? close() : setOpen(true)), menu,
  };
}

/**
 * The top of the sidebar, as Notion's new one (the new-sidebar decision of
 * 2026-10-31, seen that day): the sidebar's close on the left, the inbox with
 * its unread count and "새로" (a new page or database) on the right of a 28px
 * row, a search field with its keys under them (Notion's also asks its AI;
 * Leafen's searches), and home where Notion's tabs are (Leafen has none,
 * its one list). The workspace switcher is at the foot. A phone's drawer keeps the
 * switcher at its top, and search, home, the inbox and "새로" in its bottom
 * bar. In the Mac and Windows apps' windows the strip over the page draws
 * that row, as Notion's tab bar (seen 2026-11-11, its source read
 * 2026-11-13), and the search starts under it: the row stays unseen, its
 * buttons answering the strip's (desktop/tab_strip.js), "새로" moved to
 * where the strip's stands so its menu opens under that.
 */
export function SidebarTopActions({
  switcher,
  phone,
  homeActive,
  inboxActive,
  inboxDisabled,
  inboxUnread,
  newChoices,
  onOpenHome,
  onOpenInbox,
  onOpenSearch,
  onToggle,
}: {
  /** The workspace switcher, which heads a phone's drawer. */
  switcher: ReactNode;
  phone: boolean;
  homeActive: boolean;
  inboxActive: boolean;
  inboxDisabled: boolean;
  /** How many notifications are unread; the inbox's row counts them, as Notion's badge does. */
  inboxUnread: number;
  newChoices?: SidebarNewChoices;
  onOpenHome?: () => void;
  onOpenInbox: () => void;
  onOpenSearch: () => void;
  onToggle: () => void;
}) {
  const { t } = useTranslation("sidebar");
  const newButton = useRef<HTMLButtonElement>(null);
  const newMenu = useSidebarNewMenu(newButton, newChoices, { width: 180, shift: 6, highlightFirst: true });
  const newLabel = newChoices?.creating ? t("creating") : t("newMenu");
  const inboxLabel = inboxUnread > 0 ? t("inboxUnreadCount", { count: inboxUnread }) : t("inbox");
  const inStrip = !phone && inDesktopTab();
  // The strip shows the row's buttons as they are, and its presses do theirs.
  useEffect(() => {
    if (inStrip) retellDesktopSidebar();
  }, [inStrip, inboxActive, inboxDisabled, newMenu.open, newMenu.shown, newMenu.asks, newChoices?.creating]);
  const latest = useRef({ onOpenInbox, toggleNew: newMenu.toggle });
  latest.current = { onOpenInbox, toggleNew: newMenu.toggle };
  useEffect(() => {
    if (!inStrip) return undefined;
    return onDesktopWindowEvent("leafen:sidebar-head", (data) => {
      const asked = data as { action?: string; x?: number; y?: number } | null;
      // A press on the strip is a pointer's, as when its own button is pressed.
      setInteractionModality("pointer");
      if (asked?.action === "inbox") latest.current.onOpenInbox();
      if (asked?.action !== "new") return;
      const button = newButton.current;
      if (button && typeof asked.x === "number" && typeof asked.y === "number") {
        button.style.translate = "";
        const at = button.getBoundingClientRect();
        button.style.translate = `${Math.round(asked.x - at.left)}px ${Math.round(asked.y - at.top)}px`;
      }
      latest.current.toggleNew();
    });
  }, [inStrip]);
  return (
    <div className={styles.topArea} data-sidebar-top data-top-rows={phone || onOpenHome ? undefined : "none"}
      data-in-strip={inStrip ? "true" : undefined}>
      <nav className={styles.headerRow} aria-label={t("workspaceActions")} data-sidebar-top-actions data-in-strip={inStrip ? "true" : undefined}>
        {phone ? switcher : <button
          type="button"
          className={styles.headerIcon}
          data-sidebar-collapse-action
          onClick={onToggle}
          aria-label={t("closeSidebar")}
          title={t("closeSidebar")}
          data-tip-keys={shortcutCompactLabel("toggleSidebar")}
        >
          <MenuIcon size={SIDEBAR_TOP_ICON_SIZE} aria-hidden="true" />
        </button>}
        {phone ? null : <span className={styles.headerSpacer} />}
        {phone ? null : (
          <button type="button" className={styles.headerIcon} data-sidebar-icon-action data-sidebar-top-row="inbox"
            data-active={inboxActive ? "true" : undefined} aria-pressed={inboxActive} data-unread={inboxUnread > 0 ? "true" : undefined}
            disabled={inboxDisabled} onClick={onOpenInbox} aria-label={inboxLabel} title={t("inbox")}
            data-tip-keys={shortcutCompactLabel("openInbox")}>
            <MailIcon size={SIDEBAR_TOP_ICON_SIZE} aria-hidden="true" />
            {inboxUnread > 0 ? (
              <span className={styles.inboxUnreadBadge} data-inbox-badge aria-hidden="true">
                <span className={badge.digits}>{cappedCount(inboxUnread)}</span>
              </span>
            ) : null}
          </button>
        )}
        {!phone && newMenu.shown ? (
          <button ref={newButton} type="button" className={styles.headerIcon} data-sidebar-new-page disabled={newChoices?.creating}
            data-asks={newMenu.asks ? "true" : undefined} aria-haspopup={newMenu.popup}
            aria-expanded={newMenu.asks ? undefined : newMenu.open} onClick={newMenu.toggle} aria-label={newLabel}
            title={newChoices?.creating ? newLabel : t("newMenuTip")}>
            <ComposeIcon size={SIDEBAR_TOP_ICON_SIZE} aria-hidden="true" />
          </button>
        ) : null}
        {newMenu.menu}
      </nav>
      {phone ? null : (
        <button type="button" className={styles.searchField} data-sidebar-search data-sidebar-top-row="search" onClick={onOpenSearch}
          aria-label={t("quickFind")}>
          <span>{t("searchField")}</span>
          <kbd aria-hidden="true">{shortcutCompactLabel("quickFind")}</kbd>
        </button>
      )}
      {phone || !onOpenHome ? null : (
        <div className={styles.topRows} data-sidebar-top-rows>
          <button type="button" className={styles.topRow} data-sidebar-top-row="home" data-active={homeActive ? "true" : undefined}
            aria-current={homeActive ? "page" : undefined} onClick={onOpenHome}>
            <Home size="regular" aria-hidden="true" />
            <span>{t("home")}</span>
          </button>
        </div>
      )}
    </div>
  );
}
