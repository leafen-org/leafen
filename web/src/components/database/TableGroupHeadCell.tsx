import { memo, type RefObject } from "react";
import { GroupHeadContent, type GroupHeadGroup } from "./GroupHeadContent";
import styles from "./databaseSheet";

/** What a grouped table's group heads do, read as one is pressed (TableGrid keeps them current). */
export type TableGroupHeadWays = {
  toggle(groupKey: string): void;
  /** Its "⋯", or its name chip (`naming`, GroupHeadContent). */
  menu(groupKey: string, element: HTMLElement, naming?: boolean): void;
  add(groupKey: string): void;
};

/**
 * A grouped table's group head's one cell across the table (GroupHeadContent),
 * drawn again only for what it shows: its row is the table's to place, and a
 * row made in a group moved every head under it, each drawn whole again for
 * its new place (2026-11-15).
 */
export const TableGroupHeadCell = memo(function TableGroupHeadCell({ group, span, collapsed, count, menu, add, ways }: {
  group: GroupHeadGroup;
  /** The columns it spans: the row handles' and every property's. */
  span: number;
  collapsed: boolean;
  count?: number;
  menu: boolean;
  add: boolean;
  ways: RefObject<TableGroupHeadWays | null>;
}) {
  return (
    <div className={styles.tableGroupHeaderInner} role="gridcell" aria-colspan={span}>
      <GroupHeadContent group={group} collapsed={collapsed} count={count} onToggle={() => ways.current?.toggle(group.key)}
        onMenu={menu ? (element, naming) => ways.current?.menu(group.key, element, naming) : undefined}
        onAdd={add ? () => ways.current?.add(group.key) : undefined} />
    </div>
  );
}, (before, after) => before.span === after.span && before.collapsed === after.collapsed && before.count === after.count
  && before.menu === after.menu && before.add === after.add && before.ways === after.ways && sameFace(before.group, after.group));

/** Whether two groups show alike in their head: the rows under them are not its. */
function sameFace(before: GroupHeadGroup, after: GroupHeadGroup) {
  return before === after || (before.key === after.key && before.label === after.label && before.color === after.color
    && before.checkbox === after.checkbox && before.sprintPlace === after.sprintPlace && before.statusStage === after.statusStage
    && before.statusOption === after.statusOption && before.person === after.person
    && JSON.stringify(before.page ?? null) === JSON.stringify(after.page ?? null));
}
