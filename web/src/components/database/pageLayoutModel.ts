import {
  PAGE_LAYOUT_MAX_PINNED, type DatabasePageLayout, type PageLayoutGroupItem, type PageLayoutModule,
} from "@/lib/databasePageLayout";

// The layout builder's changes, each giving a new layout: what Notion's
// builder does to its modules, its pinned properties and its property
// group (seen 2026-10-12).

export type PageLayoutPlace = "page" | "panel";

let sequence = 0;
export function pageLayoutId(prefix: string) {
  sequence += 1;
  return `${prefix}-${Date.now().toString(36)}-${sequence.toString(36)}`;
}

export function pinProperty(layout: DatabasePageLayout, propertyId: string): DatabasePageLayout {
  if (layout.heading.pinned.includes(propertyId) || layout.heading.pinned.length >= PAGE_LAYOUT_MAX_PINNED) return layout;
  return {
    ...withoutProperty(layout, propertyId),
    heading: { ...layout.heading, pinned: [...layout.heading.pinned, propertyId] },
  };
}

export function unpinProperty(layout: DatabasePageLayout, propertyId: string): DatabasePageLayout {
  return { ...layout, heading: { ...layout.heading, pinned: layout.heading.pinned.filter((id) => id !== propertyId) } };
}

/** Takes a property out of its module, its relations group and its place in the property group, before it goes elsewhere. */
function withoutProperty(layout: DatabasePageLayout, propertyId: string): DatabasePageLayout {
  const strip = (modules: readonly PageLayoutModule[]) => modules.flatMap((module): PageLayoutModule[] => {
    if (module.kind === "property") return module.propertyId === propertyId ? [] : [module];
    if (module.kind !== "relations" || !module.relations.some((relation) => relation.propertyId === propertyId)) return [module];
    const relations = module.relations.filter((relation) => relation.propertyId !== propertyId);
    return relations.length ? [{ ...module, relations }] : [];
  });
  return {
    ...layout,
    page: strip(layout.page),
    panel: strip(layout.panel),
    propertyGroup: layout.propertyGroup.filter((item) => item.kind !== "property" || item.propertyId !== propertyId),
  };
}

// The types "+" lists first among a database's properties: files, numbers,
// text and people (Notion's, seen 2026-11-13).
const ADD_MENU_FIRST_TYPES: readonly string[] = ["files", "number", "rich_text", "person"];

/**
 * "+"'s "기존 속성 사용", as Notion's (seen 2026-11-13): every property but
 * the title and those that are a card of their own already — pinned ones and
 * the property group's too, which the new card takes from there. Files,
 * numbers, text and people come first, then the rest, each in the
 * database's order.
 */
export function pageLayoutAddCandidates<Property extends { id: string; type: string }>(
  layout: DatabasePageLayout, properties: readonly Property[],
): Property[] {
  const carded = new Set([...layout.page, ...layout.panel].flatMap((module) => (module.kind === "property" ? [module.propertyId] : [])));
  const rank = (property: Property) => {
    const at = ADD_MENU_FIRST_TYPES.indexOf(property.type);
    return at < 0 ? ADD_MENU_FIRST_TYPES.length : at;
  };
  return properties.filter((property) => property.type !== "title" && !carded.has(property.id)).sort((a, b) => rank(a) - rank(b));
}

/** Adds a property as a module of its own after the given index of a place ("+" and "패널에 추가"). */
export function addPropertyModule(layout: DatabasePageLayout, place: PageLayoutPlace, index: number, propertyId: string): DatabasePageLayout {
  const next = unpinProperty(withoutProperty(layout, propertyId), propertyId);
  const list = [...next[place]];
  list.splice(Math.max(0, Math.min(index, list.length)), 0, { id: pageLayoutId("property"), kind: "property", propertyId });
  return { ...next, [place]: list };
}

export function moduleLocation(layout: DatabasePageLayout, moduleId: string): { place: PageLayoutPlace; index: number } | null {
  for (const place of ["page", "panel"] as const) {
    const index = layout[place].findIndex((module) => module.id === moduleId);
    if (index >= 0) return { place, index };
  }
  return null;
}

/** "위로 이동" / "아래로 이동": one step within its place. */
export function moveModule(layout: DatabasePageLayout, moduleId: string, step: -1 | 1): DatabasePageLayout {
  const at = moduleLocation(layout, moduleId);
  if (!at) return layout;
  const list = [...layout[at.place]];
  const target = at.index + step;
  if (target < 0 || target >= list.length) return layout;
  [list[at.index], list[target]] = [list[target], list[at.index]];
  return { ...layout, [at.place]: list };
}

