"use client";

import {
  type CSSProperties, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent,
  type ReactNode, useCallback, useEffect, useLayoutEffect, useRef, useState,
} from "react";
import { createPortal } from "react-dom";
import { heldInAnotherTab, noteUnlockedInThisTab, unlockedInThisTab } from "@/lib/pageUnlockSession";
import { useTranslation } from "react-i18next";
import { ArrowLeft, DoubleChevronRight, OpenAsPage } from "@/icons/leafen";
import { OPEN_COMMENTS_EVENT, useMarginCompose, type OpenCommentsDetail } from "@/components/comments/commentPanelEvents";
import type { GraphProperty } from "@/contentGraph";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { peekControlKeepsEscape, usePeekTitleEscape } from "@/lib/peekTitleEscape";
import { useSideRowPeekPlace } from "@/lib/sideRowPeekLayout";
import { useSidePeekMotion } from "@/lib/sidePeekMotion"; import { handRowPageOver, ROW_PAGE_READING } from "@/lib/rowPageHandoff";
import type { Page, PropertyType, TextSpan } from "@/lib/types";
import type { TitleValue } from "@/lib/titleMerge";
import { PageFindBar } from "../page/PageFindBar";
import { useSidebarLayout } from "../shell/AppShell";
import { RowPageComments } from "./RowPageComments";
import { RowPeekAddTo, useFreshRowAddTo } from "./RowPeekAddTo";
import { CellCommentMarks } from "./CellCommentMarks";
import { PageHeaderBacklinks } from "../page/PageHeaderBacklinks";
import type { PagePresentationChange } from "../page/pagePresentation";
import { RowPageVerification, rowPageVerifyControl, useRowPageVerification } from "../page/PageVerification";
import { TopBarSidebarToggle } from "../page/TopBarSidebarToggle";
import type { Page as ProductPage, Session } from "@/types";
import { DatabaseRowArchiveBanner } from "./DatabaseRowArchiveBanner";
import { RowPageBodyEditor } from "./RowPageBodyEditor";
import { DatabaseRowTrashBanner, useRowTrashDeletion } from "./DatabaseRowTrashBanner";
import { PageSiteBand, PageTopBands } from "../page/PageTopBands";
import { sidePeekPressBeside } from "./rowOpenPress"; import { openingTakesTitleCaret, peekFocusReturn } from "./peekFocusReturn";
import { RowPeekSiblingButtons, RowPeekViewOptions, type RowPageTopBarActions } from "./RowPageTopBarItems";
import { DatabaseRowPageActions } from "./DatabaseRowPageActions";
import { useRowPageTransfer } from "./useRowPageTransfer";
import type { PropertyValueReadContext } from "./PropValue";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import type { DatabasePropertyEditing } from "./DatabasePropertyEditPanel";
import { RowPageCover, RowPageHeading, type RowPagePresentation } from "./RowPageHeader";
import { RowPagePropertyList } from "./RowPagePropertyList";
import { RowPagePinnedProperties, detailsToggleLast } from "./RowPagePinnedProperties";
import { RowPageWikiControls, RowPageWikiProperties } from "./RowPageWikiProperties";
import { verificationPropertyValue } from "./structuredPropertyValues";
import { RowPageLayoutBody, useRowPageDetails } from "./RowPageLayoutBody";
import { RowPageTemplatePrompt } from "./RowPageTemplatePrompt";
import { useDatabaseTemplates, useRowTakingTemplate } from "@/lib/databaseTemplateRequests"; import { chooseRowTemplate } from "@/lib/rowTemplateChosenHere";
import { RowPageLayoutTabs } from "./RowPageLayoutTabs";
import { type DatabasePageLayout, pageLayoutPlacedProperties, requestPageLayoutBuilder, saveDatabasePageLayoutTabView, useDatabasePageLayout } from "@/lib/databasePageLayout";
import sheet from "./databaseSheet";
import rowPageStyles from "./DatabaseRowPage.module.css";
import { RichTextCellEditor } from "./RichTextCellEditor";
import { rowTitleKeys, useRowTitleUndo } from "./useRowTitleUndo";
import { RichTextCellText } from "./RichTextCellText";
import { useRowPageTitle } from "./useRowPageTitle";
import type { RowPageMenuExtras } from "./DatabaseRowMenu";
import { RowPageHistoryDialog } from "./RowPageHistoryDialog";
import { useRowPageSuggesting } from "./useRowPageSuggesting";
import { RowPagePath, RowPathPiece, type RowPathDatabase } from "./RowPagePath"; import { usePersonRowPage } from "../people/usePersonRowPage";
import { inlineDatabasePlaceholderTitle } from "../editor/databaseTitles";
import {
  ROW_PEEK_FALLBACK_WIDTH, clampRowPeekWidth, rowPeekMaxWidth, rowPeekMinWidth, sidePeekStacksColumns, storedRowPeekWidth, storeRowPeekWidth, useCenterPeekScrollbar,
} from "./sidePeekWidth";
import { usePageCommentSummary } from "@/components/page/usePageCommentSummary";
import type { BodyFocus } from "@/lib/pageTitleFocus"; import { enterBodyOrTitleStart } from "../editor/pageBodyFrame"; import { takePeekFocus, useNewRowTitleCaret } from "./rowPageTitleCaret";
import { registerEditable } from "@/components/editor/focus"; import { rowPageMadeEmpty } from "@/lib/rowMadeHere"; import { useFullPageTitleFocus } from "./useFullPageTitleFocus"; import { usePageColumn } from "../page/usePageColumn"; import { useFullRowWindowTitle } from "./useFullRowWindowTitle"; import { titleWithClosedMentions } from "@/lib/closedMentions";
import { MarginDiscussions } from "@/components/comments/MarginDiscussions";
import { TopBarLock } from "@/components/page/TopBarLock";
import { untitledRowPageName, useUntitledRowName } from "./untitledRowName";
import { followFreshRowEdits, isFreshRow, leaveFreshRowSoon } from "./freshRows";
import { usePageVisitRecorder } from "../product/usePageVisitRecorder";
import { useMainContentRect } from "./useMainContentRect"; import { useRowWindowUnderTemplate } from "./rowWindowTemplate"; import { useRowWindowEndsHistoryBehind } from "./rowWindowHistory";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import { inlineSign, inlineStartKey } from "@/lib/readingDirection";

const styles: Record<string, string> = {
  ...sheet,
  block: rowPageStyles.block,
};
export type DatabaseRowPagePreviewMode = "side" | "center" | "full";

