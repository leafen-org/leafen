import { parseFormulaTokensRecovering } from "./formulaAst";
import { FormulaSyntaxError, MAX_FORMULA_EXPRESSION_CHARACTERS, formulaInvalidCharacters, tokenizeFormula } from "./formulaCore";
import { formulaErrorKey, type FormulaProblem } from "./formulaEditorModel";

// What keeps a formula being edited from being read, as Notion's editor names
// it (seen 2026-11-15, 1440 dark): every fault the reading meets as it reads
// on past them (formulaAst.ts parseFormulaTokensRecovering), after the
// problems of what it could read, which the editor names as it names them in
// a formula that reads ("abs(foo) +" names foo as not defined [4,7], then an
// expression wanted [9,10], then in debug mode the amber line of abs()
// [0,8]). What it leaves unread is `skipped`, where no name is checked.

// `alone` when it is named by itself: a formula too long or too deep to be read.
export type FormulaReading = { problems: FormulaProblem[]; skipped: readonly (readonly [number, number])[]; alone?: true };

const NONE: FormulaReading = { problems: [], skipped: [] };

/**
 * Notion's editor names at most three problems in red, and of the amber lines
 * of debug mode at most three, though it marks them all in the formula (seen
 * 2026-11-15: foo + bar + baz + qux names foo, bar and baz, a syntax fault
 * after them none; four abs(foo)-like calls three amber lines).
 */
export const FORMULA_SHOWN_LINES = 3;

export function formulaSyntaxProblems(expression: string): FormulaReading {
  const trimmed = expression.trim();
  if (!trimmed) return NONE;
  if (trimmed.length > MAX_FORMULA_EXPRESSION_CHARACTERS) return { problems: [{ key: "tooLong" }], skipped: [], alone: true };
  const tokens = tokenizeFormula(expression);
  try {
    const { faults, skipped } = parseFormulaTokensRecovering(tokens, expression.length);
    // What keeps its tokens from being read comes first, by place, as Notion's
    // reads them before its grammar (seen 2026-11-27 in Chrome: '1 # "abc' is
    // "#" [2,3], the string [4,5], then the end wanted [4,5]; lower("a the
    // string [6,7] before ")" [7,8]): a character no token starts with
    // (formulaSyntax.ts), and a string with no closing quote, marked at its
    // opening one ('"abc' is [0,1], seen 2026-11-03); the names it reads are
    // named before them all (formulaDraftProblems.ts — foo + "abc is foo's
    // [0,3], then [6,7], seen 2026-11-15).
    const open = tokens.find((token) => token.type === "string" && (token.end ?? 0) > expression.length);
    const unread: FormulaProblem[] = [
      ...formulaInvalidCharacters(tokens).map(({ character, start, end }) => ({ key: "invalidCharacter", token: character, start, end })),
      ...(open?.start !== undefined ? [{ key: "unclosedString", start: open.start, end: open.start + 1 }] : []),
    ].sort((left, right) => (left.start ?? 0) - (right.start ?? 0));
    return { problems: [...unread, ...faults.map(({ message, expected, start, end }) => ({ key: formulaErrorKey(message), token: expected, start, end }))], skipped };
  } catch (error) {
    // Too deep a formula is named alone, at the token its reading stopped at;
    // what else stops it is a formula that can't be read.
    if (!(error instanceof FormulaSyntaxError)) return { problems: [{ key: "invalid" }], skipped: [], alone: true };
    return { problems: [{ key: formulaErrorKey(error.message), start: error.start, end: error.end }], skipped: [], alone: true };
  }
}

/** Whether a name problem at `start` is in what the reading left unread. */
export function formulaUnread(reading: FormulaReading, start: number | undefined) {
  return start !== undefined && reading.skipped.some(([from, to]) => start >= from && start < to);
}
