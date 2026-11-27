import { withoutChosenLook } from "./carriedChoice";

type Side = "before" | "after";
type Input = { clientX: number; clientY: number };
export type LineLanding = { target: string; side: Side };

/**
 * How much of the carried copy of a dragged row shows, as Notion's copy of a
 * dragged block (opacity .4, seen 2026-11-07).
 */
const CARRIED_OPACITY = "0.4";

/**
 * A line list's row dragged by the pointer's own events (dragReorderPointer):
 * a see-through copy of the row goes with the pointer, keeping where the row
 * was pressed under it, as the browser's own drag drew its picture of the
 * row — and one of each row it carries (`others`), as they are drawn, apart
 * as they stand, as Notion's views carry their chosen rows (seen
 * 2026-11-24); the rows stay put and the list shows a line where the row
 * would land (`hit`, `show`). Letting go lands it where the line is shown,
 * or nowhere.
 */
export function startLineReorder({ row, others = [], start, hit, show }: {
  row: HTMLElement;
  others?: readonly HTMLElement[];
  start: Input;
  /** Where the dragged row lands with the pointer here, given where it was last shown landing. */
  hit(input: Input, shown: LineLanding | null): LineLanding | null;
  show(landing: LineLanding | null): void;
}) {
  const copies = [row, ...others].flatMap((drawn) => { const copy = carriedCopy(drawn); return copy ? [copy] : []; });
  let shown: LineLanding | null = null;
  return {
    place(input: Input) {
      for (const copy of copies) copy.style.transform = `translate(${input.clientX - start.clientX}px, ${input.clientY - start.clientY}px)`;
      const next = hit(input, shown);
      if (next?.target === shown?.target && next?.side === shown?.side) return;
      shown = next;
      show(next);
    },
    landing: () => shown,
    end() {
      for (const copy of copies) copy.remove();
      shown = null;
      show(null);
    },
  };
}

/**
 * A copy of the row drawn over it, beside it among its list's rows so its
 * list's styles still reach it, fixed to the window and taking no pointer;
 * a chosen row's copy wears no chosen look (withoutChosenLook).
 */
export function carriedCopy(row: HTMLElement) {
  const box = row.getBoundingClientRect();
  if (!row.isConnected || box.width === 0 || box.height === 0) return null;
  const copy = row.cloneNode(true) as HTMLElement;
  for (const element of [copy, ...copy.querySelectorAll<HTMLElement>("[id]")]) element.removeAttribute("id");
  withoutChosenLook(copy);
  copy.removeAttribute("data-dragging");
  copy.removeAttribute("data-drop-side");
  copy.setAttribute("aria-hidden", "true");
  copy.inert = true;
  copy.dataset.dragCopy = "";
  Object.assign(copy.style, {
    position: "fixed", left: `${box.left}px`, top: `${box.top}px`, right: "auto", bottom: "auto",
    width: `${box.width}px`, height: `${box.height}px`, margin: "0", boxSizing: "border-box",
    transform: "none", transition: "none", opacity: CARRIED_OPACITY, pointerEvents: "none", zIndex: "2147483647",
  });
  row.after(copy);
  // A box that holds fixed boxes itself (a transform, a container) moves the copy by its own place.
  const placed = copy.getBoundingClientRect();
  if (placed.left !== box.left) copy.style.left = `${2 * box.left - placed.left}px`;
  if (placed.top !== box.top) copy.style.top = `${2 * box.top - placed.top}px`;
  return copy;
}
