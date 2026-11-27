import type { DbProperty, Page, TextSpan } from "@/lib/types";
import {
  FORMULA_FUNCTIONS,
  FORMULA_LITERALS,
  FormulaDate,
  FormulaRichText,
  evaluateFormulaExpressionResult,
  formulaPropertyReferences,
  formulaVariableNames,
  formatFormulaValue as formatFormulaCoreValue,
  formulaPageField,
  formulaPageFieldProperty,
  resolveFormulaPropertyReference,
  tokenizeFormula,
  type FormulaReference,
  type FormulaValue,
} from "./formulaCore";
import { canonicalRollupFunction, rollupCheckboxStates, rollupShownFunction } from "./rollupPolicy";
import { formulaCallProblems, type FormulaCallProblem } from "./formulaArity";
import { parseFormulaTokensRecovering, type FormulaNode } from "./formulaAst";
import { formulaTimeZone, momentText, parseFormulaDate, validZone } from "./formulaDates";
import { formulaLocale } from "./formulaLocales";
import { normalizePersonIds, personLabel } from "@/lib/peopleDirectory";
import { formatNumberForProperty } from "./numberFormat";
import {
  displayPropertyValue,
  evaluateRollupOriginals,
  evaluateRollupValue,
  type EvaluateRollupOptions,
} from "./rollup";
import {
  admitExpression,
  admitFormulaValue,
  admitPageLookup,
  admitProperties,
  consumeString,
  newFormulaEvaluationState,
  type FormulaBounds,
  type FormulaEvaluationState,
} from "./formulaBudget";
import { valueAsRead } from "./valueAsRead";
import { lineWithClosedMentions } from "@/lib/closedMentions";
import { FormulaPropertyResult, flatFormulaItems, formulaReferenceText, formulaReferencesOf, isFormulaReference, keptFormulaValue, shownFormulaListText } from "./formulaValues";
import { formulaSpans } from "./formulaRichText";
import { FormulaRunError } from "./formulaRunError";
import { formatDate, instantOnViewerClock, storedTimeZone } from "./dateUtils";

export type { FormulaBounds } from "./formulaBudget";

type FormulaRollupOptions = Omit<EvaluateRollupOptions, "bounds"> & {
  bounds: FormulaBounds;
};

// Properties that hold a list, empty when never set.
const EMPTY_LIST_TYPES = new Set(["multi_select", "relation", "person"]);

function rawValue(row: Page, prop: DbProperty): unknown {
  if (prop.type === "title") return row.title;
  if (prop.type === "created_time") return row.createdAt;
  if (prop.type === "last_edited_time") return row.updatedAt;
  if (prop.type === "created_by") return row.createdBy;
  if (prop.type === "last_edited_by") return row.lastEditedBy;
  return valueAsRead(row, prop);
}

// An untitled page keeps no name, never its ID; a person is named by ID
// until their name is known.
function reference(kind: FormulaReference["kind"], id: string, name: string): FormulaReference {
  return { kind, id, name: kind === "page" ? name : name || id };
}

function personName(value: unknown, id: string) {
  if (value && typeof value === "object") {
    const item = value as Record<string, unknown>;
    const notion = item.notion && typeof item.notion === "object"
      ? item.notion as Record<string, unknown>
      : undefined;
    for (const candidate of [item.name, item.displayName, notion?.name]) {
      if (typeof candidate === "string" && candidate.trim()) return candidate.trim();
    }
  }
  return personLabel(id) || id;
}

// A stored date, { start, end, timeZone }, as a formula date; its time zone
// keeps its times on that zone's clock.
function storedFormulaDate(value: unknown): FormulaDate | undefined {
  if (!value || typeof value !== "object" || typeof (value as { start?: unknown }).start !== "string") return undefined;
  const { start, end, timeZone } = value as { start: string; end?: unknown; timeZone?: unknown };
  const zone = typeof timeZone === "string" && validZone(timeZone) ? timeZone : "";
  return new FormulaDate(typeof end === "string" && end ? `${start}/${end}` : start, zone);
}

