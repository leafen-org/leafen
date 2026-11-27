import { createContext, useContext, useEffect, useSyncExternalStore } from "react";
import { reorderBoardOptionsById, type BoardOption } from "./boardViewModel";
import { listedViewGroups, manualAddedLast, tableConfigAfterGroupMoved, tableKeptGroupKeys, viewGroupOrderIndex } from "./tableGroupSettingsModel";
import { NO_BOARD_GROUP, tableGroupsFollowHandOrder } from "./tableGrouping";
import { optionGroupsStartOrder } from "./optionGroupOrder";
import type { GroupPage } from "./GroupPageGlyph";
import type { StatusStage } from "./propertyStatusPolicy";

/** A board's group as its settings list it: its column's name, color and box. */
export type BoardSettingsGroup = { key: string; label: string; color?: string; checked?: boolean; page?: GroupPage; statusStage?: StatusStage; statusOption?: boolean };

/**
 * What a board shows its grouping settings: its columns as the list shows
 * them (listedViewGroups), which are hidden by hand, which while empty and
 * which pinned, and the same moves and hiding its columns' own handles make.
 */
export type BoardGroupsSurface = {
  viewId: string;
  groups: readonly BoardSettingsGroup[];
  hidden: ReadonlySet<string>;
  /** The groups left out while "빈 그룹 숨기기" hides the empty ones, their eyes shut. */
  hiddenAsEmpty: ReadonlySet<string>;
  pinned: ReadonlySet<string>;
  movable: boolean;
  move(sourceKey: string, targetKey: string, side: "before" | "after"): void;
  setHidden(key: string, hidden: boolean): void;
  setPinned(key: string, pinned: boolean): void;
  setAllHidden(hidden: boolean): void;
};

type Store = { get(): BoardGroupsSurface | null; publish(surface: BoardGroupsSurface | null): void; subscribe(listener: () => void): () => void };

export function createBoardGroupsStore(): Store {
  let current: BoardGroupsSurface | null = null;
  const listeners = new Set<() => void>();
  return {
    get: () => current,
    publish(surface) { current = surface; for (const listener of listeners) listener(); },
    subscribe(listener) { listeners.add(listener); return () => { listeners.delete(listener); }; },
  };
}

// The board draws in the view and its settings open from the toolbar, which
// share the database's mount; this carries the board's groups between them.
export const BoardGroupsContext = createContext<Store | null>(null);

const none = () => null;

/** The groups a board shows, for its own settings. */
export function useBoardGroups(viewId: string): BoardGroupsSurface | null {
  const store = useContext(BoardGroupsContext);
  const surface = useSyncExternalStore(store?.subscribe ?? (() => () => {}), store?.get ?? none, none);
  return surface?.viewId === viewId ? surface : null;
}

/** Shows a board's groups to its settings while it is drawn. */
function usePublishBoardGroups(surface: BoardGroupsSurface) {
  const store = useContext(BoardGroupsContext);
  useEffect(() => { store?.publish(surface); }, [store, surface]);
  useEffect(() => () => { if (store?.get()?.viewId === surface.viewId) store.publish(null); }, [store, surface.viewId]);
}

type Column = { key: string; label: string; rows: readonly unknown[] };

/** A status board's "정렬": in its order at first, the reverse or by hand, as Notion's (seen 2026-10-24). */
export function boardStatusSort(viewConfig: object) {
  const sort = (viewConfig as Config).groupSort;
  return sort === "manual" || sort === "descending" ? sort : "ascending";
}

/**
 * Whether a status board by hand keeps an order of its own, as one made by
 * Notion's API by hand does (by name, seen 2026-11-12): its columns move in
 * it, as an option board's do, not by reordering the status's options.
 */
export function boardStatusKeepsViewOrder(viewConfig: object) {
  return boardStatusSort(viewConfig) === "manual" && Array.isArray((viewConfig as Config).groupOrder);
}

/**
 * A board's columns as its grouping settings order and show them, as
 * Notion's: the group for no value first while they sort by value
 * (noValueGroupLeads) — by name either way, a status's in its order or the
 * reverse (seen 2026-11-08); by hand where the view's own order
 * (`groupOrder`, seen 2026-10-30: moving a column left the property's
 * options as they were) puts it, else last, as a person's (seen 2026-11-08).
 * Groups the order doesn't name follow it in their options' order — so a
 * group added from "+ 신규 그룹" goes after the group for no value. A board by
 * options from before that order keeps its options' order with the group for
 * no value where `noValueAt` put it; with neither, its options and the group
 * for no value go by name, as a new board's (optionGroupsStartOrder). Groups by what cards hold come
 * in their own order (boardValueGroups), by hand the group for no value
 * where the order puts it, those added from "+ 신규 그룹" after it in the
 * order they were added. "빈 그룹 숨기기" leaves out the columns without cards.
 */
