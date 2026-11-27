import { createFocusManager } from "@react-aria/focus";
import { fieldFocusOnOpen, raisesKeyboard } from "./screenKind";

/**
 * Where a modal layer's focus goes once the control that held it went away
 * (keepModalFocus, overlayLayer.ts): to what took its place there, else the
 * first control of the nearest group around it still drawn, else the layer's
 * first, else the layer itself. On a touch screen a field to type in waits
 * for a tap there too, as when a window opens (phoneFieldWaits): focused with
 * no tap, it raised the keyboard over the screen that took a tapped row's
 * place (2026-11-16, a Galaxy phone: settings › 서버 › 워크스페이스 opened with
 * its search field focused and the keyboard over the list), as Notion's phone
 * settings open with the keyboard down (seen 2026-11-10).
 */
export function rehomeModalFocus(root: HTMLElement, stand: HTMLElement | null, groups: readonly Element[]) {
  const typingWaits = !fieldFocusOnOpen();
  const accept = (node: Element) => !typingWaits || !raisesKeyboard(node);
  if (stand && accept(stand)) {
    stand.focus({ preventScroll: true });
    return;
  }
  // Outward from the nearest group still drawn, to the first that has a control.
  if (groups.some((group) => createFocusManager({ current: group }).focusFirst({ tabbable: true, accept }))) return;
  if (!createFocusManager({ current: root }).focusFirst({ tabbable: true, accept })) {
    if (!root.hasAttribute("tabindex")) root.tabIndex = -1;
    root.focus({ preventScroll: true });
  }
}
