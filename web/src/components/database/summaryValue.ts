import { pageNamesNow } from "@/lib/pageNames";
import type { DbProperty, Page } from "@/lib/types";
import {
  evaluateFormula,
  formatFormulaValue,
  rollupFormulaValues,
  type FormulaBounds,
} from "./formula";
import { normalizeFileAttachments } from "./files";
import { formatNotionTimestamp } from "./dateUtils";
import { normalizePersonIds, personLabel } from "./people";
import { evaluateRollupGroupingValue } from "./rollup";
import {
  admitPageLookup,
  admitRollupValue,
  consumeRollupString,
  createRollupBudget,
  reserveValuePieces,
  valueAsIdsWithin,
  type RollupBounds,
  type RollupBudget,
} from "./rollupPolicy";
import { valueAsRead } from "./valueAsRead";

export type SummaryValueBounds = {
  formula: FormulaBounds;
  projection: RollupBounds;
};

function outputScalar(
  value: string,
  bounds: RollupBounds,
  budget: RollupBudget,
) {
  reserveValuePieces(1, bounds, budget);
  consumeRollupString(value, bounds, budget);
  return value;
}

function outputArray(
  values: readonly string[],
  bounds: RollupBounds,
  budget: RollupBudget,
) {
  reserveValuePieces(values.length, bounds, budget);
  for (const value of values) consumeRollupString(value, bounds, budget);
  return [...values];
}

function optionNames(
  prop: DbProperty,
  bounds: RollupBounds,
  budget: RollupBudget,
) {
  const options = prop.config?.options ?? [];
  if (options.length > bounds.maxOptions) {
    throw new RangeError("Summary options exceed their caller-owned bound.");
  }
  reserveValuePieces(options.length, bounds, budget);
  const names = new Map<string, string>();
  for (const option of options) {
    consumeRollupString(option.id, bounds, budget);
    consumeRollupString(option.name, bounds, budget);
    consumeRollupString(option.color, bounds, budget);
    if (option.group) consumeRollupString(option.group, bounds, budget);
    if (!names.has(option.id)) names.set(option.id, option.name);
  }
  return names;
}

function effectiveValue(
  row: Page,
  prop: DbProperty,
  props: DbProperty[],
  pagesById: Record<string, Page>,
  bounds: SummaryValueBounds,
  propsByDb?: Record<string, DbProperty[]>,
) {
  if (prop.type === "title") return row.title;
  if (prop.type === "created_time") return row.createdAt;
  if (prop.type === "last_edited_time") return row.updatedAt;
  if (prop.type === "created_by") return row.createdBy;
  if (prop.type === "last_edited_by") return row.lastEditedBy;
  if (prop.type === "formula") {
    return evaluateFormula({
      row,
      prop,
      props,
      pagesById,
      propsByDb,
      bounds: bounds.formula,
    });
  }
  if (prop.type === "rollup") {
    if (row.properties?.[prop.id] === undefined) {
      const relation = props.find((property) => property.id === prop.config?.rollupRelationPropertyId);
      const targetId = relation?.config?.relationDatabaseId ?? prop.databaseId;
      return evaluateRollupGroupingValue({ row, prop, sourceProps: props,
        targetProps: propsByDb?.[targetId] ?? (targetId === prop.databaseId ? props : []), pagesById, propsByDb,
        resolveComputedValue: rollupFormulaValues({ pagesById, propsByDb, bounds: bounds.formula }), bounds: bounds.projection });
    }
  }
  return valueAsRead(row, prop);
}

export function effectiveSummaryValue(
  row: Page,
  prop: DbProperty,
  props: DbProperty[],
  pagesById: Record<string, Page>,
  bounds: SummaryValueBounds,
  propsByDb?: Record<string, DbProperty[]>,
): unknown {
  const budget = createRollupBudget(bounds.projection);
  const value = effectiveValue(row, prop, props, pagesById, bounds, propsByDb);
  admitRollupValue(value, bounds.projection, budget);
  return value;
}

