/** Where an element stands in the box that scrolls it (`placeInScrollBox`). */
export type ScrollBoxPlace = { top: number; bottom: number; height: number; boxHeight: number };

/**
 * Where `element` stands in `box`, the box that scrolls it, in the box's own
 * pixels — the ones its clientHeight and scrollTop count — from the top of
 * the box's border as it shows now, with the box's own height (`boxHeight`,
 * its border's foot). A box drawn smaller or larger than it is laid out —
 * a popover growing from 96% as it opens (popoverAppear.ts) — is measured as
 * it stands at its size: what the window draws (getBoundingClientRect) is
 * taken back by how much smaller it is drawn, so a list placed while it grows
 * lands where it would grown (2026-11-25: a code block's languages, placed by
 * rows drawn at 96% against the list's own 402px, opened 69px short of its
 * chosen 'Plain Text', cut at the foot).
 */
export function placeInScrollBox(element: Element, box: HTMLElement): ScrollBoxPlace {
  const drawn = element.getBoundingClientRect(), frame = box.getBoundingClientRect();
  const boxHeight = laidOutHeight(box);
  const scale = boxHeight > 0 && frame.height > 0 ? frame.height / boxHeight : 1;
  const top = (drawn.top - frame.top) / scale, height = drawn.height / scale;
  return { top, bottom: top + height, height, boxHeight: boxHeight > 0 ? boxHeight : frame.height };
}

// The height of its border's box as it is laid out, before any transform draws it otherwise (not rounded as offsetHeight is).
function laidOutHeight(box: HTMLElement) {
  const style = getComputedStyle(box), height = Number.parseFloat(style.height);
  if (!Number.isFinite(height)) return box.offsetHeight;
  if (style.boxSizing === "border-box") return height;
  return [style.paddingTop, style.paddingBottom, style.borderTopWidth, style.borderBottomWidth]
    .reduce((sum, value) => sum + (Number.parseFloat(value) || 0), height);
}
