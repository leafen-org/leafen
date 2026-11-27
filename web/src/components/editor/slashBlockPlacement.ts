import { runAppMenuAction } from "@/components/apps/appMenuAction";
import type { GraphBlock } from "@/contentGraph";
import { TOGGLE_BLOCK_TYPES } from "@/lib/blockSemantics";
import { newCodeBlockLanguage } from "@/lib/blockDefaults";
import { i18next } from "@/i18n";
import { flushSync } from "react-dom";
import type { BlockDef } from "./blocks";
import { insertColumnsAfter, wrapInColumns } from "./columnLayoutModel";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { focusEditable } from "./focus";
import { TEXT_BLOCKS } from "./textBlockTypes";
import { wrapInSyncedBlock } from "./blockTurnInto";
import { syncedBlockMayGoIn } from "./syncedBlockModel";
import { editorBlockChildren } from "./editorGraphModel";
import { requestBlockSetup } from "./blockSetup";
import { keyCameLast } from "@/lib/lastPress";
import { requestEditorBlockSelection } from "./editorCommands";

/**
 * Makes the block a slash item names, as Notion's slash menu does (seen
 * 2026-09-30): in an empty line's place, or in a new line right after a line
 * with text, which keeps its text; the "+" menu always adds after its block.
 * An inline database is made at once by the line and starts as the item's
 * view; the linked view of a data source opens the database picker for the
 * line; a page or a full-page database opens once it is made.
 */
export function placeSlashBlock(adapter: NativeEditorGraphAdapter, line: GraphBlock, def: BlockDef, inPlace: boolean) {
  if (def.appAction) {
    // An app's item runs its action where it was chosen and leaves the line.
    if (!adapter.session || adapter.readOnly) return;
    void runAppMenuAction({
      session: adapter.session, usable: def.appAction.usable, action: def.appAction.action, label: def.app?.title ?? "",
      input: { place: "slash", pageId: adapter.pageId, blockId: line.id }, t: i18next.getFixedT(null, "common"),
      language: i18next.language, notice: (message, tone) => adapter.moveContext?.onStatus?.(message, tone),
    });
    return;
  }
  if (def.type === "inline_database" && adapter.database.onChooseDatabase) {
    // A linked view is made at once too, as Notion's: a table waiting for
    // its source, with the sources offered by it (seen 2026-11-02).
    const link = def.id === "inline_database_linked";
    adapter.database.onChooseDatabase({
      lineId: line.id, replaceLine: inPlace, view: link ? "table" : def.databaseView,
      viewConfig: link ? { ...def.viewConfig, linkPending: true } : def.viewConfig, create: true, link,
      made: adapter.localGraphChanged,
    });
    // A dashboard, which opens no start panel, leaves the caret at the start
    // of the page's title, as Notion's. Measured in the worker Chrome in the
    // Business trial workspace, no script in the page before the keys:
    // "/대시보드" Enter on an empty line after a line of words, then one
    // letter, which went in before the title's words — 2026-11-15 07:1x twice
    // in one tab (the second dashboard on the page too) and 07:2x on another
    // page in another tab. The other views open their start panel first.
    if (def.databaseView === "dashboard") focusEditable(`title:${adapter.pageId}`, "start");
    else leaveNoCaret();
    return;
  }
  if (def.type === "synced_block") {
    // None inside a synced block, whose menus don't offer one (keptOutOfSyncedBlocks).
    if (!syncedBlockMayGoIn(adapter.graph, line.parentId)) return;
    if (inPlace) {
      if (adapter.changeLayout((graph) => wrapInSyncedBlock(graph, [line.id]))) focusEditable(line.id, "start");
    } else {
      const made = adapter.insertAfter(line.id, "synced_block", def.content ?? {});
      const child = made && editorBlockChildren(adapter.graph, made.id)[0];
      if (child) focusEditable(child.id, "start");
    }
    return;
  }
  const columns = def.columnCount ?? 2;
  if (def.type === "column_list" && inPlace) {
    // The line becomes the first column's content.
    if (adapter.changeLayout((graph) => wrapInColumns(graph, line.id, columns))) focusEditable(line.id, "end");
    return;
  }
  if (def.type === "column_list") {
    let focusId = "";
    adapter.changeLayout((graph) => {
      const change = insertColumnsAfter(graph, line.id, columns);
      focusId = change?.focusId ?? "";
      return change;
    });
    if (focusId) focusEditable(focusId, "start");
    return;
  }
  if (def.type === "child_page" || def.type === "child_database") {
    // A page is named after its line, and a full-page database starts as the
    // item's view.
    const target = inPlace ? line : adapter.insertAfter(line.id, "paragraph", {});
    if (!target) return;
    adapter.changeType([target.id], "child_page", {
      openPage: true,
      ...(def.type === "child_database" ? { database: { view: def.databaseView } } : {}),
    });
    return;
  }
  // A toggle (or toggle heading) the menu makes starts closed, as Notion's
  // (seen 2026-11-02 — one a line turns into, by "> " or its shortcut, is open).
  // A callout takes its line inside it: that line is where the caret stays (seen 2026-11-05).
  // A block with sample words ("/mermaid") has them all chosen, the next key
  // writing over them. Notion's, measured 2026-11-15 in Chrome by keys with
  // no script in the page: "/토글 제목 1" closed, "/콜아웃" and "/mermaid"
  // as said, one letter typed after each landing there.
  const content = def.type === "callout" ? undefined : TOGGLE_BLOCK_TYPES.has(def.type as never) ? { ...def.content, collapsed: true }
    : def.type === "code" && !def.content ? { language: newCodeBlockLanguage() } : def.content;
  if (inPlace) {
    flushSync(() => {
      adapter.changeType([line.id], def.type);
      const made = content ? adapter.graph.blocks.find(({ id }) => id === line.id) : undefined;
      if (made && content) adapter.updateBlock(made, content);
      if (made && def.sampleText) adapter.changeText(made, def.sampleText);
    });
    if (def.sampleText) focusEditable(line.id, "select");
    else leaveCaret(adapter, line.id, def, true);
    return;
  }
  if (def.type === "callout") {
    const held = adapter.insertAfter(line.id, "paragraph", {});
    if (!held) return;
    flushSync(() => { adapter.changeType([held.id], "callout"); });
    leaveCaret(adapter, held.id, def, false);
    return;
  }
  const made = adapter.insertAfter(line.id, def.type, content ?? {});
  if (!made) return;
  if (def.sampleText) {
    flushSync(() => { adapter.changeText(made, def.sampleText!); });
    focusEditable(made.id, "select");
    return;
  }
  leaveCaret(adapter, made.id, def, false);
}