// A date property's value as a formula reads it: a time written in UTC ("Z")
// keeps UTC's clock, as Notion keeps a date set in UTC (seen 2026-11-12:
// format() writes "2026년 1월 2일 오전 10:30 (UTC)", and the formula's cell
// shows it as the date's, "… 오후 7:30 (GMT+9)"), where a time the formula
// works out on the viewer's clock is written with "Z" too (formulaDates.ts).
function propertyFormulaDate(value: unknown): FormulaValue {
  const date = typeof value === "string" ? value : storedFormulaDate(value);
  if (!date) return "";
  const text = String(date);
  if ((date instanceof FormulaDate && date.zone) || storedTimeZone(text) !== "UTC") return date;
  return new FormulaDate(text, "UTC");
}

// A value the server computed, read back as a formula value: rich text's
// spans, a date, a reference or a list of them.
export function computedFormulaValue(value: unknown): FormulaValue {
  if (Array.isArray(value)) return value.map(computedFormulaValue);
  const spans = value && typeof value === "object" ? (value as { rich_text?: unknown }).rich_text : undefined;
  if (Array.isArray(spans)) return new FormulaRichText(spans);
  return storedFormulaDate(value) ?? value as FormulaValue;
}

// A row page's time as a formula date: its UTC instant to the second.
function timestampFormulaValue(value: unknown): FormulaValue {
  const moment = parseFormulaDate(typeof value === "string" ? value : "", "UTC");
  return moment ? new FormulaDate(momentText(moment)) : "";
}

function listItems(value: unknown) {
  if (Array.isArray(value)) return value;
  if (value == null || value === "") return [];
  return [value];
}

function propertiesForDatabase(
  databaseId: string,
  options: FormulaRollupOptions,
  state: FormulaEvaluationState,
) {
  const loaded = options.propsByDb?.[databaseId];
  if (loaded) {
    admitProperties(loaded, state);
    return loaded;
  }
  admitProperties(options.sourceProps, state);
  admitProperties(options.targetProps, state);
  if (
    options.prop.databaseId === databaseId
    || options.sourceProps.some((property) => property.databaseId === databaseId)
  ) {
    admitProperties(options.sourceProps, state);
    return options.sourceProps;
  }
  if (options.targetProps.some((property) => property.databaseId === databaseId)) {
    admitProperties(options.targetProps, state);
    return options.targetProps;
  }
  return [];
}

// What a rollup reads of a related row's formula: the value its cell works
// out, so a sum adds the numbers the formula gives.
function rollupFormulaTargetValue(
  row: Page, prop: DbProperty, props: DbProperty[],
  { pagesById, propsByDb, bounds }: Pick<FormulaRollupOptions, "pagesById" | "propsByDb" | "bounds">,
  state: FormulaEvaluationState,
): unknown {
  const value = evaluateFormula({ row, prop, props, pagesById, propsByDb, bounds }, state);
  return value == null || typeof value === "number" || typeof value === "boolean" || typeof value === "string"
    ? value : formatFormulaCoreValue(value);
}

/** The same for a rollup worked out away from a cell: a group, a summary or a chart. */
export function rollupFormulaValues(options: Pick<FormulaRollupOptions, "pagesById" | "propsByDb" | "bounds">) {
  const state = newFormulaEvaluationState(options.bounds);
  return (row: Page, prop: DbProperty) => rollupFormulaTargetValue(row, prop, options.propsByDb?.[prop.databaseId] ?? [], options, state);
}

// A rollup's options reading a related row's formula as its cell works it
// out: its value for the calculations, its text where the values are shown.
function rollupOptionsWithFormulas(
  options: FormulaRollupOptions,
  state: FormulaEvaluationState,
): EvaluateRollupOptions {
  const { bounds, ...rollupOptions } = options;
  return {
    ...rollupOptions,
    bounds: bounds.rollup,
    resolveComputedValue: (targetRow, targetProp) => rollupFormulaTargetValue(
      targetRow, targetProp, propertiesForDatabase(targetProp.databaseId, options, state), options, state,
    ),
    resolveComputedDisplay: (targetRow, targetProp) => {
      if (targetProp.type !== "formula") return undefined;
      const props = propertiesForDatabase(targetProp.databaseId, options, state);
      const value = evaluateFormula({
        row: targetRow,
        prop: targetProp,
        props,
        pagesById: options.pagesById,
        propsByDb: options.propsByDb,
        bounds,
      }, state);
      // A date reads as a formula's cell shows it, "2026년 1월 1일", and a list as its cell's, empty text left out, as
      // Notion's rollup shows them (seen 2026-11-12, and 2026-11-19: ["a", "", "b"] "a, b").
      const formatted = typeof value === "number" && formulaHasNumberSettings(targetProp)
        ? formatNumberForProperty(value, { config: (targetProp.config ?? {}) as Record<string, unknown> })
        : Array.isArray(value) ? shownFormulaListText(flatFormulaItems(value).map((item) => formulaItemText(item, bounds)))
          : value instanceof FormulaDate ? formulaItemText(value, bounds) : formatFormulaCoreValue(value);
      consumeString(formatted, state);
      return formatted;
    },
  };
}

