import type { ReactNode } from "react";
import { FilterIcon, Plus } from "@/icons/leafen";
import type { DbProperty, FilterGroup } from "@/lib/types";
import { defaultOperator, defaultValue } from "./DatabaseFilterEditors";
import { DatabaseAdvancedFilterEditor } from "./DatabaseAdvancedFilterEditor";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { filterRuleCount, updateFilterGroupAt as updateGroup, viewAdvancedFilter } from "./databaseToolbarModel";
import { PhoneAdvancedFilter } from "./DatabasePhoneAdvancedFilter";
import { databaseViewLabels } from "./databaseViewLabels";

type ConfiguredView = { config: Record<string, unknown> };


/**
 * A view's advanced filter, as the database toolbar's filter menu and the
 * library's share it: its editor of rules and groups (DatabaseAdvancedFilterEditor),
 * how many rules it holds, and "고급 필터 추가", which starts it with a rule
 * waiting for its value. Each change keeps the group with the view's older flat filters.
 */
export function advancedFilterParts<View extends ConfiguredView>({ view, properties, snapshot, onUpdateView }: {
  view: View;
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  onUpdateView(view: View): void;
}): {
  editor: ReactNode;
  /** A phone's: the rules as Notion's sheet rows, "필터 제거" leaving the filter empty and calling back. */
  sheetEditor(onEmpty: () => void): ReactNode;
  count: number;
  add(): void;
} {
  const root = viewAdvancedFilter(view);
  const commitTree = (next: FilterGroup) => onUpdateView({
    ...view,
    config: { ...view.config, filterGroup: next, filters: next.filters, filterConjunction: next.conjunction },
  });
  const editor = (
    <DatabaseAdvancedFilterEditor root={root} properties={properties} snapshot={snapshot} onCommit={commitTree}
      onRemoveAll={() => commitTree({ conjunction: "and", filters: [], groups: [] })} />
  );
  const add = () => {
    const property = properties[0];
    if (!property || filterRuleCount(root) > 0) return;
    commitTree(updateGroup(root, [], (group) => ({
      ...group, filters: [...group.filters, { propertyId: property.id, operator: defaultOperator(property), value: defaultValue(property) }],
    })));
  };
  const sheetEditor = (onEmpty: () => void) => (
    <PhoneAdvancedFilter root={root} properties={properties} snapshot={snapshot} onCommit={commitTree}
      onRemoveAll={() => { commitTree({ conjunction: "and", filters: [], groups: [] }); onEmpty(); }} />
  );
  return { editor, sheetEditor, count: filterRuleCount(root), add };
}

/**
 * The last row of a filter's property picker, as Notion's "+ 필터" (seen
 * 2026-11-11): "+ 고급 필터 추가" starting the advanced filter, or, once it
 * has rules, "고급 필터" with their count grey at its end ("규칙 2개"),
 * opening it.
 */
export function advancedFilterPickerRow(count: number, { onAdd, onOpen }: { onAdd(): void; onOpen(): void }) {
  const labels = databaseViewLabels();
  return count > 0
    ? { icon: <FilterIcon size="regular" />, label: labels.sheetAdvancedFilter, hint: labels.advancedFilterPill(count), onClick: onOpen }
    : { icon: <Plus size="regular" />, label: labels.addAdvancedFilter, onClick: onAdd };
}
