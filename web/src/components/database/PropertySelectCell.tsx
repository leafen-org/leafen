import { useRef, useState } from "react";
import type { DbProperty, Page, SelectOption } from "@/lib/types";
import { Plus } from "../icons";
import { chipStyle, type ColorName, newOptionColor } from "./colors";
import { statusGroupColumns, statusOptionsInGroupOrder } from "./propertyStatusPolicy";
import { statusOptionsForRow } from "./statusRowOptions";
import { openEditorFromCellBackground } from "./PropertyCellMenu";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import styles from "./databaseSheet";
import { restoreFocusUnlessChosen } from "./tableCellKeys";
import { valueAsRead } from "./valueAsRead";
import { useCellText } from "./cellText";
import { PropertySelectPicker } from "./PropertySelectPicker";
import { readOnlyStop, useCellReadOnly } from "./cellReadOnly";

/**
 * A select's, a multi-select's or a status's cell: its chosen options and
 * the "+" that opens its picker, which is drawn only while open — a table's
 * closed cells each kept a picker's search, list and drag state before (270
 * of a 30-row, 40-property table's cells as it opened, 2026-11-12).
 */
export function PropertySelectCell({
  row,
  prop,
  multi,
  operations,
  context,
}: {
  row: Page;
  prop: DbProperty;
  multi: boolean;
} & PropertyCellEnvironment) {
  const t = useCellText();
  const readOnly = useCellReadOnly();
  const [open, setOpen] = useState(false);
  // The color a new option takes, shown on "생성" before it is made.
  const [newColor, setNewColor] = useState<ColorName>(newOptionColor);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const statusGroups = prop.type === "status" ? statusGroupColumns(prop.config) : [];
  const options =
    statusGroups.length > 0
      ? statusOptionsInGroupOrder(prop.config)
      : prop.config?.options ?? [];
  // Its pick lists no option standing for a whole group, its chip still naming one a row holds, and a row
  // of one of several databases its own database's options alone (statusOptionsForRow).
  const choices = statusGroups.length > 0 ? statusOptionsForRow(prop.config, row.id) : options;
  // A status that sets none shows its default chosen, as Notion's picker does.
  const raw = valueAsRead(row, prop);
  const selectedIds: string[] = multi
    ? Array.isArray(raw)
      ? Array.from(new Set(raw.map(String)))
      : []
    : raw
      ? [String(raw)]
      : [];
  const selected = selectedIds
    .map((id) => options.find((o) => o.id === id))
    .filter(Boolean) as SelectOption[];
  // A status always has one value, chosen from its groups (Notion, seen 2026-10-08).
  const status = prop.type === "status";

  function openMenu() {
    if (!readOnly) setOpen(true);
  }

  // The picker opens on an empty search each time, as Notion's.
  function closeMenu(restoreFocus = false) {
    setOpen(false);
    if (restoreFocus) {
      restoreFocusUnlessChosen(triggerRef.current);
    }
  }

  return (
    <div className={styles.selectCell}>
      {/* Chip/body click is a pointer shortcut; the sibling Plus button is the keyboard path. */}
      {/* eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-noninteractive-element-interactions */}
      <div
        className={styles.chips}
        role="group"
        aria-label={prop.name}
        data-empty={selected.length === 0 ? "true" : undefined}
        onClick={(event) => openEditorFromCellBackground(event, openMenu)}
      >
        {selected.length > 0 && <span className={styles.valueMark}>{selected.map((o) => (
          <span key={o.id} className={styles.chip} style={chipStyle(o.color)} data-status-chip={status ? "" : undefined}>
            <span className={styles.chipText}>{o.name}</span>
          </span>
        ))}</span>}
        {selected.length === 0 && <span className={styles.cellEmpty}>&nbsp;</span>}
        <button
          ref={triggerRef}
          type="button"
          className={styles.propertyCellEditTrigger}
          aria-label={
            multi
              ? t("propertyCell:editMultiSelect", { name: prop.name })
              : t("propertyCell:editSelect", { name: prop.name })
          }
          aria-haspopup="dialog"
          aria-expanded={open}
          {...readOnlyStop(readOnly)}
          onClick={openMenu}
        >
          <Plus size="tiny" aria-hidden="true" />
        </button>
      </div>
      {open && !readOnly && (
        <PropertySelectPicker row={row} prop={prop} multi={multi} operations={operations} context={context}
          options={choices} statusGroups={statusGroups} selectedIds={selectedIds} selected={selected}
          triggerRef={triggerRef} newColor={newColor} setNewColor={setNewColor} closeMenu={closeMenu} />
      )}
    </div>
  );
}