export function arrangeBoardColumns<C extends Column>(columns: readonly C[], viewConfig: object, valueGroups: boolean, propertyType = ""): C[] {
  const config = viewConfig as Config;
  const none = columns.filter(({ key }) => key === NO_BOARD_GROUP);
  const named = columns.filter(({ key }) => key !== NO_BOARD_GROUP);
  const order = Array.isArray(config.groupOrder) ? config.groupOrder.map(String) : null;
  // Groups added by hand show though empty, as Notion's (seen 2026-11-12, empty groups hidden).
  const kept = tableKeptGroupKeys(config);
  const byOrder = (kept: readonly string[]) => {
    const first = columns.map(({ key }) => key);
    const at = (key: string) => { const index = viewGroupOrderIndex(kept, key); return index < 0 ? kept.length + first.indexOf(key) : index; };
    return [...columns].sort((left, right) => at(left.key) - at(right.key));
  };
  let arranged = [...columns];
  if (valueGroups) {
    // By hand with no order of its own yet, groups added from "+ 신규 그룹" come after the group for no value (manualAddedLast).
    const added = new Set(manualAddedLast(config, propertyType));
    if (tableGroupsFollowHandOrder(config, propertyType)) arranged = order ? byOrder(order)
      : [...columns.filter(({ key }) => !added.has(key)), ...columns.filter(({ key }) => added.has(key))];
  } else if (propertyType === "status") {
    const sort = boardStatusSort(config);
    arranged = sort === "manual" ? (order ? byOrder(order) : [...named, ...none]) : [...none, ...(sort === "descending" ? named.reverse() : named)];
  } else if (config.groupSort === "ascending" || config.groupSort === "descending") {
    const direction = config.groupSort === "descending" ? -1 : 1;
    arranged = [...none, ...named.sort((a, b) => direction * a.label.localeCompare(b.label))];
  } else if (order) {
    arranged = byOrder(order);
  } else if (typeof config.noValueAt === "number") {
    arranged = [...named];
    arranged.splice(Math.max(0, Math.min(arranged.length, Math.trunc(config.noValueAt))), 0, ...none);
  } else if (propertyType === "person") {
    // A person added from "+ 신규 그룹" stands after the group for no one, as Notion's (seen 2026-11-12).
    arranged = [...named.filter(({ key }) => !kept.has(key)), ...none, ...named.filter(({ key }) => kept.has(key))];
  } else if (propertyType === "select" || propertyType === "multi_select") {
    // With no order of its own yet, as a new grouping by its options starts (optionGroupsStartOrder): by name.
    arranged = byOrder(optionGroupsStartOrder(propertyType, columns, NO_BOARD_GROUP) ?? []);
  } else {
    arranged = [...none, ...named];
  }
  if (boardHidesEmptyGroups(config, valueGroups)) arranged = arranged.filter((column) => column.rows.length > 0 || kept.has(column.key));
  return arranged;
}

/**
 * Whether a board leaves out its empty groups: by options, people or a box
 * only when told to; by what cards hold unless told not to, its group for no
 * value too (Notion's, seen 2026-10-11 and 2026-11-12).
 */
export function boardHidesEmptyGroups(viewConfig: object, valueGroups: boolean) {
  const config = viewConfig as Config;
  return valueGroups ? config.hideEmptyGroups !== false : config.hideEmptyGroups === true;
}

type Config = Readonly<Record<string, unknown>>;

/**
 * The surface a board gives its settings: moving a group by hand keeps the
 * view's own order, as dragging its column does (a sort by name gives way),
 * the group for no value too as Notion's (seen 2026-11-08), a status's
 * reorders its options; hiding one is its column's "숨기기"; pinning one
 * lists it beside the board.
 */
