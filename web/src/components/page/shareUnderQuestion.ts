import { type CSSProperties, type RefObject, useEffect } from "react";
import { onLayersChanged } from "@/lib/overlayLayer";

/**
 * The share ⌘⇧O opens over a question ("이 속성을 삭제할까요?",
 * ConfirmActionDialog), as Notion's (2026-11-26, Chrome 1440, judged by the
 * screen and one letter after each step with nothing run in the page before
 * them, in two tabs at two times): drawn under the question's shade, it takes
 * the focus and the letters, and Escape closes it first; a press lands on the
 * question — on its shade, the question goes as cancelled and the share stays,
 * the focus back on what held it in the share.
 * The question's z-index as the share opens, or none with no question open.
 */
export function questionOverShare(): number | undefined {
  const question = [...document.querySelectorAll('[role="alertdialog"][aria-modal="true"]')].at(-1)?.parentElement;
  const zIndex = question ? Number(getComputedStyle(question).zIndex) : Number.NaN;
  return Number.isFinite(zIndex) ? zIndex : undefined;
}

/**
 * What the share's layer is given under such a question: no modal hold, which
 * would make the question inert, no press beside it, which would take the
 * question's own, and the focus taken at once all the same.
 */
export function shareLayerUnder(question: number | undefined, close: () => void) {
  return question === undefined ? { modal: true, onOutsidePress: close } : { modal: false, initialFocus: "first" as const };
}

/** Its window's place in the stack, under the question's shade, or as placed (`below` 1 for the window, 2 for its backdrop). */
export function underQuestion(question: number | undefined, below: 1 | 2): CSSProperties {
  return question === undefined ? {} : { zIndex: question - below };
}

/** Once the question it opened under has gone, the focus goes back to what last held it in the share, when nothing has it. */
export function useFocusBackFromQuestion(question: number | undefined, dialog: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const root = dialog.current;
    if (question === undefined || !root) return undefined;
    let held = root.contains(document.activeElement) ? document.activeElement : null;
    const hold = (event: FocusEvent) => { held = event.target instanceof Element ? event.target : held; };
    root.addEventListener("focusin", hold);
    const stop = onLayersChanged(() => {
      if (questionOverShare() !== undefined) return;
      stop();
      if (document.activeElement && document.activeElement !== document.body) return;
      (held instanceof HTMLElement && held.isConnected ? held : root).focus({ preventScroll: true });
    });
    return () => { root.removeEventListener("focusin", hold); stop(); };
  }, [question, dialog]);
}
