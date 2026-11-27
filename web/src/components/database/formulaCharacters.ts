import { MAX_FORMULA_VALUE_TEXT } from "./formulaSyntax";

// Notion's formulas count text by characters as lodash does: a character is
// a character of the Basic Multilingual Plane with one combining mark, an
// astral character, or a pair of regional indicators (a flag), then an
// optional variation selector and one more combining mark or skin tone, and
// any characters a zero-width joiner joins to it, each with its own; a skin
// tone before another stands alone. length(), padStart() and padEnd() count
// so, and other text functions count UTF-16 units. The server counts the
// same way (database_formula_characters.go).

const COMBINING = "[\\u0300-\\u036f\\ufe20-\\ufe2f\\u20d0-\\u20ff]";
const SKIN_TONE = "\\ud83c[\\udffb-\\udfff]";
const MODIFIER = `(?:${COMBINING}|${SKIN_TONE})?`;
const VARIATION = "[\\ufe0e\\ufe0f]?";
const BASIC = "[^\\ud800-\\udfff]";
const FLAG = "(?:\\ud83c[\\udde6-\\uddff]){2}";
const ASTRAL = "[\\ud800-\\udbff][\\udc00-\\udfff]";
const JOINED = `(?:\\u200d(?:${BASIC}|${FLAG}|${ASTRAL})${VARIATION}${MODIFIER})*`;
const CHARACTER = new RegExp(
  `${SKIN_TONE}(?=${SKIN_TONE})|(?:${BASIC}${COMBINING}?|${COMBINING}|${FLAG}|${ASTRAL}|[\\ud800-\\udfff])${VARIATION}${MODIFIER}${JOINED}`,
  "g",
);

// formulaCharacters splits text into the characters formulas count.
export function formulaCharacters(text: string): string[] {
  return text.match(CHARACTER) ?? [];
}

// formulaPadding is what padStart() and padEnd() add to reach a count of
// characters: the fill repeated and cut to whole characters of the repeated
// text, as Notion pads.
export function formulaPadding(missing: number, fill: string) {
  const copies = Math.ceil(missing / formulaCharacters(fill).length);
  // All but the last copy are padding, so a longer repeat is too long text.
  if (fill.length * (copies - 1) > MAX_FORMULA_VALUE_TEXT) throw new Error("Formula text is too long");
  return formulaCharacters(fill.repeat(copies)).slice(0, missing).join("");
}
