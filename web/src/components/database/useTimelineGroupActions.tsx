import { useState, type ReactNode } from "react";
import type { GraphProperty } from "@/contentGraph";
import { popoverAnchorOf } from "@/lib/popoverPlacement";
import { newNamedOption } from "./optionNameText";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import { groupOptionNames } from "./TableGroupAddRow";
import { tableConfigAfterGroupKept } from "./tableGroupSettingsModel";
import type { TimelineGroupActions } from "./TimelineGroupLanes";
import type { TimelineGroupLane } from "./timelineLaneProjection";
import { useKeepNewGroup } from "./useKeepNewGroup";
import { viewGroupAddedKey, viewGroupAddKind } from "./viewGroupAddKind";
import { ViewGroupOptions, type ViewGroupMenuAt } from "./ViewGroupOptions";

/**
 * What a grouped timeline's "+ 신규 그룹" adds, as a table's, list's and
 * board's (viewGroupAddKind; Notion's timeline alike, seen 2026-11-12); an
 * option only where its property may be written.
 */
export function timelineGroupAddKind(groupProperty: GraphProperty | undefined, config: Record<string, unknown>, writable: boolean, writesProperty: boolean) {
  const kind = writable ? viewGroupAddKind(groupProperty, config) : undefined;
  return kind === "option" && !writesProperty ? undefined : kind;
}

/**
 * A grouped timeline's group actions, as a table's and a list's: the head's
 * "⋯" with the group menu, its "+" and "+ 새로 만들기" making a row in the
 * group, and "+ 신규 그룹" adding what its grouping takes (timelineGroupAddKind).
 */
export function useTimelineGroupActions({ groupProperty, config, writable, groups, environment, createInGroup, createInTable, updateConfig, commitProperty, trashRows, openGroupSettings, loadMore, loadLimit }: {
  groupProperty?: GraphProperty;
  config: Record<string, unknown>;
  /** The view's own settings may change. */
  writable: boolean;
  /** The groups it shows, which a person, page or text added again would repeat. */
  groups: readonly TimelineGroupLane[];
  /** What a person's or a page's list reads. */
  environment?: PropertyCellEnvironment;
  createInGroup?(key: string): void;
  /** A group's row from the table's "+ 새로 만들기" (TimelineGroupActions.onAddInTable). */
  createInTable?(key: string): void;
  updateConfig(next: Record<string, unknown>): void;
  commitProperty?(property: GraphProperty): Promise<void>;
  trashRows?(rowIds: readonly string[]): void;
  openGroupSettings?(trigger: HTMLElement): void;
  /** A group's "더 불러오기" and its "⋯" (TimelineGroupActions). */
  loadMore?(key: string): void;
  loadLimit?: TimelineGroupActions["loadLimit"];
}): { actions: TimelineGroupActions; menu: ReactNode } {
  const [menu, setMenu] = useState<ViewGroupMenuAt | null>(null);
  const kind = timelineGroupAddKind(groupProperty, config, writable, Boolean(commitProperty));
  const keepGroup = useKeepNewGroup((key) => updateConfig(tableConfigAfterGroupKept(config, key)));
  function addGroup(value: string) {
    if (!groupProperty || !kind || !value.trim()) return;
    if (kind !== "option") {
      // A person's goes after the groups shown, as a table's (Notion's, seen 2026-11-12).
      const key = viewGroupAddedKey(kind, value.trim());
      return updateConfig(tableConfigAfterGroupKept(config, key, kind === "person" ? groups.map((group) => group.key) : undefined));
    }
    if (!commitProperty) return;
    const own = groupProperty.config as Record<string, unknown>;
    const options = Array.isArray(own.options) ? own.options : [];
    const option = newNamedOption(value);
    void commitProperty({ ...groupProperty, config: { ...own, options: [...options, option] } } as GraphProperty)
      .then(() => keepGroup(option.id));
  }
  const actions: TimelineGroupActions = {
    onAdd: createInGroup, onAddInTable: createInTable,
    onMenu: writable && groupProperty ? (group, element, naming) => setMenu({
      group: { ...group, rows: group.rowIds.map((id) => ({ row: { id } })) }, position: popoverAnchorOf(element), returnTo: element, naming }) : undefined,
    newGroup: kind && groupProperty ? {
      kind, property: groupProperty, environment,
      taken: kind === "option" ? groupOptionNames(groupProperty) : groups.map((group) => group.label ?? ""),
      has: (value) => kind !== "option" && groups.some((group) => group.key === viewGroupAddedKey(kind, value)),
      onAdd: addGroup,
    } : undefined,
    showCounts: config.showGroupCounts === true,
    onLoadMore: loadMore, loadLimit,
  };
  return {
    actions,
    menu: menu && groupProperty ? <ViewGroupOptions at={menu} property={groupProperty} config={config} structureLocked={!writable}
      onClose={() => setMenu(null)} onConfig={updateConfig}
      onProperty={(next) => void commitProperty?.(next)}
      onTrash={trashRows} onOpenSettings={openGroupSettings} /> : null,
  };
}
