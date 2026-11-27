import { memo, useCallback, useRef, useState } from "react";
import { useCellText } from "./cellText";
import { useDatabaseStructureLocked } from "./databaseStructureLock";
import { formatTimestampForProperty } from "./dateUtils";
import { runtimeOwnedPropertySummary } from "./structuredPropertyValues";
import { uniqueIdAwaitedAttributes, uniqueIdText } from "./uniqueIdText";
import { DatabaseButtonCell, FormulaCell, PlaceCell, RollupCell, VerificationCell } from "./PropertyComputedCells";
import { PropertyDateCell } from "./PropertyDateCell";
import { PropertyFilesCell } from "./PropertyFilesCell";
import { PropertyNumberCell } from "./PropertyNumberCell";
import { PeopleReadonly, PropertyPeopleCell } from "./PropertyPeopleCell";
import { PropertyRelationCell } from "./PropertyRelationCell";
import { PropertySelectCell } from "./PropertySelectCell";
import { pageShowsStatusCheckbox } from "./rowPageStatusDisplay";
import { TableStatusCheckbox } from "./TableStatusDisplay";
import { PropertyTextCell } from "./PropertyTextCells";
import { AppPropertyCell } from "@/components/apps/AppPropertyCell";
import type { PropertyCellEnvironment, PropertyCellPresentation, TitleIconMode } from "./propertyCellTypes";
import type { DbProperty, Page, TextSpan } from "@/lib/types";
import styles from "./databaseSheet";
import { valueAsRead } from "./valueAsRead";
import { cellReadsSame, readsOtherRows, sameOtherRowsRead } from "./otherRowsRead";
import { CellReadOnlyContext, readOnlyStop } from "./cellReadOnly";

export { CellMenuPortal } from "./PropertyCellMenu";
export { PropertyFileListItem } from "./PropertyFilesCellView";
export { PropertyRelationCellView } from "./PropertyRelationCellView";
export { PropertySelectOptionEditor } from "./PropertySelectCellView";

function getValue(row: Page, prop: DbProperty): unknown {
  if (prop.type === "title") return row.title;
  if (prop.type === "created_time") return row.createdAt;
  if (prop.type === "last_edited_time") return row.updatedAt;
  if (prop.type === "created_by") return row.createdBy;
  if (prop.type === "last_edited_by") return row.lastEditedBy;
  return valueAsRead(row, prop);
}

export interface PropertyCellProps extends PropertyCellEnvironment {
  row: Page;
  prop: DbProperty;
  autoFocus?: boolean;
  onOpenPage?: (pageId: string) => void;
  presentation?: PropertyCellPresentation;
  titleIcon?: TitleIconMode;
  /** Shows its value without editing it, drawn as when it may (cellReadOnly.ts). */
  readOnly?: boolean;
}

// A property an app draws shows the app's cell while the app may be used,
// and Leafen's own otherwise.
function PropertyCellImpl(props: PropertyCellProps) {
  const { row, prop, operations, context } = props;
  const readOnly = props.readOnly === true;
  if (!prop.config?.appEditor) return <CellReadOnlyContext.Provider value={readOnly}><LeafenPropertyCell {...props} /></CellReadOnlyContext.Provider>;
  const text = !["number", "checkbox", "select", "status", "multi_select"].includes(prop.type);
  return (
    <CellReadOnlyContext.Provider value={readOnly}>
      <AppPropertyCell row={row} prop={prop} editable={!readOnly} fallback={<LeafenPropertyCell {...props} />}
        onSet={(value) => void (text ? operations.setTextValue(row.id, prop, String(value), context) : operations.setValue(row.id, prop, value, context))} />
    </CellReadOnlyContext.Provider>
  );
}

