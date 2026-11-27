import { compareGraphOrder } from "@/contentGraph";
import type { GraphProperty, GraphView, GraphViewType } from "@/contentGraph";
import { CARD_PREVIEW_PAGE_CONTENT } from "./cardPreviewModePolicy";
import { isBoardValueGroupProperty } from "./boardValueGrouping";
import { isTableGroupProperty, NO_TABLE_GROUP } from "./tableGrouping";
import { viewWrapsAtFirst } from "@/lib/viewFirstWrap";
import { chartFirstAxis } from "./chartGroupingConfiguration";
import { chartDrawnAxisId } from "./chartDrawnAxis";
import type { DbProperty } from "@/lib/types";
import { inPublicOrder } from "./propertyPublicOrder";
import { madeInitialLoadLimit } from "./queryProjection";
import { boardOptionGroupOrder } from "./optionGroupOrder";

type ViewConfig = Readonly<Record<string, unknown>>;
type PropertyLike = Pick<GraphProperty, "id" | "type" | "position" | "publicOrder"> & { config?: unknown };

/** Properties in the order a view picks its first of a kind in: their API's (inPublicOrder), then their own. */
const pickOrder = <T extends PropertyLike>(properties: readonly T[]) => inPublicOrder([...properties].sort(compareGraphOrder));

/**
 * A view's settings once it is grouped from a pick, wherever that is (the
 * grouping screen, a board's layout settings, a property made for it), as
 * Notion's. A pick from a list, the property grouped by already too, hides
 * empty groups again (seen 2026-11-07: a board by select, status, tags or a
 * box, from its grouping or layout screen, a table, a board's sub-groups); a
 * property `made` for the view leaves them shown, as a new board's own status
 * does (2026-11-07). Another property's groups start in their own order, any
 * layout's option groups as a new board's do (boardOptionGroupOrder; a new
 * table by select or tags, 2026-11-07), none hidden (seen 2026-11-08:
 * a group hidden by hand showed again once grouped by another and back).
 * `from` is the property it groups by now, a board's default when it names none.
 */
export function regroupedViewConfig(
  config: ViewConfig, from: string | undefined, to: string, properties: readonly PropertyLike[], layout: GraphViewType,
  made = false,
): Record<string, unknown> {
  const property = properties.find((candidate) => candidate.id === to);
  const next: Record<string, unknown> = { ...config, groupBy: to };
  // On a board grouped by options, people or a box, empty groups show until asked not to (boardGroupSettings).
  if (!made) {
    delete next.hideEmptyGroups;
    if (layout === "board" && !isBoardValueGroupProperty(property)) next.hideEmptyGroups = true;
    // A board's people added from "+ 신규 그룹" go with it, the same person picked again too (Notion's, seen 2026-11-12).
    if (layout === "board") delete next.keptGroups;
  }
  if (to === from) return next;
  for (const key of ["groupSort", "groupOrder", "groupByMode", "hiddenGroups"]) delete next[key];
  // A status shows its options at first, "내 작업"'s — its sources' statuses
  // together — its groups, whatever the view showed before, as Notion's (seen
  // 2026-11-24 in two tabs: "내 작업"'s list grouped by status from none and
  // from 마감일 showed 그룹, its 옵션 set before not kept; a task database's
  // table 옵션, its 그룹 set before not kept; both hiding empty groups).
  if (config.tasksAcrossSources === true && property?.type === "status") next.groupByMode = "group";
  const order = boardOptionGroupOrder(property, layout === "board" ? undefined : NO_TABLE_GROUP);
  if (order) next.groupOrder = order;
  return next;
}

function withBoardGrouping(config: Record<string, unknown>, properties: readonly PropertyLike[]) {
  const groupBy = boardGroupDefault(properties);
  if (!groupBy) return;
  config.groupBy = groupBy;
  const order = boardOptionGroupOrder(properties.find((property) => property.id === groupBy));
  if (order && !Array.isArray(config.groupOrder)) config.groupOrder = order;
}

