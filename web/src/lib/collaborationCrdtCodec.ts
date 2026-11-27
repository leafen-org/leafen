import { spansToPlainText, type TextSpan } from "./types";
import { sanitizeTextSpans } from "./textOperations";

export function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  const chunkSize = 0x8000;
  for (let offset = 0; offset < bytes.length; offset += chunkSize) {
    const chunk = bytes.subarray(offset, offset + chunkSize);
    binary += String.fromCharCode(...chunk);
  }
  return btoa(binary);
}

export function spanToYTextAttributes(span: TextSpan): Record<string, string | boolean> | undefined {
  const attributes: Record<string, string | boolean> = {};
  if (span.bold) attributes.bold = true;
  if (span.italic) attributes.italic = true;
  if (span.underline) attributes.underline = true;
  if (span.strikethrough) attributes.strikethrough = true;
  if (span.code) attributes.code = true;
  if (span.color) attributes.color = span.color;
  if (span.background) attributes.background = span.background;
  if (span.link) attributes.link = span.link;
  if (span.commentId) attributes.commentId = span.commentId;
  if (span.mention) attributes.mention = span.mention;
  if (span.pageId) attributes.pageId = span.pageId;
  if (span.blockId) attributes.blockId = span.blockId;
  if (span.databaseId) attributes.databaseId = span.databaseId;
  if (span.customEmojiId) attributes.customEmojiId = span.customEmojiId;
  if (span.date) attributes.date = span.date;
  if (span.dateStartUnset) attributes.dateStartUnset = true;
  if (span.dateEnd) attributes.dateEnd = span.dateEnd;
  if (span.dateTimeZone) attributes.dateTimeZone = span.dateTimeZone;
  if (span.dateReminder) attributes.dateReminder = span.dateReminder;
  if (span.dateFormat) attributes.dateFormat = span.dateFormat;
  if (span.dateTimeFormat) attributes.dateTimeFormat = span.dateTimeFormat;
  if (span.userId) attributes.userId = span.userId;
  if (span.iconUrl) attributes.iconUrl = span.iconUrl;
  if (span.provider) attributes.provider = span.provider;
  if (span.description) attributes.description = span.description;
  if (span.imageUrl) attributes.imageUrl = span.imageUrl;
  if (span.equation) attributes.equation = span.equation;
  if (span.template) attributes.template = span.template;
  if (span.suggestion) attributes.suggestion = span.suggestion;
  if (span.suggestionId) attributes.suggestionId = span.suggestionId;
  if (span.suggestionBefore) attributes.suggestionBefore = span.suggestionBefore;
  return Object.keys(attributes).length > 0 ? attributes : undefined;
}

// A piece with no characters — an inline equation with no expression, a text
// piece with no words — stands in a block's collaborative text as an embed,
// {equation: ""} or {text: ""}, at its place, with its formatting, as the
// server writes it (graph_text_empty_pieces.go). An embed takes one position
// in the text and none in its plain text, so offsets in the plain text
// (textIndexAt, textOffsetAt) and positions in the text part where one stands.
type EmptyPiece = NonNullable<TextSpan["emptyPiece"]>;

export function emptyPieceKind(insert: unknown): EmptyPiece | undefined {
  if (!insert || typeof insert !== "object" || Array.isArray(insert)) return undefined;
  const embed = insert as Record<string, unknown>;
  if (Object.keys(embed).length !== 1) return undefined;
  if (embed.equation === "") return "equation";
  if (embed.text === "") return "text";
  return undefined;
}

function emptyPieceEmbed(kind: EmptyPiece) {
  return kind === "equation" ? { equation: "" } : { text: "" };
}

/** Positions a span takes in a block's collaborative text. */
function spanLength(span: TextSpan) {
  return span.emptyPiece ? 1 : span.text.length;
}

/** Whether a text holds an embed, which takes a position its plain text hasn't. */
export function textHoldsEmbeds(text: import("yjs").Text) {
  return text.length !== text.toString().length;
}

/**
 * The position in a text of an offset in its plain text, past `embeds` of
 * the embeds that stand there; null past its end.
 */
export function textIndexAt(text: import("yjs").Text, offset: number, embeds = 0): number | null {
  if (!textHoldsEmbeds(text)) return offset <= text.length ? offset : null;
  let characters = 0, index = 0, passed = 0;
  for (const delta of text.toDelta() as Array<{ insert: unknown }>) {
    if (typeof delta.insert !== "string") {
      if (characters === offset) {
        if (passed >= embeds) return index;
        passed += 1;
      }
      index += 1;
      continue;
    }
    const end = characters + delta.insert.length;
    if (offset < end || offset === end && embeds === 0) return index + offset - characters;
    characters = end; index += delta.insert.length;
  }
  return offset === characters ? index : null;
}

/**
 * The offset in a text's plain text of a position in it, and how many
 * embeds stand between that offset and the position.
 */
