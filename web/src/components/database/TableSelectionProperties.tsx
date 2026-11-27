"use client";

import { type RefObject, useContext, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { DbProperty, Page, TextSpan } from "@/lib/types";
import { PropertyCell } from "./PropertyCell";
import { CellMenuPortal, CellPickerHostContext, type CellPickerHost } from "./PropertyCellMenu";
import { DatabasePagelessRowsContext } from "./databasePagelessRows";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import { PropertyIcon } from "./PropertyIcon";
import { graphFilePropertyValue } from "./files";
import { numberInputText, parseNumberInput } from "./numberFormat";
import styles from "./databaseSheet";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

// Notion's selection bar has a button for each of the first shown
// properties it can set, opening that property's picker over the button
// for every selected row (seen 2026-10-06; a files property's too, its
// upload and link picker, seen 2026-10-13). The picker shows the top
// selected row's value. A value typed in is typed in a small field there; a
// checkbox's button checks them all, or clears them all when all are
// checked. The bar holds buttons for six places, a place property's taking
// two (seen 2026-11-03 and 2026-11-10); how many show is selectionBarLayout's.
const PLACES = 6;
const PICKED = new Set(["select", "multi_select", "status", "person", "relation", "date", "place", "files"]);
const TYPED = new Set(["title", "rich_text", "number", "url", "email", "phone"]);
// A text property has its button among the rest, in the view's column order
// (seen 2026-11-07 and 2026-11-10: "2개 선택됨 · # Count · ☑ Done · ☰ Memo ·
// ☰ Panel"); the title has none, "⋯ › 속성 편집" still listing it.
const BAR_LEFT_OUT = new Set(["title"]);

// A lone property has no button: the bar has them only for two or more
// (Notion's, seen 2026-11-15 in a synthetic database: "1개 선택됨 🗑 ⋯" with
// only a text or only a checkbox property besides the title, "≡ ≡" with two
// text ones; in People "⋯" alone with About only, About and a text one of
// its owner's both).
export function selectionBarProperties(properties: readonly GraphProperty[]) {
  let taken = 0;
  const offered = properties.filter((property) => !BAR_LEFT_OUT.has(property.type) && (property.type === "checkbox" || PICKED.has(property.type) || TYPED.has(property.type)));
  return offered.length < 2 ? [] : offered
    .filter((property) => {
      if (taken >= PLACES) return false;
      taken += property.type === "place" ? 2 : 1;
      return true;
    });
}

/**
 * The bar's buttons in a view: a source without pages has them for the values
 * the reader writes on every row chosen, its own properties none
 * (databasePagelessRows.ts).
 */
export function useSelectionBarProperties(properties: readonly GraphProperty[], rowIds: readonly string[]) {
  const pageless = useContext(DatabasePagelessRowsContext);
  if (!pageless) return selectionBarProperties(properties);
  const writable = (rowIds.length ? rowIds : [""]).map((rowId) => pageless.writable(rowId));
  return selectionBarProperties(properties.filter(({ id }) => writable.every((ids) => ids.has(id))));
}

// How the buttons show goes by their widths with names, whatever the
// window's or the view's width (Notion's, seen 2026-11-11 at 768 and 1191
// wide windows and 2026-11-10 at 1975, inline and full page alike, over 14
// sets of names): when all of them together are 400px or narrower they all
// show as icons alone, 35px each; wider, they show with names, as many from
// the first as fit in 330px, the rest left out (e.g. "Count, Done, Link,
// Mail, Memo, Note2", 452px: the first four; "Co, Do, Li, Ma, Memox, …",
// 319px for five: five; "Cnt, Don, Lin, Mai, Mem, Not", 384px: six icons).
const ICONS_ALONE_WIDTH = 400;
const NAMED_WIDTH = 330;

export function selectionBarLayout(widths: readonly number[]): { iconsAlone: boolean; shown: number } {
  if (widths.reduce((sum, width) => sum + width, 0) <= ICONS_ALONE_WIDTH) return { iconsAlone: true, shown: widths.length };
  let used = 0, shown = 0;
  for (const width of widths) {
    if (shown > 0 && used + width > NAMED_WIDTH) break;
    used += width;
    shown += 1;
  }
  return { iconsAlone: false, shown };
}

/** `typed`: written as it is typed, saved once the typing ends (lib/typedWrites.ts). */
export type SelectionValue = (property: GraphProperty, value: unknown, spans?: TextSpan[], typed?: boolean) => void;

export function TableSelectionProperties({
  environment,
  page,
  properties,
  rows,
  onValue,
}: {
  environment?: PropertyCellEnvironment;
  /** The first selected row, as its cells read it: the row the pickers show; none in a bar drawn ahead (TableSelectionToolbar). */
  page?: Page;
  properties: readonly GraphProperty[];
  rows: readonly GraphRow[];
  onValue: SelectionValue;
}) {
  const [active, setActive] = useState<string | null>(null);
  // The buttons are measured with their names, then laid out by those widths,
  // each with its 1px line as Notion's button holds it.
  const named = properties.map((property) => `${property.id}\u0000${property.type}\u0000${property.name}`).join("\u0001");
  const [layout, setLayout] = useState<{ named: string; iconsAlone: boolean; shown: number } | null>(null);
  const listRef = useRef<HTMLSpanElement>(null);
  const measuring = layout?.named !== named;
  useLayoutEffect(() => {
    if (!measuring || !listRef.current) return;
    const widths = [...listRef.current.querySelectorAll<HTMLElement>("[data-selection-property]")]
      .map((button) => button.getBoundingClientRect().width + 1);
    setLayout({ named, ...selectionBarLayout(widths) });
  }, [measuring, named]);
  return (
    <span ref={listRef} className={styles.selectionProperties}>
      {properties.map((property, index) => (
        <SelectionPropertyButton key={property.id} active={active === property.id} environment={environment}
          page={page} property={property}
          iconAlone={!measuring && Boolean(layout?.iconsAlone)} left={!measuring && index >= (layout?.shown ?? properties.length)}
          onToggle={() => {
            if (property.type === "checkbox") {
              onValue(property, !rows.every((row) => Boolean(row.values[property.id]?.value)));
              return;
            }
            setActive((current) => current === property.id ? null : property.id);
          }}
          onClosed={() => setActive((current) => current === property.id ? null : current)}
          onValue={onValue} />
      ))}
    </span>
  );
}

function SelectionPropertyButton({
  active,
  environment,
  iconAlone,
  left,
  page,
  property,
  onClosed,
  onToggle,
  onValue,
}: {
  active: boolean;
  environment?: PropertyCellEnvironment;
  /** Its icon alone, the name its label. */
  iconAlone: boolean;
  /** Left out of the bar, as Notion keeps it, unseen. */
  left: boolean;
  page?: Page;
  property: GraphProperty;
  onClosed(): void;
  onToggle(): void;
  onValue: SelectionValue;
}) {
  const buttonRef = useRef<HTMLButtonElement>(null);
  const openPickers = useRef(0);
  const closed = useRef(onClosed);
  closed.current = onClosed;
  const host = useMemo<CellPickerHost>(() => ({
    buttonRef,
    opened() {
      openPickers.current += 1;
      return () => {
        openPickers.current -= 1;
        // A picker may give way to one of its own (an option's editor); the
        // editing ends once none is open.
        window.requestAnimationFrame(() => { if (openPickers.current === 0) closed.current(); });
      };
    },
  }), []);
  // A picker that did not open leaves the button as it was.
  useLayoutEffect(() => {
    if (!active) return;
    let frame = window.requestAnimationFrame(() => {
      frame = window.requestAnimationFrame(() => { if (openPickers.current === 0) closed.current(); });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [active]);
  return (
    <span className={styles.selectionProperty} data-cell-picker-host data-selection-property={property.id} hidden={left}>
      <button ref={buttonRef} type="button" className={styles.selectionPropertyButton}
        aria-haspopup={property.type === "checkbox" ? undefined : "dialog"}
        aria-expanded={property.type === "checkbox" ? undefined : active}
        aria-label={iconAlone ? property.name : undefined} title={iconAlone ? property.name : undefined}
        onClick={onToggle}>
        <PropertyIcon property={property} />
        {!iconAlone && <span>{property.name}</span>}
      </button>
      {active && page && <SelectionEditor host={host} buttonRef={buttonRef} environment={environment} page={page} property={property}
        onValue={onValue} onClosed={onClosed} />}
    </span>
  );
}

function SelectionEditor({ host, buttonRef, environment, page, property, onValue, onClosed, own = false }: {
  host: CellPickerHost; buttonRef: RefObject<HTMLButtonElement | null>; environment?: PropertyCellEnvironment;
  page: Page; property: GraphProperty; onValue: SelectionValue; onClosed(): void; own?: boolean;
}) {
  const picked = PICKED.has(property.type);
  return (
    <CellPickerHostContext.Provider value={host}>
      {picked && environment
        ? <PickedEditor environment={environment} page={page} property={property} onValue={onValue} own={own} />
        : !picked && <TypedEditor buttonRef={buttonRef} page={page} property={property} onValue={onValue} onClose={onClosed} plain={own} />}
    </CellPickerHostContext.Provider>
  );
}

/** The properties the selected rows' "⋯ › 속성 편집" lists: every one a value can be set for, the title too (Notion's, seen 2026-11-02). */
export function selectionEditableProperties(properties: readonly GraphProperty[]) {
  return properties.filter((property) => property.type === "title" || property.type === "checkbox"
    || PICKED.has(property.type) || TYPED.has(property.type));
}

/**
 * One property's editor for every selected row, opened from their "⋯ ›
 * 속성 편집" where the menu was, as Notion's (seen 2026-11-02: the title's
 * 240px field at the menu's corner, the first row's value in it).
 */
export function SelectedRowsPropertyEditor({ at, size, own = false, environment, page, property, onValue, onClose }: {
  at: { x: number; y: number }; environment?: PropertyCellEnvironment; page: Page; property: GraphProperty;
  /** What its picker covers, a card's property slot; a 240×32 point by default. */
  size?: { width: number; height: number };
  /** One row's own value: a picker writes it as its cell does, a typed value through onValue. */
  own?: boolean;
  onValue: SelectionValue; onClose(): void;
}) {
  const buttonRef = useRef<HTMLButtonElement>(null);
  const openPickers = useRef(0);
  const closed = useRef(onClose);
  closed.current = onClose;
  const host = useMemo<CellPickerHost>(() => ({
    buttonRef,
    opened() {
      openPickers.current += 1;
      return () => {
        openPickers.current -= 1;
        window.requestAnimationFrame(() => { if (openPickers.current === 0) closed.current(); });
      };
    },
  }), []);
  // The point is where the menu's rows start: in a right-to-left app the
  // field runs from it to the left; a slot's box is where it is.
  const rtl = !size && typeof document !== "undefined" && getComputedStyle(document.documentElement).direction === "rtl";
  const place = rtl ? { insetInlineStart: document.documentElement.clientWidth - at.x } : { left: at.x };
  return (
    <span className={styles.selectionPointHost} style={{ ...place, top: at.y, ...size }} data-cell-picker-host data-selection-property={property.id}>
      <button ref={buttonRef} type="button" tabIndex={-1} aria-hidden="true" className={styles.selectionPointButton} />
      <SelectionEditor host={host} buttonRef={buttonRef} environment={environment} page={page} property={property}
        onValue={onValue} onClosed={onClose} own={own} />
    </span>
  );
}

// The property's own cell, unseen under the button, opens its picker there;
// what is picked goes to every selected row.
function PickedEditor({
  environment,
  page,
  property,
  onValue,
  own = false,
}: {
  environment: PropertyCellEnvironment;
  page: Page;
  property: GraphProperty;
  onValue: SelectionValue;
  own?: boolean;
}) {
  const cellRef = useRef<HTMLSpanElement>(null);
  // Files are written as a cell writes them: references, not what the picker shows.
  const written = (value: unknown) => property.type === "files" && value != null ? graphFilePropertyValue(value) : value;
  const shared = useMemo<PropertyCellEnvironment>(() => own ? environment : ({
    ...environment,
    operations: {
      ...environment.operations,
      setValue: (_rowId, _property, value) => onValue(property, written(value)),
      setTextValue: (_rowId, _property, value, _context, spans) => onValue(property, value, spans, true),
      setRelation: (_rowId, _property, relatedRowIds) => onValue(property, relatedRowIds),
    },
    // written follows property.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }), [environment, onValue, own, property]);
  const prop = useMemo<DbProperty>(() => ({
    id: property.id, databaseId: property.dataSourceId, name: property.name,
    type: property.type, config: property.config, position: 0,
  }) as DbProperty, [property]);
  useLayoutEffect(() => {
    cellRef.current?.querySelector<HTMLElement>('[role="group"], [aria-haspopup]')?.click();
  }, []);
  return (
    <span ref={cellRef} className={styles.selectionPropertyCell}>
      <PropertyCell {...shared} row={page} prop={prop} />
    </span>
  );
}

function TypedEditor({
  buttonRef,
  page,
  property,
  onClose,
  onValue,
  plain = false,
}: {
  buttonRef: RefObject<HTMLButtonElement | null>;
  page: Page;
  property: GraphProperty;
  onClose(): void;
  onValue: SelectionValue;
  /** A card's slot's: the field bare in its box, as wide as the slot (Notion's, seen 2026-11-06). */
  plain?: boolean;
}) {
  const { t } = useTranslation("propertyCell");
  const stored = page.properties?.[property.id];
  const initial = property.type === "number" ? numberInputText(stored, property) : stored == null ? "" : String(stored);
  const [draft, setDraft] = useState(initial);
  const finished = useRef(false);
  // As in a cell, what was typed is kept when the field closes.
  function finish() {
    if (finished.current) return;
    finished.current = true;
    if (draft !== initial) {
      if (property.type !== "number") onValue(property, draft);
      else {
        const number = parseNumberInput(draft, property);
        if (number !== undefined) onValue(property, number);
      }
    }
    onClose();
  }
  return (
    <CellMenuPortal triggerRef={buttonRef} cover restoreFocus={!plain} onClose={finish} ariaLabel={t("edit", { name: property.name })}
      className={plain ? styles.slotTypedMenu : styles.selectionTypedMenu} width={240}>
      <input className={plain ? styles.slotTypedField : styles.selectionTypedField} value={draft} aria-label={property.name}
        inputMode={property.type === "number" ? "decimal" : undefined}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={(event) => {
          if (!fieldEnter(event)) return;
          event.preventDefault();
          finish();
        }} />
    </CellMenuPortal>
  );
}
