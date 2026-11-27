/**
 * The decimal places a number shows when nothing chooses them, as Notion's
 * cells show a number property's or a formula's number (seen 2026-11-14
 * under 한지테스트페이지): 1/3 kept by a formula shows 0.333333333333, a
 * number property's 0.30000000000000004 0.300000000000.
 */
export const SHOWN_DECIMALS = 12;

/** A number's text in pieces: its sign, the digits before its point and those after it. */
export type NumberTextParts = { negative: boolean; whole: string; decimals: string };

/**
 * A number's text cut to `places` decimal places as Notion's cells cut it
 * (seen 2026-11-14): its own text (String()) split at its point, the
 * decimals past `places` dropped and the kept ones raised by one in their
 * last place when the first dropped is a digit of 5 or more, a carry going
 * into the whole part (0.9999999999999999 is 1.000000000000, 0.00099999999999999
 * 0.001000000000), then filled with zeros to `pad` places. It reads the text
 * as written, so a large number's exponent counts among its decimals: the
 * decimals of 1.2345678901234567e+21 are cut to 1.234567890123, and those of
 * 1.234567891e+25, its last kept place "e+2" raised as the number 234567891e+2,
 * to 1.23456789101 — as Notion's are.
 */
export function cutNumberText(value: number, places: number, pad = 0): NumberTextParts {
  const text = String(value);
  const negative = text.startsWith("-");
  const [first, rest = ""] = (negative ? text.slice(1) : text).split(".");
  let whole = first, decimals = rest;
  if (decimals.length > places) {
    let kept = decimals.slice(0, places);
    if (Number(decimals.charAt(places)) >= 5) {
      const zeros = /^0*/u.exec(kept)?.[0] ?? "";
      kept = zeros + String(Number(kept.slice(zeros.length)) + 1);
      if (kept.length > places) {
        whole = String(Number(whole) + Number(kept.charAt(0)));
        kept = kept.slice(1);
      }
    }
    decimals = kept;
  }
  return { negative, whole, decimals: pad > 0 ? decimals.padEnd(pad, "0") : decimals };
}

/**
 * A small number written with an exponent ("3.33333e-7") as Notion's cells
 * show it: with as many decimal places as its exponent says (0.0000003,
 * 9.9e-11 0.00000000010, 5e-13 0.0000000000005), whatever places are chosen;
 * from e-20 on, null, and the number is cut as any other (1e-20 shows 1e-20).
 */
export function smallNumberText(value: number): string | null {
  const exponent = /e-(\d+)$/u.exec(String(value));
  if (!exponent || Number(exponent[1]) >= 20) return null;
  return value.toFixed(Number(exponent[1]));
}

/** The pieces as one text, with `decimal` at the point and `group` between each three whole digits. */
export function joinNumberText(parts: NumberTextParts, decimal = ".", group = "") {
  let whole = parts.whole;
  if (group) while (/\d{4}/u.test(whole)) whole = whole.replace(/(\d+)(\d{3})/u, `$1${group}$2`);
  return `${parts.negative ? "-" : ""}${whole}${parts.decimals ? `${decimal}${parts.decimals}` : ""}`;
}

/**
 * A number as a cell without a number format shows it, as Notion's (seen
 * 2026-11-14): a small one by its exponent (smallNumberText), any other cut
 * to 12 decimal places (cutNumberText) — 0.333333333333, 1e+21, 3.3333333333333335e+21
 * as 3.333333333333, 0, -42.
 */
export function shownNumberText(value: number): string {
  return smallNumberText(value) ?? joinNumberText(cutNumberText(value, SHOWN_DECIMALS));
}
