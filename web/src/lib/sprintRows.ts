import type { GraphDataSource, GraphRowValue } from "@/contentGraph";
import { i18next } from "@/i18n";

/** What a new sprint is called until it is named. */
export function newSprintName() {
  return String(i18next.t("databaseView:newSprint"));
}

/** A title still reading as a new sprint's, which typing replaces. */
export function isNewSprintName(title: string | null | undefined) {
  return title === newSprintName();
}

/**
 * What a row made by hand in a Sprints database starts with, as Notion's
 * "+ 새 스프린트" (seen 2026-11-04): that name, a future sprint, and the runner
 * the server gives every row there. Nothing for another database.
 */
export function newSprintRowStart(source: Pick<GraphDataSource, "sprintStatusPropertyId" | "sprintFutureOptionId"> | undefined, titlePropertyId: string) {
  if (!source?.sprintStatusPropertyId || !source.sprintFutureOptionId) return null;
  const values: Record<string, GraphRowValue> = {
    [titlePropertyId]: { type: "title", value: newSprintName() },
    [source.sprintStatusPropertyId]: { type: "status", value: source.sprintFutureOptionId },
  };
  return { values, icon: "run:lightgray", iconType: "icon" as const };
}
