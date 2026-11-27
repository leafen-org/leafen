import type { DbProperty } from "@/lib/types";

/**
 * An ID without its number yet: the server numbers every row a database gains
 * (database_unique_id_allocation.go), so one without is on its way — a row
 * just made here, or rows an ID property just made wait for.
 */
export function uniqueIdAwaited(value: unknown) {
  return value == null || value === "";
}

/**
 * What an ID reads: its prefix and number, or "...", faint, while its number
 * is on its way — in a table's cell, on a card, on a list's line and on its
 * page alike, as Notion's (seen 2026-11-14 at 1440, dark: three full stops in
 * the ID's own colour, rgb 125,122,117, until the number comes).
 */
export function uniqueIdText(prop: Pick<DbProperty, "config">, value: unknown) {
  if (uniqueIdAwaited(value)) return "...";
  const prefix = prop.config?.idPrefix?.trim();
  return prefix ? `${prefix}-${value}` : String(value);
}

/** An ID waiting for its number says so to a screen reader: busy, named by its property. */
export function uniqueIdAwaitedAttributes(prop: Pick<DbProperty, "name">, value: unknown) {
  return uniqueIdAwaited(value)
    ? { "aria-busy": true, "aria-label": prop.name, "data-unique-id-pending": "true", role: "status" } as const
    : {};
}
