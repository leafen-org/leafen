import { createGraphProperty, type GraphProperty } from "@/contentGraph";
import type { DatabaseToolbarTaskFeatureWire } from "@/databaseToolbarConfigWire.generated";
import { configForType } from "./propertyTypes";
import { propertyAfterTypeChange } from "./propertyEditModel";

/** The choice that makes the property, as Notion's "새로 만들기". */
export const NEW_TASK_PROPERTY = "__new__";
export type TaskDatabaseChoice = { assigneeId: string; statusId: string; dueId: string };
export type TaskRole = keyof TaskDatabaseChoice;

/**
 * A task database's three roles as Notion's "작업 데이터베이스로 전환" asks
 * them (seen 2026-11-02, and 2026-11-18 in Chrome at 1440 on a database with
 * a person, a select, a checkbox and two dates and on one with none): each
 * picks among the database's properties of its kind or "새로 만들기", never
 * none; it starts on the one there is, on "새로 만들기" where there is none
 * and on nothing where there are more; pressed with one not chosen, the
 * window says "필수 속성입니다." in that picker and converts nothing. The
 * server holds the three to the same (completeDatabaseTaskFeatures).
 */
export function taskRoleCandidates(properties: readonly GraphProperty[]): Record<TaskRole, GraphProperty[]> {
  return {
    assigneeId: properties.filter(({ type }) => type === "person"),
    statusId: properties.filter(({ type }) => type === "status" || type === "checkbox" || type === "select"),
    dueId: properties.filter(({ type }) => type === "date"),
  };
}

export function initialTaskRole(options: readonly GraphProperty[]) {
  return options.length === 1 ? options[0].id : options.length ? "" : NEW_TASK_PROPERTY;
}

/** The roles a choice leaves unchosen, which the window names before it converts. */
export function missingTaskRoles(choice: TaskDatabaseChoice): TaskRole[] {
  return (["assigneeId", "statusId", "dueId"] as const).filter((role) => !choice[role]);
}

/**
 * The task settings a choice turns on and the properties it makes or retypes,
 * which go to the server in one change (database_task_conversion.go): those
 * chosen as "새로 만들기" made in Notion's order — assignee, due date, status
 * (seen 2026-11-03) — each after the one before, named as the server keeps
 * them; a select chosen for the status becomes one.
 */
export function taskConversionChange(
  choice: TaskDatabaseChoice, properties: readonly GraphProperty[], dataSourceId: string,
  names: { assignee: string; dueDate: string; status: string },
): { tasks: NonNullable<DatabaseToolbarTaskFeatureWire["tasks"]>; made: GraphProperty[] } {
  const made: GraphProperty[] = [];
  const role = (key: TaskRole, type: GraphProperty["type"], name: string) => {
    if (choice[key] !== NEW_TASK_PROPERTY) return choice[key];
    const config = type === "status" ? configForType("status", undefined, "") as Record<string, unknown> : {};
    const { property } = createGraphProperty([...properties, ...made], dataSourceId, type, name, config);
    made.push(property);
    return property.id;
  };
  const assignee = role("assigneeId", "person", names.assignee);
  const due = role("dueId", "date", names.dueDate);
  const status = role("statusId", "status", names.status);
  const select = properties.find((property) => property.id === status && property.type === "select");
  if (select) made.push(propertyAfterTypeChange(select, "status", dataSourceId));
  return {
    tasks: {
      enabled: true, statusPropertyId: status, assigneePropertyId: assignee, dueDatePropertyId: due,
      // What the status was, which turning it back restores.
      ...(select ? { statusWasSelect: true } : {}),
    },
    made,
  };
}