/**
 * A chart's X axis when it names none: its first by Notion's rule
 * (chartFirstAxis), as a new chart's and one made from another layout are —
 * never the grouping it was made with (seen 2026-11-18 in two tabs: boards
 * grouped by the select second by public ID made charts along the first).
 * Along a status, dates or numbers it sorts the chart the axis's own way. Its
 * format says what Notion's says, a table made a chart and one from "+" alike
 * (seen 2026-11-18 in Chrome, their views API read): along a status that its
 * axis shows its empty groups, x_axis hide_empty_groups false, where a status
 * picked later says nothing (chartGroupChangePatch); along dates or numbers
 * nothing of its empty groups, its own or its axis's, until "0 값 생략" says it
 * (chartEmptyGroupsUnsaid) — an axis picked later says its own, as
 * chartGroupChangePatch writes it, while the chart's own stays unsaid.
 */
function withChartAxis(config: Record<string, unknown>, properties: readonly PropertyLike[]) {
  const first = chartFirstAxis(pickOrder(properties));
  if (!first) return;
  config.chartGroupBy = first.property.id;
  if (first.dateBy) config.chartDateBy = first.dateBy;
  if (first.property.type === "status") Object.assign(config, { chartSort: "x_ascending", chartAxisHideEmptyGroups: false });
  if (first.property.type === "date" || first.property.type === "number") {
    Object.assign(config, { chartSort: "x_ascending", chartEmptyGroupsUnsaid: true });
  }
  // Its numbers' ranges are left to the chart, -1 each, as Notion's start reads (the views API's chartAxisRanges).
  if (first.property.type === "number") {
    config.chartAxisRanges = { property: first.property.id, range_start: -1, range_end: -1, range_size: -1 };
  }
}

/**
 * How a view shows sub-items once it is a chart, as Notion's (seen
 * 2026-11-18 in Chrome, four databases): a chart counts sub-items beside
 * their parents, so a view made a chart — new from "+" or from another
 * layout — shows them flattened from then on unless it was told how, and
 * made a table, a board or another layout again its sub-items screen says
 * "병합된 목록으로 표시"; one told to nest them still nests them.
 */
function withChartSubitems(config: Record<string, unknown>) {
  const subtasks = typeof config.subtasks === "object" && config.subtasks !== null ? config.subtasks as Record<string, unknown> : {};
  if (typeof subtasks.displayMode === "string" && subtasks.displayMode) return;
  config.subtasks = { ...subtasks, displayMode: "flattened" };
}

/**
 * What a view added from "+" keeps of how it shows sub-items, as Notion's
 * (seen 2026-11-18 in Chrome: a table added beside others, and a chart added
 * so and made a table, read toggle_column_id "title" in the views API; a
 * database's first table and a board made a table read none): their toggle
 * in its title column.
 */
function withAddedViewSubitems(config: Record<string, unknown>, properties: readonly PropertyLike[]) {
  const title = properties.find((property) => property.type === "title");
  if (!title) return;
  const subtasks = typeof config.subtasks === "object" && config.subtasks !== null ? config.subtasks as Record<string, unknown> : {};
  config.subtasks = { ...subtasks, toggleColumnId: title.id };
}

/**
 * What each layout keeps of its own, as Notion's views do (seen 2026-10-04:
 * a table turned into a board, a gallery and a list showed each layout's
 * own cards, not the table's columns): its shown properties, its card
 * preview, size and layout, and where its pages open.
 */
const LAYOUT_KEYS = [
  "visibleProperties", "hiddenProperties", "visiblePropertyIds",
  "coverProperty", "cardSize", "cardLayout", "fitImage", "openPageIn", "wrap",
] as const;

/**
 * Grouping is the view's, not a layout's, as Notion's (seen 2026-10-24: a
 * table grouped and made a list stays grouped, and back; made a board it
 * groups the board, one not grouped taking the board's first choice). A
 * board keeps it too (seen 2026-11-18 in three tabs, inline and full page: a
 * board made a table, a list or a gallery stays grouped by its property, and
 * back; made a chart, the chart's grouping screen shows it). A chart, whose
 * grouping it never draws by, leaves it (seen 2026-11-18 in three tabs:
 * charts along either select, grouped by a date, by either select or not at
 * all, made boards grouped by the board's first choice — the first by
 * public ID, not the first made — made tables not grouped), keeping the X
 * axis it drew for its return; the grouping a table, list or gallery had
 * before it became a chart comes back instead (GROUPING_BEFORE_CHART).
 */
const GROUPING_KEYS = [
  "groupBy", "groupByMode", "groupSort", "groupOrder", "hiddenGroups", "pinnedGroups", "hideEmptyGroups",
  "textGroupBy", "subGroupBy", "subGroup", "boardColumnColors",
] as const;

