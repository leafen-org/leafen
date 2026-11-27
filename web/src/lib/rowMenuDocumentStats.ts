export type RowMenuDocumentStats = {
  blocks: number;
  characters: number;
  words: number;
};

// A word where the browser has no word segmenter: a run of Hangul, of Latin
// letters and digits (with "_", and a number's decimals), a Han character, or
// a run of other letters; dashes, slashes, a dot between letters, emoji and
// other marks part them.
const WORD_PATTERN = /\p{Script=Hangul}+|[\p{Script=Latin}\p{N}_][\p{Script=Latin}\p{M}\p{N}_]*(?:\.\p{N}+)*|\p{Script=Han}|\p{L}[\p{L}\p{M}]*/gu;

const wordSegmenter = typeof Intl !== "undefined" && typeof Intl.Segmenter === "function"
  ? new Intl.Segmenter(undefined, { granularity: "word" }) : undefined;
const HAN_RUN = /^\p{Script=Han}+$/u;

/**
 * A text's words, as Notion's page, block and row menus count them (its code,
 * read 2026-11-16): the browser's word segments in the reader's language,
 * a run of Han characters counting one word for each character ("東京" is 2,
 * "中文测试句子" 6) and any other its one ("カタカナ café" 2, "사과나무 아래에서"
 * 2, "a5 x1y2 3.14 a-b c_d e.f g/h mixed한글English 42" 14 as seen
 * 2026-11-04); emoji and marks are no words.
 */
export function countWords(text: string) {
  if (!wordSegmenter) return text.match(WORD_PATTERN)?.length ?? 0;
  let words = 0;
  for (const { segment, isWordLike } of wordSegmenter.segment(text)) {
    if (isWordLike) words += HAN_RUN.test(segment) ? Array.from(segment).length : 1;
  }
  return words;
}

const graphemeSegmenter = typeof Intl !== "undefined" && typeof Intl.Segmenter === "function"
  ? new Intl.Segmenter(undefined, { granularity: "grapheme" }) : undefined;
const EMOJI = /\p{Extended_Pictographic}/u;

/**
 * A text's characters, as Notion's menus count them (its code, read
 * 2026-11-16; seen 2026-11-19: "Party 🎉 👍🏽 👨‍👩‍👧 ok" is 14): its code
 * points, or its graphemes where it holds an emoji, a skin tone or a family
 * then being one.
 */
export function countCharacters(text: string) {
  return graphemeSegmenter && EMOJI.test(text) ? Array.from(graphemeSegmenter.segment(text)).length : Array.from(text).length;
}

export function rowMenuDocumentStats(
  parts: readonly string[],
  blocks: number,
): RowMenuDocumentStats {
  const texts = parts.filter(Boolean);
  const text = texts.join(" ").trim();
  return {
    blocks,
    // Each text's own characters, its spaces too, as Notion's row menu counts
    // them ("Count probe" is 11, seen 2026-10-11).
    characters: texts.reduce((count, part) => count + countCharacters(part), 0),
    words: countWords(text),
  };
}
