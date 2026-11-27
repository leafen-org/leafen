import { optionKeyName } from "@/lib/keyboardShortcuts";
import { editorBlockChildren } from "./editorBlockIndex";
import { api } from "@/api";
import { databasePageLeftoversAhead, forgetDatabasePageAhead, readDatabasePageAhead } from "@/lib/databasePageAhead";
import { NUMBERED_LIST_FORMATS, numberedListRunStart, numberedListStoredFormat } from "@/lib/numberedList";
import { BULLETED_LIST_FORMATS, bulletedListFormat, bulletedListRun, quoteSize } from "@/lib/blockMarkerFormats";
import { Fragment, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useBlockHandleDrag } from "./useBlockHandleDrag";
import { useTranslation } from "react-i18next";
import { DragHandleIcon, Plus } from "@/icons/leafen";
import { commentOnOwnBlock } from "./blockOwnComment";
import type { GraphBlock } from "@/contentGraph";
import { copyText } from "@/lib/copyText";
import { pageHref } from "@/lib/navigation";
import { pushRoute } from "@/lib/router";
import { openPagePeek } from "@/lib/pagePeek";
import { useProductPages } from "@/components/product/productPagesContext";
import { useOpenPageGraphStore } from "@/lib/pageGraphStore";
import { popoverRectOf, type PopoverRect } from "@/lib/popoverPlacement";
import { useOptionalProductToast } from "@/lib/productToast";
import { useDisplayPreferences } from "@/lib/displayPreferences";
import { blockTextDirection, isRightToLeftLanguage, TEXT_DIRECTION_BLOCK_TYPES, type TextDirection } from "@/lib/textDirection";
import { BlockMoveToDialog } from "./BlockMoveToDialog";
import { PageBlockMoveDialog, pageBlockMoving } from "./PageBlockMoveDialog";
import { moveBlocksToPage, turnBlocksIntoPageIn } from "./blockMoveHistory";
import { blocksMoving, moveBlocksIntoDatabase } from "./blockMoveIntoDatabase";
import { nativeTiptapSelectionContext } from "./nativeTiptapSelectionContext";
import { SlashMenu, type SlashMenuAnchor } from "./SlashMenu";
import { BlockActionMenu, type BlockPageInAnchor } from "./BlockActionMenu";
import { BlockMenuFooter } from "./BlockMenuFooter";
import { blockCaptionHeld, blockCaptionShown } from "./blockCaptionModel";
import { applyBlockColor, blockMenuCountedTexts, blockMenuOwnTexts, blockMenuSelection } from "./blockHandleModel";
import { blockMenuActions } from "./blockMenuActions";
import { selectedRootsInOrder } from "./blockSelectionKeyboard";
import { type BlockTurnChoice, chosenAfterTurn, turnBlocksInto } from "./blockTurnInto";
import { PageInPicker } from "./PageInPicker";
import { pageInUnavailable } from "./pageInPickerModel";
import { blockItemText } from "./blockItemLabels";
import { editorSelectionCopiesDatabase } from "./editorGraphModel";
import { isPageBlock } from "./nativeEditorGraphAdapter";
import type { EditorSelection, NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { caretOnceAnswered, focusBlockControlSettled, focusEditable, focusEditableNextFrame, holdTypingForCaret } from "./focus";
import { focusNativeTiptapLineEnd, nativeTiptapLineAbove } from "./nativeTiptapFocus";
import { requestSlashLine } from "./slashLineRequest";
import { rememberUsedEditorColor, storedEditorColor } from "./colorMemory";
import { type EditorBlockCommand, type EditorBlockMenuAnchor, requestEditorSelectionCommand, useEditorBlockCommand } from "./editorCommands";
import { syncedBlockActions } from "./SyncedBlockContent";
import { turnBookmarkIntoMention } from "./bookmarkMention";
import { requestCalloutIconEdit } from "./CalloutIcon";
import { codeMenu } from "./codeBlockMenu";
import { placeSlashBlock } from "./slashBlockPlacement";
import { SLASH_INLINE_REQUEST, type SlashInlineRequest } from "./slashInlineEvents";
import { syncedBlockMayGoIn, syncedBlockSource } from "./syncedBlockModel";
import { useBlockImageMenu } from "./useBlockImageMenu";
import { useBlockFileMenu } from "./useBlockFileMenu";
import { useBlockDatabaseMenu } from "./useBlockDatabaseMenu";
import { childPageBlockPageId, linkedPageHref, useBlockPageMenu } from "./useBlockPageMenu";
import { FILE_MENU_TYPES } from "./blockFileMenuModel";
import { useSuggestingMode } from "./suggestingMode";
import { databaseFromSimpleTable } from "./databaseSimpleTable";
import { fitSimpleTable, shownSimpleTableWidths } from "./simpleTableColumns";
import styles from "./editorStyles.generated.module.css";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import { NativeTiptapReactContext, useNativeTiptapGraphRedraw } from "./nativeTiptapReactContext";
import { useBlockMenuCaretReturn } from "./useBlockMenuCaretReturn";
import { canPresentPage, requestPresentation } from "@/lib/pageSlides";
import { AddHereTip, HandleTip, IconTipButton } from "../IconTipButton";
import { blockTakesComments } from "./blockMenuShape";
import { inlineStartX } from "@/lib/readingDirection";

const slashBounds = {
  // Whatever is typed after "/" is searched (no length the menu stops at).
  search: { maxQueryCharacters: Number.MAX_SAFE_INTEGER, maxQueryTokens: Number.MAX_SAFE_INTEGER },
};

// The "+" opens its list from its start: its right where the page reads from the right.
function buttonAnchor(button: HTMLElement): SlashMenuAnchor {
  const rect = button.getBoundingClientRect();
  return { left: inlineStartX(rect, 0, button), top: rect.top, bottom: rect.bottom };
}

/** What woke a block's handle (BlockHandleGate.tsx), done once it is up. */
export type BlockHandleStart = { kind: "hover" } | { kind: "add"; above?: boolean } | { kind: "menu" }
  | { kind: "command"; command: EditorBlockCommand; anchor?: EditorBlockMenuAnchor };

export function ActiveBlockHandle({
  adapter,
  block,
  selection,
  onSelect,
  onRelease,
  start,
}: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  selection: EditorSelection;
  /** Selects the block, unless it is already among the selected. */
  onSelect?: () => void;
  /** Lets go of the selected blocks. */
  onRelease?: () => void;
  start: BlockHandleStart;
}) {
  const { phone } = useSidebarLayout(true);
  const editorContext = useContext(NativeTiptapReactContext);
  const editor = editorContext?.controller.editor;
  const caretHeld = useRef(false);
  // A menu a key opened for the chosen blocks (⌘/) gives the focus back where
  // it was, the page or the text, not to the handle, as Notion's (seen 2026-11-06).
  const openedByCommand = useRef(false);
  // From a touch screen's editing tools the menu is the phone's sheet, in the
  // middle of a wider one, as Notion's iPad "전환" (seen 2026-11-13).
  const [actionSheet, setActionSheet] = useState(false);
  const phoneReturnFocus = useRef<HTMLElement | null>(null);
  phoneReturnFocus.current = editor && !editor.isDestroyed ? editor.view.dom : null;
  const [addAnchor, setAddAnchor] = useState<SlashMenuAnchor>();
  const productPages = useProductPages();
  const [actionOpen, setActionOpen] = useState(false);
  // Its open menu counts and names the page's blocks as they are now.
  useNativeTiptapGraphRedraw(actionOpen);
  const [actionPanel, setActionPanel] = useState<"main" | "color" | "turn">("main");
  // A menu opened by right-clicking the block appears at the pointer.
  const [actionAnchor, setActionAnchor] = useState<EditorBlockMenuAnchor>();
  const actionButton = useRef<HTMLButtonElement>(null);
  const addButton = useRef<HTMLButtonElement>(null);
  // The block menu opens where it is asked for, or beside its handle (BlockActionMenu).
  function menuAnchor(anchor?: EditorBlockMenuAnchor): EditorBlockMenuAnchor | undefined {
    if (anchor) return anchor;
    const rect = actionButton.current?.getBoundingClientRect();
    return rect ? { x: inlineStartX(rect, -10, actionButton.current), y: rect.bottom + 4,
      handle: { top: rect.top, bottom: rect.bottom, left: rect.left, right: rect.right } } : undefined;
  }
  // From the keyboard it opens at the block's own box instead (BlockActionMenu): from the group's top, a
  // heading's room above it in, to its foot or where its first nested block starts; 6px out from the
  // block's start — before a to-do's box or a list's mark — or at a simple table's table itself, as Notion's
  // (seen 2026-11-09: the table's left and foot; 2026-11-12: at 487.5 for a to-do as for a line; 2026-11-14
  // and 11-15, two tabs: a heading 1's menu over it ends at its block's top, 501 and 564, as a to-do's, a list's).
  // Its `left` is where the menu starts: the right edge read from the right.
  function textAnchor(): EditorBlockMenuAnchor | undefined {
    const group = actionButton.current?.closest("[data-block-id]");
    const table = block.type === "table" ? group?.querySelector("[data-structured-table] [role='table']")?.getBoundingClientRect() : undefined;
    if (table) return { x: inlineStartX(table, 0, group), y: table.bottom, box: { top: table.top, bottom: table.bottom, left: inlineStartX(table, 0, group) } };
    const own = group?.getBoundingClientRect(), nested = group?.querySelector("[data-block-id][data-depth]")?.getBoundingClientRect();
    const body = group?.querySelector(`:scope > .${styles.blockRow} > .${styles.blockBody}`)?.getBoundingClientRect() ?? group?.firstElementChild?.getBoundingClientRect();
    const start = body && inlineStartX(body, -6, group), bottom = nested?.top ?? own?.bottom;
    return own && bottom !== undefined && start !== undefined ? { x: start, y: bottom, box: { top: own.top, bottom, left: start } } : undefined;
  }
  const [moveOpen, setMoveOpen] = useState(false);
  const [moveAnchor, setMoveAnchor] = useState<PopoverRect | null | undefined>();
  // Where the caret was in the line "Move to" was asked from, for a move to the line's own page.
  const moveCaret = useRef<number | undefined>(undefined);
  // "페이지 위치:" asks for the page to put the blocks in: beside the turn
  // list, or in a window when chosen from the menu's search.
  const [pageInOpen, setPageInOpen] = useState(false);
  const [pageInPlace, setPageInPlace] = useState<BlockPageInAnchor | null>(null);
  const { t, i18n } = useTranslation("blockHandle");
  const toast = useOptionalProductToast();
  // Blocks moved to another page leave the open page at the press (blockMoveHistory).
  const pageGraph = useOpenPageGraphStore();
  const notify = (message: string, tone: "default" | "success" | "error" = "default") => {
    if (toast) toast.notify(message, tone);
    else adapter.moveContext?.onStatus?.(message, tone);
  };
  const selectedIds = useMemo(() => blockMenuSelection(adapter.graph, block, selection), [adapter.graph, block, selection]);
  // What "옮기기" moves: every selected block, in its order.
  const movingBlockIds = () => selectedIds.length > 1
    ? selectedRootsInOrder(adapter.graph, new Set(selectedIds)).map((candidate) => candidate.id) : [block.id];
  // A sub-page's block moving alone goes by its page's move window (PageBlockMoveDialog).
  const pageMoving = moveOpen && adapter.moveContext ? pageBlockMoving(movingBlockIds().map((id) => adapter.graph.blocks.find((one) => one.id === id)), adapter.moveContext.pages) : null;
  const bookmarkUrl = block.type === "bookmark" && typeof block.content.url === "string" ? block.content.url.trim() : "";
  // A link to a page names it by childPageId, or pageId in older content.
  const linkedPageId = block.type !== "link_to_page" ? ""
    : [block.content.childPageId, block.content.pageId].find((id): id is string => typeof id === "string" && id.trim() !== "")?.trim() ?? "";
  // As in Notion's block menu, an inline database of its own becomes a
  // full-page database in its place, and a full-page database's page block
  // an inline database.
  const session = adapter.session;
  const suggesting = useSuggestingMode();
  // Where the blocks may go, as the server allows (pageInPickerModel.ts).
  const workspacePages = adapter.moveContext?.pages;
  const pageInBlocked = useMemo(() => (!pageInOpen && !pageInPlace) || !workspacePages ? undefined
    : pageInUnavailable(workspacePages, adapter.pageId, t("turnPageIn.unavailable")), [adapter.pageId, pageInOpen, pageInPlace, t, workspacePages]);
  const turnDatabase = session && adapter.moveContext && adapter.graphSync
    // A linked database whose views are locked keeps no "페이지로 전환" (Notion's, seen 2026-10-13).
    ? block.type === "inline_database" && !(block.content.linkedDatabaseSource === true && block.content.locked === true)
      ? { into: "page" as const, run: () => { setActionOpen(false); adapter.turnDatabaseIntoPage(block.id, pageGraph); } }
      : isPageBlock(block) && adapter.moveContext.pages.find((page) => page.id === block.id)?.kind === "database"
        ? {
          into: "inline" as const,
          run: () => { setActionOpen(false); adapter.turnDatabaseInline(block.id, pageGraph); },
          leftovers: () => databasePageLeftoversAhead(session.user.id, block.id) ?? api.databasePageLeftovers(session, block.id).catch(() => null),
        }
        : undefined
    : undefined;
  // Its menu open, the database page is read for the turn to answer at the press (databasePageAhead.ts).
  const aheadSession = useRef(session);
  aheadSession.current = session;
  const aheadPrincipal = turnDatabase?.into === "inline" && actionOpen ? session?.user.id ?? null : null;
  useEffect(() => {
    const reader = aheadSession.current;
    if (!aheadPrincipal || !reader) return;
    readDatabasePageAhead(reader, block.id);
    return () => forgetDatabasePageAhead(aheadPrincipal, block.id);
  }, [aheadPrincipal, block.id]);
  // What the menu's foot counts, from the handle only, as Notion's (seen
  // 2026-11-04): the blocks' text with all inside them (blockMenuCountedTexts,
  // blockMenuOwnTexts), none for blocks with no text (2026-11-13: two
  // dividers chosen have no counts, a line and a divider the line's alone).
  function menuTexts() {
    if (actionAnchor?.box) return [];
    const pageOf = (item: GraphBlock) => adapter.moveContext?.pages.find((page) => page.id === childPageBlockPageId(item));
    return blockMenuCountedTexts(adapter.graph, selectedIds, (item) => blockMenuOwnTexts(adapter, item, pageOf));
  }
  // Several blocks' foot names the pressed handle's block's last edit and maker from the handle only (2026-11-26); from
  // a right click their counts alone, from the keys nothing, as Notion's (seen 2026-11-13).
  function footer() {
    const texts = menuTexts(), away = selectedIds.length > 1 && !actionAnchor?.handle && !phone;
    return away && texts.length === 0 ? undefined
      : <BlockMenuFooter session={adapter.session ?? null} pageId={adapter.pageId} blockIds={[block.id]} lastEdit={!away} activity={!phone} texts={texts} />;
  }
  // A color for the selected blocks, kept first among the colors used and as the one ⌘⇧H applies.
  function applyColor(color: string) {
    applyBlockColor(adapter, selectedIds, color);
    rememberUsedEditorColor(color);
    setActionOpen(false);
  }
  // Shortcuts from the block's text or from selected blocks open what this
  // handle owns.
  function runCommand(command: EditorBlockCommand, anchor?: EditorBlockMenuAnchor) {
    // ⌘⇧H applies the color last used, as Notion's does (seen 2026-10-01):
    // to the selected text, else to the selected blocks or the one typed in.
    // Before any color was used, it opens the colors.
    const recent = command === "color" ? storedEditorColor() : null;
    if (recent) {
      if (!requestEditorSelectionCommand("color")) applyColor(recent);
    } else if (command === "menu" || command === "color" || command === "colorMenu" || command === "turn") {
      // A phone's "작업" sheet leaves no caret behind when it closes, as Notion's (seen 2026-11-11).
      caretHeld.current = !(phone && command === "menu") && Boolean(editor?.view.hasFocus());
      openedByCommand.current = true;
      setActionSheet(anchor?.sheet === true);
      setActionPanel(command === "turn" ? "turn" : command === "menu" ? "main" : "color");
      setActionAnchor(anchor ?? textAnchor() ?? menuAnchor());
      setActionOpen(true);
    } else if (command === "imageLink") {
      imageMenu.openLink();
    } else if (command === "move" && adapter.moveContext) {
      setActionOpen(false);
      setMoveAnchor(undefined);
      const typing = editor && !editor.isDestroyed && editor.view.hasFocus() ? nativeTiptapSelectionContext(editor.view.state) : null;
      moveCaret.current = typing?.block.nativeId === block.id && typing.field === "text" ? typing.start : undefined;
      setMoveOpen(true);
    } else if (command === "comment" && adapter.session && blockTakesComments(block)) {
      commentOnOwnBlock(adapter, block);
    } else if (command === "lineBelow") {
      // Enter on a block with no text of its own makes a line after it, as
      // Notion's does on a selected image (seen 2026-10-18).
      const line = adapter.insertAfter(block.id, "paragraph", {});
      if (line) focusEditableNextFrame(line.id, "start");
    } else if (command === "duplicate") {
      duplicate();
    } else if (command === "delete" || command === "deleteToLineAbove") {
      // From the line itself ("/"): the caret goes to the start of the page's
      // title, as Notion's (2026-11-15, worker Chrome, no script before the
      // action: "/삭제" Enter on a middle line, then a letter went in before
      // the title's words, in both), once the removal is answered (a database asks), what is typed meanwhile
      // typed there (caretOnceAnswered). From the phone's tools: to the end of the line above, moved there in the
      // same step before the block goes, so the page keeps it and the keyboard and the tools stay (Notion's
      // phone, seen 2026-11-08, not measured again since); at the page's top, the title's end.
      const above = command === "deleteToLineAbove" && editor ? nativeTiptapLineAbove(editor) : undefined, land = above ? null : caretOnceAnswered();
      void deleteBlocks(above && editor ? () => focusNativeTiptapLineEnd(editor, above) : undefined).then((deleted) => {
        land?.(deleted ? { key: `title:${adapter.pageId}`, position: command === "delete" ? "start" : "end" } : null);
      }, () => land?.(null));
    } else if (command === "copyLink") {
      void copyBlockLink();
    } else if (command === "present") {
      // "/" → "여기에서 발표" starts where the block menu's does.
      if (canPresentPage(adapter.pageId)) requestPresentation({ pageId: adapter.pageId, blockId: block.id });
    } else if (command === "imageFullScreen") {
      if (imageMenu.image) imageMenu.image.onFullScreen(); else fileMenu.file?.onFullScreen?.();
    } else if (command === "imageDownload") {
      if (imageMenu.image) imageMenu.image.onDownload(); else fileMenu.file?.onDownload();
    } else if (command === "mediaOriginal") {
      if (imageMenu.image) imageMenu.image.onViewOriginal(); else fileMenu.file?.onViewOriginal();
    } else if (command === "mediaReplace") {
      if (imageMenu.image) imageMenu.image.onReplace(); else if (fileMenu.relink) fileMenu.relink(); else fileMenu.file?.onReplace();
    } else if (command === "imageCrop") {
      imageMenu.image?.onCrop?.();
    } else if (command === "caption") {
      if (captionVisible !== undefined) toggleCaption();
    } else if (command === "rename") {
      databaseMenu?.rows.find((row) => row.id === "rename" && !row.disabled)?.run();
    } else if (command === "unsyncAll" && block.type === "synced_block" && !adapter.readOnly
      && !syncedBlockSource(block, adapter.pageId).copy) {
      void syncedBlockActions(adapter).unsyncAll(block);
    }
  }
  useEditorBlockCommand(block.id, runCommand, () => actionButton.current);
  const selectedBlocks = selectedIds.map((id) => adapter.graph.blocks.find((item) => item.id === id) ?? block);
  const colors = new Set(selectedBlocks.map((candidate) => String(candidate.content.color ?? "default")));
  const activeColor = colors.size === 1 ? [...colors][0] : "mixed";
  const types = new Set(selectedBlocks.map((candidate) => candidate.type));
  // "텍스트 방향" for text blocks, in a right-to-left language or when always shown.
  const { alwaysShowTextDirection } = useDisplayPreferences();
  const directions = new Set(selectedBlocks.map((candidate) => blockTextDirection(candidate.content)));
  const direction = !adapter.readOnly && selectedBlocks.every((candidate) => TEXT_DIRECTION_BLOCK_TYPES.has(candidate.type))
    && (alwaysShowTextDirection || isRightToLeftLanguage(i18n.language)) ? {
      active: directions.size === 1 ? [...directions][0] : null,
      onSelect: (next: TextDirection) => {
        for (const candidate of selectedBlocks) {
          if (blockTextDirection(candidate.content) !== next) adapter.updateBlock(candidate, { textDirection: next === "auto" ? undefined : next });
        }
        setActionOpen(false);
      },
    } : undefined;
  const captionVisible = blockCaptionShown(block);
  const imageMenu = useBlockImageMenu(adapter, block, () => setActionOpen(false), notify,
    () => actionButton.current?.closest("[data-block-id]") ?? null);
  const fileMenu = useBlockFileMenu(adapter, block, () => setActionOpen(false), notify, () => actionButton.current);
  const pageMenu = useBlockPageMenu(adapter, block, () => setActionOpen(false), notify, actionOpen);
  const databaseMenu = useBlockDatabaseMenu(adapter, block, () => setActionOpen(false), notify, actionOpen) ?? pageMenu;

  const { copyBlockLink, copyBlockLinks, duplicate, deleteBlocks, several } = blockMenuActions({
    adapter, block, selectedIds, close: () => setActionOpen(false), notify, toast, editorContext, t });

  useBlockHandleDrag(actionButton, { pageId: adapter.pageId, blockIds: selectedIds, generation: adapter.generation }, adapter.readOnly);

  // The blocks become pages in the page chosen beside the turn list.
  async function putInPage(destinationPageId: string) {
    const context = adapter.moveContext;
    setPageInPlace(null);
    setActionOpen(false);
    if (!context) return;
    try {
      const caretAtTitle = await turnBlocksIntoPageIn(adapter, selectedIds, selection.has(block.id), destinationPageId, pageGraph);
      void adapter.graphSync?.reread();
      caretAtTitle?.();
    } catch {
      notify(t("turnPageIn.failed"), "error");
    }
  }

  function turnInto(choice: BlockTurnChoice) {
    if (choice.placeIn) {
      setActionOpen(false);
      setPageInOpen(true);
      return;
    }
    const left = turnBlocksInto(adapter, selectedIds, choice);
    setActionOpen(false);
    const chosen = chosenAfterTurn(choice, selectedIds, left);
    if (chosen) editorContext?.setSelection(chosen);
  }

  function toggleCaption() {
    // File media's and a code block's "Caption" open and focus the existing
    // caption, as Notion's do even when it already has words (seen
    // 2026-10-08; code 2026-11-13 — an empty one closes as the caret leaves).
    const showing = FILE_MENU_TYPES.has(block.type) || block.type === "code" || !captionVisible;
    for (const id of selectedIds) {
      const candidate = adapter.graph.blocks.find((item) => item.id === id);
      if (candidate) adapter.updateBlock(candidate, { captionVisible: showing });
    }
    setActionOpen(false);
    if (!showing) return;
    // The caret goes to the caption: no block stays chosen with it.
    editorContext?.setSelection(new Set());
    focusBlockControlSettled(block.id, '[data-block-control="block-caption"]');
  }

  // A line above the block: after the block before it, or first in its parent
  // (the page's own lines too — 2026-11-11 it went to the page's end).
  function lineAbove() {
    const siblings = editorBlockChildren(adapter.graph, block.parentId ?? null);
    const previous = siblings[siblings.findIndex((sibling) => sibling.id === block.id) - 1];
    if (previous) return adapter.insertAfter(previous.id, "paragraph", {});
    return adapter.insertChild(block.parentId ?? null, "paragraph", {}, null);
  }
  function addBelow(button: HTMLElement, above = false) {
    if (phone) {
      setAddAnchor(buttonAnchor(button));
      return;
    }
    // Notion's "+" makes a line below — or takes this one, an empty
    // line — and filters the "/" menu by what is typed there (seen
    // 2026-10-18; it opened a menu nothing could be typed into). With
    // Option it makes the line above the block (seen 2026-11-08).
    const line = above ? lineAbove() : block.type === "paragraph" && !adapter.text(block).trim() ? block : adapter.insertAfter(block.id, "paragraph", {});
    if (!line) return;
    requestSlashLine(line.id);
    focusEditableNextFrame(line.id, "start");
  }

  // "바꾸기" from the menu leaves only its window: the block the menu chose is
  // let go, as Notion's (seen 2026-11-06 on a Miro board: no blue behind the window).
  function replaceLetsGo<T extends { onReplace: () => void }>(menu: T | null | undefined): T | undefined {
    return menu ? { ...menu, onReplace: () => { onRelease?.(); menu.onReplace(); } } : undefined;
  }

  function toggleMenu() {
    // Opening the menu selects its block, which stays selected after
    // the menu closes, as Notion's handle does (seen 2026-10-02).
    if (!actionOpen) onSelect?.();
    openedByCommand.current = false;
    setActionSheet(false);
    setActionPanel("main");
    setActionAnchor(menuAnchor());
    setActionOpen((open) => !open);
  }

  // A menu a key opened from a line gives the caret back as it closes (useBlockMenuCaretReturn.ts).
  const dismissMenu = useBlockMenuCaretReturn({ editor, pageId: adapter.pageId, open: actionOpen, caretHeld, mainMenu: actionPanel === "main", close: () => setActionOpen(false) });

  // What woke the handle is done once, as it comes up, in the same pass (a key's command: hearBlockCommands).
  const startDone = useRef(false);
  useLayoutEffect(() => {
    if (startDone.current) return;
    startDone.current = true;
    if (start.kind === "command") runCommand(start.command, start.anchor);
    else if (start.kind === "add" && addButton.current) addBelow(addButton.current, start.above);
    else if (start.kind === "menu") toggleMenu();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- the start is the handle's first act only
  }, []);

  // Woken by the pointer reaching it, this handle is new under the pointer,
  // which the browser counts as on it only once the pointer moves again: till
  // then it stays shown and takes a press (2026-11-05: on a database's line a
  // press went to the title row under it, and no menu opened).
  const [arrived, setArrived] = useState(start.kind === "hover");
  useEffect(() => {
    if (!arrived) return;
    const moved = () => setArrived(false);
    document.addEventListener("pointermove", moved, true);
    return () => document.removeEventListener("pointermove", moved, true);
  }, [arrived]);

  const ActionLayer = actionSheet ? TouchSheetLayout : Fragment;
  return (
    <div className={styles.gutter} contentEditable={false}
      data-menu-open={addAnchor || actionOpen && !openedByCommand.current ? "true" : actionOpen ? "command" : undefined} data-pointer-arrived={arrived ? "true" : undefined}>
      <IconTipButton ref={addButton} wait="handle" tabIndex={-1} className={styles.gutterBtn} label={t("buttons.addBlockBelow", { key: optionKeyName() })}
        tip={<AddHereTip />} placement="bottom"
        onClick={(event) => addBelow(event.currentTarget, event.altKey)}><Plus size="regular" aria-hidden="true" /></IconTipButton>
      <IconTipButton ref={actionButton} wait="handle" tabIndex={-1} className={styles.gutterBtn} label={t("buttons.dragHint")} tip={<HandleTip drag />}
        placement="bottom" aria-haspopup="menu" aria-expanded={actionOpen} data-native-drag-source
        onClick={toggleMenu}><DragHandleIcon size="regular" aria-hidden="true" /></IconTipButton>
      {addAnchor ? (
        <SlashMenu anchor={addAnchor} bounds={slashBounds} query="" ownerBlockId={block.id} lineCommands={false} workspaceId={adapter.workspaceId}
          syncedBlocks={syncedBlockMayGoIn(adapter.graph, block.parentId)}
          onClose={() => setAddAnchor(undefined)} onPick={(definition) => {
            // A block the menu adds goes right after this block; a database
            // starts as the view the menu names; an import opens the page's.
            if (definition.action === "import") {
              setAddAnchor(undefined);
              const title = productPages?.pages.find(({ id }) => id === adapter.pageId)?.title ?? "";
              productPages?.openImport?.({ pageId: adapter.pageId, title, format: definition.importFormat });
              return;
            }
            if (definition.inline) {
              // An inline item begins in a new line after this block, the
              // caret at its start, as Notion's "+" (2026-11-15, Chrome, no
              // script before the action; judged by the screen and one typed
              // letter: "+" › "날짜 또는 리마인더" left "@" on a new line under
              // the block and the letter went in after it).
              const line = adapter.insertAfter(block.id, "paragraph", {});
              const kind = definition.inline;
              setAddAnchor(undefined);
              if (line) {
                holdTypingForCaret();
                window.requestAnimationFrame(() => window.requestAnimationFrame(() => {
                  focusEditable(line.id, "start");
                  window.dispatchEvent(new CustomEvent<SlashInlineRequest>(SLASH_INLINE_REQUEST, { detail: { blockId: line.id, kind } }));
                }));
              }
              return;
            }
            placeSlashBlock(adapter, block, definition, false);
            setAddAnchor(undefined);
          }} />
      ) : null}
      {imageMenu.overlays}
      {fileMenu.overlays}
      {actionOpen ? (<ActionLayer>
        <BlockActionMenu
          key={actionPanel}
          initialPanel={actionPanel}
          anchor={actionAnchor}
          returnFocusRef={phone ? phoneReturnFocus : openedByCommand.current ? undefined : actionButton}
          footer={footer()}
          activeColor={activeColor}
          activeType={types.size === 1 ? selectedBlocks[0]!.type : "mixed"}
          several={several()}
          captionVisible={blockCaptionHeld(block) ? captionVisible : undefined}
          direction={direction}
          image={selectedIds.length === 1 ? replaceLetsGo(imageMenu.image) : undefined}
          emptyImage={selectedIds.length === 1 ? imageMenu.emptyImage : undefined}
          file={selectedIds.length === 1 ? replaceLetsGo(fileMenu.file) : undefined}
          database={selectedIds.length === 1 ? databaseMenu : undefined}
          selectionCount={selectedIds.length}
          turnIntoSynced={selectedIds.every((id) => syncedBlockMayGoIn(adapter.graph, adapter.graph.blocks.find((item) => item.id === id)?.parentId ?? null))}
          // "목록 형식" stands on a list's first item alone, numbered or bulleted,
          // and sets that item's format, which the items after it without one
          // of their own take, as Notion's (seen 2026-11-10, bullets 2026-11-11);
          // "기본" leaves the item no format of its own.
          listFormat={block.type === "numbered_list_item" && numberedListRunStart(adapter.graph.blocks, block).first.id === block.id ? {
            formats: [["default", t("blockHandle:menu.formatDefault")],
              ...NUMBERED_LIST_FORMATS.map((format) => [format, t(`blockHandle:menu.numbered.${format}`)] as const)],
            active: numberedListStoredFormat(block) ?? "default",
            onSelect: (format) => {
              adapter.updateBlock(block, { listFormat: format === "default" ? undefined : format });
              setActionOpen(false);
            },
          } : block.type === "bulleted_list_item" && bulletedListRun(adapter.graph.blocks, block)[0]?.id === block.id ? {
            formats: [["default", t("blockHandle:menu.formatDefault")],
              ...BULLETED_LIST_FORMATS.map((format) => [format, t(`blockHandle:menu.bullet.${format}`)] as const)],
            active: bulletedListFormat(block) ?? "default",
            onSelect: (format) => {
              adapter.updateBlock(block, { listFormat: format === "default" ? undefined : format });
              setActionOpen(false);
            },
          } : block.type === "quote" ? {
            label: t("blockHandle:menu.quoteSize"),
            formats: [["default", t("blockHandle:menu.formatDefault")], ["large", t("blockHandle:menu.quoteLarge")]],
            active: quoteSize(block) ?? "default",
            onSelect: (format) => {
              adapter.updateBlock(block, { quoteSize: format === "large" ? "large" : undefined });
              setActionOpen(false);
            },
          } : undefined}
          onClose={dismissMenu}
          onColor={applyColor}
          onComment={adapter.session && blockTakesComments(block) ? () => {
            // The block goes unchosen while its comment is written, as Notion's
            // leaves only the words' mark (seen 2026-11-07).
            setActionOpen(false);
            editorContext?.setSelection(new Set());
            commentOnOwnBlock(adapter, block);
          } : undefined}
          onInsertBelow={() => { setActionOpen(false); runCommand("lineBelow"); }}
          onSuggest={suggesting.onChange ? () => { setActionOpen(false); suggesting.onChange?.(!suggesting.active); } : undefined}
          onPresent={canPresentPage(adapter.pageId) && block.type !== "divider" ? () => { setActionOpen(false); requestPresentation({ pageId: adapter.pageId, blockId: block.id }); } : undefined}
          onCopyLink={() => void copyBlockLink()}
          onCopyLinks={() => void copyBlockLinks()}
          onDelete={() => void deleteBlocks()}
          onDuplicate={() => duplicate()}
          onDuplicateWithoutContent={selectedIds.length > 1 || editorSelectionCopiesDatabase(adapter.graph, selectedIds) || databaseMenu?.copiesWithoutContent
            ? () => duplicate(true)
            : undefined}
          onMove={() => { setMoveAnchor(popoverRectOf(actionButton.current)); setActionOpen(false); moveCaret.current = undefined; setMoveOpen(true); }}
          syncedBlock={block.type === "synced_block" && !adapter.readOnly
            ? syncedBlockSource(block, adapter.pageId).copy ? "unsync" : "copy"
            : undefined}
          onSyncedBlock={() => {
            setActionOpen(false);
            const actions = syncedBlockActions(adapter);
            if (syncedBlockSource(block, adapter.pageId).copy) void actions.unsync(block);
            else void actions.copy(block);
          }}
          onUnsyncAll={block.type === "synced_block" && !adapter.readOnly && !syncedBlockSource(block, adapter.pageId).copy
            ? () => {
              setActionOpen(false);
              void syncedBlockActions(adapter).unsyncAll(block);
            }
            : undefined}
          toggleCaption={toggleCaption}
          turnInto={turnInto}
          onPageIn={adapter.moveContext && !adapter.readOnly ? setPageInPlace : undefined}
          onTurnDatabase={turnDatabase}
          code={block.type === "code" && !adapter.readOnly ? codeMenu(adapter, block, () => setActionOpen(false), notify) : undefined}
          onEditIcon={block.type === "callout" && !adapter.readOnly ? () => { setActionOpen(false); requestCalloutIconEdit(block.id); } : undefined}
          bookmark={bookmarkUrl && !adapter.readOnly ? {
            onTurnMention: () => { setActionOpen(false); turnBookmarkIntoMention(adapter, block, bookmarkUrl); },
            onCopySource: () => {
              setActionOpen(false);
              // Said as Notion's "링크 복사 완료" (seen 2026-11-11), as a link mention's copy says it.
              void copyText(bookmarkUrl).then((copied) => notify(t(copied ? "selectionToolbar:linkEditor.copiedLink" : "notifications.couldNotCopyLink"),
                copied ? "success" : "error"));
            },
          } : undefined}
          openPage={linkedPageId ? {
            onNewTab: () => { setActionOpen(false); window.open(linkedPageHref(adapter, linkedPageId), "_blank", "noopener,noreferrer"); },
            // The block's choosing ends as the peek opens, as Notion's (seen 2026-11-07).
            onSidePeek: () => { setActionOpen(false); editorContext?.setSelection(new Set()); openPagePeek(linkedPageId); },
          } : undefined}
          onTurnTableDatabase={block.type === "table" && !adapter.readOnly && !adapter.fixedBody && adapter.graphSync ? () => {
            setActionOpen(false);
            let tooLarge = false;
            adapter.changeLayout((graph) => { const turned = databaseFromSimpleTable(graph, block, adapter.pageId, {
              title: t("database.tableTitleColumn"), column: (count) => t("database.tableColumn", { count }),
              widths: shownSimpleTableWidths(actionButton.current, Math.max(1, Number(block.content.tableWidth) || 1)) }); tooLarge = !turned; return turned; });
            if (tooLarge) notify(t("database.tooLargeToTurn"), "error");
          } : undefined}
          table={block.type === "table" && !adapter.readOnly ? {
            headerRow: block.content.hasColumnHeader === true, headerColumn: block.content.hasRowHeader === true,
            onToggleHeaderRow: () => adapter.updateBlock(block, { hasColumnHeader: block.content.hasColumnHeader !== true }),
            onToggleHeaderColumn: () => adapter.updateBlock(block, { hasRowHeader: block.content.hasRowHeader !== true }),
            onFitWidth: () => { setActionOpen(false); fitSimpleTable(adapter, block, actionButton.current); },
          } : undefined}
        />
      </ActionLayer>) : null}
      {actionOpen && pageInPlace && adapter.moveContext ? (
        <PageInPicker row={pageInPlace.row} menu={pageInPlace.menu} pages={adapter.moveContext.pages}
          returnFocusRef={pageInPlace.returnFocusRef}
          session={adapter.moveContext.session} workspaceId={adapter.workspaceId} sourcePageId={adapter.pageId}
          unavailable={pageInBlocked} onChoose={(pageId) => void putInPage(pageId)}
          onClose={(leaveList) => {
            setPageInPlace(null);
            // Enter on "〈이름〉 개인 페이지" closes the turn list too, as Notion's does (seen 2026-10-02).
            if (leaveList) pageInPlace.onLeaveList();
          }} />
      ) : null}
      {moveOpen && adapter.moveContext && pageMoving ? (
        <PageBlockMoveDialog page={pageMoving} blockId={block.id} anchor={moveAnchor} moveContext={adapter.moveContext} onClose={() => setMoveOpen(false)} />
      ) : moveOpen && adapter.moveContext ? (
        <BlockMoveToDialog anchor={moveAnchor} blockId={block.id} sourcePageId={adapter.pageId} workspaceId={adapter.workspaceId}
          pages={adapter.moveContext.pages} session={adapter.moveContext.session}
          // A page opens to the databases its body holds too, as Notion's (seen 2026-11-11).
          databases
          // Blocks that are or hold a database go into none, nor into a page moving with them (blocksMoving).
          {...blocksMoving(adapter.graph, movingBlockIds(), adapter.moveContext.pages)}
          // A new page is chosen as it is made here: the move waits for the server to have it.
          madeHere
          onChooseDestination={(destinationPageId, signal, destination) => {
            // Into a database, a full-page one or one a page's body holds, they go as its rows.
            const ids = movingBlockIds();
            return destination.kind === "database"
              ? moveBlocksIntoDatabase(adapter, ids, selection.has(block.id), destination, { store: pageGraph, setData: productPages?.setData,
                notice: (message, pageId, undo) => {
                  toast?.notify(message, "default", [
                    { label: i18n.t("moveToDialog:toast.go"), onClick: () => pushRoute(pageHref(pageId)) },
                    { label: i18n.t("moveToDialog:toast.undo"), onClick: undo },
                  ]);
                  return () => toast?.dismissMessage(message);
                } })
              : moveBlocksToPage(adapter, ids, selection.has(block.id), destinationPageId, signal, moveCaret.current, pageGraph);
          }}
          onClose={() => setMoveOpen(false)}
          onMoved={() => void adapter.graphSync?.reread()}
          onPageChange={adapter.moveContext.onPageChange}
          onStatus={adapter.moveContext.onStatus}
        />
      ) : null}
      {pageInOpen && adapter.moveContext ? (
        <BlockMoveToDialog
          blockId={block.id}
          sourcePageId={adapter.pageId}
          workspaceId={adapter.workspaceId}
          pages={adapter.moveContext.pages}
          session={adapter.moveContext.session}
          title={t("turnPageIn.title")}
          sourcePage="never"
          placeholder={blockItemText("mention.addPagePlaceholder")}
          doneMessage={null}
          failedMessage={t("turnPageIn.failed")}
          unavailable={pageInBlocked}
          onChooseDestination={(destinationPageId) => turnBlocksIntoPageIn(adapter, selectedIds, selection.has(block.id), destinationPageId, pageGraph)}
          onClose={() => setPageInOpen(false)}
          onMoved={() => void adapter.graphSync?.reread()}
          onPageChange={adapter.moveContext.onPageChange}
          onStatus={adapter.moveContext.onStatus}
        />
      ) : null}
    </div>
  );
}