type DatabaseRowPageProps = {
  mode: DatabaseRowPagePreviewMode;
  properties: readonly GraphProperty[];
  snapshot: DatabaseRowPageSnapshot;
  propertyValueContext: PropertyValueReadContext;
  valueRow: Page;
  editableRow?: Page;
  /** Writes a title or text value; its spans keep its formatting; given the title read, only over it (RowTitleWrite). */
  onRichTextChange?: (property: GraphProperty, value: string, spans?: TextSpan[], read?: TitleValue | null) => Promise<unknown> | void;
  /** A draft's title (a template's) takes each keystroke, so closing keeps what was typed; a row's saves as the typing pauses and as editing finishes. */
  titleCommitOn?: "input" | "blur";
  /**
   * Edits the other properties as their cells do, as a database page's
   * properties are edited in Notion; with it, an editable row lists its
   * empty properties too, apart from those set to hide.
   */
  propertyCellEnvironment?: PropertyCellEnvironment;
  /** Changing the database's properties from the page, where they can change. */
  propertyEditing?: DatabasePropertyEditing;
  onAddProperty?(type: PropertyType, name?: string, icon?: string): string | void;
  onClose: () => void;
  /** ⤢, which opens it as a full page; a template's window has none yet. */
  onOpenMode?: (mode: "full" | "new_tab") => void;
  // The page the row's database lives on, which a full-page row returns to.
  containerTitle?: string;
  /** The inline database the page is in, a piece of its full page's path. */
  pathDatabase?: RowPathDatabase;
  /** Its updates and analytics are docked beside it, which its words make room for as a page's do. */
  sidePanelShown?: boolean;
  currentUserId?: string;
  onUnarchive?: () => Promise<boolean>;
  /** Restores a row in trash, and deletes it forever for the workspace owner. */
  onRestore?: () => Promise<boolean>;
  onDeleteForever?: () => Promise<boolean>;
  /**
   * Opens the row's own discussion, at a thread or block of its body when
   * given; absent when the viewer cannot comment.
   */
  onOpenComments?: (detail?: OpenCommentsDetail) => void;
  /** Opens the page's actions menu from its … button, with its suggesting mode and history when offered. */
  onOpenActions?: (trigger: HTMLElement, extras: RowPageMenuExtras) => void;
  /** Reads the page again after a version of it was restored. */
  onHistoryRestored?: () => void;
  /** Hears whether the page's body holds any block, which the row's icon shows. */
  onBodyContentChange?: (hasContent: boolean) => void;
  /** Makes the database a new template, offered by an empty body; absent where its database's view isn't open. */
  onCreateTemplate?: () => void;
  /** Sets the page's icon or cover; absent when the viewer cannot. */
  onPresentationChange?: (change: PagePresentationChange) => Promise<boolean>;
  /** A signed-in writer edits the body in the page editor; others read it. */
  session?: Session;
  workspaceId?: string;
  bodyReadOnly?: boolean;
  /** A published database page shows its body as its public page does. */
  body?: ReactNode;
  /** A band under the top bar, as a template's "템플릿 편집 중" (DatabaseTemplateEditor). */
  banner?: ReactNode;
  /**
   * A database template's page, its values the template's own, laid out as
   * its database's pages are (DatabaseTemplateEditor).
   */
  template?: boolean;
  /** "레이아웃 사용자 지정" for a page laid out without the view's property editing, as a template's. */
  onCustomizeLayout?: () => void;
  /** The top bar's view options, link, favorite and last edit, where the page offers them. */
  topBar?: RowPageTopBarActions;
  /** A visitor's page: the layout its public read carries (null for none), not one read for a member. */
  layout?: DatabasePageLayout | null;
};

function readableValue(value: unknown): string {
  if (value == null) return "";
  if (["string", "number", "boolean"].includes(typeof value)) return String(value);
  if (Array.isArray(value)) return value.map(readableValue).filter(Boolean).join(", ");
  if (typeof value === "object") {
    const object = value as Record<string, unknown>;
    if ("plainText" in object) return readableValue(object.plainText);
    if ("text" in object) return readableValue(object.text);
    if ("rich" in object) return readableValue(object.rich);
  }
  return JSON.stringify(value);
}

function rowPageTitle(snapshot: DatabaseRowPageSnapshot) {
  for (const value of Object.values(snapshot.row.values)) {
    if (value.type === "title") return readableValue(value.value).trim();
  }
  return "";
}

