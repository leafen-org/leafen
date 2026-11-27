import { orderTableProperties, type TablePropertyLike } from "./tableViewModel";

/**
 * `titleFree`: the title hides and moves as any property, as in a timeline's
 * table panel (Notion's, seen 2026-11-11); elsewhere it stays shown, and
 * in its place but in a list, where it moves as any property (viewTitleMoves).
 */
type ViewLike = { type: string; config: Readonly<Record<string, unknown>>; titleFree?: boolean; titleFirst?: boolean };

/**
 * Whether the view's title hides as any property: in a timeline's table
 * panel (`titleFree`), and on a timeline's bars while its table shows, which
 * names the rows — a hidden title leaves a bar its values alone and one shown
 * again goes last. With the table shut its eye is dim and a hidden title
 * shows anyway, after the shown values (Notion's, seen 2026-11-15 at 1440:
 * "이름" dim and aria-disabled with "표 보기" off, hidden with it on, "완료 행
 * 8" once it was turned off again); a board's, a gallery's and a calendar's
 * stay shown. Hidden, it is listed among `hiddenProperties`, whichever list
 * the view keeps, as the server reads a view's properties
 * (notion_view_property_entries.go).
 */
export function viewTitleFree(view: ViewLike) {
  return view.titleFree === true || view.type === "timeline" && view.config.timelineShowTable === true;
}

/**
 * Whether the title moves among the view's properties — not where it is
 * kept first (`titleFirst`, a relation picker's rows): a table's does, its
 * column moving with it, as Notion's (seen 2026-11-16 in Chrome and in its
 * desktop app: "이름" dragged in "속성 표시 여부" below "글", the columns
 * then "U · 글 · 이름"); a list's, what the view lists before it then
 * showing before it (listRowSides), as Notion's (seen 2026-11-12: "상태 ·
 * 이름 · 주소 · 선택" in its panel), and a timeline's, its table shown or
 * not (seen 2026-11-15: its handle there).
 */
export function viewTitleMoves(view: ViewLike) {
  return !view.titleFirst && (viewTitleFree(view) || view.type === "table" || view.type === "list" || view.type === "timeline");
}

function strings(value: unknown): string[] | undefined {
  return Array.isArray(value) ? value.map(String) : undefined;
}

/**
 * Whether the view keeps a list of its shown properties rather than of its
 * hidden ones: its own list, or none yet for an older imported gallery, whose
 * cards show no property until one is chosen (as `visibleViewProperties`
 * reads it).
 */
function listsShown(view: ViewLike) {
  return strings(view.config.visibleProperties) !== undefined
    || (view.type === "gallery" && view.config.notionPropertySettings !== undefined);
}

/** The properties a view lists as shown, including an older table's column list. */
function shownList(view: ViewLike) {
  if (listsShown(view)) return strings(view.config.visibleProperties) ?? [];
  const columns = strings(view.config.visiblePropertyIds);
  return columns?.length ? columns : undefined;
}

/**
 * A view's properties as Notion's "속성 표시 여부" lists them (seen
 * 2026-09-30): the shown ones in the view's order, then the hidden ones, in
 * its order too. Every view lists its title where its order puts it — a
 * table's wherever it was moved, so "모두 표시하기" leaves it there (seen
 * 2026-11-07), a list's too (viewTitleMoves), and a board's or a gallery's too:
 * first as one made in the app has it, last where the API left it out (seen
 * 2026-11-12: "주소 · 숫자 · 이름", the title without a handle).
 */
export function viewPropertyVisibility<Property extends TablePropertyLike>(view: ViewLike, properties: readonly Property[]) {
  const listed = shownList(view);
  const hidden = new Set(strings(view.config.hiddenProperties) ?? []);
  const ordered = orderTableProperties(properties, strings(view.config.propertyOrder) ?? strings(view.config.visiblePropertyIds));
  const isShown = (property: Property) => property.type === "title" ? !viewTitleFree(view) || !hidden.has(property.id)
    : listed ? listed.includes(property.id) : !hidden.has(property.id);
  return {
    shown: ordered.filter(isShown),
    hidden: ordered.filter((property) => !isShown(property)),
  };
}

