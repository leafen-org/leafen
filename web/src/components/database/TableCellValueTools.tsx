import { useContext } from "react";
import type { DbProperty, Page } from "@/lib/types";
import { CellCommentsContext } from "./cellCommentsContext";
import { CellTools, type CellToolsShown } from "./cellToolsShown";
import { computedCellTools } from "./computedCellTools";
import { copiedDateForProperty } from "./dateUtils";
import { COMMENTLESS_PROPERTY_TYPES, COPYABLE_PROPERTY_TYPES, PropertyValueTools } from "./PropertyValueTools";
import type { PropertyValueReadContext } from "./PropValue";
import { cellValue } from "./queryProjection";
import { propertySearchText } from "./query";
import { nativeBoardQueryBounds } from "./boardViewModel";

// Values the page works out rather than keeps: read as the query does, else as the cell's text.
const COMPUTED_VALUE_TYPES = new Set(["formula", "rollup", "created_time", "last_edited_time"]);

function cellValueEmpty(value: unknown) {
  return value == null || value === "" || (Array.isArray(value) && value.length === 0);
}

/** What a cell's value reads with as its tools draw, by its table as last drawn. */
export type TableCellValueReading = {
  valueContext?: PropertyValueReadContext;
  /** The words its cell shows: the quote its "댓글" opens on, and whether a worked-out value shows any. */
  text?: () => string;
};

/**
 * A table cell's value tools under the pointer or holding the focus
 * (PropertyValueTools): "댓글" and "클립보드에 복사", a rollup's "롤업
 * 구성" first, on a value that shows something and its reader may see —
 * the same for a table's cells and a timeline's table's, as Notion's
 * (seen 2026-11-24 at 1685, dark: in both, a number's tray 4px in from its
 * cell's start and 6px down, a date's at its end, a choice's "댓글" alone,
 * none on a checkbox or the name).
 */
export function TableCellValueTools({ shown, cell, page, property, hidden, read, configure }: {
  shown: CellToolsShown;
  /** Its cell's key among its table's (toolsCellKey). */
  cell: string;
  page: Page;
  property: DbProperty;
  /** Its value is hidden from its reader. */
  hidden?: boolean;
  read: () => TableCellValueReading;
  /** A rollup's small form, which its "롤업 구성" opens from the cell. */
  configure?: (cell: HTMLElement) => void;
}) {
  const cellComments = useContext(CellCommentsContext);
  return (
    <CellTools shown={shown} cell={cell}>{() => {
      if (hidden) return null;
      const { valueContext, text } = read();
      const value = cellValue(page, property);
      if (!configure && cellValueEmpty(value) && !(COMPUTED_VALUE_TYPES.has(property.type) && text?.().trim())) return null;
      const computed = property.type === "formula" || property.type === "rollup" ? computedCellTools(page, property, valueContext) : null;
      const stored = page.properties?.[property.id];
      const cellOf = (button: HTMLElement) => button.closest<HTMLElement>("[data-table-cell]");
      return (
        <PropertyValueTools side={property.type === "number" ? "start" : computed?.side}
          onConfigure={configure ? (button) => {
            const at = cellOf(button);
            if (at) configure(at);
          } : undefined}
          onComment={cellComments?.canComment && !COMMENTLESS_PROPERTY_TYPES.has(property.type) ? (button) => {
            const at = cellOf(button);
            if (!at) return;
            cellComments.property({ rowId: page.id, property: { id: property.id, name: property.name, type: property.type, config: property.config },
              quote: text?.() ?? "", rect: at.getBoundingClientRect() });
          } : undefined}
          copyValue={computed ? computed.copy : COPYABLE_PROPERTY_TYPES.has(property.type) ? (button) => property.type === "url" ? String(stored)
            : property.type === "date" ? copiedDateForProperty(stored, property)
            : cellOf(button)?.querySelector<HTMLElement>("[data-cell-editor]")?.innerText.replace(/\s+/gu, " ") ?? "" : undefined} />
      );
    }}</CellTools>
  );
}

/**
 * The words a value shows, as a database's own search reads them
 * (propertySearchText): a cell's "댓글" quotes them, as a table's cell does
 * its table's (TableView's cellText).
 */
export function shownValueText(page: Page, property: DbProperty, context: PropertyValueReadContext): string {
  const propsByDb = context.propsByDatabaseId as Record<string, DbProperty[]>;
  return propertySearchText(page, property, { props: propsByDb[property.databaseId] ?? [], pagesById: context.pagesById as Record<string, Page>,
    propsByDb, currentUserId: context.userId, bounds: nativeBoardQueryBounds });
}
