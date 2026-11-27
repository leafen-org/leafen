import * as Y from "yjs";
import type { SuggestedCharacter, SuggestingInputQueue, SuggestingSelection } from "./suggestingInputQueue";

/** PM's pending characters are anchored to the native acknowledgement which
 * creates them. Later fast input can target them without numeric CRDT guesses.
 * A line drawn before its block is there (`drawn`) holds `length` characters
 * the block will hold once `ready` says it came. */
export function createNativeSuggestedDraft(text: Y.Text, drawn?: { length: number; ready: Promise<boolean> }) {
  // Its characters, as the line's offsets count them: an empty piece's embed
  // has a position in the text and none in the line's offsets.
  let characters: Promise<SuggestedCharacter | null>[] = [];
  const read = () => {
    const found: SuggestedCharacter[] = [];
    let index = 0;
    for (const delta of text.toDelta() as Array<{ insert: unknown }>) {
      if (typeof delta.insert !== "string") { index += 1; continue; }
      for (let offset = 0; offset < delta.insert.length; offset++, index++) found.push({
        start: Y.createRelativePositionFromTypeIndex(text, index, 0),
        end: Y.createRelativePositionFromTypeIndex(text, index + 1, -1),
      });
    }
    return found;
  };
  if (drawn) {
    const landed = drawn.ready.then(came => came ? read() : []);
    characters = Array.from({ length: drawn.length }, (_, at) => landed.then(found => found[at] ?? null));
  } else characters = read().map(character => Promise.resolve(character));
  // Characters right after words the line no longer shows but the text still
  // holds, struck through, until the page is drawn again (a selection typed
  // over or deleted while suggesting): the caret there stands before those
  // words, so what is typed next goes in before them, as Notion's does —
  // after the new words of a replacement, before words suggested away.
  const afterStruck = new WeakSet<Promise<SuggestedCharacter | null>>();
  const empty = Y.createRelativePositionFromTypeIndex(text, 0, -1);
  function point(offset: number, right: boolean) {
    const index = Math.max(0, Math.min(offset, characters.length));
    return right && index < characters.length ? characters[index].then(value => value?.start ?? null)
      : index > 0 ? characters[index - 1].then(value => value?.end ?? null) : Promise.resolve(empty);
  }
  // The caret's place: before the character after it, or after the one before
  // it when struck words lie between (afterStruck) — or when that one is gone.
  function caret(offset: number) {
    const index = Math.max(0, Math.min(offset, characters.length));
    if (index === 0 || index === characters.length || !afterStruck.has(characters[index])) return point(index, true);
    return point(index, false).then(value => value ?? point(index, true));
  }
  function select(start: number, end: number): SuggestingSelection {
    const at = start === end ? caret(start) : point(start, true);
    return { start: at, end: start === end ? at : point(end, false), collapsed: start === end,
      owned: Promise.all(characters.slice(start, end)) };
  }
  /**
   * The line changed from `before` to `after`; `selected` is what was
   * selected in `before`, which typing over replaces whole — even where a
   * typed letter is the same as the selection's last (2026-11-22: "o" typed
   * over "echo" read as "ech" deleted and "o" kept) — and `caret` where the
   * caret stands in `after`.
   */
  function change(queue: SuggestingInputQueue, before: string, after: string, selected?: { start: number; end: number; caret?: number }) {
    if (before.length !== characters.length) throw new RangeError("Suggested draft ownership is stale.");
    let start = 0, endBefore = before.length, endAfter = after.length;
    const head = selected && selected.start < selected.end ? before.slice(0, selected.start) : null;
    const tail = selected && head !== null ? before.slice(selected.end) : "";
    if (selected && head !== null && after.length >= head.length + tail.length && after.startsWith(head) && after.endsWith(tail)) {
      start = selected.start; endBefore = selected.end; endAfter = after.length - tail.length;
    } else {
      while (start < Math.min(before.length, after.length) && before[start] === after[start]) start++;
      while (endBefore > 0 && endAfter > 0 && before[endBefore - 1] === after[endAfter - 1]) { endBefore--; endAfter--; }
      const low = Math.min(endBefore, endAfter);
      if (low < start) {
        // Letters the same on both sides of what was typed or taken away: it
        // could sit anywhere from `low` to `start`, and it sits at the caret —
        // where the letters were typed, or the ones Backspace took (the caret
        // after it) — as ProseMirror reads a change in the page (findDiff's
        // preferred position). Read from the far end, a letter typed before
        // words the same letter starts went in after that letter (2026-11-22:
        // "e" typed in "om|echo", the struck "echo" drawn after the new "om",
        // went in after the struck "e", the rest of "omega" with it).
        const at = selected ? Math.min(selected.start, selected.caret ?? selected.start) : start;
        if (at >= low && at <= start) start = at;
        if (start > low && /[\uD800-\uDBFF]/u.test(before[start - 1])) start--;
        endBefore = start + Math.max(0, before.length - after.length);
        endAfter = start + Math.max(0, after.length - before.length);
      } else {
        if (start > 0 && /[\uD800-\uDBFF]/u.test(before[start - 1])) start--;
        if (endBefore < before.length && /[\uDC00-\uDFFF]/u.test(before[endBefore])) { endBefore++; endAfter++; }
      }
    }
    const selection = select(start, endBefore), inserted = after.slice(start, endAfter);
    const result = inserted ? queue.insert(inserted, selection) : null;
    if (!inserted) queue.delete(selection, "backward");
    if (endBefore > start && endBefore < characters.length) afterStruck.add(characters[endBefore]);
    characters = [...characters.slice(0, start), ...Array.from({ length: inserted.length }, (_, index) =>
      result!.then(chars => chars[index] ?? null)), ...characters.slice(endBefore)];
  }
  /** The line ends at `at`: the rest went to the line a break drew after it. */
  function cut(at: number) { characters = characters.slice(0, Math.max(0, at)); }
  return { select, change, cut };
}
