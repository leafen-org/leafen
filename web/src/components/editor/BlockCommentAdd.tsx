import { useCallback, type PointerEvent, type RefObject } from "react";
import { CommentAddIcon } from "@/icons/leafen";
import { readsRightToLeft } from "@/lib/readingDirection";
import { blockItemText } from "./blockItemLabels";
import { useHandlesDrawn } from "./drawnHandles";
import styles from "./editorStyles.generated.module.css";

/**
 * The blocks whose line offers "💬+" at its end, as Notion's (seen
 * 2026-11-12 at 1440 and 1024): a line of text of any kind, a heading, a
 * list item, a to-do, a toggle, a quote, a callout and code, held in another
 * block too. A simple table, a divider and an equation offer none, nor does
 * a block in a column.
 */
const OFFERS_COMMENT_ADD = new Set([
  "paragraph", "heading_1", "heading_2", "heading_3", "heading_4",
  "toggle_heading_1", "toggle_heading_2", "toggle_heading_3", "toggle_heading_4",
  "bulleted_list_item", "numbered_list_item", "to_do", "toggle", "quote", "callout", "code",
]);

export function offersCommentAdd(type: string) {
  return OFFERS_COMMENT_ADD.has(type);
}

/**
 * How much of a block's width, from its end, is its end: the pointer there
 * puts the block's handles away and brings its "💬+", as Notion's (seen
 * 2026-11-12: the last 30% of the block's box, at 1440 and 1024, with the
 * discussions beside the page and without).
 */
const BLOCK_END_SHARE = 0.3;
/**
 * "💬+" stands 4px past the end of the page's column, 6px outside its
 * editor, however much narrower its blocks keep (Notion's, seen 2026-11-12:
 * 1046.5 by a column to 1042.5 and blocks to 955.5 with discussions beside
 * them; 1348 by a full-width column to 1344 and blocks to 1329).
 */
const COMMENT_ADD_GAP = 4;
const COLUMN_PAST_EDITOR = 6;

/**
 * Marks a block's row with the pointer at its end (`data-pointer-end`),
 * which its CSS reads (EditorBlockHandles): only the row the pointer is
 * on, not the rows of the blocks around it, and none in a column, whose
 * blocks keep their handles there (Notion's, seen 2026-11-12). A block held
 * in another puts its "💬+" where the top block's stands.
 */
export function trackBlockEnd(event: PointerEvent<HTMLElement>) {
  const row = event.currentTarget;
  const group = row.parentElement;
  const own = event.target instanceof Element && event.target.closest(`.${styles.blockRow}`) === row;
  let atEnd = false;
  if (event.pointerType === "mouse" && own && group && !group.closest("[data-column-id]")) {
    const box = group.getBoundingClientRect();
    const rtl = readsRightToLeft(group);
    atEnd = (rtl ? event.clientX - box.left : box.right - event.clientX) <= box.width * BLOCK_END_SHARE;
    if (atEnd && row.dataset.pointerEnd !== "true") placeCommentAdd(row);
  }
  if (atEnd) row.dataset.pointerEnd = "true";
  else delete row.dataset.pointerEnd;
}

/**
 * Stands a block row's "💬+" past the page's column, from the row's end:
 * now, or as it is drawn (BlockCommentAdd), the pointer at the row's end.
 */
export function placeCommentAdd(row: HTMLElement) {
  // On the "💬+" itself, its only reader: set on the row, every box in the
  // block took it again (2026-11-12: about 80 ms as the pointer came to a
  // database block's end, a table of 30 rows by 40 properties).
  const add = row.querySelector<HTMLElement>(`:scope > .${styles.blockCommentAdd}`);
  if (!add) return;
  const group = row.parentElement ?? row;
  const rtl = readsRightToLeft(group);
  const editor = group.closest<HTMLElement>("[data-editor-root]:not([data-editor-nested])");
  const column = (editor ?? group.closest<HTMLElement>('[data-depth="0"]') ?? group).getBoundingClientRect();
  const past = editor ? COLUMN_PAST_EDITOR : 0;
  const line = row.getBoundingClientRect();
  add.style.setProperty("--block-comment-add-gap", `${(rtl ? line.left - column.left : column.right - line.right) + past + COMMENT_ADD_GAP}px`);
}

