export type RollupBounds = {
  maxDatabases: number;
  maxProperties: number;
  maxPages: number;
  maxValueNodes: number;
  maxValueDepth: number;
  maxValuePieces: number;
  maxOptions: number;
  maxFileAttachments: number;
  maxPersonNodes: number;
  maxPersonDepth: number;
  maxPeopleEntries: number;
  maxStringCharacters: number;
};

export type RollupBudget = {
  valueNodes: number;
  valuePieces: number;
  stringCharacters: number;
  pageIds: Set<string>;
  databaseIds: Set<string>;
  propertyKeys: Set<string>;
};

const NOTION_ROLLUP_FUNCTIONS = [
  "count",
  "count_values",
  "empty",
  "not_empty",
  "unique",
  "show_unique",
  "percent_empty",
  "percent_not_empty",
  "sum",
  "average",
  "median",
  "min",
  "max",
  "range",
  "earliest_date",
  "latest_date",
  "date_range",
  "checked",
  "unchecked",
  "percent_checked",
  "percent_unchecked",
  "count_per_group",
  "percent_per_group",
  "show_original",
] as const;

const LEGACY_ROLLUP_FUNCTION_ALIASES = {
  count_all: "count",
  count_empty: "empty",
  count_unique: "unique",
} as const;

export type NotionRollupFunction = (typeof NOTION_ROLLUP_FUNCTIONS)[number];
export type RollupResult = string | number;

const NOTION_ROLLUP_FUNCTION_SET = new Set<string>(NOTION_ROLLUP_FUNCTIONS);
const COMMON_ROLLUP_FUNCTIONS = [
  "show_original", "show_unique", "count", "count_values", "unique",
  "empty", "not_empty", "percent_empty", "percent_not_empty",
] as const satisfies readonly NotionRollupFunction[];
const NUMBER_ROLLUP_FUNCTIONS = [
  ...COMMON_ROLLUP_FUNCTIONS, "sum", "average", "median", "min", "max", "range",
] as const satisfies readonly NotionRollupFunction[];
const DATE_ROLLUP_FUNCTIONS = [
  ...COMMON_ROLLUP_FUNCTIONS, "earliest_date", "latest_date", "date_range",
] as const satisfies readonly NotionRollupFunction[];
// A checkbox's rollup counts its boxes, checked or not, as Notion's
// (seen 2026-10-10): no unique values and no empty counts.
const CHECKBOX_ROLLUP_FUNCTIONS = [
  "show_original", "count", "checked", "unchecked",
  "percent_checked", "percent_unchecked",
] as const satisfies readonly NotionRollupFunction[];

// A status's rollup shows its values or counts them, all or in a status
// group, or takes a group's share, as Notion's menu (seen 2026-11-12).
const STATUS_ROLLUP_MENU = [
  "show_original", "count", "count_per_group", "percent_per_group",
] as const satisfies readonly NotionRollupFunction[];

const EDITABLE_ROLLUP_FUNCTIONS = [
  ...NUMBER_ROLLUP_FUNCTIONS,
  "earliest_date", "latest_date", "date_range",
  "checked", "unchecked", "percent_checked", "percent_unchecked",
] as const satisfies readonly NotionRollupFunction[];

function assertBound(value: number, label: string) {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new RangeError(`${label} requires a caller-owned nonnegative safe-integer bound.`);
  }
}

export function validateRollupBounds(bounds: RollupBounds) {
  for (const [label, value] of [
    ["maxDatabases", bounds.maxDatabases],
    ["maxProperties", bounds.maxProperties],
    ["maxPages", bounds.maxPages],
    ["maxValueNodes", bounds.maxValueNodes],
    ["maxValueDepth", bounds.maxValueDepth],
    ["maxValuePieces", bounds.maxValuePieces],
    ["maxOptions", bounds.maxOptions],
    ["maxFileAttachments", bounds.maxFileAttachments],
    ["maxPersonNodes", bounds.maxPersonNodes],
    ["maxPersonDepth", bounds.maxPersonDepth],
    ["maxPeopleEntries", bounds.maxPeopleEntries],
    ["maxStringCharacters", bounds.maxStringCharacters],
  ] as const) assertBound(value, label);
}

