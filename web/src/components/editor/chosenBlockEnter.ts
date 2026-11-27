import type { Dispatch, SetStateAction } from "react";
import type { GraphBlock } from "@/contentGraph";
import { requestEditorBlockCommand } from "./editorCommands";
import type { EditorSelection, NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { focusBlockControl, focusEditable, getEditable } from "./focus";
import { blockCaptionShown } from "./blockCaptionModel";
import { requestBlockSetup } from "./blockSetup";
import { enterKeepsView } from "./pageViewChoice";

// What Enter and a link do to a chosen block (useBlockSelectionKeys.ts).

const pageLinkTypes = new Set(["child_page", "child_database", "link_to_page"]);

/** A chosen page, database or link block opens what it links to, as its link does. */
export function openLinkedPage(block: GraphBlock) {
  if (!pageLinkTypes.has(block.type)) return false;
  const row = document.querySelector<HTMLElement>(`[data-block-id="${CSS.escape(block.id)}"]`)?.firstElementChild;
  const link = row?.querySelector<HTMLAnchorElement>("a[href]:not([target])");
  if (!link) return false;
  link.click();
  return true;
}

/**
 * Enter goes to the end of a text block's text or a table's first cell,
 * opens a file's or an embed's replace window, a bookmark's link window or
 * a math block's editor (the block staying chosen), lets go of an inline
 * database but its table, board, list or gallery (enterKeepsView), and starts
 * a new line after any other block, a picture's, a page's or a link's too, as
 * Notion's (seen 2026-11-03, 2026-11-06; a picture's measured 2026-11-15 in
 * Chrome only, chosen by a click, no script in the page: Enter put an empty
 * line under it and the next letter typed went there).
 */
export function enterChosenBlock(anchor: GraphBlock, adapter: NativeEditorGraphAdapter, root: HTMLElement | null,
  setSelection: Dispatch<SetStateAction<EditorSelection>>) {
  if (anchor.type === "equation") {
    requestBlockSetup(anchor.id);
    return;
  }
  if (anchor.type !== "inline_database" || !enterKeepsView(root, anchor.id)) setSelection(new Set());
  if (anchor.type === "inline_database") return;
  if (anchor.type === "file" || anchor.type === "embed" || anchor.type === "bookmark") {
    if (anchor.content.fileId || anchor.content.url) requestEditorBlockCommand(anchor.id, "mediaReplace");
    else requestBlockSetup(anchor.id);
    return;
  }
  const firstRow = anchor.type === "table" ? document.querySelector(`[data-block-id="${CSS.escape(anchor.id)}"] [data-native-table-cell]`)
    ?.closest("[data-block-id]")?.getAttribute("data-block-id") : null;
  if (firstRow && focusBlockControl(firstRow, "[data-native-table-cell]")) return;
  if (getEditable(anchor.id)) {
    focusEditable(anchor.id, "end");
    return;
  }
  // A picture's caption takes the caret at its end, with no new line, as
  // Notion's (measured 2026-11-15 in Chrome only, the captioned picture
  // chosen by a click, no script in the page: the next letter typed went
  // after the caption's last word and no line was added).
  if (blockCaptionShown(anchor) && focusBlockControl(anchor.id, "[data-native-caption]")) return;
  const created = adapter.insertAfter(anchor.id, "paragraph");
  if (created) focusEditable(created.id, "start");
}
