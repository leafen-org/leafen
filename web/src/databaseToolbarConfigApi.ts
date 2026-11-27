import { request } from "@/apiTransport";
import { chosenTimeZone } from "@/lib/dateReminders";
import { followPageDatabaseCopies } from "@/lib/pageDatabaseCopies";
import type { Session } from "@/types";
import {
  databaseToolbarConfigBounds,
  databaseToolbarConfigOperations,
  type DatabaseFormFavorites,
  type DatabaseMyTask,
  type DatabaseMyTaskResult,
  type DatabaseMyTaskSources,
  type DatabaseMyTasks,
  type DatabaseSprintActionInput,
  type DatabaseSprintActionResponse,
  type DatabaseSprintState,
  type DatabaseToolbarConfigMetadataResponse,
  type DatabaseToolbarConfigMutationInput,
  type DatabaseToolbarConfigMutationResponse,
  type DatabaseToolbarTemplateRowCreateInput,
  type DatabaseToolbarTemplateRowCreateResponse,
} from "@/databaseToolbarConfigWire.generated";

function toolbarConfigPath(
  template: string,
  pageId: string,
  dataSourceId: string,
  templateId = "",
) {
  return template
    .replace("{pageId}", encodeURIComponent(pageId))
    .replace("{dataSourceId}", encodeURIComponent(dataSourceId))
    .replace("{templateId}", encodeURIComponent(templateId));
}

export function readDatabaseToolbarConfig(
  session: Session,
  pageId: string,
  dataSourceId: string,
  options: { cursor?: string; limit?: number; signal?: AbortSignal } = {},
) {
  const parameters = new URLSearchParams({
    limit: String(options.limit ?? databaseToolbarConfigBounds.metadataPageSize),
  });
  if (options.cursor) parameters.set("cursor", options.cursor);
  return request<DatabaseToolbarConfigMetadataResponse>(
    `${toolbarConfigPath(databaseToolbarConfigOperations.metadata.path, pageId, dataSourceId)}?${parameters}`,
    { method: databaseToolbarConfigOperations.metadata.method, signal: options.signal },
    session,
  );
}

export function mutateDatabaseToolbarConfig(
  session: Session,
  pageId: string,
  dataSourceId: string,
  input: DatabaseToolbarConfigMutationInput,
  signal?: AbortSignal,
) {
  return request<DatabaseToolbarConfigMutationResponse>(
    toolbarConfigPath(databaseToolbarConfigOperations.mutate.path, pageId, dataSourceId),
    {
      method: databaseToolbarConfigOperations.mutate.method,
      body: JSON.stringify(input),
      signal,
    },
    session,
  );
}

export async function createDatabaseToolbarTemplateRow(
  session: Session,
  pageId: string,
  dataSourceId: string,
  input: DatabaseToolbarTemplateRowCreateInput,
  signal?: AbortSignal,
) {
  const created = await request<DatabaseToolbarTemplateRowCreateResponse>(
    toolbarConfigPath(databaseToolbarConfigOperations.templateRowCreate.path, pageId, dataSourceId),
    {
      method: databaseToolbarConfigOperations.templateRowCreate.method,
      body: JSON.stringify(input),
      signal,
    },
    session,
  );
  // Its body's own databases come after the rest (lib/pageDatabaseCopies.ts).
  followPageDatabaseCopies(session, created.rowId, created.copyingDatabases === true);
  return created;
}

export function readDatabaseSprints(
  session: Session,
  pageId: string,
  dataSourceId: string,
  signal?: AbortSignal,
) {
  return request<DatabaseSprintState>(
    toolbarConfigPath(databaseToolbarConfigOperations.sprintRead.path, pageId, dataSourceId),
    { method: databaseToolbarConfigOperations.sprintRead.method, signal },
    session,
  );
}

export function runDatabaseSprintAction(
  session: Session,
  pageId: string,
  dataSourceId: string,
  input: DatabaseSprintActionInput,
  signal?: AbortSignal,
) {
  return request<DatabaseSprintActionResponse>(
    toolbarConfigPath(databaseToolbarConfigOperations.sprintAction.path, pageId, dataSourceId),
    { method: databaseToolbarConfigOperations.sprintAction.method, body: JSON.stringify(input), signal },
    session,
  );
}

function myTasksPath(template: string, workspaceId: string) {
  return template.replace("{workspaceId}", encodeURIComponent(workspaceId));
}

// The first window My tasks' screens read is no larger than what the list
// read before its first frame (a database's first 200 rows); the rest go
// 500 at a time, the most a window gives.
const MY_TASKS_FIRST_WINDOW = 200;
const MY_TASKS_WINDOW = 500;

/** The server's own list order (database_my_tasks.go): open first, dated first by due date, then by name. */
function myTasksListOrder(left: DatabaseMyTask, right: DatabaseMyTask) {
  if (left.done !== right.done) return left.done ? 1 : -1;
  if (!left.dueStart !== !right.dueStart) return left.dueStart ? -1 : 1;
  if (left.dueStart !== right.dueStart) return left.dueStart < right.dueStart ? -1 : 1;
  const a = left.title.toLowerCase(), b = right.title.toLowerCase();
  return a < b ? -1 : a > b ? 1 : 0;
}

