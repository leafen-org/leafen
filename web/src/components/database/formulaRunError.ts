// What keeps a formula from working out that Notion's formula editor names
// under its preview, in red, with where it is (seen 2026-11-14 under
// a Notion test page): round() to places that are no whole number or more
// than 12, toNumber() of a list, a duration unit dateAdd(), dateSubtract()
// or dateBetween() don't know, a number format formatNumber() doesn't know,
// and a function failing in its own work — a sum of a number that is not
// finite, a fraction's power past a million, a pattern the formula works out
// that repeats a repetition (one written in it is a type problem,
// formulaTypes.ts). Every one fails the formula as any failure does; the
// others, as an operator's or an empty value given to a function that takes
// numbers or dates, show only that it has no value, as Notion's.

export type FormulaRunErrorKey = "roundNonInteger" | "roundTooManyPlaces" | "toNumberList" | "durationUnit"
  | "numberFormat" | "libraryCall";

export class FormulaRunError extends Error {
  /** Where it is in the formula, from the call's start to its end, once a call places it. */
  start?: number;
  end?: number;

  constructor(
    readonly key: FormulaRunErrorKey,
    readonly values: Record<string, string> = {},
  ) {
    super(key);
  }
}

/** A failure in a function's own work, named with the function's call; an operator meeting it names nothing. */
export class FormulaWorkError extends Error {}

// A pattern that repeats a group holding a repetition — "(a+)+", "(a+)?",
// "(ab+)*c", "([a-z]+)*" — which Notion refuses, its matching taking time
// growing past any bound; an escaped sign or one in a class repeats nothing
// ("(a\+)+", "(a|b)+" are kept). The server reads patterns the same way
// (database_formula_patterns.go databaseFormulaUnsafePattern).
export function unsafeFormulaPattern(source: string) {
  const groups: boolean[] = [];
  let inClass = false;
  for (let index = 0; index < source.length; index += 1) {
    const char = source[index];
    if (char === "\\") {
      index += 1;
      continue;
    }
    if (inClass) {
      if (char === "]") inClass = false;
      continue;
    }
    if (char === "[") inClass = true;
    else if (char === "(") {
      groups.push(false);
      if (source[index + 1] === "?") index += 1;
    } else if (char === ")") {
      const repeated = groups.pop() ?? false;
      const next = source[index + 1];
      if (repeated && (next === "+" || next === "*" || next === "?" || next === "{")) return true;
      if (repeated && groups.length) groups[groups.length - 1] = true;
    } else if ((char === "+" || char === "*" || char === "{") && groups.length) {
      groups[groups.length - 1] = true;
    }
  }
  return false;
}
