import type { TFunction } from "i18next";
import type { DbProperty, TextSpan } from "@/lib/types";
import type { FormulaScopeName } from "@/components/database/formulaEditorModel";
import { tokenizeFormula, type FormulaToken } from "@/components/database/formulaSyntax";

/** A run of a formula as words take it: text as written, or a name "@" puts in words as a chip. */
type Piece = { start: number; end: number; chip?: TextSpan; text?: string };

const isToken = (token: FormulaToken | undefined, type: FormulaToken["type"], value?: string) =>
  token?.type === type && (value === undefined || token.value === value);

/**
 * The names in a formula that words hold as "@" chips (AutomationMentionMenu):
 * a variable, who ran it, the page's maker, when and on what day, the run's
 * page, and that page's property read with prop() — each where it is written.
 */
function chipsOf(tokens: readonly FormulaToken[], names: readonly FormulaScopeName[], properties: readonly DbProperty[], t: TFunction) {
  const chips: Piece[] = [];
  for (let index = 0; index < tokens.length; index += 1) {
    const token = tokens[index];
    const name = token.type === "identifier" && !isToken(tokens[index - 1], "dot") ? names.find((candidate) => candidate.name === token.value) : undefined;
    if (!name || isToken(tokens[index + 1], "paren", "(")) continue;
    const [dot, prop, open, reference, close] = tokens.slice(index + 1, index + 6);
    if (name.page && isToken(dot, "dot") && isToken(prop, "identifier", "prop") && isToken(open, "paren", "(") && isToken(reference, "string")
      && isToken(close, "paren", ")")) {
      const field = name.fields?.find(({ property }) => property.name === reference!.value);
      const property = field?.property ?? properties.find((candidate) => candidate.name === reference!.value);
      chips.push({ start: token.start ?? 0, end: close!.end ?? 0, chip: {
        text: t(name.fields ? "databaseView:thisPageProperty" : "databaseView:triggerPageProperty", { name: field?.label ?? reference!.value }),
        mention: "automation", automation: `${name.name}.prop(${JSON.stringify(reference!.value)})`, automationType: property?.type ?? "" } });
      index += 5;
      continue;
    }
    chips.push({ start: token.start ?? 0, end: token.end ?? 0,
      chip: { text: name.label, mention: "automation", automation: name.name, automationType: name.chip ?? name.type } });
  }
  return chips;
}

/** Words as a field keeps them: their text, and their spans where a chip is among them. */
function wordsOf(pieces: readonly Piece[]): { value: string; spans?: TextSpan[] } {
  const spans: TextSpan[] = [];
  for (const piece of pieces) {
    if (piece.chip) spans.push(piece.chip);
    else if (piece.text) {
      const last = spans[spans.length - 1];
      if (last && !last.mention) last.text += piece.text; else spans.push({ text: piece.text });
    }
  }
  const value = spans.map((span) => span.text).join("");
  return spans.some((span) => span.mention) ? { value, spans } : { value };
}

/**
 * A formula made words again, as Notion's "전환" makes them (seen 2026-11-10,
 * 2026-11-21 in a database automation's variables): texts and names "@" puts
 * in words joined with "+" are those words, each name a chip (`"12"` gives
 * "12", "ab" + 변수 2 + "c" gives "ab[변수 2]c", 페이지 실행.prop("메모") +
 * 페이지 작성자 two chips); any other formula is written out as it reads,
 * each such name a chip in it ("[변수 2].length() + 1"; "5+5+1" its own words).
 */
export function automationFormulaWords(expression: string, names: readonly FormulaScopeName[], properties: readonly DbProperty[], t: TFunction) {
  const written = expression.trim();
  let tokens: FormulaToken[];
  try {
    tokens = tokenizeFormula(written);
  } catch {
    return { value: written };
  }
  const chips = chipsOf(tokens, names, properties, t);
  // Each part between the "+"s at the top: one text, or one chip.
  const parts: Piece[] = [];
  let depth = 0, from = 0, joined = true;
  const close = (end: number) => {
    const inside = tokens.filter((token) => (token.start ?? 0) >= from && (token.end ?? 0) <= end);
    const chip = chips.find((candidate) => candidate.start === inside[0]?.start && candidate.end === inside[inside.length - 1]?.end);
    if (chip) parts.push(chip);
    else if (inside.length === 1 && inside[0].type === "string") parts.push({ start: from, end, text: inside[0].value });
    else joined = false;
  };
  for (const token of tokens) {
    if (isToken(token, "paren", "(") || isToken(token, "bracket", "[")) depth += 1;
    if (isToken(token, "paren", ")") || isToken(token, "bracket", "]")) depth -= 1;
    if (depth === 0 && isToken(token, "operator", "+")) { close(token.start ?? 0); from = token.end ?? 0; }
  }
  close(written.length);
  if (joined) return wordsOf(parts);
  // Written out: the formula's own text, its names as chips.
  const pieces: Piece[] = [];
  let at = 0;
  for (const chip of chips) {
    pieces.push({ start: at, end: chip.start, text: written.slice(at, chip.start) }, chip);
    at = chip.end;
  }
  pieces.push({ start: at, end: written.length, text: written.slice(at) });
  return wordsOf(pieces);
}
