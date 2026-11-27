import { useState } from "react";
import { notePageHistoryStep } from "@/components/editor/pageUndo";

let unchangedSteps = 0;

/**
 * The formula an editor holds: what it shows (`draft`) and what "모든 변경 사항 취소" goes back to (`kept` — the
 * formula it opened with, or the one an automation's "저장" kept). A property's formula changed in the editor and
 * back again before it closes is still a step of the page's history, one that changes nothing to see, as Notion's
 * (seen 2026-11-15 twice in Chrome: "tod", today(), "x" and two ⌘Z, then ✕ — the first ⌘Z on the page changes
 * nothing, the second takes the new property back; opened and closed with nothing typed, the first ⌘Z takes it back).
 */
export function useFormulaDraft(saved: string) {
  const [kept, setKept] = useState(saved);
  const [draft, setShown] = useState(saved);
  // Whether the formula was changed since it opened or was last kept, even if back as it was.
  const [edited, setEdited] = useState(false);
  return {
    kept,
    draft,
    setDraft(text: string) {
      if (text !== kept) setEdited(true);
      setShown(text);
    },
    keepDraft() {
      setKept(draft);
      setEdited(false);
    },
    /** Closing with the formula as it opened: the step it was, if it was changed meanwhile. */
    noteUnchangedStep(pageId: string, propertyId: string) {
      if (edited) notePageHistoryStep(pageId, `formula-unchanged/${propertyId}/${++unchangedSteps}`, async () => true);
    },
  };
}
