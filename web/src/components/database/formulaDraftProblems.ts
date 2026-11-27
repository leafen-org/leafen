import type { DbProperty } from "@/lib/types";
import { formulaWarnings, type FormulaBounds, type FormulaWarning } from "./formula";
import { formulaWarningsKept } from "./formulaEditorPieces";
import { formulaProblemsInOrder, formulaTypeProblemsOf, type FormulaProblem, type FormulaScopeName } from "./formulaEditorModel";
import { formulaSyntaxProblems, formulaUnread, type FormulaReading } from "./formulaSyntaxProblems";

// What the formula editor names in red, worked out the same way wherever a
// formula is written: the editor as it is typed (FormulaEditorDialog) and an
// automation's steps, which name the first of them where the formula stands
// and hold the automation back until it is fixed (automationFormulaFault.ts).

/**
 * What the names a formula reads let it not do — a name it doesn't know, a
 * call short of values — beside what keeps it from being read, which the
 * reading names (formulaSyntaxProblems.ts) and where no name is checked.
 */
export function formulaNameProblems(
  draft: string, props: DbProperty[], bounds: FormulaBounds, names: ReadonlySet<string>, reading: FormulaReading,
): FormulaProblem[] {
  let found: FormulaWarning[];
  try {
    found = formulaWarnings(draft, props, bounds, names)
      .filter((item) => !["syntax", "parentheses", "brackets"].includes(item.code) && !formulaUnread(reading, item.start));
  } catch {
    return [{ key: "tooLong" }];
  }
  return found.map((item) => ({ key: item.code, name: item.name, start: item.start, end: item.end, expected: item.expected, given: item.given, at: item.at }));
}

/**
 * The problems in the order the editor names them: those of the names and
 * types it reads, by where its check comes to them, then what keeps it from
 * being read, then what its value can't take (`result`); a formula too long
 * or too deep to read is named alone.
 */
export function formulaNamedProblems({ reading, names, types, result, draft, props, propsByDb }: {
  reading: FormulaReading; names: readonly FormulaProblem[]; types: readonly FormulaProblem[]; result?: FormulaProblem | null;
  draft: string; props: readonly DbProperty[]; propsByDb?: Record<string, DbProperty[]>;
}): FormulaProblem[] {
  if (reading.alone) return reading.problems;
  const kept = formulaWarningsKept(names, draft, props, propsByDb);
  return [...formulaProblemsInOrder(kept, types), ...reading.problems, ...(result ? [result] : [])];
}

/** A formula's problems as written, before it runs on any page, with the names it runs with: what the editor names in red for it. */
export function formulaWrittenProblems(
  draft: string, props: DbProperty[], bounds: FormulaBounds, names: readonly FormulaScopeName[], propsByDb?: Record<string, DbProperty[]>,
): { reading: FormulaReading; problems: FormulaProblem[] } {
  const reading = formulaSyntaxProblems(draft);
  const types = reading.alone ? [] : formulaTypeProblemsOf(draft, props, propsByDb, names);
  const known = new Set(names.map((name) => name.name));
  const problems = formulaNamedProblems({ reading, names: formulaNameProblems(draft, props, bounds, known, reading), types, draft, props, propsByDb });
  return { reading, problems };
}
