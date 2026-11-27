import type { Editor } from "@tiptap/core";

/**
 * The phone tools' "+" and picture open the "/" menu at the caret with no "/"
 * in the line; their "@" types its "@" instead (NativePhoneToolbar.tsx).
 */
export const NATIVE_PHONE_MENU = "leafen:native-phone-menu";
export type NativePhoneMenuRequest = { owner: HTMLElement; query: string };
export function requestNativePhoneMenu(editor: Editor, query = "") {
  window.dispatchEvent(new CustomEvent<NativePhoneMenuRequest>(NATIVE_PHONE_MENU,
    { detail: { owner: editor.view.dom, query } }));
}