export function leaveBlockEnd(event: PointerEvent<HTMLElement>) {
  delete event.currentTarget.dataset.pointerEnd;
}

// Kinds whose marks Notion stands by the block's box, not its words' line (seen 2026-11-22).
const BOXED_MARKS = new Set(["quote", "callout", "code", "equation", "table"]);

/**
 * How high a block's marks stand and how far in, as Notion's with inline
 * comments minimal (seen 2026-11-22 in Chrome at 1440, fourteen kinds): 1px
 * under its own words' first line — a paragraph's, a heading's, a list
 * item's, a to-do's, a toggle's — or, a quote, code, a callout, an equation, a
 * table or a block with no line of words of its own, 11px under the block's
 * top and 2px nearer the text.
 */
function marksLevel(row: HTMLElement) {
  const group = row.closest<HTMLElement>("[data-block-id]");
  const boxed = BOXED_MARKS.has(group?.dataset.blockType ?? "");
  const words = boxed ? undefined
    : [...row.querySelectorAll<HTMLElement>('[data-native-text="true"]')].find((field) => field.closest("[data-block-id]") === group);
  const field = words?.querySelector<HTMLElement>("[data-rt-editable]") ?? words;
  if (!field) return { top: (group ?? row).getBoundingClientRect().top + 11, inset: 2 };
  return { top: field.getBoundingClientRect().top + (Number.parseFloat(getComputedStyle(field).paddingTop) || 0) + 1, inset: 0 };
}

/**
 * Stands a block's marks ("💬 N", "☺ N") just past its top block's box,
 * whichever block within it they belong to, as Notion's (seen 2026-11-12 at
 * 1024 and 1191, a page of the usual width and a full-width one: a line's,
 * a reaction's and a nested line's marks all at its top block's end, 913 by
 * a nested line ending at 907). A block row ends at its text, 6px inside its
 * box and 6px more for each block it is held in, so the marks go that much
 * past their row (--block-marks-past). A block in a column keeps them at its
 * line's end.
 */
export function placeBlockMarks(marks: HTMLElement | null) {
  const row = marks?.parentElement;
  const top = row?.closest<HTMLElement>('[data-depth="0"]');
  if (!marks || !row || !top || row.closest("[data-column-id]")) return;
  const place = () => {
    const box = top.getBoundingClientRect();
    const line = row.getBoundingClientRect();
    const { top: markTop, inset } = marksLevel(row);
    marks.style.setProperty("--block-marks-past", `${(readsRightToLeft(top) ? line.left - box.left : box.right - line.right) - inset}px`);
    marks.style.setProperty("--block-marks-top", `${markTop - line.top}px`);
  };
  place();
  const observer = new ResizeObserver(place);
  observer.observe(top);
  observer.observe(row);
  return () => observer.disconnect();
}

/**
 * A block's "💬+": a new comment on the whole block, as its menu's "댓글".
 * Drawn while its row draws its handles (drawnHandles), out of the Tab
 * order as Notion's.
 */
export function BlockCommentAdd({ row, onComment }: { row: RefObject<HTMLElement | null>; onComment: () => void }) {
  const drawn = useHandlesDrawn(row);
  // Drawn with the pointer already at the row's end, it takes its place then.
  const place = useCallback((add: HTMLElement | null) => {
    if (add && row.current?.dataset.pointerEnd === "true") placeCommentAdd(row.current);
  }, [row]);
  if (!drawn) return null;
  return (
    <span ref={place} className={styles.blockCommentAdd} contentEditable={false}>
      <button type="button" tabIndex={-1} className={styles.blockCommentAddButton} aria-label={blockItemText("comments.add")}
        onClick={onComment}><CommentAddIcon size="small" aria-hidden="true" /></button>
    </span>
  );
}
