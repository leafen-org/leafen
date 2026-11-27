import type { DbProperty, DbView } from "@/lib/types";
import { importedListCompletionProperty, type ImportedViewModelBounds } from "./importedViewModel";
import { statusGroupColumns } from "./propertyStatusPolicy";

export type ListHierarchySource = {
  id: string;
  subitemParentId?: string | null;
  subitemChildCount?: number;
  structuralPlaceholder?: boolean;
  __structuralPlaceholder?: true;
};

export type ListHierarchyItem<Row extends ListHierarchySource> = {
  row: Row;
  depth: number;
  hasChildren: boolean;
  childCount: number;
};

/** What the list draws in order: its rows and, after an open row's
 * sub-items, the row that adds one there. */
export type ListHierarchyEntry<Row extends ListHierarchySource> =
  | { kind: "row"; item: ListHierarchyItem<Row> }
  | { kind: "subitemAdd"; parentId: string; depth: number };

export type ListHierarchyInput<Row extends ListHierarchySource> = {
  displayMode: "disabled" | "flattened" | "hidden" | "show";
  expandedRowIds: ReadonlySet<string>;
  rows: readonly Row[];
  subitemsEnabled: boolean;
  /** Whether an open row ends with "New sub-item", as Notion's list (seen 2026-10-11). */
  subitemAdds?: boolean;
};

function isStructuralPlaceholder(row: ListHierarchySource) {
  return row.structuralPlaceholder === true || row.__structuralPlaceholder === true;
}

export function projectListHierarchy<Row extends ListHierarchySource>({
  displayMode,
  expandedRowIds,
  rows,
  subitemsEnabled,
  subitemAdds = false,
}: ListHierarchyInput<Row>) {
  const renderingEnabled = subitemsEnabled && displayMode !== "disabled";
  // Which rows show for the view's sub-item filter option is already chosen
  // (`scopeFilteredSubitems`); nested, they fold under their parents.
  const expandable = renderingEnabled && displayMode === "show";
  const eligible = rows.filter((row) => !isStructuralPlaceholder(row) || expandable);
  const filtered = renderingEnabled && displayMode === "hidden" ? eligible.filter((row) => !row.subitemParentId) : eligible;

  const childCountByParent = new Map<string, number>();
  for (const row of rows) {
    if (row.subitemParentId) {
      childCountByParent.set(
        row.subitemParentId,
        (childCountByParent.get(row.subitemParentId) ?? 0) + 1,
      );
    }
  }
  for (const row of rows) {
    if (
      typeof row.subitemChildCount === "number"
      && Number.isSafeInteger(row.subitemChildCount)
      && row.subitemChildCount >= 0
    ) childCountByParent.set(row.id, row.subitemChildCount);
  }

  const shownIds = new Set(filtered.map((row) => row.id));
  const childrenByParent = new Map<string, Row[]>();
  const roots: Row[] = [];
  for (const row of filtered) {
    const parentId = expandable
      && row.subitemParentId
      && shownIds.has(row.subitemParentId)
      ? row.subitemParentId
      : undefined;
    if (!parentId) roots.push(row);
    else childrenByParent.set(parentId, [...(childrenByParent.get(parentId) ?? []), row]);
  }

  const items: ListHierarchyItem<Row>[] = [];
  const entries: ListHierarchyEntry<Row>[] = [];
  const visited = new Set<string>();
  type Frame = { row: Row; depth: number } | { addTo: string; depth: number };
  const stack: Frame[] = roots.map((row) => ({ row, depth: 0 })).reverse();
  while (stack.length) {
    const current = stack.pop();
    if (!current) continue;
    if ("addTo" in current) {
      entries.push({ kind: "subitemAdd", parentId: current.addTo, depth: current.depth });
      continue;
    }
    if (visited.has(current.row.id)) continue;
    visited.add(current.row.id);
    const childCount = childCountByParent.get(current.row.id) ?? 0;
    const item = {
      ...current,
      childCount,
      hasChildren: expandable && (isStructuralPlaceholder(current.row) || childCount > 0),
    };
    items.push(item);
    entries.push({ kind: "row", item });
    if (!expandedRowIds.has(current.row.id)) continue;
    // A row opened without sub-items still ends with the row that adds one.
    if (expandable && subitemAdds && !isStructuralPlaceholder(current.row)) stack.push({ addTo: current.row.id, depth: current.depth + 1 });
    const children = childrenByParent.get(current.row.id) ?? [];
    for (let index = children.length - 1; index >= 0; index -= 1) {
      stack.push({ row: children[index], depth: current.depth + 1 });
    }
  }
  return { expandable, items, entries };
}

