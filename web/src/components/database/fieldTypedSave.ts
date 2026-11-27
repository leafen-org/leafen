import type { TypedDraft } from "@/lib/typedDrafts";
import { typedField } from "@/lib/typedDraftSender";
import { TYPED_DRAFT_SAVE_MS } from "@/lib/useTypedDraftSave";
import { holdUntilTypingEnds } from "@/lib/typedWrites";

/** What a field's value kept on the device at each key holds, its ID left to the field's typing (typedDraftSender.ts). */
export type FieldKept = Omit<TypedDraft, "mutationId">;

/**
 * How a field that saves as it is left (richTextCellEngine's commitOn
 * "blur") saves what is typed meanwhile, its caret staying, so a reload or
 * a closed window keeps it, as Notion's row title (seen 2026-11-03: saved
 * while its caret stayed). One whose value is kept on this device at each
 * key (`kept`: a row's page's title) has it put there as the key is typed,
 * and is saved as typed titles are (typedDraftSender.ts): once the typing
 * pauses or ends — Enter, Escape, Tab, ⌘Z, the field left, the window hidden,
 * the page going — under the ID its record holds, so a browser killed
 * mid-word loses nothing. One without saves shortly after the typing stops
 * (TYPED_DRAFT_SAVE_MS), and at once as the typing ends otherwise, the
 * window hides or the page goes, held where typed fields are (typedWrites.ts
 * holdUntilTypingEnds), as a name's field does (useTypedDraftSave).
 */
export function fieldTypedSave(save: () => void) {
  let timer = 0;
  let release: (() => void) | null = null;
  const typing = typedField();
  const stopHeld = () => {
    window.clearTimeout(timer);
    timer = 0;
    release?.();
    release = null;
  };
  const saveNow = () => {
    stopHeld();
    save();
  };
  return {
    /** A key typed (an input method's too): kept on the device when the field keeps it (`kept`), saved after the pause. */
    typed(kept: FieldKept | null | undefined) {
      window.clearTimeout(timer);
      if (kept) typing.key(kept, saveNow);
      else {
        timer = window.setTimeout(saveNow, TYPED_DRAFT_SAVE_MS);
        release ??= holdUntilTypingEnds(saveNow);
      }
    },
    /** Makes the field's change, under its kept record's ID (TypedField.send). */
    send<Answer>(make: () => Answer): Answer {
      return typing.send(make);
    },
    /** Found nothing new to save: what was kept as typed goes. */
    unchanged() {
      typing.sent();
    },
    stop: stopHeld,
  };
}