/**
 * Whether a module can take one step, as Notion's moves show (seen
 * 2026-11-01): the property group and the properties' own cards pass each
 * other, and the page's discussion and its content each other, but not a
 * discussion turned off.
 */
export function canMoveModule(layout: DatabasePageLayout, moduleId: string, step: -1 | 1) {
  const at = moduleLocation(layout, moduleId);
  if (!at) return false;
  const neighbour = layout[at.place][at.index + step];
  const lower = (module: PageLayoutModule) => module.kind === "comments" || module.kind === "content";
  return Boolean(neighbour) && lower(neighbour) === lower(layout[at.place][at.index])
    && !(neighbour.kind === "comments" && !layout.options.pageComments);
}

/** "패널로 옮기기" / "페이지로 옮기기"; the content stays on the page. */
export function moveModuleTo(layout: DatabasePageLayout, moduleId: string, place: PageLayoutPlace): DatabasePageLayout {
  const at = moduleLocation(layout, moduleId);
  if (!at || at.place === place) return layout;
  const module = layout[at.place][at.index];
  if (module.kind === "content") return layout;
  return {
    ...layout,
    [at.place]: layout[at.place].filter((item) => item.id !== moduleId),
    [place]: [...layout[place], module],
  };
}

/** "레이아웃에서 제거": the property group and the content can't be removed. */
export function removeModule(layout: DatabasePageLayout, moduleId: string): DatabasePageLayout {
  const at = moduleLocation(layout, moduleId);
  if (!at) return layout;
  const module = layout[at.place][at.index];
  if (module.kind === "properties" || module.kind === "content") return layout;
  return { ...layout, [at.place]: layout[at.place].filter((item) => item.id !== moduleId) };
}

/** A relations group's relation drawn minimized, "↗ 작업 추가", or as a page section (Notion's "표시 옵션", seen 2026-11-06). */
export function setRelationDisplay(layout: DatabasePageLayout, moduleId: string, propertyId: string, display: "minimized" | "section"): DatabasePageLayout {
  const show = (modules: readonly PageLayoutModule[]) => modules.map((module) => module.id === moduleId && module.kind === "relations"
    ? { ...module, relations: module.relations.map((relation) => relation.propertyId === propertyId ? { ...relation, display } : relation) } : module);
  return { ...layout, page: show(layout.page), panel: show(layout.panel) };
}

/** A relation off its relations group, the group going with its last relation. */
export function removeGroupRelation(layout: DatabasePageLayout, moduleId: string, propertyId: string): DatabasePageLayout {
  const strip = (modules: readonly PageLayoutModule[]) => modules.flatMap((module) => module.id !== moduleId || module.kind !== "relations" ? [module]
    : module.relations.length > 1 ? [{ ...module, relations: module.relations.filter((relation) => relation.propertyId !== propertyId) }] : []);
  return { ...layout, page: strip(layout.page), panel: strip(layout.panel) };
}

/**
 * How a relations group's relation shows: a page section only while it
 * holds pages; empty, it is "↗ 작업 추가" under "관계형" whichever is chosen
 * (Notion's, seen 2026-11-07 on the page and in the builder's preview).
 */
export function relationShownAs(display: "minimized" | "section", value: unknown): "minimized" | "section" {
  return display === "section" && Array.isArray(value) && value.length > 0 ? "section" : "minimized";
}

/** A relation dragged to another place among its group's, before or after the one it was dropped on. */
export function dropGroupRelation(layout: DatabasePageLayout, moduleId: string, from: string, to: string, after: boolean): DatabasePageLayout {
  const move = (modules: readonly PageLayoutModule[]) => modules.map((module) => {
    if (module.id !== moduleId || module.kind !== "relations" || from === to) return module;
    const relations = [...module.relations];
    const at = relations.findIndex((relation) => relation.propertyId === from);
    if (at < 0 || !relations.some((relation) => relation.propertyId === to)) return module;
    const [moved] = relations.splice(at, 1);
    relations.splice(relations.findIndex((relation) => relation.propertyId === to) + (after ? 1 : 0), 0, moved);
    return { ...module, relations };
  });
  return { ...layout, page: move(layout.page), panel: move(layout.panel) };
}

/** "레이아웃에 추가": a group's relation becomes a module of its own right after the group (Notion's, seen 2026-11-06). */
export function takeRelationOut(layout: DatabasePageLayout, moduleId: string, propertyId: string): DatabasePageLayout {
  const at = moduleLocation(layout, moduleId);
  if (!at) return layout;
  const next = removeGroupRelation(layout, moduleId, propertyId);
  const index = next[at.place].findIndex((module) => module.id === moduleId);
  return addPropertyModule(next, at.place, index < 0 ? at.index : index + 1, propertyId);
}

