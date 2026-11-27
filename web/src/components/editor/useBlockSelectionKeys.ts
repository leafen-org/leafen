import { useEffect, useRef, type Dispatch, type RefObject, type SetStateAction } from "react";
import { graphTextBlockTypes, type GraphBlock } from "@/contentGraph";
import { TOGGLE_BLOCK_TYPES } from "@/lib/blockSemantics";
import { chordLetter, detectKeyboardShortcutPlatform, matchesKeyboardShortcut, primaryModifier } from "@/lib/keyboardShortcuts";
import { copyEditorBlocks, pasteBlocksAfter, pastesServerBlocks, wholeLineCopied, writeEditorBlockClipboard, readSelectionPaste } from "./blockClipboard";
import {
  allBlockSelection, arrowBlockSelection, blockStateChange, edgeBlockSelection, indentSelectedBlocks,
  lastTopLevelBlock, outdentSelectedBlocks, selectedRootsInOrder, togglesCollapsedChange,
  type BlockSelectionWalk,
} from "./blockSelectionKeyboard";
import { requestEditorBlockCommand, type EditorBlockCommand } from "./editorCommands";
import { duplicateChosenBlocks } from "./editorBlockDuplicate";
import { editorDescendantIds } from "./editorGraphModel";
import { printableTextKey, shortcutBlockType, shortcutTextMark } from "./editorInputModel";
import { dropTextRangeIn, editorOwnsKeyboard, notePointedEditor } from "./editorScope";
import type { EditorSelection, NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { focusEditable } from "./focus";
import { dispatchPastedBlocksMenu } from "./textBlockClipboardModel";
import { internalPasteSource } from "./internalBlockPaste";
import { EDITOR_PASTE_BOUNDS as SELECTION_PASTE_SOURCE_BOUNDS } from "./editorClipboardPolicy";
import { nativeTiptapSelectionContext } from "./nativeTiptapSelectionContext";
import type { createNativeTiptapController } from "./nativeTiptapController";
import { confirmBlockRemoval, removeBlocksAnnounced, removedDatabasePages } from "./databaseBlockRemoval";
import { useOptionalProductToast } from "@/lib/productToast";
import { insertEditorFiles } from "./mediaBlockUpload";
import { childPageBlockPageId } from "./useBlockPageMenu";
import { moveBlocksByKeys } from "./syncedCopyMove";
import { lineChosenByKey } from "./nativeTiptapProductKeys";
import { pageViewEdgeHandoff, pageViewHandoff, startPageChoice, useViewTabBlockChoice, viewOutSelection } from "./pageViewChoice";
import { dropStaleJoin, joinedViewKey, joinFromView, joinViewFromBelow, keepJoin, type PageViewJoin } from "./pageViewJoin";
import { enterChosenBlock, openLinkedPage } from "./chosenBlockEnter";
import { VIEW_CHOICE_OUT, VIEW_CHOICE_PAGE_ALL } from "../database/viewChoiceSteps";
import { showChosen } from "./chosenBlockShown";
import { chosenPlace, placeChoice } from "./blockOccurrences";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

const textBlockTypes = new Set<string>(graphTextBlockTypes);
/** Blocks whose menu renames them with ⌘⇧R: a sub-page and an inline database. */
const RENAMED_BLOCK_TYPES: ReadonlySet<string> = new Set(["child_page", "child_database", "inline_database"]);
/** Blocks whose menu shows them full screen with Space. */
const FULL_SCREEN_BLOCK_TYPES: ReadonlySet<string> = new Set(["image", "pdf", "embed"]);

function textFocused() {
  const active = document.activeElement as HTMLElement | null;
  return Boolean(active && (active.isContentEditable || active.closest("input, textarea, select, [role=dialog], [role=alertdialog], [role=menu]")));
}

/**
 * Selected blocks answer the keyboard as Notion's do: they are deleted,
 * duplicated, copied, cut, turned, marked, moved and indented; Enter goes
 * into a block, Space checks a to-do or opens a toggle, a typed character
 * changes nothing, and a paste puts what it brings in their place. The page's
 * toggles open or close together.
 */
export function useBlockSelectionKeys({ adapter, rootRef, nested, rootBlockId, selectionRef, setSelection, engine }: {
  adapter: NativeEditorGraphAdapter;
  rootRef: RefObject<HTMLDivElement | null>;
  nested: boolean;
  rootBlockId: string | null;
  selectionRef: RefObject<EditorSelection>;
  setSelection: Dispatch<SetStateAction<EditorSelection>>;
  engine?: ReturnType<typeof createNativeTiptapController>;
}) {
  const toast = useOptionalProductToast();
  const toastRef = useRef(toast);
  toastRef.current = toast;
  // The page as it is now, for what a key finishes later — a cut's removal once its copy is written, a delete's once
  // its question is answered — on whatever drew it again meanwhile, so the removal is a step of its history.
  const latest = useRef(adapter);
  latest.current = adapter;
  // The ends of the selection the arrow keys last made, while it is still the selection.
  const walk = useRef<{ ends: BlockSelectionWalk | null; selection: EditorSelection } | null>(null);
  // The block Escape let go, where it was chosen, chosen again by the next ↑ or ↓ until something is pressed.
  const escaped = useRef<string | null>(null);
  // Chosen blocks joined to an inline database's chosen items (pageViewJoin.ts), let go with them.
  const join = useRef<PageViewJoin | null>(null);
  useEffect(() => dropStaleJoin(join, selectionRef.current));
  useViewTabBlockChoice({ adapter, root: rootRef, nested, rootBlockId, selectionRef, setSelection, walk });
  // Tab and ⇧Tab over chosen blocks are theirs, whether they can go in or out
  // or not, and whether or not the page can change them yet (its text engine
  // still coming, as on a slow server after a reload): the browser's focus
  // never moves on from them to the page's buttons, as Notion's (2026-11-14:
  // the next Enter pressed the page's icon button, or skipped to its top).
  // Heard last, after the page's own keys took theirs.
  useEffect(() => {
    if (adapter.readOnly) return;
    const keepTab = (event: KeyboardEvent) => {
      if (event.key !== "Tab" || event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey
        || !selectionRef.current?.size || !rootRef.current?.isConnected || textFocused()) return;
      event.preventDefault();
    };
    window.addEventListener("keydown", keepTab);
    return () => window.removeEventListener("keydown", keepTab);
  }, [adapter.readOnly, rootRef, selectionRef]);
  useEffect(() => {
    if (!engine) return;
    const core = engine;
    const duplicate = (ids: readonly string[], options?: { withoutContent?: boolean; fromCaret?: boolean }) => duplicateChosenBlocks(adapter, ids, setSelection, core, options);

    // A full-page database's database is its page, which is never selected.
    function selectable(selection: Set<string>) {
      if (!adapter.fixedBody) return selection;
      return new Set([...selection].filter((id) => adapter.graph.blocks.find((block) => block.id === id)?.parentId));
    }

    function onKeyDown(event: KeyboardEvent) {
      if (adapter.readOnly || !adapter.isCurrent() || event.defaultPrevented || !editorOwnsKeyboard(rootRef.current, nested)) return;
      if (matchesKeyboardShortcut("toggleAllToggles", event)) {
        if (!editorOwnsKeyboard(rootRef.current, nested)) return;
        const shown = rootBlockId === null ? null : editorDescendantIds(adapter.graph, [rootBlockId]);
        const toggles = adapter.graph.blocks.filter((block) => TOGGLE_BLOCK_TYPES.has(block.type as never)
          && block.id !== rootBlockId && (!shown || shown.has(block.id)));
        if (!toggles.length) return;
        event.preventDefault();
        const collapse = toggles.some((block) => block.content.collapsed !== true);
        core.run(() => {
          for (const block of toggles) if ((block.content.collapsed === true) !== collapse) adapter.updateBlock(block, { collapsed: collapse });
        });
        return;
      }
      // While typing in a block, ⌘D duplicates it and selects the copy, and
      // ⌘⌃L copies a link to it, as Notion does (seen 2026-10-01). A Mac's
      // Control+D still deletes forward in the text.
      const typing = core.editor.isFocused ? nativeTiptapSelectionContext(core.editor.state)?.block.nativeId : null;
      if (typing && !imeComposing(event)) {
        const macControl = event.ctrlKey && !event.metaKey && detectKeyboardShortcutPlatform() === "mac";
        if (!macControl && matchesKeyboardShortcut("duplicateBlock", event)) {
          event.preventDefault();
          if (duplicate([typing], { fromCaret: true }).length > 0) (document.activeElement as HTMLElement | null)?.blur();
          return;
        }
        if (matchesKeyboardShortcut("copyBlockLink", event)) {
          event.preventDefault();
          requestEditorBlockCommand(typing, "copyLink");
          return;
        }

      }
      const current = selectionRef.current;
      if ((!current || current.size === 0) && !nested && !adapter.fixedBody) {
        const started = startPageChoice(event, adapter.graph, rootBlockId, escaped.current);
        if (started) {
          event.preventDefault();
          escaped.current = null;
          walk.current = null;
          // ⌘↑ and ⌘↓ go to the page's edge as from chosen blocks, below, and ⌘A chooses them all.
          if (started.edge === "last" && pageViewEdgeHandoff(adapter.graph, rootRef.current, started.blocks)) return;
          setSelection(started.blocks);
          if (started.edge) showChosen(rootRef.current, [...started.blocks][0]);
          return;
        }
      }
      if (!current || current.size === 0 || textFocused()) return;
      // A syllable started over chosen blocks has nowhere to go.
      if (imeComposing(event)) { dropTextRangeIn(rootRef.current); return; }
      if (joinedViewKey(event, join, current, { adapter, root: rootRef.current, rootId: rootBlockId, walk, setSelection, selectable, selected: () => selectionRef.current })) return;
      const roots = selectedRootsInOrder(adapter.graph, current, rootBlockId);
      const anchor = roots[0];
      if (!anchor) return;
      core.run(() => selectedKey(event, current, roots, anchor));
    }

    function selectedKey(event: KeyboardEvent, current: EditorSelection, roots: GraphBlock[], anchor: GraphBlock) {
      const ids = roots.map((block) => block.id);
      // A locked page's block is neither copied nor sent to the trash from its
      // parent, as its menu offers neither (Notion, seen 2026-10-31).
      const holdsLockedPage = roots.some((block) => block.type === "child_page"
        && adapter.moveContext?.pages.find((page) => page.id === childPageBlockPageId(block))?.isLocked === true);
      const key = chordLetter(event);
      const command = primaryModifier(event);
      const plain = !(command || event.altKey);
      const handled = () => event.preventDefault();

      // Copying or cutting selected blocks puts them on the clipboard as blocks.
      if (command && !event.shiftKey && !event.altKey && (key === "c" || key === "x")) {
        handled();
        // A cut goes without the trash's question, as Notion's: what it takes, a database's data too, comes back
        // where it is pasted (copyPastedDatabases); a sub-page it takes says it went to the trash (removeBlocksAnnounced).
        void copyEditorBlocks(adapter, ids, key === "x").then(async (copied) => {
          const page = latest.current;
          if (!copied || key !== "x" || !page.isCurrent() || page.readOnly) return;
          removeBlocksAnnounced(page, ids, (step) => core.run(step), toastRef.current);
          setSelection(new Set());
        });
        return;
      }
      // A selected block's menu keys (its caption, a page's rename, a synced
      // block's "동기화 모두 해제") do their rows' work, as the menu shows them.
      const one = roots.length === 1;
      const blockCommand: EditorBlockCommand | null = matchesKeyboardShortcut("openBlockMenu", event) ? "menu"
        : matchesKeyboardShortcut("addLink", event) && one && anchor.type === "image" ? "imageLink"
        : matchesKeyboardShortcut("blockColor", event) ? "color"
          : matchesKeyboardShortcut("moveBlock", event) ? "move"
            : matchesKeyboardShortcut("addComment", event) ? "comment"
              : matchesKeyboardShortcut("copyBlockLink", event) && one ? "copyLink"
                : matchesKeyboardShortcut("toggleCaption", event) && one ? "caption"
                  : matchesKeyboardShortcut("renamePage", event) && one && RENAMED_BLOCK_TYPES.has(anchor.type) ? "rename"
                    : matchesKeyboardShortcut("unsyncAllSynced", event) && one && anchor.type === "synced_block" ? "unsyncAll" : null;
      if (blockCommand) {
        handled();
        // Commenting on a block marks its text instead, as Notion's (seen 2026-11-04).
        if (blockCommand === "comment") setSelection(new Set());
        requestEditorBlockCommand(anchor.id, blockCommand);
        return;
      }
      const turnInto = shortcutBlockType(event);
      if (turnInto) {
        handled();
        // Chosen blocks turned pages, one or several, each stay in place
        // unopened and none stays chosen, as Notion's ⌘⌥9 (seen 2026-11-18).
        if (turnInto !== "child_page") setSelection(new Set(adapter.changeType(ids, turnInto)));
        else {
          setSelection(new Set());
          adapter.changeType(ids, "child_page");
        }
        return;
      }
      // ⌘B ⌘I ⌘U ⌘⇧S mark the chosen blocks' own words, a code block's too,
      // not those of blocks inside them; ⌘E marks nothing, and a table, a
      // column list or a synced block's frame takes none, as Notion's (seen
      // 2026-11-12: a chosen toggle's line bold, its open child's not).
      const mark = shortcutTextMark(event);
      if (mark) {
        handled();
        if (mark === "code") return;
        const markable = adapter.graph.blocks.filter((block) => current.has(block.id) && textBlockTypes.has(block.type));
        if (adapter.collaborationDocument && markable.length > 0) {
          if (core) {
            const ids = new Set(markable.map(block => block.id));
            core.run(() => {
              const tr = core.editor.state.tr;
              const type = tr.doc.type.schema.marks[mark === "strikethrough" ? "strike" : mark];
              const ranges: Array<{ from: number; to: number }> = [];
              tr.doc.descendants((node, position) => {
                const words = node.firstChild;
                if (node.type.name === "nativeBlock" && ids.has(node.attrs.sourceId || node.attrs.id)
                  && (words?.type.name === "nativeText" || words?.type.name === "nativeCode")) {
                  ranges.push({ from: position + 2, to: position + 2 + words.content.size });
                }
              });
              const remove = ranges.every(range => tr.doc.rangeHasMark(range.from, range.to, type));
              for (const range of ranges) if (remove) tr.removeMark(range.from, range.to, type); else tr.addMark(range.from, range.to, type.create());
              core.editor.view.dispatch(tr);
            });
          }
        }
        return;
      }
      if (matchesKeyboardShortcut("duplicateBlock", event)) {
        handled();
        if (!holdsLockedPage) duplicate(ids);
        return;
      }
      // ⌘⇧D duplicates whatever is chosen, its databases without their pages —
      // inline, linked or a full-page one's line, as their menus' "콘텐츠 제외
      // 복제" — and other blocks as ⌘D does (Notion's, seen 2026-11-25 in two
      // tabs: a paragraph alone and with an inline database copied too).
      if (matchesKeyboardShortcut("duplicateBlockWithoutContent", event)) {
        handled();
        if (!holdsLockedPage) duplicate(ids, { withoutContent: true });
        return;
      }
      if (matchesKeyboardShortcut("deleteBlock", event)) {
        handled();
        if (holdsLockedPage) return;
        // A database among them asks first; the rest go at once, leaving
        // nothing chosen and no caret, as Notion's (seen 2026-11-04). A
        // page's line that went says so (removeBlocksAnnounced).
        const databases = removedDatabasePages(adapter.graph, ids, adapter.moveContext?.pages ?? []);
        void confirmBlockRemoval(adapter.graph, ids, { session: adapter.session, databases }).then((confirmed) => {
          const page = latest.current;
          if (!confirmed || !page.isCurrent() || page.readOnly) return;
          removeBlocksAnnounced(page, ids, (step) => core.run(step), toastRef.current);
          setSelection(new Set());
        });
        return;
      }
      if (matchesKeyboardShortcut("moveBlockUp", event) || matchesKeyboardShortcut("moveBlockDown", event)) {
        handled();
        moveBlocksByKeys(adapter, current, event.key === "ArrowUp" ? "up" : "down", rootBlockId);
        return;
      }
      const show = (at: string | undefined, foot = false) => showChosen(rootRef.current, at, foot);
      if (command && !event.altKey) {
        if (key === "a" && !event.shiftKey) {
          handled();
          setSelection(selectable(allBlockSelection(adapter.graph, rootBlockId)));
        } else if (key === "enter" && !event.shiftKey) {
          handled();
          const change = openLinkedPage(anchor) ? null : blockStateChange(adapter.graph, current);
          if (change) adapter.changeLayout(() => change);
        } else if ((event.key === "ArrowUp" || event.key === "ArrowDown") && !event.shiftKey) {
          // ⌘↑ and ⌘↓ choose the page's first or last block, ⌘↓ an inline
          // database's last row where it is last, as Notion's (seen
          // 2026-11-13 in Chrome on a Mac).
          handled();
          walk.current = null;
          const edge = event.key === "ArrowUp" ? "first" : "last";
          const next = selectable(edgeBlockSelection(adapter.graph, current, edge, false, rootBlockId));
          if (edge === "last" && pageViewEdgeHandoff(adapter.graph, rootRef.current, next)) { setSelection(new Set()); return; }
          setSelection(next);
          show([...next][0]);
        }
        return;
      }
      if (!plain) return;
      // Home and End, Shift with them or not, leave the choice and the page
      // where they are, as Notion's (seen 2026-11-13 in Chrome on a Mac; its
      // Shift+End took the browser's own steps, from a hidden caret).
      if (event.key === "Home" || event.key === "End") {
        handled();
      } else if (event.key === "ArrowUp" || event.key === "ArrowDown") {
        handled();
        const moved = arrowBlockSelection(adapter.graph, current, event.key === "ArrowUp" ? "up" : "down", event.shiftKey, rootBlockId,
          walk.current?.selection === current ? walk.current.ends : null, { pageId: adapter.pageId, at: (id) => chosenPlace(current, id) });
        // Into an inline database's rows or cards, as Notion's page walks them.
        if (!event.shiftKey && pageViewHandoff(adapter.graph, rootRef.current, current, moved.selection, event.key === "ArrowDown")) {
          walk.current = null;
          setSelection(new Set());
          return;
        }
        // Shift+↑ onto one from the block under it joins its last row or card to the blocks.
        if (event.shiftKey && !join.current && (join.current = joinViewFromBelow(adapter.graph, rootRef.current, current, moved, event.key === "ArrowUp"))) return;
        const next = placeChoice(selectable(moved.selection), moved.places);
        walk.current = { ends: moved.walk, selection: next };
        keepJoin(join, next);
        setSelection(next);
        show(moved.walk?.focus, event.shiftKey && event.key === "ArrowDown");
      } else if (event.key === "Tab") {
        handled();
        if (event.shiftKey) outdentSelectedBlocks(adapter, current, rootBlockId);
        else indentSelectedBlocks(adapter, current);
      } else if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) {
        handled();
        requestEditorBlockCommand(anchor.id, "menu");
      } else if (event.key === "Escape" && !event.shiftKey) {
        // Escape lets go of the selected blocks first (Notion's shortcuts:
        // Esc selects the current block or clears the selection); a dialog
        // around them stays for the next Escape. A page or a row's page open
        // over the page, side or center — a new page's window and a
        // template's too — closes on it instead, its selection with it
        // (Notion's, seen 2026-11-07 and 2026-11-13).
        if (!rootRef.current?.closest('[data-row-page-mode="side"], [data-row-page-mode="center"], [data-page-peek]')) handled();
        escaped.current = walk.current?.selection === current && walk.current.ends ? walk.current.ends.focus : chosenPlace(current, anchor.id);
        setSelection(new Set());
      } else if ((event.key === "ArrowLeft" || event.key === "ArrowRight") && !event.shiftKey) {
        const change = togglesCollapsedChange(adapter.graph, current, event.key === "ArrowLeft");
        if (!change) return;
        handled();
        adapter.changeLayout(() => change);
      } else if (fieldEnter(event) && event.shiftKey) {
        // Shift+Enter does nothing to a chosen block, as Notion's (seen 2026-11-05).
        event.preventDefault();
      } else if (fieldEnter(event)) {
        handled();
        enterChosenBlock(anchor, adapter, rootRef.current, setSelection);
      } else if (event.key === " ") {
        const change = blockStateChange(adapter.graph, current);
        if (change) {
          handled();
          adapter.changeLayout(() => change);
        } else if (current.size === 1 && FULL_SCREEN_BLOCK_TYPES.has(anchor.type) && matchesKeyboardShortcut("fullScreen", event)) {
          // Space shows a selected image, PDF or embed full screen, as its menu says.
          handled();
          requestEditorBlockCommand(anchor.id, "imageFullScreen");
        } else if (current.size === 1 && textBlockTypes.has(anchor.type)) {
          handled();
          setSelection(new Set());
          focusEditable(anchor.id, "end");
        }
      } else if (printableTextKey(event)) {
        // Typing over chosen blocks changes nothing, whatever they are, as
        // Notion's (seen 2026-11-05: a line, a picture, a file, two blocks).
        handled();
      }
    }

    function onClipboard(event: ClipboardEvent) {
      if (event.defaultPrevented || !event.clipboardData || adapter.readOnly || !adapter.isCurrent()
        || !editorOwnsKeyboard(rootRef.current, nested) || textFocused()) return;
      const current = selectionRef.current;
      if (!current?.size) return;
      const ids = selectedRootsInOrder(adapter.graph, current, rootBlockId).map(block => block.id);
      if (!writeEditorBlockClipboard(event.clipboardData, adapter, ids, event.type === "cut")) return;
      event.preventDefault();
      // As ⌘X: no trash question, a database pasted comes back itself, and a sub-page cut says it went to the trash.
      if (event.type === "cut") {
        removeBlocksAnnounced(adapter, ids, (step) => core.run(step), toastRef.current);
        setSelection(new Set());
      }
    }

    // A paste with blocks chosen goes after them, and with neither blocks
    // chosen nor a caret at the end of the page; what it brings is chosen, as
    // Notion's (real ⌘V, seen 2026-11-04). A lone link becomes a line linking
    // to it, with the pasted link's choices.
    function onPaste(event: ClipboardEvent) {
      if (adapter.readOnly || !adapter.isCurrent() || event.defaultPrevented || !event.clipboardData || !editorOwnsKeyboard(rootRef.current, nested)) return;
      const current = selectionRef.current;
      if (textFocused() || !current?.size && (nested || adapter.fixedBody)) return;
      const data = event.clipboardData;
      const roots = current?.size ? selectedRootsInOrder(adapter.graph, current, rootBlockId) : [];
      const after = roots.at(-1)?.id ?? lastTopLevelBlock(adapter.graph, rootBlockId)?.id ?? null;
      const files = Array.from(data.files ?? []);
      if (files.length && (!data.types.includes("text/html") || !data.getData("text/plain").trim())) {
        event.preventDefault();
        let made: readonly string[] = [];
        core.run(() => { made = insertEditorFiles(adapter, after, files, "pasted", core.follow); });
        if (made.length) core.choosePasted(made);
        return;
      }
      const pasted = readSelectionPaste(data);
      if (!pasted) return;
      event.preventDefault();
      if ("blocks" in pasted && pastesServerBlocks(pasted.blocks)) {
        core.pasteDatabases(pasted.blocks, after, false, internalPasteSource(data, SELECTION_PASTE_SOURCE_BOUNDS));
        return;
      }
      const url = "url" in pasted ? pasted.url : "";
      let made: readonly string[] = [];
      core.run(() => { made = pasteBlocksAfter(adapter, after, url
        ? [{ type: "paragraph", content: { rich: [{ text: url, link: url }] }, plainText: url }]
        : ("blocks" in pasted ? pasted.blocks : [])); });
      if (!made.length) return;
      // What is pasted over chosen blocks is chosen under them, an address's
      // link line too, with no menu: Notion offers its paste forms only where
      // a caret takes the address (seen 2026-11-08). Copied blocks show (pastedBlocksShown).
      core.choosePasted(made, "blocks" in pasted && pasted.copied);
      if (url) return;
      // Blocks from another page offer to sync with or link to where they came from (PastedBlocksMenu).
      const source = internalPasteSource(data, SELECTION_PASTE_SOURCE_BOUNDS);
      if (source) dispatchPastedBlocksMenu({ blockIds: [...made], source });
    }

    // Text put in another way than a key (an IME's, a dictation's) changes
    // nothing either: it went into a chosen picture's frame, and on the next
    // read took the picture's place (2026-11-05).
    function onBeforeInput(event: InputEvent) {
      if (adapter.readOnly || !adapter.isCurrent() || !event.inputType.startsWith("insert") || !selectionRef.current?.size
        || textFocused() || !editorOwnsKeyboard(rootRef.current, nested)) return;
      event.preventDefault();
      dropTextRangeIn(rootRef.current);
    }

    // An inline database's view hands ↑ or ↓ on past its first or last item.
    function onViewOut(event: Event) {
      const moved = viewOutSelection(event, adapter.graph, rootBlockId);
      if (!moved || adapter.readOnly || !adapter.isCurrent()) return;
      event.preventDefault();
      if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
      notePointedEditor(rootRef.current);
      walk.current = null;
      join.current = joinFromView(event, moved);
      setSelection(moved);
    }
    // An inline database's view hands ⌘A on: all the page's blocks, as its own ⌘A chooses them (chooseAllInView).
    function onViewAll(event: Event) {
      if (event.defaultPrevented || adapter.readOnly || !adapter.isCurrent()) return;
      event.preventDefault();
      if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
      notePointedEditor(rootRef.current);
      walk.current = null;
      setSelection(selectable(allBlockSelection(adapter.graph, rootBlockId)));
    }
    const forgetEscaped = () => { escaped.current = null; };
    const root = rootRef.current;
    root?.addEventListener(VIEW_CHOICE_OUT, onViewOut);
    root?.addEventListener(VIEW_CHOICE_PAGE_ALL, onViewAll);
    document.addEventListener("pointerdown", forgetEscaped, true);
    document.addEventListener("keydown", onKeyDown);
    document.addEventListener("beforeinput", onBeforeInput, true);
    document.addEventListener("paste", onPaste);
    // All of a line's words ⌘A chose, copied, carry the line (wholeLineCopied),
    // taken before the text engine writes the words alone; they are copied
    // words, suggested ones too (copiedWords).
    function onWholeLineCopy(event: ClipboardEvent) {
      if (event.defaultPrevented || !event.clipboardData || adapter.readOnly || !adapter.isCurrent() || !textFocused()
        || !lineChosenByKey(core.editor.state)) return;
      const line = wholeLineCopied(rootRef.current, adapter);
      if (!line || !writeEditorBlockClipboard(event.clipboardData, adapter, [line], false, true)) return;
      event.preventDefault();
      event.stopPropagation();
    }
    document.addEventListener("copy", onWholeLineCopy, true);
    document.addEventListener("copy", onClipboard); document.addEventListener("cut", onClipboard);
    return () => {
      root?.removeEventListener(VIEW_CHOICE_OUT, onViewOut);
      root?.removeEventListener(VIEW_CHOICE_PAGE_ALL, onViewAll);
      document.removeEventListener("pointerdown", forgetEscaped, true);
      document.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("beforeinput", onBeforeInput, true);
      document.removeEventListener("paste", onPaste);
      document.removeEventListener("copy", onWholeLineCopy, true);
      document.removeEventListener("copy", onClipboard); document.removeEventListener("cut", onClipboard);
    };
  }, [adapter, nested, rootBlockId, rootRef, selectionRef, setSelection, engine]);
}
