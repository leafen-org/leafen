/**
 * A popover's size as its content draws it, for usePopoverPlacement.
 *
 * A popover placed by usePopoverPlacement keeps the max-height the size step
 * writes on it, which stands over its class's own: it is measured without the
 * class's cap too (`uncapped`), or it was placed at the capped height and drawn
 * at the written one, past the window (2026-11-08: a settings language list
 * placed 270 high at 380 drew 506 high to 886 in a 723 window).
 *
 * Measured whole, a list scrolled in it holds all it has for that moment and
 * loses its place; each is put back where it was, so a list that grows while
 * read stays where the reader is (2026-11-19: a move search's "더 불러오기"
 * at the list's foot put the list back at its top).
 */
export function naturalSize(element: HTMLElement, uncapped = false): { width: number; height: number } {
  const { maxHeight, maxWidth } = element.style;
  const scrolled = [element, ...element.querySelectorAll<HTMLElement>("*")].flatMap((node) => node.scrollTop > 0 ? [[node, node.scrollTop] as const] : []);
  element.style.maxHeight = uncapped ? "none" : ""; element.style.maxWidth = "";
  const measured = { width: element.offsetWidth, height: element.offsetHeight };
  element.style.maxHeight = maxHeight; element.style.maxWidth = maxWidth;
  for (const [node, top] of scrolled) node.scrollTop = top;
  return measured;
}