export function createRollupBudget(bounds: RollupBounds): RollupBudget {
  validateRollupBounds(bounds);
  return {
    valueNodes: 0,
    valuePieces: 0,
    stringCharacters: 0,
    pageIds: new Set(),
    databaseIds: new Set(),
    propertyKeys: new Set(),
  };
}

export function reserveStringCharacters(
  length: number,
  bounds: RollupBounds,
  budget: RollupBudget,
) {
  if (!Number.isSafeInteger(length) || length < 0) {
    throw new RangeError("Invalid rollup string length.");
  }
  budget.stringCharacters += length;
  if (
    !Number.isSafeInteger(budget.stringCharacters)
    || budget.stringCharacters > bounds.maxStringCharacters
  ) {
    throw new RangeError("Rollup strings exceed their caller-owned bound.");
  }
}

export function consumeRollupString(
  value: string,
  bounds: RollupBounds,
  budget: RollupBudget,
) {
  reserveStringCharacters(value.length, bounds, budget);
}

export function reserveValuePieces(
  count: number,
  bounds: RollupBounds,
  budget: RollupBudget,
) {
  if (!Number.isSafeInteger(count) || count < 0) throw new RangeError("Invalid rollup piece count.");
  budget.valuePieces += count;
  if (!Number.isSafeInteger(budget.valuePieces) || budget.valuePieces > bounds.maxValuePieces) {
    throw new RangeError("Rollup value pieces exceed their caller-owned bound.");
  }
}

export function admitRollupValue(
  value: unknown,
  bounds: RollupBounds,
  budget: RollupBudget,
) {
  const ancestors = new Set<object>();
  const visit = (current: unknown, depth: number) => {
    budget.valueNodes += 1;
    if (budget.valueNodes > bounds.maxValueNodes) {
      throw new RangeError("Rollup value nodes exceed their caller-owned bound.");
    }
    if (depth > bounds.maxValueDepth) {
      throw new RangeError("Rollup value depth exceeds its caller-owned bound.");
    }
    if (typeof current === "string") {
      consumeRollupString(current, bounds, budget);
      return;
    }
    if (!current || typeof current !== "object" || current instanceof Date) return;
    if (ancestors.has(current)) throw new RangeError("Cyclic rollup values are not admitted.");
    ancestors.add(current);
    if (Array.isArray(current)) {
      if (current.length > bounds.maxValueNodes - budget.valueNodes) {
        throw new RangeError("Rollup value nodes exceed their caller-owned bound.");
      }
      for (const item of current) visit(item, depth + 1);
    } else {
      for (const key in current) {
        if (!Object.prototype.hasOwnProperty.call(current, key)) continue;
        consumeRollupString(key, bounds, budget);
        visit((current as Record<string, unknown>)[key], depth + 1);
      }
    }
    ancestors.delete(current);
  };
  visit(value, 0);
}

export function valueAsIdsWithin(
  value: unknown,
  bounds: RollupBounds,
  budget: RollupBudget,
): string[] {
  const values = Array.isArray(value) ? value : value ? [value] : [];
  if (values.length > bounds.maxValuePieces - budget.valuePieces) {
    throw new RangeError("Rollup value pieces exceed their caller-owned bound.");
  }
  const ids: string[] = [];
  for (const item of values) {
    admitRollupValue(item, bounds, budget);
    const id = String(item);
    consumeRollupString(id, bounds, budget);
    reserveValuePieces(1, bounds, budget);
    ids.push(id);
  }
  return ids;
}

export function admitPageLookup(id: string, bounds: RollupBounds, budget: RollupBudget) {
  consumeRollupString(id, bounds, budget);
  if (budget.pageIds.has(id)) return;
  if (budget.pageIds.size >= bounds.maxPages) {
    throw new RangeError("Rollup pages exceed their caller-owned bound.");
  }
  budget.pageIds.add(id);
}

