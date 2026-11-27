import { useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { CheckboxIcon, LibraryIcon, MoreAppsIcon, ServerAppsIcon, Settings, SharePeopleIcon, Trash } from "@/icons/leafen";
import { useTranslation } from "react-i18next";
import type { Session, Workspace } from "@/types";
import type { SettingsSection } from "@/components/settings/settingsWindowModel";
import { SidebarActionMenu, type SidebarMenuRow } from "./SidebarActionMenu";
import { SidebarCardSlot } from "./SidebarCardSlot";
import { styles } from "./sidebarStyles";

/** The places the list's foot opens, each where the reader may go. */
export type SidebarFootPlaces = {
  onOpenLibrary?: () => void;
  onOpenApps?: () => void;
  onOpenTrash?: () => void;
  onOpenSettings: () => void;
};

/**
 * The places a phone's list ends with, in Notion's phone drawer's order (seen
 * 2026-11-13 in iPhone Safari: 이전 홈, 라이브러리, 멤버, 알림과 설정, 요금제,
 * 청구, 휴지통, 도움말 및 지원): the library, Leafen's apps after it, the
 * members, "알림과 설정" and the trash. Home is the bar's under it; plans,
 * billing and help Leafen has none of.
 */
export function sidebarFootItems(t: (key: string) => string, places: SidebarFootPlaces & { onOpenMembers?: () => void }) {
  return [
    places.onOpenLibrary ? { key: "library", label: t("library"), Icon: LibraryIcon, run: places.onOpenLibrary } : null,
    places.onOpenApps ? { key: "apps", label: t("appsPlace"), Icon: ServerAppsIcon, run: places.onOpenApps } : null,
    places.onOpenMembers ? { key: "members", label: t("phoneMembers"), Icon: SharePeopleIcon, run: places.onOpenMembers } : null,
    { key: "settings", label: t("phoneSettings"), Icon: Settings, run: places.onOpenSettings },
    places.onOpenTrash ? { key: "trash", label: t("trash"), Icon: Trash, run: places.onOpenTrash } : null,
  ].filter((item) => item !== null);
}

/**
 * The foot of the sidebar, as Notion's new one (the new-sidebar decision of
 * 2026-10-31, seen that day): the workspace switcher, whose menu holds the
 * settings, and on the right "더 보기", whose menu opens above it with the
 * library, the apps (Leafen's own), my tasks and the trash, its first row
 * lit as it opens however it was opened (seen 2026-11-11 and 2026-11-13: Enter
 * opens the library, ↓ goes to the next). Notion's also
 * holds its AI notes, skills and marketplace; its help button sits
 * between them, and Leafen has none. A guest's right is the library's own
 * button instead, as Notion's guest foot (seen 2026-11-12). A member's foot
 * has the sidebar's card place over it (SidebarCardSlot: Notion's "멤버
 * 초대" card, and Leafen's own cards before it), and the foot's
 * line is Notion's: 1px of its secondary border over a 44px row (seen
 * 2026-11-22, the switcher where it stood).
 */
export function SidebarFooter({ switcher, places, guest = false, workspace = null, session, onInviteMembers, onOpenSettings }: {
  switcher: ReactNode;
  places: SidebarFootPlaces & { onOpenMyTasks?: () => void };
  guest?: boolean;
  workspace?: Workspace | null;
  session?: Session;
  onInviteMembers?: () => void;
  onOpenSettings?: (section: SettingsSection) => void;
}) {
  const { t } = useTranslation("sidebar");
  const button = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  // Put away without a choice, the button takes the focus back, as Notion's
  // (2026-11-15, Chrome, no script before the action; judged by the screen
  // and one typed letter: pressed, then Escape — the button ringed, its tip
  // shown, the letter landing nowhere).
  function close(restoreFocus = true) {
    setOpen(false);
    if (restoreFocus) window.requestAnimationFrame(() => button.current?.focus({ preventScroll: true }));
  }
  // The trash stands over the button, and gives the focus back to it as it closes — ringed with its tip after
  // Escape, ringless after a press on the page until a key comes — as Notion's (seen 2026-11-26 in Chrome at 1440,
  // two tabs at different times, judged after the action): the button takes it back first, ringless, as its menu
  // closes, so the trash opens from it.
  const row = (key: string, label: string, Icon: typeof Trash, run?: () => void, over = false): SidebarMenuRow[] => (run ? [{
    key, label, icon: <Icon size="regular" aria-hidden="true" />, data: { "data-sidebar-more-item": key }, run: () => {
      close(false);
      if (over) button.current?.focus({ preventScroll: true, focusVisible: false } as FocusOptions);
      run();
    },
  }] : []);
  const rows = [
    ...row("library", t("library"), LibraryIcon, places.onOpenLibrary),
    ...row("apps", t("appsPlace"), ServerAppsIcon, places.onOpenApps),
    ...row("my-tasks", t("myTasks"), CheckboxIcon, places.onOpenMyTasks),
    ...row("trash", t("trash"), Trash, places.onOpenTrash, true),
  ];
  if (guest) {
    const openLibrary = places.onOpenLibrary;
    return (
      <div className={styles.footerRow} data-sidebar-footer>
        {switcher}
        {openLibrary ? (
          <button type="button" className={styles.headerIcon} data-sidebar-library aria-label={t("library")} title={t("library")}
            onClick={openLibrary}>
            <LibraryIcon size="regular" aria-hidden="true" />
          </button>
        ) : null}
      </div>
    );
  }
  return (<>
    {session && onInviteMembers && onOpenSettings
      ? <SidebarCardSlot session={session} workspace={workspace} onInvite={onInviteMembers} onOpenSettings={onOpenSettings} /> : null}
    <div className={styles.footerRow} data-sidebar-footer>
      {switcher}
      {rows.length ? (
        <button ref={button} type="button" className={styles.headerIcon} data-sidebar-more aria-haspopup="menu" aria-expanded={open}
          aria-label={t("moreMenuTip")} title={t("moreMenuTip")} onClick={() => (open ? close() : setOpen(true))}>
          <MoreAppsIcon size="regular" aria-hidden="true" />
        </button>
      ) : null}
      {open && rows.length ? createPortal(<SidebarActionMenu button={button} label={t("moreMenu")} width={240}
        backdropLabel={t("closeMoreMenu")} close={close} rows={rows} shift={8} gap={6} highlightFirst />, document.body) : null}
    </div>
  </>);
}
