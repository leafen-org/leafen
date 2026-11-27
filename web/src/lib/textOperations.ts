import { ONE_CHANGE_CHARACTERS } from "./oneChange";
import { spansToPlainText, type TextSpan } from "./types";

const TEXT_OPERATION_CONTEXT_CHARS = 24;
// One block's text in an operation goes as far as one change holds; the
// editor's own view of a block is never cut short (a cut view would save
// without its end).
const MAX_TEXT_OPERATION_TOTAL_CHARS = ONE_CHANGE_CHARACTERS;

const MARK_KEYS: (keyof TextSpan)[] = [
  "bold",
  "italic",
  "underline",
  "strikethrough",
  "code",
  "color",
  "link",
  "commentId",
  "mention",
  "pageId",
  "blockId",
  "databaseId",
  "customEmojiId",
  "date",
  "dateStartUnset",
  "dateEnd",
  "dateTimeZone",
  "dateReminder",
  "dateFormat",
  "dateTimeFormat",
  "userId",
  "iconUrl",
  "provider",
  "description",
  "imageUrl",
  "equation",
  "suggestion",
  "suggestionId",
  "suggestionBefore",
];

export interface TextSpanOperation {
  afterText: string;
  beforeText: string;
  deleteCount: number;
  deletedText: string;
  insert: TextSpan[];
  prefixContext: string;
  start: number;
  suffixContext: string;
}

function assertTextSpanCollectionBound(spans: readonly TextSpan[], label: string) {
  let characters = 0;
  for (const span of spans) characters += span.text.length;
  if (characters > MAX_TEXT_OPERATION_TOTAL_CHARS) {
    throw new Error(`${label} exceed their authoritative character bound.`);
  }
}

function assertTextOperationStringBound(value: string, label: string) {
  if (value.length > MAX_TEXT_OPERATION_TOTAL_CHARS) {
    throw new Error(`${label} exceeds its authoritative character bound.`);
  }
}

function assertTextOperationBound(operation: TextSpanOperation) {
  if (
    operation.start > MAX_TEXT_OPERATION_TOTAL_CHARS
    || operation.deleteCount > MAX_TEXT_OPERATION_TOTAL_CHARS
  ) {
    throw new Error("Text operation offsets exceed their authoritative bound.");
  }
  assertTextOperationStringBound(operation.beforeText, "before text");
  assertTextOperationStringBound(operation.afterText, "after text");
  assertTextOperationStringBound(operation.deletedText, "deleted text");
  assertTextSpanCollectionBound(operation.insert, "insert spans");
  if (
    operation.prefixContext.length > TEXT_OPERATION_CONTEXT_CHARS
    || operation.suffixContext.length > TEXT_OPERATION_CONTEXT_CHARS
  ) {
    throw new Error("Text operation context exceeds its authoritative bound.");
  }
}

function sameMarks(a: TextSpan, b: TextSpan): boolean {
  return MARK_KEYS.every((key) => a[key] === b[key]);
}

/** A mention or an equation is a piece of its own: it never joins a neighbour that reads alike. */
function atomic(span: TextSpan): boolean {
  return Boolean(span.mention || span.equation || span.template || span.emptyPiece);
}

function coalesce(spans: TextSpan[]): TextSpan[] {
  const out: TextSpan[] = [];
  for (const span of spans) {
    // An empty piece stays, as one with no characters (TextSpan.emptyPiece).
    if (span.text === "" && !span.emptyPiece) continue;
    const last = out[out.length - 1];
    if (last && !atomic(last) && !atomic(span) && sameMarks(last, span)) last.text += span.text;
    else out.push({ ...span });
  }
  return out;
}

/** Where the mention or equation an offset falls inside begins, or the offset. */
function pieceStartAt(spans: readonly TextSpan[], offset: number): number {
  let at = 0;
  for (const span of spans) {
    const end = at + span.text.length;
    if (offset < end) return at < offset && atomic(span) ? at : offset;
    at = end;
  }
  return offset;
}

/** Where the mention or equation an offset falls inside ends, or the offset. */
function pieceEndAt(spans: readonly TextSpan[], offset: number): number {
  let at = 0;
  for (const span of spans) {
    const end = at + span.text.length;
    if (offset < end) return at < offset && atomic(span) ? end : offset;
    at = end;
  }
  return offset;
}

function splitSpans(spans: TextSpan[], offset: number): [TextSpan[], TextSpan[]] {
  const before: TextSpan[] = [];
  const after: TextSpan[] = [];
  let acc = 0;
  for (const span of spans) {
    const len = span.text.length;
    if (acc >= offset) {
      after.push(span);
    } else if (acc + len <= offset) {
      before.push(span);
    } else {
      const cut = offset - acc;
      before.push({ ...span, text: span.text.slice(0, cut) });
      after.push({ ...span, text: span.text.slice(cut) });
    }
    acc += len;
  }
  return [coalesce(before), coalesce(after)];
}

