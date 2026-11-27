// WebKit, the desktop app's engine, keeps :hover on an element moved with the
// native drag and drop until the pointer crosses that element again (seen
// 2026-11-06: a dropped sidebar row kept its fill and buttons with the pointer
// elsewhere, where Notion's row shows them only under the pointer). The row is
// marked after its drop, its styles skip :hover while marked, and the pointer's
// next move on it takes the mark off.

export function markHoverStale(element: HTMLElement) {
  element.dataset.hoverStale = "true";
  const clear = () => {
    delete element.dataset.hoverStale;
    element.removeEventListener("pointermove", clear);
  };
  element.addEventListener("pointermove", clear);
}
