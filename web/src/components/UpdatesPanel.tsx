"use client";

import {
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import {
  ArchiveIcon, DotsHorizontal, DoubleChevronLeft, FilterIcon, InboxFilterCircleIcon, MailIcon, MarkReadIcon, PinIcon, PinSlashIcon,
} from "@/icons/leafen";
import { deadlineNoticeIdOf } from "@/lib/deadlines";
import { useWeekStartDay } from "@/lib/displayPreferences";
import { pageHref } from "@/lib/navigation";
import { pushRoute } from "@/lib/router";
import { standaloneConsent } from "@/lib/standaloneConsent";
import { unansweredReadText } from "@/lib/unansweredRead";
import { escapeLayerOver, useOverlayLayer } from "@/lib/overlayLayer";
import type { Page, Session } from "@/types";
import chrome from "./UpdatesPanelChrome.module.css";
import styles from "./UpdatesPanelFeed.module.css";
import { InboxFilterMenu, InboxMoreMenu, InboxPageNotifyMenu } from "./updates/InboxMenus";
import { InboxRow } from "./updates/InboxRow";
import { InboxPhoneBar, InboxPhoneEmpty, InboxPhoneFoot } from "./updates/InboxPhoneFrame";
import { askPageDiscussions } from "./comments/commentPanelEvents";
import { InboxResizeHandle } from "./updates/InboxResizeHandle";
import { INBOX_WIDTH } from "./updates/inboxDock";
import {
  accessRequestIdOf,
  INBOX_TYPES,
  inboxDiscussionsPage,
  inboxOpenHref,
  actorProfilesById,
  filterInboxTypes,
  groupNotificationActivities,
  inboxSection,
  notificationActivities,
  readInboxFilter,
  storeInboxFilter,
  type InboxState,
  type InboxType,
  type NotificationActivity,
} from "./updates/updatesPanelModel";
import { useUpdatesPanelController } from "./updates/useUpdatesPanelController";
import { useAccessRequestDecisions } from "./updates/useAccessRequestDecisions";
import { useDeadlineAcknowledgements } from "./updates/useDeadlineAcknowledgements";
import { useWorkspaceActivityController } from "./updates/useWorkspaceActivityController";
import { WorkspaceActivityFeed } from "./updates/WorkspaceActivityFeed";
import { focusedOpener } from "@/lib/lastPress";
import { giveFocusBack } from "@/lib/focusGiveBack";

export type UpdatesPanelProps = {
  exiting?: boolean;
  /** Where the sidebar ends, beside which the inbox opens. */
  left?: number;
  onClose: () => void;
  /** A phone's foot: search and a new page, as the sidebar makes them (InboxPhoneFrame.tsx). */
  phoneFoot?: ReactNode;
  /** Docked beside the page, or lying over it; null where it cannot pin. */
  pinned?: boolean | null;
  onPinnedChange?: (pinned: boolean) => void;
  pages: Page[];
  /** Beside the sidebar, or in its column on a phone. */
  /** "phone": a touch phone's whole screen, in the Notion Android app's frame (InboxPhoneFrame.tsx). */
  placement?: "sidebar" | "sidebar-inline" | "phone";
  /**
   * A touch screen wider than a phone's, beside the sidebar: Notion's iPad
   * head, its ringed filter and "…" with no pin or close (seen 2026-11-16 in
   * iPad Safari at 834); a press outside or the sidebar's inbox closes it.
   */
  touch?: boolean;
  session: Session;
  /** Beside the sidebar, how wide; its edge drags it wider or narrower. */
  width?: number;
  onWidthChange?: (width: number, keep: boolean) => void;
  workspaceId: string;
  /** The workspace's icon, which a notice about the workspace shows, and a phone's top bar. */
  workspaceIcon?: string;
  workspaceName?: string;
};

const EMPTY_ICONS: Record<InboxState, typeof MailIcon> = {
  inbox: MailIcon, unread: MarkReadIcon, archived: ArchiveIcon, workspace: MailIcon,
};

/**
 * The inbox, as Notion's (seen 2026-10-07): a 390px panel beside the
 * sidebar, its title and pin, close, filter and more buttons on top, then
 * this week's notifications and the earlier ones. Pinned, it docks beside
 * the page; otherwise it lies over it and a press outside closes it.
 */
export function UpdatesPanel({
  exiting = false,
  left = 0,
  onClose,
  phoneFoot,
  pinned = null,
  onPinnedChange,
  pages,
  placement = "sidebar",
  touch = false,
  session,
  width = INBOX_WIDTH,
  onWidthChange,
  workspaceId,
  workspaceIcon,
  workspaceName = "",
}: UpdatesPanelProps) {
  const { t } = useTranslation(["updatesPanel", "common"]);
  const inline = placement === "sidebar-inline";
  const sheet = placement === "phone";
  const wideTouch = touch && placement === "sidebar";
  const docked = !inline && pinned === true;
  const titleId = useId();
  const panelRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  // What it lists and hides stay as last set, between openings and reloads.
  const [state, setStateValue] = useState<InboxState>(() => readInboxFilter(session.user.id).state);
  const [hidden, setHiddenValue] = useState<ReadonlySet<InboxType>>(() => readInboxFilter(session.user.id).hidden);
  const [menu, setMenu] = useState<{ kind: "filter" | "more"; anchor: HTMLElement } | null>(null);
  const [pageMenu, setPageMenu] = useState<{ anchor: HTMLElement; pageId: string } | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const weekStart = useWeekStartDay();
  const controller = useUpdatesPanelController({ pages, session, state, workspaceId });
  const workspaceActivity = useWorkspaceActivityController({ enabled: state === "workspace", pages, session, workspaceId });
  const activities = useMemo(
    () => notificationActivities(controller.snapshot.items, pages, session.user.id),
    [controller.snapshot.items, pages, session.user.id],
  );
  const requestIds = useMemo(
    () => activities.map(({ notification }) => accessRequestIdOf(notification)).filter(Boolean),
    [activities],
  );
  const requests = useAccessRequestDecisions({ requestIds, session, workspaceId });
  const noticeIds = useMemo(
    () => activities.map(({ notification }) => deadlineNoticeIdOf(notification.activityKey)).filter(Boolean),
    [activities],
  );
  const deadlines = useDeadlineAcknowledgements(noticeIds);
  const profiles = useMemo(
    () => actorProfilesById(controller.snapshot.actorProfiles),
    [controller.snapshot.actorProfiles],
  );
  const visible = useMemo(() => filterInboxTypes(activities, hidden), [activities, hidden]);
  const groups = useMemo(
    () => groupNotificationActivities(visible, (at) => inboxSection(at, now, weekStart)),
    [now, visible, weekStart],
  );
  const loading = controller.loading && activities.length === 0;
  // The tab reads on as its end scrolls into view.
  const moreRef = useRef<HTMLDivElement>(null);
  const { hasMore } = controller.snapshot;
  const { loadingMore, loadMore } = controller;
  useEffect(() => {
    if (!moreRef.current || !hasMore || loadingMore) return;
    const observer = new IntersectionObserver(([entry]) => { if (entry?.isIntersecting) void loadMore(); },
      { root: moreRef.current.closest("[data-inbox-list]"), rootMargin: "160px" });
    observer.observe(moreRef.current);
    return () => observer.disconnect();
  }, [hasMore, loadingMore, loadMore]);
  const filtered = state !== "inbox" || hidden.size > 0;
  // Every type turned off: the list says so and offers them all back, and
  // "⋯" goes, as Notion's (seen 2026-11-13, in each of its three states).
  const noTypes = state !== "workspace" && INBOX_TYPES.every((type) => hidden.has(type));

  function setState(next: InboxState) {
    storeInboxFilter(session.user.id, { state: next, hidden });
    setStateValue(next);
  }

  function showAllTypes() {
    storeInboxFilter(session.user.id, { state, hidden: new Set() });
    setHiddenValue(new Set());
  }

  function toggleType(type: InboxType) {
    const next = new Set(hidden);
    if (next.has(type)) next.delete(type);
    else next.add(type);
    storeInboxFilter(session.user.id, { state, hidden: next });
    setHiddenValue(next);
  }

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60_000);
    return () => window.clearInterval(timer);
  }, []);

  // Beside the sidebar it takes no focus: the button that opened it keeps it,
  // by a press or a key, and a focus pressed inside it goes with it, to
  // nothing chosen — so its button wears the keyboard's ring after Escape
  // only where the focus stayed on it. Notion's, judged by the screen and a
  // letter typed after the action with no script in the page before it
  // (2026-11-15, Chrome 1440): 수신함 pressed, then Escape, its button ringed
  // and the letter landing nowhere. A phone's whole-screen sheet takes it,
  // and gives it back.
  useEffect(() => {
    if (!sheet) return;
    restoreFocusRef.current = focusedOpener();
    const frame = window.requestAnimationFrame(() => closeRef.current?.focus());
    return () => window.cancelAnimationFrame(frame);
  }, [sheet]);

  // Lying over the page it is a layer: Escape closes it once nothing opened
  // over it takes the key. Docked or in the sidebar it closes from inside it.
  const layered = !inline && !docked;
  // The sidebar beside it counts as its own for Escape: with the focus back
  // on the sidebar's switcher after its menu closed, Escape closes the inbox,
  // as Notion's (seen 2026-11-06).
  const { zIndex } = useOverlayLayer(layered, { kind: "panel", ref: panelRef, onEscape: onClose,
    inside: () => [document.querySelector("[data-sidebar-slot]")] });
  // Beside the sidebar, the sidebar's own menus still open over it, as
  // Notion's (seen 2026-11-06: the workspace menu and a page row's "…"
  // over the inbox): while it lies over the page the sidebar stands just
  // above it, and layers opened later stand above both.
  useLayoutEffect(() => {
    const slot = layered && zIndex !== undefined ? document.querySelector<HTMLElement>("[data-sidebar-slot]") : null;
    if (!slot) return;
    slot.style.zIndex = String(zIndex! + 1);
    return () => { slot.style.zIndex = ""; };
  }, [layered, zIndex]);

  // A phone's sheet gives the focus back to what opened it only from inside
  // it: one left elsewhere stays there. The sheet is gone by now, so focus
  // from inside it is on the body.
  useEffect(() => () => {
    const restore = restoreFocusRef.current;
    restoreFocusRef.current = null;
    const active = document.activeElement;
    const fromInside = !active || active === document.body || !active.isConnected;
    if (restore?.isConnected && fromInside) giveFocusBack({ to: restore, keepScroll: true });
  }, []);

  function onPanelKeyDown(event: ReactKeyboardEvent<HTMLElement>) {
    if (event.defaultPrevented || layered || event.key !== "Escape") return;
    // A menu open over it (the filter, "…") takes the Escape and closes alone.
    if (escapeLayerOver(event.currentTarget, event.nativeEvent)) { event.stopPropagation(); return; }
    event.preventDefault();
    onClose();
  }

  // Opening what a notice points at leaves focus to that place rather than
  // the inbox's button, as Notion's does (seen 2026-11-06: a reply's notice
  // opens its thread with the caret in the reply field).
  function closeForTarget() {
    restoreFocusRef.current = null;
    if (!docked) onClose();
  }

  function openActivity(activity: NotificationActivity) {
    void controller.markRead(activity.notification.id);
    const target = activity.notification.target;
    const discussions = inboxDiscussionsPage(activity.notification);
    if (discussions) {
      askPageDiscussions(discussions.pageId);
      pushRoute(discussions.path);
      closeForTarget();
      return;
    }
    // A question answered outside the workspace (an agent's call to approve) opens as its own screen.
    if (target.startsWith("/") && standaloneConsent(new URL(target, window.location.origin).href)) {
      window.location.assign(target);
      return;
    }
    pushRoute(target
      ? inboxOpenHref(target, new URL(window.location.href))
      : activity.notification.pageId ? pageHref(activity.notification.pageId) : "/settings");
    closeForTarget();
  }

  const EmptyIcon = EMPTY_ICONS[state];
  const title = t(`updatesPanel:inbox.titles.${state}`);
  const groupHead = state !== "workspace" && !noTypes && !loading && groups.length ? t(`updatesPanel:inbox.${groups[0].section}`) : "";
  const panel = (
    <aside
      ref={panelRef}
      className={chrome.panel}
      style={{
        ...(inline || sheet ? {} : { left, width }),
        ...(zIndex === undefined ? {} : { zIndex }),
      }}
      data-placement={placement}
      data-docked={docked ? "true" : undefined}
      data-closing={exiting ? "true" : undefined}
      data-inbox-panel
      aria-labelledby={titleId}
      onKeyDown={onPanelKeyDown}
    >
      {sheet ? <InboxPhoneBar closeRef={closeRef} onClose={onClose} unread={controller.snapshot.unreadCount > 0}
        workspaceIcon={workspaceIcon} workspaceName={workspaceName} /> : null}
      <div className={chrome.header}>
        {/* A phone's head line reads the first group's name, as the app's ("오늘", none while empty); the title stays for the panel's name. */}
        <h2 id={titleId} className={chrome.title}>{sheet ? <><span className={chrome.phoneTitle}>{title}</span>
          <span aria-hidden="true" data-inbox-group-head>{groupHead}</span></> : title}</h2>
        <div className={chrome.headerActions}>
          {pinned !== null && onPinnedChange ? (
            <button type="button" className={chrome.headerButton} data-inbox-pin aria-pressed={pinned}
              aria-label={t(pinned ? "updatesPanel:inbox.unpin" : "updatesPanel:inbox.pin")}
              title={t(pinned ? "updatesPanel:inbox.unpin" : "updatesPanel:inbox.pin")}
              onClick={() => onPinnedChange(!pinned)}>
              {pinned ? <PinSlashIcon size="regular" aria-hidden="true" /> : <PinIcon size="regular" aria-hidden="true" />}
            </button>
          ) : null}
          {/* A phone's goes back by the workspace's icon (InboxPhoneBar), and has no gear, as the app's. */}
          {wideTouch || sheet ? null : (
            <button ref={closeRef} type="button" className={chrome.headerButton} data-inbox-close
              aria-label={t("updatesPanel:inbox.close")} title={t("updatesPanel:inbox.close")} onClick={onClose}>
              <DoubleChevronLeft size="regular" aria-hidden="true" />
            </button>
          )}
          <button type="button" className={chrome.headerButton} data-inbox-filter
            data-filtered={filtered ? "true" : undefined} aria-haspopup="menu" aria-expanded={menu?.kind === "filter"}
            aria-label={t("updatesPanel:inbox.filter")} title={t("updatesPanel:inbox.filter")}
            onClick={(event) => setMenu(menu?.kind === "filter" ? null : { kind: "filter", anchor: event.currentTarget })}>
            {/* A wide touch screen's filter is ringed, as Notion's iPad (InboxFilterCircleIcon); a phone's is the app's bare lines. */}
            {wideTouch ? <InboxFilterCircleIcon size="wide" aria-hidden="true" /> : <FilterIcon size="regular" aria-hidden="true" />}
          </button>
          {state === "archived" || state === "workspace" || noTypes ? null : (
            <button type="button" className={chrome.headerButton} data-inbox-more aria-haspopup="menu"
              aria-expanded={menu?.kind === "more"}
              aria-label={t("updatesPanel:inbox.more")} title={t("updatesPanel:inbox.more")}
              onClick={(event) => setMenu(menu?.kind === "more" ? null : { kind: "more", anchor: event.currentTarget })}>
              <DotsHorizontal size="regular" aria-hidden="true" />
            </button>
          )}
        </div>
      </div>

      <div className={`${chrome.list} nscroll`} data-inbox-list aria-busy={(state === "workspace" ? workspaceActivity.loading : loading) || undefined}>
        {state === "workspace" ? <WorkspaceActivityFeed controller={workspaceActivity} pages={pages} actorId={session.user.id} now={now}
          onOpen={(item, href) => { pushRoute(href ?? pageHref(item.pageId)); closeForTarget(); }} /> : noTypes ? (
          <div className={styles.empty} role="status" data-inbox-no-types>
            <FilterIcon size="card" aria-hidden="true" />
            <span className={styles.emptyTitle}>{t("updatesPanel:inbox.noTypes.title")}</span>
            <span className={styles.emptyNote}>{t(`updatesPanel:inbox.noTypes.${state}`)}</span>
            <button type="button" className={styles.emptyAction} data-inbox-show-all-types onClick={showAllTypes}>
              {t("updatesPanel:inbox.noTypes.showAll")}
            </button>
          </div>
        ) : loading ? (
          <div className={styles.skeleton} aria-hidden="true">
            {[0, 1, 2, 3].map((index) => <div key={index} className={styles.skeletonRow}><span /><span /></div>)}
          </div>
        ) : visible.length === 0 && controller.unanswered ? (
          // A tab the server wasn't there to read says so, not that nothing came.
          <div className={styles.empty} role="status" data-read-unanswered>
            <span>{unansweredReadText()}</span>
          </div>
        ) : visible.length === 0 && sheet && state === "inbox" ? (
          <InboxPhoneEmpty title={t("updatesPanel:inbox.empty.inbox")} note={t("updatesPanel:inbox.phoneEmptyNote")} />
        ) : visible.length === 0 ? (
          <div className={styles.empty} role="status">
            <EmptyIcon size="card" aria-hidden="true" />
            <span>{t(`updatesPanel:inbox.empty.${state}`)}</span>
          </div>
        ) : groups.map((group, index) => (
          <section key={group.section} className={styles.section} aria-label={t(`updatesPanel:inbox.${group.section}`)}>
            {sheet && index === 0 ? null : <div className={styles.sectionLabel}>{t(`updatesPanel:inbox.${group.section}`)}</div>}
            <ul className={styles.sectionList}>
              {group.items.map((activity) => (
                <InboxRow key={activity.id} activity={activity} profiles={profiles} now={now} readerId={session.user.id}
                  readerName={session.user.name.trim() || session.user.email} workspaceIcon={workspaceIcon}
                  archived={state === "archived"} requests={requests} deadlines={deadlines}
                  onOpen={() => openActivity(activity)}
                  onPageSettings={(anchor) => setPageMenu({ anchor, pageId: activity.notification.pageId })}
                  onMarkRead={() => void controller.markRead(activity.notification.id)}
                  onMarkUnread={() => void controller.markUnread(activity.notification.id)}
                  onArchive={() => void controller.archiveNotification(activity.notification.id)}
                  onUnarchive={() => void controller.unarchiveNotification(activity.notification.id)} />
              ))}
            </ul>
          </section>
        ))}
        {state !== "workspace" && !loading && controller.snapshot.hasMore ? (
          <div ref={moreRef} data-inbox-continuation aria-busy={controller.loadingMore || undefined}>
            <button type="button" className={styles.activityContinue} disabled={controller.loadingMore} onClick={() => void controller.loadMore()}>
              {t("updatesPanel:inbox.continue")}
            </button>
          </div>
        ) : null}
      </div>
      {sheet && phoneFoot ? <InboxPhoneFoot>{phoneFoot}</InboxPhoneFoot> : null}
      {menu?.kind === "filter" ? (
        <InboxFilterMenu anchor={menu.anchor} state={state} hidden={hidden} onState={setState}
          onToggleType={toggleType} onClose={() => setMenu(null)} />
      ) : null}
      {menu?.kind === "more" ? (
        <InboxMoreMenu anchor={menu.anchor} canMarkRead={controller.snapshot.unreadCount > 0}
          canArchive={controller.snapshot.items.length > 0}
          onMarkAllRead={() => void controller.markAllRead()} onArchiveAll={() => void controller.archiveAll()}
          onClose={() => setMenu(null)} />
      ) : null}
      {!inline && onWidthChange ? <InboxResizeHandle width={width} onWidthChange={onWidthChange} onClose={onClose} /> : null}
      {pageMenu ? (
        <InboxPageNotifyMenu anchor={pageMenu.anchor} pageId={pageMenu.pageId} session={session}
          onClose={() => setPageMenu(null)} />
      ) : null}
    </aside>
  );

  if (inline) return panel;
  return createPortal(
    <>
      {layered && !sheet ? (
        // Over the page only, as Notion's: the sidebar stays in reach.
        <button type="button" className={chrome.backdrop} tabIndex={-1} aria-label={t("updatesPanel:inbox.close")}
          style={{ left, ...(zIndex === undefined ? {} : { zIndex: zIndex - 1 }) }} onClick={onClose} />
      ) : null}
      {panel}
    </>,
    document.body,
  );
}