function evaluateRollupWithComputedDisplayValue(
  options: FormulaRollupOptions,
  state: FormulaEvaluationState,
) {
  return evaluateRollupValue(rollupOptionsWithFormulas(options, state));
}

/**
 * A rollup showing checkboxes — a checkbox's or a true-or-false formula's
 * values shown as they are, counted per group, or a share of them it doesn't
 * count — as their boxes, as Notion's table, board and page draw them (seen
 * 2026-11-12); null for any other.
 */
export function evaluateRollupCheckboxes(options: FormulaRollupOptions): boolean[] | null {
  const shown = evaluateRollupShownValues(options);
  return shown && rollupCheckboxStates(shown.values, shown.target?.type, rollupShownFunction(options.prop.config));
}

/** The related values a rollup shows as they are, formulas worked out, and their property; null when it shows something else. */
export function evaluateRollupShownValues(options: FormulaRollupOptions) {
  return evaluateRollupOriginals(rollupOptionsWithFormulas(options, newFormulaEvaluationState(options.bounds)));
}

/** A rollup's value for a filter or a sort: a number while it gives one. */
export function evaluateRollupCellValue(options: FormulaRollupOptions) {
  return evaluateRollupWithComputedDisplayValue(options, newFormulaEvaluationState(options.bounds));
}

export function evaluateRollupWithComputedDisplay(options: FormulaRollupOptions): string {
  const state = newFormulaEvaluationState(options.bounds);
  const result = evaluateRollupWithComputedDisplayValue(options, state);
  if (typeof result !== "number") return result;
  const formatted = String(result);
  consumeString(formatted, state);
  return formatted;
}

export type EvaluateFormulaOptions = {
  row: Page;
  prop: DbProperty;
  props: DbProperty[];
  pagesById: Record<string, Page>;
  propsByDb?: Record<string, DbProperty[]>;
  bounds: FormulaBounds;
};

