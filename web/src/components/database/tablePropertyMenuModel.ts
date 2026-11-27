import type { GraphProperty } from "@/contentGraph";
import type { DbProperty } from "@/lib/types";
import { formulaStaticResultType } from "./formulaEditorModel";
import { orderTableProperties, tableColumnWidth, type TablePropertyLike } from "./tableViewModel";
import { regroupedViewConfig } from "./viewLayoutDefaults";
import { urlShownFull } from "./urlDisplay";

type ViewConfigLike = Readonly<Record<string, unknown>>;
type Sort = { propertyId: string; direction: "asc" | "desc" };

function strings(value: unknown): string[] | undefined {
  return Array.isArray(value) ? value.map(String) : undefined;
}

/** The view's column settings as the table reads them. */
export function tableColumnSettings(config: ViewConfigLike) {
  return {
    visiblePropertyIds: strings(config.visiblePropertyIds),
    propertyOrder: strings(config.propertyOrder),
    visibleProperties: strings(config.visibleProperties),
    hiddenProperties: strings(config.hiddenProperties),
  };
}

/**
 * Column settings in the form every view and the toolbar share: the order
 * of all properties and the ones hidden. An older table's column list turns
 * into the same order with the unlisted properties hidden.
 */
function sharedColumnSettings(config: ViewConfigLike, properties: readonly TablePropertyLike[]) {
  const settings = tableColumnSettings(config);
  const next: Record<string, unknown> = { ...config };
  delete next.visiblePropertyIds;
  const visibleIds = settings.visiblePropertyIds?.length ? settings.visiblePropertyIds : undefined;
  const order = orderTableProperties(properties, settings.propertyOrder ?? visibleIds).map((property) => property.id);
  const hidden = settings.hiddenProperties
    ?? (visibleIds ? properties.filter((property) => property.type !== "title" && !visibleIds.includes(property.id)).map((property) => property.id) : []);
  return { next, order, hidden, visible: settings.visibleProperties };
}

/**
 * The config with the order its columns show in now kept, as any change of a
 * column made through the view keeps Notion's (seen 2026-11-12: resized,
 * wrapped or hidden, a view showing its columns by name lists them all so,
 * and a property made afterwards follows them).
 */
function tableConfigKeepingOrder(config: ViewConfigLike, properties: readonly TablePropertyLike[]): Record<string, unknown> {
  if (strings(config.propertyOrder)) return { ...config };
  const { next, order, hidden, visible } = sharedColumnSettings(config, properties);
  return visible || !hidden.length ? { ...next, propertyOrder: order } : { ...next, propertyOrder: order, hiddenProperties: hidden };
}

/** A column at a new width, the view's order kept with it (tableConfigKeepingOrder). */
export function tableConfigAfterResize(config: ViewConfigLike, properties: readonly TablePropertyLike[], propertyId: string, width: number) {
  const widths = config.propertyWidths && typeof config.propertyWidths === "object" ? config.propertyWidths as Record<string, unknown> : {};
  return { ...tableConfigKeepingOrder(config, properties), propertyWidths: { ...widths, [propertyId]: width } };
}

/** Hides a column; the title stays. */
export function tableConfigAfterHide(config: ViewConfigLike, properties: readonly TablePropertyLike[], propertyId: string) {
  const { next, order, hidden, visible } = sharedColumnSettings(config, properties);
  if (visible) return { ...next, propertyOrder: order, visibleProperties: visible.filter((id) => id !== propertyId) };
  return { ...next, propertyOrder: order, hiddenProperties: [...new Set([...hidden, propertyId])] };
}

/**
 * Moves a column before or after another shown column; hidden properties
 * keep their places in the order.
 */
export function tableConfigAfterReorder(
  config: ViewConfigLike,
  properties: readonly TablePropertyLike[],
  sourceId: string,
  targetId: string,
  side: "before" | "after",
) {
  const { next, order, hidden, visible } = sharedColumnSettings(config, properties);
  if (sourceId === targetId || !order.includes(sourceId) || !order.includes(targetId)) return null;
  const without = order.filter((id) => id !== sourceId);
  const index = without.indexOf(targetId) + (side === "after" ? 1 : 0);
  const propertyOrder = [...without.slice(0, index), sourceId, ...without.slice(index)];
  return visible ? { ...next, propertyOrder, visibleProperties: visible } : { ...next, propertyOrder, hiddenProperties: hidden };
}

