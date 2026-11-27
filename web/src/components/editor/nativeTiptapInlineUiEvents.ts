export const NATIVE_EQUATION_REQUEST = "leafen:native-equation-request";
export type NativeEquationRequest = { from: number; to: number };
/** Opens the equation field for the words from `from` to `to`, or, with no words, one made there. */
export function requestNativeTiptapEquation(editor: { view: { dom: HTMLElement } }, from: number, to: number) {
  editor.view.dom.dispatchEvent(new CustomEvent<NativeEquationRequest>(NATIVE_EQUATION_REQUEST, { detail: { from, to } }));
}

export const NATIVE_LINK_EDIT_REQUEST = "leafen:native-link-edit-request";
/** Opens the window for the link the selection stands on: under the
 * selection's middle, or from `opener`, the control that asked for it. */
export function requestNativeTiptapLinkEdit(editor: unknown, opener?: HTMLElement | null) {
  window.dispatchEvent(new CustomEvent(NATIVE_LINK_EDIT_REQUEST, { detail: { editor, opener } }));
}

/**
 * A link window's choosing of its link's words: "quiet" while it is open,
 * which draws no selection toolbar not already shown, as Notion's (seen
 * 2026-11-09: the first Escape in a window opened by the link's "편집" chooses
 * the words, no toolbar), and "done" as it closes, which may draw one.
 */
export const LINK_WINDOW_CHOICE = "leafenLinkWindowChoice";