/**
 * Whether a view's grouping leaves out its empty groups: a board's by
 * options, people or a box only when told to, any other only when not told
 * not to (boardHidesEmptyGroups, tableHidesEmptyGroups).
 */
function hidesEmptyGroups(config: ViewConfig, layout: GraphViewType, properties: readonly PropertyLike[]) {
  const byOptions = layout === "board" && !isBoardValueGroupProperty(properties.find((property) => property.id === config.groupBy));
  return byOptions ? config.hideEmptyGroups === true : config.hideEmptyGroups !== false;
}

/**
 * Where a table, list or gallery made a chart keeps the grouping it had, as
 * Notion's (seen 2026-11-18 in two tabs, tables grouped by either select,
 * "빈 그룹 숨기기" on): the chart's own grouping screen shows none, and made
 * a board or a table again the view groups by it, its empty groups as they
 * showed — a grouping picked on the chart's screen meanwhile goes, as any
 * chart's does. A board's grouping stays the chart's own (its grouping
 * screen shows it, GROUPING_KEYS).
 */
const GROUPING_BEFORE_CHART = "groupingBeforeChart";

/** The layouts whose first settings show the title alone (layoutDefaults). */
const TITLE_ALONE_LAYOUTS = new Set<GraphViewType>(["board", "gallery", "list", "calendar", "timeline", "feed"]);

/** Where a view keeps the settings of the layouts it was before, by layout. */
const LAYOUT_SETTINGS = "layoutSettings";

/**
 * A board groups by a status first, then a select, a person, a multi-select
 * and a relation, the first of the kind in the order their API lists them
 * (inPublicOrder, seen 2026-11-12), and by nothing else — Notion's new board and a table turned into a board grouped
 * by the status "Stage" before the select "Status" (seen 2026-10-08); a new
 * board took a person made after a multi-select, and a board whose group
 * went fell to a status, a select, a person, a multi-select, a relation and
 * then asked for one with a checkbox, dates, text and numbers left (seen
 * 2026-11-08). The server's agent boards and deletions go by the same
 * (mcpBoardGroupPreference).
 */
const BOARD_GROUP_PREFERENCE = ["status", "select", "person", "multi_select", "relation"] as const;

export function boardGroupDefault(properties: readonly PropertyLike[]) {
  const ordered = pickOrder(properties);
  for (const type of BOARD_GROUP_PREFERENCE) {
    const found = ordered.find((property) => property.type === type);
    if (found) return found.id;
  }
  return undefined;
}

/**
 * The property a board groups by: the one its settings name where a board
 * can group by it, else its default — Notion's board always has one (seen
 * 2026-10-11). Its cards, its settings and its toolbar read it here, so a
 * board made without one (an agent's, an import's) shows the same group
 * everywhere.
 */
export function boardGroupPropertyId(config: ViewConfig | undefined, properties: readonly PropertyLike[]) {
  const own = properties.find((property) => property.id === config?.groupBy);
  return own && isTableGroupProperty(own) ? own.id : boardGroupDefault(properties);
}

/**
 * A layout's first settings, as Notion's new views and switches show them
 * (seen 2026-10-04, 2026-10-08): a board, gallery, list, calendar, timeline
 * or feed shows the title alone (a feed's "속성 표시 여부 0", seen 2026-11-02;
 * calendar and timeline 2026-11-03), so properties added later stay hidden
 * there as in Notion; a board groups by its default property and wraps all
 * its content (viewWrapsAtFirst); a table wraps only as its database's first
 * view (newViewDefaults) — added beside others, made from another layout,
 * turned to another source or as a new source's view it starts unwrapped
 * (seen 2026-11-04, and 2026-11-18 a board made a table); a gallery previews
 * the page's content; a chart runs along its first axis (withChartAxis).
 */
export function layoutDefaults(type: GraphViewType, properties: readonly PropertyLike[], config: ViewConfig = {}): Record<string, unknown> {
  const defaults: Record<string, unknown> = {};
  if (TITLE_ALONE_LAYOUTS.has(type)) defaults.visibleProperties = [];
  if (type === "board" && viewWrapsAtFirst(type)) defaults.wrap = true;
  if (type === "gallery") defaults.coverProperty = CARD_PREVIEW_PAGE_CONTENT;
  if (type === "board" && typeof config.groupBy !== "string") withBoardGrouping(defaults, properties);
  if (type === "chart" && typeof config.chartGroupBy !== "string") withChartAxis(defaults, properties);
  return defaults;
}