/** Places a duplicated property right after its source and shows it. */
export function tableConfigAfterDuplicate(
  config: ViewConfigLike,
  properties: readonly TablePropertyLike[],
  sourceId: string,
  duplicateId: string,
) {
  const { next, order, hidden, visible } = sharedColumnSettings(config, properties);
  const without = order.filter((id) => id !== duplicateId);
  const index = without.indexOf(sourceId);
  const propertyOrder = index < 0 ? [...without, duplicateId] : [...without.slice(0, index + 1), duplicateId, ...without.slice(index + 1)];
  const widths = config.propertyWidths && typeof config.propertyWidths === "object"
    ? config.propertyWidths as Record<string, unknown> : undefined;
  const propertyWidths = widths?.[sourceId] !== undefined ? { ...widths, [duplicateId]: widths[sourceId] } : widths;
  return {
    ...next, propertyOrder, ...(propertyWidths ? { propertyWidths } : {}),
    ...(visible ? { visibleProperties: [...visible, duplicateId] } : { hiddenProperties: hidden.filter((id) => id !== duplicateId) }),
  };
}

/**
 * The config that shows a property added from the table's header as its last
 * column. The view keeps its columns' order from then on, as Notion's head
 * "+" sets it (seen 2026-11-12: a view showing its columns by name took the
 * new one last, its others staying put); it names the property, so the
 * server's hiding of it in the source's other tables passes over this one.
 */
export function tableConfigAfterAdd(config: ViewConfigLike, properties: readonly TablePropertyLike[], propertyId: string) {
  const { next, order, hidden, visible } = sharedColumnSettings(config, properties);
  const propertyOrder = [...order.filter((id) => id !== propertyId), propertyId];
  return visible
    ? { ...next, propertyOrder, visibleProperties: [...visible.filter((id) => id !== propertyId), propertyId] }
    : { ...next, propertyOrder, hiddenProperties: hidden.filter((id) => id !== propertyId) };
}

/** Sorts by the column first, keeping the view's other sorts after it. */
export function tableConfigAfterSort(config: ViewConfigLike, propertyId: string, direction: "asc" | "desc") {
  const sorts = (Array.isArray(config.sorts) ? config.sorts as Sort[] : []).filter((sort) => sort.propertyId !== propertyId);
  return { ...config, sorts: [{ propertyId, direction }, ...sorts] };
}

/**
 * The columns whose text wraps: every column when the view wraps all content
 * (Notion's "모든 콘텐츠 줄바꿈", the API's `wrap_cells`), else those wrapped
 * on their own.
 */
export function tableWrappedColumnIds(config: ViewConfigLike, propertyIds: readonly string[]): ReadonlySet<string> {
  return new Set(config.wrap === true ? propertyIds : strings(config.wrappedColumns) ?? []);
}

export function tableColumnWrapped(config: ViewConfigLike, propertyId: string) {
  return config.wrap === true || (strings(config.wrappedColumns)?.includes(propertyId) ?? false);
}

/**
 * Turns text wrapping in a column on or off. Unwrapping one column of a view
 * that wraps all content leaves the others wrapped and the view's switch off,
 * as in Notion (seen 2026-09-30); the switch is then set off, as Notion's API
 * gives `wrap_cells: false` once a column's wrap changed (seen 2026-11-10).
 */
export function tableConfigAfterWrap(config: ViewConfigLike, propertyId: string, properties: readonly TablePropertyLike[]) {
  const wrapped = [...tableWrappedColumnIds(config, properties.map(({ id }) => id))];
  const nextWrapped = wrapped.includes(propertyId) ? wrapped.filter((id) => id !== propertyId) : [...wrapped, propertyId];
  const next: Record<string, unknown> = { ...tableConfigKeepingOrder(config, properties), wrappedColumns: nextWrapped, wrap: false };
  if (!nextWrapped.length) delete next.wrappedColumns;
  return next;
}

/**
 * The status columns a table shows as checkboxes, as Notion's column menu
 * "다음과 같이 표시: 체크박스" (seen 2026-10-11; the API's `status_show_as`).
 */
