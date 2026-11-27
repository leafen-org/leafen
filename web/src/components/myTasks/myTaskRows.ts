import type { GraphRow, GraphView } from "@/contentGraph";
import type { DatabaseMyTask } from "@/databaseToolbarConfigWire.generated";
import { currentUserFilterValue } from "@/shared/database/queryFilterPolicy";

// "내 작업"'s rows: each task of its sources as its own view draws it, and
// what a change of its status writes to the task's own database
// (MyTasksDatabaseView.tsx).

/** The properties of "내 작업"'s own view over its sources' tasks. */
export const TITLE = "my-tasks-title", STATUS = "my-tasks-status", ASSIGNEE = "my-tasks-assignee", DUE = "my-tasks-due", SOURCE = "my-tasks-source";
export const GROUPS = ["todo", "inProgress", "complete"] as const;
export const MY_TASKS_TAB_ICON = "checkmark-square:lightgray", MY_TASKS_PATH = "/my-tasks";
export const GROUP_COLORS = { todo: "default", inProgress: "blue", complete: "green" } as const;

export type Group = (typeof GROUPS)[number];
export const groupOf = (task: DatabaseMyTask): Group =>
  (GROUPS as readonly string[]).includes(task.statusGroup) ? task.statusGroup as Group : task.done ? "complete" : "todo";

// An option's id is never a group's, so that a filter on "todo" reads the whole group.
export const statusOptionOf = (task: DatabaseMyTask) => `${groupOf(task)}|${task.statusName}`;
export const groupOfOption = (option: string) => option.split("|")[0];
export const nameOfOption = (option: string) => option.slice(option.indexOf("|") + 1);

/**
 * Whether a status written over the one a task shows changes it: a named
 * option — chosen, a card moved to its column, or the one a task had, put
 * back by ⌘Z — when it is another; a group's own when it is another group.
 */
export const statusChanges = (written: string, showing: string) =>
  nameOfOption(written) ? written !== showing : groupOfOption(written) !== groupOfOption(showing);

/** What sets a task's status to an option of this page's: its group, and its name — the group's first option for a group's own. */
export const statusWrite = (option: string, requestId: string) => {
  const group = groupOfOption(option), statusName = nameOfOption(option);
  return { requestId, done: group === "complete", group, ...(statusName ? { statusName } : {}) };
};

/**
 * A task as the view draws it — one read, or one made here as the server
 * will read it back (myTaskMadeHere.ts) — with what this device changed and
 * hasn't read back yet over it (`shown`).
 */
export const taskRow = (task: DatabaseMyTask, sourceId: string, position: string, shown?: GraphRow["values"]): GraphRow => ({
  id: task.rowId, dataSourceId: sourceId, subitemParentId: null, position,
  ...(task.icon && task.iconType !== "none" ? { icon: task.icon, iconType: task.iconType } : {}),
  // A task in a database the reader may only view takes no check or edit there.
  ...(task.canEdit ? {} : { readOnlyForMe: true }),
  values: {
    [TITLE]: { type: "title", value: task.title },
    [STATUS]: { type: "status", value: statusOptionOf(task) },
    [ASSIGNEE]: { type: "person", value: task.assignees ?? [] },
    [DUE]: { type: "date", value: task.dueStart ? { start: task.dueStart, ...(task.dueEnd ? { end: task.dueEnd } : {}) } : null },
    [SOURCE]: { type: "rich_text", value: task.databaseTitle },
    ...shown,
  },
});

/**
 * The person's views name them as "나", whoever reads them, as Notion's
 * "담당자: 값을 포함하는 데이터 나" (seen 2026-11-08); a view kept with their own
 * id as its assignee is the same person here, so it reads as "나" too.
 */
export function withAssigneeAsMe(view: GraphView, me: string): GraphView {
  const filters = view.config.quickFilters;
  if (!Array.isArray(filters)) return view;
  const isMe = (filter: { propertyId?: unknown; value?: unknown }) => filter.propertyId === ASSIGNEE
    && Array.isArray(filter.value) && filter.value.length === 1 && filter.value[0] === me;
  if (!filters.some(isMe)) return view;
  return { ...view, config: { ...view.config, quickFilters: filters.map((filter) => isMe(filter) ? { ...filter, value: [currentUserFilterValue()] } : filter) } };
}

/** Its tab's check is Notion's light grey (seen 2026-11-15: #7f7f7f on dark), a view kept with the earlier grey too. */
export const withTabIcon = (view: GraphView): GraphView =>
  view.config.icon === "checkmark-square:gray" ? { ...view, config: { ...view.config, icon: MY_TASKS_TAB_ICON } } : view;
