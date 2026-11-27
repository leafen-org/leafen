import type { DbProperty } from "@/lib/types";
import type { FormulaValue } from "./formulaCore";
import { validateRollupBounds, type RollupBounds } from "./rollupPolicy";

export type FormulaBounds = {
  maxExpressionCharacters: number;
  maxProperties: number;
  maxOptions: number;
  maxPages: number;
  maxEvaluations: number;
  maxValueNodes: number;
  maxValueDepth: number;
  maxStringCharacters: number;
  rollup: RollupBounds;
};

export type FormulaEvaluationState = {
  bounds: FormulaBounds;
  cache: Map<string, FormulaValue>;
  active: Set<string>;
  cyclic: Set<string>;
  /** What kept each formula that failed from working out, by row and property. */
  failures: Map<string, unknown>;
  propertyKeys: Set<string>;
  pageIds: Set<string>;
  evaluations: number;
  valueNodes: number;
  stringCharacters: number;
};

function assertBound(value: number, label: string) {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new RangeError(`${label} requires a caller-owned nonnegative safe-integer bound.`);
  }
}

function validateBounds(bounds: FormulaBounds) {
  for (const [label, value] of [
    ["maxExpressionCharacters", bounds.maxExpressionCharacters],
    ["maxProperties", bounds.maxProperties],
    ["maxOptions", bounds.maxOptions],
    ["maxPages", bounds.maxPages],
    ["maxEvaluations", bounds.maxEvaluations],
    ["maxValueNodes", bounds.maxValueNodes],
    ["maxValueDepth", bounds.maxValueDepth],
    ["maxStringCharacters", bounds.maxStringCharacters],
  ] as const) assertBound(value, label);
  validateRollupBounds(bounds.rollup);
}

export function newFormulaEvaluationState(bounds: FormulaBounds): FormulaEvaluationState {
  validateBounds(bounds);
  return {
    bounds,
    cache: new Map(),
    active: new Set(),
    cyclic: new Set(),
    failures: new Map(),
    propertyKeys: new Set(),
    pageIds: new Set(),
    evaluations: 0,
    valueNodes: 0,
    stringCharacters: 0,
  };
}

export function consumeString(value: string, state: FormulaEvaluationState) {
  state.stringCharacters += value.length;
  if (
    !Number.isSafeInteger(state.stringCharacters)
    || state.stringCharacters > state.bounds.maxStringCharacters
  ) {
    throw new RangeError("Formula strings exceed their caller-owned bound.");
  }
}

export function admitExpression(value: string, state: FormulaEvaluationState) {
  if (value.length > state.bounds.maxExpressionCharacters) {
    throw new RangeError("Formula expression exceeds its caller-owned bound.");
  }
  consumeString(value, state);
}

export function admitProperties(
  properties: readonly DbProperty[],
  state: FormulaEvaluationState,
) {
  if (properties.length > state.bounds.maxProperties) {
    throw new RangeError("Formula properties exceed their caller-owned bound.");
  }
  for (const property of properties) {
    const key = `${property.databaseId}\u0000${property.id}`;
    if (state.propertyKeys.has(key)) continue;
    if (state.propertyKeys.size >= state.bounds.maxProperties) {
      throw new RangeError("Formula properties exceed their caller-owned bound.");
    }
    consumeString(property.databaseId, state);
    consumeString(property.id, state);
    consumeString(property.name, state);
    consumeString(property.type, state);
    state.propertyKeys.add(key);
  }
}

export function admitPageLookup(id: string, state: FormulaEvaluationState) {
  consumeString(id, state);
  if (state.pageIds.has(id)) return;
  if (state.pageIds.size >= state.bounds.maxPages) {
    throw new RangeError("Formula pages exceed their caller-owned bound.");
  }
  state.pageIds.add(id);
}

export function admitFormulaValue(value: unknown, state: FormulaEvaluationState) {
  const ancestors = new Set<object>();
  const visit = (current: unknown, depth: number) => {
    state.valueNodes += 1;
    if (state.valueNodes > state.bounds.maxValueNodes) {
      throw new RangeError("Formula value nodes exceed their caller-owned bound.");
    }
    if (depth > state.bounds.maxValueDepth) {
      throw new RangeError("Formula value depth exceeds its caller-owned bound.");
    }
    if (typeof current === "string") {
      consumeString(current, state);
      return;
    }
    if (!current || typeof current !== "object" || current instanceof Date) return;
    if (ancestors.has(current)) throw new RangeError("Cyclic formula values are not admitted.");
    ancestors.add(current);
    if (Array.isArray(current)) {
      if (current.length > state.bounds.maxValueNodes - state.valueNodes) {
        throw new RangeError("Formula value nodes exceed their caller-owned bound.");
      }
      for (const item of current) visit(item, depth + 1);
    } else {
      for (const key in current) {
        if (!Object.prototype.hasOwnProperty.call(current, key)) continue;
        consumeString(key, state);
        visit((current as Record<string, unknown>)[key], depth + 1);
      }
    }
    ancestors.delete(current);
  };
  visit(value, 0);
}
