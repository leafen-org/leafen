import { useEffect, useRef, useState } from "react";
import { graphOperationSubject, type ContentGraph, type GraphOperation } from "@/contentGraph";
import { typedField } from "./typedDraftSender";
import { holdUntilPageGoes } from "./typedWrites";
import { typedGraphChangeOf, typedNameRecord } from "./typedGraphChanges";

/**
 * A name typed in a field that saves it as it is left (blur) or on Enter —
 * a property's name and its description, an option's, a view's — saved too
 * when it goes another way: its window closed by a press outside, Escape,
 * its × or a phone sheet swiped down, or the field turned to another thing
 * (`key`, a view chosen while its settings stay open). A window closed so
 * takes the field away before its blur reaches the page, and the words were
 * lost; Notion's keep them (seen 2026-11-18 in Chrome 1440: a view's name
 * typed in its settings stays after a press outside, Escape and ×, the tab
 * renamed only then; on a Galaxy A90 after the sheet was swiped down).
 *
 * Each key keeps on this device the change its saving would make
 * (`change`, as the open page's writes send it: typedGraphChanges.ts), so a
 * reload, a tab closed, a browser killed or a phone's app ended with the
 * field open loses nothing: it goes when Leafen opens here again
 * (typedDraftSender.ts recoverTypedDrafts). Its saving goes under the same
 * ID (typedGraphChange), so the server takes it once; set aside, what it
 * kept goes. `part` tells two fields naming one thing apart (a property's
 * name and description). The page going closes the field and saves it
 * then (typedWrites.ts holdUntilPageGoes), as the outbox still takes it.
 *
 * `save` is called with what was typed and not yet saved, from the field's
 * last drawing before it went (so for the thing it showed then).
 */
export function useTypedName(key: string, part: string, save: (value: string) => void,
  change?: (value: string, graph: ContentGraph) => GraphOperation[] | null) {
  const typed = useRef<string | null>(null);
  const latest = useRef({ save, change });
  // Set after each drawing: when the field turns to another thing, the
  // leaving below runs first and still reads the one it showed.
  useEffect(() => { latest.current = { save, change }; });
  const [field] = useState(typedField);
  // The page going closes the field: what was typed is saved then, as the outbox still takes it.
  const going = useRef<(() => void) | null>(null);
  const saveTyped = () => {
    going.current?.();
    going.current = null;
    const value = typed.current;
    typed.current = null;
    if (value !== null) field.send(() => latest.current.save(value));
    else field.sent();
  };
  useEffect(() => saveTyped, [key, field]); // eslint-disable-line react-hooks/exhaustive-deps
  return {
    /** The field's words as typed: kept on this device as the change saving them makes, saved as it goes unless saved before. */
    typed(value: string) {
      typed.current = value;
      going.current ??= holdUntilPageGoes(() => { going.current = null; saveTyped(); });
      const { change: made } = latest.current;
      const saving = made ? typedGraphChangeOf((graph) => made(value, graph)) : null;
      // Typed back to the name it has, nothing it kept stands.
      if (!saving) {
        field.sent();
        return;
      }
      field.record({ recordKey: typedNameRecord(`${graphOperationSubject(saving.operations[0])}/${part}`), kind: "graph", hostPageId: saving.pageId,
        operations: saving.operations });
    },
    /** Saved now (blur, Enter), under the ID its typing was kept as. */
    saved(value: string) {
      typed.current = value;
      saveTyped();
    },
    /** Set aside: the field shows another value, or what was typed is let go (a new view's name closed unnamed). */
    settled() {
      typed.current = null;
      saveTyped();
    },
  };
}