export function admitDatabaseLookup(id: string, bounds: RollupBounds, budget: RollupBudget) {
  consumeRollupString(id, bounds, budget);
  if (budget.databaseIds.has(id)) return;
  if (budget.databaseIds.size >= bounds.maxDatabases) {
    throw new RangeError("Rollup databases exceed their caller-owned bound.");
  }
  budget.databaseIds.add(id);
}

export function admitProperties(
  properties: readonly { id: string; databaseId: string; type: string }[],
  bounds: RollupBounds,
  budget: RollupBudget,
) {
  if (properties.length > bounds.maxProperties) {
    throw new RangeError("Rollup properties exceed their caller-owned bound.");
  }
  for (const property of properties) {
    consumeRollupString(property.id, bounds, budget);
    consumeRollupString(property.databaseId, bounds, budget);
    consumeRollupString(property.type, bounds, budget);
    const key = `${property.databaseId}\u0000${property.id}`;
    if (budget.propertyKeys.has(key)) continue;
    if (budget.propertyKeys.size >= bounds.maxProperties) {
      throw new RangeError("Rollup properties exceed their caller-owned bound.");
    }
    budget.propertyKeys.add(key);
  }
}

export function canonicalRollupFunction(value: unknown): NotionRollupFunction | undefined {
  if (typeof value !== "string") return undefined;
  const alias = LEGACY_ROLLUP_FUNCTION_ALIASES[value as keyof typeof LEGACY_ROLLUP_FUNCTION_ALIASES];
  const normalized = alias ?? value;
  return NOTION_ROLLUP_FUNCTION_SET.has(normalized)
    ? normalized as NotionRollupFunction
    : undefined;
}

export function rollupFunctionsForPropertyType(
  propertyType: string | undefined,
): readonly NotionRollupFunction[] {
  if (propertyType === "number") return NUMBER_ROLLUP_FUNCTIONS;
  if (["date", "created_time", "last_edited_time", "last_visited_time"].includes(propertyType ?? "")) {
    return DATE_ROLLUP_FUNCTIONS;
  }
  if (propertyType === "checkbox") return CHECKBOX_ROLLUP_FUNCTIONS;
  if (propertyType === "formula" || propertyType === "rollup") return EDITABLE_ROLLUP_FUNCTIONS;
  return COMMON_ROLLUP_FUNCTIONS;
}

// A rollup's calculate menu, as Notion's (seen 2026-10-10): the
// calculations chosen on the spot, then the table calculate menu's groups —
// counts, percents and, for numbers "추가 옵션", for dates "날짜" — holding
// what the target's type offers, in Notion's order.
const ROLLUP_CALCULATION_GROUPS = [
  { key: "count", items: ["count", "count_values", "unique", "empty", "not_empty", "checked", "unchecked", "count_per_group"] },
  { key: "percent", items: ["percent_empty", "percent_not_empty", "percent_checked", "percent_unchecked", "percent_per_group"] },
  { key: "more", items: ["sum", "average", "median", "min", "max", "range"] },
  { key: "date", items: ["earliest_date", "latest_date", "date_range"] },
] as const satisfies readonly { key: string; items: readonly NotionRollupFunction[] }[];

export function rollupCalculationChoices(propertyType: string | undefined) {
  const offered = new Set<NotionRollupFunction>(propertyType === "status" ? STATUS_ROLLUP_MENU : rollupFunctionsForPropertyType(propertyType));
  return {
    leading: (["show_original", "show_unique"] as const).filter((fn) => offered.has(fn)),
    groups: ROLLUP_CALCULATION_GROUPS
      .map((group) => ({ key: group.key, items: group.items.filter((fn) => offered.has(fn)) }))
      .filter((group) => group.items.length > 0),
  };
}