/**
 * "고급 › 관계형 그룹으로 전환": a relation's own card becomes a relations
 * group holding it in its place, still a page section (Notion's, seen
 * 2026-11-14: its pages under its name, "관계형" over it only while empty).
 */
export function relationIntoGroup(layout: DatabasePageLayout, moduleId: string): DatabasePageLayout {
  const group = (modules: readonly PageLayoutModule[]) => modules.map((module): PageLayoutModule => module.id === moduleId && module.kind === "property"
    ? { id: pageLayoutId("relations"), kind: "relations", relations: [{ propertyId: module.propertyId, display: "section" }] } : module);
  return { ...layout, page: group(layout.page), panel: group(layout.panel) };
}

export function canRemoveModule(module: PageLayoutModule) {
  return module.kind === "property" || module.kind === "comments" || module.kind === "relations";
}

/**
 * The property group's items in order with every property it shows: the
 * listed ones, then those it leaves out (made after the layout), without
 * the pinned ones and those in modules of their own.
 */
export function propertyGroupItems(layout: DatabasePageLayout, propertyIds: readonly string[]): PageLayoutGroupItem[] {
  const placed = new Set(layout.heading.pinned);
  for (const module of [...layout.page, ...layout.panel]) {
    if (module.kind === "property") placed.add(module.propertyId);
    if (module.kind === "relations") for (const relation of module.relations) placed.add(relation.propertyId);
  }
  const known = new Set(propertyIds);
  const listed = layout.propertyGroup.filter((item) => item.kind === "section"
    || known.has(item.propertyId) && !placed.has(item.propertyId));
  const present = new Set(listed.flatMap((item) => (item.kind === "property" ? [item.propertyId] : [])));
  return [...listed, ...propertyIds.filter((id) => !placed.has(id) && !present.has(id))
    .map((propertyId) => ({ kind: "property" as const, propertyId }))];
}

/** "섹션 추가": a named section at the top of the group, over every property. */
export function addSection(layout: DatabasePageLayout, propertyIds: readonly string[], name: string): DatabasePageLayout {
  return {
    ...layout,
    propertyGroup: [{ kind: "section", id: pageLayoutId("section"), name }, ...propertyGroupItems(layout, propertyIds)],
  };
}

export function renameSection(layout: DatabasePageLayout, sectionId: string, name: string): DatabasePageLayout {
  return {
    ...layout,
    propertyGroup: layout.propertyGroup.map((item) => (item.kind === "section" && item.id === sectionId ? { ...item, name } : item)),
  };
}

export function removeSection(layout: DatabasePageLayout, sectionId: string): DatabasePageLayout {
  return { ...layout, propertyGroup: layout.propertyGroup.filter((item) => item.kind !== "section" || item.id !== sectionId) };
}

/** Drops a dragged module before or after another, in that one's place; the content stays on the page. */
export function dropModule(layout: DatabasePageLayout, moduleId: string, targetId: string, after: boolean): DatabasePageLayout {
  const from = moduleLocation(layout, moduleId), to = moduleLocation(layout, targetId);
  if (!from || !to || moduleId === targetId) return layout;
  const module = layout[from.place][from.index];
  if (module.kind === "content" && to.place === "panel") return layout;
  const next = { ...layout, [from.place]: layout[from.place].filter((item) => item.id !== moduleId) };
  const list = [...next[to.place]];
  list.splice(list.findIndex((item) => item.id === targetId) + (after ? 1 : 0), 0, module);
  return { ...next, [to.place]: list };
}

/** Drops a dragged item of the property group before or after another, the group listed in full first. */
export function dropGroupItem(layout: DatabasePageLayout, propertyIds: readonly string[], from: number, to: number, after: boolean): DatabasePageLayout {
  const items = propertyGroupItems(layout, propertyIds);
  if (from === to || !items[from] || !items[to]) return layout;
  const [item] = items.splice(from, 1);
  items.splice((to > from ? to - 1 : to) + (after ? 1 : 0), 0, item);
  return { ...layout, propertyGroup: items };
}

/** Drops a dragged pinned property before or after another, reordering the title's line. */
export function dropPinned(layout: DatabasePageLayout, from: number, to: number, after: boolean): DatabasePageLayout {
  const pinned = [...layout.heading.pinned];
  if (from === to || !pinned[from] || !pinned[to]) return layout;
  const [id] = pinned.splice(from, 1);
  pinned.splice((to > from ? to - 1 : to) + (after ? 1 : 0), 0, id);
  return { ...layout, heading: { ...layout.heading, pinned } };
}
