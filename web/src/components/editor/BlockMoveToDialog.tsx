"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal, flushSync } from "react-dom";
import { useTranslation } from "react-i18next";
import { i18next } from "@/i18n";
import { FileText, LockIcon, Search } from "@/icons/leafen";
import { createBlockMoveService } from "@/lib/blockMoveService";
import { databasePagePlace, pagePlace } from "@/lib/pagePath";
import { pathLeavesOwnPages } from "@/lib/pagePathPieces";
import { usePagePathsAbove } from "@/lib/usePagePathsAbove";
import { createNativePage, pageCreationHere } from "@/lib/pageCreationService";
import { showPageMadeHere } from "@/lib/pageMadeHere";
import { afterPageMade } from "@/lib/pagesNotYetMade";
import { searchWorkspacePages, type PageSearchWindowResult } from "@/lib/workspacePageSearch";
import { keepLayerKeys, useOverlayLayer } from "@/lib/overlayLayer";
import { foldNfcText } from "@/shared/database/natural-order.mjs";
import type { Page, Session } from "@/types";
import { PageIconGlyph } from "../page/PageIcon";
import { PlaceLine } from "../PlaceLine";
import {
  MOVE_SEARCH_DEBOUNCE_MS as BLOCK_MOVE_SEARCH_DEBOUNCE_MS,
  blockMoveDestinations,
  blockMoveTreeChildren,
  mergeMoveRemotePages,
  moveListRows,
  pageMap,
  toggledMoveRow,
  type MoveDestination,
} from "../pageMoveDialogModel";
import { MOVE_LIST_KEYS, MoveGroupHeading, MoveListRows, useMoveListEdges } from "../MoveDestinationRow";
import { MOVE_LOAD_MORE_KEY, MoveLoadMoreRow, moveSearchShown, useMoveSearchWindow } from "../moveSearchWindow";
import { useMovePlaces } from "../useMovePlaces";
import { type MovePopoverAnchor, movePopoverAnchorOf, useMovePopoverStyle } from "../movePopoverPlacement";
import styles from "../MoveToDialog.module.css";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { rememberMoveTarget, useMoveSuggestions } from "../moveSuggestions";
import { useProductPages } from "../product/productPagesContext";
import { ownPresses } from "@/lib/ownPresses";
import { usePageEditRefused } from "@/lib/pageActionAccessContext";

type BlockMoveTitleSearchState = {
  complete: boolean;
  queryKey: string;
  workspaceId: string;
};

/** A page to choose: the move list's (pageMoveDialogModel) page destination. */
type Destination = Extract<MoveDestination, { type: "page" }>;

/**
 * Where blocks go, and the other choices of a page made in the same window
 * ("페이지 추가", "페이지 위치"), as Notion's block "옮기기" (seen 2026-11-10):
 * the page window's list (MoveListRows) — before a search, "추천" over the
 * private pages, the teamspaces and pages, each opening to the pages under
 * it, titles alone; a search's pages under "페이지" with where they are. The
 * page holding the blocks is in the tree, and among the suggestions only as a
 * place last moved into, not in a search: chosen, the blocks go last on it
 * (seen 2026-11-10, 2026-11-22 — pageMoveDialogModel sourcePage). A private
 * page or teamspace row puts nothing there: pressed, it opens or closes, as
 * Notion's "페이지 위치" window's private row (seen 2026-10-02). Notion's
 * "페이지에 추가" (place) keeps its own flat list.
 */
