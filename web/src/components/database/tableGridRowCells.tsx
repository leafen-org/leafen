import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { Page } from "@/lib/types";
import { graphRowPageTimes } from "./galleryViewModel";
import { formatNumberForProperty, numberInputText, parseNumberInput } from "./numberFormat";
import { graphRowPropertySpans, graphRowTitleSpans } from "./richTextCellModel";

// A table row read as a page, and a cell's plain editor where no property cell
// draws it (TableGrid).

function displayValue(row: GraphRow, property: GraphProperty) {
  const value = row.values[property.id]?.value;
  if (Array.isArray(value)) return value.join(", ");
  if (value && typeof value === "object") return JSON.stringify(value);
  return value == null ? "" : String(value);
}

// Each row's page, kept while the row and its title property stay: a graph
// read again keeps its unchanged rows (lib/keptGraph.ts), and so their pages.
const rowPages = new WeakMap<GraphRow, { title: GraphProperty | undefined; workspaceId: string; page: Page }>();

/** A table row as the page its cells and the table's foot read. */
export function tableRowPage(row: GraphRow, properties: readonly GraphProperty[], workspaceId: string): Page {
  const title = properties.find((property) => property.type === "title");
  const kept = rowPages.get(row);
  if (kept && kept.title === title && kept.workspaceId === workspaceId) return kept.page;
  const page = readRowPage(row, title, workspaceId);
  rowPages.set(row, { title, workspaceId, page });
  return page;
}

function readRowPage(row: GraphRow, title: GraphProperty | undefined, workspaceId: string): Page {
  return {
    id: row.id,
    workspaceId,
    parentId: row.dataSourceId,
    parentType: "database",
    kind: "page",
    title: title ? displayValue(row, title) : "",
    properties: Object.fromEntries(Object.entries(row.values).map(([id, value]) => [id, value.value])),
    propertySpans: graphRowPropertySpans(row),
    titleSpans: graphRowTitleSpans(row),
    subitemParentId: row.subitemParentId ?? undefined,
    ...graphRowPageTimes(row),
    icon: row.icon,
    iconType: row.iconType ?? "none",
    hasContent: row.hasContent,
    position: 0,
  };
}

export function fallbackEditor({
  property,
  readOnly,
  row,
  staticReadOnlySemantics,
  onCellValue,
}: {
  property: GraphProperty;
  readOnly: boolean;
  row: GraphRow;
  staticReadOnlySemantics: boolean;
  onCellValue(row: GraphRow, property: GraphProperty, value: unknown): void;
}) {
  const value = row.values[property.id]?.value;
  if (readOnly) return <span aria-label={staticReadOnlySemantics ? undefined : property.name}>{property.type === "number" ? formatNumberForProperty(value, property) : displayValue(row, property)}</span>;
  if (property.type === "checkbox") {
    return <input type="checkbox" aria-label={property.name} checked={Boolean(value)} onChange={(event) => onCellValue(row, property, event.target.checked)} />;
  }
  const inputType = property.type === "date" ? "date" : property.type === "email" ? "email" : property.type === "url" ? "url" : "text";
  return (
    <input
      key={`${row.id}:${property.id}:${String(value ?? "")}`}
      aria-label={property.name}
      defaultValue={property.type === "number" ? numberInputText(value, property) : displayValue(row, property)}
      type={inputType}
      inputMode={property.type === "number" ? "decimal" : undefined}
      onBlur={(event) => {
        const next = property.type === "number" ? parseNumberInput(event.target.value, property) : event.target.value;
        if (next !== undefined) onCellValue(row, property, next);
      }}
    />
  );
}