export function tableStatusCheckboxIds(config: ViewConfigLike): ReadonlySet<string> {
  return new Set(strings(config.statusCheckboxColumns) ?? []);
}

/** Shows a status column as a checkbox, or as its option again. */
export function tableConfigAfterStatusDisplay(config: ViewConfigLike, propertyId: string, checkbox: boolean, properties: readonly TablePropertyLike[]) {
  const ids = [...tableStatusCheckboxIds(config)].filter((id) => id !== propertyId);
  const next: Record<string, unknown> = { ...tableConfigKeepingOrder(config, properties), statusCheckboxColumns: checkbox ? [...ids, propertyId] : ids };
  if (!(next.statusCheckboxColumns as string[]).length) delete next.statusCheckboxColumns;
  return next;
}

/**
 * The URL columns a table's view held as showing their links in full before
 * Notion's "전체 URL 표시" became the property's (urlDisplay.ts), read only:
 * a property's own setting goes before it.
 */
export function tableFullUrlColumnIds(config: ViewConfigLike): ReadonlySet<string> {
  return new Set(strings(config.fullUrlColumns) ?? []);
}

/** Whether a URL property shows its links whole, here in a table with this view's config or anywhere else. */
export function propertyShowsFullUrl(property: GraphProperty, config?: ViewConfigLike) {
  return urlShownFull(property, Boolean(config && tableFullUrlColumnIds(config).has(property.id)));
}

/** The property with its "전체 URL 표시" turned on or off, wherever it shows (Notion's, seen 2026-11-14). */
export function propertyAfterFullUrl(property: GraphProperty, full: boolean): GraphProperty {
  return { ...property, config: { ...property.config, showFullUrl: full } };
}

/**
 * Whether the layout's "모든 콘텐츠 줄바꿈" switch is on: the view wraps all
 * its content, not each column on its own — Notion's switch stays off over
 * columns all wrapped one by one (seen 2026-11-08 on a linked view whose
 * columns came wrapped from its source).
 */
export function tableWrapsAllColumns(config: ViewConfigLike) {
  return config.wrap === true;
}

/**
 * Wraps every column, or none. Turned on it wraps each of its columns too,
 * as Notion's switch does (seen 2026-11-08: the view's `table_wrap` and every
 * column's own wrap), which a linked view made from it takes (linkedViewDefaults).
 * Turned off, it stays set off, as Notion's API then gives `wrap_cells: false`
 * (seen 2026-11-10).
 */
export function tableConfigAfterWrapAll(config: ViewConfigLike, wrap: boolean, columnIds: readonly string[] = []) {
  const next: Record<string, unknown> = { ...config, wrap: true, wrappedColumns: [...columnIds] };
  if (!columnIds.length) delete next.wrappedColumns;
  if (!wrap) { next.wrap = false; delete next.wrappedColumns; }
  return next;
}

/**
 * Shows a property inserted beside a column, to its left or right, as
 * Notion's column menu inserts one.
 */
export function tableConfigAfterInsert(
  config: ViewConfigLike,
  properties: readonly TablePropertyLike[],
  propertyId: string,
  targetId: string,
  side: "left" | "right",
) {
  const { next, order, hidden, visible } = sharedColumnSettings(config, properties);
  const without = order.filter((id) => id !== propertyId);
  const at = without.indexOf(targetId);
  const index = at < 0 ? without.length : at + (side === "right" ? 1 : 0);
  const propertyOrder = [...without.slice(0, index), propertyId, ...without.slice(index)];
  return visible
    ? { ...next, propertyOrder, visibleProperties: [...visible.filter((id) => id !== propertyId), propertyId] }
    : { ...next, propertyOrder, hiddenProperties: hidden.filter((id) => id !== propertyId) };
}

/** Groups the table's rows by the column as a pick from its grouping screen does (regroupedViewConfig), or no longer. */
export function tableConfigAfterGroup(config: ViewConfigLike, propertyId: string, properties: readonly GraphProperty[]) {
  if (config.groupBy !== propertyId) {
    return regroupedViewConfig(config, typeof config.groupBy === "string" ? config.groupBy : undefined, propertyId, properties, "table");
  }
  const next: Record<string, unknown> = { ...config };
  delete next.groupBy;
  return next;
}

