import { useEffect, useRef, type RefObject } from "react";
import { occurrenceBlockId } from "./blockOccurrences";
import { editorRootOf } from "./editorScope";
import styles from "./editorStyles.generated.module.css";

/** Where the block whose text holds the caret or the words chosen in this editor, all in one block, is drawn (blockOccurrences). */
export function caretBlockIn(root: Element | null) {
  const range = root ? document.getSelection() : null;
  if (!root || !range?.anchorNode || !range.focusNode || !root.contains(range.anchorNode)) return null;
  const element = (node: Node) => node instanceof Element ? node : node.parentElement;
  const at = element(range.anchorNode)?.closest("[data-rt-editable]")?.closest<HTMLElement>(`.${styles.blockGroup}`);
  if (!at || element(range.focusNode)?.closest(`.${styles.blockGroup}`) !== at) return null;
  const drawn = at.parentElement?.closest("[data-native-occurrence]")?.getAttribute("data-native-occurrence");
  return drawn && occurrenceBlockId(drawn) === at.dataset.blockId ? drawn : at.dataset.blockId ?? null;
}

/**
 * Where Shift and a press choose an inline view's item or row: a list's row, a gallery's, board's or calendar's card,
 * a timeline's bar or table row (cardSelection.tsx), a table's cell (TableGridRow), a field in them aside.
 */
const VIEW_ITEM = "[data-card-choice], [data-card-choice-for], [data-table-cell]";

/** Where the caret was when the page took the press going on, until it is released. */
let taken: string | null = null;

/**
 * Shift and a press on an inline database's item or row while the page's
 * text around it holds the caret (or words in one block) is the page's: it
 * chooses the page's blocks from the caret's through the database's
 * (BlockFrame), and the view chooses nothing and opens nothing, as Notion's
 * (seen 2026-11-25 in Chrome, two tabs at different times: a list's row, a
 * gallery's card, a table's cell); not a view's tab or tool. A view's own
 * text being written in (a timeline table's name written in place, behind
 * the view's uneditable frame) holds no caret of the page's.
 * Read as the press begins, before the press moves the caret.
 */
function readPress(event: PointerEvent) {
  const target = event.target instanceof Element ? event.target : null;
  const active = document.activeElement;
  if (!target || event.button !== 0 || !event.shiftKey || event.metaKey || event.ctrlKey || event.altKey) return null;
  if (!(active instanceof HTMLElement) || !active.isContentEditable) return null;
  const database = target.closest("[data-inline-database-wrapper]"), frame = target.closest('[contenteditable="false"]');
  if (!database || !frame || !active.contains(database) || !active.contains(frame)) return null;
  if (!target.closest(VIEW_ITEM) || target.closest("input, textarea, select")) return null;
  return caretBlockIn(editorRootOf(target));
}

if (typeof window !== "undefined") {
  window.addEventListener("pointerdown", (event) => { taken = readPress(event); }, true);
  // The press ends with its release and the click that comes in the same turn, once every listener has had them.
  window.addEventListener("pointerup", () => { if (taken) window.setTimeout(() => { taken = null; }); }, true);
}

/**
 * The block whose text held the caret when the press going on began, if the
 * page took it (above), for its pointerdown, mousedown and click in an inline
 * database: the cell pressed may be drawn anew as the blocks are chosen.
 */
export function pageTookPress(event: Event): string | null {
  return taken && event.target instanceof Element && event.target.closest("[data-inline-database-wrapper]") ? taken : null;
}

/**
 * A block's frame takes the press the page took on its inline database (above)
 * before the view under it does: the database's view is drawn in a node view
 * of its own, whose React events never pass the frame, so this listens on the
 * frame's element itself, as the press goes down to the view. Its pointerdown,
 * mousedown and click reach nothing under it, and its mousedown calls `choose`
 * with where the caret was.
 */
export function usePageTakesPress(frame: RefObject<HTMLElement | null>, choose: (from: string) => void) {
  const chooseNow = useRef(choose);
  chooseNow.current = choose;
  useEffect(() => {
    const group = frame.current;
    if (!group) return;
    const take = (event: Event) => {
      const from = pageTookPress(event);
      // Only the innermost block's frame: the database's, not a toggle's around it.
      if (!from || !(event.target instanceof Element) || event.target.closest(`.${styles.blockGroup}`) !== group) return;
      event.stopPropagation();
      if (event.type === "pointerdown") return;
      event.preventDefault();
      if (event.type === "mousedown") chooseNow.current(from);
    };
    const types = ["pointerdown", "mousedown", "click"] as const;
    types.forEach((type) => group.addEventListener(type, take, true));
    return () => types.forEach((type) => group.removeEventListener(type, take, true));
  }, [frame]);
}
