import { useEffect, useRef } from "react";

/**
 * Escape closes an open sidebar menu's last opened part wherever the keys
 * are, as Notion's menus do; a part that answers Escape itself stops it.
 */
export function useMenuEscape(open: boolean, onEscape: (event: KeyboardEvent) => void) {
  const handler = useRef(onEscape);
  handler.current = onEscape;
  useEffect(() => {
    if (!open) return undefined;
    const listener = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented) return;
      event.preventDefault();
      handler.current(event);
    };
    // Before the page's own Escape (a peek, a selection) hears it.
    document.addEventListener("keydown", listener, true);
    return () => document.removeEventListener("keydown", listener, true);
  }, [open]);
}
