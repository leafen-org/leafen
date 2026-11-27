import type { EditorView } from "@tiptap/pm/view";
import * as Y from "yjs";
import type { TextSpan } from "@/lib/types";
import { spanToYTextAttributes } from "@/lib/collaborationCrdtCodec";

type Character = { start: Y.RelativePosition; end: Y.RelativePosition };
type Attributes = Record<string, string | boolean>;

function characters(text: Y.Text, start: number, length: number): Character[] {
  return Array.from({ length }, (_, offset) => ({
    // Each interval belongs to one existing character. A peer insertion at
    // either edge is outside it, so replacing a composing range keeps peers.
    start: Y.createRelativePositionFromTypeIndex(text, start + offset, 0),
    end: Y.createRelativePositionFromTypeIndex(text, start + offset + 1, -1),
  }));
}
function attributes(rich: TextSpan[]): Attributes[] {
  return rich.flatMap(span => Array.from({ length: span.text.length }, () => spanToYTextAttributes(span) ?? {}));
}

/**
 * A caret the browser put on an empty line itself, on focus, reads the same as
 * one set there and isn't: what is composed from it is not a composition, and
 * each input method makes what is typed next a new one (2026-11-05: "본문" came
 * out "ㅂㅗㄴ문" on a new page's first line). Set anew before it starts, it is.
 */
export function keepCompositionCaret(view: EditorView) {
  const selection = view.dom.ownerDocument.getSelection();
  if (selection?.isCollapsed && selection.anchorNode?.nodeType === Node.ELEMENT_NODE && view.dom.contains(selection.anchorNode)) {
    selection.collapse(selection.anchorNode, selection.anchorOffset);
  }
}

/** Transient ownership of the composing field; no second persisted draft. */
export function createNativeCompositionText(document: Y.Doc, text: Y.Text) {
  let owned = characters(text, 0, text.length);
  const absolute = (point: Y.RelativePosition) => {
    const value = Y.createAbsolutePositionFromRelativePosition(point, document);
    return value?.type === text ? value.index : null;
  };
  function point(offset: number) {
    const bounded = Math.max(0, Math.min(offset, owned.length));
    return bounded > 0 ? owned[bounded - 1].end : owned[0]?.start
      ?? Y.createRelativePositionFromTypeIndex(text, 0, -1);
  }
  function write(before: TextSpan[], after: TextSpan[]) {
    const first = before.map(span => span.text).join(""), last = after.map(span => span.text).join("");
    if (first.length !== owned.length) throw new RangeError("Composing text ownership is stale.");
    let start = 0, endBefore = first.length, endAfter = last.length;
    while (start < Math.min(endBefore, endAfter) && first[start] === last[start]) start++;
    if (start > 0 && /[\uD800-\uDBFF]/u.test(first[start - 1])) start--;
    while (endBefore > start && endAfter > start && first[endBefore - 1] === last[endAfter - 1]) { endBefore--; endAfter--; }
    if (endBefore < first.length && /[\uDC00-\uDFFF]/u.test(first[endBefore])) { endBefore++; endAfter++; }
    const anchor = start < owned.length ? owned[start].start : point(start);
    // Resolve each owned interval after prior deletes. Deleted peer characters
    // resolve to an empty interval and are never resurrected by composition.
    const deletions: { from: number; to: number }[] = [];
    for (let index = start; index < endBefore; index++) {
      const from = absolute(owned[index].start), to = absolute(owned[index].end);
      if (from === null || to === null || to <= from) continue;
      const previous = deletions[deletions.length - 1];
      if (previous?.to === from) previous.to = to;
      else deletions.push({ from, to });
    }
    for (const range of deletions.reverse()) text.delete(range.from, range.to - range.from);
    const insertion = absolute(anchor);
    if (insertion === null) throw new RangeError("Composing text anchor is unavailable.");
    const nextAttributes = attributes(after), oldAttributes = attributes(before);
    let position = insertion;
    let richOffset = 0;
    for (const span of after) {
      const from = Math.max(start, richOffset), to = Math.min(endAfter, richOffset + span.text.length);
      if (to > from) {
        const inserted = span.text.slice(from - richOffset, to - richOffset);
        text.insert(position, inserted, spanToYTextAttributes(span) ?? {}); position += inserted.length;
      }
      richOffset += span.text.length;
    }
    const nextOwned = [...owned.slice(0, start), ...characters(text, insertion, endAfter - start), ...owned.slice(endBefore)];
    // Retained characters receive only marks changed by this local PM edit.
    // Unseen peer marks and characters outside this ownership stay intact.
    const formats: { from: number; to: number; changes: Record<string, string | boolean | null> }[] = [];
    for (let index = 0; index < nextOwned.length; index++) {
      if (index >= start && index < endAfter) continue;
      const oldIndex = index < start ? index : index - endAfter + endBefore;
      const beforeAttrs = oldAttributes[oldIndex] ?? {}, afterAttrs = nextAttributes[index] ?? {};
      const changes: Record<string, string | boolean | null> = {};
      for (const key of new Set([...Object.keys(beforeAttrs), ...Object.keys(afterAttrs)])) {
        if (beforeAttrs[key] !== afterAttrs[key]) changes[key] = afterAttrs[key] ?? null;
      }
      if (!Object.keys(changes).length) continue;
      const from = absolute(nextOwned[index].start), to = absolute(nextOwned[index].end);
      if (from === null || to === null || to <= from) continue;
      const previous = formats[formats.length - 1];
      if (previous?.to === from && JSON.stringify(previous.changes) === JSON.stringify(changes)) previous.to = to;
      else formats.push({ from, to, changes });
    }
    for (const range of formats) text.format(range.from, range.to - range.from, range.changes);
    owned = nextOwned;
  }
  return { write, point };
}