/**
 * Whether the title column shows each page's icon, as Notion's tables do
 * until "show page icon" is turned off (seen 2026-09-30).
 */
export function tableShowsPageIcons(config: ViewConfigLike) {
  return config.showPageIcons !== false;
}

export function tableConfigAfterPageIcons(config: ViewConfigLike, show: boolean) {
  const next: Record<string, unknown> = { ...config, showPageIcons: false };
  if (show) delete next.showPageIcons;
  return next;
}

/**
 * Whether the columns are frozen up to this one; its menu then unfreezes
 * them, and any other column's freezes them up to itself.
 */
export function tableColumnFrozenEdge(config: ViewConfigLike, shownIds: readonly string[], propertyId: string) {
  const frozen = Math.floor(Number(config.frozenColumns) || 0);
  return frozen > 0 && shownIds.indexOf(propertyId) === frozen - 1;
}

/**
 * Whether the columns up to this one may be frozen: together no wider than
 * the frame the table is in less 100px, as Notion's, whose "고정" is grey past
 * that (seen 2026-11-06: in a 1010px frame, 910 frozen yes and 912 no; 1110
 * yes once the sidebar closed to a 1280 frame).
 */
export function tableColumnFreezable(config: ViewConfigLike, shown: readonly { id: string; type: string }[], propertyId: string, frameWidth: number | undefined) {
  const index = shown.findIndex((property) => property.id === propertyId);
  if (index < 0 || !frameWidth) return true;
  const frozenWidth = shown.slice(0, index + 1)
    .reduce((sum, property) => sum + tableColumnWidth(config as Record<string, unknown>, property.id, property.type), 0);
  return frozenWidth <= frameWidth - 100;
}

export function tableConfigAfterFreeze(config: ViewConfigLike, shownIds: readonly string[], propertyId: string) {
  const next: Record<string, unknown> = { ...config };
  if (tableColumnFrozenEdge(config, shownIds, propertyId)) delete next.frozenColumns;
  else if (shownIds.includes(propertyId)) next.frozenColumns = shownIds.indexOf(propertyId) + 1;
  return next;
}

/** Sets the column's calculation under the table, or takes it away. */
export function tableConfigAfterCalculation(config: ViewConfigLike, propertyId: string, calculation: string | undefined) {
  const current = config.calculations && typeof config.calculations === "object"
    ? config.calculations as Record<string, unknown> : {};
  const calculations: Record<string, unknown> = { ...current, [propertyId]: calculation };
  if (!calculation) delete calculations[propertyId];
  return { ...config, calculations };
}

/**
 * The column's two sorts as Notion names them (seen 2026-09-30): text sorts
 * A to Z, a number low to high, a date old to new, a checkbox unchecked to
 * checked (seen 2026-10-11), anything else ascending.
 */
function tableSortLabels(type: string): readonly [ascending: string, descending: string] {
  if (["title", "rich_text", "url", "email", "phone", "phone_number", "person", "relation", "created_by", "last_edited_by", "files"].includes(type)) return ["sortAToZ", "sortZToA"];
  if (type === "number") return ["sortLowToHigh", "sortHighToLow"];
  if (["date", "created_time", "last_edited_time"].includes(type)) return ["sortOldToNew", "sortNewToOld"];
  if (type === "checkbox") return ["sortUncheckedToChecked", "sortCheckedToUnchecked"];
  return ["sortAscending", "sortDescending"];
}

const FORMULA_RESULT_SORTS_AS: Partial<Record<string, string>> = { text: "rich_text", number: "number", checkbox: "checkbox", date: "date", person: "person" };

/**
 * A sort's directions, named for what the property holds, as Notion's (seen
 * 2026-11-04): a formula's for what it comes to.
 */
export function propertySortLabels(property: DbProperty | undefined, properties: readonly DbProperty[]) {
  if (property?.type !== "formula") return tableSortLabels(property?.type ?? "");
  return tableSortLabels(FORMULA_RESULT_SORTS_AS[formulaStaticResultType(property.config?.formula ?? "", properties, [])] ?? "");
}
