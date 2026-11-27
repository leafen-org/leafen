import type { GraphView, GraphViewType } from "@/contentGraph";
import type { FilterGroup, ViewFilter } from "@/lib/types";
import { filterAwaitsValue } from "@/shared/database/queryFilterPolicy";
import type { QueryFilter } from "@/shared/database/queryTypes";
import { withFilterAdded, withGroupAdded } from "./filterGroupTerms";

export type DatabaseToolbarMenu =
  | "settings"
  | "automations"
  | "layout"
  | "group"
  | "properties"
  | "filter"
  | "sort"
  | "color"
  | "templates"
  | "tasks"
  | "view"
  | "newView";

/**
 * What the toolbar lends the rest of its database view: opening one of its
 * menus beside another control, as a table header's "⋯" opens the
 * properties menu beside itself.
 */
export type DatabaseToolbarCommands = {
  openMenu(menu: DatabaseToolbarMenu, trigger: HTMLElement): void;
  /** Opens the view settings at this property's "속성 편집". */
  openPropertySettings(propertyId: string): void;
  /**
   * Opens the view settings at the layout, the properties, the groups or the
   * sub-groups, docked at the side: a group's "그룹 편집하기" and an empty
   * board's open them at "그룹화", as Notion's (seen 2026-11-12, a board and a timeline).
   */
  openViewSettings(screen?: "layout" | "properties" | "subitems" | "group" | "subGroup" | "filter", choosePagesIn?: boolean): void;
  /** On a phone, opens the filter sheet at a property's filter: true when it did, the filter bar's to do otherwise. */
  openFilter?(propertyId: string): boolean;
};

/** How many rules a filter group holds, its nested groups' included. */
export function filterRuleCount(group: FilterGroup): number {
  return group.filters.length + (group.groups ?? []).reduce((total, child) => total + filterRuleCount(child), 0);
}

/** The group at `path` in a filter tree, if it is there. */
export function filterGroupAt(root: FilterGroup, path: readonly number[]): FilterGroup | undefined {
  return path.reduce<FilterGroup | undefined>((group, index) => group?.groups?.[index], root);
}

/** A filter tree with the group at `path` changed. */
export function updateFilterGroupAt(root: FilterGroup, path: number[], change: (group: FilterGroup) => FilterGroup): FilterGroup {
  if (!path.length) return change(root);
  const [index, ...rest] = path;
  return {
    ...root,
    groups: (root.groups ?? []).map((group, current) => current === index
      ? updateFilterGroupAt(group, rest, change)
      : group),
  };
}

export function toolbarMenuWidth(menu: DatabaseToolbarMenu) {
  if (menu === "view" || menu === "newView") return 290;
  if (menu === "layout") return 560;
  if (menu === "group") return 320;
  if (menu === "properties" || menu === "templates") return 360;
  // The toolbar's filter and sort open their property lists, 290px as
  // Notion's (seen 2026-11-02); a picked one's editor opens at its pill.
  if (menu === "filter" || menu === "sort") return 290;
  if (menu === "color") return 520;
  return 300;
}

/**
 * Where a toolbar menu lines up across the window, and how far under its
 * button it opens; the menu placement picks the side and any cut. `left` is
 * where the menu starts: mirrored where the screen reads from the right
 * (`rtl`), where the menu's right edge stands there and the toolbar's end
 * is its left (toolbarButtonsEnd).
 */
export function toolbarMenuColumn(
  trigger: { left: number; right: number },
  menu: DatabaseToolbarMenu,
  _viewportWidth: number,
  toolbarEnd?: number,
  /** A form's "폼 설정" hangs 15 under its button (Notion's, seen 2026-11-08 at 1280). */
  viewType?: string,
  rtl = false,
) {
  const width = toolbarMenuWidth(menu);
  // An inline database's view settings open 3px under their button, from
  // 6px past the toolbar's end, no nearer than 12px to the window's right
  // edge, as Notion's (seen 2026-10-01 in 1280, 1433 and 1700px windows); a
  // new view's settings line up the same (seen 2026-10-30).
  const settings = menu === "view" || menu === "newView";
  const view = settings && toolbarEnd !== undefined;
  const preferredLeft = rtl ? (menu === "templates" ? trigger.left + width : view ? toolbarEnd - 6 : trigger.right)
    : menu === "templates" ? trigger.right - width : view ? toolbarEnd + 6 : trigger.left;
  // Filter and sort lists hang 4px under their buttons, from their left (seen 2026-11-02).
  const gap = settings ? (menu === "view" && viewType === "form" ? 15 : 3) : menu === "filter" || menu === "sort" ? 4 : 8;
  return { left: preferredLeft, width, margin: view ? 12 : 8, gap };
}

/**
 * A view's settings and the filter's and sort's lists — the toolbar's
 * "필터 기준" and "정렬 기준" and the filter bar's "+ 필터" — stand no taller
 * than four fifths of the window, scrolling inside, as Notion's: the settings
 * under their button where that fits, else risen over it to show whole (seen
 * 2026-11-15: 578.4 high in a 723 window, 562.4 in a 703 one, a board's
 * layout screen and its list alike); a list under its button where it fits,
 * over it where only that side has room, and with room on neither moved up
 * over its button to show whole (`whole`; seen 2026-11-17 in two tabs: 21
 * properties 578.4 high at 137..715.4 in a 723 window and 623.2 at
 * 148..771.2 in a 779 one, over their button at 228..252, 8px over the
 * window's foot, "+ 필터"'s the same; two properties, 150 high, over a button
 * at 636..664 in the 723 one, at 482..632). The filter bar's sort and "규칙
 * N개" pills open their windows the same way, 8px over the window's foot
 * (seen 2026-11-17 in two tabs: 15 sorts 578.4 high at 137.4..715.4 in a 723
 * window and 418.4 at 97.4..515.8 in a 523 one, over the pill at
 * 183.4..207.4; 14 rules the same).
 */
