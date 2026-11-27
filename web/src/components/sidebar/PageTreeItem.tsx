"use client";

import { useSidebarLayout } from "@/components/shell/AppShell";
import { memo, useEffect, useId, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent, type MouseEvent } from "react";
import { useTranslation } from "react-i18next";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { PageRenamePopover, renamePopoverAt, renamePopoverAtPoint } from "@/components/page/PageRenamePopover";
import { presentationBefore, type PagePresentationChange } from "@/components/page/pagePresentation";
import { notePageChangeStep, renameStep } from "@/components/editor/pageUndo";
import { RowMenu } from "@/components/RowMenu";
import { newPagePeekHref, openPagePeek } from "@/lib/pagePeek";
import { pushRoute } from "@/lib/router";
import { markFreshPage } from "@/components/page/freshPages";
import { editorBlockDragPayload } from "@/components/editor/editorDragModel";
import { droppedBlockMove } from "@/components/editor/blockMoveHistory";
import { ArchivedMarkIcon, DotsHorizontal, DragHandleIcon, Plus, SidebarChevronRight } from "@/icons/leafen";
import { pageDuplicationDestinationLocked } from "@/lib/pageDuplicationService";
import { showTypedPageTitle, usePageNames } from "@/lib/pageNames";
import { TitleWithCustomEmoji, usePageDisplayTitle } from "@/components/page/TitleWithCustomEmoji";
import { holdTypingWhile, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { useProductToast } from "@/lib/productToast";
import type { TextSpan } from "@/lib/types";
import { sameTitle } from "@/lib/titleMerge";
import { usePageTreeRowContext } from "./pageTreeContext";
import { usePageTreeRow } from "./pageTreeRowStore";
import { bodyOrderedChildren, pageDropPlan, treeMenuPoint, treePageDragId, type PageTreeDropIntent } from "./pageTreeModel";
import { isFavoriteRowDrag, usePageTreeRowDrag, type PageTreeRowDrag } from "./usePageTreeRowDrag";
import { onTreeRowKeyDown } from "./pageTreeDom";
import { styles } from "./sidebarStyles";
import { pageActionHintKey, usePageActionAccess } from "@/lib/pageActionAccessContext";
import { confirmMovePrivate } from "../movePrivateConfirm";
import { useSectionOrder } from "./sidebarSectionOrder";
import { scrollIntoViewNearest } from "@/lib/scrollIntoViewNearest";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { SidebarFavoriteDatabaseRow } from "./SidebarFavoriteDatabaseRow";
import { inlineStartX } from "@/lib/readingDirection";
import { pressedHere } from "@/lib/ownPresses";
import { hidePageBlockHalo, showPageBlockHalo } from "@/lib/pageBlockHalo";
import { openPastPaint } from "@/lib/openPastPaint";
import { useSettledPage } from "./settledPage";
import { treeEmptyChildrenIndent, treeRowIndent } from "./pageTreeIndent"; import { addFailedNotice } from "./addFailedNotice";

export const PageTreeItem = memo(
  PageTreeItemComponent,
);

function PageTreeItemComponent({
  pageId,
  depth,
  index,
  setSize,
  sharedRoot = false,
  listCopy,
  onFavoriteDrop,
  inArchivedTeamspace = false,
}: {
  pageId: string;
  depth: number;
  /** A page in an archived teamspace the reader still opens, which the recent pages mark as archived, as Notion's (seen 2026-11-14). */
  inArchivedTeamspace?: boolean;
  /** A page heading the Shared section. */
  sharedRoot?: boolean;
  /**
   * A page heading the favorites or recent pages, a copy of its row elsewhere:
   * its opening is its own, and a favorite's menu is the favorites'.
   */
  listCopy?: "favorite" | "recent";
  /** A favorite dragged before or after this one, in the favorites' own order. */
  onFavoriteDrop?: (draggedId: string, edge: "before" | "after") => void;
  index?: number;
  setSize?: number;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["pageTreeItem", "common"]);
  const { notify } = useProductToast();
  const sectionOrder = useSectionOrder();
  const {
    store,
    session,
    pageAccess,
    canCreateRoot,
    onCreateSubpage,
    onDuplicatePage,
    onFavoriteChange,
    onOpenPage,
    onPageChange,
    onPageMetadataChange,
    onStatus,
    onTransferBlock,
    setPageExpanded,
  } = usePageTreeRowContext();
  const rowRef = useRef<HTMLDivElement>(null);
  const actionsButtonRef = useRef<HTMLButtonElement>(null);
  const expandTimerRef = useRef<number | null>(null);
  const operationRef = useRef<AbortController | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const [menuAnchor, setMenuAnchor] = useState<{ x: number; y: number; pressed?: boolean } | null>(null);
  // Where a right click opened the row's menu, for "이름 바꾸기" from it.
  const contextPoint = useRef<{ x: number; y: number } | null>(null);
  const [dragging, setDragging] = useState(false);
  const [drop, setDrop] = useState<PageTreeDropIntent | null>(null);
  const [dropCopy, setDropCopy] = useState(false);
  // Where the rename popover opens, while it is open.
  const [renameAt, setRenameAt] = useState<{ top: number; left: number } | null>(null);
  const renaming = renameAt !== null;
  const renameByKey = useRef(false);
  // The rename open now, one step of the history once closed.
  const renameSteps = useRef<ReturnType<typeof renameStep<PageName>> | null>(null);
  const expansionKey = listCopy ? `${listCopy}:${pageId}` : pageId;
  // The row's own part of the tree's state: it draws again only when that changes.
  const { page: rowPage, childIds, databases, active, expanded, favorite, menuPages } = usePageTreeRow(store, {
    pageId, expansionKey, menuOpen,
  });
  // A touch screen's shown sidebar — a phone's open drawer, a tablet's
  // sidebar — reads its rows' access once the page has come up, so a row it
  // may add to shows "+ ⋯" as Notion's does with no pointer to approach it
  // (seen 2026-11-13 in iPad Safari: every row the person may add to).
  const { phone, touch, sidebarOpen, sidebarCollapsed } = useSidebarLayout(true);
  const touchReads = useSettledPage(phone ? sidebarOpen : touch && !sidebarCollapsed);
  const access = usePageActionAccess(pageId, active || menuOpen || renaming || touchReads);
  // Under the row are its sub-pages, then the databases the page holds in its
  // body, as Notion's sidebar lists them under it (seen 2026-11-02).
  const hasChildren = childIds.length > 0 || databases.length > 0;
  // The page as it is named now, a title being typed in its field as it is typed (lib/pageNames.ts).
  const pageTitle = usePageDisplayTitle(rowPage);
  const titleId = useId();
  // Each level further in, and "하위 페이지 없음" 6px before the page's own title (pageTreeIndent.ts).
  const rowIndent = treeRowIndent(depth);
  const rowStyle = {
    paddingInlineStart: rowIndent,
    "--tree-drag-handle-left": rowIndent,
  } as CSSProperties;
  const emptyChildrenStyle = { paddingInlineStart: treeEmptyChildrenIndent(depth) } as CSSProperties;

  function clearExpandTimer() {
    if (expandTimerRef.current === null) return;
    window.clearTimeout(expandTimerRef.current);
    expandTimerRef.current = null;
  }

  function startOperation() {
    operationRef.current?.abort();
    const controller = new AbortController();
    operationRef.current = controller;
    return controller;
  }

  // The sidebar stays where the reader left it as a page opens, from its
  // own rows, a link or search, as Notion's (seen 2026-11-11 and
  // 2026-11-13: a row pressed stays under the pointer, its favorite's row
  // lit too out of sight); a phone's drawer opens at the open page's row.
  useEffect(() => {
    if (active && phone) scrollIntoViewNearest(rowRef.current);
  }, [active, phone]);

  const shown = Boolean(rowPage);

  // While its menu is open, the page's block in the page open beside washes
  // blue, so the menu's page shows there too, as Notion's (seen 2026-11-13).
  useLayoutEffect(() => {
    if (!menuOpen) return undefined;
    showPageBlockHalo(pageId);
    return () => hidePageBlockHalo(pageId);
  }, [menuOpen, pageId]);

  useEffect(() => () => {
    clearExpandTimer();
    operationRef.current?.abort();
  }, []);

  // A row being renamed stays put; a locked page's row lifts, to be dropped among the favorites.
  const treeDrag = usePageTreeRowDrag(rowRef, pageId, shown, shown && !renaming, Boolean(onFavoriteDrop));

  if (!rowPage) return null;
  const page = rowPage;

  function setExpanded(next: boolean) {
    setPageExpanded(expansionKey, next);
  }

  // A locked page takes sub-pages too, as Notion's row "+" adds them (seen 2026-10-31).
  // The new page opens over the one open, with the place it went to change
  // in its bar, as Notion's (seen 2026-11-01); a phone opens it as the page.
  async function createSubpage(inPeek: boolean) {
    if (!access.canEdit) return;
    await holdTypingWhile(async () => {
      try {
        const created = await onCreateSubpage(page.id);
        // A full-page database took a row, which opened in it.
        if (!created) return;
        setExpanded(true);
        onPageChange(created);
        requestPageTitleFocus(created.id);
        if (!inPeek) {
          onOpenPage(created, false);
          return;
        }
        markFreshPage(created.id);
        pushRoute(newPagePeekHref(created.id));
      } catch (error) {
        notify(...addFailedNotice(error, t("pageTreeItem:pageAccessRequired")));
      }
    });
  }

  function addSubpage(event: MouseEvent) {
    event.stopPropagation();
    void createSubpage(!event.currentTarget.closest('[data-phone="true"]'));
  }

  // "이름 바꾸기" opens Notion's rename popover at the row's "…" (seen
  // 2026-10-17), or at the pointer when a right click opened the menu (seen
  // 2026-10-27); F2 and a double click, which Notion lacks, open it too.
  function beginRename(point?: { x: number; y: number } | null, byKey = false) {
    if (!access.canEdit) return;
    if (page.isLocked) {
      notify(t("pageTreeItem:pageLocked"));
      return;
    }
    renameByKey.current = byKey;
    renameSteps.current = renameStep(pageId, { title: page.title, titleSpans: page.titleSpans ?? [] }, samePageName, saveChange);
    setRenameAt(point ? renamePopoverAtPoint(point) : renamePopoverAt(actionsButtonRef.current) ?? { top: 16, left: 12 });
  }

  function saveTitle(title: string, titleSpans?: TextSpan[]) {
    if (!pageAccess.canEdit(pageId)) return;
    const controller = startOperation();
    const saving = onPageMetadataChange(page, { title, ...(titleSpans ? { titleSpans } : {}) }, controller.signal).then((result) => {
      if (typeof result === "object" && !controller.signal.aborted) onPageChange(result);
      return result;
    });
    renameSteps.current?.saved({ title, titleSpans: titleSpans ?? [] }, saving);
  }

  // A change ⌘Z or ⌘⇧Z saves again, on the page as it is then.
  function saveChange(change: PageName | PagePresentationChange) {
    return onPageMetadataChange(store.get().pagesById[pageId] ?? page, change, new AbortController().signal).then((result) => {
      if (typeof result === "object") onPageChange(result);
      return result;
    });
  }

  // The icon picked is a step of its own.
  function savePresentation(change: PagePresentationChange) {
    if (!pageAccess.canEdit(pageId)) return;
    const before = presentationBefore(page, change);
    const controller = startOperation();
    void onPageMetadataChange(page, change, controller.signal).then((result) => {
      if (typeof result === "object" && !controller.signal.aborted) onPageChange(result);
      if (result !== false) notePageChangeStep(pageId, before, change, saveChange);
    });
  }

  // Closed, the popover leaves focus to the page, as Notion's does (seen
  // 2026-11-07: a focused row kept its ring and buttons); a rename F2 began
  // on the row hands the keys back to it.
  function endRename() {
    setRenameAt(null);
    renameSteps.current?.close();
    renameSteps.current = null;
    if (renameByKey.current) window.requestAnimationFrame(() => rowRef.current?.focus());
  }

  function onRowKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    onTreeRowKeyDown(event, {
      open: (newTab) => onOpenPage(page, newTab),
      expanded,
      setExpanded,
      rename: () => beginRename(null, true),
      menu: () => {
        const rect = rowRef.current?.getBoundingClientRect();
        contextPoint.current = null;
        setMenuAnchor(rect ? { x: inlineStartX(rect, 24, rowRef.current), y: rect.bottom } : null);
        setMenuOpen(true);
      },
    });
  }

  // A drag reads the page list as it is then.
  function canDropPage(draggedId: string, intent: PageTreeDropIntent, copy: boolean) {
    return pageDropPlan({
      copy,
      draggedPageId: draggedId,
      intent,
      pages: store.get().pages,
      targetPageId: pageId,
      targetSharedRoot: sharedRoot,
      canEditPage: pageAccess.canEdit,
      canCreateRoot,
    });
  }

  function canDropBlocks(sourcePageId: string, copy: boolean) {
    return !page.isLocked && pageAccess.canEdit(pageId) && sourcePageId !== pageId
      && (copy || pageAccess.canEdit(sourcePageId));
  }

  async function dropPage(draggedId: string, intent: PageTreeDropIntent, copy: boolean) {
    const { pages, pagesById } = store.get();
    const plan = pageDropPlan({ copy, draggedPageId: draggedId, intent, pages, targetPageId: pageId, targetSharedRoot: sharedRoot,
      canEditPage: pageAccess.canEdit, canCreateRoot });
    const dragged = pagesById[draggedId];
    if (!plan || !dragged) {
      notify(t(copy ? "pageTreeItem:cantCopyPageHere" : "pageTreeItem:cantMovePageHere"));
      return;
    }
    // A page moved to the top of private pages is shared with no one.
    if (!copy && plan.teamspaceId === "" && dragged.teamspaceId && !await confirmMovePrivate(pageNames.title(dragged))) {
      return;
    }
    // Put by hand among the top rows of the private or shared pages, sorted by
    // last edit, it first asks to sort them by hand, as Notion's (seen
    // 2026-11-14), and goes where it was put among them as they showed.
    const kept = sectionOrder && depth === 0 && intent !== "inside" ? await sectionOrder.toManual() : undefined;
    if (kept === null) return;
    const placed = kept?.size ? pageDropPlan({ copy, draggedPageId: draggedId, intent, targetPageId: pageId, targetSharedRoot: sharedRoot,
      pages: pages.map((page) => kept.has(page.id) ? { ...page, position: kept.get(page.id)! } : page),
      canEditPage: pageAccess.canEdit, canCreateRoot }) ?? plan : plan;
    const controller = startOperation();
    try {
      // A copy is made where it goes, so a page under a locked page copies out.
      const subject = copy
        ? await onDuplicatePage(dragged, controller.signal, { parentId: placed.parentId })
        : dragged;
      const result = await onPageMetadataChange(subject, placed, controller.signal);
      if (controller.signal.aborted || result === false) return;
      onPageChange(typeof result === "object" ? result : { ...subject, ...placed });
      if (intent === "inside") setExpanded(true);
    } catch (error) {
      if (!(error instanceof DOMException && error.name === "AbortError")) {
        notify(t(copy && pageDuplicationDestinationLocked(error)
          ? "pageTreeItem:cantCopyIntoLockedPage"
          : copy ? "pageTreeItem:couldntCopyPage" : "pageTreeItem:couldntMovePage"), "error");
      }
    }
  }

  async function dropBlocks(blockIds: string[], sourcePageId: string, copy: boolean) {
    if (blockIds.length === 0 || !canDropBlocks(sourcePageId, copy)) {
      notify(t(copy ? "pageTreeItem:cantCopyBlocksHere" : "pageTreeItem:cantMoveBlocksHere"));
      return;
    }
    // Moved from the page an editor shows, they leave it at the drop and the
    // move is one step of its ⌘Z, with no notice, as Notion's (seen
    // 2026-11-13); refused, they are back and it says so (moveBlocksToPage).
    const move = !copy && page.kind !== "database" ? droppedBlockMove(sourcePageId) : undefined;
    if (move) {
      void move(blockIds, pageId).catch(() => notify(t("pageTreeItem:couldntMoveBlocks"), "error"));
      return;
    }
    const controller = startOperation();
    try {
      for (const blockId of blockIds) {
        await onTransferBlock({
          blockId,
          copy,
          destinationPageId: pageId,
          signal: controller.signal,
          sourcePageId,
        });
      }
      notify(t(copy ? "pageTreeItem:copiedBlocks" : "pageTreeItem:movedBlocks", {
        count: blockIds.length,
      }), "success");
    } catch (error) {
      if (!(error instanceof DOMException && error.name === "AbortError")) {
        notify(t(copy ? "pageTreeItem:couldntCopyBlocks" : "pageTreeItem:couldntMoveBlocks"), "error");
      }
    }
  }

  // Whether this page takes a drop is known once its access is read; a drag
  // passing over or leaving from it asks for it.
  function approach() {
    access.approach();
    if (page.parentId) void pageAccess.read(page.parentId).catch(() => undefined);
  }
  const treeRowDrag: PageTreeRowDrag = {
    // A locked page lifts but is not copied (its menu has no "복제") or moved: the favorites take it.
    canDrag: (copy) => !renaming && (copy ? !page.isLocked : page.isLocked || pageAccess.canEdit(pageId)),
    dragging(active) {
      setDragging(active);
      if (active) approach();
    },
    accepts(data, intent, copy) {
      approach();
      const draggedId = treePageDragId(data);
      if (draggedId !== null) return Boolean(canDropPage(draggedId, intent, copy));
      const blocks = editorBlockDragPayload(data);
      return Boolean(blocks && canDropBlocks(blocks.pageId, copy));
    },
    over(data, intent, copy) {
      // Blocks always go into the page.
      const next = treePageDragId(data) === null ? "inside" : intent;
      setDrop(next);
      setDropCopy(copy);
      if (next === "inside" && hasChildren && !expanded && expandTimerRef.current === null) {
        expandTimerRef.current = window.setTimeout(() => {
          expandTimerRef.current = null;
          setExpanded(true);
        }, 600);
      } else if (next !== "inside") clearExpandTimer();
    },
    leave() {
      clearExpandTimer();
      setDrop(null);
      setDropCopy(false);
    },
    drop(data, intent, copy) {
      clearExpandTimer();
      setDrop(null);
      setDropCopy(false);
      const draggedId = treePageDragId(data);
      if (draggedId !== null) {
        void dropPage(draggedId, intent, copy);
        return;
      }
      const blocks = editorBlockDragPayload(data);
      if (blocks) void dropBlocks(blocks.blockIds, blocks.pageId, copy);
    },
  };
  treeDrag.current = treeRowDrag;
  // Among the favorites a favorite moves only in their order: the page stays
  // where it is, as Notion's (seen 2026-11-06); a page from elsewhere is the
  // tree's drop, as before.
  if (onFavoriteDrop) {
    const tree = treeRowDrag;
    const edge = (intent: PageTreeDropIntent) => (intent === "before" ? "before" : "after");
    const among = (data: Parameters<PageTreeRowDrag["accepts"]>[0], copy: boolean) => !copy && isFavoriteRowDrag(data);
    treeDrag.current = {
      ...tree,
      canDrag: (copy) => !renaming && !copy,
      accepts: (data, intent, copy) => (among(data, copy) ? treePageDragId(data) !== pageId : tree.accepts(data, intent, copy)),
      over(data, intent, copy) {
        if (!among(data, copy)) { tree.over(data, intent, copy); return; }
        setDrop(edge(intent));
        setDropCopy(false);
      },
      drop(data, intent, copy) {
        const draggedId = treePageDragId(data);
        if (!among(data, copy) || draggedId === null) { tree.drop(data, intent, copy); return; }
        tree.leave();
        onFavoriteDrop(draggedId, edge(intent));
      },
    };
  }

  return (
    <>
      <div
        ref={rowRef}
        className={`${styles.treeRow} ${active ? styles.treeRowActive : ""}`}
        role="treeitem"
        tabIndex={0}
        aria-label={page.isLocked ? t("pageTreeItem:lockedName", { title: pageTitle }) : pageTitle}
        aria-level={depth + 1}
        aria-current={active ? "page" : undefined}
        aria-selected={active}
        aria-expanded={expanded}
        aria-posinset={index == null ? undefined : index + 1}
        aria-setsize={setSize}
        data-page-tree-item="true"
        data-tree-page-id={listCopy ? undefined : pageId}
        data-tree-copy-page-id={listCopy ? pageId : undefined}
        data-tree-copy={listCopy}
        data-tree-parent-id={page.parentId}
        data-dragging={dragging || undefined}
        data-drop={drop ?? undefined}
        data-drop-copy={dropCopy || undefined}
        data-has-children={hasChildren}
        data-hover-leading="chevron"
        style={rowStyle}
        // The row's own presses alone: a window its menu draws at the
        // document's root (move, the phone's sheets) is the row's in React's
        // tree, and a pick there opened the page it moved (2026-11-12).
        onClick={(event) => {
          if (renaming || !pressedHere(event)) return;
          // ⌥Click opens the page in a side peek, as Notion's.
          if (event.altKey) openPagePeek(page.id);
          else if (event.metaKey || event.ctrlKey) onOpenPage(page, true);
          // From a large page, the press shows before the page changes over (lib/openPastPaint.ts).
          else openPastPaint(event.currentTarget, () => onOpenPage(page, false));
        }}
        onFocus={access.approach}
        onMouseEnter={access.approach}
        onDoubleClick={(event) => { if (!pressedHere(event)) return; event.preventDefault(); event.stopPropagation(); beginRename(); }}
        onKeyDown={onRowKeyDown}
        onAuxClick={(event) => { if (event.button === 1 && pressedHere(event)) onOpenPage(page, true); }}
        onContextMenu={(event) => {
          if (!pressedHere(event)) return;
          event.preventDefault();
          event.stopPropagation();
          contextPoint.current = { x: event.clientX, y: event.clientY };
          setMenuAnchor({ x: event.clientX, y: event.clientY, pressed: true });
          setMenuOpen(true);
        }}
      >
        <span
          className={styles.treeDragHandle}
          data-tree-drag-handle="true"
          title={t("pageTreeItem:dragToMove", { title: pageTitle })}
          aria-hidden="true"
          onClick={(event) => event.stopPropagation()}
          onDoubleClick={(event) => event.stopPropagation()}
        >
          <DragHandleIcon size="small" />
        </span>
        <span className={styles.treeLeading} data-tree-leading="true">
          <button
            type="button"
            className={`${styles.disclosure} ${expanded ? styles.disclosureOpen : ""}`}
            onClick={(event) => { event.stopPropagation(); setExpanded(!expanded); }}
            // As Notion's (seen 2026-11-11): "열기" while folded, "닫기" while
            // open, described by the row's title.
            aria-label={t(expanded ? "pageTreeItem:hideInside" : "pageTreeItem:showInside")}
            aria-describedby={titleId}
            aria-expanded={expanded}
            data-tree-disclosure="true"
          >
            <SidebarChevronRight size="micro" />
          </button>
          <span className={styles.treeIcon} data-tree-icon="true" data-tree-emoji={page.iconType === "emoji" && page.icon ? "true" : undefined}>
            <PageIconGlyph page={page} size={18} />
          </span>
        </span>
        <span id={titleId} className={styles.treeTitle} data-tree-title="true">
          <TitleWithCustomEmoji text={pageTitle} named={rowPage} workspaceId={page.workspaceId} />
        </span>
        {/* A page in an archived teamspace is marked with no words or tip, as
            Notion's recents mark it (seen 2026-11-14); an archived page says so. */}
        {page.archivedAt || inArchivedTeamspace ? (
          <span className={styles.treeArchive} title={page.archivedAt ? t("pageTreeItem:archived") : undefined}
            aria-label={page.archivedAt ? t("pageTreeItem:archivedPage") : undefined} aria-hidden={page.archivedAt ? undefined : true} data-tree-archived>
            <ArchivedMarkIcon size="small" aria-hidden="true" />
          </span>
        ) : null}
        <span className={styles.treeActions}>
          {/* A locked page's row looks and acts as any other's here, as Notion's (seen 2026-10-31).
              A guest's rows have no "+", even where they may edit, as Notion's (seen 2026-10-08, 2026-11-12);
              the sidebar lets only a guest make no page of their own (canCreateRoot). */}
          {/* A page in the trash takes no page inside, its favorite's row "⋯" alone, as Notion's (seen 2026-11-13; 2026-11-16 in iPhone Safari's drawer). */}
          {access.canEdit && canCreateRoot && !rowPage?.inTrash ? (
            <button
              type="button"
              className={styles.iconBtn}
              title={t("pageTreeItem:addInsideTitle")}
              aria-label={t("pageTreeItem:addInside", { title: pageTitle })}
              onClick={addSubpage}
            ><Plus size="small" /></button>
          ) : null}
          <button
            ref={actionsButtonRef}
            data-tree-more="true"
            type="button"
            className={styles.iconBtn}
            title={t(`pageTreeItem:${pageActionHintKey(access.canEdit, listCopy === "favorite")}`, { title: pageTitle })}
            aria-label={t("pageTreeItem:openActions", { title: pageTitle })}
            onClick={(event) => {
              event.stopPropagation();
              contextPoint.current = null;
              setMenuAnchor(treeMenuPoint(event));
              setMenuOpen(true);
            }}
          ><DotsHorizontal size="small" /></button>
        </span>
        {menuOpen ? (
          <RowMenu
            variant="sidebar"
            canEdit={access.canEdit}
            accessKnown={access.known}
            favoritesList={listCopy === "favorite"}
            anchor={menuAnchor}
            page={page}
            pages={menuPages ?? store.get().pages}
            session={session}
            favorite={favorite}
            onFavoriteChange={(favorite) => onFavoriteChange(page.id, favorite)}
            onClose={() => { setMenuOpen(false); setMenuAnchor(null); }}
            onPageChange={onPageChange}
            onPageMetadataChange={onPageMetadataChange}
            onDuplicatePage={onDuplicatePage}
            onTrashChange={(subject, inTrash, signal) => onPageMetadataChange(subject, { inTrash }, signal)}
            onRename={() => beginRename(contextPoint.current)}
            onStatus={onStatus}
          />
        ) : null}
      </div>
      {/* Outside the row, so the row hears none of the popover's clicks and keys. */}
      {renameAt ? (
        <PageRenamePopover page={page} title={page.title} titleSpans={page.titleSpans} untitled={untitledPageDisplayTitle(page)} at={renameAt}
          onTitleChange={saveTitle} onTitleShown={(title) => showTypedPageTitle(pageId, title, page.title)} onPresentationChange={savePresentation} onClose={endRename} />
      ) : null}
      {expanded ? (
        <div role="group" aria-label={t("pageTreeItem:subpages", { title: pageTitle })} data-drop-inside={drop === "inside" || undefined}>
          {hasChildren ? <>{bodyOrderedChildren(childIds, session ? databases : []).map((child, childIndex) => child.kind === "page" ? (
            <PageTreeItem
              key={child.id}
              pageId={child.id}
              depth={depth + 1}
              index={childIndex}
              setSize={childIds.length + databases.length}
            />
          ) : session ? (
            <SidebarFavoriteDatabaseRow key={child.database.containerId} session={session} depth={depth + 1} database={child.database} />
          ) : null)}</> : (
            <div
              className={styles.treeEmptyChildren}
              style={emptyChildrenStyle}
              data-tree-empty-children="true"
              aria-hidden="true"
            >{t("pageTreeItem:noPagesInside")}</div>
          )}
        </div>
      ) : null}
    </>
  );
}

type PageName = { title: string; titleSpans: TextSpan[] };

const samePageName = (left: PageName, right: PageName) => sameTitle({ title: left.title, spans: left.titleSpans }, { title: right.title, spans: right.titleSpans });
