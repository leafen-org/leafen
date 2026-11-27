import { useTranslation } from "react-i18next";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { HamburgerIcon as List, MenuIcon as SidebarPanel } from "@/icons/leafen";
import { useDocumentUnread } from "@/lib/documentChrome";
import { inDesktopTab } from "@/lib/desktopTabs";
import { styles } from "./topBarStyles";
import { TopBarHistoryArrows } from "./TopBarHistoryArrows";
import badge from "../CountBadge.module.css";
import { cappedCount } from "@/lib/countCap";

/**
 * The bar's ☰, which opens and closes the sidebar, with the inbox's unread
 * count over it, as Notion's (seen 2026-10-27). Every bar in the app's frame
 * draws it in the same place — a page's, a database row opened as its own
 * page (Notion's, seen 2026-11-14 at 1440 with its sidebar folded: 24px at
 * 12, the path 6px after it), an app's — and it shows while the sidebar is
 * folded or on a phone. A screen outside the frame (a published page, a
 * share or view link) has no sidebar to open. In the Mac app the window's
 * strip of tabs has it instead, as Notion's app's tab bar
 * (desktop/tab_strip.js). A touch screen wider than a phone's draws the
 * sidebar's own panel mark there with no count, as Notion's iPad (seen
 * 2026-11-13 and 2026-11-16 in iPad Safari at 834 and 1194: "▭", the count
 * on the inbox's mark in the sidebar), and after it the ← and → of the
 * tab's way back and forward (TopBarHistoryArrows).
 */
export function TopBarSidebarToggle() {
  const { t } = useTranslation("topBar");
  const unread = useDocumentUnread();
  const { frame, phone, touch, sidebarCollapsed, sidebarOpen, toggleSidebarChrome } = useSidebarLayout(true);
  if (!frame || (inDesktopTab() && !phone)) return null;
  const panel = touch && !phone;
  return (
    <>
      <button
        type="button"
        className={styles.hamburger}
        data-topbar-sidebar-toggle
        data-desktop-visible={sidebarCollapsed ? "true" : undefined}
        data-panel-mark={panel ? "true" : undefined}
        onClick={toggleSidebarChrome}
        aria-label={t(sidebarCollapsed || !sidebarOpen ? "openSidebar" : "closeSidebar")}
        aria-expanded={sidebarCollapsed ? false : sidebarOpen}
      >
        {panel ? <SidebarPanel size="regular" aria-hidden="true" /> : <List size="regular" aria-hidden="true" />}
        {unread > 0 && !panel ? (
          <span className={styles.hamburgerBadge} data-topbar-unread aria-hidden="true">
            <span className={badge.digits}>{cappedCount(unread)}</span>
          </span>
        ) : null}
      </button>
      {panel ? <TopBarHistoryArrows /> : null}
    </>
  );
}
