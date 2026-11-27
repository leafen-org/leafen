import { compareGraphOrder, type GraphDataSource, type GraphProperty, type GraphRowValue } from "@/contentGraph";
import { dateKey } from "@/components/database/dateUtils";
import { newSprintRowStart } from "@/lib/sprintRows";

type NewRowSource = Pick<GraphDataSource, "id" | "sprintStatusPropertyId" | "sprintFutureOptionId" | "meetingDatabase">;

/**
 * What a row made by hand starts with in a database that starts its rows: a
 * Sprints database's (sprintRows.ts), or a meeting notes database's, as
 * Notion's "+ 새 회의" (seen 2026-11-12): today, the maker's day, in its date —
 * the first date property, the one its template made — and the gray calendar
 * the server gives every meeting made there (database_meeting_rows.go).
 * Nothing for another database; the values a view's filter or group gives the
 * row go over these.
 */
export function newRowStart(source: NewRowSource | undefined, titlePropertyId: string, properties: readonly GraphProperty[]) {
  const sprint = newSprintRowStart(source, titlePropertyId);
  if (sprint || !source?.meetingDatabase) return sprint;
  const date = properties.filter((property) => property.dataSourceId === source.id && property.type === "date").sort(compareGraphOrder)[0];
  const values: Record<string, GraphRowValue> = { [titlePropertyId]: { type: "title", value: "" } };
  if (date) values[date.id] = { type: "date", value: { start: dateKey(new Date()) } };
  return { values, icon: "calendar:gray", iconType: "icon" as const };
}