/**
 * What a new list shows beside its title, as Notion's new list (seen
 * 2026-11-04, three databases: the first of these kinds, whichever kind it
 * is, in the order their API lists them — seen 2026-11-12, neither by name
 * nor as made) — never a number, text, checkbox or URL. Made from a
 * list it shows the title alone, and a view made a list does too.
 */
const NEW_LIST_SHOWN_KINDS = new Set<string>(["person", "date", "multi_select", "select", "status"]);

/** Of a table's settings, what the new table added beside it takes: which properties it shows and in what order. */
const NEW_TABLE_TAKES = ["hiddenProperties", "visibleProperties", "propertyOrder"] as const;

/**
 * Whether a table keeps a property made later out of its columns: one that
 * hides some property, or lists the ones it shows, does; one never touched
 * shows every property of its source, as Notion's (seen 2026-11-04), and so
 * does one whose columns were only moved, sized or wrapped — it shows the new
 * one after its own, by name (seen 2026-11-12).
 */
export function tableKeepsItsShownProperties(config: ViewConfig | undefined) {
  const hidden = config?.hiddenProperties;
  return config?.visibleProperties !== undefined || (Array.isArray(hidden) && hidden.length > 0);
}

type NewViewContext = {
  /** The view it was made from, and the other views of its source. */
  from?: { type: string; config?: ViewConfig };
  siblings?: readonly { type: string; config?: ViewConfig }[];
};

/** A new view's first settings: its layout's (a new chart's X axis among
 * them, layoutDefaults), a new list's one property shown. */
export function newViewDefaults(type: GraphViewType, properties: readonly PropertyLike[], context?: NewViewContext): Record<string, unknown> {
  // It keeps its own row order from the start, newest rows first (viewRowOrder.ts); a table made
  // with no other view beside it is its database's first and wraps all its content (layoutDefaults).
  const databaseFirst = !context && type === "table" && viewWrapsAtFirst(type);
  const defaults: Record<string, unknown> = { ...layoutDefaults(type, properties), ...(databaseFirst ? { wrap: true } : {}), rowOrder: [] };
  // A new dashboard beside other views hides its title row, "새 대시보드", until its switch shows it (Notion's, seen 2026-11-18).
  if (type === "dashboard") defaults.showDataSourceTitle = false;
  if (context) withAddedViewSubitems(defaults, properties);
  // Added beside other views it keeps the count it is made with through its layouts (madeInitialLoadLimit).
  const made = context ? madeInitialLoadLimit(type) : undefined;
  if (made) defaults.madeLoadLimit = made;
  if (type === "chart") withChartSubitems(defaults);
  if (type === "table" && context) {
    // Added beside other views it does not wrap, however they do, and shows
    // what the table it was made from shows, or the source's first table
    // when it was made from another layout (seen 2026-11-04).
    const table = context.from?.type === "table" ? context.from : context.siblings?.find((view) => view.type === "table");
    for (const key of NEW_TABLE_TAKES) if (table?.config?.[key] !== undefined) defaults[key] = table.config[key];
    return defaults;
  }
  if (type !== "list" || context?.from?.type === "list") return defaults;
  const found = pickOrder(properties).find((property) => NEW_LIST_SHOWN_KINDS.has(property.type));
  return found ? { ...defaults, visibleProperties: [found.id] } : defaults;
}

/** Of its source's table, what a linked view's first table takes besides a new table's: how wide its columns are and which wrap on their own. */
const LINKED_TABLE_TAKES = ["propertyWidths", "wrappedColumns"] as const;

/**
 * A linked view's first settings, while it waits in "새 보기" and once that
 * is left unchosen or "+ 새 표 보기" made, as Notion's (seen 2026-11-08: its
 * columns its source's first view's — which show, in what order, how wide,
 * which wrap on their own — its rows the newest first, not its source's
 * order, and the rest a table's first: its lines shown and no wrapping of
 * all its content). `from` is its source's first view.
 */
