import type { GraphProperty, GraphRowValue } from "@/contentGraph";
import { propertyTextLink } from "@/components/database/propertyLinks";

const TEXT_TYPES = new Set(["rich_text", "url", "email", "phone"]);
const OPTION_TYPES = new Set(["select", "status"]);
const LINKED_TYPES = new Set(["url", "email", "phone"]);

/** Types whose cells are stored alike, so a change between them converts nothing on the server. */
export function typesKeepTheirCells(left: GraphProperty["type"], right: GraphProperty["type"]) {
  return left === right || (TEXT_TYPES.has(left) && TEXT_TYPES.has(right)) || (OPTION_TYPES.has(left) && OPTION_TYPES.has(right));
}

function optionName(property: GraphProperty, id: unknown) {
  const options = (property.config as { options?: { id?: string; name?: string }[] } | undefined)?.options;
  return options?.find((option) => option.id === id)?.name ?? "";
}

/** A value as the server's conversion reads it; what only the server can name reads as nothing. */
function valueText(property: GraphProperty, value: unknown): string {
  if (value === null || value === undefined) return "";
  switch (property.type) {
    case "select":
    case "status":
      return optionName(property, value);
    case "multi_select":
      return Array.isArray(value) ? value.map((id) => optionName(property, id)).filter(Boolean).join(",") : "";
    case "date": {
      const date = value as { start?: string; end?: string | null };
      return date.start ? (date.end ? `${date.start} → ${date.end}` : date.start) : "";
    }
    case "checkbox":
      return value === true ? "Yes" : "No";
    case "files":
      return Array.isArray(value) ? value.map((file) => file?.name ?? "").filter(Boolean).join(",") : "";
    case "person":
    case "relation":
      return "";
    default:
      return typeof value === "object" ? "" : String(value);
  }
}

/**
 * A cell as a change between types stored alike leaves it, as the server's
 * (database_property_alike_type_change.go): a URL, email or phone made text is
 * its value linked to the first link it holds, as Notion's (propertyTextLink);
 * any other keeps the type it was written as, text under a URL read as its
 * plain value and an option as its option.
 */
function keptCell(cell: GraphRowValue, type: GraphProperty["type"]): GraphRowValue {
  if (type !== "rich_text" || !LINKED_TYPES.has(cell.type) || typeof cell.value !== "string") return cell;
  const href = propertyTextLink(cell.value);
  return { type: "rich_text", value: cell.value, ...(href ? { spans: [{ text: cell.value, link: href }] } : {}) };
}

function cellOfType(type: GraphProperty["type"], text: string): GraphRowValue {
  if (TEXT_TYPES.has(type)) return { type, value: text || null };
  const number = type === "number" && text.trim() ? Number(text) : NaN;
  return { type, value: Number.isFinite(number) ? number : null };
}

/**
 * The rows while the server changes a property's type: its cells read as the
 * new type, never as the old type's raw value (an option's ID, an object),
 * until the converted cells are read; a change between types stored alike
 * keeps them (keptCell). Notion's show the new value at once.
 */
export function rowsWhileTypeChanges<Row extends { values: Record<string, GraphRowValue> }>(
  rows: Row[],
  before: readonly GraphProperty[],
  saved: readonly GraphProperty[],
): Row[] {
  const changed = saved.flatMap((next) => {
    const was = before.find((property) => property.id === next.id);
    return was && was.type !== next.type ? [{ was, next }] : [];
  });
  if (!changed.length) return rows;
  return rows.map((row) => {
    let values = row.values;
    for (const { was, next } of changed) {
      const cell = row.values[was.id];
      if (!cell || cell.type === next.type) continue;
      const shown = typesKeepTheirCells(was.type, next.type) ? keptCell(cell, next.type) : cellOfType(next.type, valueText(was, cell.value));
      if (shown !== cell) values = { ...values, [was.id]: shown };
    }
    return values === row.values ? row : { ...row, values };
  });
}