export function DatabaseRowPage({
  mode,
  properties,
  snapshot,
  propertyValueContext,
  valueRow,
  editableRow: givenEditableRow,
  onRichTextChange: givenRichTextChange,
  titleCommitOn = "blur",
  propertyCellEnvironment: givenCellEnvironment,
  propertyEditing,
  onAddProperty,
  onClose: closeGiven,
  onOpenMode,
  containerTitle,
  pathDatabase,
  sidePanelShown,
  currentUserId,
  onUnarchive,
  onRestore,
  onDeleteForever,
  onOpenComments,
  onOpenActions,
  onHistoryRestored,
  onBodyContentChange,
  onCreateTemplate,
  onPresentationChange: changePresentation,
  session,
  workspaceId,
  bodyReadOnly,
  body,
  banner,
  template,
  onCustomizeLayout: givenCustomizeLayout,
  topBar, layout: givenLayout,
}: DatabaseRowPageProps) {
  const { t } = useTranslation(["rowProperties", "topBar", "databaseView", "common", "pageFindBar", "editor", "pageHeader"]);
  // Drawn over the app, a phone's row page takes the phone page's look as the app's own pages do.
  const { phone } = useSidebarLayout(true);
  const panelRef = useRef<HTMLElement>(null);
  const leave = useSidePeekMotion(panelRef, mode === "side" && !phone, snapshot.row.id);
  const freshRow = useFreshRowAddTo(snapshot, panelRef, session); usePersonRowPage(snapshot.person, mode); const underTemplate = useRowWindowUnderTemplate(snapshot.row.id) && mode !== "full";
  const onClose = () => leave(() => { freshRow.leave(); closeGiven(); });
  // A peek is a layer over the page: Escape closes its find bar, then the
  // peek, and only while nothing opened over it takes the key (2026-10-03:
  // Escape in its select menu closed the peek too). A center peek holds the
  // page still; the focus on opening and closing is the effect's below.
  const escapeRef = useRef<() => void>(() => undefined);
  useOverlayLayer(mode !== "full", { kind: mode === "side" ? "side-peek" : "dialog", ref: panelRef, modal: mode === "center", restoreFocus: false, initialFocus: { current: null }, waitsForHeldTyping: true, modalOwned: () => [panelRef.current?.previousElementSibling], onEscape: () => escapeRef.current(), ...(mode === "side" ? sidePeekPressBeside(onClose) : {}) });
  usePeekTitleEscape(panelRef, mode !== "full", () => escapeRef.current()); useCenterPeekScrollbar(panelRef, mode === "center"); useRowWindowEndsHistoryBehind(mode !== "full", snapshot.row.id);
  const findRootRef = useRef<HTMLDivElement>(null);
  usePageColumn(findRootRef, mode === "full");
  const bodyRef = useRef<HTMLElement>(null);
  // The database's page layout, where one is applied (Notion's "레이아웃 사용자 지정");
  // a wiki's page is laid out as Notion's wiki pages are instead.
  const wiki = snapshot.wiki;
  // A wiki's page that is a full-page database is titled by its database's
  // name and holds only its database, as Notion's (seen 2026-10-29).
  const fullPageDatabase = wiki?.fullPageDatabase === true;
  const databaseLayout = useDatabasePageLayout(session, snapshot.row.containerPageId, snapshot.row.dataSourceId, snapshot.row.id).layout;
  const pageLayout = wiki ? null : givenLayout !== undefined ? givenLayout : databaseLayout;
  const details = useRowPageDetails(pageLayout, properties.filter((property) => property.type !== "title").map((property) => property.id));
  // A wiki page shows its comments once it has some or one is being written,
  // as Notion's wiki pages (seen 2026-10-15); its header's "인증 추가" opens
  // its Verification's menu.
  const [wikiComposing, setWikiComposing] = useState(false);
  const [wikiVerifyRequest, setWikiVerifyRequest] = useState(0);
  useEffect(() => setWikiComposing(false), [snapshot.row.id]);
  // A page New made here opens empty: its starter is drawn with it, not once its body has said so (rowPageMadeEmpty).
  const [bodyEmpty, setBodyEmpty] = useState(() => rowPageMadeEmpty(snapshot));
  // Inline discussions sit beside a full page's text unless its layout keeps them minimal, as Notion's.
  const [marginDiscussions, setMarginDiscussions] = useState(false);
  // A page just made by a table's "+ 새 페이지" or a new-page shortcut goes
  // when it is left as it was made (freshRows.ts): writing, or pressing its
  // header, values or body, keeps it.
  useEffect(() => {
    const id = snapshot.row.id, panel = panelRef.current;
    if (!isFreshRow(id) || !panel) return;
    const stop = followFreshRowEdits(panel, id, (target) => Boolean(target.closest("[data-page-scroll]")) && !target.closest("[data-row-page-title]"));
    return () => { stop(); leaveFreshRowSoon(id); };
  }, [snapshot.row.id]);
  const hearBodyContent = useCallback((hasContent: boolean) => {
    // A body emptied again, by an undo or a restore, offers the templates again, as Notion's (seen 2026-11-06).
    setBodyEmpty((was) => { if (!was && !hasContent) setTemplatesSet(false); return !hasContent; });
    onBodyContentChange?.(hasContent);
  }, [onBodyContentChange]);
  // `sign` is 1 left to right: a side peek at the window's end widens toward its start.
  const resizeStartRef = useRef<{ startX: number; startWidth: number; sign: 1 | -1 } | null>(null);
  const sideWidthRef = useRef(ROW_PEEK_FALLBACK_WIDTH);
  const [sideWidth, setSideWidth] = useState(storedRowPeekWidth);
  const [resizingSide, setResizingSide] = useState(false);
  const [entered, setEntered] = useState(false);
  const [findOpen, setFindOpen] = useState(false);
  // A row's page is a page visited, however it opens; quick find and the library's "최근" list it.
  usePageVisitRecorder(session, snapshot.row.id);
  const [historyOpen, setHistoryOpen] = useState(false);
  const comments = usePageCommentSummary({ pageId: snapshot.row.id, row: true, session });
  const trashDeletion = useRowTrashDeletion(session, snapshot.workspaceId, snapshot.row.id, snapshot.row.trashedAt);
  const canEditPage = snapshot.accessRole === "edit" || snapshot.accessRole === "full_access";
  // A restored version replaces the body, which the editor then reads anew.
  const [bodyGeneration, setBodyGeneration] = useState(0);
  // The database's templates its empty body lists, until one is chosen or "비어 있음"; none while a template's body comes (useRowTakingTemplate).
  const templates = useDatabaseTemplates(snapshot.row.dataSourceId);
  const [templatesSet, setTemplatesSet] = useState(false), takingTemplate = useRowTakingTemplate(snapshot.row.id, bodyEmpty);
  const [findFocusTick, setFindFocusTick] = useState(0);
  const fullRect = useMainContentRect(mode === "full");
  // A locked database page keeps its title, properties, icon, cover and
  // body until this person unlocks it, for as long as this tab (pageUnlockSession.ts).
  const unlockedForMe = snapshot.row.unlockedForMe && unlockedInThisTab(snapshot.row.id);
  const lockedForMe = snapshot.row.locked && !unlockedForMe;
  const lockForMe = topBar?.lock?.onLockForMe;
  const staleUnlock = snapshot.row.locked && snapshot.row.unlockedForMe && !unlockedInThisTab(snapshot.row.id);
  useEffect(() => {
    // An unlock from a tab since closed or reloaded ends unless another tab holds it.
    if (!staleUnlock || !lockForMe) return;
    let current = true;
    void heldInAnotherTab(snapshot.row.id).then((held) => { if (current && !held) lockForMe(false); });
    return () => { current = false; };
  }, [lockForMe, snapshot.row.id, staleUnlock]);
  // Suggesting edits needs the menu that turns it on and the discussion that shows the suggestions; meanwhile the head
  // waits as a lock holds it, as Notion's (seen 2026-11-14: no "+ 속성 추가" or buttons over its title, values not pressable).
  const suggesting = useRowPageSuggesting(
    snapshot.row.id, Boolean(onOpenActions && onOpenComments && session && workspaceId && !body) && !lockedForMe,
  ), headHeld = lockedForMe || suggesting.active;
  const editableRow = headHeld ? undefined : givenEditableRow;
  // "레이아웃 사용자 지정" lays out every page of the database, for whoever may change it, the page locked or not (seen 2026-11-14);
  // an inline database's from the page it sits on, as Notion's (seen 2026-10-24). Suggesting edits holds it.
  const layoutPageId = propertyEditing?.writable && !suggesting.active ? propertyEditing.dataSource.pageId ?? snapshot.row.containerPageId : undefined;
  const customizeLayout = suggesting.active || wiki ? undefined : givenCustomizeLayout ?? (layoutPageId ? () => requestPageLayoutBuilder({
    pageId: layoutPageId, dataSourceId: snapshot.row.dataSourceId, rowId: snapshot.row.id,
  }) : undefined);
  const onRichTextChange = headHeld ? undefined : givenRichTextChange;
  const propertyCellEnvironment = headHeld ? undefined : givenCellEnvironment;
  const onPresentationChange = headHeld ? undefined : changePresentation;
  // The title as the row holds it now: a change made in the page shows in
  // its bar too, not only the title read when it opened.
  const storedTitle = typeof valueRow?.title === "string" ? valueRow.title : rowPageTitle(snapshot);
  // An untitled row reads as its database's default template, a template being edited by its own name.
  const rowName = useUntitledRowName(), untitledRow = untitledRowPageName({ template: Boolean(template),
    untitledName: snapshot.untitledName, rowId: snapshot.row.id, dataSourceId: snapshot.row.dataSourceId, rowName, templates });
  const title = titleWithClosedMentions(storedTitle, valueRow?.titleSpans) || (fullPageDatabase ? inlineDatabasePlaceholderTitle() : untitledRow);
  const titleProperty = properties.find((property) => property.type === "title");
  const titleEditable = Boolean(editableRow && onRichTextChange && titleProperty) && !snapshot.snapshotIdentity.startsWith(ROW_PAGE_READING);
  // A row just made takes its caret here, with what was typed while it was
  // being made, and its title's Enter goes to the body, as a page's
  // (PageHeader).
  // The body's first line, made where the body is empty, as a page's title's Enter takes.
  const rowBodyFocus = useRef<((selection: BodyFocus) => void) | null>(null), titleHandle = useRowTitleUndo(snapshot.row.id);
  const focusRowBody = (selection: BodyFocus = "new") => rowBodyFocus.current ? rowBodyFocus.current(selection)
    : bodyRef.current?.querySelector<HTMLElement>("[contenteditable='true']")?.focus();
  useNewRowTitleCaret(snapshot.row.id, titleEditable, findRootRef);
  useFullPageTitleFocus(mode === "full" && bodyEmpty && titleEditable && !phone, mode === "full" && !titleEditable && !snapshot.snapshotIdentity.startsWith(ROW_PAGE_READING), snapshot.row.id, panelRef, findRootRef);
  useFullRowWindowTitle(mode === "full", title, { icon: snapshot.row.icon ?? "", iconType: snapshot.row.iconType ?? "none", workspaceId, id: snapshot.row.id, drawn: { inTrash: Boolean(snapshot.row.trashedAt), parentId: snapshot.row.containerPageId } });
  // The body's first line goes up to this title on ↑, ← and Backspace, as a
  // page's (2026-11-02: nothing took it, so the line stayed put).
  useLayoutEffect(() => {
    const title = titleEditable ? findRootRef.current?.querySelector<HTMLElement>("[data-row-page-title]") : null;
    return title ? registerEditable(`title:${snapshot.row.id}`, title) : undefined;
  }, [snapshot.row.id, titleEditable]);
  // The heading follows the row as the view holds it, which takes each save at once.
  const heading = useRowPageTitle({ rowId: snapshot.row.id, record: titleProperty && { propertyId: titleProperty.id, hostPageId: snapshot.row.containerPageId }, cell: !titleProperty ? undefined
    : typeof valueRow?.title === "string" ? { value: valueRow.title, spans: valueRow.titleSpans } : snapshot.row.values[titleProperty.id],
    write: titleProperty && onRichTextChange ? (value, spans, read) => onRichTextChange(titleProperty, value, spans, read) : undefined, named: snapshot.row }), titleSpans = heading.spans;
  const presentation: RowPagePresentation = {
    id: snapshot.row.id, workspaceId: workspaceId ?? snapshot.workspaceId, icon: snapshot.row.icon,
    iconType: snapshot.row.iconType, cover: snapshot.row.cover ?? "", coverPosition: snapshot.row.coverPosition ?? 50,
  };
  const pageStyle = { font: snapshot.row.font ?? "default", smallText: Boolean(snapshot.row.smallText), fullWidth: Boolean(snapshot.row.fullWidth), tableOfContentsDisplay: snapshot.row.tableOfContentsDisplay ?? "default" };
  const showMarginDiscussions = mode === "full" && !body && ((wiki ? databaseLayout : pageLayout)?.options.inlineComments ?? "default") !== "minimal";
  const transfer = useRowPageTransfer({
    rowId: snapshot.row.id, title: storedTitle, workspaceId, session: body ? undefined : session,
    readOnly: bodyReadOnly !== false || lockedForMe,
  });

  useEffect(() => {
    sideWidthRef.current = sideWidth;
  }, [sideWidth]);
  const marginCompose = useMarginCompose(), takeMarginCompose = marginCompose.take;
  // The body's comment buttons and suggestions open the row's discussion.
  useEffect(() => {
    if (!onOpenComments) return;
    function open(event: Event) {
      const detail = (event as CustomEvent<OpenCommentsDetail>).detail;
      if (detail?.pageId === snapshot.row.id && !takeMarginCompose(detail, !snapshot.row.trashedAt)) onOpenComments?.(detail);
    }
    window.addEventListener(OPEN_COMMENTS_EVENT, open);
    return () => window.removeEventListener(OPEN_COMMENTS_EVENT, open);
  }, [takeMarginCompose, onOpenComments, snapshot.row.id, snapshot.row.trashedAt]);
  // The page makes room for the peek beside it and its views mark the item (Notion's, seen 2026-11-03).
  useSideRowPeekPlace(panelRef, mode === "side", sideWidth, snapshot.row.id);
  useEffect(() => {
    // Focus goes back where a key left it (peekFocusReturn).
    const giveBack = peekFocusReturn(panelRef);
    // The panel takes focus unless a field of it (its title) has it, or typing is held for its new row's title
    // or body; a peek opens with the cursor at its title's end, as Notion's (measured 2026-11-15 in Chrome at
    // 1440, no script in the page: after a row's "열기" or a group's "+", a typed "z" went after the side peek
    // title's last letter; center seen 2026-11-04) — a center one's first button the layer gave it too, with
    // its ring when opened by its address (peekFocusReturn.ts); a touch screen's leaves the title (openingTakesTitleCaret).
    // The caret goes there once the title's editor is made, what is typed meanwhile waiting for it (takePeekFocus).
    const focus = takePeekFocus(panelRef, findRootRef, mode !== "full" && titleEditable && openingTakesTitleCaret(), () => setEntered(true));
    return () => { focus(); giveBack(); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [snapshot.row.id]);
  useEffect(() => {
    if (!resizingSide) return;
    function move(event: PointerEvent) {
      if (!resizeStartRef.current) return;
      const next = clampRowPeekWidth(
        resizeStartRef.current.startWidth + (resizeStartRef.current.startX - event.clientX) * resizeStartRef.current.sign,
      );
      sideWidthRef.current = next;
      setSideWidth(next);
    }
    function stop() {
      setResizingSide(false);
      resizeStartRef.current = null;
      storeRowPeekWidth(sideWidthRef.current);
    }
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop, { once: true });
    window.addEventListener("pointercancel", stop, { once: true });
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
      window.removeEventListener("pointercancel", stop);
    };
  }, [resizingSide]);

  escapeRef.current = () => {
    if (findOpen) setFindOpen(false);
    else if (!peekControlKeepsEscape(panelRef.current)) onClose();
  };

  function onPanelKeyDown(event: ReactKeyboardEvent<HTMLElement>) {
    if (primaryModifier(event) && chordLetter(event) === "f") {
      event.preventDefault();
      setFindOpen(true);
      setFindFocusTick((current) => current + 1);
      return;
    }

  }

  function commitSideWidth(next: number) {
    const width = clampRowPeekWidth(next);
    sideWidthRef.current = width;
    setSideWidth(width);
    storeRowPeekWidth(width);
  }

  function startSideResize(event: ReactPointerEvent<HTMLDivElement>) {
    if (mode !== "side" || event.button !== 0) return;
    event.preventDefault();
    resizeStartRef.current = { startX: event.clientX, startWidth: sideWidthRef.current, sign: inlineSign(event.currentTarget) };
    setResizingSide(true);
  }

  function onResizeKeyDown(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (mode !== "side" || !["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    const step = event.shiftKey ? 80 : 24;
    if (event.key === inlineStartKey(event.currentTarget)) commitSideWidth(sideWidthRef.current + step);
    else if (event.key !== "Home" && event.key !== "End") commitSideWidth(sideWidthRef.current - step);
    else if (event.key === "Home") commitSideWidth(rowPeekMinWidth());
    else commitSideWidth(rowPeekMaxWidth());
  }

  const motionState = entered ? "open" : "opening";
  const rowPeekStyle = mode === "side" ? ({ "--row-peek-width": `${sideWidth}px` } as CSSProperties)
    : mode === "full" && fullRect ? ({ left: fullRect.left, top: fullRect.top, width: fullRect.width, height: fullRect.height } as CSSProperties) : undefined;
  // A peek opens whole (⤢) with what it shows handed over (rowPageHandoff.ts); its menu's updates open it so too (RowPageMenuExtras.openWhole).
  const openWhole = mode !== "full" && onOpenMode ? () => { handRowPageOver(snapshot, valueRow, findRootRef.current); onOpenMode("full"); } : undefined;
  const backLabel = t("databaseView:backToDatabase", { title: containerTitle?.trim() || t("databaseView:databaseFallbackTitle") });
  // A locked page's "Locked" follows its name in its path as a page's does (TopBar), or the open-as-page button in a peek, as Notion's.
  const lock = topBar?.lock;
  // A row page in the trash has its site's bar under the trash band (PageTopBands).
  const trashBanner = <><DatabaseRowTrashBanner trashedAt={snapshot.row.trashedAt} trashedBy={snapshot.row.trashedBy}
    currentUserId={currentUserId} onRestore={onRestore} onDeleteForever={onDeleteForever} {...trashDeletion} />
    {session && snapshot.row.trashedAt ? <PageSiteBand session={session} workspaceId={snapshot.workspaceId} pageId={snapshot.row.id} /> : null}</>;
  const rowLock = (className?: string) => snapshot.row.locked && !snapshot.row.trashedAt ? (
    <TopBarLock className={className} pageId={snapshot.row.id} unlockedForMe={unlockedForMe} lockedBy={snapshot.row.lockedBy}
      onUnlockForMe={lock ? () => { noteUnlockedInThisTab(snapshot.row.id, true); lock.onLockForMe(true); } : undefined}
      onLockAgain={lock ? () => { noteUnlockedInThisTab(snapshot.row.id, false); lock.onLockForMe(false); } : undefined}
      onUnlockForEveryone={lock ? () => lock.onLock(false) : undefined} />
  ) : null;
  const placed = pageLayout ? pageLayoutPlacedProperties(pageLayout) : null;
  // A property's settings, the layout builder and its discussions, which its name opens in the list and among the pinned (usePinnedPropertyMenu).
  const propertyMenu = { editing: headHeld ? undefined : propertyEditing, onCustomizeLayout: customizeLayout, comments: onOpenComments && session ? {
    containerPageId: snapshot.row.containerPageId, rowId: snapshot.row.id, session,
    workspaceId: workspaceId ?? snapshot.workspaceId, canEditPage, openByProperty: marginDiscussions && showMarginDiscussions ? new Map() : comments.openByProperty,
    wholeValueProperties: comments.wholeValueProperties, canComment: snapshot.accessRole !== "view" && !snapshot.row.trashedAt,
  } : undefined };
  const propertyList = (shape: Pick<Parameters<typeof RowPagePropertyList>[0], "layout" | "only" | "visitor">) => (
    <RowPagePropertyList rowId={snapshot.row.id} properties={properties} values={snapshot.row.values}
      access={{ hidden: snapshot.row.hiddenValues, readOnly: snapshot.row.readOnlyValues }} valueRow={valueRow} propertyValueContext={propertyValueContext} editableRow={editableRow}
      onRichTextChange={onRichTextChange} propertyCellEnvironment={propertyCellEnvironment} propertyEditing={propertyMenu.editing} onAddProperty={headHeld ? undefined : onAddProperty}
      onCustomizeLayout={customizeLayout} comments={propertyMenu.comments} {...shape} />
  );
  // A wiki page's other properties, without the wiki's nesting.
  const wikiPlaced = wiki ? new Set([...wiki.barPropertyIds, ...wiki.hiddenPropertyIds]) : null;
  const wikiMore = wikiPlaced && (onAddProperty && !headHeld
    || properties.some((property) => property.type !== "title" && !wikiPlaced.has(property.id)))
    ? propertyList({ layout: { items: [], placed: wikiPlaced, icons: true } }) : null;
  // A wiki's template shows its comments as a database page does, as Notion's (seen 2026-11-01); its pages, as the wiki's page comments switch says (useWikiPageComments).
  const wikiCommentsShown = !wiki || Boolean(template) || databaseLayout?.options.pageComments !== false && (comments.open > 0 || wikiComposing);
  // One who adds none — a page in Trash (comment_trash.go), a reader of a page or template (seen 2026-11-15) — reads its threads; with none, no comments block, as Notion's.
  const inTrash = Boolean(snapshot.row.trashedAt), readsOnly = snapshot.accessRole === "view" && !snapshot.snapshotIdentity.startsWith(ROW_PAGE_READING);
  const pageComments = onOpenComments && session && wikiCommentsShown && !((inTrash || readsOnly) && comments.open === 0) ? (
    <RowPageComments rowId={snapshot.row.id} containerPageId={snapshot.row.containerPageId} session={session} autoFocus={wikiComposing}
      workspaceId={workspaceId ?? snapshot.workspaceId} canEditPage={canEditPage && !inTrash} canComment={snapshot.accessRole !== "view" && !inTrash} />
  ) : null;
  const wikiVerification = wiki ? properties.find((property) => property.type === "verification" && wiki.barPropertyIds.includes(property.id)) : undefined;
  const wikiVerified = wikiVerification
    && verificationPropertyValue(editableRow?.properties?.[wikiVerification.id] ?? snapshot.row.values[wikiVerification.id]?.value)?.state === "verified";
  const rowVerification = useRowPageVerification(snapshot.row.sourceInTrash ? null : session, snapshot.row.id); // none read in a trashed source
  // A database naming a Verification property verifies its pages with it,
  // as a wiki does: "인증 추가" opens that property's value.
  const namedVerification = !wiki && rowVerification && !rowVerification.failed && rowVerification.byProperty
    ? properties.find((property) => property.type === "verification") : undefined;
  const namedVerified = namedVerification
    && verificationPropertyValue(editableRow?.properties?.[namedVerification.id] ?? snapshot.row.values[namedVerification.id]?.value)?.state === "verified";
  const namedControls = namedVerification && !namedVerified && editableRow ? (
    <RowPageWikiControls onVerify={() => findRootRef.current
      ?.querySelector<HTMLElement>(`[data-row-property-id="${CSS.escape(namedVerification.id)}"] button[aria-haspopup='dialog']`)?.click()} />
  ) : undefined;
  // The badge over the title follows the Verification value the page is
  // verified with as it changes here, before the server is read again.
  const verifiedWith = wikiVerification ?? namedVerification;
  const verifiedValue = verifiedWith
    ? verificationPropertyValue(editableRow?.properties?.[verifiedWith.id] ?? snapshot.row.values[verifiedWith.id]?.value) : null;
  const verifiedNow = verifiedValue?.state === "verified";
  const shownVerification = rowVerification && !rowVerification.failed && verifiedWith ? {
    ...rowVerification, page: { ...rowVerification.page,
      // No badge over the title of a page in an ordinary database, its value says it, as Notion's (seen 2026-11-05).
      verifiedAt: verifiedNow && wikiVerification ? verifiedValue?.date?.start ?? "" : "",
      verifiedBy: verifiedNow ? String(verifiedValue?.verified_by ?? session?.user.id ?? "") : "",
      verificationExpiresAt: verifiedNow ? verifiedValue?.date?.end ?? "" : "" },
  } : rowVerification;
  const wikiControls = wiki ? (
    <RowPageWikiControls
      onVerify={wikiVerification && !wikiVerified && editableRow && propertyCellEnvironment
        ? () => setWikiVerifyRequest((count) => count + 1) : undefined}
      onComment={onOpenComments && session && snapshot.accessRole !== "view" && !wikiCommentsShown ? () => setWikiComposing(true) : undefined} />
  ) : undefined;
  const backlinks = session && workspaceId ? (
    <PageHeaderBacklinks session={session} pages={[]}
      // Only the item's ID and workspace are read; a database item is mentioned by its row's ID.
      page={{ id: snapshot.row.id, workspaceId } as Parameters<typeof PageHeaderBacklinks>[0]["page"]} />
  ) : null;
  // An empty body's template prompt, which stands where the page's starter would, its words alone where its templates aren't the reader's (RowPageTemplatePrompt).
  // A body of one empty line, as a template with nothing in it gives, shows neither (seen 2026-11-02).
  const templatePrompt = Boolean(bodyEmpty && !body && (onCreateTemplate || !template) && bodyReadOnly === false && !lockedForMe
    && (pageLayout?.options.templates ?? true) && !templatesSet && !takingTemplate);
  // An empty body's room, under its template prompt or its starter, is the
  // body's own: a press there starts the writing on its first line, as one on
  // the prompt's words does (Notion's, seen 2026-11-07: a row page's prompt,
  // and a new page's window from the sidebar's "+" and a page's peeks).
  const writeFromRoom = templatePrompt || (bodyEmpty && !body && bodyReadOnly === false && !lockedForMe) ? {
    "data-writes-from-room": "", onMouseDown: (event: React.MouseEvent) => { if (event.target === event.currentTarget) event.preventDefault(); },
    onClick: (event: React.MouseEvent) => {
      if (event.target !== event.currentTarget) return;
      if (templatePrompt) setTemplatesSet(true);
      focusRowBody();
    },
  } : undefined;
  const pageBody = (
    // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions, jsx-a11y/click-events-have-key-events -- Enter in the title or body does the same.
    <section ref={bodyRef} className={styles.rowPeekEditor} aria-label={t("editor:content.pageBody")}
      data-under-wiki-line={wiki && !pageComments && !pageLayout ? "true" : undefined} {...writeFromRoom}>
      {/* The prompt stands on the empty body's first line, as Notion's (seen 2026-11-01); the editor's room follows. */}
      {templatePrompt
        ? <RowPageTemplatePrompt dataSourceId={snapshot.row.dataSourceId} untitledName={template ? undefined : untitledRow} templates={templates} onCreate={onCreateTemplate} onWrite={() => { setTemplatesSet(true); focusRowBody(); }}
          onApply={(templateId) => {
            setTemplatesSet(true);
            if (session) void chooseRowTemplate(session, snapshot.row, templateId).then(() => {
              setBodyGeneration((current) => current + 1);
              onHistoryRestored?.();
            }, () => setTemplatesSet(false));
          }} /> : null}
      {body ?? (session && workspaceId && (bodyReadOnly === false || snapshot.body.length > 0) ? (
        <RowPageBodyEditor key={`${snapshot.row.id}:${bodyGeneration}`} session={session}
          rowId={snapshot.row.id} workspaceId={workspaceId} tableOfContentsDisplay={snapshot.row.tableOfContentsDisplay} readOnly={bodyReadOnly !== false || lockedForMe} lockedForMe={lockedForMe} databasesLockable={lockedForMe && bodyReadOnly === false}
          suggesting={suggesting.active} peek={mode === "full" ? undefined : mode} whole={mode === "full"} onSuggestingChange={suggesting.control?.onChange}
          onCanSuggestChange={suggesting.onBodyCanSuggest} onContentChange={hearBodyContent} fullPageDatabase={fullPageDatabase}
          template={snapshot.template === true} pageStarter={!templatePrompt && !templatesSet && bodyEmpty && !takingTemplate} focusRef={rowBodyFocus}
          presentAs={{ id: snapshot.row.id, workspaceId, title: storedTitle, icon: presentation.icon, iconType: presentation.iconType, kind: fullPageDatabase ? "database" : "page" } as ProductPage} />
      // A body nobody here can write that has nothing in it shows nothing, as Notion's (seen 2026-10-30, in trash).
      ) : snapshot.body.map((block) => (
        <div className={styles.block} data-block-type={block.type} key={block.id}>
          {readableValue(block.content) || "\u00A0"}
        </div>
      )))}
    </section>
  );
  const page = (
    <>
      {mode === "center" && (
        <button type="button" className={styles.rowPeekBackdrop} data-mode={mode} hidden={underTemplate || undefined}
          data-row-peek-backdrop data-motion-state={motionState} tabIndex={-1}
          aria-label={t("databaseView:closeRowPreview")} onClick={onClose} />
      )}
      <aside ref={panelRef} className={styles.rowPeek} data-mode={mode} data-row-peek-panel hidden={underTemplate || undefined} data-phone={phone ? "true" : undefined} data-stack-columns={mode === "side" && sidePeekStacksColumns(sideWidth) ? "true" : undefined}
        data-motion-state={motionState} data-resizing={resizingSide ? "true" : undefined}
        style={rowPeekStyle} role={mode === "full" ? "region" : "dialog"} aria-modal={mode === "center" ? true : undefined}
        aria-label={mode === "full" ? title : t("databaseView:rowPreviewFor", { title })} tabIndex={-1} onKeyDown={onPanelKeyDown}>
        {mode === "side" && (
          <div className={styles.rowPeekResizeHandle} role="separator" tabIndex={0}
            aria-label={t("databaseView:resizeSidePreview")} aria-orientation="vertical"
            aria-valuemin={rowPeekMinWidth()} aria-valuemax={rowPeekMaxWidth()} aria-valuenow={sideWidth}
            onPointerDown={startSideResize} onKeyDown={onResizeKeyDown} />
        )}
        <PageFindBar focusTick={findFocusTick} onClose={() => setFindOpen(false)} open={findOpen}
          pageId={snapshot.row.id} revision={`${snapshot.containerSnapshotRevision}:${snapshot.bodySnapshotRevision}`}
          rootRef={findRootRef} />
        <div className={styles.rowPeekTop} data-row-peek-top={mode === "full" ? undefined : ""} data-topbar-bar>
          <div className={styles.rowPeekChromeSide} data-row-peek-chrome-side data-path={mode === "full" ? "true" : undefined}
            aria-label={t("databaseView:peekOptions", { title })}>
            {mode === "full" ? (
              <>
                <TopBarSidebarToggle />
                <RowPagePath row={snapshot.row} title={title} untitled={fullPageDatabase ? undefined : untitledRow} spelled={{ title: storedTitle, titleSpans }} workspaceId={snapshot.workspaceId} database={pathDatabase} onReturn={onClose} currentOnly={phone} trailing={phone ? undefined : rowLock()}
                  rename={titleEditable && titleProperty ? {
                    title: storedTitle, titleSpans, onPresentationChange, onTitleChange: heading.rename,
                  } : undefined} fallback={(
                  <button type="button" className={styles.rowPeekBack} data-row-page-back
                    aria-label={backLabel} title={backLabel} onClick={onClose}>
                    <ArrowLeft size="regular" />
                    <span>{containerTitle?.trim() || t("databaseView:databaseFallbackTitle")}</span>
                  </button>
                )} />
                {phone ? rowLock(styles.rowPeekTextAction) : null}
              </>
            ) : (
              <>
                {/* A center peek closes from outside it or with Escape, as Notion's does. */}
                {mode === "side" ? (
                  <button type="button" className={styles.rowPeekIconAction} data-row-peek-close
                    aria-label={t("databaseView:closeRowPreview")} onClick={onClose}>
                    <DoubleChevronRight size="regular" />
                  </button>
                ) : null}
                {onOpenMode ? (
                  <button type="button" className={styles.rowPeekIconAction} data-row-peek-open-page
                    aria-label={t("databaseView:openAsFullPage")} onClick={openWhole}
                    onAuxClick={(event) => { if (event.button === 1) onOpenMode("new_tab"); }}>
                    <OpenAsPage size="small" />
                  </button>
                ) : null}
                {/* Moving it is changing it: a reader who may only view the item gets no move. */}
                {bodyReadOnly === false ? <RowPeekAddTo snapshot={snapshot} title={title} containerTitle={containerTitle} session={session} addTo={freshRow.addTo} moving={mode === "side"} /> : null}
                {rowLock(styles.rowPeekTextAction)}<RowPathPiece row={snapshot.row} title={title} untitled={fullPageDatabase ? undefined : untitledRow} spelled={{ title: storedTitle, titleSpans }} workspaceId={snapshot.workspaceId} database={pathDatabase} />
                {topBar?.onChangeMode ? <RowPeekViewOptions mode={mode} onOpenMode={(next) => { if (next === "full") handRowPageOver(snapshot, valueRow, findRootRef.current); topBar.onChangeMode?.(next); }} onChangeDefault={topBar.onChangeDefaultMode} /> : null}
                {topBar?.siblings ? <RowPeekSiblingButtons siblings={topBar.siblings} /> : null}
              </>
            )}
          </div>
          <DatabaseRowPageActions snapshot={snapshot} title={title} mode={mode} phone={phone} session={session} valueRow={valueRow}
            topBar={topBar} suggesting={suggesting} canEditPage={canEditPage} onOpenComments={onOpenComments} comments={comments}
            details={details} onOpenActions={onOpenActions} openWhole={openWhole} menuExtras={() => ({
              suggesting: suggesting.control,
              onHistory: session && workspaceId && !body ? () => setHistoryOpen(true) : undefined,
              style: onPresentationChange ? { value: pageStyle, onChange: (change) => void onPresentationChange(change) } : undefined,
              onExport: transfer.onExport, onImport: transfer.onImport,
              page: session && workspaceId ? {
                id: snapshot.row.id, title, workspaceId, session, dataSourceId: snapshot.row.dataSourceId,
                sendable: properties.some((property) => property.type === "person"), layoutHeld: !customizeLayout && !suggesting.active && !wiki,
                shownStyle: pageStyle, layout: wiki ? undefined : { pageId: snapshot.row.containerPageId, dataSourceId: snapshot.row.dataSourceId, lockHeld: Boolean(snapshot.databaseLocked) && !suggesting.active } } : undefined,
              onCustomizeLayout: customizeLayout, openWhole,
            })} />
        </div>
        {session ? <PageTopBands session={session} workspaceId={snapshot.workspaceId} pageId={snapshot.row.id} trashed={Boolean(snapshot.row.trashedAt)} /> : null}
        {/* A deleted template's trash banner leads its own, as Notion's (seen 2026-10-30). */}
        {banner ? <>{trashBanner}{banner}</> : null}
        <div className={`${styles.rowPeekScroll} nscroll`} data-page-scroll data-side-panel={sidePanelShown ? "true" : undefined}>
          {banner ? null : trashBanner}
          <DatabaseRowArchiveBanner archivedAt={snapshot.row.archivedAt} archivedBy={snapshot.row.archivedBy}
            currentUserId={currentUserId} onUnarchive={onUnarchive} />
          <RowPageCover row={presentation} compact={mode !== "full"} onChange={onPresentationChange} />
          <div ref={findRootRef} className={styles.rowPeekDoc} data-has-cover={presentation.cover ? "true" : "false"}
            data-font={pageStyle.font} data-small-text={pageStyle.smallText} data-full-width={pageStyle.fullWidth || pageLayout?.options.fullWidth === true} data-row-page="true" data-row-page-row={snapshot.row.id} data-row-page-mode={mode} data-row-peek-search-root
            data-margin-discussions={marginDiscussions && showMarginDiscussions ? "true" : undefined}>
            <RowPageHeading row={presentation} onChange={onPresentationChange} onCustomizeLayout={customizeLayout} controls={wikiControls} backlinks={backlinks}
              verifyControl={rowPageVerifyControl(rowVerification, bodyReadOnly !== false || suggesting.active) ?? namedControls} titleEmpty={storedTitle === ""} compact={mode === "side"}
              layoutHeading={!wiki && mode !== "full"}>
              <RowPageVerification verification={shownVerification} readOnly={bodyReadOnly !== false}
                organizationId={propertyCellEnvironment?.snapshot.organization?.id} />
              {titleEditable && titleProperty ? (
                // The heading edits the row's title property, as a database page's
                // title does in Notion, saving as the typing pauses and as it finishes.
                // Enter goes to the body's first line, as a page title's does (pageBodyFrame: not a body kept unseen).
                <RichTextCellEditor key={snapshot.row.id} afterPaint lineRules holds="title" handleRef={titleHandle} element="h1" className={styles.rowPeekTitle} commitOn={titleCommitOn}
                  spans={titleSpans} value={heading.value} pageId={snapshot.row.id} names={snapshot.row.id} propertyId={titleProperty.id} inviteOn={snapshot.row.id} follows={!template} kept={heading.kept}
                  ariaLabel={t("rowProperties:rowTitle")} placeholder={fullPageDatabase ? inlineDatabasePlaceholderTitle() : untitledRow} onKeyDown={rowTitleKeys(snapshot.row.id, titleHandle, focusRowBody)}
                  onEnter={() => enterBodyOrTitleStart(findRootRef.current?.querySelector<HTMLElement>("[data-row-page-title]") ?? null, focusRowBody)}
                  dataAttributes={{ "data-row-page-title": "", dir: "auto" }} pastedLines={bodyReadOnly === false && !body && !fullPageDatabase ? { titleOf: snapshot.row.id } : undefined}
                  onChange={heading.save} pageTitle={{ comment: () => onOpenComments && session && snapshot.accessRole !== "view" && !snapshot.row.trashedAt ? () => onOpenComments({ pageId: snapshot.row.id, blockId: null, compose: true }) : undefined }} />
              ) : <h1 className={styles.rowPeekTitle}>{titleSpans ? <RichTextCellText spans={titleSpans} interactive /> : title}</h1>}
              {wiki ? (
                <RowPageWikiProperties bar={wiki.barPropertyIds} properties={properties} values={snapshot.row.values} valueRow={valueRow}
                  editableRow={editableRow} propertyValueContext={propertyValueContext} onRichTextChange={onRichTextChange}
                  propertyCellEnvironment={propertyCellEnvironment} more={wikiMore} verifyRequest={wikiVerifyRequest}
                  lined={!pageComments} />
              ) : pageLayout ? (
                <RowPagePinnedProperties pinned={pageLayout.heading.pinned} properties={properties} labels={pageLayout.heading.labels}
                  icons={pageLayout.options.propertyIcons} values={snapshot.row.values} valueRow={valueRow} editableRow={editableRow}
                  propertyValueContext={propertyValueContext} onRichTextChange={onRichTextChange} propertyCellEnvironment={propertyCellEnvironment} details={details} menu={propertyMenu} />
              ) : null}
            </RowPageHeading>
            {pageLayout ? (
              <RowPageLayoutTabs layout={pageLayout} row={valueRow} context={propertyValueContext} underDetails={detailsToggleLast(pageLayout.heading.pinned, properties, details)} memory={snapshot.row.dataSourceId} onSaveTabView={session ? (tabId, view) =>
                saveDatabasePageLayoutTabView(session, snapshot.row.containerPageId, snapshot.row.dataSourceId, tabId, view) : undefined}>
                <RowPageLayoutBody layout={pageLayout} details={details} properties={properties} frameMode={phone ? "phone" : mode} valueOf={(propertyId) => valueRow.properties?.[propertyId]}
                  renderModule={(module) => module.kind === "properties"
                  ? propertyList({ visitor: !session, layout: { items: pageLayout.propertyGroup, placed: placed!, icons: pageLayout.options.propertyIcons } })
                  : module.kind === "property" ? propertyList({ visitor: !session, only: module.propertyId, layout: { items: [], placed: new Set(), icons: pageLayout.options.propertyIcons } })
                    : module.kind === "comments" ? (pageLayout.options.pageComments ? pageComments : null)
                      : pageBody} />
              </RowPageLayoutTabs>
            ) : <>
              {wiki ? null : propertyList({ visitor: !session })}
              {pageComments}
              {pageBody}
            </>}
            {showMarginDiscussions && session && workspaceId ? (
              <MarginDiscussions pageId={snapshot.row.id} rowId={snapshot.row.id} session={session} workspaceId={workspaceId}
                canEditPage={bodyReadOnly === false && !lockedForMe} canComment={Boolean(onOpenComments) && !snapshot.row.trashedAt} compose={marginCompose.compose} onComposeClose={marginCompose.closeCompose} onFitChange={marginCompose.setRoom} onActiveChange={setMarginDiscussions} />
            ) : null}
          </div>
        </div>
      </aside>
      {historyOpen && session && workspaceId ? (
        <RowPageHistoryDialog rowId={snapshot.row.id} title={storedTitle} workspaceId={workspaceId}
          lockedForMe={lockedForMe} properties={properties} session={session} onClose={() => setHistoryOpen(false)}
          onRestored={() => { setBodyGeneration((current) => current + 1); onHistoryRestored?.(); }} />
      ) : null}
      {transfer.surface}
      <CellCommentMarks threadIds={comments.wordThreads} />
    </>
  );
  return typeof document === "undefined" ? page : createPortal(page, document.body);
}
