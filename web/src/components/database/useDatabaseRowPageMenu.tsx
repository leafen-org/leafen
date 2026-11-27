import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ArchiveIcon, Bell, ClipboardIcon, UpdatesClockIcon, ConnectionsIcon, Copy, DotsHorizontal, ExportIcon, EyeIcon, ImportIcon, LinkIcon, PageLayoutIcon, SendIcon,
  LockIcon, PresentIcon, TableOfContentsIcon, SuggestEditIcon, Trash, UndoIcon, UnlockIcon, VersionHistoryIcon,
} from "@/icons/leafen";
import { copyText } from "@/lib/copyText";
import { shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import { canPresentPage, requestPresentation } from "@/lib/pageSlides";
import { useProductToast } from "@/lib/productToast";
import { mutateDatabaseRowPagePermission, readDatabaseRowPagePermissions } from "@/databaseRowPageApi";
import type { DatabaseRowPagePermissions } from "@/databaseRowPageWire.generated";
import { pageLayoutCommentsChange, pageLayoutCommentsStyle, saveDatabasePageLayoutComments, useDatabasePageLayout } from "@/lib/databasePageLayout";
import type { DatabaseRowNotificationLevel } from "@/notificationSettingsApi";
import type { Session } from "@/types";
import { readEveryWindow } from "@/lib/listWindows";
import { publicMutationService } from "@/lib/publicMutationService";
import { requestReadLogDialog } from "@/lib/readLog";
import { requestSendItemDialog } from "@/lib/rowSend";
import { requestPageUndo } from "../editor/pageUndo";
import { pageMenuSearchWords } from "@/lib/pageMenuSearchWords";
import { menuCheck, menuSwitch, pageMenuItem, suggestingStop, usePageMenu, type PageMenuEntry, type PageMenuShow, type PageMenuSubmenu } from "../PageMenu";
import { usePageThemeSubmenu } from "../PageStyleControls";
import type { PageSuggestingControl } from "../RowMenu";
import { RowMenuChoiceSubmenu, usePageConnections, type PageConnectionShares } from "../RowMenuSubmenus";
import { RowMenuConnectionsSubmenu } from "../RowMenuConnections";
import styles from "../RowMenu.module.css";
import type { DatabaseRowMoveDrop, DatabaseRowMoveTarget, DatabaseRowPageLockControl, RowPageMenuExtras } from "./DatabaseRowMenu";
import { DatabaseRowMoveSubmenu, DatabaseRowMovePhoneList } from "./DatabaseRowMoveSubmenu";
import type { RowMenuPhoneActions } from "../RowMenuPhoneSheet";
import type { PageStyleChange } from "../PageStyleControls";
import type { DatabaseRowMoveTargetSearch } from "./useDatabaseRowMoveTargets";
import { withLoadDeadline } from "@/lib/lazyScreen";
import { useProductPages } from "@/components/product/productPagesContext";
import { menuKeyHint } from "@/lib/menuKeyHint";
import { pageMenuRows } from "../lockedPageMenu";
import { noteUnlockedInThisTab } from "@/lib/pageUnlockSession";
import { copiesPageContents, pageMenuOffers, type PageMenuRowKey } from "@/lib/pageReaderChrome";

/** The open row page the menu acts on. */
export type RowPageMenuPage = { id: string; title: string; workspaceId: string; session: Session; dataSourceId?: string; sendable?: boolean;
  /** A database template's page, which has no sends or read log, and whose connections are its database page's. */
  template?: boolean;
  /** Its database's layout can't change now (its lock): a phone's "레이아웃 사용자 지정" faint, "페이지 사용자 지정" beside it. */
  layoutHeld?: boolean;
  /** Its style as it shows, which a phone's "작업" sheet shows while the page's lock holds it. */
  shownStyle?: RowMenuPhoneActions["style"];
  /** Its database's layout, whose comments a phone's "페이지 사용자 지정" shows as they change; `lockHeld` while its database's lock
   * holds the layout for this reader, who may still change its comments, there. */
  layout?: { pageId: string; dataSourceId: string; lockHeld: boolean } };

const ROW_NOTIFY_LEVELS = ["all_updates", "all_comments", "replies_and_mentions"] as const;

/**
 * A database page's shares, as its own share menu reads and changes them; only its owners change them.
 * The workspace's Notion-compatible connections are the page's connections, as any page's.
 */
function rowPageShares(session: Session, rowId: string): PageConnectionShares {
  let revision = 0;
  const shares = (state: DatabaseRowPagePermissions) => state.permissions.map(({ id, principalId }) => ({ id, principalId }));
  // Every share, a window at a time, so an app's share is found however many people share the page.
  const all = async (signal?: AbortSignal) => {
    let manageable = true;
    const read = await readEveryWindow(async (cursor) => {
      const state = await readDatabaseRowPagePermissions(session, rowId, signal, cursor);
      manageable = state.canManage;
      return { items: shares(state), nextCursor: state.canManage ? state.nextCursor : undefined };
    }, signal);
    return manageable ? read : null;
  };
  return {
    read: async (signal) => {
      const read = await all(signal);
      if (!read) return null;
      const connections = await publicMutationService.appConnections(session, rowId, signal);
      revision = connections.revision;
      return { shares: read, manageable: true, notion: connections.notionConnections };
    },
    changeNotion: async (grantId, connect) => {
      const state = await publicMutationService.changeAppConnection(session, rowId,
        { expectedRevision: revision, action: connect ? "connect" : "disconnect", grantId });
      revision = state.revision;
      return state.notionConnections;
    },
    grant: async (principalId) => {
      await mutateDatabaseRowPagePermission(session, rowId, { action: "grant", principalId, role: "edit" });
      return (await all()) ?? [];
    },
    remove: async (shareId) => {
      await mutateDatabaseRowPagePermission(session, rowId, { action: "remove", permissionId: shareId });
      return (await all()) ?? [];
    },
  };
}

/**
 * A database page's own "…" below its search, as Notion's (seen 2026-10-17):
 * the page menu's layout — link, contents, duplicate, move, history, updates
 * | 테마와 레이아웃 › (fonts; small text, full width, 목차 | 레이아웃 사용자 지정)
 * | suggest edits, notifications › | lock, archive, trash | 더 보기 ›
 * (presentation mode, undo, import, export | connections ›). The AI rows
 * aren't Leafen's. The rows go in the menu, the rows beside it outside it.
 */
export function useDatabaseRowPageMenu({
  page, query, busy, readOnly, archived, pageStyle, lock, suggesting, notifyLevel, notifyBusy, onNotifyLevel, onNotifyRetry,
  moveTargets, searchMoveTargets, onMove, onPreviewMove, onOpenMove, onCopyLink, onDuplicate, onArchive, onTrash,
  onImport, onExport, onAnalytics, onHistory, onCustomizeLayout, onClose,
}: {
  /** The open row page; without it the menu is a row's in its view, and this offers nothing. */
  page?: RowPageMenuPage;
  query: string;
  busy: boolean;
  readOnly: boolean;
  archived: boolean;
  pageStyle?: RowPageMenuExtras["style"];
  lock?: DatabaseRowPageLockControl;
  suggesting?: PageSuggestingControl;
  /** How the reader follows the row once read; null while unknown or not offered. */
  notifyLevel: DatabaseRowNotificationLevel | null;
  notifyBusy: boolean;
  onNotifyLevel?: (level: DatabaseRowNotificationLevel) => void;
  /** The "Notify me" choice couldn't be read; choosing the row reads it again. */
  onNotifyRetry?: () => void;
  moveTargets: DatabaseRowMoveTarget[];
  searchMoveTargets?: DatabaseRowMoveTargetSearch;
  onMove?: (target: DatabaseRowMoveTarget, acceptDroppedPropertyIds: string[]) => Promise<void>;
  onPreviewMove?: (target: DatabaseRowMoveTarget) => Promise<DatabaseRowMoveDrop[]>;
  /** Opens the pages' move popover for "옮기기" instead of the list under it. */
  onOpenMove?: () => void;
  onCopyLink: () => Promise<void>;
  onDuplicate?: () => Promise<void>;
  onArchive?: (archived: boolean) => Promise<void>;
  onTrash?: () => Promise<void>;
  onImport?: () => void;
  onExport?: () => void;
  onAnalytics?: () => void;
  onHistory?: () => void;
  /** Opens the database's page layout, where the database may change. */
  onCustomizeLayout?: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation(["rowMenu", "topBar", "databaseView"]);
  const { notify } = useProductToast();
  const menu = usePageMenu(query);
  // As a page's menu: a guest, one who edits too, doesn't copy or export the contents; a member who reads does (copiesPageContents).
  const guest = useProductPages()?.workspaces?.find(({ id }) => id === page?.workspaceId)?.guest === true;
  // A template keeps no shares of its own: its connections are read as a page's, all from its database page.
  const shares = useMemo(() => (page && !page.template ? rowPageShares(page.session, page.id) : undefined), [page?.id, page?.session, page?.template]);
  // Read as it changes, so "페이지 사용자 지정" shows a change at its press, made here or on another of the database's pages.
  const layout = useDatabasePageLayout(page?.session, page?.layout?.pageId, page?.layout?.dataSourceId).layout;
  const connections = usePageConnections(page ?? { id: "", workspaceId: "" }, page?.session as Session, Boolean(page) && !readOnly, shares);
  // Every row is made; the menu's search finds and ranks them (usePageMenu).
  const visible: PageMenuShow = menu.matches;
  // The lock switch turns at its press and the menu stays open with the
  // locked page's rows, as Notion's (seen 2026-11-16, Chrome 1440: a page's, a
  // database's and a database page's "…", the last twice): what was pressed
  // shows until the page is read again with it, or goes back if refused.
  const [pressedLock, setPressedLock] = useState<boolean | null>(null);
  const shownLock = lock && pressedLock !== null ? { ...lock, locked: pressedLock } : lock;
  const turnLock = () => {
    if (!lock || !shownLock) return;
    const next = !shownLock.locked;
    setPressedLock(next);
    void Promise.resolve(lock.onLock(next)).catch(() => undefined).finally(() => setPressedLock((now) => now === next ? null : now));
  };
  const locked = Boolean(shownLock?.locked && !shownLock.unlockedForMe);
  const lockRows = pageMenuRows(locked, Boolean(suggesting?.active));
  // One who may only read or comment on the page has the reader's rows alone, the rest left out, as a page's "…" (pageMenuOffers).
  const offers = (row: PageMenuRowKey) => pageMenuOffers(row, !readOnly);
  // A row with a person property is passed on to a member with "보내기" (Leafen's own).
  const sendable = Boolean(!readOnly && page?.sendable && !page.template);
  const item = pageMenuItem(busy);
  const styleLocked = busy || readOnly || locked;
  // Locked, the page keeps only its database's layout there, as Notion's (seen 2026-11-14).
  const theme = usePageThemeSubmenu({
    busy, disabled: styleLocked, styled: !locked,
    style: pageStyle?.value ?? { font: "default", smallText: false, fullWidth: false },
    onChange: (change) => pageStyle?.onChange(change),
    rows: (show) => [
      // Nor 목차 while its database's lock holds its layout, as Notion's (seen 2026-11-16 twice, Chrome 1440).
      pageStyle && !locked && !page?.layout?.lockHeld ? item(show, "outline", t("rowMenu:customize.tableOfContents"), <TableOfContentsIcon size="regular" aria-hidden="true" />,
        () => pageStyle.onChange({ tableOfContentsDisplay: pageStyle.value.tableOfContentsDisplay === "off" ? "default" : "off" }),
        { disabled: readOnly || locked, role: "menuitemcheckbox", checked: pageStyle.value.tableOfContentsDisplay !== "off",
          trailing: menuSwitch(pageStyle.value.tableOfContentsDisplay !== "off"), data: { "data-page-outline-toggle": "" } }) : null,
    ],
    // Apart from the theme's rows, as Notion's (seen 2026-11-21 twice); faint while its database's lock holds it (seen 2026-11-16, Chrome 1440).
    apart: (show) => [
      onCustomizeLayout || page?.layout?.lockHeld ? item(show, "layout", t("rowMenu:actions.customizeLayout"), <PageLayoutIcon size="regular" aria-hidden="true" />,
        () => { onClose(); onCustomizeLayout?.(); }, { disabled: !onCustomizeLayout, data: { "data-row-page-customize-layout": "" } }) : null,
    ],
  });
  if (!page) return { items: null, overlay: null, onPointerMove: undefined, phone: null };
  const copyContents = async () => {
    onClose();
    try {
      const { readPageMarkdown } = await withLoadDeadline(import("../nativeExport"));
      const markdown = (await readPageMarkdown(page, { session: page.session, signal: new AbortController().signal }))
        .replace(/^# .*\n\n?/u, "");
      // An empty page has nothing to copy, which isn't a failure.
      if (!markdown.trim()) {
        notify(t("rowMenu:toast.nothingToCopy"));
        return;
      }
      if (!(await copyText(markdown))) throw new Error("Copy failed.");
      notify(t("rowMenu:toast.copiedContents"), "success");
    } catch {
      notify(t("rowMenu:toast.couldntCopyContents"), "error");
    }
  };
  const levelLabel = (level: DatabaseRowNotificationLevel) => t(level === "all_updates" ? "rowMenu:notify.allUpdates"
    : level === "all_comments" ? "rowMenu:notify.allComments" : "rowMenu:notify.repliesAndMentions");
  // The menu's line says the level short, as Notion's "알림 받기 댓글" and "멘션" (seen 2026-10-30).
  const shownLevel = (level: DatabaseRowNotificationLevel) => level === "replies_and_mentions" ? t("rowMenu:notify.shortMentions")
    : level === "all_comments" ? t("rowMenu:notify.shortAll") : levelLabel(level);

  const notifyLabel = t("rowMenu:notify.label");
  const notifyRow: PageMenuSubmenu | null = onNotifyLevel ? {
    submenu: "notify", label: notifyLabel, icon: <Bell size="regular" aria-hidden="true" />, width: 300,
    value: notifyLevel ? shownLevel(notifyLevel)
      : onNotifyRetry ? t("rowMenu:notify.loadFailed") : "",
    disabled: busy || notifyBusy || (!notifyLevel && !onNotifyRetry), onChoose: !notifyLevel && onNotifyRetry ? onNotifyRetry : undefined,
    data: { "data-row-notify-level": notifyLevel ?? "", ...(onNotifyRetry && !notifyLevel ? { "data-read-failed": "" } : {}) },
    panel: (row, close) => (
      <RowMenuChoiceSubmenu row={row} label={notifyLabel} value={notifyLevel}
        choices={ROW_NOTIFY_LEVELS.map((level) => ({
          value: level, label: levelLabel(level),
          hint: level === "all_updates" ? t("rowMenu:notify.allUpdatesHint") : undefined,
        }))}
        onChoose={onNotifyLevel} onClose={close} />
    ),
    // A search lists the choices under its name, once read.
    sections: notifyLevel ? (show, searching) => searching ? [{
      rows: ROW_NOTIFY_LEVELS.map((level) => item(show, `notify-${level}`, levelLabel(level), null, () => onNotifyLevel(level),
        { disabled: notifyBusy, role: "menuitemradio", checked: notifyLevel === level, trailing: menuCheck(notifyLevel === level),
          data: { "data-page-notify-choice": level } })),
    }] : [] : undefined,
  } : null;
  const more: PageMenuSubmenu = {
    submenu: "more", label: t("rowMenu:more"), icon: <DotsHorizontal size="regular" aria-hidden="true" />, width: 280, data: { "data-page-more-menu": "" },
    sections: (show) => [
      {
        rows: [
          // "프레젠테이션 모드" presents the open page from its title, as Notion's row page menu (seen 2026-10-12)
          // and a template's (seen 2026-11-16, Chrome 1440: 프레젠테이션 모드 · 번역 · 실행 취소 · 가져오기 · 내보내기 | 연결 | Mac 앱에서 열기).
          canPresentPage(page.id) ? item(show, "present", t("rowMenu:actions.presentationMode"), <PresentIcon size="regular" aria-hidden="true" />,
            () => { onClose(); requestPresentation({ pageId: page.id }); },
            { hint: shortcutCompactLabel("presentPage"), badge: t("rowMenu:actions.beta"), data: { "data-page-present-action": "" } }) : null,
          readOnly || !lockRows.undo ? null : item(show, "undo", t("rowMenu:actions.undo"), <UndoIcon size="regular" aria-hidden="true" />,
            () => { onClose(); requestPageUndo(page.id); }, { hint: menuKeyHint("⌘Z"), data: { "data-page-undo-action": "" } }),
          onImport && lockRows.import && offers("import") ? item(show, "import", t("rowMenu:actions.import"), <ImportIcon size="regular" aria-hidden="true" />, () => { onClose(); onImport(); },
            { disabled: readOnly, data: { "data-row-page-import": "" } }) : null,
          onExport ? item(show, "export", t("rowMenu:actions.export"), <ExportIcon size="regular" aria-hidden="true" />, () => { onExport(); onClose(); },
            { disabled: !copiesPageContents(guest), data: { "data-page-export-action": "" } }) : null,
          // Leafen's read log follows the export, as in a page's menu (RowMenuActions).
          !page.template && !readOnly ? item(show, "read-log", t("rowMenu:actions.readLog"), <EyeIcon size="regular" aria-hidden="true" />,
            () => { onClose(); requestReadLogDialog({ workspaceId: page.workspaceId, scope: `page:${page.id}` }); },
            { data: { "data-row-page-read-log": "page" } }) : null,
        ],
      },
      {
        rows: [!readOnly && (connections.manageable || page.template) && lockRows.connections ? {
          submenu: "connections", label: t("rowMenu:actions.connections"), search: pageMenuSearchWords("connections"),
          icon: <ConnectionsIcon size="regular" aria-hidden="true" />, width: 350,
          value: connections.value, disabled: busy, data: { "data-page-connections-action": "" },
          panel: (row, close) => <RowMenuConnectionsSubmenu row={row} connections={connections} onClose={close} />,
        } satisfies PageMenuSubmenu : null],
      },
    ],
  };

  const groups: PageMenuEntry[][] = [
    [
      item(visible, "link", t("rowMenu:actions.copyLink"), <LinkIcon size="regular" aria-hidden="true" />, () => void onCopyLink(), { hint: shortcutCompactLabel("copyPageLink") }),
      sendable ? item(visible, "send", t("rowMenu:actions.send"), <SendIcon size="regular" aria-hidden="true" />,
        () => { onClose(); requestSendItemDialog({ rowId: page.id, title: page.title, workspaceId: page.workspaceId, dataSourceId: page.dataSourceId ?? "" }); }, { data: { "data-row-page-send": "" } }) : null,
      item(visible, "contents", t("rowMenu:actions.copyContents"), <ClipboardIcon size="regular" aria-hidden="true" />, () => void copyContents(),
        { disabled: !copiesPageContents(guest), data: { "data-page-copy-contents": "" } }),
      onDuplicate && lockRows.duplicate && offers("duplicate") ? item(visible, "duplicate", t("rowMenu:actions.duplicate"), <Copy size="regular" aria-hidden="true" />, () => void onDuplicate(),
        { hint: menuKeyHint("⌘D"), disabled: readOnly }) : null,
      // A locked page can't move, nor go back to an earlier version, as a page's menu (lockedPageMenu.ts).
      onMove && lockRows.move && offers("move") ? (
        <DatabaseRowMoveSubmenu key="move" data-menu-label={t("rowMenu:actions.moveTo")} busy={busy} disabled={readOnly} hint={menuKeyHint("⌘⇧P")} moveTargets={moveTargets}
          searchMoveTargets={searchMoveTargets} onMove={onMove} onPreviewMove={onPreviewMove} onOpen={onOpenMove} />
      ) : null,
      // The history and the updates are found by their own words too (pageMenuSearchWords: "2.0", "수정").
      onHistory && lockRows.history && offers("history") ? item(visible, "history", t("rowMenu:actions.pageHistory"), <VersionHistoryIcon size="regular" aria-hidden="true" />,
        () => { onHistory(); onClose(); }, { data: { "data-row-page-history": "" }, search: pageMenuSearchWords("pageHistory") }) : null,
      onAnalytics ? item(visible, "analytics", t("topBar:updatesAndAnalytics"), <UpdatesClockIcon size="regular" aria-hidden="true" />, onAnalytics,
        { data: { "data-row-analytics-action": "" }, search: pageMenuSearchWords("updatesAndAnalytics") }) : null,
    ],
    [lockRows.pageStyle && offers("theme") && (locked ? onCustomizeLayout : pageStyle || onCustomizeLayout) ? theme : null],
    [
      // Not on a locked page, as Notion's. Offered (suggesting given) to whoever may suggest on the body, one who may only
      // comment too — the page's own menu offers it them as well — so the page being read-only for them doesn't dim it.
      suggesting && !locked ? item(visible, "suggest", t("rowMenu:actions.suggestEdits"), <SuggestEditIcon size="regular" aria-hidden="true" />,
        () => { suggesting.onChange(!suggesting.active); onClose(); },
        { data: { "data-page-suggest-action": "" }, trailing: suggesting.active ? suggestingStop(t("rowMenu:actions.stopSuggesting")) : null }) : null,
      notifyRow,
    ],
    [
      shownLock && lockRows.lock && offers("lock") ? item(visible, "lock", t("rowMenu:actions.pageLock"), shownLock.locked ? <LockIcon size="regular" aria-hidden="true" /> : <UnlockIcon size="regular" aria-hidden="true" />,
        turnLock,
        { disabled: readOnly, role: "menuitemcheckbox", checked: shownLock.locked, trailing: menuSwitch(shownLock.locked), data: { "data-row-page-lock": "" } }) : null,
      // No 보관 on a locked page, as a page's menu; an archived one can still come back.
      onArchive && offers("archive") && !(locked && !archived) ? item(visible, "archive", t(archived ? "rowMenu:actions.unarchive" : "rowMenu:actions.archive"),
        <ArchiveIcon size="regular" aria-hidden="true" />, () => void onArchive(!archived),
        { disabled: readOnly, data: { "data-row-archive-action": archived ? "archived" : "active" } }) : null,
      onTrash && offers("trash") ? item(visible, "trash", t("rowMenu:actions.moveToTrash"), <Trash size="regular" aria-hidden="true" />, () => void onTrash(),
        { disabled: readOnly || locked, className: styles.dangerOnHover, data: { "data-page-trash-action": "" } }) : null,
    ],
    [more],
  ];
  // A phone's sheet holds the same actions as Notion's "작업" sheet does a database's page (seen 2026-10-27).
  const phone: RowPagePhoneActions = {
    kind: "row", canEdit: !readOnly, canExport: copiesPageContents(guest), busy, lockedForMe: locked,
    style: { ...(pageStyle?.value ?? page.shownStyle), ...pageLayoutCommentsStyle(layout) },
    onStyle: pageStyle ? (change) => pageStyle.onChange(change as PageStyleChange) : undefined,
    // The comments its database's layout shows, every page's, changed from "페이지 사용자 지정" while that database's lock holds the rest.
    layoutComments: page.layout?.lockHeld ? (change) => void saveDatabasePageLayoutComments(page.session, page.layout!.pageId, page.layout!.dataSourceId,
      pageLayoutCommentsChange(change)).catch(() => notify(t("databaseView:toast.couldntSaveChanges"), "error")) : undefined,
    onCopyLink: () => void onCopyLink(),
    onCopyContents: () => void copyContents(),
    onDuplicate: onDuplicate ? () => void onDuplicate() : undefined,
    // "옮기기" opens the pages' move, as Notion's phone does a database's page (seen 2026-10-31).
    move: onMove ? onOpenMove ? { onPress: onOpenMove } : {
      sheet: (close) => <DatabaseRowMovePhoneList busy={busy} moveTargets={moveTargets} searchMoveTargets={searchMoveTargets}
        onMove={onMove} onPreviewMove={onPreviewMove} onDone={() => { close(); onClose(); }} />,
    } : undefined,
    onTrash: onTrash ? () => void onTrash() : undefined,
    onCustomizeLayout: onCustomizeLayout ? () => { onClose(); onCustomizeLayout(); } : undefined, layoutHeld: page.layoutHeld,
    // Unlocked for the reader alone from "페이지 사용자 지정" as from the top bar's "잠금", for this tab (pageUnlockSession).
    lock: lock && shownLock ? { on: shownLock.locked, label: t("rowMenu:actions.pageLock"), onToggle: turnLock, forMe: {
      unlockedForMe: shownLock.unlockedForMe, change: (unlocked: boolean) => { noteUnlockedInThisTab(page.id, unlocked); lock.onLockForMe(unlocked); },
    } } : undefined,
    archive: onArchive && (!locked || archived) ? { label: t(archived ? "rowMenu:actions.unarchive" : "rowMenu:actions.archive"), onToggle: () => void onArchive(!archived) } : undefined,
    onExport: onExport ? () => { onExport(); onClose(); } : undefined,
    onAnalytics,
    onHistory: onHistory ? () => { onHistory(); onClose(); } : undefined,
    onReadLog: !readOnly ? () => {
      onClose();
      requestReadLogDialog({ workspaceId: page.workspaceId, scope: `page:${page.id}` });
    } : undefined,
    notify: onNotifyLevel ? {
      value: notifyLevel ? shownLevel(notifyLevel)
        : onNotifyRetry ? t("rowMenu:notify.loadFailed") : "",
      current: notifyLevel, disabled: notifyBusy,
      choices: ROW_NOTIFY_LEVELS.map((level) => ({ value: level, label: levelLabel(level) })),
      onChoose: (level) => onNotifyLevel(level as DatabaseRowNotificationLevel), onRetry: onNotifyRetry,
    } : undefined,
    connections: !readOnly && connections.manageable ? {
      value: connections.value ?? "", state: connections,
    } : undefined,
  };
  return { items: menu.list(groups), overlay: menu.overlay(groups), onPointerMove: menu.onPointerMove, phone };
}

/** A database page's actions for a phone's "작업" sheet, but for what its menu adds: favorite, share and foot. */
export type RowPagePhoneActions = Omit<RowMenuPhoneActions, "favorite" | "onShare" | "foot">;
