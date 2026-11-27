import { memo, useMemo, useRef, useState, type CSSProperties, type PointerEvent, type Ref } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import type { DragReorder } from "@/lib/useDragReorder";
import { POINTER_REORDER_END } from "@/lib/dragReorderPointer";
import { InfoIcon } from "../icons";
import { usePropertyDescriptionTip } from "./PropertyDescriptionTip";
import { PropertyAccessLock } from "./PropertyAccessMarks";
import { ButtonPropertyHeadMark } from "./ButtonPropertyHeadMark";
import { PropertyIcon } from "./PropertyIcon";
import { useDatabaseStructureLocked } from "./databaseStructureLock";
import { stickyPad, tableColumnMinWidth } from "./tableInteractionModel";
import { cellLine, cellOnLine } from "./tableCellLine";
import styles from "./databaseSheet";
import { inlineSign } from "@/lib/readingDirection";

type TablePropertyHeaderProps = {
  property: GraphProperty;
  width: number;
  readOnly: boolean;
  /** The table's columns, which a header drags along. */
  reorder: DragReorder;
  onOpenMenu(property: GraphProperty, returnTo: HTMLElement): void;
  onResize(propertyId: string, width: number): void;
  /** Where a frozen column sticks, from the left of the scrolling table. */
  stickyLeft?: CSSProperties["left"];
  frozenEdge?: boolean;
};

/**
 * A column's head, drawn again only when what it shows changes: the table is
 * drawn again for each value typed into it, and its heads with their menus
 * and tips took a tenth of each character's work (2026-11-12, 40 columns).
 */
export function TablePropertyHeader(props: TablePropertyHeaderProps) {
  // A locked database's properties and columns stay as they are.
  const structureLocked = useDatabaseStructureLocked();
  const readOnly = props.readOnly || structureLocked;
  const latest = useRef(props);
  latest.current = props;
  const handlers = useMemo(() => ({
    onOpenMenu: (property: GraphProperty, returnTo: HTMLElement) => latest.current.onOpenMenu(property, returnTo),
    onResize: (propertyId: string, width: number) => latest.current.onResize(propertyId, width),
  }), []);
  const drag = props.reorder.row(props.property.id, () => true, !readOnly);
  return <KeptPropertyHeader property={props.property} width={props.width} readOnly={readOnly} stickyLeft={props.stickyLeft}
    frozenEdge={props.frozenEdge} dragRef={drag.ref} dragging={drag["data-dragging"]} dropSide={drag["data-drop-side"]}
    dragSource={drag["data-native-drag-source"]} {...handlers} />;
}

type KeptPropertyHeaderProps = Omit<TablePropertyHeaderProps, "reorder"> & {
  dragRef: Ref<HTMLDivElement>;
  dragging?: string;
  dropSide?: string;
  dragSource?: string;
};

