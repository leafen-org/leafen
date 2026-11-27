"use client";

import { type MutableRefObject, type ReactNode, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { DbProperty, Page } from "@/lib/types";
import { popoverRectOf, type PopoverRect } from "@/lib/popoverPlacement";
import { PersonValueLookArea } from "./PersonBadge";
import { PropValue, type PropertyValueReadContext } from "./PropValue";
import { PropertyIcon } from "./PropertyIcon";
import type { GraphProperty } from "@/contentGraph";
import { SelectedRowsPropertyEditor, type SelectionValue } from "./TableSelectionProperties";
import { isEmptyRowProperty } from "./rowPropertiesModel";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import styles from "./databaseSheet";

// What a card's property slot shows nothing of, and so offers no "추가" for: what Leafen fills in itself.
const SLOTLESS_TYPES = new Set(["formula", "rollup", "created_time", "created_by", "last_edited_time", "last_edited_by", "unique_id", "button", "verification"]);

/** Whether an event began in the card's own page, not in a slot's editor
 * drawn over the page, whose events reach the card through React. */
export const fromWithinCard = (event: { currentTarget: Element; target: EventTarget }) => event.currentTarget.contains(event.target as Node);

export const graphPropertyOf = (property: DbProperty) => ({ id: property.id, dataSourceId: property.databaseId, name: property.name,
  type: property.type, config: property.config }) as unknown as GraphProperty;
const TEXT_TYPES = new Set(["title", "rich_text", "url", "email", "phone"]);
// What is typed in a slot's field is its row's own.
export const typed = (page: Page, environment: PropertyCellEnvironment): SelectionValue => (property, value, spans) => {
  const prop = { id: property.id, databaseId: property.dataSourceId, name: property.name, type: property.type, config: property.config, position: 0 } as DbProperty;
  if (TEXT_TYPES.has(property.type)) void environment.operations.setTextValue(page.id, prop, String(value ?? ""), environment.context, spans);
  else void environment.operations.setValue(page.id, prop, value, environment.context);
};

/** A card whose name is being typed — one being made, or one whose pencil
 * was pressed — shows under its name every property the cards show: its
 * value, or an empty one's icon, name and "추가", whose press opens its editor
 * there — a status its group's, a box its name, a rollup what it shows
 * (Notion's board and gallery, seen 2026-11-06). */
export function CardPropertySlots({ cardProps, context, page, rowId, environment, group, holdRef, ensureRow, onClosed, className, ...rest }: {
  cardProps: readonly DbProperty[]; context: PropertyValueReadContext;
  /** Its row, once made. */
  page?: Page; rowId?: string; environment?: PropertyCellEnvironment;
  /** Before its row is made, the grouping property's slot shows the column's own value. */
  group?: { propertyId: string; value: ReactNode };
  /** True from a slot's press until its editor closes: the name being typed does not end as it loses the caret. */
  holdRef: MutableRefObject<boolean>;
  /** Makes its row now, for a value set before its name. */
  ensureRow?(): Promise<string | null>;
  /** A slot's editor closed, or could not open. */
  onClosed(): void;
  className: string;
  "data-board-draft-slots"?: string;
}) {
  const { t } = useTranslation("boardView");
  const [editing, setEditing] = useState<{ propertyId: string; anchor: PopoverRect } | null>(null);
  const editable = Boolean(environment);
  function open(property: DbProperty, element: HTMLElement) {
    const anchor = popoverRectOf(element);
    if (!anchor || !editable) return;
    holdRef.current = true;
    void (rowId ? Promise.resolve(rowId) : ensureRow?.() ?? Promise.resolve(null)).then((made) => {
      // A box is checked or cleared at once.
      if (made && property.type === "checkbox") {
        void environment?.operations.setValue(made, property, !page?.properties?.[property.id], environment.context);
        close();
      } else if (made) setEditing({ propertyId: property.id, anchor });
      else close();
    });
  }
  function close() {
    setEditing(null);
    holdRef.current = false;
    onClosed();
  }
  const editingProperty = editing ? cardProps.find(({ id }) => id === editing.propertyId) : undefined;
  return <>
    <div className={className} data-card-property-slots {...rest}>
      <PersonValueLookArea look="card">
      {cardProps.map((property) => {
        const empty = !page || isEmptyRowProperty(page, property);
        if (empty && SLOTLESS_TYPES.has(property.type)) return null;
        const pressable = editable && !SLOTLESS_TYPES.has(property.type);
        const slot = (content: ReactNode) => (
          <span key={property.id} className={`${styles.cardLine} ${styles.draftSlot}`} data-draft-slot={property.id}
            data-draft-checkbox={property.type === "checkbox" ? "" : undefined}
            role={pressable ? "button" : undefined} tabIndex={pressable ? -1 : undefined}
            onMouseDown={(event) => { if (!pressable) return; event.preventDefault(); holdRef.current = true; }}
            onClick={(event) => { event.stopPropagation(); if (pressable) open(property, event.currentTarget); }}>
            {content}
          </span>
        );
        if (!empty && page) return slot(<PropValue context={context} row={page} prop={property} interactive={false} linksOpen checkboxName />);
        if (!page && group?.propertyId === property.id) return slot(group.value);
        if (property.type === "checkbox") return slot(page
          ? <PropValue context={context} row={page} prop={property} interactive={false} linksOpen checkboxName />
          : <><span className={styles.draftSlotBox} aria-hidden="true" />{property.name}</>);
        return slot(<span className={styles.draftSlotEmpty}><PropertyIcon property={property} size="small" />{t("draftSlot", { name: property.name })}</span>);
      })}
      </PersonValueLookArea>
    </div>
    {/* Its own picker over the slot, or a field there for what is typed (Notion's, seen 2026-11-06). */}
    {editing && editingProperty && page && environment && typeof document !== "undefined" ? createPortal(
      // Its corner 1px left of the slot's and 1.5px over it (Notion's, seen 2026-11-06: 387,338 on a slot at 388,339.4).
      <SelectedRowsPropertyEditor own at={{ x: editing.anchor.left - 1, y: editing.anchor.top - 1.5 }}
        size={{ width: editing.anchor.right - editing.anchor.left, height: editing.anchor.bottom - editing.anchor.top }}
        environment={environment} page={page} property={graphPropertyOf(editingProperty)} onValue={typed(page, environment)} onClose={close} />,
      document.body) : null}
  </>;
}
