import { Fragment, type Attrs, type Mark, type Node, type Schema } from "@tiptap/pm/model";
import type { TextSpan } from "@/lib/types";
import type { DateMentionChange } from "@/lib/collaborativeMentions";
import { sanitizeTextSpans } from "@/lib/textOperations";
import { textSpanColors } from "@/lib/textSpanColors";

const booleanMarks = {
  bold: "bold", italic: "italic", underline: "underline",
  strikethrough: "strike", code: "code",
} as const;
const formatting = new Set<string>([...Object.keys(booleanMarks), "color", "background", "link"]);
const annotationFields = new Set(["commentId", "suggestion", "suggestionId", "suggestionBefore"]);

function metadata(span: TextSpan) {
  return Object.fromEntries(Object.entries(span).filter(([key]) => key !== "text" && !formatting.has(key)));
}

function atomPayload(span: TextSpan) {
  return JSON.stringify(Object.fromEntries(Object.entries(metadata(span)).filter(([key]) => !annotationFields.has(key))));
}

/**
 * An atom's payload written as the document's projection writes it, its
 * fields in the same order, so an atom changed here reads the same when the
 * document hands it back.
 */
export function nativeAtomPayload(span: TextSpan) {
  return atomPayload(sanitizeTextSpans([span])?.[0] ?? span);
}

/** A date mention atom's attributes as its date window changed them; its stored words stay. */
export function changedDateAtomAttrs(attrs: Attrs, change: DateMentionChange) {
  const payload = readNativeInlineMetadata(attrs.payload);
  payload.date = change.date;
  delete payload.dateStartUnset;
  for (const key of ["dateEnd", "dateTimeZone", "dateReminder", "dateFormat", "dateTimeFormat"] as const) {
    if (change[key]) payload[key] = change[key]; else if (change[key] !== undefined) delete payload[key];
  }
  return { ...attrs, payload: nativeAtomPayload({ ...payload, text: attrs.text }) };
}

function spanMarks(span: TextSpan, schema: Schema, atom = false) {
  const marks: Mark[] = [];
  for (const [key, type] of Object.entries(booleanMarks)) {
    if (span[key as keyof TextSpan] && schema.marks[type]) marks.push(schema.marks[type].create());
  }
  // A text color and a background are two marks, as Notion keeps both.
  const { color, background } = textSpanColors(span);
  if (color) marks.push(schema.marks.nativeColor.create({ stringValue: color }));
  if (background && schema.marks.nativeBackground) marks.push(schema.marks.nativeBackground.create({ stringValue: background }));
  if (span.link) marks.push(schema.marks.link.create({ href: span.link }));
  const rest = atom ? Object.fromEntries(Object.entries(metadata(span)).filter(([key]) => annotationFields.has(key))) : metadata(span);
  if (Object.keys(rest).length) marks.push(schema.marks.nativeAnnotation.create({ stringValue: JSON.stringify(rest) }));
  return marks;
}

/** Preserves native atom payloads and marks; in code, its text stays one run of characters and atoms read as text. */
export function nativeRichTextToFragment(spans: readonly TextSpan[], schema: Schema, inCode = false) {
  const nodes: Node[] = [];
  for (const span of spans) {
    // An empty equation is an atom of no text; an empty text piece shows nothing.
    const atom = !inCode && Boolean(span.mention || span.equation || span.template || span.emptyPiece === "equation");
    if (!span.text && !atom) continue;
    const marks = spanMarks(span, schema, atom);
    if (atom) {
      nodes.push(schema.nodes.nativeAtom.create({ text: span.text, payload: atomPayload(span) }, null, marks));
      continue;
    }
    if (inCode) nodes.push(schema.text(span.text, marks));
    else {
      const lines = span.text.split("\n");
      lines.forEach((line, index) => {
        if (index) nodes.push(schema.nodes.hardBreak.create(null, null, marks));
        if (line) nodes.push(schema.text(line, marks));
      });
    }
  }
  return Fragment.fromArray(nodes);
}

