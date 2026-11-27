import type { PropertyType } from "@/lib/types";
import type { UniqueIdSource } from "@/lib/databaseUniqueIdState";
import { TableNewPropertyBox } from "./TableAddProperty";

// A column menu's "insert left" and "insert right", as Notion's (seen
// 2026-09-30, 2026-11-14): the menu gives way to the box the header's "+"
// opens (TableNewPropertyBox), its name field focused, over the empty column
// the table makes room for at that edge of the column (tableNewColumn.ts),
// 4px into the header line. Choosing a type adds the property there, with no
// menu after it but a relation's database (the column goes as the box
// closes); Escape or a click elsewhere adds nothing. However it closes, the
// focus stays on the page with no ring on the column's head, as the column
// menu it came from leaves it (Notion's, seen 2026-11-17 in Chrome, no
// script before the action: Escape, or a type pressed, then Escape — no
// ring, the page's body focused).
export function TableInsertProperty({ anchor, omit, idSource, onAdd, onClose }: {
  /** The column's edge the new one goes at, the header's top and what opened it. */
  anchor: { left: number; top: number; trigger: HTMLElement };
  omit?: readonly PropertyType[];
  idSource?: UniqueIdSource;
  onAdd(type: PropertyType, typedName: string, icon?: string): void;
  onClose(): void;
}) {
  return <TableNewPropertyBox omit={omit} idSource={idSource} marker="data-table-insert-property"
    at={() => ({ left: anchor.left, right: anchor.left, top: anchor.top })} element={() => anchor.trigger}
    onFinish={(chosen) => { onClose(); if (chosen) onAdd(chosen.type, chosen.name, chosen.icon); }} />;
}