export function evaluateFormula(
  options: EvaluateFormulaOptions,
  existingState?: FormulaEvaluationState,
): FormulaValue {
  const { row, prop, props, pagesById, propsByDb, bounds } = options;
  const state = existingState ?? newFormulaEvaluationState(bounds);
  admitProperties(props, state);
  const key = `${row.id}\u0000${prop.id}`;
  consumeString(key, state);
  if (state.cache.has(key)) return state.cache.get(key)!;
  if (state.active.has(key)) {
    for (const activeKey of state.active) state.cyclic.add(activeKey);
    return "";
  }
  state.evaluations += 1;
  if (state.evaluations > bounds.maxEvaluations) {
    throw new RangeError("Formula evaluations exceed their caller-owned bound.");
  }
  state.active.add(key);

  try {
    const sourceExpression = prop.config?.formula ?? "";
    admitExpression(sourceExpression, state);
    const expression = sourceExpression.trim();
    if (!expression) {
      state.cache.set(key, "");
      return "";
    }
    // A property of this row, or of a related page for page.prop("Name").
    const resolveOn = (page: Page, pageProps: DbProperty[], name: string, selfId?: string): FormulaValue | FormulaPropertyResult => {
      consumeString(name, state);
      const field = formulaPageField(pageProps, name);
      const target: DbProperty | undefined = field
        ? formulaPageFieldProperty(field, pageProps[0]?.databaseId ?? "")
        : resolveFormulaPropertyReference(pageProps, name);
      if (!target || target.id === selfId) return "";
      if (target.type === "formula" || target.type === "rollup") {
        // Another formula's result is read as it is, made empty text too; one
        // that failed fails this one with what failed it, as Notion's (seen
        // 2026-11-14: empty(prop("round(2, 0.5)")) is no value).
        if (target.type === "formula") {
          const value = evaluateFormula({ row: page, prop: target, props: pageProps, pagesById, propsByDb, bounds }, state);
          const failure = state.failures.get(`${page.id}\u0000${target.id}`);
          if (failure !== undefined) throw failure;
          return new FormulaPropertyResult(value);
        }

        const sourceProps = propsByDb?.[target.databaseId] ?? pageProps;
        admitProperties(sourceProps, state);
        const relationProp = sourceProps.find(
          (item) => item.id === target.config?.rollupRelationPropertyId,
        );
        if (!relationProp || relationProp.type !== "relation") return "";
        const relationValue = rawValue(page, relationProp);
        admitFormulaValue(relationValue, state);
        const relationIds: string[] = [];
        for (const item of listItems(relationValue)) {
          const id = String(item ?? "");
          consumeString(id, state);
          if (!id) continue;
          admitPageLookup(id, state);
          relationIds.push(id);
        }
        if (relationIds.some((id) => !pagesById[id])) return "";

        const targetDbId = relationProp.config?.relationDatabaseId ?? relationProp.databaseId;
        const targetProps = propsByDb?.[targetDbId];
        if (targetProps) admitProperties(targetProps, state);
        const rollupFunction = canonicalRollupFunction(target.config?.rollupFunction);
        if (
          rollupFunction !== "count"
          && (!targetProps || !targetProps.some((item) => item.id === target.config?.rollupTargetPropertyId))
        ) return "";
        return evaluateRollupWithComputedDisplayValue({
          row: page,
          prop: target,
          sourceProps,
          targetProps: targetProps ?? [],
          pagesById,
          propsByDb,
          bounds,
        }, state);
      }

      const value = rawValue(page, target);
      admitFormulaValue(value, state);
      if (target.type === "created_time" || target.type === "last_edited_time") return timestampFormulaValue(value);
      if (typeof value === "number" || typeof value === "boolean") return value;
      // A checkbox never set is unchecked, as Notion reads it (seen 2026-10-01).
      if (target.type === "checkbox") return false;
      // An empty value, as an untitled page's title, is empty text, never the
      // word for untitled, as Notion reads it (seen 2026-09-30); a list
      // property never set is an empty list (seen 2026-11-12:
      // prop("Tags").map(current) of none is empty and its length() 0).
      if ((value == null || value === "") && !EMPTY_LIST_TYPES.has(target.type)) return "";
      if (target.type === "number") return value as FormulaValue;
      if (target.type === "multi_select") {
        const output: string[] = [];
        const seen = new Set<string>();
        const options = target.config?.options ?? [];
        if (options.length > bounds.maxOptions) {
          throw new RangeError("Formula options exceed their caller-owned bound.");
        }
        for (const item of listItems(value)) {
          const id = String(item ?? "");
          const name = options.find((option) => option.id === id || option.name === id)?.name ?? id;
          consumeString(name, state);
          if (name && !seen.has(name)) {
            seen.add(name);
            output.push(name);
          }
        }
        return output;
      }
      if (target.type === "relation") {
        const relatedDatabase = target.config?.relationDatabaseId ?? target.databaseId;
        const output: FormulaReference[] = [];
        for (const item of listItems(value)) {
          const id = String(item ?? "");
          if (!id) continue;
          admitPageLookup(id, state);
          const related = pagesById[id];
          if (related) output.push({ ...reference("page", id, related.title?.trim() ?? ""), databaseId: relatedDatabase });
        }
        return output;
      }
      if (["person", "created_by", "last_edited_by"].includes(target.type)) {
        const source = target.type === "created_by"
          ? page.createdBy
          : target.type === "last_edited_by"
            ? page.lastEditedBy
            : value;
        admitFormulaValue(source, state);
        const byId = new Map<string, unknown>();
        for (const item of listItems(source)) {
          const remainingNodes = bounds.maxValueNodes - state.valueNodes;
          const ids = normalizePersonIds(item, {
            maxNodes: Math.min(remainingNodes, bounds.rollup.maxPersonNodes),
            maxDepth: Math.min(bounds.maxValueDepth, bounds.rollup.maxPersonDepth),
            maxEntries: bounds.rollup.maxPeopleEntries,
          });
          for (const id of ids) byId.set(id, item);
        }
        const people = Array.from(byId, ([id, item]) => reference("person", id, personName(item, id)));
        // As Notion's, who made or last edited a page is one person; a
        // person property holds a list.
        return target.type === "person" ? people : people[0] ?? "";
      }
      if (target.type === "date" && (typeof value === "string" || (value && typeof value === "object"))) return propertyFormulaDate(value);
      return displayPropertyValue(page, target, pagesById, bounds.rollup);
    };
    const { value: result, failure } = evaluateFormulaExpressionResult(expression, (name) => resolveOn(row, props, name, prop.id), {
      pageId: row.id,
      resolveRelatedProp: (page, name) => {
        const related = pagesById[page.id];
        const relatedProps = page.databaseId ? propsByDb?.[page.databaseId] : undefined;
        if (!related || !relatedProps) return "";
        admitProperties(relatedProps, state);
        return resolveOn(related, relatedProps, name);
      },
    });
    // Its number rounded as Notion keeps a formula's (keptFormulaValue).
    const value = state.cyclic.has(key) ? "" : keptFormulaValue(result);
    if (failure !== undefined && !state.cyclic.has(key)) state.failures.set(key, failure);
    admitFormulaValue(value, state);
    state.cache.set(key, value);
    return value;
  } finally {
    state.active.delete(key);
  }
}

