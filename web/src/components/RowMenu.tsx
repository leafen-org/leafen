"use client";

import { useProductPagesSelect } from "@/components/product/productPagesStore";
import { usePageFullAccess, usePageHeldBy } from "@/lib/pageActionAccessContext";
import { requestReadLogDialog } from "@/lib/readLog";
import type { ProductStatusSink } from "@/lib/productStatus";
import { type RefObject, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { pageHref } from "@/lib/navigation";
import { readDatabasePageViews } from "@/lib/databasePageLink";
import type { NativePageMetadataChange } from "@/lib/nativePageMetadata";
import { pageDuplicationRefusal, type PageDuplicationOptions } from "@/lib/pageDuplicationService";
import { offlinePagePinService } from "@/lib/offlinePagePins";
import { pageArchiveRoot, pageLookup } from "@/lib/pageArchive";
import { createPageAnalyticsPanelService, opensPageUpdates } from "@/lib/pageAnalyticsPanelService";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import { editorName } from "@/lib/peopleDirectory";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import { useProductToast } from "@/lib/productToast";
import { pageBusyNoticeKey } from "@/lib/pageBusy";
import { openPageBodyStats } from "@/lib/pageBodyStats";
import { rowMenuDocumentStats, type RowMenuDocumentStats } from "@/lib/rowMenuDocumentStats";
import { pushRoute } from "@/lib/router";
import { pageTrashRoot, routeLeavingTrashed } from "@/lib/pageTrash";
import type { Page, Session } from "@/types";
import { api } from "@/api";
import type { PageNotificationLevel } from "@/notificationSettingsApi";
import type { PageExportChoice } from "./PageExportDialog";
import type { PagePresentationChange } from "./page/pagePresentation";
import { usePageMenu } from "./PageMenu";
import { useRowMenuGroups } from "./RowMenuActions";
import { RowMenuApps } from "./RowMenuApps";
import { RowMenuDeveloper } from "./RowMenuDeveloper";
import { RowMenuDialogs } from "./RowMenuDialogs";
import { RowMenuEditedFoot } from "./RowMenuEditedFoot";
import { RowMenuPhoneSheet, RowMenuPhoneSidebarSheet, type RowMenuPhoneActions } from "./RowMenuPhoneSheet";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import { pageEditedActivity, usePageLastEdit, usePageLastEditVia } from "@/lib/pageEditedActivity";
import { RowMenuSidebarActions } from "./RowMenuSidebarActions";
import { useRowMenuDatabaseBlock } from "./useRowMenuDatabaseBlock";
import { RowMenuNotifySubmenu, usePageConnections, notifyValueKey } from "./RowMenuSubmenus";
import { RowMenuConnectionsSubmenu } from "./RowMenuConnections";
import { openPagePeek } from "@/lib/pagePeek";
import styles from "./RowMenu.module.css";
import { PageActionSearch } from "./PageActionSearch";
import { confirmDatabasePageTrash, confirmDatabaseSprintsTrash } from "./editor/databaseBlockRemoval";
import { requestPageUndo } from "./editor/pageUndo";
import { requestPageLayoutBuilder } from "@/lib/databasePageLayout";
import { canPresentPage, requestPresentation } from "@/lib/pageSlides";
import { copiesPageContents } from "@/lib/pageReaderChrome";
import { NativeActionMenu, NativeMenuHeader, NativeMenuSection } from "@/lib/nativeActionMenu";
import { withLoadDeadline } from "@/lib/lazyScreen";
import { PlacedPageMenu } from "./RowMenuPlaced";
import { useRowMenuSharedHandlers } from "./useRowMenuSharedHandlers";
import { useGuestRowMenu } from "./useGuestRowMenu";

type MetadataResult = boolean | Page | void;
/** Unlocking a locked page for oneself only, and locking it again. */
export type PageLockForMe = { unlockedForMe: boolean; change: (unlocked: boolean) => Promise<void> };
/** Suggesting mode on the page, where the reader may suggest edits to its text. */
export type PageSuggestingControl = { active: boolean; onChange: (active: boolean) => void };
/** A page's menu on a touch screen wider than a phone's is the phone's "작업" sheet in its middle, as Notion's iPad (seen 2026-11-13 at 834 and 1210, its page's "⋯" and a database page's alike; TouchSheetLayout); a sidebar row's keeps its own. */
export const RowMenu = (props: Parameters<typeof PageRowMenu>[0]) => props.variant === "sidebar" ? <PageRowMenu {...props} /> : <TouchSheetLayout><PageRowMenu {...props} /></TouchSheetLayout>;

function PageRowMenu({
  anchor,
  opener,
  lockForMe,
  page,
  pages,
  session,
  onClose,
  onPageChange,
  favorite = false,
  onFavoriteChange,
  onPresentationChange,
  onPageMetadataChange,
  onDuplicatePage,
  databasePage,
  exportDatabase,
  inlineDatabase,
  onTrashChange,
  onImport,
  readDocumentStats,
  onRename,
  canEdit,
  accessKnown = true,
  variant = "page",
  favoritesList = false,
  suggesting,
  onStatus,
  initialPanel,
  onOpenUpdatesPanel,
  onOpenAppPanel,
  onShare,
  ahead = false,
}: {
  anchor?: { x: number; y: number } | null;
  /** The bar's "…" it hangs from where it opens at no point (the focused control when none is given). */
  opener?: RefObject<HTMLElement | null>;
  /** The page lock as it applies to this person, where they can unlock it for themselves. */
  lockForMe?: PageLockForMe;
  page: Page;
  pages: Page[];
  session: Session;
  onClose: () => void;
  onPageChange?: (page: Page) => void | Promise<void>;
  favorite?: boolean;
  onFavoriteChange?: (favorite: boolean) => Promise<void>;
  onPresentationChange?: (
    change: PagePresentationChange,
    basePage?: Page,
  ) => boolean | Page | void | Promise<boolean | Page | void>;
  onPageMetadataChange?: (
    page: Page,
    change: NativePageMetadataChange,
    signal: AbortSignal,
  ) => Promise<MetadataResult>;
  onDuplicatePage?: (page: Page, signal: AbortSignal, options?: PageDuplicationOptions & { onOpened?: (copy: Page) => void }) => Promise<Page>;
  // A full-page database may be duplicated without its rows; "linked" a
  // linked database's page, whose lock locks its views.
  databasePage?: boolean | "linked";
  /**
   * The database a database page shows, which its HTML and Markdown & CSV
   * export (its data source and name); a linked one's, itself (its block, or
   * its page as a full page), exported in every format as its source named
   * after it.
   */
  exportDatabase?: { id: string; title: string; linked?: boolean };
  /** The block of an inline database opened alone, which the menu works on in place of its page. */
  inlineDatabase?: string;
  onTrashChange?: (page: Page, inTrash: boolean, signal: AbortSignal) => Promise<MetadataResult>;
  /** Opens the import screen, which imports into the page. */
  onImport?: (page: Page, mode?: "csvMerge") => void;
  readDocumentStats?: (page: Page) => RowMenuDocumentStats;
  onRename?: () => void;
  /** The reader may change the page; a view-only page's menu offers no changes. */
  canEdit: boolean;
  /** Whether canEdit comes from a read of the person's access; until then a sidebar menu names no one. */
  accessKnown?: boolean;
  /** A sidebar page's own short menu, as Notion's, or the page's full one. */
  variant?: "page" | "sidebar";
  /** The sidebar row is in the favorites, whose short menu has no duplicate or trash. */
  favoritesList?: boolean;
  suggesting?: PageSuggestingControl;
  onStatus?: ProductStatusSink;
  /** Opens straight at the page's updates, as the top bar's edited time does, or its export window (the app's 파일 › 인쇄). */
  initialPanel?: "updates" | "export";
  /** Opens the page's updates beside it; only the open page's own menu has it. */
  onOpenUpdatesPanel?: () => void;
  /** Opens an app beside the page; only the open page's own menu has it. */
  onOpenAppPanel?: (appId: string) => void;
  /** Opens the page's share, from a phone's sheet as Notion's "공유". */
  onShare?: () => void;
  /** Drawn ahead and hidden (TopBarPageMenu): it takes no layer and no focus until it shows. */
  ahead?: boolean;
}) {
  const { t } = useTranslation(["rowMenu", "topBar", "common", "syncStatusBadge"]);
  const { notify } = useProductToast();
  // What holds the page's changes back for now is why the menu is short.
  const held = usePageHeldBy(page.id);
  // The foot names who edited last once the people directory knows them.
  usePeopleDirectoryRevision();
  const [busy, setBusy] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const databaseBlock = useRowMenuDatabaseBlock({ blockId: inlineDatabase, canEdit, menuRef, onClose, notify });
  const lockedForMe = databaseBlock ? databaseBlock.lockedForMe : (page.isLocked && !lockForMe?.unlockedForMe) || !canEdit;
  // Moving asks the page's full access, as Notion's does (2026-10-31).
  const fullAccess = usePageFullAccess(page.id, canEdit);
  const [offlinePinned, setOfflinePinned] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [historyVersionId, setHistoryVersionId] = useState<string | undefined>();
  const [analyticsOpen, setAnalyticsOpen] = useState<false | "updates" | "analytics">(initialPanel === "updates" ? "updates" : false);
  const analyticsService = useMemo(() => (session ? createPageAnalyticsPanelService(session) : null), [session]);
  const [exportOpen, setExportOpen] = useState(initialPanel === "export");
  const [notifyLevel, setNotifyLevel] = useState<PageNotificationLevel | null>(null);
  const [notifyReadFailed, setNotifyReadFailed] = useState(false);
  const [notifyAttempt, setNotifyAttempt] = useState(0);
  const searchRef = useRef<HTMLInputElement>(null);
  const operationRef = useRef<AbortController | null>(null);
  // The page menu's search, and the rows open beside it.
  const [query, setQuery] = useState("");
  const pageMenu = usePageMenu(query);
  const connections = usePageConnections(page, session, variant === "page" && canEdit);
  // Its mentions as a plain title says them (closedMentions.ts).
  const title = titleWithClosedMentions(page.title, page.titleSpans) || untitledPageDisplayTitle(page);
  const archiveRoot = pageArchiveRoot(page, pageLookup(pages));
  // The page's own menu on a page in the trash keeps its updates, export and foot alone (useRowMenuGroups).
  const trashed = variant === "page" && Boolean(pageTrashRoot(page, pageLookup(pages)));
  const archiveState = !archiveRoot ? "active" : archiveRoot.id === page.id ? "archived" : "inherited";
  // A locked page offers no 보관, as Notion's (seen 2026-11-05); an archived one can still come back.
  const archiveHiddenByLock = page.isLocked && !lockForMe?.unlockedForMe && archiveState === "active";
  // A page open in a peek counts the body shown there (pageBodyStats.ts).
  const stats = readDocumentStats?.(page) ?? openPageBodyStats(page.id)
    ?? rowMenuDocumentStats([page.content], page.content ? 1 : 0);
  // A database's menus end without a word count, its description or not, as Notion's (seen 2026-11-09: a full-page
  // database's "…" and its sidebar row's; 2026-11-11: an inline one's alone), and a wiki's (2026-11-13).
  const words = page.kind === "database" || databaseBlock || (page.isWiki && page.wikiRootId === page.id) ? 0 : stats.words;
  // What had focus as it opened, which a bar's menu hangs from (RowMenuPlaced); one drawn ahead
  // (TopBarPageMenu) opens as it shows.
  const [focusedAtOpen, setFocusedAtOpen] = useState<Element | null | undefined>(() => ahead ? undefined : typeof document === "undefined" ? null : document.activeElement);
  if (!ahead && focusedAtOpen === undefined) setFocusedAtOpen(document.activeElement);
  const hangsFrom = () => opener?.current ?? (focusedAtOpen instanceof HTMLElement && focusedAtOpen !== document.body ? focusedAtOpen : null);

  const close = useCallback(() => {
    operationRef.current?.abort();
    operationRef.current = null;
    onClose();
  }, [onClose]);
  const { copyLink, copyContents, openMove, moveOpen, setMoveOpen, moveAnchor, openInNewTab, rename, wiki } = useRowMenuSharedHandlers({
    page, session, anchor, menuRef, hangsFrom, close, notify, runOperation, runPastMenu, onPageChange, onRename,
  });
  // "업데이트와 애널리틱스" is for one who may open them (opensPageUpdates) — a page in the trash keeps it, its menu edited by no one — and
  // opens on its updates, as Notion's (seen 2026-10-04): beside the open page from its own menu, else in a window.
  const openAnalytics = !trashed && !opensPageUpdates(canEdit) ? undefined : onOpenUpdatesPanel ? () => { close(); onOpenUpdatesPanel(); }
    : analyticsService ? () => setAnalyticsOpen("updates") : undefined;

  const showMenu = !historyOpen && !exportOpen && !analyticsOpen && !moveOpen && !databaseBlock?.moving;
  const edited = usePageLastEdit(session, page, showMenu && accessKnown);
  const lastEditorId = edited.lastEditedBy || page.createdBy || null;
  const via = usePageLastEditVia(session, page.id, lastEditorId, showMenu && accessKnown);
  // A phone's page menu is Notion's "작업" sheet, which keeps its own layer.
  const { phone: phoneSheet } = useSidebarLayout(true);

  useEffect(() => () => {
    operationRef.current?.abort();
    operationRef.current = null;
  }, []);

  // The page's own menu opens at its search field, as Notion's. Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440:
  // the top bar's "⋯" pressed, "z" typed went into its search (Leafen the same).
  useEffect(() => {
    if (showMenu && variant === "page") searchRef.current?.focus({ preventScroll: true });
  }, [showMenu, variant]);
  // A sidebar full-page database's views, for its link (databasePageLink.ts).
  useEffect(() => {
    if (showMenu && variant === "sidebar") void readDatabasePageViews(session, page);
  }, [showMenu, variant, page.id, page.kind, session]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const controller = new AbortController();
    setNotifyLevel(null);
    setNotifyReadFailed(false);
    // A page in the trash has no notifications row to fill (useRowMenuGroups).
    if (trashed) return;
    void api.readPageNotificationPreference(session, page.id, controller.signal, inlineDatabase).then((preference) => {
      if (!controller.signal.aborted) setNotifyLevel(preference.level);
    }).catch(() => {
      // The row says it couldn't load, and reads again when chosen.
      if (!controller.signal.aborted) setNotifyReadFailed(true);
    });
    return () => controller.abort();
  }, [notifyAttempt, page.id, inlineDatabase, trashed, session.user.id]);

  useEffect(() => {
    let current = true;
    void offlinePagePinService.read(session.user.id, page.id).then((pinned) => {
      if (current) setOfflinePinned(pinned);
    }).catch(() => {
      if (current) setOfflinePinned(false);
    });
    return () => { current = false; };
  }, [page.id, session.user.id]);

  function runOperation<T>(
    operation: (signal: AbortSignal) => Promise<T>,
    options: {
      closeAfter?: boolean;
      onFailure?: (error: unknown) => void;
      onSuccess?: (result: T) => void | Promise<void>;
    } = {},
  ) {
    if (busy) return;
    operationRef.current?.abort();
    const controller = new AbortController();
    operationRef.current = controller;
    setBusy(true);
    void operation(controller.signal).then(async (result) => {
      if (operationRef.current !== controller || controller.signal.aborted) return;
      await options.onSuccess?.(result);
    }).catch((error) => {
      if (
        operationRef.current === controller
        && !controller.signal.aborted
        && !(error instanceof DOMException && error.name === "AbortError")
      ) options.onFailure?.(error);
    }).finally(() => {
      if (operationRef.current !== controller) return;
      operationRef.current = null;
      setBusy(false);
      if (options.closeAfter !== false) onClose();
    });
  }

  // Archive and trash take the page out of the sidebar row this menu belongs
  // to as soon as they start, so they run past the menu, which closes, and
  // their notice still comes.
  function runPastMenu<T>(operation: (signal: AbortSignal) => Promise<T>, options: {
    onFailure(error: unknown): void;
    onSuccess(result: T): void;
  }) {
    close();
    void operation(new AbortController().signal).then(options.onSuccess, options.onFailure);
  }

  async function exportPage(choice: PageExportChoice, signal: AbortSignal) {
    const adapter = await withLoadDeadline(import("./nativeExport"));
    signal.throwIfAborted();
    const database = exportDatabase && { id: exportDatabase.id, title: exportDatabase.title, workspaceId: page.workspaceId,
      inline: Boolean(inlineDatabase) || exportDatabase.linked === true };
    await adapter.exportMenuChoice(page, database || null, choice, { session, signal });
    // A file comes down as Notion's does, its window closing with no notice (seen 2026-11-07); a PDF says its print window opened.
    if (choice.format === "pdf") notify(t("pageExport:printing"), "success");
  }

  // A database may be duplicated without its rows.
  function duplicatePage(options?: { withoutContent?: boolean; toPrivatePages?: boolean }) {
    if (!onDuplicatePage || busy) return;
    // The copy shows in the list as the menu closes (usePageMenuActions); a
    // longer one opens while it is made, as Notion's (seen 2026-11-05), and
    // fills in as it goes (lib/pageCopyProgress.ts). A sidebar row's copy
    // opens nowhere: the reader stays on what they were reading, the page
    // the row stands for too (seen 2026-11-11 and 2026-11-13); the open
    // page's own menu goes to its copy.
    const stay = variant === "sidebar";
    let opened = false;
    const openCopy = (copy: Page) => {
      opened = true;
      onPageChange?.(copy);
      pushRoute(pageHref(copy.id));
    };
    runPastMenu((signal) => onDuplicatePage(page, signal, { ...options, ...(stay ? {} : { onOpened: openCopy }) }), {
      onFailure: (error) => {
        if (error instanceof DOMException && error.name === "AbortError") return;
        notify(t(pageDuplicationRefusal(error)), "error");
        if (opened) pushRoute(pageHref(page.id));
      },
      onSuccess: (duplicate) => {
        onPageChange?.(duplicate);
        if (opened || stay) return;
        // Where the copy's caret goes is the page's own rule: Notion's copy of
        // a page with a title and blocks opens without one (seen 2026-11-07),
        // an empty or untitled one with it (useEmptyPageTitleCaret).
        pushRoute(pageHref(duplicate.id));
      },
    });
  }

  async function trashPage() {
    if (!onTrashChange) return;
    // A database asks first, as Notion's (seen 2026-10-04): its pages go with
    // it. One that only links other databases goes at once (seen 2026-10-12).
    // The notice offers a page or a database back as "복원하기" (seen
    // 2026-10-28).
    const database = page.kind === "database";
    let withIt: Page[] = [];
    if (database) {
      close();
      if (!await confirmDatabasePageTrash(session, page.id)) return;
      const sprints = await confirmDatabaseSprintsTrash(session, page.id, page.title);
      if (!sprints) return;
      withIt = sprints.flatMap((id) => pageLookup(pages)(id) ?? []);
    }
    // The page leaves the list and the screen moves on at once, the page open or one it is under
    // too (user decision 2026-11-12); a refusal brings both back. Restoring brings the person back
    // where they were; else they stay, as Notion's "복원하기" does (2026-11-07: it opened the page).
    const { next, left } = routeLeavingTrashed(page, pageLookup(pages));
    if (next) onPageChange?.({ ...page, inTrash: true, trashedAt: new Date().toISOString() });
    if (next) pushRoute(next);
    runPastMenu(async (signal) => {
      const result = await onTrashChange(page, true, signal);
      if (result === false) throw new Error("Page trash mutation failed.");
      await Promise.all(withIt.map((other) => onTrashChange(other, true, signal)));
      return result;
    }, {
      onFailure: () => {
        notify(t("rowMenu:toast.couldntMoveToTrash"), "error");
        if (left) { onPageChange?.(page); pushRoute(left); }
      },
      onSuccess: (result) => {
        const trashedPage = typeof result === "object" ? result : { ...page, inTrash: true };
        if (typeof result === "object") onPageChange?.(result);
        notify(t("rowMenu:toast.movedToTrash"), "success", {
          label: t("rowMenu:trashDatabase.restore"),
          onClick: async () => {
            const controller = new AbortController();
            const restored = await onTrashChange(trashedPage, false, controller.signal);
            await Promise.all(withIt.map((other) => onTrashChange({ ...other, inTrash: true }, false, controller.signal)));
            if (restored === false) {
              notify(t("rowMenu:toast.couldntRestorePage"), "error");
              return;
            }
            if (typeof restored === "object") onPageChange?.(restored);
            if (left) pushRoute(left);
          },
        });
      },
    });
  }

  function updatePageStyle(change: PagePresentationChange) {
    if (lockedForMe || (!onPageMetadataChange && !onPresentationChange)) return;
    // Each style goes out at once, in order behind the page's earlier changes
    // (the page's metadata queue), so a switch pressed while the last one is
    // still saving lands too and the menu stays live, as Notion's (seen
    // 2026-11-07: font, small text and full width pressed one after another).
    const saving = onPageMetadataChange
      ? onPageMetadataChange(page, change, new AbortController().signal)
      : onPresentationChange?.(change, page);
    void Promise.resolve(saving).catch(() => undefined);
  }

  // The menu stays open with the locked page's rows, shown at once as a style's
  // change is (the page's metadata queue), and says nothing more, as Notion's (seen 2026-10-31).
  function toggleLock() {
    if (onPageMetadataChange) void onPageMetadataChange(page, { isLocked: !page.isLocked }, new AbortController().signal).catch(() => undefined);
  }

  function toggleArchive() {
    if (!onPageMetadataChange) return;
    const archiving = archiveState === "active";
    runPastMenu(async (signal) => {
      const result = await onPageMetadataChange(page, { isArchived: archiving }, signal);
      if (result === false) throw new Error("Page archive mutation failed.");
      return result;
    }, {
      onFailure: () => notify(t(archiving
        ? "rowMenu:toast.couldntArchivePage"
        : "rowMenu:toast.couldntUnarchivePage"), "error"),
      // An archived page leaves the sidebar, so its notice offers it back as
      // "보관 취소", as Notion's (seen 2026-10-11).
      onSuccess: (result) => {
        if (!archiving) return;
        if (typeof result === "object") onPageChange?.(result);
        const archived = typeof result === "object" ? result : page;
        notify(t("rowMenu:toast.archivedPage"), "success", {
          label: t("rowMenu:actions.unarchive"),
          onClick: async () => {
            const restored = await onPageMetadataChange(archived, { isArchived: false }, new AbortController().signal);
            if (restored === false) {
              notify(t("rowMenu:toast.couldntUnarchivePage"), "error");
              return;
            }
            if (typeof restored === "object") onPageChange?.(restored);
          },
        });
      },
    });
  }

  // As Notion's (seen 2026-11-06): choosing closes the menu and says what the
  // page now sends, "실행 취소" putting back what it sent before.
  function changeNotifyLevel(level: PageNotificationLevel) {
    if (level === notifyLevel) return;
    const before = notifyLevel;
    const failed = () => notify(t("rowMenu:toast.couldntUpdateNotifications"), "error");
    runPastMenu(() => api.savePageNotificationPreference(session, page.id, level, inlineDatabase), {
      onFailure: failed,
      onSuccess: (preference) => {
        setNotifyLevel(preference.level);
        notify(t(preference.level === "all_comments" ? "rowMenu:toast.subscribedAllComments" : "rowMenu:toast.subscribedRepliesAndMentions"),
          "default", before ? {
            label: t("rowMenu:actions.undo"),
            icon: "none",
            onClick: () => void api.savePageNotificationPreference(session, page.id, before, inlineDatabase)
              .then((back) => setNotifyLevel(back.level), failed),
          } : undefined);
      },
    });
  }

  // The favorites show it at once (pageFavoriteService), the menu closing.
  function toggleFavorite() {
    if (!onFavoriteChange) return;
    runPastMenu(async () => onFavoriteChange(!favorite), {
      onFailure: () => notify(t("rowMenu:toast.couldntUpdateFavorites"), "error"), onSuccess: () => undefined,
    });
  }

  function toggleOfflinePin() {
    const next = !offlinePinned;
    setOfflinePinned(next);
    runPastMenu(async () => offlinePagePinService.set(session.user.id, page.id, next), {
      onFailure: () => undefined,
      onSuccess: (result) => onStatus?.(t(result.readiness === "ready"
        ? "topBar:offlinePinReady"
        : result.readiness === "pending"
          ? "topBar:offlinePinPending"
          : "topBar:offlinePinRemoved"), "default"),
    });
  }

  // Only whether the reader is a guest there: the whole list, new with each edit's time, drew the bar's
  // menu put away again with each line made (2026-11-15, its React Aria menu new each time).
  const guest = useProductPagesSelect((pages) => pages?.workspaces?.find(({ id }) => id === page.workspaceId)?.guest === true);
  const { editsAbove, copy: guestCopy, leave, rows: guestRows } = useGuestRowMenu({ session, page, pages, guest, canEdit, fullAccess, busy, favoritesList, close });
  const groups = useRowMenuGroups({
    guest,
    archiveState,
    busy,
    offlinePinned,
    page,
    lockedForMe,
    canEdit,
    canMove: fullAccess ?? canEdit,
    databasePage: Boolean(databasePage), trashed,
    onArchive: onPageMetadataChange ? toggleArchive : undefined,
    onCopyLink: copyLink, onCopyContents: copyContents, onDuplicate: () => duplicatePage(),
    onDuplicateWithoutContent: databasePage ? () => duplicatePage({ withoutContent: true }) : undefined,
    // A wiki's page also goes to one's private pages, as Notion's (seen 2026-10-15).
    onDuplicateToPrivate: page.wikiDataSourceId && !page.isWiki ? () => duplicatePage({ toPrivatePages: true }) : undefined,
    duplicateToWorkspace: guestCopy,
    onExport: () => setExportOpen(true),
    onHistory: () => { setHistoryVersionId(undefined); setHistoryOpen(true); },
    onAnalytics: openAnalytics,
    onReadLog: () => { close(); requestReadLogDialog({ workspaceId: page.workspaceId, scope: `page:${page.id}` }); },
    onImport: onImport ? () => { close(); onImport(page); } : undefined,
    onMergeCSV: onImport && (databasePage || (page.isWiki && page.wikiRootId === page.id && page.wikiDataSourceId))
      ? () => { close(); onImport(page, "csvMerge"); } : undefined,
    onCustomizeLayout: databasePage ? () => { close(); requestPageLayoutBuilder({ pageId: page.id }); } : undefined,
    onPresent: canPresentPage(page.id) ? () => { close(); requestPresentation({ pageId: page.id }); } : undefined,
    onLock: toggleLock,
    lockKind: databasePage === "linked" ? "views" : databasePage && page.kind === "database" ? "database" : "page",
    onMove: openMove,
    notifyLevel,
    onNotifyLevel: changeNotifyLevel,
    onNotifyRetry: notifyReadFailed ? () => setNotifyAttempt((current) => current + 1) : undefined,
    connectionsValue: connections.value,
    onConnections: true,
    onOffline: toggleOfflinePin,
    onPresentationChange: updatePageStyle,
    onTrash: trashPage,
    suggesting: suggesting?.active,
    onSuggesting: suggesting ? () => { suggesting.onChange(!suggesting.active); close(); } : undefined,
    onUndo: () => { close(); requestPageUndo(page.id); },
    onWiki: wiki,
    panels: {
      notify: (row, onClose) => <RowMenuNotifySubmenu row={row} level={notifyLevel} onLevel={changeNotifyLevel} onClose={onClose} />,
      connections: (row, onClose, onCloseAll) => <RowMenuConnectionsSubmenu row={row} connections={connections} onClose={onClose} onAnswered={onCloseAll} />,
    },
    ...databaseBlock?.groups,
  });

  const phoneActions = (): RowMenuPhoneActions => {
    const wikiRoot = Boolean(page.isWiki && page.wikiRootId === page.id);
    return {
      kind: databasePage ? "database" : wikiRoot ? "wiki" : "page", canEdit, canExport: copiesPageContents(guest), busy, lockedForMe, trashed,
      style: page, onStyle: updatePageStyle,
      favorite: onFavoriteChange ? { on: Boolean(favorite), onToggle: toggleFavorite } : undefined,
      onShare,
      onCopyLink: copyLink,
      onCopyContents: copyContents,
      onDuplicate: editsAbove ? () => duplicatePage() : undefined, duplicateToWorkspace: guest && !databasePage && !wikiRoot ? guestCopy : undefined,
      onDuplicateWithoutContent: databasePage ? () => duplicatePage({ withoutContent: true }) : undefined,
      onDuplicateToPrivate: page.wikiDataSourceId && !page.isWiki ? () => duplicatePage({ toPrivatePages: true }) : undefined,
      move: { onPress: openMove }, moveDisabled: !(fullAccess ?? canEdit),
      onTrash: () => void trashPage(), trashDisabled: !editsAbove,
      onCustomizeLayout: databasePage ? () => { close(); requestPageLayoutBuilder({ pageId: page.id }); } : undefined,
      lock: {
        on: page.isLocked,
        label: t(wikiRoot ? "rowMenu:actions.wikiLock" : databasePage === "linked" ? "rowMenu:actions.viewLock"
          : databasePage && page.kind === "database" ? "rowMenu:actions.databaseLock" : "rowMenu:actions.pageLock"),
        onToggle: toggleLock, forMe: lockForMe,
      },
      archive: !guest && onPageMetadataChange && archiveState !== "inherited" && !archiveHiddenByLock
        ? { label: t(archiveState === "archived" ? "rowMenu:actions.unarchive" : "rowMenu:actions.archive"), onToggle: toggleArchive } : undefined,
      onExport: () => setExportOpen(true),
      wiki: wiki && !guest ? { label: t(wikiRoot ? "topBar:undoWiki" : "topBar:turnIntoWiki"), onToggle: wiki } : undefined,
      onAnalytics: openAnalytics,
      onHistory: () => { setHistoryVersionId(undefined); setHistoryOpen(true); },
      onReadLog: guest && !fullAccess ? undefined : () => { close(); requestReadLogDialog({ workspaceId: page.workspaceId, scope: `page:${page.id}` }); },
      notify: {
        value: notifyLevel ? t(`rowMenu:${notifyValueKey(notifyLevel)}`) : notifyReadFailed ? t("rowMenu:notify.loadFailed") : "",
        current: notifyLevel,
        choices: (["all_comments", "replies_and_mentions"] as const).map((level) => ({
          value: level, label: t(level === "all_comments" ? "rowMenu:notify.allComments" : "rowMenu:notify.repliesAndMentions"),
        })),
        onChoose: (level) => changeNotifyLevel(level as PageNotificationLevel),
        onRetry: notifyReadFailed ? () => setNotifyAttempt((current) => current + 1) : undefined,
      },
      connections: canEdit && !guest ? { // Notion's phone sheet: no '연결' row for a guest, even an editing one
        value: connections.value ?? "", state: connections,
      } : undefined,
      offline: { on: offlinePinned, onToggle: toggleOfflinePin },
      foot: canEdit || trashed ? <RowMenuEditedFoot name={editorName(edited.lastEditedBy || page.createdBy, session.user.id)}
        editorId={lastEditorId} via={via} at={edited.updatedAt}
        words={words > 0 ? t("rowMenu:meta.wordCount", { count: words }) : undefined} marker="page-menu-edited" /> : undefined,
    };
  };

  return (
    <>
      {showMenu && phoneSheet && variant === "page" ? <RowMenuPhoneSheet actions={{ ...phoneActions(), ...databaseBlock?.phone }} onClose={close} /> : null}
      {showMenu && phoneSheet && variant === "sidebar" ? (
        <RowMenuPhoneSidebarSheet onClose={close} actions={{
          canEdit, busy, favoritesList, guestRows, onLeave: leave, favorite: { on: Boolean(favorite), onToggle: toggleFavorite },
          onCopyLink: copyLink, onDuplicate: () => duplicatePage(), onMove: openMove, onTrash: () => void trashPage(),
          archive: onPageMetadataChange && archiveState !== "inherited" && !archiveHiddenByLock
            ? { label: t(archiveState === "archived" ? "rowMenu:actions.unarchive" : "rowMenu:actions.archive"), onToggle: toggleArchive } : undefined,
          wiki: wiki && page.kind !== "database" ? { label: t(page.isWiki && page.wikiRootId === page.id ? "topBar:undoWiki" : "topBar:turnIntoWiki"), onToggle: wiki } : undefined,
          foot: accessKnown ? <RowMenuEditedFoot name={editorName(edited.lastEditedBy || page.createdBy, session.user.id)}
            editorId={lastEditorId} via={via} at={edited.updatedAt} marker="sidebar-menu-edited" /> : undefined,
        }} />
      ) : null}
      {showMenu && !phoneSheet ? <PlacedPageMenu menuRef={menuRef} anchor={anchor} hangsFrom={hangsFrom} ahead={ahead} onClose={close} initialFocus={searchRef}
        backdropLabel={t("rowMenu:aria.closePageActions")} label={t("rowMenu:aria.pageActionsFor", { title })} variant={variant} accessOf={accessKnown ? undefined : page.id}
        onPointerMove={variant === "page" ? pageMenu.onPointerMove : undefined} after={variant === "page" ? pageMenu.overlay(groups) : null}>
        {variant === "sidebar" ? (
          <RowMenuSidebarActions busy={busy} canEdit={canEdit} heldBy={held ? t(pageBusyNoticeKey(held)) : ""} restricted={accessKnown && !canEdit} favorite={favorite} favoritesList={favoritesList}
            archiveState={archiveState} onArchive={toggleArchive}
            edited={{ name: editorName(edited.lastEditedBy || page.createdBy, session.user.id), at: edited.updatedAt,
              editorId: lastEditorId, via, activity: pageEditedActivity({ ...page, ...edited }, session.user.id) }}
            lockedForMe={lockedForMe} page={page} guest={guest} guestRows={guestRows} onLeave={leave}
            onCopyLink={copyLink} onDuplicate={() => duplicatePage()} onFavorite={toggleFavorite} onMove={openMove}
            onDuplicateWithoutContent={page.kind === "database" ? () => duplicatePage({ withoutContent: true }) : undefined}
            onOpenInNewTab={openInNewTab} onRename={rename} onTrash={trashPage} onWiki={wiki}
            onOpenInSidePeek={() => { close(); openPagePeek(page.id); }} />
        ) : (
          <>
          <NativeActionMenu aria-label={t("rowMenu:aria.pageActionsFor", { title })}
            renderEmptyState={() => <p className={styles.menuEmpty}>{t("rowMenu:noResults")}</p>}>
          {/* The search keeps its letters while its arrows move the menu's highlight, as Notion's. */}
          <NativeMenuSection aria-label={t("rowMenu:searchActions")}><NativeMenuHeader>
            <PageActionSearch className={styles.actionSearch} label={t("rowMenu:searchPageActions")} value={query} inputRef={searchRef}
              marker="page-menu-search" onChange={setQuery} />
          </NativeMenuHeader></NativeMenuSection>
          {pageMenu.list(groups)}
          {trashed ? null : <RowMenuApps session={session} pageId={page.id} query={query} onOpenPanel={onOpenAppPanel} onClose={close} />}
          {query.trim() || trashed ? null : <RowMenuDeveloper page={page} session={session} busy={busy} onClose={close} />}
          </NativeActionMenu>
          {/* A search lists rows alone, as Notion's and the database page's menu. */}
          {(canEdit || trashed) && !query.trim() ? <RowMenuEditedFoot name={editorName(edited.lastEditedBy || page.createdBy, session.user.id)}
            editorId={lastEditorId} via={via} at={edited.updatedAt}
            words={words > 0 ? t("rowMenu:meta.wordCount", { count: words }) : undefined} activity={pageEditedActivity({ ...page, ...edited }, session.user.id)}
            marker="page-menu-edited" /> : null}
          </>
        )}
      </PlacedPageMenu> : null}
      <RowMenuDialogs page={page} pages={pages} session={session} canEdit={canEdit} title={title}
        analyticsService={analyticsService} exportOpen={exportOpen} historyOpen={historyOpen} historyVersionId={historyVersionId} analyticsOpen={analyticsOpen}
        moveOpen={moveOpen} moveAnchor={moveAnchor} onExport={exportPage} database={databaseBlock?.history}
        onCloseExport={() => { setExportOpen(false); close(); }}
        onCloseHistory={() => { setHistoryOpen(false); close(); }}
        onCloseAnalytics={() => { setAnalyticsOpen(false); close(); }}
        onHistoryFromAnalytics={(versionId) => { setAnalyticsOpen(false); setHistoryVersionId(versionId); setHistoryOpen(true); }}
        onCloseMove={() => { setMoveOpen(false); close(); }}
        onPageChange={onPageChange} onStatus={onStatus} />
      {databaseBlock?.dialog}
    </>
  );
}
