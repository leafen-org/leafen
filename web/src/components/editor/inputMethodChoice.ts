import type { Selection } from "@tiptap/pm/state";
import { imeComposing } from "@/lib/fieldKeyBoundary";

/** An editor's view as the rule reads it: whether it composes, and what is chosen. */
type ChoosingView = { composing: boolean; state: { selection: Selection } };

// What an input method held chosen as its composition ended, by the view.
const heldByInputMethod = new WeakMap<object, Selection>();

/**
 * Whether an editor's chosen words are a person's — the one rule the tools
 * over chosen words draw by: the page's format bar, a field's (a table
 * cell's, a row value's, a comment box's) and a phone's tools for chosen
 * words. An input method's are not: during a Japanese conversion (Space
 * choosing a candidate) macOS Chrome hands the clause being converted over as
 * a DOM range, which the editor reads as chosen words; Notion draws no bar
 * for it (seen 2026-11-21 in Chrome at 1440 with the Japanese romaji input
 * method: a page's line twice, a table cell once). What the input method
 * held chosen stays not chosen after the composition ends, until the
 * selection moves: a table cell kept its bar after the conversion was
 * confirmed.
 */
export function wordsChosenByPerson(view: ChoosingView): boolean {
  const { selection } = view.state;
  if (imeComposing(view)) {
    heldByInputMethod.set(view, selection);
    return false;
  }
  const held = heldByInputMethod.get(view);
  if (held) {
    if (held.eq(selection)) return false;
    heldByInputMethod.delete(view);
  }
  return !selection.empty;
}
