"use client";

import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { placeAround, popoverPlacementStyle, type PopoverRect, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { DbProperty, Page } from "@/lib/types";
import { PropertyCell } from "./PropertyCell";
import { PropertyIcon } from "./PropertyIcon";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import styles from "./DatabaseRowPropertyPopover.module.css";

/**
 * A row's value of one property, edited by the row or card whose menu's
 * "속성 편집 ›" chose it, as Notion's opens that value's editor there (seen
 * 2026-10-11): the property's name over its cell, whose own picker opens at
 * once — a select's options, a date's calendar — or whose field takes the
 * caret. Escape or a press outside closes it.
 */
export function DatabaseRowPropertyPopover({ anchor, row, property, environment, onClose }: {
  anchor: PopoverRect;
  row: Page;
  property: DbProperty;
  environment: PropertyCellEnvironment;
  onClose(): void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(ref, (size, viewport) => placeAround(anchor, size, viewport, { gap: 4 }),
    [anchor], undefined, { kind: "picker", onEscape: onClose, onOutsidePress: onClose });
  const opened = useRef(false);
  useEffect(() => {
    const cell = ref.current?.querySelector<HTMLElement>("[data-row-property-popover-cell]");
    if (!place || opened.current || !cell) return;
    opened.current = true;
    const picker = cell.querySelector<HTMLElement>("button[aria-haspopup='dialog']:not(:disabled)");
    if (picker) picker.click();
    else cell.querySelector<HTMLElement>("input, textarea, [contenteditable='true']")?.focus();
  }, [place]);
  if (typeof document === "undefined") return null;
  return createPortal(
    <div ref={ref} className={styles.popover} role="dialog" aria-label={property.name} data-row-property-popover={property.id}
      style={place ? popoverPlacementStyle(place) : { visibility: "hidden" }}>
      <div className={styles.name}>
        <PropertyIcon property={property} size="small" />
        <span>{property.name}</span>
      </div>
      <div className={styles.cell} data-row-property-popover-cell>
        <PropertyCell row={row} prop={property} presentation="rowDetail" autoFocus {...environment} />
      </div>
    </div>,
    document.body,
  );
}
