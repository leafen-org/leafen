import { autoUpdate, type ReferenceElement } from "@floating-ui/dom";
import { watchPhoneTools } from "./phoneTools";

/**
 * What places an open popover again (usePopoverPlacement, useFitPopoverInWindow):
 * Floating UI's autoUpdate — its opener moved or resized, a scroll under it,
 * its own size at the next frame's ResizeObserver step —, and its content
 * changing its size, placed again in that very task. Rows drawn after it was
 * placed (a menu's list built a pass later, rows that follow an access
 * read) left it grown at its old place until that step, past the window to
 * whatever read it in between (2026-11-21: a sidebar row's menu placed over
 * the point at 443 for its 4 rows read 443..873 with its 10 for a frame's
 * scripts, then stood at 182..612). What it writes on itself — its place,
 * cap and marks — is not its content, a change that leaves its size as it
 * was places nothing, and it is placed so at most three times in one task:
 * content that grows each time it is placed is left to the frame's step.
 * `shown` also places it again for what shows of the page (the visual
 * viewport: a phone's keyboard opening or closing under it, or the view
 * panned) and for the phone's editing tools coming, going or moving.
 */
export function placeAgainOn(reference: ReferenceElement, root: HTMLElement, element: HTMLElement, update: () => void,
  { layoutShift, shown }: { layoutShift: boolean; shown: boolean }): () => void {
  const stopAuto = autoUpdate(reference, element, update, { elementResize: typeof ResizeObserver !== "undefined", layoutShift });
  const view = shown ? window.visualViewport : null;
  view?.addEventListener("resize", update);
  view?.addEventListener("scroll", update);
  const unwatch = shown ? watchPhoneTools(update) : () => {};
  let seen = sizeOf(element), burst = 0, calm = 0;
  const content = typeof MutationObserver === "undefined" ? null : new MutationObserver((records) => {
    if (!records.some((record) => record.type !== "attributes" || (record.target !== root && record.target !== element))) return;
    const now = sizeOf(element);
    if (now === seen || burst >= 3) return;
    seen = now;
    burst += 1;
    if (!calm) calm = window.setTimeout(() => { burst = 0; calm = 0; });
    update();
  });
  content?.observe(root, { subtree: true, childList: true, characterData: true, attributes: true });
  return () => {
    stopAuto();
    unwatch();
    content?.disconnect();
    window.clearTimeout(calm);
    view?.removeEventListener("resize", update);
    view?.removeEventListener("scroll", update);
  };
}

// Its laid-out size, as the ResizeObserver step sees it change; a transform
// (an opening's scale) does not count.
function sizeOf(element: HTMLElement) {
  return `${element.offsetWidth} ${element.offsetHeight}`;
}