// evaluateFormulaRun is a formula's value on a row with what kept it from
// working out that its editor names (formulaRunError.ts), or null, and
// whether that is its own or one of a formula it reads, whose place is in
// that formula, as Notion's names it (seen 2026-11-14: prop("A") + 1 of a
// failing A names A's failure and A's place).
export function evaluateFormulaRun(options: EvaluateFormulaOptions) {
  const state = newFormulaEvaluationState(options.bounds);
  const value = evaluateFormula(options, state);
  const key = `${options.row.id}\u0000${options.prop.id}`;
  const failure = state.failures.get(key);
  const run = failure instanceof FormulaRunError ? failure : null;
  const own = run !== null && ![...state.failures].some(([other, failed]) => other !== key && failed === failure);
  return { value, run, own };
}

// A formula's problems the editor names, by code: a function, property or
// name it doesn't know, unmatched brackets, or text the evaluator can't read
// (with the evaluator's message); a name's problem has where the name is
// written, from its first character up to its end, a property's from "prop"
// to its ")".
export type FormulaWarning = {
  code: "unknownFunction" | "unknownProperty" | "unknownIdentifier" | "parentheses" | "brackets" | "syntax"
    | FormulaCallProblem["code"];
  name?: string;
  message?: string;
  start?: number;
  end?: number;
  expected?: number;
  given?: number; at?: number;
};