function LeafenPropertyCell({
  row,
  prop,
  autoFocus,
  onOpenPage,
  presentation = "default",
  titleIcon,
  snapshot,
  operations,
  context,
  readOnly = false,
}: PropertyCellProps) {
  const t = useCellText();
  const structureLocked = useDatabaseStructureLocked();
  const environment = { snapshot, operations, context };
  const setValue = (value: unknown) => operations.setValue(row.id, prop, value, context);
  const setTextValue = (value: string, spans?: TextSpan[]) => operations.setTextValue(row.id, prop, value, context, spans);
  const value = getValue(row, prop);

  switch (prop.type) {
    case "button":
      return <DatabaseButtonCell row={row} prop={prop} {...environment} />;
    case "checkbox":
      return (
        <span className={styles.cellCheck}>
          <input
            type="checkbox"
            aria-label={t("propertyCell:edit", { name: prop.name })}
            checked={!!value}
            {...readOnlyStop(readOnly)}
            onChange={(event) => { if (!readOnly) void setValue(event.target.checked); }}
            // In a table the press also chooses its cell, focus going to the
            // cell so the arrow keys go on from it (Notion's, seen 2026-11-07;
            // measured once 2026-11-15 in Chrome 1440 with no script before
            // the press: the box ticked, its cell chosen, ↓ choosing the one below).
            onClick={(event) => event.currentTarget.closest<HTMLElement>("[data-table-cell]")?.focus({ preventScroll: true })}
          />
        </span>
      );
    case "number":
      return (
        <PropertyNumberCell
          prop={prop}
          value={value}
          autoFocus={autoFocus}
          onCommit={(next) => void setValue(next)}
        />
      );
    case "status":
      // Its pages may show it as a checkbox (rowPageStatusDisplay.ts), finished when ticked, as a table's column may.
      if (presentation === "rowDetail" && pageShowsStatusCheckbox(prop)) {
        return <TableStatusCheckbox property={prop} value={value} readOnly={readOnly} label={t("propertyCell:edit", { name: prop.name })}
          onChange={(next) => void setValue(next)} />;
      }
      return <PropertySelectCell row={row} prop={prop} multi={false} {...environment} />;
    case "select":
      return <PropertySelectCell row={row} prop={prop} multi={false} {...environment} />;
    case "multi_select":
      return <PropertySelectCell row={row} prop={prop} multi {...environment} />;
    case "files":
      return <PropertyFilesCell row={row} prop={prop} {...environment} />;
    case "relation":
      return <PropertyRelationCell row={row} prop={prop} onOpenPage={onOpenPage} presentation={presentation} {...environment} />;
    case "rollup":
      return <RollupCell row={row} prop={prop} onOpenPage={onOpenPage} {...environment} />;
    case "formula":
      return <FormulaCell row={row} prop={prop} {...environment} />;
    case "date":
      // The date and time formats belong to the property, as in Notion: not
      // where the database's properties may not change.
      return <PropertyDateCell prop={prop} value={value} rowId={row.id} autoFocus={autoFocus} templateValues={snapshot.templateValues}
        onCommit={(next) => void setValue(next)}
        onPropertyConfig={structureLocked || operations.addOptionsOnly ? undefined
          : (patch) => void operations.updateProperty(prop.id, { config: { ...prop.config, ...patch } }, context)} />;
    case "place":
      return <PlaceCell row={row} prop={prop} value={value} {...environment} onInput={(next) => void setValue(next)} />;
    case "verification":
      return <VerificationCell prop={prop} value={value} currentUserId={environment.snapshot.userId} onCommit={(next) => void setValue(next)} />;
    case "location":
    case "last_visited_time":
      // Kept for the columns that have one, but nothing fills it in: it says so (decided 2026-10-16).
      return <span className={styles.cellReadonly} data-structured-property={prop.type} title={t("propertyCell:notFilledIn")}>
        {runtimeOwnedPropertySummary(prop.type, value)}</span>;
    case "person":
      return <PropertyPeopleCell row={row} prop={prop} presentation={presentation} {...environment} />;
    case "created_by":
    case "last_edited_by":
      return <PeopleReadonly value={value} userId={snapshot.userId} snapshot={snapshot} />;
    case "created_time":
    case "last_edited_time":
      return <span className={styles.cellReadonly}>{formatTimestampForProperty(value, prop)}</span>;
    case "unique_id":
      return <span className={`${styles.cellReadonly} ${styles.uniqueIdValue}`} {...uniqueIdAwaitedAttributes(prop, value)}>{uniqueIdText(prop, value)}</span>;
    default:
      return (
        <PropertyTextCell
          row={row}
          prop={prop}
          value={value}
          autoFocus={autoFocus}
          onInput={(next, spans) => void setTextValue(next, spans)}
          presentation={presentation}
          titleIcon={titleIcon}
        />
      );
  }
}

// The snapshot's lookups of other rows, which a cell reading only its own row
// leaves alone: they are made again as any row changes.
export const OTHER_ROWS_LOOKUPS = new Set(["pagesById", "propertiesByDataSourceId", "hydratedRelationTargetIds", "loadedDataSourceIds"]);
// The cells whose editors use their context only to hand it to their
// operations, as they write: drawn again only for their own row, they write
// with the context the table was last drawn with (liveCellWays).
export const OWN_ROW_EDITORS = new Set([
  "title", "rich_text", "url", "email", "phone", "number", "checkbox", "date", "select", "status", "multi_select",
  "created_time", "last_edited_time", "created_by", "last_edited_by", "unique_id", "verification", "location", "last_visited_time",
]);

function sameShallow(previous: object, next: object, skip?: (key: string) => boolean) {
  const before = previous as Record<string, unknown>, after = next as Record<string, unknown>;
  const keys = Object.keys(after);
  if (Object.keys(before).length !== keys.length) return false;
  return keys.every((key) => skip?.(key) || before[key] === after[key]);
}

// The cells that show their own value of their row and nothing else of it:
// such a cell is its own piece of the row (ownValueSame).
const OWN_VALUE_CELLS = new Set([
  "number", "checkbox", "date", "select", "status", "multi_select", "created_time", "last_edited_time", "created_by",
  "last_edited_by", "unique_id", "verification", "location", "last_visited_time",
]);

