"use client";
import { pagesByIdOf } from "@/lib/pageIndex";

import type { ProductStatusSink } from "@/lib/productStatus";
import {
  type MouseEvent as ReactMouseEvent,
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import { isLegalHoldPermanentDeleteError } from "@/lib/nativeTrashLifecycle";
import { pagePermanentDeleteErrorCodes } from "@/pagePermanentDeleteWire.generated";
import { openPageInNewTab } from "@/lib/databasePageLink";
import { inlineDatabaseFullPageHref } from "@/lib/inlineDatabaseFullPage";
import { pushRoute } from "@/lib/router";
import { pagePath, pagePlace, placeText, placeUnder, type Place } from "@/lib/pagePath";
import { knownPathAboveId } from "@/lib/pagePathPieces";
import { usePagePathsAbove } from "@/lib/usePagePathsAbove";
import { pageNamesNow } from "@/lib/pageNames";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { Page, Session, Workspace } from "@/types";
import { DatabaseCylinderIcon, RestoreIcon, TableIcon, Trash } from "@/icons/leafen";
import { SearchClearButton } from "./PageActionSearch";
import { deleteDeletedDatabase, deletedDatabaseName, restoreDeletedDatabase, useDeletedDatabases } from "@/lib/deletedDatabases";
import type { DeletedDatabase, DeletedDatabaseRestored } from "@/databaseDataSourceWire.generated";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { confirmAction } from "./ConfirmActionDialog";
import { HoverTip } from "./IconTipButton";
import { PlaceLine } from "./PlaceLine";
import { TrashFilterBar } from "./TrashFilters";
import { TrashPhoneSheet, type TrashEntry } from "./TrashPhoneSheet";
import { TrashNoMatches } from "./TrashNoMatches";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import { initialTrashFilters, searchTrash, trashFilterMatches, trashLineMoment, trashSearchHits, type TrashFilterState } from "./trashFilterModel";
import trashStyles from "./TrashView.module.css";
import { untitledPageDisplayTitle } from "../lib/pageTitle";
import { unansweredReadText } from "@/lib/unansweredRead";
import { heldDatabaseKey, useTrashLegalHolds } from "@/lib/trashLegalHolds";

function pageTitle(page: Page) {
  return pageNamesNow.title(page);
}

/** A database's line, named as its database is everywhere (its own name,
 * else its sources' — "A 및 B"); a source kept in its database by its own. */
function databaseLineTitle(item: DeletedDatabase) {
  return item.inPlace ? item.title || untitledPageDisplayTitle("database") : deletedDatabaseName(item.title, item.sourceTitles);
}

/** The sidebar's "더 보기" the trash opens from, when the sidebar shows it
 * (a closed one stays in the page, slid away and inert). */
function sidebarMoreButton() {
  const button = document.querySelector<HTMLElement>("[data-sidebar-more]");
  return button && !button.closest("[inert]") && button.getClientRects().length ? button : null;
}

/**
 * The trash, as Notion's (seen 2026-09-30): a window over the sidebar's
 * foot with a search, the last editor (the reader at first), where to look
 * and teamspace filters, a row per deleted page with its place and buttons
 * to restore it or delete it forever, and at its foot how long pages stay.
 * A deleted inline database is there too, as in Notion: restoring it puts
 * its block back at the end of its page, where it opens alone as a page.
 */
export function TrashView({
  initialQuery = "",
  canPermanentlyDelete,
  onClose,
  onDeletePage,
  onOpenPage,
  onRestorePage,
  onStatus,
  pages,
  session,
  workspace,
  locationOf,
  restoresToPrivatePages,
  onDatabaseRestored,
}: {
  /** What it opens searching for, as from search's "삭제된 페이지 검색". */
  initialQuery?: string;
  canPermanentlyDelete: boolean;
  onClose(): void;
  onDeletePage(page: Page): Promise<void>;
  onOpenPage(page: Page): void;
  onRestorePage(page: Page): Promise<void>;
  onStatus: ProductStatusSink;
  pages: Page[];
  session: Session;
  workspace: Workspace;
  /** Where a page sits where its parents don't say it, as a database page in an inline database. */
  locationOf?(page: Page, pagesById: Record<string, Page>): Place | undefined;
  /** A page whose restore takes it to the reader's private pages, its parent
   *  being deleted: a page of a data source in the trash. */
  restoresToPrivatePages?(page: Page): boolean;
  /** Opens a database the trash brought back, `restored` naming where it is now. */
  onDatabaseRestored?(restored: DeletedDatabaseRestored, item: DeletedDatabase): Promise<void>;
}) {
  const { t } = useTranslation(["trashView", "common"]);
  // A phone's trash is a sheet over the whole screen, as Notion's (seen 2026-11-10);
  // a wider touch screen's the sheet in its middle, as Notion's iPad (seen 2026-11-13).
  const { phone: phoneScreen, touch } = useSidebarLayout(true);
  const phone = phoneScreen || touch;
  const workspaceId = workspace.id;
  const pagesById = pagesByIdOf(pages);
  const trashed = useMemo(
    () => pages
      .filter((page) => (
        page.workspaceId === workspaceId
        && page.inTrash
        // A full-page database's page left as a signpost stays out of the list.
        && !page.movedDatabase
      ))
      .sort((a, b) => trashLineMoment(b.trashedAt) - trashLineMoment(a.trashedAt) || a.id.localeCompare(b.id)),
    [pages, workspaceId],
  );
  const [query, setQuery] = useState(initialQuery);
  const [filters, setFilters] = useState<TrashFilterState>(
    () => initialTrashFilters({ userId: session.user.id, label: t("trashView:filters.me") }),
  );
  const deletedDatabases = useDeletedDatabases(session, workspaceId);
  // A page a legal hold keeps says so on its delete before the press.
  const held = useTrashLegalHolds(session, workspaceId, canPermanentlyDelete);
  const refusedWhy = (key: string, database = false) => !canPermanentlyDelete ? t("trashView:deleteForeverNotAllowed")
    : held.has(key) ? t(database ? "trashView:confirm.legalHoldErrorDatabase" : "trashView:confirm.legalHoldError") : undefined;
  const [actionBusy, setActionBusy] = useState(false);
  const popoverRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  // The trash is a layer over the page; its delete confirmation, the app's
  // own over it, takes Escape first.
  const { zIndex: popoverLayer } = useOverlayLayer(!phone, { kind: "panel", ref: popoverRef, onEscape: onClose });
  // Notion's stands over the sidebar's "더 보기" (seen 2026-11-01): its left
  // edge at the button's, its foot 6px over it, kept 12px inside the window.
  // With no button on screen (a closed sidebar, a phone) it keeps its place.
  const [moreButton] = useState(() => (phone ? null : sidebarMoreButton()));
  const placement = usePopoverPlacement(popoverRef, moreButton
    ? (size, viewport) => placeAround(popoverRectOf(moreButton)!, size, viewport, { prefer: "above", gap: 6 })
    : null, [moreButton], undefined, false);
  const listRef = useRef<HTMLDivElement>(null);

  // The search takes the keys as soon as the trash opens.
  useLayoutEffect(() => {
    searchRef.current?.focus();
  }, []);

  // A line is found by the words it shows: its name, an untitled one's too, and where it was.
  const pageText = useCallback((page: Page) => `${pageTitle(page)} ${pagePath(page, pagesById)}`, [pagesById]);
  // A database sits where the page holding it is, named down to that page; a
  // full-page database's page where that page sits — as Notion's trash shows
  // a database (seen 2026-11-11). A data source kept in its database sits
  // through that database, as Notion's (seen 2026-11-12: "… / W639 H db", an
  // inline one by its name, its sources' while it has none — "… / I one 및 I
  // second").
  // One a database's page holds — a row's body, a template's — sits below
  // that page, which the page list doesn't hold: the path the server reads
  // above it, then its name, as Notion's trash places one from a row's body
  // (seen 2026-11-15: "한지테스트페이지 / ... / 행 하나").
  const pathsRead = usePagePathsAbove(useMemo(() => deletedDatabases.items
    .filter((item) => item.holderRow && !pagesById[item.pageId]).map((item) => ({ id: item.pageId, workspaceId })),
  [deletedDatabases.items, pagesById, workspaceId]));
  const databasePlace = useCallback((item: DeletedDatabase) => {
    const holder = pagesById[item.pageId];
    if (!holder && item.holderRow) {
      return placeUnder(knownPathAboveId(item.pageId) ?? "", pagesById, item.holderTitle || untitledPageDisplayTitle("page"));
    }
    if (!holder) return undefined;
    if (item.inPlace) {
      if (holder.kind === "database") return placeUnder(holder.id, pagesById);
      return placeUnder(holder.id, pagesById, deletedDatabaseName(item.databaseTitle, item.sourceTitles));
    }
    return holder.kind === "database" ? pagePlace(holder, pagesById) : placeUnder(holder.id, pagesById);
  }, [pagesById, pathsRead]);
  const databaseText = useCallback((item: DeletedDatabase) =>
    `${databaseLineTitle(item)} ${placeText(databasePlace(item) ?? [])}`, [databasePlace]);
  // A line being restored leaves the list at the press, as Notion's (below).
  const [leaving, setLeaving] = useState<ReadonlySet<string>>(() => new Set());
  const filtered = useMemo(() => searchTrash(trashed.filter((page) => !leaving.has(page.id) && trashFilterMatches(page, filters, pagesById)),
    query, pageText), [filters, leaving, pageText, pagesById, query, trashed]);
  const databases = useMemo(() => searchTrash(deletedDatabases.items, query, databaseText),
    [databaseText, deletedDatabases.items, query]);
  // Pages and databases rank together, the lines holding more of the words
  // first, then the latest sent there, as Notion's trash lists a data source
  // between the pages that went there before and after it (seen 2026-11-12) —
  // what went there with what holds it at its own last edit, as Notion's
  // places "L inline" below the rows added to it after; of those at one
  // moment, a page before what went with it, as Notion's lists a trashed
  // page above the pages, databases and rows in it.
  const hits = new Map<string, number>([...filtered.map((page) => [page.id, trashSearchHits(query, pageText(page))] as const),
    ...databases.map((item) => [`database:${item.blockId}`, trashSearchHits(query, databaseText(item))] as const)]);
  const sentAt = new Map<string, number>([...filtered.map((page) => [page.id, trashLineMoment(page.trashedAt)] as const),
    ...databases.map((item) => [`database:${item.blockId}`, item.editedAt || item.deletedAt] as const)]);
  const depthOf = (pageId: string) => {
    let depth = 0;
    const seen = new Set<string>();
    for (let id = pagesById[pageId]?.parentId; id && !seen.has(id); id = pagesById[id]?.parentId) {
      seen.add(id);
      depth += 1;
    }
    return depth;
  };
  const depth = new Map<string, number>([...filtered.map((page) => [page.id, depthOf(page.id)] as const),
    ...databases.map((item) => [`database:${item.blockId}`, depthOf(item.pageId) + 1] as const)]);

  async function restoreDatabase(item: DeletedDatabase) {
    if (actionBusy) return;
    // A database in a page in the trash comes back as a full-page database in
    // private pages, as Notion's after it asks as for a page there (seen
    // 2026-11-12: "T inline").
    if (item.inTrashedPage && !(await confirmAction({
      message: t("trashView:confirm.restoreToPrivate"), detail: t("trashView:confirm.restoreToPrivateDetail"),
      confirmLabel: t("trashView:actions.restore"), accent: true,
    }))) return;
    // The trash closes at the press, as a page's restore closes it; the
    // database opens once the server has it back, or a notice says it couldn't.
    onClose();
    try {
      await onDatabaseRestored?.(await restoreDeletedDatabase(session, workspaceId, item.blockId), item);
    } catch {
      onStatus(t("trashView:toast.couldntRestore"), "error");
    }
  }

  // Notion asks before a page goes for good (seen 2026-10-28, 10-29) in its
  // confirmation card, the red-outlined
  // "영구 삭제하기" taking focus above "취소" (2026-11-15, worker Chrome, no
  // script before the press: document.activeElement read after the card
  // showed was that button, in both). The focus back on the list once it is
  // gone is not Notion's measured: nothing of Notion's is deleted for good.
  async function deleteForever(run: () => Promise<void>) {
    if (actionBusy) return;
    const confirmed = await confirmAction({
      message: t("trashView:confirm.deletePage"), confirmLabel: t("trashView:rowBanner.deleteForever"), danger: true,
    });
    if (!confirmed) return;
    setActionBusy(true);
    try {
      await run();
      onStatus(t("trashView:toast.deletedForever"), "default");
      window.requestAnimationFrame(() => listRef.current?.focus());
    } catch (error) {
      onStatus(t(isLegalHoldPermanentDeleteError(error) ? "trashView:confirm.legalHoldError"
        : (error as { code?: unknown } | null)?.code === pagePermanentDeleteErrorCodes.conflict ? "trashView:toast.deletingAlready" : "trashView:toast.couldntDelete"), "error");
    } finally {
      setActionBusy(false);
    }
  }

  async function restoreFromTrash(page: Page) {
    if (actionBusy) return;
    // Notion asks first when the page comes back to private pages (seen
    // 2026-11-12 with a page of a data source in the trash): "복원" in blue
    // above "취소".
    if (restoresToPrivatePages?.(page) && !(await confirmAction({
      message: t("trashView:confirm.restoreToPrivate"), detail: t("trashView:confirm.restoreToPrivateDetail"),
      confirmLabel: t("trashView:actions.restore"), accent: true,
    }))) return;
    setActionBusy(true);
    // Its line leaves the list at the press; the trash closes and what came
    // back opens once it can (a page at once, a database's row once the
    // server has it back), as Notion's (a row's, seen twice 2026-11-16 in
    // Chrome: "표시할 결과 없음" on the next screen, then the row's page).
    // A refusal puts the line back.
    setLeaving((ids) => new Set(ids).add(page.id));
    try {
      await onRestorePage(page);
      // Notion closes the trash and opens what came back, without a notice
      // (seen 2026-11-06 with a page and a database).
      onOpenPage(page);
      onClose();
    } catch {
      setLeaving((ids) => {
        const next = new Set(ids);
        next.delete(page.id);
        return next;
      });
      onStatus(t("trashView:toast.couldntRestore"), "error");
    } finally {
      setActionBusy(false);
    }
  }

  function openTrashedPage(page: Page, event: ReactMouseEvent<HTMLElement>) {
    if (event.metaKey || event.ctrlKey) {
      openPageInNewTab(page, session.user.id);
      return;
    }
    onOpenPage(page);
    onClose();
  }

  // A database's line opens it alone, under the trash's banner, as Notion's
  // (seen 2026-11-24, an inline one sent there alone, two tabs): at its own
  // address in its page (lib/trashedDatabaseAlone.ts). A source kept in its
  // database, and one in a database page's body, have no line to open.
  function openTrashedDatabase(item: DeletedDatabase, newTab: boolean) {
    const href = inlineDatabaseFullPageHref(item.pageId, item.blockId);
    if (newTab) {
      window.open(href, "_blank", "noopener,noreferrer");
      return;
    }
    pushRoute(href);
    onClose();
  }

  // A line per trashed page and deleted database, the lines holding more of
  // the searched words first.
  const entries: TrashEntry[] = [
    ...filtered.map((page): TrashEntry => ({
      key: page.id, title: pageTitle(page), page, place: locationOf?.(page, pagesById) ?? pagePlace(page, pagesById),
      restore: () => void restoreFromTrash(page),
      deleteForever: refusedWhy(page.id) ? undefined : () => void deleteForever(() => onDeletePage(page)),
      deleteRefused: refusedWhy(page.id),
    })),
    ...databases.map((item): TrashEntry => {
      return {
        key: `database:${item.blockId}`, title: databaseLineTitle(item), databaseId: item.blockId,
        source: item.inPlace, place: databasePlace(item),
        restore: () => void restoreDatabase(item),
        open: item.inPlace || item.holderRow ? undefined : (newTab) => openTrashedDatabase(item, newTab),
        // A database a legal hold keeps, by its page, says so as a page does.
        deleteForever: refusedWhy(heldDatabaseKey(item.blockId), true) ? undefined : () => void deleteForever(async () => {
          await deleteDeletedDatabase(session, workspaceId, item.blockId);
          deletedDatabases.reload();
        }),
        deleteRefused: refusedWhy(heldDatabaseKey(item.blockId), true),
      };
    }),
  ].sort((a, b) => (hits.get(b.key) ?? 0) - (hits.get(a.key) ?? 0) || (sentAt.get(b.key) ?? 0) - (sentAt.get(a.key) ?? 0)
    || (depth.get(a.key) ?? 0) - (depth.get(b.key) ?? 0));
  const emptyTrash = trashed.length === 0 && deletedDatabases.items.length === 0;

  if (phone) {
    return (
      <TouchSheetLayout>
        <TrashPhoneSheet query={query} onQuery={setQuery} filters={filters} onFilters={setFilters} entries={entries}
          unanswered={emptyTrash && deletedDatabases.unanswered} wide={!phoneScreen}
          busy={actionBusy} listRef={listRef} pages={pages} session={session} workspace={workspace} onClose={onClose}
          onOpen={(page) => { onOpenPage(page); onClose(); }} />
      </TouchSheetLayout>
    );
  }

  return (
    <>
      <button type="button" className={trashStyles.popoverBackdrop} tabIndex={-1} aria-label={t("trashView:close")}
        style={popoverLayer === undefined ? undefined : { zIndex: popoverLayer - 1 }}
        onClick={onClose} />
      <div ref={popoverRef} className={trashStyles.popover} role="dialog" aria-label={t("trashView:trash")}
        data-trash-popover data-anchored={moreButton ? "true" : undefined}
        style={{ ...(moreButton ? popoverPlacementStyle(placement) : {}), zIndex: popoverLayer }}>
        <div className={trashStyles.search}>
          <input
            ref={searchRef}
            value={query}
            aria-label={t("trashView:searchTrash")}
            placeholder={t("trashView:searchTrash")}
            onChange={(e) => setQuery(e.target.value)}
          />
          {/* Words in it bring Notion's shared "입력 값 지우기" (seen 2026-11-15, and
              2026-11-17 at 1440 with no tooltip): it empties the search, the keys staying in it. */}
          <SearchClearButton value={query} inputRef={searchRef} onClear={() => setQuery("")} />
        </div>
        <TrashFilterBar filters={filters} onChange={setFilters} pages={pages} session={session} workspace={workspace} />
        <div
          ref={listRef}
          className={trashStyles.list}
          role="list"
          tabIndex={-1}
          aria-label={t("trashView:deletedPages")}
        >
          {emptyTrash && deletedDatabases.unanswered ? (
            // Its databases the server wasn't there to list: not an empty trash.
            <div className={trashStyles.empty} role="status" data-read-unanswered>
              <Trash size="wide" />
              <p>{unansweredReadText()}</p>
            </div>
          ) : emptyTrash ? (
            <div className={trashStyles.empty} role="status">
              <Trash size="wide" />
              <p>{t("trashView:empty")}</p>
            </div>
          ) : entries.length === 0 ? <TrashNoMatches searched={Boolean(query.trim())} /> : entries.map((entry) => {
            // A data source kept in its database shows a data source's icon, as Notion's (seen 2026-11-12);
            // a page its own icon at the page size, 18 in the row's 20px box, as Notion's and the sidebar's (seen 2026-11-16).
            const glyph = entry.page ? <PageIconGlyph page={entry.page} size={18} />
              : entry.source ? <DatabaseCylinderIcon size="regular" aria-hidden="true" /> : <TableIcon size="regular" aria-hidden="true" />;
            const text = (
              <>
                <span className={trashStyles.icon}>{glyph}</span>
                <span className={trashStyles.pageText}>
                  <span className={trashStyles.title}>{entry.title}</span>
                  {entry.place?.length ? <PlaceLine place={entry.place} className={trashStyles.meta} /> : null}
                </span>
              </>
            );
            const page = entry.page;
            return (
              <div key={entry.key} className={trashStyles.row} role="listitem" data-trash-database={entry.databaseId}
                data-trash-placed={entry.place?.length ? "" : undefined}>
                {page ? (
                  <button
                    type="button"
                    className={trashStyles.page}
                    aria-label={t("trashView:aria.openFromTrash", { title: entry.title })}
                    onClick={(e) => openTrashedPage(page, e)}
                    onAuxClick={(e) => {
                      if (e.button === 1) {
                        e.preventDefault();
                        openPageInNewTab(page, session.user.id);
                      }
                    }}
                  >
                    {text}
                  </button>
                ) : entry.open ? (
                  <button type="button" className={trashStyles.page} aria-label={t("trashView:aria.openFromTrash", { title: entry.title })}
                    onClick={(e) => entry.open?.(e.metaKey || e.ctrlKey)}
                    onAuxClick={(e) => { if (e.button === 1) { e.preventDefault(); entry.open?.(true); } }}>
                    {text}
                  </button>
                ) : <span className={trashStyles.page}>{text}</span>}
                <span className={trashStyles.actions}>
                  <button
                    type="button"
                    className={trashStyles.restore}
                    disabled={actionBusy}
                    aria-label={t("trashView:aria.restorePage", { title: entry.title })}
                    title={t("trashView:actions.restore")}
                    data-trash-restore
                    onClick={entry.restore}
                  >
                    <RestoreIcon size="small" aria-hidden="true" />
                  </button>
                  {entry.deleteForever ? (
                    <button
                      type="button"
                      className={trashStyles.delete}
                      disabled={actionBusy}
                      aria-label={t("trashView:aria.deletePageForever", { title: entry.title })}
                      title={t("trashView:actions.deleteForever")}
                      data-trash-delete
                      onClick={entry.deleteForever}
                    >
                      <Trash size="small" aria-hidden="true" />
                    </button>
                  ) : entry.deleteRefused ? (
                    // Dim to all but the workspace's owners, and on what a
                    // legal hold keeps, saying why, as the trashed page's band.
                    <HoverTip label={entry.deleteRefused} className={trashStyles.refused} wide>
                      <button type="button" className={trashStyles.delete} aria-disabled="true"
                        aria-label={t("trashView:aria.deletePageForever", { title: entry.title })}
                        aria-description={entry.deleteRefused} data-trash-delete>
                        <Trash size="small" aria-hidden="true" />
                      </button>
                    </HoverTip>
                  ) : null}
                </span>
              </div>
            );
          })}
        </div>
        {/* Pages go for good 30 days after they went into the trash. */}
        <div className={trashStyles.retention} data-trash-retention><span>{t("trashView:retention")}</span></div>
      </div>
    </>
  );
}
