import { useEffect, useId, useRef, useState, type ComponentProps, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { DoubleChevronRight, OpenAsPage } from "@/icons/leafen";
import { RowPageBodyEditor, type OpenedRowPageDatabase } from "@/components/database/RowPageBodyEditor";
import { inlineDatabasePlaceholderTitle } from "@/components/editor/databaseTitles";
import { RowPeekViewOptions } from "@/components/database/RowPageTopBarItems";
import type { DatabaseViewOpenMode } from "@/components/database/DatabaseView";
import sheet from "@/components/database/databaseSheet";
import {
  clampRowPeekWidth, rowPeekMaxWidth, rowPeekMinWidth, storeRowPeekWidth, storedRowPeekWidth, useCenterPeekScrollbar,
} from "@/components/database/sidePeekWidth";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { peekControlKeepsEscape, usePeekTitleEscape } from "@/lib/peekTitleEscape";
import { useSideRowPeekPlace } from "@/lib/sideRowPeekLayout";
import { useSidePeekMotion } from "@/lib/sidePeekMotion";
import { usePageNames } from "@/lib/pageNames";
import { type BodyFocus, holdingTyping } from "@/lib/pageTitleFocus";
import type { Page, Session } from "@/types";
import type { PageTitleBase } from "@/lib/pageTitleFollow";
import type { TextSpan } from "@/lib/types";
import { CommentsPanel } from "../CommentsPanel";
import { type DatabaseDescriptionSource, useDatabaseDescription } from "./DatabaseDescription";
import { PageCover } from "./PageCover";
import { PageHeader } from "./PageHeader";
import type { PagePresentationChange } from "./pagePresentation";
import { PagePeekActions } from "./PagePeekActions";
import { PagePeekAddTo } from "./PagePeekAddTo";
import { followFreshPageEdits, isFreshPage } from "./freshPages";
import { PageVerifiedBadge, PageVerifyControl } from "./PageVerification";
import { usePageCommentSummary } from "./usePageCommentSummary";
import { usePageDiscussionPlace } from "./usePageDiscussionPlace";
import { usePageColumn } from "./usePageColumn";
import { sidePeekPressBeside } from "../database/rowOpenPress";
import { useRowPageSuggesting } from "../database/useRowPageSuggesting";
import { caretAtPeekTitle, PEEK_TITLE, peekFieldFocused, peekFocusReturn } from "../database/peekFocusReturn";
import { useSidebarLayout } from "../shell/AppShell";
import { inlineSign, inlineStartKey } from "@/lib/readingDirection";

/**
 * A page opened in a peek, as Notion's (seen 2026-10-01: ⌥Click on a page
 * opens it at the side, `?p=<page>&pm=s`): beside the page that stays open,
 * or over its center, with "»" to close, "전체 페이지로 열기" and the view
 * options a database page's peek has, then the page's cover, title and body,
 * edited as the page is. It shares the database page peek's frame and width.
 */
export function PagePeek({
  page,
  mode,
  session,
  organizationId,
  readOnly,
  canEdit,
  canComment = false,
  onClose: closeGiven,
  onOpenMode,
  onTitleChange,
  onTitleTyped,
  onPresentationChange,
  onPageChange,
  actions,
  inlineDatabaseBlockId,
  addTo = false,
}: {
  page: Page;
  mode: "side" | "center";
  session: Session;
  organizationId?: string;
  readOnly: boolean;
  canEdit: boolean;
  /** Whether the reader may comment on the page, which starts its discussion under the title. */
  canComment?: boolean;
  onClose: () => void;
  onOpenMode: (mode: DatabaseViewOpenMode) => void;
  /** Saves the title typed in the peek's head, with the title it was typed on (PageHeader). */
  onTitleChange: (title: string, base?: PageTitleBase, spans?: TextSpan[]) => unknown;
  /** A title as it is typed, shown before it is saved (PageHeader). */
  onTitleTyped?: (title: string) => void;
  onPresentationChange: (change: PagePresentationChange) => void;
  onPageChange: (page: Page) => void;
  /** The top bar's share, link, comments, favorite and "…", as the page's own bar has them. */
  actions: Omit<ComponentProps<typeof PagePeekActions>, "page" | "session" | "onPageChange" | "onPresentationChange" | "onOpenFullPage" | "canEdit" | "suggesting">;
  /**
   * An inline database of the page opened alone in the peek, as Notion's
   * "사이드 보기에서 열기" shows it (seen 2026-10-10): the database — its
   * title, views and rows — without the page's cover, title and other blocks,
   * its bar's the one it has opened alone as a page (PagePeekActions).
   */
  inlineDatabaseBlockId?: string;
  /** A page the sidebar's "+" just made: "추가 대상 ⌄" in the bar, and what is done in it keeps it (freshPages.ts). */
  addTo?: boolean;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["pagePeek", "databaseView", "topBar"]);
  const panelRef = useRef<HTMLElement>(null);
  const { phone } = useSidebarLayout(true);
  const leave = useSidePeekMotion(panelRef, mode === "side", inlineDatabaseBlockId ?? page.id);
  const onClose = () => leave(closeGiven);
  // Its blocks keep its column; the pointer beside one is on it (usePageColumn).
  const docRef = useRef<HTMLDivElement>(null);
  usePageColumn(docRef, false);
  // A layer over the page: Escape closes it while nothing opened over it
  // takes the key; a center peek holds the page still. Either gives the focus
  // back as a row's peek does (peekFocusReturn), not by the layer.
  // A side peek goes with a press on the room of the page beside it, which acts on the page too (sidePeekPressBeside).
  useOverlayLayer(true, { kind: mode === "side" ? "side-peek" : "dialog", ref: panelRef, modal: mode === "center", restoreFocus: false, waitsForHeldTyping: true, modalOwned: () => [panelRef.current?.previousElementSibling], onEscape: () => { if (!peekControlKeepsEscape(panelRef.current)) onClose(); },
    ...(mode === "side" ? sidePeekPressBeside(onClose) : {}) });
  usePeekTitleEscape(panelRef, true, onClose);
  useCenterPeekScrollbar(panelRef, mode === "center");
  // `sign` is 1 left to right: a side peek at the window's end widens toward its start.
  const resizeStart = useRef<{ startX: number; startWidth: number; sign: 1 | -1 } | null>(null);
  const [sideWidth, setSideWidth] = useState(storedRowPeekWidth);
  const [resizing, setResizing] = useState(false);
  const [entered, setEntered] = useState(false);
  // An inline database opened alone is named by the name it goes by, as its body reads it (inlineDatabaseShownName).
  const [openedDatabase, setOpenedDatabase] = useState<OpenedRowPageDatabase | null>(null);
  const title = inlineDatabaseBlockId ? openedDatabase?.title || inlineDatabasePlaceholderTitle() : pageNames.title(page);
  // The page's own discussion under its title, as the full page and Notion's
  // peek show it (seen 2026-10-20): its open threads, or "댓글 추가" starting
  // one. A full-page database takes none, as on its own page.
  const pageComments = usePageCommentSummary({ pageId: inlineDatabaseBlockId ? undefined : page.id, session });
  const discussionPlace = usePageDiscussionPlace(page.id, pageComments.open, pageComments.reactions);
  const { composing: composingPageComment, setComposing: setComposingPageComment } = discussionPlace;
  const commentsHostId = `page-comments-inline-${page.id}-${useId()}`;
  const pageCommentsAllowed = !inlineDatabaseBlockId && page.kind !== "database" && page.pageCommentsDisplay !== "off";
  // A full-page database is drawn as on its own page, its views right under
  // its title rather than its block's own title line under the page's; a
  // wiki whose pages are its database's as its page shows it, its Home and
  // views' picker under its title rather than its database's block (Notion's
  // side peek, seen 2026-11-14).
  const databasePeek = page.kind === "database" ? "database"
    : page.isWiki && page.wikiRootId === page.id && page.wikiDataSourceId ? "wiki" : undefined;
  // Its database's description, as on its own page: "설명 추가" over the
  // title, the field under it (Notion's side peek, seen 2026-11-14: "아이콘
  // 추가 · 커버 추가 · 인증 추가 · 설명 추가", "설명 숨기기" over a shown
  // one), read from its body; a linked database's or a view block's page
  // neither shows nor offers its source's, as on its own page (PageView).
  const [databaseSource, setDatabaseSource] = useState<DatabaseDescriptionSource>();
  const pageDiscussionShown = pageCommentsAllowed && discussionPlace.shown;
  // Suggesting edits, as the page opened full and Notion's peek (seen
  // 2026-11-13): its "…" menu and a block's "편집 내용 제안" turn it on where
  // the reader may comment, not on a locked page; the head waits meanwhile.
  const suggesting = useRowPageSuggesting(page.id, canComment && !page.isLocked && !inlineDatabaseBlockId && page.kind !== "database");
  const headReadOnly = readOnly || suggesting.active;
  // Locked, its description still shows or hides, as on its own page (useDatabaseDescription).
  const lockedOnly = readOnly && canEdit && page.isLocked;
  const description = useDatabaseDescription({
    source: databasePeek && !databaseSource?.linked && !(page.kind === "database" && page.viewBlock === true) ? databaseSource?.source ?? null : null,
    readOnly: suggesting.active || readOnly && !lockedOnly, locked: lockedOnly, onChange: databaseSource?.onChange,
  });

  // The page makes room for the peek beside it (Notion's, seen 2026-11-03).
  useSideRowPeekPlace(panelRef, mode === "side", sideWidth, page.id);
  useEffect(() => {
    if (!addTo || !panelRef.current || !isFreshPage(page.id)) return;
    return followFreshPageEdits(panelRef.current, page.id);
  }, [addTo, page.id]);
  useEffect(() => {
    // Focus goes back where a key left it: a peek a press opened — a page
    // block's ⌥ press, the sidebar's "+" — closes to nothing chosen, as
    // Notion's (seen 2026-11-13: Escape left the focus on the body, no ring
    // on the block and no caret in the page).
    const giveBack = peekFocusReturn(panelRef);
    // Its title takes the caret at its end as Notion's does, what is typed from
    // the opening on waiting for it (caretAtPeekTitle); else the panel takes
    // focus a frame on, unless its title already has it, or typing is held for
    // its new page's title or body — a center one's first button the layer
    // gave it too (peekFocusReturn.ts).
    const titled = !phone && !inlineDatabaseBlockId && page.kind !== "database" && !holdingTyping() && !peekFieldFocused(panelRef.current);
    const stopCaret = titled ? caretAtPeekTitle(panelRef.current, PEEK_TITLE, mode === "center", true) : () => undefined;
    const frame = window.requestAnimationFrame(() => {
      setEntered(true);
      if (holdingTyping() || peekFieldFocused(panelRef.current)) return;
      panelRef.current?.focus();
    });
    return () => {
      window.cancelAnimationFrame(frame);
      stopCaret();
      giveBack();
    };
    // Its focus is placed once, as the page opens in it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page.id]);
  useEffect(() => {
    if (!resizing) return;
    let width = sideWidth;
    function move(event: PointerEvent) {
      if (!resizeStart.current) return;
      width = clampRowPeekWidth(resizeStart.current.startWidth + (resizeStart.current.startX - event.clientX) * resizeStart.current.sign);
      setSideWidth(width);
    }
    function stop() {
      setResizing(false);
      resizeStart.current = null;
      storeRowPeekWidth(width);
    }
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop, { once: true });
    window.addEventListener("pointercancel", stop, { once: true });
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
      window.removeEventListener("pointercancel", stop);
    };
    // The width while dragging starts from the one shown when it began.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resizing]);

  function commitWidth(next: number) {
    const width = clampRowPeekWidth(next);
    setSideWidth(width);
    storeRowPeekWidth(width);
  }

  function startResize(event: ReactPointerEvent<HTMLDivElement>) {
    if (event.button !== 0) return;
    event.preventDefault();
    resizeStart.current = { startX: event.clientX, startWidth: sideWidth, sign: inlineSign(event.currentTarget) };
    setResizing(true);
  }

  function onResizeKeyDown(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    const step = event.shiftKey ? 80 : 24;
    if (event.key === inlineStartKey(event.currentTarget)) commitWidth(sideWidth + step);
    else if (event.key !== "Home" && event.key !== "End") commitWidth(sideWidth - step);
    else if (event.key === "Home") commitWidth(rowPeekMinWidth());
    else commitWidth(rowPeekMaxWidth());
  }


  // The body's first line, where the title's Enter or Down moves the caret.
  // The title's Enter takes the body's first line, made where the body is empty.
  const bodyFocus = useRef<((selection: BodyFocus) => void) | null>(null);
  function focusBody(selection: BodyFocus) {
    if (bodyFocus.current) bodyFocus.current(selection);
    else panelRef.current?.querySelector<HTMLElement>("[data-page-peek-body] [contenteditable='true']")?.focus();
  }
  // An empty body's room, under its starter, is the body's own: a press there
  // starts the writing on its first line, as Notion's peeks and a new page's
  // window from the sidebar's "+" (seen 2026-11-07), and a row page's (DatabaseRowPage).
  const [bodyEmpty, setBodyEmpty] = useState(false);
  const writeFromRoom = !readOnly && !suggesting.active && bodyEmpty ? {
    "data-writes-from-room": "",
    onMouseDown: (event: ReactMouseEvent) => { if (event.target === event.currentTarget) event.preventDefault(); },
    onClick: (event: ReactMouseEvent) => { if (event.target === event.currentTarget) focusBody("new"); },
  } : undefined;

  const motionState = entered ? "open" : "opening";
  const style = mode === "side" ? ({ "--row-peek-width": `${sideWidth}px` } as CSSProperties) : undefined;
  const view = (
    <>
      {mode === "center" ? (
        <button type="button" className={sheet.rowPeekBackdrop} data-mode={mode} data-motion-state={motionState}
          tabIndex={-1} aria-label={t("pagePeek:close")} onClick={onClose} />
      ) : null}
      <aside ref={panelRef} className={sheet.rowPeek} data-mode={mode} data-page-peek={page.id}
        data-motion-state={motionState} data-resizing={resizing ? "true" : undefined} style={style}
        role="dialog" aria-modal={mode === "center" ? true : undefined}
        aria-label={t("pagePeek:label", { title })} tabIndex={-1}>
        {mode === "side" ? (
          <div className={sheet.rowPeekResizeHandle} role="separator" tabIndex={0}
            aria-label={t("databaseView:resizeSidePreview")} aria-orientation="vertical"
            aria-valuemin={rowPeekMinWidth()} aria-valuemax={rowPeekMaxWidth()} aria-valuenow={sideWidth}
            onPointerDown={startResize} onKeyDown={onResizeKeyDown} />
        ) : null}
        <div className={sheet.rowPeekTop}>
          <div className={sheet.rowPeekChromeSide} aria-label={t("databaseView:peekOptions", { title })}>
            {mode === "side" ? (
              <button type="button" className={sheet.rowPeekIconAction} data-page-peek-close
                aria-label={t("pagePeek:close")} title={t("pagePeek:close")} onClick={onClose}>
                <DoubleChevronRight size="regular" />
              </button>
            ) : null}
            <button type="button" className={sheet.rowPeekIconAction} data-page-peek-open-page
              aria-label={t("databaseView:openAsFullPage")} title={t("databaseView:openAsFullPage")}
              onClick={() => onOpenMode("full")}
              onAuxClick={(event) => { if (event.button === 1) onOpenMode("new_tab"); }}>
              <OpenAsPage size="small" />
            </button>
            {addTo ? (
              <PagePeekAddTo page={page} pages={actions.pages} session={session} onPageChange={onPageChange} onStatus={actions.onStatus} />
            ) : null}
            <RowPeekViewOptions mode={mode} onOpenMode={onOpenMode} />
          </div>
          <PagePeekActions {...actions} page={page} session={session} canEdit={canEdit} onPageChange={onPageChange}
            onPresentationChange={onPresentationChange} onOpenFullPage={() => onOpenMode("full")} suggesting={suggesting.control}
            inlineDatabase={inlineDatabaseBlockId ? { blockId: inlineDatabaseBlockId, title } : undefined} />
        </div>
        <div className={`${sheet.rowPeekScroll} nscroll`} data-page-scroll>
          {inlineDatabaseBlockId ? (
            <div className={sheet.rowPeekDoc} data-inline-database-peek={inlineDatabaseBlockId}>
              <section className={sheet.rowPeekEditor} data-page-peek-body aria-label={title}>
                {/* In the peek: its view press leaves the address and the window's title alone, as Notion's (useDatabaseViewController). */}
                <RowPageBodyEditor key={`${page.id}:${inlineDatabaseBlockId}`} session={session} rowId={page.id} workspaceId={page.workspaceId}
                  readOnly={readOnly} ownPage onlyBlockId={inlineDatabaseBlockId} peek={mode} onOpenedDatabase={setOpenedDatabase} />
              </section>
            </div>
          ) : <>
          <PageCover page={page} compact readOnly={headReadOnly} onChange={onPresentationChange} />
          <div ref={docRef} className={sheet.rowPeekDoc} data-peek-mode={mode} data-has-cover={page.cover ? "true" : "false"}
            data-database-peek={databasePeek}
            data-font={page.font ?? "default"} data-small-text={Boolean(page.smallText)} data-full-width={Boolean(page.fullWidth)}>
            <PageHeader page={page} readOnly={headReadOnly} onTitleChange={onTitleChange} onTitleTyped={onTitleTyped} compact={mode === "side"}
              databaseDescription={databasePeek ? description : undefined}
              onPresentationChange={onPresentationChange} onFocusBody={focusBody} commentsHostId={commentsHostId}
              onOpenComments={pageCommentsAllowed && !databasePeek && canComment && discussionPlace.addInHead ? () => setComposingPageComment(true) : undefined}
              // Locked, the page still offers its verification to one who may edit it, as a page opened whole (PageView).
              verifyControl={suggesting.active || (readOnly && !(canEdit && page.isLocked)) ? undefined : (
                <PageVerifyControl page={page} session={session} onChange={(change) => onPageChange({ ...page, ...change })} />
              )}
              verifiedBadge={(
                <PageVerifiedBadge page={page} session={session} organizationId={organizationId}
                  readOnly={headReadOnly} onChange={(change) => onPageChange({ ...page, ...change })} />
              )} />
            {/* eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions, jsx-a11y/click-events-have-key-events -- Enter in the title does the same. */}
            <section className={sheet.rowPeekEditor} data-page-peek-body aria-label={title} {...writeFromRoom}>
              {/* Its block menus suggest edits and present from a block, as the page opened full (presentAs). */}
              <RowPageBodyEditor key={page.id} session={session} rowId={page.id} workspaceId={page.workspaceId}
                readOnly={readOnly} ownPage tableOfContentsDisplay={page.tableOfContentsDisplay} focusRef={bodyFocus}
                fullPageDatabase={databasePeek === "database"} wiki={databasePeek === "wiki" ? page : undefined}
                onDatabaseSource={databasePeek ? setDatabaseSource : undefined}
                suggesting={suggesting.active} peek={mode} onSuggestingChange={suggesting.control?.onChange}
                onCanSuggestChange={suggesting.onBodyCanSuggest} presentAs={page}
                onContentChange={(hasContent) => {
                  setBodyEmpty(!hasContent);
                  // The page list's icons of it (an empty document or a lined one) follow, as the open page's do.
                  if (page.kind !== "database" && page.hasContent !== hasContent) onPageChange({ ...page, hasContent });
                }} />
            </section>
          </div>
          </>}
        </div>
      </aside>
      {pageDiscussionShown ? (
        <CommentsPanel key={`page-discussion:${page.id}`} inline sideOnly={discussionPlace.sideOnly} inlineHostId={commentsHostId} autoFocus={composingPageComment}
          pageId={page.id} workspaceId={page.workspaceId} blockId={null} organizationId={organizationId} session={session}
          canEditPage={canEdit} canComment={canComment} onClose={() => setComposingPageComment(false)} onStatus={actions.onStatus} />
      ) : null}
    </>
  );
  return typeof document === "undefined" ? view : createPortal(view, document.body);
}
