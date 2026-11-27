/**
 * How many lines Notion's face takes for a card title's words on a line so
 * wide, worked out from Leafen's face: Notion draws each Hangul syllable
 * 0.055em narrower than Leafen's Noto Sans KR (12.975px at 15px 500 against
 * 13.80) and its other letters 1.6% narrower than Inter's (seen 2026-10-30
 * and 2026-11-15). Its lines break as the browser breaks them: between any
 * two Hangul syllables, a run of other letters kept whole, a space hanging
 * at a line's end, a word longer than the line broken where it runs out —
 * Notion's own widths so laid out gave all 96 of a board's titles their
 * lines (2026-11-15, a 260px card's 238 and 212 lines).
 */
export const HANGUL_EXCESS_EM = 0.055;
export const OTHER_EXCESS = 0.016;
const HANGUL_LETTER = /\p{Script=Hangul}/u;

/** Measures a piece of text in Leafen's face. */
export type Measure = (text: string) => number;

export function notionTitleLines(text: string, line: number, fontSize: number, measure: Measure): number {
  const hangul = Math.max(0, measure("가") - HANGUL_EXCESS_EM * fontSize);
  const space = measure(" ") * (1 - OTHER_EXCESS);
  const letters = [...text];
  let lines = 1;
  let at = 0;
  const place = (width: number) => {
    if (at > 0 && at + width > line + 0.01) {
      lines++;
      at = 0;
    }
    // A word wider than the line takes as many lines as it fills.
    if (width > line && line > 0) {
      lines += Math.ceil(width / line) - 1;
      at = width % line;
      return;
    }
    at += width;
  };
  for (let index = 0; index < letters.length;) {
    const letter = letters[index];
    if (letter === "\n") {
      lines++;
      at = 0;
      index++;
    } else if (letter === " ") {
      at += space;
      index++;
    } else if (HANGUL_LETTER.test(letter)) {
      place(hangul);
      index++;
    } else {
      let end = index;
      while (end < letters.length && letters[end] !== " " && letters[end] !== "\n" && !HANGUL_LETTER.test(letters[end])) end++;
      place(measure(letters.slice(index, end).join("")) * (1 - OTHER_EXCESS));
      index = end;
    }
  }
  return lines;
}
