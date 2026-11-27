import { applyTextOperationToSpans, createTextOperation } from "@/lib/textOperations";
import type { TextSpan } from "@/lib/types";

/** Wire JSON object key order carries no product meaning. */
export function editorContentEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (!a || !b || typeof a !== "object" || typeof b !== "object") return false;
  if (Array.isArray(a) || Array.isArray(b)) {
    return Array.isArray(a) && Array.isArray(b) && a.length === b.length && a.every((item, index) => editorContentEqual(item, b[index]));
  }
  const first = a as Record<string, unknown>, last = b as Record<string, unknown>;
  const keys = Object.keys(first);
  return keys.length === Object.keys(last).length && keys.every(key => Object.hasOwn(last, key) && editorContentEqual(first[key], last[key]));
}

function richSpans(value: unknown): value is TextSpan[] {
  return Array.isArray(value) && value.every(span => span && typeof span === "object" && typeof span.text === "string");
}

const booleanMarks = new Set(["bold", "italic", "underline", "strikethrough", "code"]);
function markEqual(key: string, a: unknown, b: unknown) {
  return editorContentEqual(booleanMarks.has(key) ? a ?? false : a, booleanMarks.has(key) ? b ?? false : b);
}

/** Align equal text by span boundaries, retaining every untouched peer mark. */
function richMarkDelta(current: TextSpan[], expected: TextSpan[], target: TextSpan[], conditional: boolean) {
  const text = (spans: TextSpan[]) => spans.map(span => span.text).join("");
  if (text(current) !== text(expected) || text(expected) !== text(target)) return undefined;
  const cursors = [current, expected, target].map(spans => ({ spans: spans.filter(span => span.text), index: 0, offset: 0 }));
  const result: TextSpan[] = [];
  while (cursors.every(cursor => cursor.index < cursor.spans.length)) {
    const [stored, before, wanted] = cursors.map(cursor => cursor.spans[cursor.index]);
    const length = Math.min(...cursors.map(cursor => cursor.spans[cursor.index].text.length - cursor.offset));
    const value = { ...stored, text: stored.text.slice(cursors[0].offset, cursors[0].offset + length) } as unknown as Record<string, unknown>;
    const baseline = before as unknown as Record<string, unknown>, next = wanted as unknown as Record<string, unknown>;
    for (const key of new Set([...Object.keys(baseline), ...Object.keys(next)])) {
      if (key === "text" || markEqual(key, baseline[key], next[key])) continue;
      if (conditional && !markEqual(key, value[key], baseline[key])) continue;
      if (!Object.hasOwn(next, key) || booleanMarks.has(key) && !next[key]) delete value[key];
      else value[key] = next[key];
    }
    const span = value as unknown as TextSpan;
    const last = result.at(-1);
    if (last && !span.mention && !span.equation && !span.template && editorContentEqual({ ...last, text: "" }, { ...span, text: "" })) last.text += span.text;
    else result.push(span);
    for (const cursor of cursors) {
      cursor.offset += length;
      if (cursor.offset === cursor.spans[cursor.index].text.length) { cursor.index++; cursor.offset = 0; }
    }
  }
  return result;
}

function fieldDelta(current: unknown, expected: unknown, target: unknown, conditional: boolean) {
  if (editorContentEqual(current, expected)) return target;
  if (richSpans(current) && richSpans(expected) && richSpans(target)) {
    const marks = richMarkDelta(current, expected, target, conditional);
    if (marks) return marks;
    const operation = createTextOperation(expected, target);
    const applied = operation && applyTextOperationToSpans(current, operation);
    if (applied) return applied;
  }
  return conditional ? current : target;
}

/** Captions and each table cell retain their existing native graph authority. */
export function applyEditorContentDelta(
  current: Record<string, unknown>, expected: Record<string, unknown>, target: Record<string, unknown>, conditional = false,
) {
  const result = { ...current };
  for (const key of new Set([...Object.keys(expected), ...Object.keys(target)])) {
    if (editorContentEqual(expected[key], target[key])) continue;
    if (key === "cells" && Array.isArray(current[key]) && Array.isArray(expected[key]) && Array.isArray(target[key])
      && current[key].length === expected[key].length && expected[key].length === target[key].length) {
      const cells = [...current[key]];
      for (let index = 0; index < cells.length; index++) {
        if (!editorContentEqual(expected[key][index], target[key][index])) {
          cells[index] = fieldDelta(cells[index], expected[key][index], target[key][index], conditional);
        }
      }
      result[key] = cells;
    } else if (key === "caption") {
      // A caption the target has none of goes, rather than staying as an empty
      // key: an Undo of a picture pasted over a line left the line one, and the
      // next Undo kept the line as changed since (2026-11-06).
      const caption = fieldDelta(current[key], expected[key], target[key], conditional);
      if (caption === undefined && !Object.hasOwn(target, key)) delete result[key];
      else result[key] = caption;
    }
    else if (!conditional || editorContentEqual(current[key], expected[key])) {
      if (Object.hasOwn(target, key)) result[key] = target[key]; else delete result[key];
    }
  }
  return result;
}
