import type { TextSpan } from "@/lib/types";
import { TEXT_SPAN_FIELDS } from "@/lib/modelTypes/content";
import { textSpanColors } from "@/lib/textSpanColors";
import { formulaCharacters, formulaPadding } from "./formulaCharacters";
import { formulaDateText } from "./formulaDateText";
import type { FormulaViewer } from "./formulaLocales";
import { FormulaDate, FormulaRichText, MAX_FORMULA_VALUE_TEXT, type FormulaValue } from "./formulaSyntax";
import { formulaReferenceText, isFormulaReference, richShown, toText } from "./formulaValues";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";

// A formula's rich text is text with formatting, links and mentions, as
// Notion's formulas write it: style() and link() format text, and a date,
// page or person joined to text is a mention of it. Its spans are those a
// title or text cell keeps: a date or person mention's text starts with "@",
// a page mention's is the page's title. Text without formatting, links or
// mentions stays plain text. The server writes rich text the same way
// (database_formula_rich_text.go).

const STYLE_COLORS = new Set(["gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red"]);
const SPAN_FIELDS = TEXT_SPAN_FIELDS;

// A span with only the fields it sets, as the server writes spans.
function cleanSpan(span: TextSpan): TextSpan {
  const clean: Record<string, unknown> = { text: span.text };
  for (const field of SPAN_FIELDS) {
    if (span[field]) clean[field] = span[field];
  }
  return clean as unknown as TextSpan;
}

function sameMarks(left: TextSpan, right: TextSpan) {
  return SPAN_FIELDS.every((field) => (left[field] || "") === (right[field] || ""));
}

// formulaSpans is a value as rich text spans: a date, page or person as a
// mention, a list's items between commas, anything else as its text.
export function formulaSpans(value: FormulaValue | undefined, viewer: FormulaViewer): TextSpan[] {
  if (value instanceof FormulaRichText) return value.spans;
  if (value instanceof FormulaDate) {
    const split = value.text.indexOf("/");
    const end = split < 0 ? "" : value.text.slice(split + 1);
    const span: TextSpan = {
      text: `@${formulaDateText(value, viewer)}`, mention: "date", date: split < 0 ? value.text : value.text.slice(0, split),
      ...(value.zone ? { dateTimeZone: value.zone } : {}),
    };
    return [end ? { ...span, dateEnd: end } : span];
  }
  if (isFormulaReference(value)) {
    return value.kind === "person"
      ? [{ text: `@${formulaReferenceText(value)}`, mention: "person", userId: value.id }]
      // An untitled page's mention reads the viewer's word for one, as Notion's ("새 페이지", seen 2026-11-12).
      : [{ text: value.name || untitledPageDisplayTitle("page"), mention: "page", pageId: value.id }];
  }
  if (Array.isArray(value)) {
    return value.flatMap((item, index) => [...(index > 0 ? [{ text: "," }] : []), ...formulaSpans(item, viewer)]);
  }
  return [{ text: toText(value) }];
}

// richValue is spans as a value: rich text when a span is formatted, a link
// or a mention, else its plain text. Neighbouring text spans alike are one
// span, and empty text spans are left out.
export function richValue(spans: readonly TextSpan[]): FormulaValue {
  const merged: TextSpan[] = [];
  let rich = false;
  for (const raw of spans) {
    const span = cleanSpan(raw);
    if (!span.mention && span.text === "") continue;
    const last = merged[merged.length - 1];
    if (!span.mention && last && !last.mention && sameMarks(last, span)) {
      merged[merged.length - 1] = { ...last, text: last.text + span.text };
      continue;
    }
    merged.push(span);
    rich ||= Object.keys(span).length > 1;
  }
  return rich ? new FormulaRichText(merged) : merged.map((span) => span.text).join("");
}

// joinedValue is values joined as text, dates, pages and people as mentions.
export function joinedValue(values: readonly (FormulaValue | undefined)[], separator: string, viewer: FormulaViewer) {
  return richValue(values.flatMap((value, index) => [
    ...(index > 0 && separator ? [{ text: separator }] : []), ...formulaSpans(value, viewer),
  ]));
}