export function boardGroupsSurface({ viewId, columns, hidden, valueGroups, propertyType, options, config: viewConfig, readOnly, writeOptions,
  replaceConfig, setHidden }: {
  viewId: string;
  columns: readonly (BoardSettingsGroup & { rows: readonly unknown[] })[];
  hidden: ReadonlySet<string>;
  valueGroups: boolean;
  /** The kind of property it groups by: a status's sort in its order at first. */
  propertyType: string;
  options: readonly BoardOption[];
  config: object;
  readOnly: boolean;
  writeOptions(options: BoardOption[]): void;
  replaceConfig(config: Record<string, unknown>): void;
  setHidden(key: string, hidden: boolean): void;
}): BoardGroupsSurface {
  const config = viewConfig as Config;
  const status = !valueGroups && propertyType === "status" && !boardStatusKeepsViewOrder(viewConfig);
  const ordered = arrangeBoardColumns(columns, { ...config, hideEmptyGroups: false }, valueGroups, propertyType);
  const keys = ordered.map(({ key }) => key);
  const kept = tableKeptGroupKeys(config);
  const hiddenAsEmpty = new Set(boardHidesEmptyGroups(config, valueGroups) ? ordered.filter(({ key, rows }) => !rows.length && !kept.has(key)).map(({ key }) => key) : []);
  const listed = listedViewGroups(ordered, ({ key }) => !hidden.has(key) && !hiddenAsEmpty.has(key));
  const fixed = (key: string) => key === NO_BOARD_GROUP || key.startsWith("unknown:");
  const pinned = boardPinnedGroupKeys(config);
  return {
    viewId, hidden, hiddenAsEmpty, pinned, movable: !readOnly,
    groups: listed.map(({ key, label, color, checked, page, statusStage, statusOption }) => ({ key, label, color, checked, page, statusStage, statusOption })),
    move(sourceKey, targetKey, side) {
      if (readOnly || sourceKey === targetKey) return;
      if (!status) {
        if (!valueGroups && sourceKey.startsWith("unknown:")) return;
        const next = tableConfigAfterGroupMoved({ ...config, groupSort: valueGroups || propertyType === "status" ? "manual" : undefined }, keys, sourceKey, targetKey, side);
        if (next) replaceConfig(next);
        return;
      }
      if (fixed(sourceKey) || fixed(targetKey)) return;
      const after = keys[keys.indexOf(targetKey) + 1];
      writeOptions(reorderBoardOptionsById(options, sourceKey, side === "before" ? targetKey : after && !fixed(after) ? after : null));
      // Moved by hand, a status's sorts by hand.
      if (config.groupSort !== "manual") replaceConfig({ ...config, groupSort: "manual" });
    },
    setHidden: (key, isHidden) => { if (!readOnly) setHidden(key, isHidden); },
    setPinned: (key, isPinned) => { if (!readOnly) replaceConfig(boardConfigAfterGroupPinned(config, key, isPinned)); },
    setAllHidden: (isHidden) => { if (!readOnly) replaceConfig({ ...config, hiddenGroups: isHidden ? keys : undefined }); },
  };
}

/**
 * Whether a board's group shows no count: its counts hide and show all at
 * once, as Notion's board has one count for the view (seen 2026-11-12: a
 * column's "집계 표시" showed every column's, "집계 숨기기" hid them all, a
 * board made through the API had none, its later groups too); a list of
 * groups whose counts hide, as a starter's board keeps, hides theirs.
 */
export function boardGroupCountHidden(viewConfig: object, key: string) {
  const config = viewConfig as Config;
  return config.groupCountsHidden === true || (Array.isArray(config.hiddenGroupCounts) && config.hiddenGroupCounts.map(String).includes(key));
}

/** A board's settings with all its groups' counts shown, or hidden. */
export function boardConfigWithGroupCounts(viewConfig: object, shown: boolean): Record<string, unknown> {
  return { ...viewConfig, groupCountsHidden: shown ? undefined : true, hiddenGroupCounts: undefined };
}

/** The groups a board lists under "고정된 그룹". */
export function boardPinnedGroupKeys(viewConfig: object): ReadonlySet<string> {
  const pinned = (viewConfig as Config).pinnedGroups;
  return new Set(Array.isArray(pinned) ? pinned.map(String) : []);
}

/** A board's settings with a group pinned beside it, or let go. */
export function boardConfigAfterGroupPinned(viewConfig: object, key: string, pinned: boolean): Record<string, unknown> {
  const next = new Set(boardPinnedGroupKeys(viewConfig));
  if (pinned) next.add(key); else next.delete(key);
  return { ...viewConfig, pinnedGroups: next.size ? [...next] : undefined };
}

/** Publishes a board's groups for its settings while the board is drawn. */
export function BoardGroupsPublisher({ surface }: { surface: BoardGroupsSurface }) {
  usePublishBoardGroups(surface);
  return null;
}