/**
 * Every task "내 작업" takes from its sources — none until the person picks
 * them, then everyone's — as the My tasks page and Home's widget show them,
 * or with own only the person's own there, as Home's "예정된 이벤트" lists
 * them. Read a window at a time to the last and given in
 * the server's list order (2026-11-24: both showed a task database's first
 * 200 rows, 500 in all, as every task). While more follow, the first window
 * goes to onFirst, so a screen shows it at once and the rest after.
 */
export async function readMyTasksToTheEnd(session: Session, workspaceId: string,
  { signal, onFirst, own = false }: { signal?: AbortSignal; onFirst?(tasks: DatabaseMyTask[]): void; own?: boolean } = {}) {
  const tasks: DatabaseMyTask[] = [];
  const path = myTasksPath(databaseToolbarConfigOperations.myTasks.path, workspaceId);
  for (let cursor = "", limit = MY_TASKS_FIRST_WINDOW; ; limit = MY_TASKS_WINDOW) {
    const answer = await request<DatabaseMyTasks>(
      `${path}?${new URLSearchParams({ scope: "sources", ...(own ? { assignee: "me" } : {}), limit: String(limit), ...(cursor ? { cursor } : {}) })}`,
      { method: databaseToolbarConfigOperations.myTasks.method, signal },
      session,
    );
    tasks.push(...answer.tasks);
    if (!answer.nextCursor) return tasks.sort(myTasksListOrder);
    if (!cursor) onFirst?.([...tasks].sort(myTasksListOrder));
    cursor = answer.nextCursor;
  }
}

/** My tasks' sources and the task databases there are to pick from. */
export function readMyTaskSources(session: Session, workspaceId: string, signal?: AbortSignal) {
  return request<DatabaseMyTaskSources>(
    myTasksPath(databaseToolbarConfigOperations.myTaskSourcesRead.path, workspaceId),
    { method: databaseToolbarConfigOperations.myTaskSourcesRead.method, signal },
    session,
  );
}

export function saveMyTaskSources(session: Session, workspaceId: string, dataSourceIds: string[], signal?: AbortSignal) {
  return request<DatabaseMyTaskSources>(
    myTasksPath(databaseToolbarConfigOperations.myTaskSourcesSave.path, workspaceId),
    { method: databaseToolbarConfigOperations.myTaskSourcesSave.method, body: JSON.stringify({ dataSourceIds }), signal },
    session,
  );
}

/** Makes a task in one of the sources, assigned to the person, from its database's default template. */
export function createMyTask(session: Session, workspaceId: string, input: { requestId: string; dataSourceId: string; title: string }) {
  return request<DatabaseMyTaskResult>(
    myTasksPath(databaseToolbarConfigOperations.myTaskCreate.path, workspaceId),
    { method: databaseToolbarConfigOperations.myTaskCreate.method, body: JSON.stringify({ ...input, timeZone: chosenTimeZone() }) },
    session,
  );
}

/**
 * Sets a task's status: done or not, or a group — the option named in it when
 * its database has one so named, else the group's first.
 */
export function markMyTaskDone(session: Session, workspaceId: string, rowId: string, input: { requestId: string; done: boolean; group?: string; statusName?: string },
  signal?: AbortSignal) {
  return request<DatabaseMyTaskResult>(
    myTasksPath(databaseToolbarConfigOperations.myTaskDone.path, workspaceId).replace("{rowId}", encodeURIComponent(rowId)),
    { method: databaseToolbarConfigOperations.myTaskDone.method, body: JSON.stringify(input), signal },
    session,
  );
}

/** My tasks' saved views, the person's own. */
export function readMyTaskViews<View>(session: Session, workspaceId: string, signal?: AbortSignal) {
  return request<{ views: View[] }>(
    myTasksPath(databaseToolbarConfigOperations.myTaskViewsRead.path, workspaceId),
    { method: databaseToolbarConfigOperations.myTaskViewsRead.method, signal },
    session,
  );
}

export function saveMyTaskViews<View>(session: Session, workspaceId: string, views: readonly View[]) {
  return request<void>(
    myTasksPath(databaseToolbarConfigOperations.myTaskViewsSave.path, workspaceId),
    { method: databaseToolbarConfigOperations.myTaskViewsSave.method, body: JSON.stringify({ views }) },
    session,
  );
}

/** The person's favorite forms in the workspace. */
export function readFormFavorites(session: Session, workspaceId: string, signal?: AbortSignal) {
  return request<DatabaseFormFavorites>(
    myTasksPath(databaseToolbarConfigOperations.formFavoritesRead.path, workspaceId),
    { method: databaseToolbarConfigOperations.formFavoritesRead.method, signal },
    session,
  );
}

export function saveFormFavorite(session: Session, token: string, favorite: boolean) {
  return request<void>(
    databaseToolbarConfigOperations.formFavoriteSave.path,
    { method: databaseToolbarConfigOperations.formFavoriteSave.method, body: JSON.stringify({ token, favorite }) },
    session,
  );
}