export function textOffsetAt(text: import("yjs").Text, index: number): { offset: number; embeds: number } {
  if (!textHoldsEmbeds(text)) return { offset: index, embeds: 0 };
  let offset = 0, embeds = 0, at = 0;
  for (const delta of text.toDelta() as Array<{ insert: unknown }>) {
    if (at >= index) break;
    if (typeof delta.insert === "string") {
      const taken = Math.min(delta.insert.length, index - at);
      if (taken > 0) embeds = 0;
      offset += taken; at += taken;
    } else {
      embeds += 1; at += 1;
    }
  }
  return { offset, embeds };
}

export function yTextToRichText(text: import("yjs").Text): TextSpan[] | undefined {
  const spans: TextSpan[] = [];
  for (const delta of text.toDelta()) {
    const emptyPiece = typeof delta.insert === "string" ? undefined : emptyPieceKind(delta.insert);
    if (typeof delta.insert === "string" ? delta.insert.length === 0 : !emptyPiece) continue;
    const attributes =
      delta.attributes && typeof delta.attributes === "object"
        ? (delta.attributes as Record<string, unknown>)
        : {};
    const span: TextSpan = { text: typeof delta.insert === "string" ? delta.insert : "" };
    if (emptyPiece) span.emptyPiece = emptyPiece;
    if (attributes.bold === true) span.bold = true;
    if (attributes.italic === true) span.italic = true;
    if (attributes.underline === true) span.underline = true;
    if (attributes.strikethrough === true) span.strikethrough = true;
    if (attributes.code === true) span.code = true;
    if (typeof attributes.color === "string") span.color = attributes.color;
    if (typeof attributes.background === "string") span.background = attributes.background;
    if (typeof attributes.link === "string") span.link = attributes.link;
    if (typeof attributes.commentId === "string") span.commentId = attributes.commentId;
    if (
      attributes.mention === "page" ||
      attributes.mention === "database" ||
      attributes.mention === "custom_emoji" ||
      attributes.mention === "date" ||
      attributes.mention === "person" ||
      attributes.mention === "external" ||
      attributes.mention === "template"
    ) {
      span.mention = attributes.mention;
    }
    if (typeof attributes.pageId === "string") span.pageId = attributes.pageId;
    if (typeof attributes.blockId === "string" && span.mention === "page") span.blockId = attributes.blockId;
    if (typeof attributes.databaseId === "string") span.databaseId = attributes.databaseId;
    if (typeof attributes.customEmojiId === "string") span.customEmojiId = attributes.customEmojiId;
    if (typeof attributes.date === "string") span.date = attributes.date;
    if (attributes.dateStartUnset === true) span.dateStartUnset = true;
    if (typeof attributes.dateEnd === "string") span.dateEnd = attributes.dateEnd;
    if (typeof attributes.dateTimeZone === "string") span.dateTimeZone = attributes.dateTimeZone;
    if (typeof attributes.dateReminder === "string") span.dateReminder = attributes.dateReminder;
    if (typeof attributes.dateFormat === "string") span.dateFormat = attributes.dateFormat;
    if (typeof attributes.dateTimeFormat === "string") span.dateTimeFormat = attributes.dateTimeFormat;
    if (typeof attributes.userId === "string") span.userId = attributes.userId;
    if (typeof attributes.iconUrl === "string") span.iconUrl = attributes.iconUrl;
    if (typeof attributes.provider === "string") span.provider = attributes.provider;
    if (typeof attributes.description === "string") span.description = attributes.description;
    if (typeof attributes.imageUrl === "string") span.imageUrl = attributes.imageUrl;
    if (typeof attributes.equation === "string") span.equation = attributes.equation;
    if (attributes.template === "today" || attributes.template === "now" || attributes.template === "me") {
      span.template = attributes.template;
    }
    if (attributes.suggestion === "insert" || attributes.suggestion === "delete" || attributes.suggestion === "format") {
      span.suggestion = attributes.suggestion;
    }
    if (typeof attributes.suggestionId === "string") span.suggestionId = attributes.suggestionId;
    if (typeof attributes.suggestionBefore === "string") span.suggestionBefore = attributes.suggestionBefore;
    spans.push(span);
  }
  return sanitizeTextSpans(spans);
}

const BLOCK_TEXT_ATTR_KEYS = [
  "bold",
  "italic",
  "underline",
  "strikethrough",
  "code",
  "color",
  "background",
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
  "template",
  "suggestion",
  "suggestionId",
  "suggestionBefore",
  "piece",
] as const;

// Two neighbouring pieces that read alike — two mentions of the same page with
// the same marks, two equal equations — stay two, as Notion keeps them. The
// collaborative text joins neighbouring runs whose formatting is the same, so
// every other piece of such a row carries a "piece" mark, which readers leave
// out; it is given again from where the pieces stand on every write, the
// same way the server gives it (graph_text_pieces.go).
function staysApart(previous: TextSpan | undefined, span: TextSpan) {
  if (!previous || !(span.mention || span.equation || span.template)) return false;
  const left = spanToYTextAttributes(previous) ?? {};
  const right = spanToYTextAttributes(span) ?? {};
  const keys = Object.keys(left);
  return keys.length === Object.keys(right).length && keys.every((key) => left[key] === right[key]);
}