export function toolbarListFit(viewportHeight: number, settings = false) {
  return { ...(settings ? { rise: true } : { whole: true }), cap: viewportHeight * 0.8 };
}

export function toolbarTaskFeaturePatch(
  feature: "subitems" | "dependencies",
  enabled: boolean,
  configuration: Record<string, unknown> = {},
) {
  return { [feature]: { enabled, ...configuration } };
}

/**
 * A template's name, as Notion names it: its page's title. A template whose
 * title is empty goes by the name it was made with.
 */
export function toolbarTemplateName(template: { name: string; title?: string }) {
  return template.title?.trim() || template.name.trim();
}

type ConfiguredView = { config: Record<string, unknown> };

/** A view's advanced filter: its group, or its older flat filters as one. */
export function viewAdvancedFilter(view: ConfiguredView): FilterGroup {
  const current = view.config.filterGroup;
  if (current && typeof current === "object" && Array.isArray((current as FilterGroup).filters)) {
    return current as FilterGroup;
  }
  return {
    conjunction: view.config.filterConjunction === "or" ? "or" : "and",
    filters: Array.isArray(view.config.filters) ? view.config.filters as ViewFilter[] : [],
    groups: [],
  };
}

/**
 * The view with one of its quick filters moved into its advanced filter,
 * as the pill's "고급 필터에 추가" does in Notion (seen 2026-10-01).
 */
export function viewWithQuickFilterInAdvanced<View extends ConfiguredView>(view: View, index: number): View {
  const quick = Array.isArray(view.config.quickFilters) ? view.config.quickFilters as Array<ViewFilter | FilterGroup> : [];
  const moved = quick[index];
  if (!moved) return view;
  const root = viewAdvancedFilter(view);
  // It goes at the end, after whatever the filter shows last (filterGroupTerms).
  const next: FilterGroup = typeof (moved as ViewFilter).propertyId === "string"
    ? withFilterAdded(root, moved as ViewFilter)
    : withGroupAdded(root, moved as FilterGroup);
  const rest = quick.filter((_item, current) => current !== index);
  return {
    ...view,
    config: {
      ...view.config,
      quickFilters: rest.length ? rest : undefined,
      filterGroup: next,
      filters: next.filters,
      filterConjunction: next.conjunction,
    },
  };
}

/** Every quick filter folded into the advanced filter, in their order, as Notion's "고급 통합 필터" (seen 2026-11-04). */
export function viewWithQuickFiltersInAdvanced<View extends ConfiguredView>(view: View): View {
  const quick = Array.isArray(view.config.quickFilters) ? view.config.quickFilters : [];
  return quick.reduce<View>((next) => viewWithQuickFilterInAdvanced(next, 0), view);
}

/**
 * A database's properties as Notion lists them to pick from — filters,
 * sorts, grouping, conditional color (seen 2026-11-03): the name property,
 * then the rest by name, not the table's column order.
 */
export function propertyPickerOrder<P extends { type: string; name: string }>(properties: readonly P[]): P[] {
  return [...properties].sort((a, b) => a.type === "title" ? -1 : b.type === "title" ? 1 : a.name < b.name ? -1 : a.name > b.name ? 1 : 0);
}

/**
 * The filters a view's settings list counts, as its bar shows them: each
 * quick filter by its property, leaving out those still waiting for a value
 * (one holding a whole group by none), and its advanced filter as one with
 * no property, its rules waiting or not. Notion's settings name the one
 * property, or count them (seen 2026-10-24; the advanced filter 2026-11-03).
 */
export function appliedFilterItems(config: Record<string, unknown>): Array<string | null> {
  const items: Array<string | null> = filterRuleCount(viewAdvancedFilter({ config })) > 0 ? [null] : [];
  for (const term of Array.isArray(config.quickFilters) ? config.quickFilters as unknown[] : []) {
    if (!term || typeof term !== "object") continue;
    const filter = term as { propertyId?: unknown };
    if (typeof filter.propertyId !== "string") items.push(null);
    else if (!filterAwaitsValue(term as QueryFilter)) items.push(filter.propertyId);
  }
  return items;
}

// The layouts Notion's grid offers, in its order (seen 2026-09-30), the
// dashboard last (seen 2026-10-11) where a view can turn into one; a form
// shows there only as the layout the view already has.
export const LAYOUT_VIEW_TYPES: readonly GraphViewType[] = ["table", "board", "timeline", "calendar", "list", "gallery", "chart", "feed", "map"];

export function layoutViewTypes(view: GraphView, dashboard: boolean): GraphViewType[] {
  const offered: GraphViewType[] = dashboard ? [...LAYOUT_VIEW_TYPES, "dashboard"] : [...LAYOUT_VIEW_TYPES];
  return offered.includes(view.type) ? offered : [...offered, view.type];
}
