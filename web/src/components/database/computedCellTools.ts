import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import type { DbProperty, Page } from "@/lib/types";
import { evaluateFormula, formulaDisplay } from "./formula";
import type { PropertyValueReadContext } from "./PropValue";
import { rollupGivesNumber } from "./rollupNumberDisplay";
import { computedValueText } from "./tableCellClipboard";

/**
 * A formula's or a rollup's table cell tools under the pointer, as Notion's
 * (seen 2026-11-12, 1440 dark): a number result's "댓글" and "클립보드에
 * 복사" sit at the cell's start over its room, as a number's do, the value
 * showing at its end; a text or a date result's at its end; a true or false
 * result offers "댓글" alone. Its copy gives what a copy of the cell gives
 * ("@2026년 1월 2일 오전 10:30" for a date, tableCellText).
 */
export function computedCellTools(page: Page, property: DbProperty, context: PropertyValueReadContext | undefined) {
  const copy = () => context ? computedValueText(page.id, property, context) : "";
  if (property.type === "rollup") {
    return { side: rollupGivesNumber(property.config?.rollupFunction, { type: "number" }) ? "start" as const : undefined, copy };
  }
  if (!context) return { side: undefined, copy };
  const propsByDb = context.propsByDatabaseId as Record<string, DbProperty[]>;
  const prop = propsByDb[property.databaseId]?.find((candidate) => candidate.id === property.id) ?? property;
  const display = formulaDisplay(page, prop, context.bounds, untitledPageDisplayTitle("page"), () => evaluateFormula({
    row: page, prop, props: propsByDb[prop.databaseId] ?? [], pagesById: context.pagesById as Record<string, Page>, propsByDb, bounds: context.bounds,
  }));
  const checks = display.checked !== undefined || display.checks !== undefined;
  return { side: display.number !== undefined || display.numeric ? "start" as const : undefined, copy: checks ? undefined : copy };
}
