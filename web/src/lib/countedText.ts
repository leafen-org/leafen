import type * as Y from "yjs";
import { dateMentionDisplayText } from "@/components/editor/dateMentions";

type Marks = Readonly<Record<string, unknown>>;
const word = (value: unknown) => typeof value === "string" ? value : undefined;

/**
 * A run of text as the page's and block's menus count it: what Notion draws
 * there, its code read 2026-11-16 and its block menu foot seen 2026-11-19 in
 * Chrome ("Read @Count target now" 4 words and 22 characters, "Solve $x^2+y$
 * fast" 5 and 18, "회의 @2026년 11월 1일 준비" 8 and 19). A person's,
 * page's, database's, template's or date's mention is "@" and the name or
 * date it shows now (a date in the reader's language, "오늘" and its time
 * too), an inline equation "$" and its TeX, a custom emoji one "x".
 */
function countedRun(text: string, marks: Marks): string {
  const equation = word(marks.equation);
  if (equation !== undefined) return `$${equation}$`;
  const at = (shown: string) => `@${shown.replace(/^@/u, "")}`;
  switch (marks.mention) {
    case "date": {
      const date = word(marks.date);
      return date ? at(dateMentionDisplayText(date, text, undefined, word(marks.dateEnd), word(marks.dateFormat), word(marks.dateTimeFormat))) : text;
    }
    case "page": case "database": case "person": case "template": return at(text);
    case "custom_emoji": return "x";
    default: return text;
  }
}

/** A collaborative text's counted text (countedRun). */
export function countedText(text: Y.Text | undefined): string {
  if (!text) return "";
  let counted = "";
  for (const { insert, attributes } of text.toDelta() as Array<{ insert?: unknown; attributes?: Marks }>) {
    if (typeof insert === "string") counted += countedRun(insert, attributes ?? {});
  }
  return counted;
}

/** Text spans' counted text (a caption, a table cell; countedRun). */
export function countedSpans(value: unknown): string {
  if (!Array.isArray(value)) return "";
  return value.map((span) => span && typeof span === "object" && typeof (span as { text?: unknown }).text === "string"
    ? countedRun((span as { text: string }).text, span as Marks) : "").join("");
}
