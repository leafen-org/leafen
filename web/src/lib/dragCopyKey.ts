import { detectKeyboardShortcutPlatform } from "./keyboardShortcuts";

/**
 * Whether a drag copies what it carries rather than moving it: Option (Alt)
 * held as it is let go, pressed or let go of on the way too, as Notion's
 * (seen 2026-11-16 in its app: Option pressed during a block's drag left a
 * copy where it was let go, the block where it was).
 *
 * On a Mac the system makes a drag a copy while Option is held, and tells
 * the page so at each of the drag's moves and at its drop: the effects the
 * drag allows are "copy" alone (WebKit's DragController and Chromium's pass
 * the system's own on). The keys of those events can be stale — WebKit (the
 * Mac app) reads them in its page's process, which the key never reaches
 * during a drag (2026-11-16: Option pressed during a block's drag moved it
 * there) — so on a Mac the allowed effects are read, the event's Option key
 * only where a drag says nothing of them; elsewhere Alt is the event's.
 * They are noted at the window before the drag's targets read the event
 * (Pragmatic drag and drop listens there from the drag's start, after this).
 */
let allowed = "";
const mac = detectKeyboardShortcutPlatform() === "mac";

if (typeof window !== "undefined") {
  const note = (event: DragEvent) => { allowed = event.dataTransfer?.effectAllowed ?? ""; };
  for (const type of ["dragenter", "dragover", "drop"] as const) window.addEventListener(type, note, { capture: true, passive: true });
  window.addEventListener("dragend", () => { allowed = ""; }, { capture: true, passive: true });
}

/** Whether a drag with this event's keys, as it is now, copies. */
export function dragCopies(input: { altKey: boolean }) {
  if (mac && allowed && allowed !== "uninitialized") return allowed === "copy";
  return input.altKey;
}