// length counts a mention as one character.
export function richLength(value: FormulaRichText) {
  let length = 0;
  let run = "";
  for (const span of value.spans) {
    if (span.mention) {
      length += formulaCharacters(run).length + 1;
      run = "";
    } else {
      run += span.text;
    }
  }
  return length + formulaCharacters(run).length;
}

// mapRichText writes each text span's text anew; mentions stay.
export function mapRichText(value: FormulaRichText, write: (text: string) => string) {
  return richValue(value.spans.map((span) => (span.mention ? span : { ...span, text: write(span.text) })));
}

// styleValue formats a value: b, i, u, s and c for bold, italic, underline,
// strikethrough and code, and a color or a color's background; other names
// are passed over, and a later color wins.
export function styleValue(args: readonly FormulaValue[], viewer: FormulaViewer): FormulaValue {
  if (!args.length) return "";
  const spans = formulaSpans(args[0], viewer).map((span) => ({ ...span }));
  for (const value of args.slice(1)) {
    const name = typeof value === "string" ? value : "";
    for (const span of spans) {
      if (name === "b") span.bold = true;
      else if (name === "i") span.italic = true;
      else if (name === "u") span.underline = true;
      else if (name === "s") span.strikethrough = true;
      else if (name === "c") span.code = true;
      // A text color and a background go together, as in Notion.
      else if (name.endsWith("_background") && STYLE_COLORS.has(name.slice(0, -"_background".length))) {
        Object.assign(span, { color: textSpanColors(span).color ?? "", background: name });
      } else if (STYLE_COLORS.has(name)) Object.assign(span, { background: textSpanColors(span).background ?? "", color: name });
    }
  }
  return richValue(spans);
}

// unstyleValue takes the named formatting away, or all of it when none is
// named; links and mentions stay.
export function unstyleValue(args: readonly FormulaValue[], viewer: FormulaViewer): FormulaValue {
  if (!args.length) return "";
  const spans = formulaSpans(args[0], viewer).map((span) => {
    const next = { ...span };
    if (args.length === 1) {
      next.bold = next.italic = next.underline = next.strikethrough = next.code = false;
      next.color = "";
      next.background = "";
      return next;
    }
    for (const value of args.slice(1)) {
      const name = typeof value === "string" ? value : "";
      if (name === "b") next.bold = false;
      else if (name === "i") next.italic = false;
      else if (name === "u") next.underline = false;
      else if (name === "s") next.strikethrough = false;
      else if (name === "c") next.code = false;
      else if (name === next.color) next.color = "";
      else if (name === next.background) next.background = "";
    }
    return next;
  });
  return richValue(spans);
}

// linkValue makes a value's text a link; mentions stay what they are.
export function linkValue(label: FormulaValue | undefined, url: FormulaValue | undefined, viewer: FormulaViewer) {
  const address = toText(url ?? null);
  return richValue(formulaSpans(label, viewer).map((span) => (span.mention || !address ? span : { ...span, link: address })));
}

// trimRichText takes white space from the ends of rich text's outer text
// spans.
export function trimRichText(value: FormulaRichText) {
  const spans = value.spans.map((span) => ({ ...span }));
  if (spans.length && !spans[0].mention) spans[0].text = spans[0].text.trimStart();
  const last = spans[spans.length - 1];
  if (last && !last.mention) last.text = last.text.trimEnd();
  return richValue(spans);
}

// repeatRichText writes rich text count times, whole: when the copies' text
// would pass a value's, the formula fails before they are written.
export function repeatRichText(value: FormulaRichText, count: number) {
  const units = value.spans.reduce((sum, span) => sum + span.text.length + (span.link?.length ?? 0), 0);
  if (units * count > MAX_FORMULA_VALUE_TEXT) throw new Error("Formula text is too long");
  const spans: TextSpan[] = [];
  for (let round = 0; round < count; round += 1) spans.push(...value.spans);
  return richValue(spans);
}

// padRichText pads rich text to the length of the text it shows, keeping
// its spans.
export function padRichText(value: FormulaRichText, target: number, fill: string, start: boolean): FormulaValue {
  const missing = target - formulaCharacters(richShown(value)).length;
  if (missing <= 0 || !fill) return value;
  const padding = formulaPadding(missing, fill);
  return richValue(start ? [{ text: padding }, ...value.spans] : [...value.spans, { text: padding }]);
}
