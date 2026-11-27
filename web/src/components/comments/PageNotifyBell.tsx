import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { ReadFailed } from "@/components/ReadFailed";
import { Bell, CheckIcon, ChevronDown } from "@/icons/leafen";
import type { PageNotificationLevel } from "@/notificationSettingsApi";
import type { Session } from "@/types";
import styles from "./PageDiscussions.module.css";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";

const LEVELS: readonly PageNotificationLevel[] = ["all_comments", "replies_and_mentions"];

/**
 * The bell beside a page's discussions, as Notion's "페이지 알림 설정" (seen
 * 2026-09-30): the page's comments reach the person all of them, or only
 * replies and mentions. The page menu's "알림 받기" sets the same preference.
 * A touch screen's page updates show it as a field naming the choice, its
 * menu across the field's width just under it (`field`, Notion's, measured
 * 2026-11-15 at 402pt: 28px rows of 14px words in its 4px-padded menu).
 */
export function PageNotifyBell({ pageId, session, onFailure, field }: {
  pageId: string;
  session: Session;
  onFailure?: (message: string) => void;
  /** Drawn as a field with the choice in it, its label's id naming it. */
  field?: { labelledBy: string; className: string; menuClassName: string };
}) {
  const { t } = useTranslation(["commentsPanel", "rowMenu"]);
  const [open, setOpen] = useState(false);
  const [level, setLevel] = useState<PageNotificationLevel | null>(null);
  const [readFailed, setReadFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const host = useRef<HTMLSpanElement>(null);

  useEffect(() => {
    if (!open && !field) return;
    const controller = new AbortController();
    setReadFailed(false);
    void api.readPageNotificationPreference(session, pageId, controller.signal).then((preference) => {
      if (!controller.signal.aborted) setLevel(preference.level);
    }).catch(() => { if (!controller.signal.aborted) setReadFailed(true); });
    return () => controller.abort();
  }, [attempt, open, pageId, session, field]);

  const menuRef = useRef<HTMLDivElement>(null);
  usePopoverAppear(open, menuRef, () => host.current);
  const { zIndex, onKeyDown } = useOverlayLayer(open, { kind: "menu", ref: menuRef, onEscape: () => setOpen(false),
    onOutsidePress: () => setOpen(false), inside: () => [host.current] });
  const menu = useNativeMenu({ items: [...LEVELS], open, menuRef, label: t("commentsPanel:discussions.notifications"),
    keyOf: item => item, textOf: item => t(item === "all_comments" ? "rowMenu:notify.allComments" : "rowMenu:notify.repliesAndMentions"),
    disabledKeys: level ? [] : [...LEVELS], firstKey: level ?? undefined, onAction: next => { void choose(next); } });

  async function choose(next: PageNotificationLevel) {
    setOpen(false);
    if (next === level) return;
    try {
      setLevel((await api.savePageNotificationPreference(session, pageId, next)).level);
    } catch {
      onFailure?.(t("rowMenu:toast.couldntUpdateNotifications"));
    }
  }

  return (
    <span ref={host} className={styles.menuHost} data-field={field ? "true" : undefined}>
      {field ? (
        <button type="button" className={field.className} aria-haspopup="menu" aria-expanded={open} aria-labelledby={field.labelledBy}
          data-page-notify-field onClick={() => setOpen((current) => !current)}>
          <Bell size="small" aria-hidden="true" />
          <span>{level ? t(level === "all_comments" ? "rowMenu:notify.allComments" : "rowMenu:notify.repliesAndMentions") : ""}</span>
          <ChevronDown size="small" aria-hidden="true" />
        </button>
      ) : (
        <button type="button" className={styles.iconButton} aria-haspopup="menu" aria-expanded={open}
          aria-label={t("commentsPanel:discussions.notifications")} title={t("commentsPanel:discussions.notifications")}
          data-page-notify-bell onClick={() => setOpen((current) => !current)}>
          <Bell size="regular" aria-hidden="true" />
        </button>
      )}
      {open ? (
        <div {...menu.menuProps} ref={menuRef} className={`${styles.menu} ${field?.menuClassName ?? ""}`} style={{ zIndex }}
          onKeyDown={event => { menu.menuProps.onKeyDown?.(event); onKeyDown(event); }}>
          {field ? null : <div className={styles.menuLabel}>{t("commentsPanel:discussions.notifications")}</div>}
          {readFailed && !level ? (
            <div className={styles.menuLabel}>
              <ReadFailed message={t("rowMenu:notify.loadFailed")} onRetry={() => setAttempt((current) => current + 1)} />
            </div>
          ) : null}
          {LEVELS.map((choice) => (
            <NativeMenuItem key={choice} itemKey={choice} state={menu.state} className={styles.menuItem}
              role="menuitemradio" aria-checked={level === choice} disabled={!level}>{({ labelProps }) => <>
              <span {...labelProps}>{t(choice === "all_comments" ? "rowMenu:notify.allComments" : "rowMenu:notify.repliesAndMentions")}</span>
              {level === choice ? <span className={styles.menuCheck} aria-hidden="true"><CheckIcon size="small" /></span> : null}
            </>}</NativeMenuItem>
          ))}
        </div>
      ) : null}
    </span>
  );
}
