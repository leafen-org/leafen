import { i18next } from "@/i18n";
import type { NativeComment } from "@/lib/commentService";

export type SuggestionKind = "insert" | "delete" | "replace" | "format";
export type SuggestionState = "accepted" | "rejected" | "withdrawn";

/** What a suggestion thread's root holds: the suggestion's kind, its text
 * (the start of a long one; a replacement's new words, `replaced` its old
 * ones), a formatting suggestion's marks, the blocks it
 * suggests in, its text at the end of the block it started in when it grew
 * from text suggested there ("" for none) and the blocks' text block by
 * block (none for a thread kept before the server wrote them), whether its
 * block is one "/" picked on a suggested line, how many blocks it suggests
 * away (a removal's, its lines their words), and once it is settled, how. */
export type SuggestionFacts = {
  kind: SuggestionKind;
  text: string;
  replaced: string;
  marks: Readonly<Record<string, boolean | string>>;
  blocks: number;
  head: string;
  lines: readonly string[] | null;
  picked: boolean;
  removes: number;
  state: SuggestionState | null;
};

// Formatting a suggestion turns on, each with its own summary.
const SUMMARIZED_MARKS = new Set(["bold", "italic", "underline", "strikethrough", "code"]);

/** A thread root's suggestion, when the thread holds one. */
export function suggestionFacts(comment: Pick<NativeComment, "body" | "parentId">): SuggestionFacts | null {
  if (comment.parentId) return null;
  const suggestion = (comment.body as { suggestion?: unknown } | null | undefined)?.suggestion;
  if (!suggestion || typeof suggestion !== "object") return null;
  const { kind, text, replaced, marks, blocks, head, lines, picked, removed, state } = suggestion as Record<string, unknown>;
  if (kind !== "insert" && kind !== "delete" && kind !== "replace" && kind !== "format") return null;
  return {
    kind,
    text: typeof text === "string" ? text : "",
    replaced: typeof replaced === "string" ? replaced : "",
    marks: marks && typeof marks === "object" ? marks as Record<string, boolean | string> : {},
    blocks: typeof blocks === "number" && Number.isSafeInteger(blocks) && blocks > 0 ? blocks : 0,
    head: typeof head === "string" ? head : "",
    lines: Array.isArray(lines) && lines.every((line) => typeof line === "string") ? lines as string[] : null,
    picked: picked === true,
    removes: Array.isArray(removed) ? removed.length : 0,
    state: state === "accepted" || state === "rejected" || state === "withdrawn" ? state : null,
  };
}

/** A suggestion's summary as Notion writes it: what it does, to which text. */
export function suggestionSummary(facts: SuggestionFacts): string {
  return suggestionSummaryParts(facts).map(({ label, quote }) => label + quote).join(" | ");
}

/**
 * A part of a suggestion's summary: what it does ("추가: ") and the text it
 * quotes ("“더함”"), none for a line break, in the colors of words suggested
 * in or away (`kind`); the quote also as its marks and the words between
 * them (`open`, `words`, `close`).
 */
export type SuggestionSummaryPart = {
  label: string; quote: string; kind: SuggestionKind; open: string; words: string; close: string;
};

// Stands for the quoted text while a summary is read, to find where it goes.
const QUOTED = "\u2063";
// The opening quote mark a language writes just before the quoted text.
const OPENING_QUOTE = /[\p{Pi}\p{Ps}"']$/u;

/**
 * A suggestion's summary in its parts, as Notion's card draws them: what it
 * does in one color and the quoted text with its quote marks in another
 * (MarginDiscussions, SuggestionSummary).
 */
export function suggestionSummaryParts(facts: SuggestionFacts): SuggestionSummaryPart[] {
  return summaryEntries(facts).map(({ key, text, kind = facts.kind }) => {
    if (text === undefined) return { label: i18next.t(key), quote: "", kind, open: "", words: "", close: "" };
    const [before = "", after = ""] = i18next.t(key, { text: QUOTED }).split(QUOTED);
    const open = OPENING_QUOTE.exec(before)?.[0] ?? "";
    return { label: before.slice(0, before.length - open.length), quote: open + text + after, kind, open, words: text, close: after };
  });
}

function summaryEntries(facts: SuggestionFacts): { key: string; text?: string; kind?: SuggestionKind }[] {
  const text = facts.text;
  // A replacement reads its old words as a deletion's, then its new words as
  // an addition's, as Notion's card does (seen 2026-11-22 in Chrome, twice:
  // "바꾸기: “echo”" in a deletion's grays over "바꾼 내용:: “omega”" in an
  // addition's blues — Notion's "바꾼 내용:" label takes its ": " too, as
  // "검색 범위::" does; English "Replace" and "with").
  if (facts.kind === "replace") {
    return [
      { key: "commentsPanel:suggestion.replaceOld", text: facts.replaced, kind: "delete" },
      { key: "commentsPanel:suggestion.replaceNew", text, kind: "insert" },
    ];
  }
  // Blocks suggested away read each one's words, none for an empty line, as
  // Notion's card does (seen 2026-11-18: "삭제: “four line”", "삭제: “tog line”"
  // for three chosen blocks with an empty line among them; an empty line alone, no line).
  if (facts.removes > 0) {
    return (facts.lines ?? [text]).filter(Boolean).map((line) => ({ key: "commentsPanel:suggestion.delete", text: line }));
  }
  // New blocks read as Notion's (seen 2026-11-15, two ways each): the line
  // break they start with — none when they go on from text suggested at the
  // end of the line before ("추가: “gh”", "추가: “jk”") — each line's
  // words, then a line break for each line without words that another line
  // follows: the breaks a "⏎" marks (suggestedBreakAfter), one after words
  // suggested before it being part of them. A thread kept before its
  // blocks' text was reads its words as one block's, or its blocks as empty
  // without words.
  // A block "/" picked on a suggested line is a card of its own after the
  // break's, as Notion's (seen 2026-11-18, a page and a database page's side
  // peek): "추가: 블록" while it is empty, then its words ("추가: “hd”").
  if (facts.blocks > 0) {
    const lines = facts.lines ?? (text ? [text] : Array.from({ length: facts.blocks }, () => ""));
    if (facts.picked && !lines.some(Boolean)) return [{ key: "commentsPanel:suggestion.block" }];
    return [
      ...(facts.head || facts.picked ? [] : [{ key: "commentsPanel:suggestion.lineBreak" }]),
      ...[facts.head, ...lines].filter(Boolean).map((line) => ({ key: "commentsPanel:suggestion.insert", text: line })),
      ...lines.slice(0, -1).filter((line) => !line).map(() => ({ key: "commentsPanel:suggestion.lineBreak" })),
    ];
  }
  if (facts.kind !== "format") return [{ key: `commentsPanel:suggestion.${facts.kind}`, text }];
  const marks = Object.entries(facts.marks);
  const [mark, value] = marks[0] ?? [];
  if (marks.length === 1 && value === true && mark && SUMMARIZED_MARKS.has(mark)) {
    return [{ key: `commentsPanel:suggestion.${mark}`, text }];
  }
  return [{ key: "commentsPanel:suggestion.format", text }];
}