export function formulaWarnings(
  expression: string | undefined,
  props: DbProperty[],
  bounds: FormulaBounds,
  /** Names the formula may use where it runs (FormulaScopeName). */
  known: ReadonlySet<string> = new Set(),
): FormulaWarning[] {
  const state = newFormulaEvaluationState(bounds);
  const sourceExpression = expression ?? "";
  admitExpression(sourceExpression, state);
  admitProperties(props, state);
  const expr = sourceExpression.trim();
  if (!expr) return [];
  const warnings: FormulaWarning[] = [];
  const names = new Set(props.map((property) => property.name.toLowerCase()));
  const ids = new Set(props.map((property) => property.id));
  const tokens = tokenizeFormula(sourceExpression);
  const variables = formulaVariableNames(tokens);
  const seenWarnings = new Set<string>();
  const pushWarning = (warning: FormulaWarning) => {
    const key = `${warning.code}\u0000${warning.name ?? ""}\u0000${warning.message ?? ""}`;
    if (seenWarnings.has(key)) return;
    consumeString(key, state);
    seenWarnings.add(key);
    warnings.push(warning);
  };

  tokens.forEach((token, index) => {
    if (token.type !== "identifier") return;
    const isCall = tokens[index + 1]?.type === "paren" && tokens[index + 1]?.value === "(";
    if (isCall) {
      // A method's name is checked with the call it makes (formulaCallProblems).
      if (tokens[index - 1]?.type === "dot" && token.value !== "prop") return;
      if (!FORMULA_FUNCTIONS.has(token.value) && token.value !== "true" && token.value !== "false") {
        pushWarning({ code: "unknownFunction", name: token.value, start: token.start, end: token.end });
      }
      if (token.value === "prop") {
        if (tokens[index - 1]?.type === "dot") return;
        const ref = tokens[index + 2]?.type === "string" ? tokens[index + 2]?.value : "";
        if (ref && !resolveFormulaPropertyReference(props, ref) && !formulaPageField(props, ref) &&
          !names.has(ref.toLowerCase()) && !ids.has(ref)) {
          const close = tokens[index + 3];
          const end = close?.type === "paren" && close.value === ")" ? close.end : tokens[index + 2]?.end;
          pushWarning({ code: "unknownProperty", name: ref, start: token.start, end });
        }
      }
    } else if (tokens[index - 1]?.type !== "dot" && !FORMULA_LITERALS.has(token.value) && !variables.has(token.value) && !known.has(token.value)) {
      pushWarning({ code: "unknownIdentifier", name: token.value, start: token.start, end: token.end });
    }
  });

  let depth = 0;
  for (const token of tokens) {
    if (token.type !== "paren") continue;
    if (token.value === "(") depth += 1;
    else if (token.value === ")") depth -= 1;
    if (depth < 0) break;
  }
  if (depth !== 0) pushWarning({ code: "parentheses" });
  let listDepth = 0;
  for (const token of tokens) {
    if (token.type !== "bracket") continue;
    if (token.value === "[") listDepth += 1;
    else if (token.value === "]") listDepth -= 1;
    if (listDepth < 0) break;
  }
  if (listDepth !== 0) pushWarning({ code: "brackets" });
  let tree: FormulaNode | null = null;
  try {
    tree = parseFormulaTokensRecovering(tokens, sourceExpression.length).node;
  } catch {
    // Too deep to be read: the syntax's problem (formulaSyntaxProblems.ts).
  }
  if (tree) for (const problem of formulaCallProblems(tree)) warnings.push(problem);
  if (formulaPropertyReferences(tokens).size === 0) {
    const result = evaluateFormulaExpressionResult(expr, () => "");
    if (result.error) pushWarning({ code: "syntax", message: result.error });
  }
  return warnings;
}

export function formatFormulaValue(value: FormulaValue, bounds: FormulaBounds, untitled = "") {
  const state = newFormulaEvaluationState(bounds);
  admitFormulaValue(value, state);
  const formatted = formatFormulaCoreValue(value, untitled);
  consumeString(formatted, state);
  return formatted;
}

// formulaItemText is one value as a formula cell writes it: a date as a date
// property's cell shows it for the viewer, as Notion's formula cell does (seen
// 2026-11-12: a New York 10:30 shows "2026년 1월 3일 오전 12:30 (GMT+9)", as
// the property holding it), anything else as format() writes it.
// A time the formula keeps on the viewer's clock, written as its UTC instant
// ("…Z"), shows on that clock naming none, as Notion's (parseDate("2026-01-02T10:30")
// shows "2026년 1월 2일 오전 10:30").
export function formulaItemText(item: FormulaValue, bounds: FormulaBounds, untitled = "") {
  if (item instanceof FormulaDate) {
    const split = item.text.indexOf("/");
    const shown = (part: string) => item.zone ? part : instantOnViewerClock(part);
    return formatDate({
      start: shown(split < 0 ? item.text : item.text.slice(0, split)),
      end: split < 0 ? null : shown(item.text.slice(split + 1)),
      ...(item.zone ? { timeZone: item.zone } : {}),
    });
  }
  return formatFormulaValue(item, bounds, untitled);
}

// A formula with a number format, decimal places or display of its own.
function formulaHasNumberSettings(prop: DbProperty) {
  const config = (prop.config ?? {}) as Record<string, unknown>;
  return ["numberFormat", "decimalPlaces", "numberDisplay"].some((key) => config[key] !== undefined && config[key] !== null && config[key] !== "");
}