export function readNativeInlineMetadata(value: unknown): Partial<TextSpan> {
  if (typeof value !== "string") return {};
  try {
    const parsed: unknown = JSON.parse(value);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {};
    // Projection payloads cannot override text or formatting owned by PM marks.
    return Object.fromEntries(Object.entries(parsed).filter(([key]) => key !== "text" && !formatting.has(key)));
  } catch { return {}; }
}

function withMarks(span: TextSpan, marks: readonly Mark[]): TextSpan {
  for (const mark of marks) {
    for (const [key, type] of Object.entries(booleanMarks)) {
      if (mark.type.name === type) (span as unknown as Record<string, unknown>)[key] = true;
    }
    if (mark.type.name === "nativeColor") span.color = mark.attrs.stringValue;
    if (mark.type.name === "nativeBackground") span.background = mark.attrs.stringValue;
    if (mark.type.name === "link") span.link = mark.attrs.href;
    if (mark.type.name === "nativeAnnotation") Object.assign(span, readNativeInlineMetadata(mark.attrs.stringValue));
  }
  return span;
}

/** Reads PM nodes directly so native atoms retain their formatting marks. */
export function fragmentToNativeRichText(fragment: Fragment): TextSpan[] {
  const spans: TextSpan[] = [];
  fragment.forEach((node) => {
    let span: TextSpan;
    if (node.isText) span = { text: node.text! };
    else if (node.type.name === "hardBreak") span = { text: "\n" };
    else if (node.type.name === "nativeAtom") span = { ...readNativeInlineMetadata(node.attrs.payload), text: node.attrs.text };
    else throw new Error(`Unsupported native inline projection: ${node.type.name}`);
    withMarks(span, node.marks);
    const previous = spans.at(-1);
    if (previous && !previous.mention && !previous.equation && !previous.template && !previous.emptyPiece
      && JSON.stringify({ ...previous, text: "" }) === JSON.stringify({ ...span, text: "" })) previous.text += span.text;
    else spans.push(span);
  });
  return spans;
}

/** Native offsets count an atom's displayed text; PM counts its single node. */
export function nativeOffsetAt(fragment: Fragment, pmOffset: number) {
  let offset = 0;
  fragment.forEach((node, position) => {
    if (position >= pmOffset) return;
    const length = node.type.name === "nativeAtom" ? String(node.attrs.text).length : node.nodeSize;
    offset += node.type.name === "nativeAtom" ? length : Math.min(node.nodeSize, pmOffset - position);
  });
  return offset;
}

export function pmOffsetAt(fragment: Fragment, nativeOffset: number, association = 1) {
  let nativePosition = 0, result = fragment.size;
  let found = false;
  fragment.forEach((node, position) => {
    if (found) return;
    const atom = node.type.name === "nativeAtom";
    const length = atom ? String(node.attrs.text).length : node.nodeSize;
    if (nativeOffset <= nativePosition + length) {
      const within = Math.max(0, nativeOffset - nativePosition);
      result = position + (atom ? within === 0 ? 0 : within === length || association > 0 ? 1 : 0 : within);
      found = true;
    }
    nativePosition += length;
  });
  return result;
}

function emptyPieceAtom(node: Node) {
  return node.type.name === "nativeAtom" && !node.attrs.text && readNativeInlineMetadata(node.attrs.payload).emptyPiece !== undefined;
}

/**
 * How many empty pieces stand between the last character before a PM offset
 * and it: each takes a position in the block's text (an embed) but none in
 * its native offsets, so a point after one is told from one before it.
 */
export function emptyPiecesBefore(fragment: Fragment, pmOffset: number) {
  let count = 0;
  fragment.forEach((node, position) => {
    if (position + node.nodeSize > pmOffset) return;
    count = emptyPieceAtom(node) ? count + 1 : node.isText && !node.text ? count : 0;
  });
  return count;
}

/** A PM offset moved past the empty pieces that follow it, as many as asked. */
export function pmOffsetPastEmptyPieces(fragment: Fragment, pmOffset: number, count: number) {
  let result = pmOffset;
  fragment.forEach((node, position) => {
    if (count > 0 && position === result && emptyPieceAtom(node)) { result += node.nodeSize; count -= 1; }
  });
  return result;
}