function concatSpans(...parts: TextSpan[][]): TextSpan[] {
  return coalesce(parts.flat());
}

export function sanitizeTextSpans(value: unknown): TextSpan[] | undefined {
  if (!Array.isArray(value)) return undefined;
  // Every run, whole: a block's text is never cut short here.
  return value
    .map((item) => {
      if (!item || typeof item !== "object") return null;
      const source = item as Record<string, unknown>;
      if (typeof source.text !== "string") return null;
      const span: TextSpan = { text: source.text };
      if (source.bold === true) span.bold = true;
      if (source.italic === true) span.italic = true;
      if (source.underline === true) span.underline = true;
      if (source.strikethrough === true) span.strikethrough = true;
      if (source.code === true) span.code = true;
      if (typeof source.color === "string") span.color = source.color;
      if (typeof source.background === "string") span.background = source.background;
      if (typeof source.link === "string") span.link = source.link;
      if (typeof source.commentId === "string") span.commentId = source.commentId;
      if (
        source.mention === "page" ||
        source.mention === "database" ||
        source.mention === "custom_emoji" ||
        source.mention === "date" ||
        source.mention === "person" ||
        source.mention === "external" ||
        source.mention === "template" ||
        source.mention === "automation"
      ) {
        span.mention = source.mention;
      }
      if (typeof source.pageId === "string") span.pageId = source.pageId;
      if (typeof source.blockId === "string" && source.mention === "page") span.blockId = source.blockId;
      if (typeof source.databaseId === "string") span.databaseId = source.databaseId;
      if (typeof source.customEmojiId === "string") span.customEmojiId = source.customEmojiId;
      if (typeof source.date === "string") span.date = source.date;
      if (source.dateStartUnset === true && source.mention === "date") span.dateStartUnset = true;
      if (typeof source.dateEnd === "string") span.dateEnd = source.dateEnd;
      if (typeof source.dateTimeZone === "string") span.dateTimeZone = source.dateTimeZone;
      if (typeof source.dateReminder === "string") span.dateReminder = source.dateReminder;
      if (typeof source.dateFormat === "string") span.dateFormat = source.dateFormat;
      if (typeof source.dateTimeFormat === "string") span.dateTimeFormat = source.dateTimeFormat;
      if (typeof source.userId === "string") span.userId = source.userId;
      if (typeof source.iconUrl === "string") span.iconUrl = source.iconUrl;
      if (source.mention === "external") {
        if (typeof source.provider === "string") span.provider = source.provider;
        if (typeof source.description === "string") span.description = source.description;
        if (typeof source.imageUrl === "string") span.imageUrl = source.imageUrl;
      }
      if (typeof source.equation === "string") span.equation = source.equation;
      if ((source.emptyPiece === "equation" || source.emptyPiece === "text") && source.text === "") span.emptyPiece = source.emptyPiece;
      if (typeof source.automation === "string") span.automation = source.automation;
      if (typeof source.automationType === "string") span.automationType = source.automationType;
      if (source.template === "today" || source.template === "now" || source.template === "me") {
        span.template = source.template;
      }
      if (source.suggestion === "insert" || source.suggestion === "delete" || source.suggestion === "format") {
        span.suggestion = source.suggestion;
      }
      if (typeof source.suggestionId === "string") span.suggestionId = source.suggestionId;
      if (typeof source.suggestionBefore === "string") span.suggestionBefore = source.suggestionBefore;
      return span;
    })
    .filter((item): item is TextSpan => !!item);
}

