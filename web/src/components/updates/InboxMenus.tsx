import { useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import {
  ArchiveIcon, AtIcon, CheckIcon, ClockIcon, InboxDiscussionsIcon, InboxPermissionsIcon, LightningIcon, MailIcon,
  SparkleBurstIcon, TeamspaceIcon, MarkUnreadIcon,
} from "@/icons/leafen";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheetMenu } from "@/components/shell/PhoneSheet";
import type { PageNotificationLevel } from "@/notificationSettingsApi";
import type { Session } from "@/types";
import { INBOX_STATES, INBOX_TYPES, type InboxState, type InboxType } from "./updatesPanelModel";
import styles from "../UpdatesPanelChrome.module.css";

/** A menu below one of the inbox's buttons, over the window; on a phone a short list at the screen's bottom, as Notion's. */
function InboxMenu({ anchor, label, width, onClose, children }: {
  anchor: HTMLElement;
  label: string;
  width: number;
  onClose: () => void;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const { phone } = useSidebarLayout(true);
  const place = usePopoverPlacement(ref, (size, viewport) => (
    // 2px under its button, as Notion's filter and "⋯" (seen 2026-11-13).
    placeAround(popoverRectOf(anchor)!, size, viewport, { align: "start", gap: 2 })
  ), [anchor], undefined, { kind: "menu", inside: () => [anchor], onEscape: onClose, onOutsidePress: onClose, initialFocus: "first" });
  if (phone) return <PhoneSheetMenu label={label} onClose={onClose} className={styles.phoneMenu}>{children}</PhoneSheetMenu>;
  return createPortal(
    <div ref={ref} className={styles.menu} role="menu" aria-label={label} data-inbox-menu
      style={{ position: "fixed", width, ...popoverPlacementStyle(place) }}>
      {children}
    </div>,
    document.body,
  );
}

const STATE_ICONS: Record<InboxState, ReactNode> = {
  inbox: <MailIcon size="regular" aria-hidden="true" />,
  unread: <MarkUnreadIcon size="regular" aria-hidden="true" />,
  archived: <ArchiveIcon size="regular" aria-hidden="true" />,
  workspace: <ClockIcon size="regular" aria-hidden="true" />,
};

const TYPE_ICONS: Record<InboxType, ReactNode> = {
  permissions: <InboxPermissionsIcon size="regular" aria-hidden="true" />,
  discussions: <InboxDiscussionsIcon size="regular" aria-hidden="true" />,
  mentions: <AtIcon size="regular" aria-hidden="true" />,
  automations: <LightningIcon size="regular" aria-hidden="true" />,
  updates: <SparkleBurstIcon size="regular" aria-hidden="true" />,
  workspace: <TeamspaceIcon size="regular" aria-hidden="true" />,
};

/**
 * "알림 필터링", as Notion's (250px, seen 2026-10-07): under "상태" which
 * notifications the inbox lists, under "유형" the kinds it shows. Choosing
 * closes the state menu; workspace activity has no notification types.
 */
export function InboxFilterMenu({ anchor, state, hidden, onState, onToggleType, onClose }: {
  anchor: HTMLElement;
  state: InboxState;
  hidden: ReadonlySet<InboxType>;
  onState: (state: InboxState) => void;
  onToggleType: (type: InboxType) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("updatesPanel");
  return (
    <InboxMenu anchor={anchor} label={t("inbox.filter")} width={250} onClose={onClose}>
      <div className={styles.menuHeading}>{t("inbox.stateHeading")}</div>
      {INBOX_STATES.map((value) => (
        <button key={value} type="button" role="menuitemradio" aria-checked={state === value} className={styles.menuItem}
          data-inbox-state={value} onClick={() => { onState(value); onClose(); }}>
          <span className={styles.menuRadio} aria-hidden="true" />
          {STATE_ICONS[value]}
          <span>{t(`inbox.states.${value}`)}</span>
        </button>
      ))}
      {state === "workspace" ? null : <>
      <div className={`${styles.menuDivider} ${styles.filterDivider}`} role="separator" />
      <div className={styles.menuHeading}>{t("inbox.typeHeading")}</div>
      {INBOX_TYPES.map((value) => (
        <button key={value} type="button" role="menuitemcheckbox" aria-checked={!hidden.has(value)} className={styles.menuItem}
          data-inbox-type={value} onClick={() => onToggleType(value)}>
          <span className={styles.menuCheckbox} aria-hidden="true" />
          {TYPE_ICONS[value]}
          <span>{t(`inbox.types.${value}`)}</span>
        </button>
      ))}
      </>}
    </InboxMenu>
  );
}

/** "추가 작업": mark everything read, or archive everything. */
export function InboxMoreMenu({ anchor, canMarkRead, canArchive, onMarkAllRead, onArchiveAll, onClose }: {
  anchor: HTMLElement;
  canMarkRead: boolean;
  canArchive: boolean;
  onMarkAllRead: () => void;
  onArchiveAll: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("updatesPanel");
  return (
    <InboxMenu anchor={anchor} label={t("inbox.more")} width={250} onClose={onClose}>
      <button type="button" role="menuitem" className={styles.menuItem} disabled={!canMarkRead} data-inbox-mark-all-read
        onClick={() => { onClose(); onMarkAllRead(); }}>
        <CheckIcon size="regular" aria-hidden="true" />
        <span>{t("inbox.markAllRead")}</span>
      </button>
      <button type="button" role="menuitem" className={styles.menuItem} disabled={!canArchive} data-inbox-archive-all
        onClick={() => { onClose(); onArchiveAll(); }}>
        <ArchiveIcon size="regular" aria-hidden="true" />
        <span>{t("inbox.archiveAll")}</span>
      </button>
    </InboxMenu>
  );
}

const PAGE_LEVELS: PageNotificationLevel[] = ["all_comments", "replies_and_mentions"];

/**
 * "페이지 알림 설정 변경" on a notification: which comments on its page
 * notify the reader, as the page menu's "알림 받기".
 */
export function InboxPageNotifyMenu({ anchor, pageId, session, onClose }: {
  anchor: HTMLElement;
  pageId: string;
  session: Session;
  onClose: () => void;
}) {
  const { t } = useTranslation(["updatesPanel", "rowMenu"]);
  const [level, setLevel] = useState<PageNotificationLevel | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    api.readPageNotificationPreference(session, pageId, controller.signal)
      .then((preference) => setLevel(preference.level))
      .catch(() => { if (!controller.signal.aborted) setFailed(true); });
    return () => controller.abort();
  }, [pageId, session]);

  function choose(next: PageNotificationLevel) {
    const before = level;
    setLevel(next);
    api.savePageNotificationPreference(session, pageId, next).then(onClose, () => {
      setLevel(before);
      setFailed(true);
    });
  }

  return (
    <InboxMenu anchor={anchor} label={t("updatesPanel:inbox.pageSettings")} width={300} onClose={onClose}>
      {PAGE_LEVELS.map((value) => (
        <button key={value} type="button" role="menuitemradio" aria-checked={level === value} className={styles.menuItem}
          disabled={level === null && !failed} data-inbox-page-notify={value} onClick={() => choose(value)}>
          <span>{t(value === "all_comments" ? "rowMenu:notify.allComments" : "rowMenu:notify.repliesAndMentions")}</span>
          {level === value ? <CheckIcon size="small" className={styles.menuCheck} aria-hidden="true" /> : null}
        </button>
      ))}
      {failed ? <p className={styles.menuNote} role="alert" data-error>{t("updatesPanel:inbox.pageSettingsFailed")}</p> : null}
    </InboxMenu>
  );
}
