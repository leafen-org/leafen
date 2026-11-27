import type { DatabaseSubitemsFeature } from "@/lib/types";

export type SubitemFilterScope = "parents" | "parents_and_subitems" | "subitems";
export type SubitemDisplayMode = "disabled" | "flattened" | "hidden" | "show";

type SubitemsConfig = { subtasks?: { displayMode?: unknown; filterScope?: unknown } } | undefined;

/** A view's sub-item filter option: "상위 항목만" until another is chosen, as Notion's (seen 2026-10-11). */
export function subitemFilterScope(config: SubitemsConfig): SubitemFilterScope {
  const scope = config?.subtasks?.filterScope;
  return scope === "parents_and_subitems" || scope === "subitems" ? scope : "parents";
}

export function subitemDisplayMode(config: SubitemsConfig): SubitemDisplayMode {
  const mode = config?.subtasks?.displayMode;
  return mode === "disabled" || mode === "flattened" || mode === "hidden" ? mode : "show";
}

/**
 * The filter a board, gallery or calendar keeps for its sub-item option: its
 * parent property "비어 있음" while it shows the top rows — as a card property,
 * or a flattened list's parents — and "비어 있지 않음" for a flattened list's
 * sub-items only. Notion shows it by the filter icon's color alone, with no
 * pill and no name in the view's settings (seen 2026-11-05).
 */
export function cardSubitemFilter(
  view: { type: string; config?: object },
  subitems: { enabled?: unknown; parentPropertyId?: unknown } | null | undefined,
): { propertyId: string; operator: "is_empty" | "is_not_empty" } | null {
  if (subitems?.enabled !== true || typeof subitems.parentPropertyId !== "string") return null;
  if (view.type !== "board" && view.type !== "gallery" && view.type !== "calendar") return null;
  const config = view.config as SubitemsConfig;
  const mode = subitemDisplayMode(config), scope = subitemFilterScope(config);
  if (mode === "show" || mode === "hidden" || (mode === "flattened" && scope === "parents")) return { propertyId: subitems.parentPropertyId, operator: "is_empty" };
  return mode === "flattened" && scope === "subitems" ? { propertyId: subitems.parentPropertyId, operator: "is_not_empty" } : null;
}

/** The database's sub-items as its toolbar settings name them, when on. */
export function toolbarSubitemsFeature(features: { revision?: unknown; subitems?: Record<string, unknown> | null } | null): DatabaseSubitemsFeature | null {
  const subitems = features?.subitems;
  if (subitems?.enabled !== true || typeof subitems.parentPropertyId !== "string" || typeof subitems.subitemsPropertyId !== "string") return null;
  return {
    enabled: true, parentPropertyId: subitems.parentPropertyId, childrenPropertyId: subitems.subitemsPropertyId,
    revision: typeof features?.revision === "number" ? features.revision : 0,
    showToggleOnTitle: subitems.showToggleOnTitle === true,
  };
}

/** A view's settings without its filters and search: every row, in its order. */
function withoutViewFilters<Config extends Record<string, unknown>>(config: Config): Config {
  return { ...config, filterGroup: undefined, filters: undefined, quickFilters: undefined, search: undefined };
}

type Row = { id: string; subitemParentId?: string | null };

/**
 * The rows a filtered view shows with sub-items, as Notion's (seen
 * 2026-10-11), from every row in the view's order and those its filter
 * keeps. Nested: "상위 항목만" shows each kept top row with all its
 * sub-items; "상위 항목 및 하위 항목" each kept row; "하위 항목만" each kept
 * sub-item — a row shown for one it holds or that holds it, but not kept,
 * fades. Merged: the kept top rows, the kept rows, or the kept sub-items;
 * with sub-items hidden, the kept top rows.
 */
export function scopeFilteredSubitems<R extends Row>({ ordered, kept, displayMode, scope }: {
  ordered: readonly R[];
  kept: ReadonlySet<string>;
  displayMode: SubitemDisplayMode;
  scope: SubitemFilterScope;
}): { rows: R[]; faded: ReadonlySet<string> } {
  const byId = new Map(ordered.map((row) => [row.id, row]));
  const parentOf = (row: R) => row.subitemParentId && byId.has(row.subitemParentId) ? row.subitemParentId : null;
  const isTop = (row: R) => !parentOf(row);
  const shown = new Set<string>();
  if (displayMode === "show") {
    const childrenOf = new Map<string, string[]>();
    for (const row of ordered) {
      const parent = parentOf(row);
      if (parent) childrenOf.set(parent, [...(childrenOf.get(parent) ?? []), row.id]);
    }
    for (const row of ordered) {
      if (!kept.has(row.id)) continue;
      if (scope === "parents") {
        if (!isTop(row)) continue;
        const queue = [row.id];
        while (queue.length) {
          const id = queue.pop()!;
          if (shown.has(id)) continue;
          shown.add(id);
          queue.push(...(childrenOf.get(id) ?? []));
        }
      } else if (scope === "parents_and_subitems" || !isTop(row)) {
        // The row and the rows above it, up to its top row.
        for (let id: string | null = row.id, steps = 0; id && !shown.has(id) && steps <= ordered.length; steps += 1) {
          shown.add(id);
          id = parentOf(byId.get(id)!);
        }
      }
    }
  } else {
    for (const row of ordered) {
      if (!kept.has(row.id)) continue;
      const keep = displayMode === "hidden" ? isTop(row)
        : displayMode === "flattened" ? scope === "parents" ? isTop(row) : scope === "subitems" ? !isTop(row) : true
        : true;
      if (keep) shown.add(row.id);
    }
  }
  const rows = ordered.filter((row) => shown.has(row.id));
  return { rows, faded: new Set(rows.filter((row) => !kept.has(row.id)).map((row) => row.id)) };
}

/**
 * A view's rows as `read` gives them with its settings — filtered and
 * sorted — and, where some are sub-items, those around them its sub-item
 * option shows, read again without its filters for their order.
 */
export function readScopedRows<R extends Row, Config>(rows: readonly R[], config: Config, read: (config: Config) => R[]) {
  const kept = read(config);
  if (!rows.some((row) => !!row.subitemParentId)) return { rows: kept, faded: new Set<string>() as ReadonlySet<string> };
  const options = config as SubitemsConfig;
  return scopeFilteredSubitems({
    ordered: read(withoutViewFilters((config ?? {}) as Record<string, unknown>) as Config), kept: new Set(kept.map((row) => row.id)),
    displayMode: subitemDisplayMode(options), scope: subitemFilterScope(options),
  });
}
