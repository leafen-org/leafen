import { useEffect, useRef } from "react";
import { noteSettingsPlaceWindowShown } from "@/lib/databaseStartPanel";

/**
 * A window standing where the database's settings open is their window: its
 * "⚙" stays pressed while it shows, and a press on it puts the window away
 * (noteSettingsPlaceWindowShown). `toolbar` is that database's toolbar, none
 * where it opened elsewhere — by a page's or a layout's own button, or inside
 * the settings, which keep "⚙" pressed themselves.
 */
export function useSettingsPlaceWindow(toolbar: HTMLElement | null, cancel: () => void) {
  const cancelRef = useRef(cancel);
  cancelRef.current = cancel;
  useEffect(() => toolbar ? noteSettingsPlaceWindowShown(toolbar, () => cancelRef.current()) : undefined, [toolbar]);
}
