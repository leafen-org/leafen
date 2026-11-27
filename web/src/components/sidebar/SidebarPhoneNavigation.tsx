import { useRef } from "react";
import { useTranslation } from "react-i18next";
import { ComposeIcon, Home, MailIcon, Plus, Search } from "@/icons/leafen";
import { sidebarFootItems, type SidebarFootPlaces } from "./SidebarFooter";
import { useSidebarNewMenu, type SidebarNewChoices } from "./SidebarTopActions";
import { styles } from "./sidebarStyles";
import badge from "../CountBadge.module.css";
import { PhonePageFoot } from "../shell/PhoneFoot";
import { cappedCount } from "@/lib/countCap";

/** A phone's list ends with the places as its rows: the library, the apps, the members, the settings and the trash. */
export function SidebarPhoneNavigation(places: SidebarFootPlaces & { onOpenMembers?: () => void }) {
  const { t } = useTranslation("sidebar");
  return <nav className={styles.phoneNavigation} data-sidebar-phone-navigation>
    {sidebarFootItems(t, places).map(({ key, label, Icon, run }) => <button type="button" key={key} data-sidebar-phone-item={key}
      onClick={run}><Icon size="regular" aria-hidden="true" /><span>{label}</span></button>)}
  </nav>;
}

/**
 * A phone drawer's bottom bar (the one-sidebar decision of 2026-10-29, 10):
 * home, the inbox with its unread count, search and "+", which opens "새로",
 * as Notion's phone app keeps home, search, the inbox and a new page under
 * the thumb. With the drawer shut (`pageFoot`), a page has the app's foot
 * of search and a new page over it (PhonePageFoot), the inbox's own.
 */
export function SidebarPhoneTabBar({ inboxUnread, inboxActive, newChoices, onOpenHome, onOpenInbox, onOpenSearch, pageFoot }: {
  inboxUnread: number;
  inboxActive: boolean;
  pageFoot: boolean;
  newChoices?: SidebarNewChoices;
  onOpenHome?: () => void;
  onOpenInbox: () => void;
  onOpenSearch: () => void;
}) {
  const { t } = useTranslation("sidebar");
  const newButton = useRef<HTMLButtonElement>(null);
  const newMenu = useSidebarNewMenu(newButton, newChoices);
  return (
    <nav className={styles.phoneTabBar} aria-label={t("phoneTabBar")} data-sidebar-phone-tabs>
      {onOpenHome ? <button type="button" data-sidebar-phone-tab="home" onClick={onOpenHome}>
        <Home size="regular" aria-hidden="true" /><span>{t("home")}</span>
      </button> : null}
      <button type="button" data-sidebar-phone-tab="inbox" aria-pressed={inboxActive} onClick={onOpenInbox}
        aria-label={inboxUnread > 0 ? t("inboxUnreadCount", { count: inboxUnread }) : undefined}>
        <MailIcon size="regular" aria-hidden="true" /><span>{t("inbox")}</span>
        {inboxUnread > 0 ? <span className={styles.phoneTabCount} data-inbox-badge aria-hidden="true">
          <span className={badge.digits}>{cappedCount(inboxUnread)}</span>
        </span> : null}
      </button>
      <button type="button" data-sidebar-phone-tab="search" onClick={onOpenSearch}>
        <Search size="regular" aria-hidden="true" /><span>{t("searchField")}</span>
      </button>
      {newMenu.shown ? <button ref={newButton} type="button" className={styles.phoneTabNew} data-sidebar-phone-tab="new"
        aria-label={t("newMenu")} aria-haspopup={newMenu.popup} aria-expanded={newMenu.asks ? undefined : newMenu.open} onClick={newMenu.toggle}>
        <Plus size="regular" aria-hidden="true" />
      </button> : null}
      {newMenu.menu}
      {pageFoot ? <PhonePageFoot><SidebarPhoneFoot newChoices={newChoices} onOpenSearch={onOpenSearch} /></PhonePageFoot> : null}
    </nav>
  );
}

/**
 * A phone's foot, the inbox's and a page's alike (PhoneFoot), in the Notion
 * Android app's frame: search and a new page, round, at its two ends (the
 * app's "AI에게 질문하기" between them is left out). The new page's
 * button opens "새로" as the drawer's "+" does.
 */
export function SidebarPhoneFoot({ newChoices, onOpenSearch }: { newChoices?: SidebarNewChoices; onOpenSearch: () => void }) {
  const { t } = useTranslation("sidebar");
  const newButton = useRef<HTMLButtonElement>(null);
  const newMenu = useSidebarNewMenu(newButton, newChoices);
  return <>
    <button type="button" data-phone-foot-search aria-label={t("searchField")} title={t("searchField")} onClick={onOpenSearch}>
      <Search size="regular" aria-hidden="true" />
    </button>
    {newMenu.shown ? <button ref={newButton} type="button" data-phone-foot-new aria-label={t("newMenu")} title={t("newMenu")}
      aria-haspopup={newMenu.popup} aria-expanded={newMenu.asks ? undefined : newMenu.open} onClick={newMenu.toggle}>
      <ComposeIcon size="regular" aria-hidden="true" />
    </button> : null}
    {newMenu.menu}
  </>;
}
