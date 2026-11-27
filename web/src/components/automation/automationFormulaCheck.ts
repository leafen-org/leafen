import type { DbProperty } from "@/lib/types";
import type { FormulaBounds } from "@/components/database/formulaBudget";
import { formulaWrittenProblems } from "@/components/database/formulaDraftProblems";
import type { FormulaProblem, FormulaScopeName } from "@/components/database/formulaEditorModel";

const UNBOUNDED = Number.MAX_SAFE_INTEGER;
// A value's formula is computed on the server as it runs; the editor's own reading stays within these.
export const VALUE_FORMULA_BOUNDS: FormulaBounds = {
  maxExpressionCharacters: 4_096, maxProperties: UNBOUNDED, maxOptions: UNBOUNDED, maxPages: UNBOUNDED, maxEvaluations: 10_000,
  maxValueNodes: UNBOUNDED, maxValueDepth: 10, maxStringCharacters: 1_000_000,
  rollup: { maxDatabases: UNBOUNDED, maxProperties: UNBOUNDED, maxPages: UNBOUNDED, maxValueNodes: UNBOUNDED, maxValueDepth: 10,
    maxValuePieces: UNBOUNDED, maxOptions: UNBOUNDED, maxFileAttachments: UNBOUNDED, maxPersonNodes: UNBOUNDED, maxPersonDepth: 8,
    maxPeopleEntries: UNBOUNDED, maxStringCharacters: 1_000_000 },
};

/**
 * The first problem the formula editor names in a formula an automation
 * holds, and whether it is one of reading it (`parse`). Notion's editor
 * keeps such a formula on "저장", then says it where the formula stands and
 * holds "활성화" back until it is fixed (seen 2026-11-21: a variable's "1."
 * and "foo + 1", a page line's "1." and "foo", "속성 편집"'s "1." and
 * "foo + 1"); the server refuses the one it can't read. The names it runs
 * with are read as of their types: a text variable's "- 1" is a problem.
 */
export type AutomationFormulaFault = { problem: FormulaProblem; parse: boolean };

export function automationFormulaFault(
  expression: string, properties: readonly DbProperty[], names: readonly FormulaScopeName[],
): AutomationFormulaFault | null {
  if (!expression.trim()) return null;
  const { reading, problems } = formulaWrittenProblems(expression, [...properties], VALUE_FORMULA_BOUNDS, names);
  return problems[0] ? { problem: problems[0], parse: reading.problems.length > 0 } : null;
}