// formulaDisplay is a formula cell's text, worked out here (local), with
// each untitled related page named in the reader's language, each date
// written as Notion's date cells show it, as format() writes it for the
// viewer, and its spans when the value is rich text or a list holding some;
// a true or false result is a checkbox's state.
export function formulaDisplay(
  _row: Page,
  prop: DbProperty,
  bounds: FormulaBounds,
  untitled: string,
  local: () => FormulaValue,
): FormulaDisplay {
  const value = local();
  // A number takes the formula's number format, decimal places and display
  // when it has them, as a number property's does (Notion's, seen 2026-10-05).
  if (typeof value === "number" && Number.isFinite(value) && formulaHasNumberSettings(prop)) {
    return { text: formatNumberForProperty(value, { config: (prop.config ?? {}) as Record<string, unknown> }), number: value };
  }
  // A list shows its items with lists within it flat and empty ones left out
  // (Notion's, seen 2026-11-12: "[[], [], "x"]" shows "x"), empty text and a number that is
  // no number too (seen 2026-11-14: [1e308 * 10, sqrt(-1), 1] shows "Infinity, 1"; 2026-11-19:
  // ["a", "b", ""] "a, b" — its kind read with them, [true, ""] "true"; shownFormulaListText).
  const items = Array.isArray(value) ? flatFormulaItems(value).filter((item) => !Number.isNaN(item)) : [value];
  // A mention its reader may not open reads in the formula as its marks,
  // which show as the mention says itself (closedMentions.ts).
  const shown = (item: FormulaValue) => lineWithClosedMentions(formulaItemText(item, bounds, untitled));
  const text = lineWithClosedMentions(Array.isArray(value) || value instanceof FormulaDate ? shownFormulaListText(items.map(shown))
    : formatFormulaValue(value, bounds, untitled));
  // People alone, one or a list, show as a person property's people, and
  // pages alone as a relation's pages, as Notion's (seen 2026-11-12).
  const references = value != null ? formulaReferencesOf(items) : null;
  if (references?.[0]?.kind === "person") return { text, people: references.map((person) => person.id) };
  if (references?.[0]?.kind === "page") return { text, pages: references.map((page) => page.id) };
  if (typeof value === "boolean") return { text, checked: value };
  // True or false alone in a list are a checkbox each, as Notion's ("[true,
  // false]" shows ☑☐, seen 2026-11-12); among other items they are words.
  if (Array.isArray(value) && items.length > 0 && items.every((item) => typeof item === "boolean")) {
    return { text, checks: items as boolean[] };
  }
  // A number result is a number however it shows, and so is a list of numbers:
  // a table's cell sets it at its end (Notion's "1, 2, 3", seen 2026-11-07),
  // an infinite one's too ("Infinity", "Infinity, 1", seen 2026-11-14).
  const numbers = Array.isArray(value) ? items.length > 0 && items.every((item) => typeof item === "number")
    : typeof value === "number";
  // Dates alone show as dates; among other items a date is its mention.
  const mentionedDates = items.some((item) => item instanceof FormulaDate) && !items.every((item) => item instanceof FormulaDate);
  if (!mentionedDates && !items.some((item) => item instanceof FormulaRichText || isFormulaReference(item))) {
    return numbers ? { text, numeric: true } : { text };
  }
  // Among other items a person, a page or a date is its mention, as Notion's
  // ("@이름, x", "@2026년 1월 2일, x", seen 2026-11-12).
  const viewer = { timeZone: formulaTimeZone(undefined), locale: formulaLocale() };
  const spans = items.filter((item) => shown(item) !== "").flatMap((item, index): TextSpan[] => [
    ...(index > 0 ? [{ text: ", " }] : []),
    ...(item instanceof FormulaRichText ? item.spans : isFormulaReference(item) ? formulaReferenceSpans(item, untitled)
      : item instanceof FormulaDate ? formulaSpans(item, viewer) : [{ text: shown(item) }]),
  ]);
  return { text, spans };
}

export type FormulaDisplay = {
  text: string;
  spans?: TextSpan[];
  checked?: boolean;
  /** True or false alone in a list, a checkbox each. */
  checks?: boolean[];
  number?: number;
  numeric?: boolean;
  /** People alone, by their IDs, shown as a person property's. */
  people?: string[];
  /** Pages alone, by their IDs, shown as a relation's. */
  pages?: string[];
};

function formulaReferenceSpans(item: FormulaReference, untitled: string): TextSpan[] {
  return item.kind === "person"
    ? [{ text: `@${formulaReferenceText(item)}`, mention: "person", userId: item.id }]
    : [{ text: item.name || untitled, mention: "page", pageId: item.id }];
}
