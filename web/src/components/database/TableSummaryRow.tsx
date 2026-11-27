import { useDeferredValue, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { DbProperty, Page } from "@/lib/types";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PopoverLayer } from "@/lib/popoverLayer";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { useDatabaseStructureLocked } from "./databaseStructureLock";
import { galleryPresentationProperty } from "./galleryViewModel";
import { TableCalculationMenu } from "./TableCalculationMenu";
import { tableCalculation, tableCalculationResult, type TableCalculation } from "./tableCalculationModel";
import { tableRowPage } from "./TableGrid";
import styles from "./databaseSheet";
import { tableTrackColumns, withNewColumnCell } from "./tableNewColumn";

type TableSummaryRowProps = {
  /** The columns shown, in their order. */
  properties: readonly GraphProperty[];
  /** Every property of the source, which formulas and rollups read. */
  allProperties: readonly GraphProperty[];
  rows: readonly GraphRow[];
  widths: readonly number[];
  /** Where an insert's empty column stands among the columns (tableNewColumn.ts). */
  newColumnAt?: number;
  calculations: Readonly<Record<string, unknown>>;
  limitLargeCounts: boolean;
  pagesById?: Record<string, Page>;
  propertiesByDatabaseId?: Record<string, DbProperty[]>;
  workspaceId: string;
  readOnly: boolean;
  onChange(propertyId: string, calculation: TableCalculation | undefined): void;
  onLimitLargeCounts(limit: boolean): void;
  /** Its own grid without the rows' gutter, and drawn with no calculation too: a timeline's table (TimelineRows). */
  columns?: string;
};

const summaryBounds = { formula: nativeBoardQueryBounds.formula, projection: nativeBoardQueryBounds.formula.rollup };

/**
 * The table's foot, as Notion's (seen 2026-09-30, 2026-10-06): under each
 * column its calculation's short name and value ("합계 8"), or, under the cell
 * the pointer is on, "계산" to choose one from the calculate menu. A table with
 * no calculation has no foot, and someone who can't change them sees only the
 * values.
 */
export function TableSummaryRow({
  properties, allProperties, rows, widths, newColumnAt, calculations, limitLargeCounts, pagesById, propertiesByDatabaseId, workspaceId, readOnly,
  onChange, onLimitLargeCounts, columns,
}: TableSummaryRowProps) {
  const [openId, setOpenId] = useState<string | null>(null);
  const { t } = useTranslation("tableView");
  // The menu opens over its calculation, outside the table's scrolling.
  const menuRef = useRef<HTMLDivElement>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  // Right under its button, from its start, and over it
  // where the window has no room under it, as Notion's (seen 2026-11-14 in
  // dark at 1440: a 44×21 "개수 3", its 200×123 menu at its start, under
  // its foot with room there, over its top 646 near the window's end).
  const place = usePopoverPlacement(menuRef, openId ? (size, viewport) => {
    const anchor = popoverRectOf(opener.current) ?? { top: 0, bottom: 0, left: 0, right: 0 };
    return placeAround(anchor, size, viewport, { align: "start", gap: 0 });
  } : null, [openId], undefined, { kind: "inner" });
  // As Notion's, the menu closes on Escape or a press outside it, giving the
  // keys back to its calculation; its own button toggles it.
  useOverlayLayer(openId !== null, {
    kind: "menu", ref: menuRef, inside: () => [opener.current], onEscape: () => setOpenId(null), onOutsidePress: () => setOpenId(null),
    returnFocusRef: opener,
  });
  // A locked database's calculations, part of its view, stay as they are.
  const structureLocked = useDatabaseStructureLocked();
  // The calculations are worked out behind a press (useDeferredValue: in
  // pieces, dropped for a newer press) and again only when what they count
  // changed: a value, a filter or a calculation picked draws first, the foot
  // following, and a press elsewhere in the table counts nothing again.
  const counting = useDeferredValue(useMemo(() => ({ rows, calculations, allProperties, pagesById, propertiesByDatabaseId, limitLargeCounts }),
    [allProperties, calculations, limitLargeCounts, pagesById, propertiesByDatabaseId, rows]));
  const results = useMemo(() => {
    const counted = counting.allProperties;
    const pages = counting.rows.map((row) => tableRowPage(row, counted, workspaceId));
    const environment = { properties: counted.map(galleryPresentationProperty), propertiesByDatabaseId: counting.propertiesByDatabaseId,
      pagesById: counting.pagesById ?? {}, bounds: summaryBounds, limitLargeCounts: counting.limitLargeCounts };
    const byId = new Map<string, { calculation: TableCalculation; result: ReturnType<typeof tableCalculationResult> }>();
    for (const property of counted) {
      const calculation = tableCalculation(counting.calculations[property.id]);
      if (calculation) byId.set(property.id, { calculation, result: tableCalculationResult(calculation, property, pages, environment) });
    }
    return byId;
  }, [counting, workspaceId]);
  // A timeline's table keeps its foot with none, "계산" under the pointer (Notion's, seen 2026-11-15).
  if (!columns && !properties.some((property) => tableCalculation(calculations[property.id]))) return null;
  const fixed = readOnly || structureLocked;
  return (
    <div
      className={styles.tableSummary}
      data-table-summary-row
      role="row"
      style={{ gridTemplateColumns: columns ?? `${readOnly ? "" : "var(--table-gutter-width) "}${tableTrackColumns(widths, properties.length, newColumnAt)}` }}
    >
      {!readOnly && !columns && <div role="gridcell" aria-hidden="true" />}
      {withNewColumnCell(properties.map((property) => {
        const calculation = tableCalculation(calculations[property.id]);
        // As last worked out: a calculation just picked shows once counted.
        const counted = calculation ? results.get(property.id) : undefined;
        const result = counted?.result ?? null;
        return (
          <div className={styles.summaryCell} role="gridcell" key={property.id}>
            <button
              type="button"
              className={styles.summaryButton}
              data-empty={result ? undefined : "true"}
              data-table-calculation={result ? counted?.calculation : undefined}
              disabled={fixed}
              aria-haspopup="menu"
              aria-expanded={openId === property.id}
              onClick={(event) => {
                opener.current = event.currentTarget;
                setOpenId((current) => current === property.id ? null : property.id);
              }}
            >
              {result ? (
                <>
                  <span>{t(`calculationLabels.${result.label}`)}</span>
                  <strong>{result.value}</strong>
                </>
              ) : fixed ? null : t("calculate")}
            </button>
            {openId === property.id && (
              <PopoverLayer><div ref={menuRef} className={styles.summaryMenu} style={popoverPlacementStyle(place)}>
                <TableCalculationMenu label={t("propertyCalculation", { name: property.name })} type={property.type}
                  calculation={calculation} limitLargeCounts={limitLargeCounts} onLimitLargeCounts={onLimitLargeCounts}
                  onPick={(next) => { onChange(property.id, next); setOpenId(null); }} />
              </div></PopoverLayer>
            )}
          </div>
        );
      }), newColumnAt, () => <div key="new-column" aria-hidden="true" data-table-new-column />)}
    </div>
  );
}