// Whether a cell showing its own value alone shows the same of its row
// now: a press that changes one value of a row draws that value's cell, not
// the row's every cell (user decision 2026-11-12).
function ownValueSame(previous: PropertyCellProps, next: PropertyCellProps) {
  const prop = next.prop, before = previous.row, after = next.row;
  if (!OWN_VALUE_CELLS.has(prop.type) || prop.config?.appEditor || before.id !== after.id) return false;
  // The value as the row keeps it, its property compared apart: read through
  // the property (a status's default, the options a select still has) it is
  // made afresh. Times and authors are the row's own.
  return ROW_OWN_VALUES.has(prop.type) ? getValue(before, prop) === getValue(after, prop)
    : before.properties?.[prop.id] === after.properties?.[prop.id];
}
const ROW_OWN_VALUES = new Set(["created_time", "last_edited_time", "created_by", "last_edited_by"]);

// A cell is drawn again when what it shows changed: its row (or, for a cell
// showing its own value alone, that value), its property (made afresh for
// each drawing of a table, so read by its parts), its editor's ways and,
// for a cell reading other rows, those rows (a relation's and a plain
// rollup's, the pages it reads), for a formula of its own row alone its
// database's properties (otherRowsRead.ts). Typing in one cell of a
// table of 30 rows by 40 properties drew all 1,205 again for each letter
// (seen 2026-11-12).
function samePropertyCell(previous: PropertyCellProps, next: PropertyCellProps) {
  const sameProp = previous.prop === next.prop || sameShallow(previous.prop, next.prop);
  const others = readsOtherRows(next.prop, next.snapshot.propertiesByDataSourceId[next.prop.databaseId]);
  const sameRead = sameProp && others && sameOtherRowsRead(next.prop, previous.row, next.row, previous.snapshot, next.snapshot);
  return sameShallow(previous, next, (key) => key === "prop" || key === "snapshot" || (key === "row" && (sameRead || ownValueSame(previous, next))))
    && sameProp && (previous.snapshot === next.snapshot || sameRead || (!others
      && sameShallow(previous.snapshot, next.snapshot, (key) => OTHER_ROWS_LOOKUPS.has(key))
      && cellReadsSame(next.prop, previous.row, next.row, previous.snapshot, next.snapshot)));
}

// A relation's cell hands its context to its operations too, its picker's
// reads included, a rollup's to its reads of the databases it rolls up and a
// formula's to its editor's: they go by the latest ways while drawn again
// for what they read alone (a table with a rollup column drew all its rows
// again for a row made or copied, 2026-11-14). Their reads start as they
// show and as the databases they read change, not again for each drawing.
const LIVE_WAYS = new Set([...OWN_ROW_EDITORS, "relation", "rollup", "formula"]);

/** Whether a property's cell writes with the table's latest ways, drawn again only for what it reads (liveCellWays). */
export function cellTakesLatestWays(prop: { type: string; config?: { appEditor?: unknown } | null }) {
  return LIVE_WAYS.has(prop.type) && !prop.config?.appEditor;
}

const ShownPropertyCell = memo(PropertyCellImpl, samePropertyCell);

type LiveCellWays = Pick<PropertyCellProps, "context" | "operations">;

// A context and operations that stay the same object while the table's are
// made again for each change of a row (its generation): the context reads as
// the latest, and handed to an operation it is the latest itself, so a write
// is checked against the table as last drawn, as from a cell drawn again.
export function liveCellWays(latest: { readonly current: LiveCellWays }): LiveCellWays {
  const context = new Proxy({} as LiveCellWays["context"], {
    get: (_, key) => Reflect.get(latest.current.context, key),
    has: (_, key) => Reflect.has(latest.current.context, key),
  });
  const operations = new Proxy({} as LiveCellWays["operations"], {
    get(_, key) {
      const value: unknown = Reflect.get(latest.current.operations, key);
      if (typeof value !== "function") return value;
      return (...args: unknown[]) => value.apply(latest.current.operations,
        args.map((arg) => arg === context ? latest.current.context : arg));
    },
    has: (_, key) => Reflect.has(latest.current.operations, key),
  });
  return { context, operations };
}

export function PropertyCell(props: PropertyCellProps) {
  const latest = useRef<PropertyCellProps>(props);
  latest.current = props;
  const [live] = useState(() => liveCellWays(latest));
  // Opening a page goes by the latest way given, which a table makes afresh
  // for each drawing.
  const onOpenPage = useCallback((pageId: string) => latest.current.onOpenPage?.(pageId), []);
  const ownRow = cellTakesLatestWays(props.prop);
  return <ShownPropertyCell {...props} {...(ownRow ? live : null)} onOpenPage={props.onOpenPage ? onOpenPage : undefined} />;
}
