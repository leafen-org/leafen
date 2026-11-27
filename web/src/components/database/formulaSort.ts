import { FormulaDate, type FormulaValue } from "./formulaSyntax";
import { parseFormulaDate } from "./formulaDates";
import { toText } from "./formulaValues";

// sort() orders a list's items by their keys — the value its expression gives
// each, or each item itself — as Notion's does (seen 2026-11-19 through its
// API, 48 lists): the lowest first, equal keys kept in their order; numbers by
// number, dates by time and false before true while every key is one of
// those; any other mix by its keys' text, an empty one ("", nothing, an empty
// list) last, the rest as a person reads them — letters alike whatever their
// case or accent ("a", "B", "b", "C"; "e", "é", "f"), an upper case before
// its lower ("B" before "b"), the numbers in them by their worth ("x2", "X3",
// "x10"; "1.2", "1.5", "1.10"), and spaces and marks before digits and digits
// before letters ("a b", "a-b", "ab"). Text reads in the viewer's language,
// as Notion's screen reads it ("가", "나", "A", "b" in Korean, seen 2026-11-19)
// where its API reads English ("A", "b", "가", "나"), as the server does
// (database_formula_each_item.go).

const collators = new Map<string, Intl.Collator>();

export function sortedFormulaList(
  items: readonly FormulaValue[], keys: readonly FormulaValue[], timeZone: string, language: string,
): FormulaValue[] {
  const compare = keyOrder(keys, timeZone, language);
  return items.map((item, index) => ({ item, index }))
    .sort((left, right) => compare(left.index, right.index) || left.index - right.index)
    .map(({ item }) => item);
}

function keyOrder(keys: readonly FormulaValue[], timeZone: string, language: string): (left: number, right: number) => number {
  if (keys.every((key) => typeof key === "number")) return (left, right) => order(keys[left] as number, keys[right] as number);
  const times = keys.map((key) => (key instanceof FormulaDate ? parseFormulaDate(key, timeZone)?.instant : undefined));
  if (times.every((time) => time !== undefined)) return (left, right) => order(times[left]!, times[right]!);
  if (keys.every((key) => typeof key === "boolean")) return (left, right) => Number(keys[left]) - Number(keys[right]);
  const texts = keys.map((key) => toText(key));
  const collator = collatorOf(language);
  return (left, right) => {
    const [leftText, rightText] = [texts[left], texts[right]];
    if (leftText === "" || rightText === "") return Number(leftText === "") - Number(rightText === "");
    return collator.compare(leftText, rightText) || upperFirst(leftText, rightText);
  };
}

function collatorOf(language: string) {
  let collator = collators.get(language);
  if (!collator) {
    try {
      collator = new Intl.Collator(language || "en", { numeric: true, sensitivity: "accent" });
    } catch {
      collator = new Intl.Collator("en", { numeric: true, sensitivity: "accent" });
    }
    collators.set(language, collator);
  }
  return collator;
}

function order(left: number, right: number) {
  return left < right ? -1 : left > right ? 1 : 0;
}

// Two texts alike but for their case, by the first letter they differ in,
// its upper case first.
function upperFirst(left: string, right: string) {
  const [leftLetters, rightLetters] = [Array.from(left), Array.from(right)];
  for (let index = 0; index < leftLetters.length && index < rightLetters.length; index += 1) {
    const [a, b] = [leftLetters[index], rightLetters[index]];
    if (a === b) continue;
    const [aUpper, bUpper] = [isUpper(a), isUpper(b)];
    if (aUpper !== bUpper) return aUpper ? -1 : 1;
    return a < b ? -1 : 1;
  }
  return order(leftLetters.length, rightLetters.length);
}

function isUpper(letter: string) {
  return letter !== letter.toLowerCase() && letter === letter.toUpperCase();
}
