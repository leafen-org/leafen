import { optionValueKnown, statusValueOrDefault } from "@/lib/statusDefaultValue";

/**
 * A row's value as it reads, where a property shows a default it doesn't
 * keep in the row: a status its default option while the row sets none
 * (statusValueOrDefault), and a person property that defaults to the creator
 * the row's creator while it names no one, as Notion's "기본값: 생성자" (seen
 * 2026-10-10: read, not stored; clearing it shows the creator again, and
 * turning the default off empties those rows), and a select or multi-select
 * without the options it no longer has (optionValueKnown). Every cell, filter, sort,
 * group, sum and formula reads a value here; the server reads the same
 * (databaseValueAsRead).
 */
export function valueAsRead(
  row: { properties?: Record<string, unknown>; createdBy?: string | null },
  prop: { id: string; type: string; config?: object | null },
  value: unknown = row.properties?.[prop.id],
) {
  if (prop.type === "status") return statusValueOrDefault(prop.config, value);
  if (prop.type === "select" || prop.type === "multi_select") return optionValueKnown(prop.config, value);
  if (prop.type !== "person" || (prop.config as { defaultCreator?: boolean } | null | undefined)?.defaultCreator !== true || !row.createdBy) return value;
  const names = Array.isArray(value) ? value.length > 0 : value != null && value !== "";
  return names ? value : [row.createdBy];
}
