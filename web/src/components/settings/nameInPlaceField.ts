import type { FocusEvent, KeyboardEvent } from "react";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

/**
 * Opens a name field (NameInPlaceField.module.css) with the caret at the
 * name's end, as Notion's settings windows do (seen 2026-11-16, judged by
 * the letter typed next: it goes after the name, not over it). Only as it
 * opens: coming back to the window leaves the caret where it was.
 */
export function caretAtEnd(event: FocusEvent<HTMLInputElement>) {
  const field = event.currentTarget;
  if (field.dataset.caretPlaced) return;
  field.dataset.caretPlaced = "1";
  field.setSelectionRange(field.value.length, field.value.length);
}

/**
 * A name field's keys, as Notion's settings windows take them (seen
 * 2026-11-16: the group's two ways, the teamspace's three): Enter keeps the
 * name typed; Escape drops it — the field closes on the name as it was, the
 * window stays open and nothing is saved — unlike the rename boxes over a
 * page, a tab or a library row, which keep what was typed. Keys pressed while
 * a word is still being composed (Korean) are the composer's. Closed by
 * either key, the field leaves the keys on the page, not on the pencil that
 * takes its place: the field is marked data-focus-leaves (overlayLayer.ts), as
 * Notion's focus goes to the page's body with no ring (seen 2026-11-16:
 * Escape three times, opened by a press twice and by Enter on the pencil once,
 * and Enter once — the letter typed next went nowhere).
 */
export function nameFieldKeyDown(event: KeyboardEvent<HTMLInputElement>, keys: {
  onEnter(field: HTMLInputElement): void;
  onEscape(): void;
}) {
  if (imeComposing(event)) return;
  if (fieldEnter(event)) {
    event.preventDefault();
    keys.onEnter(event.currentTarget);
  } else if (event.key === "Escape") {
    event.preventDefault();
    event.stopPropagation();
    keys.onEscape();
  }
}
