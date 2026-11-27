import type { TextSpan } from "@/lib/types";

// Mentions whose words their target names, not the title's own text.
const NAMED_MENTIONS: ReadonlySet<TextSpan["mention"]> = new Set(["page", "database", "person"]);

type Titled = { title?: string | null; titleSpans?: readonly TextSpan[] };

/**
 * A page as a result row of search names it, as Notion's rows (seen
 * 2026-11-16, twelve titles, in two tabs): the first and the last of the
 * title's own words lose the spaces at the title's ends, a page, database
 * or person mention not counting as words — "행 " and a mention reads
 * "행@대상", a mention and " 시작" "@대상시작", while "앞 ", a mention and " 뒤"
 * keep both spaces, as does the space between two mentions and the one
 * before a date mention ("날짜 @2026년 11월 16일"). The page itself, its
 * preview beside the results and every other list keep the title as it is.
 */
export function searchRowTitled<T extends Titled>(page: T): T {
  const spans = page.titleSpans;
  if (!spans?.some((span) => NAMED_MENTIONS.has(span.mention))) return page;
  const spelled = spans.map((span) => span.text).join("");
  if (spelled !== page.title && spelled.trim() !== page.title) return page;
  const words = (span: TextSpan) => !NAMED_MENTIONS.has(span.mention) && span.text.trim() !== "";
  const first = spans.findIndex(words);
  if (first < 0) return page;
  let last = spans.length - 1;
  while (!words(spans[last])) last -= 1;
  const trimmed = spans.map((span, index) => {
    let text = span.text;
    if (index === first) text = text.trimStart();
    if (index === last) text = text.trimEnd();
    return text === span.text ? span : { ...span, text };
  });
  return { ...page, title: trimmed.map((span) => span.text).join(""), titleSpans: trimmed };
}
