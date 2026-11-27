import { TextSelection, type EditorState, type Selection, type Transaction } from "@tiptap/pm/state";
import { ReplaceStep } from "@tiptap/pm/transform";
import type { EditorView } from "@tiptap/pm/view";
import { TEXT_UNDO_CAPTURE_MS } from "@/lib/blockTextEditing";
import { imeComposing } from "@/lib/fieldKeyBoundary";

/** Whether what is chosen runs over more than one line of text. */
function overLines(selection: Selection): selection is TextSelection {
  return selection instanceof TextSelection && !selection.$from.sameParent(selection.$to);
}

/**
 * What the person did: a change a plugin appended to theirs (a pasted line's
 * typed rules, nativeTiptapClipboard; the caret line's focus) is not it, and
 * the page reads the last of them (2026-11-13: a paste over lines read as no change).
 */
function done(transaction: Transaction): Transaction {
  return transaction.getMeta("appendedTransaction") ?? transaction;
}

/** Whether a change only puts words in, or takes them out: no line or block of its own. */
function wordsOnly(transaction: Transaction) {
  return transaction.steps.length > 0 && transaction.steps.every(step => {
    if (!(step instanceof ReplaceStep)) return false;
    let inline = true;
    step.slice.content.forEach(node => { if (!node.isInline) inline = false; });
    return inline;
  });
}

/** Whether a change put words over chosen lines, or took them out: typed, deleted, pasted, cut — not dropped. */
function overwritesLines(before: EditorState, transaction: Transaction) {
  return overLines(before.selection) && wordsOnly(done(transaction)) && done(transaction).getMeta("uiEvent") !== "drop";
}

/**
 * The page as an Undo of a typed change brings it back, with what it chooses.
 *
 * Typing starts where its text goes in. The caret held before it can still be
 * where it was before the click that placed it (2026-11-05: a row page's
 * body, after Undo had taken the caret to its title, held the line's end, and
 * Undo of what was typed at its start went there).
 *
 * What was typed, deleted, pasted or cut over lines goes back chosen from the
 * first line's start to where the choice ended, its moving end at the bottom
 * however the choice was made. As Notion's, measured in Chrome with no script
 * before the action, the screen after ⌘Z and then one key: "q" typed over a
 * ⇧↓ choice came back chosen from the first line's start, and the next letter
 * replaced that (2026-11-15, worker w1132); one typed over a ⇧↑ choice came
 * back with its moving end at the bottom, ⇧→ and ⇧← after ⌘Z moving the lower
 * end (w1132 2 of 2, and w1249 2 of 2 in another tab at another time; once
 * of those Notion's came back from where the choice began rather than from
 * the line's start). Typed letters undone leave the caret where they went in,
 * as Notion's (2 of 3 the same day; once, after a run of undos, Notion's
 * stayed where it had been clicked). A drop puts what was dragged elsewhere
 * and keeps its own choice.
 */
export function typedUndoStart(before: EditorState, transaction: Transaction, typing: boolean) {
  const step = transaction.steps[0], { selection } = before, { $from, $to } = selection;
  const insertedAt = typing && selection.empty && step instanceof ReplaceStep && step.from === step.to ? step.from : null;
  const chosen = insertedAt !== null && insertedAt !== selection.from ? TextSelection.create(before.doc, insertedAt)
    : overwritesLines(before, transaction) ? TextSelection.create(before.doc, $from.start(), $to.pos) : null;
  return chosen ? before.apply(before.tr.setSelection(chosen).setMeta("addToHistory", false)) : before;
}

/**
 * What is typed, deleted, pasted, cut or composed over chosen lines, and
 * typed on right after it, is one Undo with the lines it took out: one ⌘Z
 * brings the lines back, chosen as typedUndoStart says, as Notion's (seen
 * 2026-11-13: "yz" typed over two lines, ⌫ or ⌘V or ⌘X then "a", came back
 * with one ⌘Z; here the first took back only "z").
 * Composing (Korean, Japanese, Chinese) begun over chosen lines: the browser
 * takes the lines' words out as it starts, and the engine reads that as a
 * change of its own before the composed letters come (2026-11-13: the first
 * ⌘Z took back only the composed letter and left the lines joined). Typing on
 * goes with it as typing goes with typing (TEXT_UNDO_CAPTURE_MS apart, or one
 * composition); a paste, a cut, a pause or anything else ends it.
 */
export function typingOverLines<Action>() {
  // The composition going on in the join: null for the one whose start took the lines out.
  let starting = false, joining: { action: Action; time: number; composition: unknown } | null = null;
  return {
    /** A composition starts in the page (before the engine reads what the browser took out). */
    start(view: EditorView) {
      starting = !imeComposing(view) && overLines(view.state.selection);
      if (starting) joining = null;
    },
    /** The action a change goes into: what took the lines out, for what is typed in their place. */
    joins(transaction: Transaction, typing: boolean) {
      const own = done(transaction), composition = own.getMeta("composition");
      if (!joining || !typing || own.getMeta("uiEvent") || !(own.time - joining.time < TEXT_UNDO_CAPTURE_MS
        || composition && (joining.composition === null || joining.composition === composition))) { joining = null; return null; }
      joining.time = own.time;
      joining.composition = composition;
      return joining.action;
    },
    /** The action a change made (none when it joined one). */
    made(action: Action | null | undefined, before: EditorState, transaction: Transaction) {
      const own = done(transaction);
      if (action && (starting || overwritesLines(before, transaction))) {
        joining = { action, time: own.time, composition: own.getMeta("composition") ?? (starting ? null : undefined) };
      }
      starting = false;
    },
  };
}
