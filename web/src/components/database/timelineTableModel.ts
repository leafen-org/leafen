import { comparePropertyNames } from "./viewPropertyOrder";

/**
 * A timeline's table panel lists the properties chosen for it, apart from
 * the properties its bars show, as Notion's "Table properties". Its title
 * hides and moves as any of them, as Notion's (seen 2026-11-11: "Owner |
 * Name"); settings that don't list it show it first. A property the settings
 * do not list stays out of the panel.
 */
export type TimelineTableProperty = { id: string; visible: boolean; width?: number; wrap?: boolean };

// A column 200px, the title's 280px, the panel as wide as its columns
// however far it reaches, as Notion's (seen 2026-11-12 at 1440).
const TIMELINE_TABLE_COLUMN_WIDTH = 200;
const TIMELINE_TABLE_TITLE_WIDTH = 280;
const TIMELINE_TABLE_MIN_WIDTH = 60;
const TIMELINE_TABLE_MAX_WIDTH = 2000;

type PropertyLike = { id: string; name: string; type: string };

/** The panel settings a view config holds, without unusable entries. */
export function timelineTableProperties(config: Record<string, unknown> | undefined): TimelineTableProperty[] {
  const raw = Array.isArray(config?.timelineTableProperties) ? config.timelineTableProperties : [];
  const seen = new Set<string>();
  const entries: TimelineTableProperty[] = [];
  for (const item of raw) {
    if (!item || typeof item !== "object") continue;
    const entry = item as Record<string, unknown>;
    if (typeof entry.id !== "string" || !entry.id || seen.has(entry.id)) continue;
    seen.add(entry.id);
    entries.push({
      id: entry.id,
      visible: entry.visible === true,
      ...(typeof entry.width === "number" && entry.width >= 0 ? { width: entry.width } : {}),
      ...(entry.wrap === true ? { wrap: true } : {}),
    });
  }
  return entries;
}

/**
 * Every property, the listed ones first in their order; the title first and
 * shown where they don't list it; the rest hidden after them by name, as
 * Notion's "표" tab lists them (seen 2026-11-12, viewPropertyOrder.ts).
 */
export function timelineTableOptions<Property extends PropertyLike>(
  properties: readonly Property[],
  config: Record<string, unknown> | undefined,
) {
  const entries = timelineTableProperties(config);
  const byId = new Map(properties.map((property) => [property.id, property]));
  const listed = entries.flatMap((entry) => {
    const property = byId.get(entry.id);
    return property ? [{ property, entry }] : [];
  });
  const listedIds = new Set(listed.map((item) => item.property.id));
  const unlisted = properties.filter((property) => !listedIds.has(property.id));
  const titles = unlisted.filter((property) => property.type === "title")
    .map((property) => ({ property, entry: { id: property.id, visible: true } as TimelineTableProperty }));
  const rest = unlisted.filter((property) => property.type !== "title")
    .sort((left, right) => comparePropertyNames(left.name, right.name))
    .map((property) => ({ property, entry: { id: property.id, visible: false } as TimelineTableProperty }));
  return [...titles, ...listed, ...rest];
}

/** The columns the panel shows, the title's among them where it stands. */
export function timelineTableColumns<Property extends PropertyLike>(
  properties: readonly Property[],
  config: Record<string, unknown> | undefined,
) {
  return timelineTableOptions(properties, config)
    .filter((item) => item.entry.visible)
    .map((item) => ({
      property: item.property,
      title: item.property.type === "title",
      width: item.entry.width ?? (item.property.type === "title" ? TIMELINE_TABLE_TITLE_WIDTH : TIMELINE_TABLE_COLUMN_WIDTH),
      wrap: item.entry.wrap === true,
    }));
}

/**
 * The panel's settings as a table view's, so its list in "속성 표시 여부" reads
 * and changes as any view's does (propertyVisibilityModel.ts), as Notion's
 * "표" tab (seen 2026-11-04).
 */
export function timelineTableAsView<Property extends PropertyLike>(
  properties: readonly Property[],
  config: Record<string, unknown> | undefined,
) {
  const options = timelineTableOptions(properties, config);
  return {
    type: "table",
    titleFree: true,
    config: {
      propertyOrder: options.map(({ property }) => property.id),
      hiddenProperties: options.filter(({ entry }) => !entry.visible).map(({ property }) => property.id),
    } as Record<string, unknown>,
  };
}

/** The panel's settings back from a list's change to that table view. */
export function timelineTableFromView<Property extends PropertyLike>(
  properties: readonly Property[],
  config: Record<string, unknown> | undefined,
  changed: Readonly<Record<string, unknown>>,
): TimelineTableProperty[] {
  const options = new Map(timelineTableOptions(properties, config).map(({ property, entry }) => [property.id, entry]));
  const hidden = new Set(Array.isArray(changed.hiddenProperties) ? changed.hiddenProperties.map(String) : []);
  const order = Array.isArray(changed.propertyOrder) ? changed.propertyOrder.map(String) : [];
  return order.flatMap((id) => {
    const entry = options.get(id);
    return entry ? [{ ...entry, visible: !hidden.has(id) }] : [];
  });
}

/** The panel settings with one shown property's column at a new width. */
export function resizeTimelineTableProperty<Property extends PropertyLike>(
  properties: readonly Property[],
  config: Record<string, unknown> | undefined,
  id: string,
  width: number,
): TimelineTableProperty[] {
  const next = Math.round(Math.min(TIMELINE_TABLE_MAX_WIDTH, Math.max(TIMELINE_TABLE_MIN_WIDTH, width)));
  return timelineTableOptions(properties, config)
    .map((item) => item.property.id === id ? { ...item.entry, width: next } : item.entry);
}