const KeptPropertyHeader = memo(function KeptPropertyHeader({
  property,
  width,
  readOnly,
  onOpenMenu,
  onResize,
  stickyLeft,
  frozenEdge,
  dragRef,
  dragging,
  dropSide,
  dragSource,
}: KeptPropertyHeaderProps) {
  const { t } = useTranslation("tableView");
  const resizeWidth = useRef(width);
  const [previewWidth, setPreviewWidth] = useState<number | null>(null);
  const displayedWidth = previewWidth ?? width;
  const { description, handlers: tipHandlers, tip } = usePropertyDescriptionTip(property, "above");

  function beginResize(event: PointerEvent<HTMLDivElement>) {
    if (readOnly) return;
    event.preventDefault();
    const startX = event.clientX;
    const startWidth = width;
    // Its end border, dragged toward the reading direction's end, widens it.
    const sign = inlineSign(event.currentTarget);
    resizeWidth.current = width;
    const move = (next: globalThis.PointerEvent) => {
      resizeWidth.current = Math.max(tableColumnMinWidth(property.type), startWidth + (next.clientX - startX) * sign);
      setPreviewWidth(resizeWidth.current);
    };
    const finish = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", finish);
      setPreviewWidth(null);
      if (resizeWidth.current !== width) onResize(property.id, resizeWidth.current);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", finish, { once: true });
  }

  // A chosen cell stays chosen through a column's move, in its place in the
  // column order, its Escape then choosing its row, as Notion's (seen
  // 2026-11-08; not measured again 2026-11-15: the worker Chrome's drag did
  // not move Notion's column, and the Notion desktop app needed an approval
  // no one gave); the head's button took the focus as the drag began. A press
  // that drags nothing opens the head's menu as before.
  function keepChosenCellThroughMove(event: PointerEvent<HTMLDivElement>) {
    const head = event.currentTarget, active = document.activeElement;
    const grid = head.closest<HTMLElement>('[role="grid"]');
    if (!grid || !(active instanceof HTMLElement) || !active.matches("[data-table-cell]") || !grid.contains(active)) return;
    const line = cellLine(active), column = active.dataset.colIndex;
    const done = new AbortController();
    head.addEventListener("click", () => done.abort(), { capture: true, signal: done.signal });
    const moved = () => {
      done.abort();
      window.requestAnimationFrame(() => window.requestAnimationFrame(() => cellOnLine(grid, line, column)?.focus({ preventScroll: true })));
    };
    // The browser's drag (a finger's) ends with "dragend", the pointer's with its own (dragReorderPointer).
    head.addEventListener("dragend", moved, { signal: done.signal });
    head.addEventListener(POINTER_REORDER_END, moved, { signal: done.signal });
  }

  return (
    <div
      ref={dragRef}
      data-dragging={dragging}
      data-native-drag-source={dragSource}
      onPointerDownCapture={readOnly ? undefined : keepChosenCellThroughMove}
      data-drag-over={dropSide ? "true" : undefined}
      data-drop-side={dropSide}
      className={styles.headCell}
      role="columnheader"
      style={{
        width: displayedWidth, minWidth: displayedWidth,
        ...(stickyLeft === undefined ? {} : { ...stickyPad, position: "sticky", insetInlineStart: stickyLeft, zIndex: 3, background: "var(--table-surface, var(--bg-app))" }),
      }}
      data-frozen={stickyLeft === undefined ? undefined : "true"}
      data-frozen-edge={frozenEdge ? "true" : undefined}
      data-property-id={property.id}
      {...tipHandlers}
    >
      <button
        type="button"
        className={styles.headCellBtn}
        disabled={readOnly}
        aria-label={t("propertyOptionsFor", { name: property.name })}
        aria-description={description || undefined}
        onClick={(event) => onOpenMenu(property, event.currentTarget)}
      >
        {/* Its type's icon stays under the pointer, the head only lit, as Notion's (seen 2026-11-07),
            drawn as large as a property list's (DatabaseTableChrome.module.css). */}
        <span className={styles.headGlyph} aria-hidden="true">
          <span className={styles.headGlyphIcon}><PropertyIcon property={property} size="regular" /></span>
        </span>
        <span className={styles.headName}>{property.name}</span>
        <ButtonPropertyHeadMark property={property} />
        {property.access?.restricted ? <PropertyAccessLock /> : null}
        {description && <span className={styles.headInfo} aria-hidden="true" data-property-described><InfoIcon size="tiny" /></span>}
      </button>
      {!readOnly && (
        <div
          className={styles.colResize}
          role="separator"
          aria-label={t("resizeColumnValue", { name: property.name, width: Math.round(displayedWidth) })}
          aria-orientation="vertical"
          tabIndex={0}
          onPointerDown={beginResize}
          onKeyDown={(event) => {
            if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
            event.preventDefault();
            onResize(property.id, width + (event.key === "ArrowRight" ? 12 : -12));
          }}
        />
      )}
      {tip}
    </div>
  );
});