/** The status group a percent per group counts, when it names one. */
export function rollupStatusGroupOf(config: { rollupFunction?: unknown; rollupStatusGroup?: unknown } | null | undefined): string | undefined {
  const group = config?.rollupFunction === "percent_per_group" ? config.rollupStatusGroup : undefined;
  return typeof group === "string" && group ? group : undefined;
}

/** The status group a count per group counts, when it names one. */
export function rollupGroupCountOf(config: { rollupFunction?: unknown; rollupStatusGroup?: unknown } | null | undefined): string | undefined {
  const group = config?.rollupFunction === "count_per_group" ? config.rollupStatusGroup : undefined;
  return typeof group === "string" && group ? group : undefined;
}

/**
 * The calculation a rollup's cell shows by: one counting its values per group
 * shows the values themselves, as Notion's table, board and page show them
 * (seen 2026-11-12: "A B" chips, "1, 2"), unless it names a status group,
 * whose count ("2/4") or share it shows.
 */
export function rollupShownFunction(
  config: { rollupFunction?: unknown; rollupStatusGroup?: unknown } | null | undefined,
): NotionRollupFunction | undefined {
  const fn = canonicalRollupFunction(config?.rollupFunction);
  if (fn === "count_per_group" && !rollupGroupCountOf(config)) return "show_original";
  if (fn === "percent_per_group" && !rollupStatusGroupOf(config)) return "show_original";
  return fn;
}

export function rollupFunctionSupportedForPropertyType(
  fn: NotionRollupFunction,
  propertyType: string | undefined,
) {
  return rollupFunctionsForPropertyType(propertyType).includes(fn);
}

// The counts of values: what count_values, not_empty, empty, unique and the
// shares of empty and not empty count.
const VALUE_COUNTS = new Set<NotionRollupFunction>([
  "count_values", "empty", "not_empty", "unique", "percent_empty", "percent_not_empty",
]);

/**
 * Whether the counts of values count a related value: a checkbox, ticked or
 * not, or a formula's true or false is neither a value nor empty to them, as
 * Notion's (seen 2026-11-12: 0 for each count, and a share shows the boxes).
 */
export function rollupCountsValue(value: unknown, targetType: string | undefined) {
  return targetType !== "checkbox" && typeof value !== "boolean";
}

/**
 * A calculation over a checkbox Notion's menu doesn't offer but its API sets
 * and its table shows: a count of values, or its values each once.
 */
export function rollupCheckboxTakes(fn: NotionRollupFunction, targetType: string | undefined) {
  return targetType === "checkbox" && (VALUE_COUNTS.has(fn) || fn === "show_unique");
}

/**
 * The boxes of a rollup showing checkboxes: a checkbox's values, ticked or
 * not, or a formula's when every one is true or false, each once for
 * "고유한 값 표시"; null for any other values or none.
 */
export function rollupCheckboxStates(values: readonly unknown[], targetType: string | undefined, fn: NotionRollupFunction | undefined) {
  if (!values.length || (targetType !== "checkbox" && !values.every((value) => typeof value === "boolean"))) return null;
  const states = values.map((value) => value === true || rollupCheckedValue(value));
  return fn === "show_unique" ? [...new Set(states)] : states;
}

export function valueIsPresent(value: unknown) {
  return Array.isArray(value) ? value.length > 0 : value !== null && value !== undefined && value !== "";
}

/** A status group's share: always two places, "0.00%", as Notion's (seen 2026-11-04). */
export function rollupGroupPercent(count: number, total: number) {
  return `${((count / total) * 100).toFixed(2)}%`;
}

export function rollupPercent(count: number, total: number) {
  if (!total) return "0%";
  // With two decimal places, as Notion's (seen 2026-11-04: 100.00%).
  return `${((count / total) * 100).toFixed(2)}%`;
}

export function rollupCheckedValue(value: unknown) {
  if (value === true) return true;
  return typeof value === "string"
    && ["true", "1", "yes", "checked"].includes(value.trim().toLowerCase());
}