export function linkedViewDefaults(from: { viewType: string; viewConfig?: ViewConfig }): Record<string, unknown> {
  // It is its database's first table, made with no count of its own: made a feed it shows a feed's 10 (madeInitialLoadLimit).
  const { madeLoadLimit: _made, ...defaults } = newViewDefaults("table", [], { from: { type: from.viewType, config: from.viewConfig } });
  void _made;
  if (from.viewType === "table") for (const key of LINKED_TABLE_TAKES) if (from.viewConfig?.[key] !== undefined) defaults[key] = from.viewConfig[key];
  return defaults;
}

/**
 * A view's settings once it becomes another layout: the layout it leaves
 * keeps its own settings for when it comes back; the one it becomes takes
 * the settings it kept, or its first ones.
 */
export function switchedLayoutConfig(
  config: ViewConfig, from: GraphViewType, to: GraphViewType, properties: readonly PropertyLike[],
): Record<string, unknown> {
  if (from === to) return { ...config };
  const kept = (typeof config[LAYOUT_SETTINGS] === "object" && config[LAYOUT_SETTINGS] !== null
    ? config[LAYOUT_SETTINGS] : {}) as Record<string, Record<string, unknown>>;
  const leaving: Record<string, unknown> = {};
  const next: Record<string, unknown> = { ...config };
  for (const key of LAYOUT_KEYS) {
    if (key in next) leaving[key] = next[key];
    delete next[key];
  }
  // A chart reading its axis from its configuration (chartDrawnAxis.ts) names it, before that goes with its grouping.
  const axis = from === "chart" && typeof config.chartGroupBy !== "string"
    ? chartDrawnAxisId({ type: "chart", config } as unknown as GraphView, properties as unknown as DbProperty[]) : undefined;
  if (from === "chart") {
    for (const key of GROUPING_KEYS) delete next[key];
    // The grouping it had as a table, list or gallery comes back, its empty groups as they showed (GROUPING_BEFORE_CHART).
    const before = config[GROUPING_BEFORE_CHART];
    if (typeof before === "object" && before !== null) Object.assign(next, before);
    delete next[GROUPING_BEFORE_CHART];
  }
  if (axis) next.chartGroupBy = axis;
  // A board's grouping goes on as the group it shows, its default when its settings name none (boardGroupPropertyId).
  const shown = from === "board" ? boardGroupPropertyId(config, properties) : undefined;
  if (shown) next.groupBy = shown;
  // A table's, list's or gallery's grouping is kept apart while it is a chart, hiding its empty groups or not as it did.
  if (to === "chart" && from !== "board" && typeof next.groupBy === "string") {
    const grouping: Record<string, unknown> = { hideEmptyGroups: hidesEmptyGroups(next, from, properties) };
    for (const key of GROUPING_KEYS) {
      if (key in next && key !== "hideEmptyGroups") grouping[key] = next[key];
      delete next[key];
    }
    next[GROUPING_BEFORE_CHART] = grouping;
  }
  // Its empty groups stay as they showed, each layout reading its setting its own way (seen 2026-11-18:
  // a board by a select showing its empty groups made a table and a chart showing them, "빈 그룹 숨기기" off;
  // a table hiding them made a board without its empty column).
  if (typeof next.groupBy === "string") {
    const hides = hidesEmptyGroups(next, from, properties);
    if (hidesEmptyGroups(next, to, properties) !== hides) next.hideEmptyGroups = hides;
  }
  // A layout's kept settings never bring back a grouping (views saved before kept one there).
  const returning = kept[to] && Object.fromEntries(Object.entries(kept[to])
    .filter(([key]) => !(GROUPING_KEYS as readonly string[]).includes(key)));
  const { [to]: _taken, ...others } = kept;
  void _taken;
  next[LAYOUT_SETTINGS] = { ...others, [from]: leaving };
  const switched: Record<string, unknown> = { ...next, ...(returning ?? layoutDefaults(to, properties, next)) };
  if (to === "board" && typeof switched.groupBy !== "string") withBoardGrouping(switched, properties);
  if (to === "chart" && typeof switched.chartGroupBy !== "string") withChartAxis(switched, properties);
  if (to === "chart") withChartSubitems(switched);
  // "내 작업" keeps what it shows through its layouts and boards its status groups, as Notion's (seen 2026-11-05).
  if (config.tasksAcrossSources === true) {
    switched.visibleProperties = config.visibleProperties;
    if (to === "board") switched.groupByMode = "group";
  }
  return switched;
}