function displaySummaryValueWithin(
  value: unknown,
  prop: DbProperty,
  pagesById: Record<string, Page>,
  bounds: SummaryValueBounds,
  budget: RollupBudget,
): string | string[] {
  const projection = bounds.projection;
  if (prop.type === "select" || prop.type === "status") {
    admitRollupValue(value, projection, budget);
    const id = value ? String(value) : "";
    consumeRollupString(id, projection, budget);
    return outputScalar(optionNames(prop, projection, budget).get(id) ?? id, projection, budget);
  }
  if (prop.type === "multi_select") {
    const ids = valueAsIdsWithin(value, projection, budget);
    const namesById = optionNames(prop, projection, budget);
    const names: string[] = [];
    const seen = new Set<string>();
    for (const id of ids) {
      const name = namesById.get(id) ?? id;
      if (seen.has(name)) continue;
      seen.add(name);
      names.push(name);
    }
    return outputArray(names, projection, budget);
  }
  if (prop.type === "relation") {
    const ids = valueAsIdsWithin(value, projection, budget);
    const names: string[] = [];
    for (const id of ids) {
      admitPageLookup(id, projection, budget);
      names.push(pageNamesNow.title(pagesById[id]));
    }
    return outputArray(names, projection, budget);
  }
  if (prop.type === "files") {
    admitRollupValue(value, projection, budget);
    const inputCount = Array.isArray(value) ? value.length : value ? 1 : 0;
    if (inputCount > projection.maxFileAttachments) {
      throw new RangeError("Summary files exceed their caller-owned bound.");
    }
    const files = normalizeFileAttachments(value);
    if (files.length > projection.maxFileAttachments) {
      throw new RangeError("Summary files exceed their caller-owned bound.");
    }
    return outputArray(files.map((file) => file.name), projection, budget);
  }
  if (prop.type === "person" || prop.type === "created_by" || prop.type === "last_edited_by") {
    admitRollupValue(value, projection, budget);
    const ids = normalizePersonIds(value, {
      maxNodes: projection.maxPersonNodes,
      maxDepth: projection.maxPersonDepth,
      maxEntries: projection.maxPeopleEntries,
    });
    const labels: string[] = [];
    const seen = new Set<string>();
    for (const id of ids) {
      if (seen.has(id)) continue;
      seen.add(id);
      labels.push(personLabel(id));
    }
    return outputArray(labels, projection, budget);
  }
  if (prop.type === "created_time" || prop.type === "last_edited_time") {
    admitRollupValue(value, projection, budget);
    return outputScalar(formatNotionTimestamp(value), projection, budget);
  }
  if (prop.type === "formula") {
    admitRollupValue(value, projection, budget);
    return outputScalar(
      formatFormulaValue(value as ReturnType<typeof evaluateFormula>, bounds.formula),
      projection,
      budget,
    );
  }
  if (Array.isArray(value)) {
    admitRollupValue(value, projection, budget);
    return outputArray(value.map(String), projection, budget);
  }
  if (value == null) return outputScalar("", projection, budget);
  admitRollupValue(value, projection, budget);
  return outputScalar(String(value), projection, budget);
}

export function summaryValuePieces(
  value: unknown,
  prop: DbProperty,
  pagesById: Record<string, Page>,
  bounds: SummaryValueBounds,
) {
  const budget = createRollupBudget(bounds.projection);
  const display = displaySummaryValueWithin(value, prop, pagesById, bounds, budget);
  const parts = Array.isArray(display) ? display : [display];
  const output: string[] = [];
  for (const part of parts) {
    const normalized = String(part).trim();
    consumeRollupString(normalized, bounds.projection, budget);
    if (!normalized) continue;
    reserveValuePieces(1, bounds.projection, budget);
    output.push(normalized);
  }
  return output;
}

export function summaryValuePresent(
  value: unknown,
  prop: DbProperty,
  pagesById: Record<string, Page>,
  bounds: SummaryValueBounds,
) {
  return summaryValuePieces(value, prop, pagesById, bounds).length > 0;
}
