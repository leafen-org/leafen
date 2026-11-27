import type { TextSpan } from "./types";

// Formatted text shown as a short line — a changed line in the page's and
// the workspace's updates, a first line in the publish tab's preview, a
// line of search's preview and of a page's card — keeps its equations,
// mentions and marks, drawn as in the page (RichTextLine), as Notion's are
// (seen 2026-11-13: an inline equation drawn as math, not its TeX, in all
// but search's preview, which names it; marks and colors in all). Its plain
// text is only what shows where no spans match it. An equation or a mention
// is one piece: never cut, it goes whole to the part it starts in.

export type RichTextParts<T> = { before: T; added: T; after: T };

/** An inline equation, one with no expression yet among them. */
export const inlineEquation = (span: TextSpan) => span.equation !== undefined || span.emptyPiece === "equation";

const whole = (span: TextSpan) => Boolean(span.mention) || inlineEquation(span);

/**
 * A text's spans cut at the parts its trimmed text was split into:
 * [start, start + before), then the added part, then the rest, where start is
 * the whitespace the text trimmed; none when the spans don't spell the text.
 */
export function richTextParts(rich: readonly TextSpan[], plainText: string,
  parts: RichTextParts<string>): RichTextParts<TextSpan[]> | undefined {
  if (!rich.length || rich.map((span) => span.text ?? "").join("") !== plainText) return undefined;
  const start = plainText.length - plainText.trimStart().length;
  const cuts = [start + parts.before.length, start + parts.before.length + parts.added.length];
  const pieces: [TextSpan[], TextSpan[], TextSpan[]] = [[], [], []];
  let offset = 0;
  for (const span of rich) {
    const text = span.text ?? "";
    if (whole(span)) {
      const part = offset < cuts[0] ? 0 : offset < cuts[1] ? 1 : 2;
      if (offset + text.length > start) pieces[part].push(span);
    } else {
      for (let part = 0; part < 3; part += 1) {
        const from = Math.max(offset, part === 0 ? start : cuts[part - 1]);
        const to = Math.min(offset + text.length, part === 2 ? plainText.trimEnd().length : cuts[part]);
        if (to > from) pieces[part].push({ ...span, text: text.slice(from - offset, to - offset) });
      }
    }
    offset += text.length;
  }
  return { before: pieces[0], added: pieces[1], after: pieces[2] };
}

/**
 * A line's spans as one short line: runs of spaces made one, its ends
 * trimmed, and a longer one cut to `limit` characters, "…" the last, after
 * the last piece that fits; empty when nothing is left.
 */
export function richTextSummary(spans: readonly TextSpan[], limit: number): TextSpan[] {
  const line: TextSpan[] = [];
  for (const span of spans) {
    if (whole(span)) line.push(span);
    else {
      const text = (span.text ?? "").replace(/\s+/gu, " ");
      if (text) line.push({ ...span, text });
    }
  }
  const trim = (at: number, side: "start" | "end") => {
    const span = line[at];
    if (!span || whole(span)) return;
    const text = side === "start" ? span.text.trimStart() : span.text.trimEnd();
    if (text) line[at] = { ...span, text };
    else line.splice(at, 1);
  };
  while (line.length && !whole(line[0]) && !line[0].text.trim()) line.shift();
  trim(0, "start");
  while (line.length && !whole(line[line.length - 1]) && !line[line.length - 1].text.trim()) line.pop();
  trim(line.length - 1, "end");
  const lengths = line.map((span) => Array.from(span.text).length);
  if (lengths.reduce((sum, length) => sum + length, 0) <= limit) return line;
  const kept: TextSpan[] = [];
  let used = 0;
  for (const [at, span] of line.entries()) {
    if (used + lengths[at] <= limit - 1) {
      kept.push(span);
      used += lengths[at];
      continue;
    }
    if (!whole(span)) {
      const text = Array.from(span.text).slice(0, limit - 1 - used).join("").trimEnd();
      if (text) kept.push({ ...span, text });
    }
    break;
  }
  kept.push({ text: "…" });
  return kept;
}