// Where the caret goes once a block is made.
function leaveCaret(adapter: NativeEditorGraphAdapter, blockId: string, def: BlockDef, inPlace: boolean) {
  if (SETUP.has(def.type) && def.content?.service !== "app" && !adapter.readOnly) {
    // The page's caret goes before the block takes it, or keys would go on
    // writing where the "/" was, or at the page's start. Chosen with a key,
    // its window sends the caret on when put away (BlockEmptyPopover).
    leaveNoCaret();
    requestBlockSetup(blockId, keyCameLast() ? { slashKey: { pageId: adapter.pageId } } : {});
  } else if (NEXT_LINE.has(def.type)) caretToNextLine(adapter, blockId);
  else if (NO_CARET.has(def.type)) selectMadeBlock(adapter, blockId, def.type === "tab");
  else if (LINE_AFTER.has(def.type)) writeOnAfter(adapter, blockId);
  else if (!inPlace || (!adapter.readOnly && TEXT_BLOCKS.has(def.type))) focusEditable(blockId, "start");
}

// What a block without text leaves, as Notion's, measured in Chrome by keys
// with no script in the page and one letter typed after (2026-11-15 by
// worker w1126, and again in another tab at another time by w1249): "/표"
// (a simple table) is selected even with an empty line right after it, the
// letter going nowhere; "/목차" and "/이동 경로" put the caret at the start
// of the next line that takes words, passing another table of contents (an
// empty line right after takes it too), and at the page's end are selected.
// A tab block is selected, or an empty line already after it takes the caret
// (seen 2026-10-29). A divider is followed by an empty line that takes the
// caret; a button's settings, a media block's upload choice, a link block's
// field and a math block's editor take it.
const NO_CARET: ReadonlySet<string> = new Set(["tab", "table", "table_of_contents", "breadcrumb"]);
// Blocks whose caret goes on to the next line that takes words.
const NEXT_LINE: ReadonlySet<string> = new Set(["table_of_contents", "breadcrumb"]);
const LINE_AFTER: ReadonlySet<string> = new Set(["divider"]);
// Blocks that open what they first need (blockSetup.ts).
const SETUP: ReadonlySet<string> = new Set(["button", "image", "video", "audio", "file", "pdf", "bookmark", "embed", "equation"]);

function writeOnAfter(adapter: NativeEditorGraphAdapter, blockId: string) {
  const line = adapter.insertAfter(blockId, "paragraph", {});
  if (line) focusEditable(line.id, "start");
}

/** Leaves a block without text selected, or, where emptyLineTakes, the empty line after it takes the caret. */
function selectMadeBlock(adapter: NativeEditorGraphAdapter, blockId: string, emptyLineTakes: boolean) {
  const made = adapter.graph.blocks.find(({ id }) => id === blockId);
  const siblings = made ? editorBlockChildren(adapter.graph, made.parentId) : [];
  const next = siblings[siblings.findIndex(({ id }) => id === blockId) + 1];
  if (emptyLineTakes && next?.type === "paragraph" && !adapter.text(next).trim() && editorBlockChildren(adapter.graph, next.id).length === 0) {
    focusEditable(next.id, "start");
    return;
  }
  selectBlockWithoutCaret(blockId);
}

/**
 * The caret goes to the start of the first line after the block that takes
 * words, in reading order: past other blocks of NEXT_LINE's kind, and on past
 * the end of what holds it; with none, the block is selected.
 */
function caretToNextLine(adapter: NativeEditorGraphAdapter, blockId: string) {
  let block = adapter.graph.blocks.find(({ id }) => id === blockId);
  while (block) {
    const siblings = editorBlockChildren(adapter.graph, block.parentId);
    const after = siblings.slice(siblings.findIndex(({ id }) => id === block!.id) + 1);
    for (const next of after) {
      if (TEXT_BLOCKS.has(next.type)) {
        focusEditable(next.id, "start");
        return;
      }
      if (!NEXT_LINE.has(next.type)) {
        selectBlockWithoutCaret(blockId);
        return;
      }
    }
    const parentId = block.parentId;
    block = parentId === adapter.pageId ? undefined : adapter.graph.blocks.find(({ id }) => id === parentId);
  }
  selectBlockWithoutCaret(blockId);
}

/** Selects a block, its keys acting on it; with no editor to take it, the caret only leaves. */
export function selectBlockWithoutCaret(blockId: string) {
  if (!requestEditorBlockSelection(blockId)) leaveNoCaret();
}

function leaveNoCaret() {
  const active = document.activeElement;
  if (active instanceof HTMLElement) active.blur();
}