export function BlockMoveToDialog({
  blockId,
  sourcePageId,
  workspaceId: sourceWorkspaceId,
  pages,
  session,
  title = i18next.t("blockMoveToDialog:title"),
  placeholder,
  sourcePage = "browsing",
  place,
  doneMessage,
  failedMessage,
  unavailable,
  hiddenPageIds,
  databases = false,
  databaseMoving = false,
  movingPageIds,
  madeHere = false,
  anchor: givenAnchor,
  onChooseDestination,
  onClose,
  onMoved,
  onPageChange,
  onStatus,
}: {
  blockId: string;
  sourcePageId: string;
  /** The source page's workspace, where the destinations are. */
  workspaceId: string;
  pages: Page[];
  session: Session;
  title?: string;
  /** The search field's prompt, for a choice other than moving blocks. */
  placeholder?: string;
  /** Whether the page holding the block is a choice: always, never, or before a search (pageMoveDialogModel). */
  sourcePage?: "always" | "browsing" | "never";
  /**
   * Notion's picker of a page to put something on (its file preview's
   * "페이지에 추가", seen 2026-11-06): the page it is on comes first, tagged
   * as the current page; a search that finds none says so; and under the
   * list a new private page is always a choice, in place of making one by
   * the search's name.
   */
  place?: { current: string; none: string; create: string };
  /** What is said once done (none when null) or when it fails, other than moving. */
  doneMessage?: string | null;
  failedMessage?: string;
  /** Why a page can't be chosen for this choice, if it can't. */
  unavailable?: (page: Page) => string | undefined;
  /** Pages that are no choice at all, as the database a source moves out of. */
  hiddenPageIds?: readonly string[];
  /** Whether a page in the tree opens to the databases its body holds too, which are choices (blockMoveTreeChildren). */
  databases?: boolean;
  /** Whether what moves is or holds a database, which no database takes: databases are dimmed, or left out unsearched (databaseRefused). */
  databaseMoving?: boolean;
  /** The pages moving with the blocks, which can't take them (BlockMoveInput.movingPageIds). */
  movingPageIds?: readonly string[];
  /**
   * Whether what takes the move over (onChooseDestination) waits itself for
   * a page made on this device until the server has it (afterPageMade): its
   * new page is then chosen at once. The dialog's own move waits for it.
   */
  madeHere?: boolean;
  /** Where it hangs when what it moves isn't on the screen, as under a sidebar row's menu. */
  anchor?: MovePopoverAnchor | null;
  /**
   * Takes the move over, with the page chosen (a full-page database's takes
   * the blocks as its rows); what it answers runs once the dialog has
   * closed, and focus is its to place.
   */
  onChooseDestination?: (destinationPageId: string, signal: AbortSignal, destination: Page) => Promise<void | (() => void)> | void;
  onClose: () => void;
  /** Once the blocks are on the destination page. */
  onMoved?: () => void;
  onPageChange: (page: Page) => void;
  onStatus?: (message: string, tone?: "default" | "success" | "error") => void;
}) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const resultsRef = useRef<HTMLDivElement>(null);
  const restoreOnClose = useRef(true);
  const operationRef = useRef<AbortController | null>(null);
  const { t } = useTranslation("blockMoveToDialog");
  const [query, setQuery] = useState("");
  const { shown, loadMore, remoteLimit } = useMoveSearchWindow(query);
  const [remotePagesById, setRemotePagesById] = useState<Record<string, Page>>({});
  const [databasePages, setDatabasePages] = useState<NonNullable<PageSearchWindowResult["databasePages"]>>({});
  const [titleSearchState, setTitleSearchState] = useState<BlockMoveTitleSearchState | null>(null);
  // The suggestions opened to the pages under them, by their place in the tree.
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  const pagesById = useMemo(() => pageMap(pages), [pages]);
  const blockMove = useMemo(() => createBlockMoveService(session), [session]);
  const workspaceId = sourceWorkspaceId.trim();
  const places = useMovePlaces(session, place ? "" : workspaceId);
  const { recentPageIds, movedToPageIds } = useMoveSuggestions(session, workspaceId);
  const hiddenKey = hiddenPageIds?.join(" ") ?? "";
  const movingPageKey = movingPageIds?.join(" ") ?? "";
  // A page the person can't edit takes nothing (the server refuses it): its
  // access is read for the pages shown, and a page read so is left out of a
  // place picker, as the current page among them, and dimmed with why in a
  // move's list, where the pages under it stay in reach.
  const [shownPageIds, setShownPageIds] = useState("");
  const editRefused = usePageEditRefused(useMemo(() => shownPageIds.split(" ").filter(Boolean), [shownPageIds]));
  const searchablePagesById = useMemo(() => {
    const all: Record<string, Page> = { ...pagesById, ...remotePagesById };
    for (const id of hiddenKey.split(" ")) delete all[id];
    if (place) for (const id of Object.keys(all)) if (editRefused(id)) delete all[id];
    return all;
  }, [editRefused, hiddenKey, pagesById, place, remotePagesById]);
  const queryKey = foldNfcText(query.trim());
  const titleSearchComplete = titleSearchState?.complete === true
    && titleSearchState.queryKey === queryKey
    && titleSearchState.workspaceId === workspaceId;

  useEffect(() => {
    const trimmedQuery = query.trim();
    if (!trimmedQuery || !workspaceId) {
      setTitleSearchState(null);
      return;
    }
    let cancelled = false;
    const controller = new AbortController();
    setTitleSearchState(null);
    const timer = window.setTimeout(() => {
      void searchWorkspacePages(session, trimmedQuery, remoteLimit, workspaceId, { signal: controller.signal, places: true })
        .then((result) => {
          if (cancelled) return;
          setRemotePagesById((current) => mergeMoveRemotePages(current, result.pages, workspaceId));
          setDatabasePages((current) => ({ ...current, ...result.databasePages }));
          setTitleSearchState({
            complete: result.hasMore !== true,
            queryKey: foldNfcText(trimmedQuery),
            workspaceId,
          });
        })
        .catch(() => {
          if (cancelled || controller.signal.aborted) return;
          setTitleSearchState({ complete: false, queryKey: foldNfcText(trimmedQuery), workspaceId });
        });
    }, BLOCK_MOVE_SEARCH_DEBOUNCE_MS);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [query, remoteLimit, session, workspaceId]);

  // A database's page, and a page under one, is placed by the path the
  // server names (pagePathPieces.ts), as in search; the list asks below.
  const pathsRead = usePagePathsAbove([]);
  const searching = query.trim().length > 0;
  // The private pages' row opens to those the sidebar's private section lists.
  const productPages = useProductPages();
  const sharedPageIds = productPages?.sharedPageIds;
  const allInlineDatabases = productPages?.inlineDatabases;
  const inlineDatabases = useMemo(() => databases ? (allInlineDatabases ?? []).filter((database) => database.workspaceId === workspaceId
    && !hiddenKey.split(" ").includes(database.containerId)) : undefined, [allInlineDatabases, databases, hiddenKey, workspaceId]);
  const { rows, more } = useMemo(() => {
    const input = {
      pagesById: searchablePagesById, workspaceId, sourcePageId, sourcePage, sharedPageIds: new Set(sharedPageIds ?? []), inlineDatabases,
      unavailable: (page: Page) => unavailable?.(page) ?? (editRefused(page.id) ? t("blockMoveToDialog:results.cannotEdit") : undefined), databaseMoving,
      movingPageIds: movingPageKey ? movingPageKey.split(" ") : [],
      location: (page: Page) => databasePages[page.id]
        ? databasePagePlace(databasePages[page.id].containerPageId, searchablePagesById, databasePages[page.id], page.id)
        : pagePlace(page, searchablePagesById),
    };
    // A search shows its answers twenty at a time, as a page's window (moveSearchWindow.tsx).
    const { found, more } = moveSearchShown(blockMoveDestinations({ ...input, places: place ? [] : places, query, sourceFirst: Boolean(place),
      recentPageIds, movedToPageIds }), searching, shown);
    return { more, rows: moveListRows({ destinations: found, searching: searching || Boolean(place), expanded,
      childrenOf: (destination) => blockMoveTreeChildren(destination, input) }) };
    // The paths read so far place them again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [databaseMoving, databasePages, editRefused, expanded, inlineDatabases, movedToPageIds, movingPageKey, place, places, query, recentPageIds, searchablePagesById, searching, shown, sharedPageIds, sourcePage, sourcePageId, t, unavailable, workspaceId, pathsRead]);
  const destinations = useMemo(() => rows.flatMap((row) => (
    row.kind === "option" && row.destination.type === "page" ? [{ key: row.key, destination: row.destination }] : []
  )), [rows]);
  const shownKey = destinations.map(({ destination }) => destination.id).join(" ");
  useEffect(() => setShownPageIds(shownKey), [shownKey]);
  usePagePathsAbove(useMemo(() => destinations.flatMap(({ destination: { page } }) => (
    databasePages[page.id] || pathLeavesOwnPages(page, (id) => searchablePagesById[id]) ? [page] : []
  )), [databasePages, destinations, searchablePagesById]));

  // Closed after a move, focus stays where the move leads.
  const close = useCallback((restoreFocus = true) => {
    operationRef.current?.abort();
    operationRef.current = null;
    blockMove.cancel();
    restoreOnClose.current = restoreFocus;
    onClose();
  }, [blockMove, onClose]);

  // A modal layer: Escape closes it once nothing opened over it takes the key.
  const { zIndex } = useOverlayLayer(Boolean(workspaceId), {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: () => close(),
    initialFocus: inputRef, shouldRestoreFocus: () => restoreOnClose.current,
  });

  const moveTo = useCallback(async (destination: Page, operation: AbortController) => {
    let afterClose: void | (() => void) = undefined;
    if (onChooseDestination) afterClose = await onChooseDestination(destination.id, operation.signal, destination);
    else {
      // Its own move closes the dialog at the press (user decision
      // 2026-11-12: a press answers on the next frame — it stayed up until
      // the server's move job was done, a few round trips): the block goes
      // once the page it goes to is made, one made here first included
      // (pagesNotYetMade), by a move of its own that closing doesn't cancel;
      // done, what follows a move runs, and refused, it says so.
      const moves = createBlockMoveService(session);
      void afterPageMade(destination.id).then(() => moves.move({ blockId, destinationPageId: destination.id, sourcePageId }))
        .then(() => {
          onMoved?.();
          if (doneMessage) onStatus?.(doneMessage, "success");
        }, () => onStatus?.(failedMessage ?? i18next.t("blockMoveToDialog:toast.moveFailed"), "error"));
      if (operationRef.current !== operation || operation.signal.aborted) return false;
      operationRef.current = null;
      close();
      return true;
    }
    onMoved?.();
    if (operationRef.current !== operation || operation.signal.aborted) return false;
    // Blocks move without a notice, as Notion's (seen 2026-11-03).
    if (doneMessage) onStatus?.(doneMessage, "success");
    operationRef.current = null;
    if (afterClose) {
      flushSync(() => close(false));
      afterClose();
    } else close();
    return true;
  }, [blockId, close, doneMessage, failedMessage, onChooseDestination, onMoved, onStatus, session, sourcePageId]);

  const choose = useCallback(async (destination: Destination) => {
    if (operationRef.current) return;
    if (destination.disabled) {
      onStatus?.(destination.disabledReason ?? t("blockMoveToDialog:toast.locked"), "default");
      return;
    }
    if (!pagesById[destination.id]) onPageChange(destination.page);
    rememberMoveTarget(session, workspaceId, destination.id);
    const operation = new AbortController();
    operationRef.current = operation;
    try {
      await moveTo(destination.page, operation);
    } catch {
      if (operationRef.current === operation && !operation.signal.aborted) {
        onStatus?.(failedMessage ?? t("blockMoveToDialog:toast.moveFailed"), "error");
        operationRef.current = null;
      }
    }
  }, [failedMessage, moveTo, onPageChange, onStatus, pagesById, session, t, workspaceId]);

  // The new private page shows in the press's own frame, in the sidebar,
  // and is chosen at once where what moves waits for it (madeHere), as
  // Notion's (user decision 2026-11-12: a press answers on the next frame —
  // it waited for the page to be kept here first): made on this device and
  // kept behind the press (showPageMadeHere); without an outbox, or for what
  // takes the move over without waiting, the server makes it first.
  const outbox = productPages?.mutationOutbox;
  const newDestination = useCallback((title: string, signal: AbortSignal): Page | Promise<Page> => {
    if (outbox && (madeHere || !onChooseDestination)) {
      const creation = pageCreationHere(session, { siblings: pages, title, workspaceId });
      // Refused, the sidebar shows what the server holds.
      void showPageMadeHere(outbox, session, creation, { announce: (message, tone) => onStatus?.(message, tone), made: onPageChange,
        refused: () => void productPages?.refreshPages?.()?.catch(() => undefined) });
      return creation.page;
    }
    return createNativePage(session, { parentId: "", signal, title, workspaceId });
  }, [madeHere, onChooseDestination, onPageChange, onStatus, outbox, pages, productPages, session, workspaceId]);

  const createAndMove = useCallback(async () => {
    if (!workspaceId || operationRef.current) return;
    const operation = new AbortController();
    operationRef.current = operation;
    try {
      // Notion's "새 개인 페이지" names no page; the new page starts untitled.
      const page = await newDestination("", operation.signal);
      if (operationRef.current !== operation || operation.signal.aborted) return;
      onPageChange(page);
      await moveTo(page, operation);
    } catch {
      if (operationRef.current === operation && !operation.signal.aborted) {
        onStatus?.(failedMessage ?? t("blockMoveToDialog:toast.moveFailed"), "error");
        operationRef.current = null;
      }
    }
  }, [failedMessage, moveTo, newDestination, onPageChange, onStatus, t, workspaceId]);

  const optionCount = rows.filter((row) => row.kind === "option").length;
  // A search offers what it finds and nothing else, as Notion's (seen
  // 2026-11-13); only "페이지에 추가" (place) has a new page under its list.
  const itemCount = optionCount + (place ? 1 : 0);
  // As Notion's (seen 2026-11-04), it hangs right under the block it moves,
  // or over it near the bottom, whole, moved up over it where neither side
  // holds it (seen 2026-11-14, a database page's line); from the block's
  // menu, right under the handle.
  const [anchor] = useState(() => givenAnchor !== undefined ? givenAnchor : typeof document === "undefined" ? null
    : movePopoverAnchorOf(document.querySelector(`[data-block-id="${CSS.escape(blockId)}"]`)));
  // A place picker is at most 360px tall, as Notion's, a block's list 420 (MoveToDialog.module.css).
  const whole = givenAnchor === undefined;
  const popoverStyle = useMovePopoverStyle(anchor, dialogRef, `${itemCount}:${query}`, 0, place ? 360 : whole ? 420 : undefined, whole);
  useEffect(() => {
    return () => {
      operationRef.current?.abort();
      operationRef.current = null;
      blockMove.cancel();
    };
  }, [blockMove]);

  const choices: Array<{ id: string; label: string; destination?: MoveDestination }> = [
    ...rows.flatMap(row => row.kind === "option" ? [{ id: row.key, label: row.destination.label, destination: row.destination }] : []),
    ...(more ? [{ id: MOVE_LOAD_MORE_KEY, label: t("moveToDialog:results.loadMore") }] : []),
    ...(place ? [{ id: "__create", label: place.create }] : []),
  ];
  const toggle = (key: string) => setExpanded((current) => toggledMoveRow(current, key));
  const listEdges = useMoveListEdges(resultsRef, `${itemCount}:${rows.length}`);
  const picker = useNativeAutocomplete({ items: choices, query, onQuery: setQuery, inputRef, listRef: resultsRef,
    // A choice other than moving names its search by its own prompt, not "where to move".
    popoverRef: dialogRef, label: placeholder ? placeholder.replace(/(\.\.\.|…)$/, "") : t("blockMoveToDialog:search.label"),
    open: Boolean(workspaceId), commitTab: false,
    // The suggestions wait for an arrow key, the pointer grounding the page
    // under it apart; a search's first page is chosen, as Notion's (seen
    // 2026-11-06, 2026-11-10, 2026-11-14).
    // A dimmed one too in the move list (MOVE_LIST_KEYS); a place picker's keys are its own.
    highlightFirst: searching && (!place || destinations.length > 0), ...(place ? {} : MOVE_LIST_KEYS),
    disabledKeys: choices.filter(choice => choice.destination?.disabled).map(choice => choice.id),
    keyOf: choice => choice.id, textOf: choice => choice.label,
    onPick: choice => {
      if (choice.destination?.type === "place") toggle(choice.id);
      else if (choice.destination) void choose(choice.destination);
      else if (choice.id === MOVE_LOAD_MORE_KEY) loadMore();
      else void createAndMove();
    } });

  if (!workspaceId) return null;
  // The popover floats over the page; outside it, a click closes it.
  const dialog = (
    <div className={styles.overlay} style={{ zIndex }} {...ownPresses}>
      <button type="button" className={styles.backdrop} onClick={() => close()} tabIndex={-1} aria-label={t("blockMoveToDialog:dialog.closeLabel")} />
      <div ref={dialogRef} className={styles.dialog} style={popoverStyle} role="dialog" aria-modal="true" aria-label={title}
        data-move-popover={anchor ? "anchored" : undefined} data-place={place ? "true" : undefined} data-workspace-foot="none"
        data-searching={searching && !place ? "true" : undefined} onKeyDown={keepLayerKeys}>
        {/* A 16px magnifier, the words 6px after it, as the page's window (seen 2026-11-10). */}
        <div className={styles.searchRow}>
          <Search size="small" aria-hidden="true" />
          <input {...picker.inputProps} ref={inputRef} placeholder={placeholder ?? t("blockMoveToDialog:search.placeholder")} />
        </div>
        <div {...picker.listBoxProps} {...listEdges} className={styles.results} ref={resultsRef}
          onPointerLeave={() => { if (!searching) picker.state.selectionManager.setFocusedKey(null); }}>
          {place ? destinations.length > 0 && !query.trim() ? (
            <MoveGroupHeading group="suggested">{t("blockMoveToDialog:results.suggested")}</MoveGroupHeading>
          ) : destinations.length === 0 && (!query.trim() || titleSearchComplete) ? (
            <div className={styles.groupHeading} role="status" data-move-group>{place.none}</div>
          ) : null : null}
          {place ? destinations.map(({ destination }) => (
            <NativePickerOption key={destination.id} state={picker.state} itemKey={destination.id} className={styles.result}
              data-disabled={destination.disabled ? "true" : undefined} disabled={destination.disabled}>
              {({ labelProps, placeProps, descriptionProps }) => <>
              <span className={styles.icon}><PageIconGlyph page={destination.page} size={17} /></span>
              <span className={styles.text}>
                <span {...labelProps}>{destination.label}</span>
                {destination.disabledReason ? <span {...descriptionProps}>{destination.disabledReason}</span>
                  : destination.where.length ? <span {...placeProps}><PlaceLine place={destination.where} /></span> : null}
              </span>
              {destination.disabled && <LockIcon className={styles.lock} size="tiny" aria-hidden="true" />}
              {place && !query.trim() && destination.id === sourcePageId ? <span className={styles.currentTag}>{place.current}</span> : null}
              </>}
            </NativePickerOption>
          )) : <MoveListRows rows={rows} state={picker.state} owner={{ id: session.user.id, name: session.user.name.trim() || session.user.email }}
            heading={(group) => t(`blockMoveToDialog:results.${group}`)}
            onToggle={searching ? undefined : toggle} />}
          {more && <MoveLoadMoreRow state={picker.state} />}
          {optionCount === 0 && !place && (!searching || titleSearchComplete)
            && <div className={styles.empty} role="status">{t("blockMoveToDialog:results.empty")}</div>}
          {place ? (
            <NativePickerOption state={picker.state} itemKey="__create" className={`${styles.result} ${styles.placeCreate}`}
              data-move-create="private">
              {({ labelProps }) => <>
              <span className={styles.icon}><FileText size="regular" aria-hidden="true" /></span>
              <span className={styles.text}><span {...labelProps}>{place.create}</span></span>
              </>}
            </NativePickerOption>
          ) : null}
        </div>
      </div>
    </div>
  );
  return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}
