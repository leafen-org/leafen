import type { StatusStage } from "./propertyStatusPolicy";
import type { GroupPage } from "./GroupPageGlyph";

export type BoardOption = {
  id: string;
  name: string;
  color?: string;
  /** A status option drawn in its group's color, none chosen (SelectOption's). */
  colorFromGroup?: boolean;
  /** A related page's group, titled with a page's glyph. */
  page?: GroupPage;
  /** A status's group, shown by the status's groups: where it stands. */
  statusStage?: StatusStage;
  /** A status's option, shown by its options. */
  statusOption?: boolean;
};

export type BoardBucket<Row> = {
  key: string;
  label: string;
  color?: string;
  /**
   * A status option drawn in its group's color, none chosen: its chip takes
   * the group's color and its column a default one's — wash, cards, "⋯ +"
   * and "+ 새 페이지" — as Notion's board draws a status's "Not started"
   * (seen 2026-11-15: chip gray, "⋯ +" rgb(188,186,182) and cards
   * rgb(32,32,32) as a "default" option's, against a "gray" one's 142 and 44).
   */
  colorFromGroup?: boolean;
  page?: GroupPage;
  statusStage?: StatusStage;
  statusOption?: boolean;
  /** A checkbox's column: whether its box is checked. */
  checked?: boolean;
  rows: Row[];
};

export type BoardProjectionInput<Row> = {
  rows: readonly Row[];
  options: readonly BoardOption[];
  membership(row: Row): readonly string[];
  emptyLabel: string;
  includeEmpty?: boolean;
  /** Whether the group for no value comes first, as a date's does. */
  emptyFirst?: boolean;
};

// A view works over the rows its page's content graph already holds, so
// every count the data sets — rows, pages, properties, options, relation
// ids, people, files, status groups, formula and rollup pages — is
// unbounded: the data is in memory already, and a count past a number would
// only break the view. Kept: one value's nesting depth and the sorts and
// filters a view saves (their growth is refused with a notice), and a
// scalar's length, which a change's bytes bound.
// Every option a property holds (PROPERTY_OPTION_CAPACITY in queryBudget.ts;
// this module stays import-free).
const BOARD_PROPERTY_OPTIONS = 32_768;
const UNBOUNDED = Number.MAX_SAFE_INTEGER;

export const nativeBoardQueryBounds = Object.freeze({
  maxRows: UNBOUNDED,
  maxProperties: UNBOUNDED,
  maxPages: UNBOUNDED,
  // As many sorts, conditions and groups as a view holds (Notion states
  // none); a group in a group in the advanced filter and no deeper, the
  // advanced filter's own counted 0, as Notion's (seen 2026-11-03 and
  // 2026-11-19: three levels; the server's maxDatabaseQueryFilterDepth
  // counts the same three from 1).
  maxSorts: UNBOUNDED,
  maxFilters: UNBOUNDED,
  maxFilterGroups: UNBOUNDED,
  maxFilterDepth: 2,
  maxOptionsPerProperty: BOARD_PROPERTY_OPTIONS,
  maxStatusGroups: UNBOUNDED,
  maxRelationIds: UNBOUNDED,
  maxValueNodes: UNBOUNDED,
  maxValueDepth: 20,
  maxContainerEntries: UNBOUNDED,
  maxScalarCharacters: 8_388_608,
  maxTotalCharacters: UNBOUNDED,
  personValue: { maxNodes: UNBOUNDED, maxDepth: 8, maxEntries: UNBOUNDED },
  formula: {
    maxExpressionCharacters: 4_096,
    maxProperties: UNBOUNDED,
    maxOptions: BOARD_PROPERTY_OPTIONS,
    maxPages: UNBOUNDED,
    // Each formula is read once per row (cached, cycles stop it).
    maxEvaluations: UNBOUNDED,
    maxValueNodes: UNBOUNDED,
    maxValueDepth: 10,
    maxStringCharacters: UNBOUNDED,
    rollup: {
      maxDatabases: UNBOUNDED,
      maxProperties: UNBOUNDED,
      maxPages: UNBOUNDED,
      maxValueNodes: UNBOUNDED,
      maxValueDepth: 10,
      maxValuePieces: UNBOUNDED,
      maxOptions: BOARD_PROPERTY_OPTIONS,
      maxFileAttachments: UNBOUNDED,
      maxPersonNodes: UNBOUNDED,
      maxPersonDepth: 8,
      maxPeopleEntries: UNBOUNDED,
      maxStringCharacters: UNBOUNDED,
    },
  },
});