export function createTextOperation(
  beforeSpans: TextSpan[],
  afterSpans: TextSpan[]
): TextSpanOperation | undefined {
  assertTextSpanCollectionBound(beforeSpans, "before spans");
  assertTextSpanCollectionBound(afterSpans, "after spans");
  const beforeText = spansToPlainText(beforeSpans);
  const afterText = spansToPlainText(afterSpans);
  if (beforeText === afterText) return undefined;

  let start = 0;
  while (
    start < beforeText.length &&
    start < afterText.length &&
    beforeText[start] === afterText[start]
  ) {
    start += 1;
  }

  let suffix = 0;
  while (
    suffix < beforeText.length - start &&
    suffix < afterText.length - start &&
    beforeText[beforeText.length - 1 - suffix] === afterText[afterText.length - 1 - suffix]
  ) {
    suffix += 1;
  }

  // A change never begins or ends inside a mention or an equation, which
  // would leave halves of it as two pieces.
  for (;;) {
    const snappedStart = Math.min(pieceStartAt(beforeSpans, start), pieceStartAt(afterSpans, start));
    const snappedSuffix = Math.min(
      beforeText.length - pieceEndAt(beforeSpans, beforeText.length - suffix),
      afterText.length - pieceEndAt(afterSpans, afterText.length - suffix),
    );
    if (snappedStart === start && snappedSuffix === suffix) break;
    start = snappedStart;
    suffix = snappedSuffix;
  }

  const deleteCount = beforeText.length - start - suffix;
  const insertLength = afterText.length - start - suffix;
  const [, afterStart] = splitSpans(afterSpans, start);
  const [insert] = splitSpans(afterStart, insertLength);

  return {
    afterText,
    beforeText,
    deleteCount,
    deletedText: beforeText.slice(start, start + deleteCount),
    insert,
    prefixContext: beforeText.slice(Math.max(0, start - TEXT_OPERATION_CONTEXT_CHARS), start),
    start,
    suffixContext: beforeText.slice(
      start + deleteCount,
      start + deleteCount + TEXT_OPERATION_CONTEXT_CHARS
    ),
  };
}

// Others' writing is the one run where the current text differs from the base.
// A change clear of that run keeps its place, past the run when after it, as
// the server's graph merge does (content_graph_rich_merge.go).
function startClearOfPeers(currentText: string, operation: TextSpanOperation): number | null {
  const base = operation.beforeText;
  let same = 0;
  while (same < base.length && same < currentText.length && base[same] === currentText[same]) same += 1;
  let sameEnd = 0;
  while (
    sameEnd < base.length - same &&
    sameEnd < currentText.length - same &&
    base[base.length - 1 - sameEnd] === currentText[currentText.length - 1 - sameEnd]
  ) {
    sameEnd += 1;
  }
  // Where the change sits both at the end of the untouched start and at the
  // start of the untouched end — a peer's text added right there — it keeps
  // to the start's side: a Redo puts its text back before what a peer added
  // after it ("Caption local peer", not "Caption peer local").
  if (operation.start + operation.deleteCount <= same) return operation.start;
  return operation.start >= base.length - sameEnd ? operation.start + currentText.length - base.length : null;
}

function findAnchoredStart(currentText: string, operation: TextSpanOperation): number | null {
  if (currentText === operation.beforeText) return operation.start;
  if (operation.beforeText) {
    const clear = startClearOfPeers(currentText, operation);
    if (clear !== null) return clear;
  }

  const exactContext = `${operation.prefixContext}${operation.deletedText}${operation.suffixContext}`;
  if (exactContext) {
    const index = currentText.indexOf(exactContext);
    if (index >= 0 && currentText.indexOf(exactContext, index + 1) < 0) {
      return index + operation.prefixContext.length;
    }
  }

  if (operation.deleteCount === 0) {
    const insertAnchor = `${operation.prefixContext}${operation.suffixContext}`;
    if (insertAnchor) {
      const index = currentText.indexOf(insertAnchor);
      if (index >= 0 && currentText.indexOf(insertAnchor, index + 1) < 0) {
        return index + operation.prefixContext.length;
      }
    }
  }

  if (operation.prefixContext && operation.suffixContext) {
    const prefixIndex = currentText.indexOf(operation.prefixContext);
    if (prefixIndex >= 0 && currentText.indexOf(operation.prefixContext, prefixIndex + 1) < 0) {
      const start = prefixIndex + operation.prefixContext.length;
      const suffixIndex = currentText.indexOf(operation.suffixContext, start);
      if (suffixIndex >= 0) return start;
    }
  }

  if (operation.prefixContext) {
    const index = currentText.indexOf(operation.prefixContext);
    if (index >= 0 && currentText.indexOf(operation.prefixContext, index + 1) < 0) {
      return index + operation.prefixContext.length;
    }
  }

  if (operation.suffixContext) {
    const index = currentText.indexOf(operation.suffixContext);
    if (index >= 0 && currentText.indexOf(operation.suffixContext, index + 1) < 0) {
      return index;
    }
  }

  return null;
}

export function applyTextOperationToSpans(
  currentSpans: TextSpan[],
  operation: TextSpanOperation
): TextSpan[] | null {
  assertTextSpanCollectionBound(currentSpans, "current spans");
  assertTextOperationBound(operation);
  const currentText = spansToPlainText(currentSpans);
  const start = findAnchoredStart(currentText, operation);
  if (start === null) return null;
  if (currentText.slice(start, start + operation.deleteCount) !== operation.deletedText) {
    return null;
  }

  const [head, deleteAndTail] = splitSpans(currentSpans, start);
  const [, tail] = splitSpans(deleteAndTail, operation.deleteCount);
  return concatSpans(head, operation.insert, tail);
}
