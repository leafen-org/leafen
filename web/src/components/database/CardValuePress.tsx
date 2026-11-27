"use client";

import { useEffect, useMemo, useRef, useState, type MouseEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { DbProperty, Page } from "@/lib/types";
import { copyText } from "@/lib/copyText";
import { useOptionalProductToast } from "@/lib/productToast";
import { placeAtPoint, popoverBackdropStyle, popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { graphPropertyOf, typed } from "./CardPropertySlots";
import { FormulaEditorDialog } from "./FormulaEditorDialog";
import { PersonValueLookArea } from "./PersonBadge";
import { DatabaseButtonCell } from "./PropertyComputedCells";
import { PropertyTypeConfig } from "./PropertyTypeConfig";
import { SelectedRowsPropertyEditor, selectionEditableProperties } from "./TableSelectionProperties";
import { useDatabaseStructureLocked } from "./databaseStructureLock";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import styles from "./databaseSheet";
import menuStyles from "./TablePropertyMenu.module.css";
import { fieldEscape } from "@/lib/fieldKeyBoundary";

/*
 * A card's or a list row's value, pressed, does there what its cell does, as
 * Notion's board, gallery and list (seen 2026-11-12): a value one picks or
 * types opens its picker or field over it, a checkbox checks or clears at
 * once, a formula opens its editor, a rollup its small form ("관계형", "속성",
 * "계산") and an ID is copied ("속성 복사 완료"); a button shows as its
 * button, which runs it. The rest — a time or person Leafen fills in — opens
 * the row as the card does, as does the room beside a card's value; a link in
 * a value opens the link. Where the values can't change, a press opens the
 * row.
 */

type Box = { left: number; top: number; width: number; height: number };
type Press = "editor" | "checkbox" | "formula" | "rollup" | "copy";

// A card's value is pressed 5px around it, its line's other room opening
// the card; a checkbox's line and a file's are pressed whole, 1px in from
// its ends (Notion's, seen 2026-11-12: a value's button 5px around it 6px
// into a 260 card, a checkbox's 248 wide, a file's the gallery card's width).
const CARD_VALUE_RING = 5;
const WHOLE_LINE = new Set(["checkbox", "files"]);

function pressOf(property: DbProperty, structureEditable: boolean): Press | null {
  if (property.type === "checkbox") return "checkbox";
  if (property.type === "unique_id") return "copy";
  if (property.type === "formula" || property.type === "rollup") return structureEditable ? property.type : null;
  if (property.type === "title") return null;
  return selectionEditableProperties([graphPropertyOf(property)]).length ? "editor" : null;
}

function rectBox(rect: DOMRect | Box): Box {
  return { left: rect.left, top: rect.top, width: rect.width, height: rect.height };
}

// What a value shows takes the room its words, pictures and chips take, not
// the boxes around them, which may run the line's width.
const DRAWN = new Set(["svg", "img", "input", "canvas", "video"]);

function drawnExtent(element: HTMLElement): { left: number; right: number } | null {
  let left = Infinity, right = -Infinity;
  const take = (rect: DOMRect) => {
    if (!rect.width) return;
    left = Math.min(left, rect.left);
    right = Math.max(right, rect.right);
  };
  const walk = (node: Node) => {
    for (const child of node.childNodes) {
      if (child.nodeType === Node.TEXT_NODE) {
        if (!child.textContent?.trim()) continue;
        const range = document.createRange();
        range.selectNodeContents(child);
        take(range.getBoundingClientRect());
      } else if (child instanceof Element) {
        const style = getComputedStyle(child);
        if (style.display === "none") continue;
        if (DRAWN.has(child.localName) || style.backgroundColor !== "rgba(0, 0, 0, 0)" && style.backgroundColor !== "transparent") {
          take(child.getBoundingClientRect());
        } else walk(child);
      }
    }
  };
  walk(element);
  return left < right ? { left, right } : null;
}

/** Where the press landed, or null where it opens the row. */
function pressedBox(element: HTMLElement, line: boolean, whole: boolean, event: MouseEvent<HTMLElement>): Box | null {
  const own = element.getBoundingClientRect();
  if (!line) return rectBox(own);
  if (whole) return { left: own.left + 1, top: own.top, width: own.width - 2, height: own.height };
  const drawn = drawnExtent(element);
  if (!drawn) return null;
  const box = { left: drawn.left - CARD_VALUE_RING, top: own.top, width: drawn.right - drawn.left + CARD_VALUE_RING * 2, height: own.height };
  return event.clientX >= box.left && event.clientX <= box.left + box.width ? box : null;
}

export function CardValuePress({ property, page, environment, line = false, className, children, ...marks }: {
  property: DbProperty;
  page: Page;
  /** Edits it in place; without it, a press opens the row as the card's other room does. */
  environment?: PropertyCellEnvironment;
  /** A card's line, pressed around its value; a list's value is pressed whole. */
  line?: boolean;
  className: string;
  children: ReactNode;
  "data-card-full-line"?: string;
}) {
  const { t } = useTranslation("propertyCell");
  const toast = useOptionalProductToast();
  const structureLocked = useDatabaseStructureLocked();
  const [open, setOpen] = useState<{ press: "editor" | "formula" | "rollup"; box: Box } | null>(null);
  const press = environment ? pressOf(property, !structureLocked && !environment.operations.addOptionsOnly) : null;
  // A button shows as its ringed button on a card or a row, as Notion's (seen 2026-11-12: 24 high, 10 in, its line 32).
  const button = property.type === "button" && environment;

  function onClick(event: MouseEvent<HTMLElement>) {
    const element = event.currentTarget;
    // A button's press is its own.
    if (button && (event.target as Element).closest("button")) {
      event.stopPropagation();
      return;
    }
    // Its picker, drawn over the page, sends its presses here through React.
    if (!press || !environment || !element.contains(event.target as Node)) return;
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    const box = pressedBox(element, line, WHOLE_LINE.has(property.type), event);
    if (!box) return;
    event.stopPropagation();
    if (press === "checkbox") {
      void environment.operations.setValue(page.id, property, !page.properties?.[property.id], environment.context);
    } else if (press === "copy") {
      void copyText(element.innerText.trim()).then((copied) => { if (copied) toast?.notify(t("valueCopied")); });
    } else setOpen({ press, box });
  }

  const close = () => setOpen(null);
  return (
    <span {...marks} className={className} data-card-value-press={press ? property.id : undefined}
      data-card-button={button ? "" : undefined}
      role={press && !line ? "button" : undefined} tabIndex={press && !line ? -1 : undefined} onClick={onClick}>
      {button ? <DatabaseButtonCell row={page} prop={property} {...environment} />
        // People show as a card's or, in a list's row, as faces alone.
        : <PersonValueLookArea look={line ? "card" : "faces"}>{children}</PersonValueLookArea>}
      {open && environment && typeof document !== "undefined" ? createPortal(
        open.press === "editor" ? (
          // Its picker or field over the value, 1px left of it and 1px over it (Notion's, seen 2026-11-12:
          // a select's at 963,252.9 over a value at 964,253.9; a list's text field at 508,170.4 over 509,171.4).
          <SelectedRowsPropertyEditor own at={{ x: open.box.left, y: open.box.top - 1 }}
            size={{ width: open.box.width, height: open.box.height }} environment={environment} page={page}
            property={graphPropertyOf(property)} onValue={typed(page, environment)} onClose={close} />
        ) : open.press === "formula" ? (
          <CardFormulaEditor property={property} page={page} environment={environment} box={open.box} onClose={close} />
        ) : <CardRollupForm property={property} environment={environment} box={open.box} onClose={close} />,
        document.body) : null}
    </span>
  );
}

// The formula's editor hangs from the value's foot, as from its cell.
function CardFormulaEditor({ property, page, environment, box, onClose }: {
  property: DbProperty; page: Page; environment: PropertyCellEnvironment; box: Box; onClose(): void;
}) {
  const { snapshot, operations, context } = environment;
  const anchor = useMemo<PopoverRect>(() => ({ top: box.top + box.height - 1, bottom: box.top + box.height - 1,
    left: box.left - 1, right: box.left - 1 }), [box]);
  return (
    <FormulaEditorDialog prop={property} props={snapshot.propertiesByDataSourceId[property.databaseId] ?? []}
      pagesById={snapshot.pagesById[page.id] ? snapshot.pagesById : { ...snapshot.pagesById, [page.id]: page }}
      propsByDb={snapshot.propertiesByDataSourceId} bounds={snapshot.formulaBounds} rowId={page.id} anchor={anchor}
      onSave={(formula) => void operations.updateProperty(property.id, { config: { ...property.config, formula } }, context)}
      onClose={onClose} />
  );
}

// A rollup's small form, its corner on the value's and 2px over it (Notion's,
// seen 2026-11-12: at 1032,216.9 for a value at 1032,218.9, 180 wide).
function CardRollupForm({ property, environment, box, onClose }: {
  property: DbProperty; environment: PropertyCellEnvironment; box: Box; onClose(): void;
}) {
  const { t } = useTranslation("tableView");
  const menuRef = useRef<HTMLDivElement>(null);
  const { snapshot, operations, context } = environment;
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeAtPoint({ x: box.left, y: box.top - 2 }, size, viewport),
    [box.left, box.top], undefined, { kind: "picker", restoreFocus: false, onEscape: onClose });
  const databases = useMemo(() => Object.values(snapshot.pagesById).filter((candidate) => candidate.kind === "database"), [snapshot.pagesById]);
  // It takes the keys at once, ringing none of its choices, as from a cell; Escape closes it.
  const placed = Boolean(place);
  useEffect(() => {
    if (!placed) return;
    const frame = window.requestAnimationFrame(() => menuRef.current?.focus({ preventScroll: true }));
    return () => window.cancelAnimationFrame(frame);
  }, [placed]);
  return (
    <>
      <button type="button" className={styles.propertyMenuBackdrop} style={popoverBackdropStyle(place)} aria-label={t("closePropertyMenu")} onClick={onClose} />
      <div ref={menuRef} className={menuStyles.menu} role="dialog" tabIndex={-1} aria-label={t("editProperty")} data-card-rollup-form={property.id}
        style={popoverPlacementStyle(place)} onKeyDown={(event) => {
          if (!fieldEscape(event)) return;
          event.preventDefault();
          event.stopPropagation();
          onClose();
        }}>
        <PropertyTypeConfig cellForm prop={property} onClose={onClose}
          snapshot={{ databases, formulaBounds: snapshot.formulaBounds, propertiesByDatabase: snapshot.propertiesByDataSourceId,
            snapshotIdentity: context.sessionIdentity, pagesById: snapshot.pagesById }}
          context={{ contextIdentity: context.sessionIdentity, ops: {
            loadDatabase: () => {},
            setRelationDatabase: () => {},
            setRelationTwoWay: () => {},
            updateProperty: (_operation, propertyId, patch) => void operations.updateProperty(propertyId, patch, context),
          } }} />
      </div>
    </>
  );
}