export const nativeBoardGroupingBounds = Object.freeze({
  maxRows: UNBOUNDED,
  maxMembers: UNBOUNDED,
  maxValues: UNBOUNDED,
  maxStatusOptions: BOARD_PROPERTY_OPTIONS,
  maxStatusGroups: UNBOUNDED,
  maxStatusOptionIds: BOARD_PROPERTY_OPTIONS,
  maxPersonNodes: UNBOUNDED,
  maxPersonDepth: 8,
  maxPeopleEntries: UNBOUNDED,
  maxStringCharacters: UNBOUNDED,
});

function normalizedMembership(values: readonly string[]) {
  const distinct = new Set<string>();
  for (const value of values) {
    const normalized = String(value ?? "").trim();
    if (normalized) distinct.add(normalized);
  }
  return [...distinct];
}

export function boardMoveGroupValue<Value>({
  currentValue,
  replacementValue,
  sourceGroupId,
  targetGroupId,
}: {
  currentValue: Value;
  replacementValue: Value;
  sourceGroupId: string | null;
  targetGroupId: string;
}) {
  return sourceGroupId === targetGroupId ? currentValue : replacementValue;
}

export function reorderBoardOptionsById(
  options: readonly BoardOption[],
  sourceId: string,
  beforeId: string | null,
) {
  const next = options.map((option) => ({ ...option }));
  const sourceIndex = next.findIndex((option) => option.id === sourceId);
  if (sourceIndex < 0) return next;
  const [source] = next.splice(sourceIndex, 1);
  if (!beforeId) {
    next.push(source);
    return next;
  }
  const targetIndex = next.findIndex((option) => option.id === beforeId);
  next.splice(targetIndex < 0 ? next.length : targetIndex, 0, source);
  return next;
}

export function projectBoardColumns<Row>({
  rows,
  options,
  membership,
  emptyLabel,
  includeEmpty = true,
  emptyFirst = false,
}: BoardProjectionInput<Row>): BoardBucket<Row>[] {
  const configured = options.map<BoardBucket<Row>>((option) => ({
    key: option.id,
    label: option.name,
    color: option.color,
    ...(option.colorFromGroup ? { colorFromGroup: true } : {}),
    ...(option.page ? { page: option.page } : {}),
    ...(option.statusStage ? { statusStage: option.statusStage } : {}),
    ...(option.statusOption ? { statusOption: true } : {}),
    rows: [],
  }));
  const configuredById = new Map(configured.map((bucket) => [bucket.key, bucket]));
  const configuredByName = new Map(configured.map((bucket) => [bucket.label, bucket]));
  const unknown: BoardBucket<Row>[] = [];
  const unknownByValue = new Map<string, BoardBucket<Row>>();
  const empty: BoardBucket<Row> = { key: "__none", label: emptyLabel, rows: [] };

  for (const row of rows) {
    const values = normalizedMembership(membership(row));
    if (values.length === 0) {
      empty.rows.push(row);
      continue;
    }
    for (const value of values) {
      let bucket = configuredById.get(value) ?? configuredByName.get(value);
      if (!bucket) {
        bucket = unknownByValue.get(value);
        if (!bucket) {
          bucket = { key: `unknown:${value}`, label: value, rows: [] };
          unknownByValue.set(value, bucket);
          unknown.push(bucket);
        }
      }
      bucket.rows.push(row);
    }
  }
  if (!includeEmpty) return [...configured, ...unknown];
  return emptyFirst ? [empty, ...configured, ...unknown] : [...configured, ...unknown, empty];
}

export function boardInsertionNeighbours<Row extends { id: string }>(
  rows: readonly Row[],
  draggedId: string,
  insertionIndex: number,
) {
  const fromIndex = rows.findIndex((row) => row.id === draggedId);
  const withoutDragged = rows.filter((row) => row.id !== draggedId);
  const adjustedIndex = fromIndex >= 0 && fromIndex < insertionIndex
    ? insertionIndex - 1
    : insertionIndex;
  const boundedIndex = Math.max(0, Math.min(adjustedIndex, withoutDragged.length));
  return {
    afterId: withoutDragged[boundedIndex]?.id ?? null,
    beforeId: withoutDragged[boundedIndex - 1]?.id ?? null,
  };
}
