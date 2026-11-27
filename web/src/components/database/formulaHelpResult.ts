import { evaluateFormulaExpressionResult, FormulaDate, FormulaRichText, tokenizeFormula, type FormulaValue } from "./formulaCore";
import { formulaDateText } from "./formulaDateText";
import { formulaTimeZone } from "./formulaDates";
import { FORMULA_HELP_LOCALE, formulaLocale } from "./formulaLocales";
import { formatFormulaValue } from "./formulaValues";
import { FORMULA_EXAMPLE_SAMPLES } from "./formulaFunctionCatalog";

// The help works its examples out as Notion's help does, which has no viewer
// of its own (seen 2026-11-28 in Chrome on its Korean screen at Seoul time):
// it reads a time written as text on UTC's clock (hour(parseDate(
// "2023-07-10T17:35Z")) is 17, day() 1 and date() 10, where a cell at Seoul
// gives 2, 2 and 11), the clock's now() and today() on the viewer's
// (formatDate(now(), "YYYY-MM-DD HH:mm") is the time at Seoul), and writes
// text in English on a 24-hour clock (formatDate(now(), "MMMM D, Y") is
// "November 28, 2026" and format(now()) "November 28, 2026 03:41", where a
// cell writes "11월 28, 2026" and "2026년 11월 28일 오전 3:41"). A date it gives
// is shown as a mention on the viewer's clock and in their language, as a
// cell's (parseDate("2022-01-01T00:00Z") is 2022년 1월 1일 오전 9:00 at Seoul).
// So an example that asks for now() or today() is worked out on the viewer's
// clock and any other on UTC's.

function sampleProperty(name: string) {
  if (!(name in FORMULA_EXAMPLE_SAMPLES)) throw new Error(`No sample for ${name}`);
  return FORMULA_EXAMPLE_SAMPLES[name];
}

function readsClock(run: string) {
  try {
    return tokenizeFormula(run).some((token, index, tokens) =>
      token.type === "identifier" && (token.value === "now" || token.value === "today") && tokens[index + 1]?.value === "(");
  } catch {
    return false;
  }
}

/** An example's result as the help writes it after "=", or "" for none: run is what works it out. */
export function formulaHelpResult(expression: string, run: string): string {
  const viewerZone = formulaTimeZone(undefined);
  const outcome = evaluateFormulaExpressionResult(run, sampleProperty, {
    timeZone: readsClock(run) ? viewerZone : "UTC", locale: FORMULA_HELP_LOCALE,
  });
  if (outcome.error) return "";
  const viewer = { timeZone: viewerZone, locale: formulaLocale() };
  // As Notion's help writes a result: text quoted, a date as a mention, a
  // list in brackets, a number whole (sqrt(7) = 2.6457513110645907),
  // nothing as "empty".
  const shown = (value: FormulaValue): string => value === null || value === "" ? "empty"
    : typeof value === "number" && Number.isFinite(value) ? String(value)
    : value instanceof FormulaDate ? `@${formulaDateText(value, viewer)}`
    : Array.isArray(value) ? `[${value.map(shown).join(", ")}]`
    : typeof value === "string" || value instanceof FormulaRichText ? JSON.stringify(formatFormulaValue(value))
    : formatFormulaValue(value);
  const result = shown(outcome.value);
  // A result that only repeats the example is left out (true).
  return result === expression ? "" : result;
}
