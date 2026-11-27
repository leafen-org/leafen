import type { Editor } from "@tiptap/core";
import { nodeViewsOpened } from "./NativeEditorContent";

// The rows already drawn, the ones a page opened with among them.
const drawnRows = new WeakSet<HTMLElement>();

/**
 * A simple table's row out of view isn't laid out (content-visibility,
 * NativeTiptap.module.css), and the browser finds a row put in near the view
 * only in the frame after: a row made, pasted or come from someone else
 * would paint its cells empty for a frame. One new to a page already drawn
 * is marked to be painted whole until its first frame has painted, as a
 * database's rows are (lib/arrivingWhole.ts); the rows a page opens with
 * keep theirs, so a long table opening lays out only the rows near the view.
 */
export function drawNewTableRowWhole(editor: Editor, row: HTMLElement | null) {
  if (!row || drawnRows.has(row)) return;
  drawnRows.add(row);
  if (!nodeViewsOpened(editor)) return;
  row.dataset.arriving = "true";
  requestAnimationFrame(() => requestAnimationFrame(() => { delete row.dataset.arriving; }));
}