export function rollupValuePiecesFromAdmitted(
  value: unknown,
  bounds: RollupBounds,
  budget: RollupBudget,
): string[] {
  if (Array.isArray(value)) return value.flatMap((item) => rollupValuePiecesFromAdmitted(item, bounds, budget));
  if (!valueIsPresent(value)) return [];
  const rendered = typeof value === "object" ? JSON.stringify(value) : String(value);
  if (typeof rendered === "string") consumeRollupString(rendered, bounds, budget);
  reserveValuePieces(1, bounds, budget);
  return [rendered as string];
}

// The dates read from a moment with its zone — a page's created or edited
// time — whose day is the viewer's, as Notion shows it (seen 2026-11-12:
// "11월 12일" at 00:58 in Seoul for 15:58 UTC the day before).
const MOMENTS = new WeakSet<Date>();
// The dates written as a day alone, with no time; any other keeps its time,
// at midnight too (a time in Seoul at 09:00 is midnight in UTC).
const DAYS = new WeakSet<Date>();

function rollupDate(value: unknown) {
  const source = String(value ?? "");
  const slash = source.indexOf("/");
  const raw = (slash >= 0 ? source.slice(0, slash) : source).trim();
  if (!raw) return null;
  if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})$/iu.test(raw)) {
    const moment = new Date(raw);
    if (!Number.isNaN(moment.getTime())) {
      MOMENTS.add(moment);
      return moment;
    }
  }
  const match = /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2}))?)?/.exec(raw);
  if (!match) {
    const parsed = new Date(raw);
    return Number.isNaN(parsed.getTime()) ? null : parsed;
  }
  const values = match.slice(1, 7).map((part, index) => Number(part ?? (index < 3 ? 1 : 0)));
  const [year, month, day, hour, minute, second] = values;
  const date = new Date(Date.UTC(year, month - 1, day, hour, minute, second));
  if (
    Number.isNaN(date.getTime())
    || date.getUTCFullYear() !== year
    || date.getUTCMonth() !== month - 1
    || date.getUTCDate() !== day
    || date.getUTCHours() !== hour
    || date.getUTCMinutes() !== minute
    || date.getUTCSeconds() !== second
  ) return null;
  if (match[4] === undefined) DAYS.add(date);
  return date;
}

/** Whether a rollup's date was written as a day alone, with no time. */
export function rollupDateIsDay(date: Date) {
  return DAYS.has(date);
}

export function rollupDateValuesFromAdmitted(
  value: unknown,
  bounds: RollupBounds,
  budget: RollupBudget,
): Date[] {
  const rawValues = Array.isArray(value)
    ? value.flatMap((item) => rollupDateValuesFromAdmitted(item, bounds, budget))
    : value && typeof value === "object" && !(value instanceof Date)
      ? [(value as { start?: unknown }).start, (value as { end?: unknown }).end]
        .flatMap((item) => rollupDateValuesFromAdmitted(item, bounds, budget))
      : value
        ? (() => {
            const source = String(value);
            const dates: Date[] = [];
            let start = 0;
            while (start <= source.length) {
              const slash = source.indexOf("/", start);
              const end = slash >= 0 ? slash : source.length;
              const part = source.slice(start, end);
              consumeRollupString(part, bounds, budget);
              const date = rollupDate(part);
              if (date) {
                reserveValuePieces(1, bounds, budget);
                dates.push(date);
              }
              if (slash < 0) break;
              start = slash + 1;
            }
            return dates;
          })()
        : [];
  return rawValues.sort((left, right) => left.getTime() - right.getTime());
}

/** A date's day: a moment's in the viewer's time zone, a day's as written. */
export function rollupDateKey(date: Date) {
  const moment = MOMENTS.has(date);
  return [
    moment ? date.getFullYear() : date.getUTCFullYear(),
    String((moment ? date.getMonth() : date.getUTCMonth()) + 1).padStart(2, "0"),
    String(moment ? date.getDate() : date.getUTCDate()).padStart(2, "0"),
  ].join("-");
}