// Reconcile a Y.Text to `rich` WITHOUT deleting and re-inserting everything.
// A full replace re-creates every character under the writer's clientID, so two
// clients editing the same block never converge — Yjs keeps both full inserts.
// Instead: (1) a minimal prefix/suffix plain-text diff preserves the identity of
// unchanged text, and (2) each run is formatted to its exact target attributes
// (absent marks explicitly cleared). This makes concurrent edits converge
// character-by-character and keeps payloads proportional to the actual change.
export function writeRichTextToYText(text: import("yjs").Text, rich: TextSpan[]) {
  const current = text.toString();
  if (text.length !== current.length || rich.some((span) => span.emptyPiece)) {
    writeUnitsToYText(text, rich);
    formatRichTextInYText(text, rich);
    return;
  }
  const next = spansToPlainText(rich);
  if (current !== next) {
    let prefix = 0;
    const maxPrefix = Math.min(current.length, next.length);
    while (prefix < maxPrefix && current[prefix] === next[prefix]) prefix += 1;
    let suffix = 0;
    const maxSuffix = Math.min(current.length - prefix, next.length - prefix);
    while (
      suffix < maxSuffix &&
      current[current.length - 1 - suffix] === next[next.length - 1 - suffix]
    ) {
      suffix += 1;
    }
    const deleteCount = current.length - prefix - suffix;
    const insert = next.slice(prefix, next.length - suffix);
    if (deleteCount > 0) text.delete(prefix, deleteCount);
    if (insert) text.insert(prefix, insert);
  }
  formatRichTextInYText(text, rich);
}

// One position of a text: a character, or an empty piece's embed.
type TextUnit = string | { emptyPiece: EmptyPiece };

function sameUnit(left: TextUnit, right: TextUnit) {
  return typeof left === "string" || typeof right === "string" ? left === right : left.emptyPiece === right.emptyPiece;
}

/**
 * writeRichTextToYText's minimal change for a text that holds, or is to
 * hold, an embed: the same prefix/suffix diff, by positions, an empty piece
 * one; any other embed counts as one that differs.
 */
function writeUnitsToYText(text: import("yjs").Text, rich: TextSpan[]) {
  const current: TextUnit[] = [];
  for (const delta of text.toDelta() as Array<{ insert: unknown }>) {
    if (typeof delta.insert === "string") current.push(...delta.insert.split(""));
    else current.push({ emptyPiece: emptyPieceKind(delta.insert) ?? ("" as EmptyPiece) });
  }
  const next: TextUnit[] = [];
  for (const span of rich) {
    if (span.emptyPiece) next.push({ emptyPiece: span.emptyPiece });
    else next.push(...span.text.split(""));
  }
  let prefix = 0;
  const maxPrefix = Math.min(current.length, next.length);
  while (prefix < maxPrefix && sameUnit(current[prefix], next[prefix])) prefix += 1;
  let suffix = 0;
  const maxSuffix = Math.min(current.length - prefix, next.length - prefix);
  while (suffix < maxSuffix && sameUnit(current[current.length - 1 - suffix], next[next.length - 1 - suffix])) suffix += 1;
  const deleteCount = current.length - prefix - suffix;
  if (deleteCount > 0) text.delete(prefix, deleteCount);
  let at = prefix, words = "";
  const flush = () => { if (words) { text.insert(at, words); at += words.length; words = ""; } };
  for (const unit of next.slice(prefix, next.length - suffix)) {
    if (typeof unit === "string") { words += unit; continue; }
    flush();
    text.insertEmbed(at, emptyPieceEmbed(unit.emptyPiece), {});
    at += 1;
  }
  flush();
}

// Each run is formatted to its exact target attributes, absent marks cleared;
// an empty piece takes its one position and parts the pieces around it.
function formatRichTextInYText(text: import("yjs").Text, rich: TextSpan[]) {
  let index = 0;
  let previous: TextSpan | undefined;
  let apart = false;
  for (const span of rich) {
    const length = spanLength(span);
    if (!length) continue;
    if (span.emptyPiece) {
      apart = false;
      previous = undefined;
    } else {
      apart = staysApart(previous, span) ? !apart : false;
      previous = span;
    }
    const want: Record<string, string | boolean> = { ...spanToYTextAttributes(span), ...(apart ? { piece: true } : {}) };
    const format: Record<string, string | boolean | null> = {};
    for (const key of BLOCK_TEXT_ATTR_KEYS) {
      format[key] = key in want ? want[key] : null;
    }
    text.format(index, length, format);
    index += length;
  }
}