export type ListOperationContext = {
  requestKey: string;
  signal: AbortSignal;
};

export type ListOperationHandlers<Context extends ListOperationContext, Result> = {
  isCurrent(context: Context): boolean;
  onError(error: unknown, context: Context): void;
  perform(context: Context): Promise<Result>;
  publish(result: Result, context: Context): void;
};

/**
 * A view's operations by request key. A read, or a press that makes
 * something and moves on (a row, a property, a template), runs once per key
 * while it is current (`run`): pressed again before it ends, the press is
 * the same one. A write (`write`) is never refused nor held for another of
 * its key still out: it shows at once and goes as made, and the outbox sends
 * a page's writes in the order made, so one made before the last is answered
 * builds on it and both are kept (2026-11-19, at a 5 s round trip: a board's
 * second group's "숨기기" waited 5.6 s for the first group's, and a second
 * card sent to the trash right after the first stayed, its press dropped).
 * Either answers the screen only while the context it was made in is current.
 */
export function createListOperationCoordinator() {
  const pending = new Map<string, ListOperationContext>();
  async function settle<Context extends ListOperationContext, Result>(context: Context, operations: ListOperationHandlers<Context, Result>) {
    try {
      const result = await operations.perform(context);
      if (operations.isCurrent(context) && !context.signal.aborted) {
        operations.publish(result, context);
      }
    } catch (error) {
      if (operations.isCurrent(context) && !context.signal.aborted) {
        operations.onError(error, context);
      }
    }
  }
  return Object.freeze({
    async run<Context extends ListOperationContext, Result>(
      context: Context,
      operations: ListOperationHandlers<Context, Result>,
    ) {
      const current = pending.get(context.requestKey);
      if (current && !current.signal.aborted && operations.isCurrent(current as Context)) {
        return false;
      }
      pending.set(context.requestKey, context);
      try {
        await settle(context, operations);
      } finally {
        if (pending.get(context.requestKey) === context) pending.delete(context.requestKey);
      }
      return true;
    },
    async write<Context extends ListOperationContext, Result>(
      context: Context,
      operations: ListOperationHandlers<Context, Result>,
    ) {
      await settle(context, operations);
      return true;
    },
  });
}

/** The status property an imported Notion list checks off, with the status
 * a check sets and those that count as done. */
export function listCompletionBinding(props: DbProperty[], view: DbView, bounds: ImportedViewModelBounds) {
  const property = importedListCompletionProperty(props, view, bounds);
  const groups = property ? statusGroupColumns(property.config) : [];
  const incompleteStatusId = groups[0]?.optionIds[0];
  const completeGroup = groups.at(-1);
  const completeStatusId = completeGroup?.optionIds[0];
  return property && groups.length >= 2 && incompleteStatusId && completeGroup && completeStatusId
    ? { property, incompleteStatusId, completeStatusId, completeStatusIds: new Set(completeGroup.optionIds) }
    : undefined;
}

/**
 * A list row's properties on each side of its title, in the view's order:
 * those the view lists before its title show before it, the rest at the
 * row's end, as Notion's list (seen 2026-11-12: a view the API gave status,
 * URL, select and number without its title reads them in that order before
 * the title, which the view keeps after them; given the title second, the
 * status before it and the rest at the end). The row's checkbox property
 * shows as its checkbox instead.
 */
export function listRowSides(visible: readonly DbProperty[], completionPropertyId?: string) {
  const titleAt = visible.findIndex((property) => property.type === "title");
  const beside = (property: DbProperty) => property.type !== "title" && property.id !== completionPropertyId;
  return { leading: visible.slice(0, Math.max(titleAt, 0)).filter(beside), others: visible.slice(titleAt + 1).filter(beside) };
}