/**
 * The view's settings with these shown and hidden properties, in these
 * orders: a view that lists its shown properties keeps listing them, any
 * other lists its hidden ones. A change puts the title first but in a table
 * or a list (viewTitleMoves), as Notion's board does with a title its API left
 * last (seen 2026-11-12: "주소 · 숫자 · 이름" became "이름 · 주소 · 숫자 · …").
 */
function configWith(view: ViewLike, properties: readonly TablePropertyLike[], shownIds: readonly string[], hidden: readonly string[]) {
  const titles = new Set(properties.filter((property) => property.type === "title").map((property) => property.id));
  const shown = viewTitleMoves(view) ? shownIds
    : [...shownIds.filter((id) => titles.has(id)), ...shownIds.filter((id) => !titles.has(id))];
  const next: Record<string, unknown> = { ...view.config, propertyOrder: [...shown, ...hidden] };
  delete next.visiblePropertyIds;
  if (listsShown(view)) {
    next.visibleProperties = shown.filter((id) => !titles.has(id));
    // A title hidden where it hides (viewTitleFree) is listed as hidden.
    const hiddenTitles = hidden.filter((id) => titles.has(id));
    if (hiddenTitles.length) next.hiddenProperties = hiddenTitles;
    else delete next.hiddenProperties;
  } else {
    next.hiddenProperties = [...hidden];
    delete next.visibleProperties;
  }
  return next;
}

/**
 * Shows or hides one property. As in Notion, a property shown again goes to
 * the end of the shown ones and a hidden one to the start of the hidden ones;
 * the title stays shown.
 */
export function viewConfigAfterPropertyShown(view: ViewLike, properties: readonly TablePropertyLike[], propertyId: string, show: boolean) {
  const target = properties.find((property) => property.id === propertyId);
  if (!target || (target.type === "title" && !viewTitleFree(view))) return { ...view.config };
  const { shown, hidden } = viewPropertyVisibility(view, properties);
  const shownIds = shown.map(({ id }) => id).filter((id) => id !== propertyId);
  const hiddenIds = hidden.map(({ id }) => id).filter((id) => id !== propertyId);
  return show
    ? configWith(view, properties, [...shownIds, propertyId], hiddenIds)
    : configWith(view, properties, shownIds, [propertyId, ...hiddenIds]);
}

/** Shows every property, or hides all but the title, keeping their order. */
export function viewConfigAfterAllPropertiesShown(view: ViewLike, properties: readonly TablePropertyLike[], show: boolean) {
  const { shown, hidden } = viewPropertyVisibility(view, properties);
  const all = [...shown, ...hidden];
  if (show) return configWith(view, properties, all.map(({ id }) => id), []);
  const titles = all.filter((property) => property.type === "title");
  return configWith(view, properties, titles.map(({ id }) => id), all.filter((property) => property.type !== "title").map(({ id }) => id));
}

/**
 * Moves a property before or after another of the same list (shown or
 * hidden); the title keeps its place, first if it is, but where it moves
 * (viewTitleMoves).
 */
export function viewConfigAfterPropertyMoved(
  view: ViewLike,
  properties: readonly TablePropertyLike[],
  sourceId: string,
  targetId: string,
  side: "before" | "after",
) {
  const { shown, hidden } = viewPropertyVisibility(view, properties);
  const shownIds = shown.map(({ id }) => id);
  const hiddenIds = hidden.map(({ id }) => id);
  const list = shownIds.includes(sourceId) ? shownIds : hiddenIds;
  const pinned = new Set(viewTitleMoves(view) ? [] : properties.filter((property) => property.type === "title").map(({ id }) => id));
  if (sourceId === targetId || !list.includes(targetId) || pinned.has(sourceId)) return null;
  const moved = list.filter((id) => id !== sourceId);
  const first = moved.length > 0 && pinned.has(moved[0]) ? 1 : 0;
  moved.splice(Math.max(first, moved.indexOf(targetId) + (side === "after" ? 1 : 0)), 0, sourceId);
  return list === shownIds ? configWith(view, properties, moved, hiddenIds) : configWith(view, properties, shownIds, moved);
}
