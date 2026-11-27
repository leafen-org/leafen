import type { DbProperty } from "@/lib/types";
import type { FormulaValueMark } from "./PropertyTypeIcon";
import type { FormulaShape, FormulaValueType } from "./formulaTypes";

/**
 * A name a formula may use where it runs beside its page's properties — an
 * automation's "실행한 모든 사람" (executionActor) and the rest — written bare
 * and listed before the properties, as Notion's automation formulas list
 * them (seen 2026-10-22).
 */
export type FormulaScopeName = {
  name: string; label: string; type: DbProperty["type"]; description: string;
  /** What the help shows written with it, as Notion's; the name alone when none. */
  examples?: readonly string[];
  /** A page whose properties follow its "." — prop("Name") — as Notion's "페이지 실행". */
  page?: boolean;
  /** That page's database, whose properties its prop() reads: a database automation's or button's run page. */
  database?: string;
  /**
   * The page's own fields its "." reads where no database's properties are
   * at hand — a page button's "이 페이지" — each with the words its "@" menu
   * lists it by ("생성자:").
   */
  fields?: readonly { property: DbProperty; label: string }[];
  /**
   * What it holds, where its type doesn't say it: an automation variable's
   * value as its formula gives it — a number, a list of numbers, a value of
   * a type that can't be told (Notion's, seen 2026-11-21: a later formula
   * reads "1"'s variable as a number, "변수 + 1" is a number, "변수 - 1" of a
   * text one names text and number).
   */
  shape?: FormulaShape;
  /** The mark its chips show where its type's isn't theirs: a list's "[ ]", "?" for one that can't be told. */
  chip?: FormulaValueMark;
};

/**
 * The type a formula reads a name as: what it holds where that is known, a
 * time's a date, the run's people a person and its page a page ("block") of
 * its database, whose prop() reads that database's property; the others as
 * text. Notion's automation editor names their problems so (seen 2026-11-21
 * in Chrome at 1440: "페이지 작성자 - 1" and "실행한 모든 사람 - 1" are
 * math on person and number, "페이지 실행 - 1" on block and number,
 * 페이지 실행.prop("메모") * 2 on text and number).
 */
export function formulaScopeNameType(names: readonly FormulaScopeName[]) {
  return (name: string): FormulaValueType | FormulaShape => {
    const found = names.find((candidate) => candidate.name === name);
    if (found?.shape) return found.shape;
    if (!found) return "unknown";
    if (found.page) return { type: "block", ...(found.database ? { source: found.database } : {}) };
    return found.type === "person" ? "person" : found.type === "date" ? "date" : "text";
  };
}
