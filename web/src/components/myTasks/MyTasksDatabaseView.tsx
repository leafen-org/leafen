import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { GraphBlock, GraphDataSource, GraphProperty, GraphRow, GraphView, GraphViewType } from "@/contentGraph";
import { DatabaseFullPageLinkContext, DatabaseOwnPageContext } from "@/components/database/databaseFullPageLink";
import { DatabaseEmptyRowsContext, DatabaseNewRowControlContext, DatabaseOwnValueContext, DatabaseRowsAddableContext, DatabaseSearchableContext, DatabaseSettingsFootContext, DatabaseSourceSettingsContext } from "@/components/database/databaseNewRowControl";
import { NativeDatabaseViewMount } from "@/components/database/NativeDatabaseViewMount";
import { projectRows } from "@/components/database/viewRowProjection";
import { readMyTaskViews, saveMyTaskViews } from "@/databaseToolbarConfigApi";
import { makeMyTask } from "@/lib/myTaskMadeHere";
import { afterPageMade } from "@/lib/pagesNotYetMade";
import { sendRowValues, sendTaskStatus } from "@/lib/rowValuesKept";
import { DatabaseLockContext, DatabaseStructureLockContext, type DatabaseLockControls } from "../database/databaseStructureLock";
import type { DatabaseMyTask, DatabaseMyTaskSource } from "@/databaseToolbarConfigWire.generated";
import { sendRowTrash } from "@/lib/rowTrashKept";
import { formFavoritesChanged } from "@/lib/formFavorites";
import { newId } from "@/lib/ids";
import { useProductToast } from "@/lib/productToast";
import { rememberRowUntitledNames } from "@/lib/untitledRowNames";
import type { NativeDatabaseViewProductAdapter } from "@/lib/nativeDatabaseViewProductAdapter";
import type { NativeBoardProductAdapter } from "@/lib/nativeBoardProductAdapter";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import { currentUserFilterValue } from "@/shared/database/queryFilterPolicy";
import type { Session } from "@/types";
import { MyTaskDatabaseIcon } from "./MyTaskSourcesDialog";
import { useTaskEditsShownAtOnce } from "./useTaskEditsShownAtOnce";
import { useMyTasksUndo, type MyTasksStep } from "./useMyTasksUndo";
import { fitMyTaskViews, orderAsList, orderKept, ownOrderKept, taskIdsNewestFirst, withOrder, withOwnRowOrder, type MyTaskViewOrder } from "./myTasksViewOrder";
import { myTasksDropGroupOf, type MyTasksPlacing } from "./myTasksDropGroup";
import styles from "./MyTasksPage.module.css";

const TITLE = "my-tasks-title", STATUS = "my-tasks-status", ASSIGNEE = "my-tasks-assignee", DUE = "my-tasks-due", SOURCE = "my-tasks-source";
const GROUPS = ["todo", "inProgress", "complete"] as const;
const MY_TASKS_TAB_ICON = "checkmark-square:lightgray", MY_TASKS_PATH = "/my-tasks";
const GROUP_COLORS = { todo: "default", inProgress: "blue", complete: "green" } as const;

type Group = (typeof GROUPS)[number];
const groupOf = (task: DatabaseMyTask): Group =>
  (GROUPS as readonly string[]).includes(task.statusGroup) ? task.statusGroup as Group : task.done ? "complete" : "todo";

// An option's id is never a group's, so that a filter on "todo" reads the whole group.
const statusOptionOf = (task: DatabaseMyTask) => `${groupOf(task)}|${task.statusName}`;
const groupOfOption = (option: string) => option.split("|")[0];
const nameOfOption = (option: string) => option.slice(option.indexOf("|") + 1);

/**
 * Whether a status written over the one a task shows changes it: a named
 * option — chosen, a card moved to its column, or the one a task had, put
 * back by ⌘Z — when it is another; a group's own when it is another group.
 */
const statusChanges = (written: string, showing: string) =>
  nameOfOption(written) ? written !== showing : groupOfOption(written) !== groupOfOption(showing);

/** What sets a task's status to an option of this page's: its group, and its name — the group's first option for a group's own. */
const statusWrite = (option: string, requestId: string) => {
  const group = groupOfOption(option), statusName = nameOfOption(option);
  return { requestId, done: group === "complete", group, ...(statusName ? { statusName } : {}) };
};

/**
 * A task as the view draws it — one read, or one made here as the server
 * will read it back (myTaskMadeHere.ts) — with what this device changed and
 * hasn't read back yet over it (`shown`).
 */
const taskRow = (task: DatabaseMyTask, sourceId: string, position: string, shown?: GraphRow["values"]): GraphRow => ({
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
function withAssigneeAsMe(view: GraphView, me: string): GraphView {
  const filters = view.config.quickFilters;
  if (!Array.isArray(filters)) return view;
  const isMe = (filter: { propertyId?: unknown; value?: unknown }) => filter.propertyId === ASSIGNEE
    && Array.isArray(filter.value) && filter.value.length === 1 && filter.value[0] === me;
  if (!filters.some(isMe)) return view;
  return { ...view, config: { ...view.config, quickFilters: filters.map((filter) => isMe(filter) ? { ...filter, value: [currentUserFilterValue()] } : filter) } };
}

/** Its tab's check is Notion's light grey (seen 2026-11-15: #7f7f7f on dark), a view kept with the earlier grey too. */
const withTabIcon = (view: GraphView): GraphView =>
  view.config.icon === "checkmark-square:gray" ? { ...view, config: { ...view.config, icon: MY_TASKS_TAB_ICON } } : view;

/** A task's source, as Notion's row shows it (seen 2026-11-08): its database's icon and name, underlined as a link. */
function MyTaskSourceValue({ task }: { task: DatabaseMyTask }) {
  return (
    <span className={styles.source} data-my-task-source-value>
      <MyTaskDatabaseIcon icon={task.databaseIcon} iconType={task.databaseIconType} />
      <span className={styles.sourceName}>{task.databaseTitle}</span>
    </span>
  );
}

/**
 * "내 작업" as Notion's page shows it (seen 2026-11-05): a database view of its
 * own over the sources — a list by default, its layout, properties, filters,
 * sorts, groups and order of tasks the view's, more views beside it, kept for the person. Its
 * status is the three groups, whatever a source calls them, and what is
 * changed here goes to the task's own database.
 */
export function MyTasksDatabaseView({ session, workspaceId, tasks, sources, newRowControl, emptyRows = null, sourceSettings, settingsFoot, widgetHead = null, onLockControls, onFavorite, onOpenRow, onChanged, onTaskMade }: {
  session: Session;
  workspaceId: string;
  tasks: readonly DatabaseMyTask[];
  sources: readonly DatabaseMyTaskSource[];
  newRowControl: ReactNode;
  /** What its list shows with no task to show (none while it has no source, the page's own "출처 없음" standing there). */
  emptyRows?: ((addRow?: () => void) => ReactNode) | null;
  /** The view's settings' "작업 출처", in place of a database's rows, and "작업이 보이지 않나요?" after its lock. */
  sourceSettings: ReactNode;
  settingsFoot: ReactNode;
  /**
   * Home's widget: its first view alone, its tools in the widget's head and
   * "전체 페이지로 열기" to the page, as Notion's Home "내 작업" (seen 2026-11-24).
   */
  widgetHead?: HTMLElement | null;
  /** The lock and the star, for the page's top bar to show as Notion's does (seen 2026-11-05). */
  onLockControls(controls: DatabaseLockControls): void;
  onFavorite(favorite: boolean, change: (favorite: boolean) => void): void;
  onOpenRow(rowId: string): void;
  onChanged(): void;
  onTaskMade(task: DatabaseMyTask): void;
}) {
  const { t } = useTranslation("myTasks");
  const { t: tRow } = useTranslation("rowMenu");
  const { notify } = useProductToast();
  const sourceId = `my-tasks:${workspaceId}`;
  const source = useMemo<GraphDataSource>(() => ({ id: sourceId, title: t("title") }), [sourceId, t]);
  const me = session.user.id;
  // Each group has its own option and its sources' statuses' options, whose names their chips show (Notion's, seen 2026-11-05);
  // a group's own stands for it, so a filter lists the group alone, and a view by option, a pick neither (statusOptionsByOption).
  // The options come in Notion's order whatever order tasks are read or sources picked in (seen 2026-11-24, its board
  // and grouping by option, two ways): the oldest database's first, each in its own order in its group, one named alike
  // once; then any a task carries that its source's status no longer lists.
  const statusConfig = useMemo(() => {
    const named = new Map<string, { id: string; name: string; color: string; group: Group }>();
    const gather = (group: Group, name: string, color: string) => {
      const id = `${group}|${name}`;
      if (name && !named.has(id)) named.set(id, { id, name, color: color || GROUP_COLORS[group], group });
    };
    const oldestFirst = [...sources].sort((left, right) =>
      (left.createdAt ?? 0) - (right.createdAt ?? 0) || left.dataSourceId.localeCompare(right.dataSourceId));
    for (const source of oldestFirst) for (const option of source.statusOptions ?? []) gather(option.group, option.name, option.color);
    for (const task of tasks) gather(groupOf(task), task.statusName, task.statusColor);
    // Each task's status is picked from its own database's options, in its order (statusRowOptions.ts); a checkbox's lists none.
    const own = new Map(sources.map((source) => [source.dataSourceId, (source.statusOptions ?? []).map(({ group, name }) => `${group}|${name}`)]));
    const rowOptionIds = Object.fromEntries(tasks.flatMap((task): Array<[string, string[]]> => own.get(task.dataSourceId)?.length ? [[task.rowId, own.get(task.dataSourceId)!]] : []));
    return {
      rowOptionIds,
      options: [...GROUPS.map((id) => ({ id: `${id}|`, name: t(`group.${id}`), color: GROUP_COLORS[id], standsForGroup: true })), ...named.values()],
      statusGroups: GROUPS.map((id) => ({ id, name: t(`group.${id}`), color: GROUP_COLORS[id],
        optionIds: [`${id}|`, ...[...named.values()].filter((option) => option.group === id).map((option) => option.id)] })),
    };
  }, [sources, t, tasks]);
  const properties = useMemo<GraphProperty[]>(() => [
    { id: TITLE, dataSourceId: sourceId, type: "title", name: t("taskName"), position: "a0", config: {} },
    { id: STATUS, dataSourceId: sourceId, type: "status", name: t("status"), position: "a1", config: statusConfig },
    { id: ASSIGNEE, dataSourceId: sourceId, type: "person", name: t("assignee"), position: "a2", config: {} },
    { id: DUE, dataSourceId: sourceId, type: "date", name: t("dueDate"), position: "a3", config: {} },
    { id: SOURCE, dataSourceId: sourceId, type: "rich_text", name: t("source"), position: "a4", config: {} },
  ], [sourceId, statusConfig, t]);
  const latestProperties = useRef(properties);
  latestProperties.current = properties;
  // A task without a title goes by its own database's default template's name.
  useMemo(() => rememberRowUntitledNames(tasks.map((task) => ({ id: task.rowId, untitledName: task.untitledName }))), [tasks]);
  // Tasks sent to the trash leave the list at the press (user decision
  // 2026-11-12: a press answers on the next frame — they stayed until the
  // server had trashed each), until the list read again leaves them out.
  const [gone, setGone] = useState<ReadonlySet<string>>(() => new Set());
  useEffect(() => setGone((current) => {
    const kept = [...current].filter((id) => tasks.some(({ rowId }) => rowId === id));
    return kept.length === current.size ? current : new Set(kept);
  }), [tasks]);
  // What is changed here shows at once, before the server has it (useTaskEditsShownAtOnce).
  const { shown: shownEdits, write: writeShown } = useTaskEditsShownAtOnce(tasks, onChanged);
  const rows = useMemo<GraphRow[]>(() => tasks.filter(({ rowId }) => !gone.has(rowId))
    .map((task, index) => taskRow(task, sourceId, `a${String(index).padStart(6, "0")}`, shownEdits.get(task.rowId)?.values)), [gone, shownEdits, sourceId, tasks]);
  const firstView = useMemo<GraphView>(() => ({
    id: `${sourceId}:list`, dataSourceId: sourceId, containerId: sourceId, type: "list", name: t("viewName"), position: "a0",
    config: {
      // Its tab's checkbox, and its filters folded until the filter button opens them (Notion's, seen 2026-11-08).
      icon: MY_TASKS_TAB_ICON, filterBarHidden: true,
      statusCheckboxPropertyId: STATUS, tasksAcrossSources: true, visibleProperties: [DUE, STATUS, SOURCE], propertyOrder: [TITLE, DUE, STATUS, ASSIGNEE, SOURCE],
      // Not grouped, as a new person's in a new workspace (Notion's, seen 2026-11-15 three times in two tabs: "그룹화" empty);
      // the status grouping seen in a shared workspace that day was that view's own, set by someone.
      sorts: [{ propertyId: DUE, direction: "asc" }],
      quickFilters: [
        { propertyId: ASSIGNEE, operator: "contains", value: [currentUserFilterValue()] },
        { propertyId: STATUS, operator: "equals", value: ["todo", "inProgress"] },
      ],
    },
  }), [sourceId, t]);
  const [views, setViews] = useState<readonly GraphView[]>([firstView]);
  // A view's link opens that view (보기 링크 복사).
  const [activeId, setActiveId] = useState(() => new URLSearchParams(window.location.search).get("v") ?? firstView.id);
  useEffect(() => {
    const controller = new AbortController();
    void readMyTaskViews<GraphView>(session, workspaceId, controller.signal)
      .then((saved) => { if (saved.views.length) setViews(saved.views.map((view) => withTabIcon(withAssigneeAsMe(view, me)))); }, () => undefined);
    return () => controller.abort();
  }, [me, session, workspaceId]);
  const latestViews = useRef(views);
  latestViews.current = views;
  // Each view shows its tasks in its own order (myTasksViewOrder.ts), those it never placed newest first; it is saved with one once the screen gave it one.
  const taskIds = useMemo(() => taskIdsNewestFirst(tasks), [tasks]);
  const latestTaskIds = useRef(taskIds);
  latestTaskIds.current = taskIds;
  const shownViews = useMemo(() => views.map((view) => withOwnRowOrder(view, taskIds)), [taskIds, views]);
  // An order the server can't keep is told and the view keeps the one it had (fitMyTaskViews). A drop writes down its group's tasks
  // (myTasksDropGroup.ts) with what the move (`placing`) sets, tasks made here and not yet read among them (`made`, `remember`).
  const made = useRef(new Map<string, GraphRow>()), remember = useCallback((row: GraphRow) => (made.current.set(row.id, row), row), []);
  const saveViews = useCallback(async (next: readonly GraphView[], placing?: MyTasksPlacing) => {
    const taskIds = latestTaskIds.current, was = latestViews.current, groupOf = myTasksDropGroupOf({ properties: latestProperties.current,
      currentUserId: me, pageId: sourceId, workspaceId }, latestRows.current, made.current.values(), placing?.values);
    const { views: kept, refused } = fitMyTaskViews(next.map((view) => ownOrderKept(view, was.find(({ id }) => id === view.id), taskIds, groupOf, placing?.ids)), was, taskIds);
    if (refused) notify(t("orderFull"), "error");
    latestViews.current = kept;
    setViews(kept);
    await saveMyTaskViews(session, workspaceId, kept);
  }, [me, notify, session, sourceId, t, workspaceId]);
  const activeView = (widgetHead ? undefined : shownViews.find((view) => view.id === activeId)) ?? shownViews[0];
  // "보기 잠금" keeps its views as they are; its tasks stay editable, as a database's lock.
  const locked = views[0]?.config.viewsLocked === true;
  const [unlockedForMe, setUnlockedForMe] = useState(false);
  const lockControls = useMemo<DatabaseLockControls>(() => {
    const setLocked = (next: boolean) => {
      setUnlockedForMe(false);
      void saveViews(latestViews.current.map((view, index) => index ? view : { ...view, config: { ...view.config, viewsLocked: next || undefined } }));
    };
    return { locked, page: true, views: true, unlockedForMe, lock: () => setLocked(true), unlockForEveryone: () => setLocked(false),
      unlockForMe: () => setUnlockedForMe(true), relock: () => setUnlockedForMe(false) };
  }, [locked, saveViews, unlockedForMe]);
  useEffect(() => onLockControls(lockControls), [lockControls, onLockControls]);
  const favorite = views[0]?.config.myTasksFavorite === true;
  useEffect(() => onFavorite(favorite, (next) => {
    void saveViews(latestViews.current.map((view, index) => index ? view : { ...view, config: { ...view.config, myTasksFavorite: next || undefined } }))
      .then(formFavoritesChanged);
  }), [favorite, onFavorite, saveViews]);
  const block = useMemo<GraphBlock>(() => ({
    id: sourceId, parentId: null, afterId: null, position: "a0", type: "inline_database",
    content: { dataSourceId: sourceId, viewId: activeView.id },
  }), [activeView.id, sourceId]);
  // Its status filter's list opens the options of the databases a task the view shows is in (statusFilterFolded).
  const shownKey = useMemo(() => JSON.stringify([...new Set(projectRows({ currentUserId: me, pageId: sourceId, properties, rows, view: activeView, workspaceId })
    .flatMap((row) => statusConfig.rowOptionIds[row.id] ?? []))]), [activeView, me, properties, rows, sourceId, statusConfig, workspaceId]);
  const shownProperties = useMemo(() => properties.map((property) => property.id === STATUS
    ? { ...property, config: { ...property.config, shownOptionIds: JSON.parse(shownKey) as string[] } } : property), [properties, shownKey]);
  const graph = useMemo(() => ({ properties: shownProperties, rows }), [shownProperties, rows]);
  const byRow = useMemo(() => new Map(tasks.map((task) => [task.rowId, task])), [tasks]);
  const latestTasks = useRef(byRow);
  latestTasks.current = byRow;
  const ownValue = useCallback((rowId: string, propertyId: string) => {
    const task = propertyId === SOURCE ? byRow.get(rowId) : undefined;
    return task ? <MyTaskSourceValue task={task} /> : null;
  }, [byRow]);
  // A new task goes to a source the reader may add to; with none, no "+ 새 작업" offers one.
  const addable = sources.some((candidate) => candidate.canEdit);

  const viewAdapter = useMemo<NativeDatabaseViewProductAdapter>(() => ({
    changeTitle() {}, changeIcon() {}, changeDescription() {}, changeCover() {}, showRowContent() {},
    selectView: (_block, view) => setActiveId(view.id),
    setLocked() {}, setArchived() {}, setLinkedPresentation() {},
    createView: async (_block, _source, type: GraphViewType, _isCurrent, from) => {
      const view: GraphView = { id: newId(), dataSourceId: sourceId, containerId: sourceId, type, name: from?.name ?? t("viewName"),
        position: `a${latestViews.current.length}`, config: { ...(from ? ownOrderKept(from, latestViews.current.find(({ id }) => id === from.id), latestTaskIds.current).config : firstView.config) } };
      await saveViews([...latestViews.current, view]);
      setActiveId(view.id);
      return view;
    },
    updateViews: async (next) => {
      const changed = new Map(next.map((view) => [view.id, view]));
      await saveViews(latestViews.current.map((view) => changed.get(view.id) ?? view));
    },
    deleteView: async (view) => {
      if (latestViews.current.length < 2) return;
      await saveViews(latestViews.current.filter((candidate) => candidate.id !== view.id));
      setActiveId((current) => current === view.id ? latestViews.current[0].id : current);
    },
    restoreView: async (view) => { await saveViews([...latestViews.current, view]); },
    addDashboardWidget: async () => null, duplicateDashboardWidget: async () => null,
    retypeDashboardWidget: async () => {}, removeDashboardWidget: async () => null, arrangeDashboard: async () => {},
    turnViewIntoDashboard: async () => false, turnDashboardInto: async () => false,
  }), [firstView.config, saveViews, sourceId, t]);

  // What a row's cells change goes to its task: the status as its option (its group's first for a group's own), the name and date to their own properties.
  // Each change is told from the task as it shows, a change of this device's not yet read back included.
  const latestShown = useRef(shownEdits);
  latestShown.current = shownEdits;
  const latestRows = useRef(rows);
  latestRows.current = rows;
  // Each change made here is a step of the page's ⌘Z (useMyTasksUndo), put back through this same way.
  const recordStep = useMyTasksUndo(sourceId);
  // A group's own set over a task — its check, a card moved to a group's column — shows at once as the option its own
  // database gives it there, its group's first (myTaskStatus.optionIn), as Notion's: checked off, a task stood under its
  // database's first "완료" option, not the first of all its sources' (seen 2026-11-24 in two tabs, "Done" not "완료").
  const ownOption = useCallback((dataSourceId: string, option: string) => {
    const group = groupOfOption(option);
    const first = nameOfOption(option) ? undefined : sources.find((candidate) => candidate.dataSourceId === dataSourceId)?.statusOptions
      ?.find((candidate) => candidate.group === group);
    return first ? `${group}|${first.name}` : option;
  }, [sources]);
  // `alongside`: what the same press changed besides the task — its place in the view — taken back and made again with it.
  const commitRow = useCallback((row: GraphRow, stepped = false, alongside?: MyTasksStep) => {
    const read = latestTasks.current.get(row.id);
    if (!read) return;
    const showing = latestShown.current.get(row.id)?.values ?? {};
    const shownDue = showing[DUE]?.value as { start?: string; end?: string } | null | undefined;
    const task: DatabaseMyTask = {
      ...read,
      ...(showing[TITLE] ? { title: String(showing[TITLE].value ?? "") } : {}),
      ...(showing[DUE] ? { dueStart: shownDue?.start ?? "", dueEnd: shownDue?.end ?? "" } : {}),
    };
    // Each write kept in the outbox behind the task's making, on the page holding its database (rowValuesKept.ts).
    const steps: Array<() => Promise<unknown>> = [], holder = sources.find(({ dataSourceId }) => dataSourceId === read.dataSourceId)?.pageId;
    const shown: GraphRow["values"] = {};
    // The status goes as the option itself, so ⌘Z puts back the one the task
    // had, not its group's first (useMyTasksUndo; a check's group's own goes as ownOption says).
    const asked = row.values[STATUS]?.value;
    const status = typeof showing[STATUS]?.value === "string" ? showing[STATUS].value : statusOptionOf(read);
    if (typeof asked === "string" && statusChanges(asked, status)) {
      const written = ownOption(read.dataSourceId, asked);
      const input = statusWrite(written, newId());
      shown[STATUS] = { type: "status", value: written };
      steps.push(() => sendTaskStatus(session, workspaceId, task.rowId, input, holder));
    }
    const values: Parameters<typeof sendRowValues>[2]["values"] = {};
    const title = row.values[TITLE]?.value;
    if (typeof title === "string" && title !== task.title && task.titlePropertyId) {
      values[task.titlePropertyId] = { type: "title", value: title };
      shown[TITLE] = row.values[TITLE];
    }
    // A date cell writes "start", "start/end" or an object with its reminder.
    // A row that carries no date cell — a ⌘Z or ⌘⇧Z step's, which carries the
    // cells it changed alone — leaves the task's date as it is.
    const dueWritten = row.values[DUE]?.value as string | { start?: string; end?: string } | null | undefined;
    const [start, end] = typeof dueWritten === "string" ? dueWritten.split("/") : [];
    const due = typeof dueWritten === "string" ? { start, ...(end ? { end } : {}) } : dueWritten;
    if (DUE in row.values && ((due?.start ?? "") !== task.dueStart || (due?.end ?? "") !== task.dueEnd)) {
      if (task.dueDatePropertyId) {
        values[task.dueDatePropertyId] = { type: "date", value: due ?? null };
        shown[DUE] = { type: "date", value: due?.start ? due : null };
      }
    }
    if (Object.keys(values).length) {
      const mutationId = newId();
      steps.push(() => sendRowValues(session, task.rowId, { mutationId, values }, holder));
    }
    if (!stepped && (steps.length || alongside)) {
      // What the changed cells showed before, to put back, and them again — the
      // changed cells alone, so a step leaves the task's other values as they
      // are then; the task leaves and comes back with its status as Notion's does,
      // a card moved to another group on a board too (a task database's board,
      // seen 2026-11-24 in two tabs: ⌘Z put the card back, its other values as
      // they were, and ⌘⇧Z moved it again), in its place there.
      const before = latestRows.current.find(({ id }) => id === row.id)?.values ?? {};
      const back: GraphRow = { ...row, values: Object.fromEntries(Object.keys(shown).map((key) => [key, before[key]])) };
      const again: GraphRow = { ...row, values: { ...shown } };
      const changed = steps.length > 0;
      recordStep({
        back: () => { if (changed) commitRowRef.current(back, true); alongside?.back(); },
        again: () => { if (changed) commitRowRef.current(again, true); alongside?.again(); },
      });
    }
    writeShown(row.id, shown, steps);
  }, [ownOption, recordStep, session, sources, workspaceId, writeShown]);
  const commitRowRef = useRef(commitRow);
  commitRowRef.current = commitRow;
  // A task moved in a view's own order: the view saved with it, and the order it had to put back with ⌘Z — no step when it wasn't kept.
  const placeInOrder = useCallback((placed: GraphView, placing: MyTasksPlacing) => {
    const orderOf = () => orderKept(latestViews.current.find(({ id }) => id === placed.id)?.config), was = JSON.stringify(orderOf());
    const setOrder = (order: MyTaskViewOrder) => void saveViews(latestViews.current.map((view) => view.id === placed.id ? { ...view, config: withOrder(view.config, order) } : view));
    const before = orderAsList(latestViews.current.find(({ id }) => id === placed.id)?.config, latestTaskIds.current);
    const saved = saveViews(latestViews.current.map((view) => view.id === placed.id ? placed : view), placing), after = orderOf();
    const step: MyTasksStep | undefined = JSON.stringify(after) === was ? undefined : { back: () => setOrder(before), again: () => setOrder(after) };
    return { step, saved };
  }, [saveViews]);
  const tableAdapter = useMemo(() => ({
    // "+ 새 작업" makes it in the source added last, as Notion's (seen 2026-11-05) — the last one the reader may add to.
    createRow: async (_source: GraphDataSource, _parent: string | null, _after: string | null, isCurrent: () => boolean) => {
      const target = [...sources].reverse().find((candidate) => candidate.canEdit);
      if (!target) return null;
      // Made on this device first (myTaskMadeHere.ts).
      const task = await makeMyTask(session, workspaceId, target);
      onTaskMade(task);
      if (!isCurrent()) return null;
      // Drawn as the task the list shows (taskRow), its database's default template's title and icon with it.
      return remember(taskRow(task, sourceId, "z"));
    },
    publishCreatedRow: (row: GraphRow) => row,
    // Shown at once and sent behind (useTaskEditsShownAtOnce).
    commitRows: async (next: readonly GraphRow[]) => { next.forEach((row) => commitRow(row)); },
    // A list's, table's or gallery's tasks moved in the view's own order, a step of ⌘Z; one dropped in
    // another status's group takes its status in that step, as a board's card (commitRow).
    moveRows: async (_rows: readonly GraphRow[], view: GraphView | undefined, rowIds: readonly string[], _isCurrent: () => boolean,
      values?: Readonly<Record<string, GraphRow["values"]>>) => {
      if (!view) return;
      const placed = placeInOrder(view, { ids: rowIds, values });
      const [moved] = Object.entries(values ?? {}).flatMap(([rowId, set]) => latestRows.current.filter(({ id }) => id === rowId).map((row) => ({ ...row, values: set })));
      if (moved) commitRow(moved, false, placed.step); else if (placed.step) recordStep(placed.step);
      await placed.saved;
    },
    restoreValues: async () => false,
    commitProperties: async () => {},
    deleteProperties: async () => {},
    deleteRows: async (ids: readonly string[]) => {
      // Each by the page holding its task's database, kept for a server out of reach (rowTrashKept.ts); refused, they are back.
      const pageOf = (id: string) => sources.find(({ dataSourceId }) => dataSourceId === latestRows.current.find((row) => row.id === id)?.dataSourceId)?.pageId;
      const send = (trash: boolean) => Promise.all(ids.map((id) => sendRowTrash(session, [id], trash, { pageId: pageOf(id) })));
      const back = () => setGone((current) => new Set([...current].filter((id) => !ids.includes(id))));
      setGone((current) => new Set([...current, ...ids]));
      await send(true).catch((error: unknown) => { back(); throw error; }).finally(onChanged);
      // "휴지통으로 이동됨 · 복원하기", as Notion's (seen 2026-11-15, a task's menu).
      notify(tRow("toast.movedToTrash"), "success", { label: tRow("trashDatabase.restore"), onClick: () => {
        back();
        void send(false).catch(() => notify(tRow("toast.couldntRestorePage"), "error")).finally(onChanged);
      } });
    },
    duplicateRows: async () => [],
    // What a task's menu changed shows once the tasks are read again.
    rowsChanged: onChanged,
    updateView: async (view: GraphView) => { await saveViews(latestViews.current.map((candidate) => candidate.id === view.id ? view : candidate)); },
  }) as unknown as NativeTableProductAdapter, [commitRow, notify, onChanged, onTaskMade, placeInOrder, recordStep, remember, saveViews, session, sourceId, sources, tRow, workspaceId]);

  // A board's cards move between the status groups; a new card goes where the "+ 새 작업" line's does, in its group.
  const boardAdapter = useMemo<NativeBoardProductAdapter>(() => ({
    addProperty: async () => null,
    // A card's move is a step of the page's own ⌘Z (commitRow), none of a page editor's.
    stepMove: async () => false,
    createRow: async (_source, propertyId, value) => {
      const target = [...sources].reverse().find((candidate) => candidate.canEdit);
      if (!target) return null;
      // Made on this device first (myTaskMadeHere.ts); another status than its first shows at once and is set once
      // the server has it (useTaskEditsShownAtOnce), the card standing in its column throughout; the tasks are read
      // again after that, a read before then missing it (useMyTasksRead).
      const task = await makeMyTask(session, workspaceId, target);
      // A column of an option gives the card that option, one of a group its database's first there (ownOption).
      const option = propertyId === STATUS && typeof value === "string" && statusChanges(value, statusOptionOf(task)) ? ownOption(task.dataSourceId, value) : null;
      onTaskMade(task);
      const input = option ? statusWrite(option, newId()) : null;
      if (option && input) writeShown(task.rowId, { [STATUS]: { type: "status", value: option } }, [() => sendTaskStatus(session, workspaceId, task.rowId, input, target.pageId)]);
      else void afterPageMade(task.rowId).then(onChanged, onChanged);
      return remember(taskRow(task, sourceId, "z", option ? { [STATUS]: { type: "status", value: option } } : undefined));
    },
    moveRow: async (rowId, change) => {
      const row = latestRows.current.find(({ id }) => id === rowId);
      if (!row || change.propertyId !== STATUS || typeof change.value !== "string") return;
      // The card stands in its new group and place at once, as a database's board's, its
      // status alone changed as a cell's is — one step of ⌘Z with its place (commitRow).
      const placed = change.view ? placeInOrder(change.view, { ids: [rowId], values: { [rowId]: { [STATUS]: { type: "status", value: change.value } } } }) : undefined;
      commitRow({ ...row, values: { [STATUS]: { type: "status", value: change.value } } }, false, placed?.step);
      await placed?.saved;
    },
    updateOptions: async () => {},
    updateView: async (view) => { await saveViews(latestViews.current.map((candidate) => candidate.id === view.id ? view : candidate)); },
  }), [commitRow, onChanged, onTaskMade, ownOption, placeInOrder, remember, saveViews, session, sourceId, sources, workspaceId, writeShown]);

  return (
    <DatabaseSourceSettingsContext.Provider value={sourceSettings}>
    <DatabaseSettingsFootContext.Provider value={settingsFoot}>
    <DatabaseLockContext.Provider value={lockControls}>
    <DatabaseStructureLockContext.Provider value={locked && !unlockedForMe}>
    <DatabaseNewRowControlContext.Provider value={newRowControl}>
    <DatabaseEmptyRowsContext.Provider value={emptyRows}>
    <DatabaseRowsAddableContext.Provider value={addable}>
    <DatabaseSearchableContext.Provider value={sources.length > 0}>
    <DatabaseOwnValueContext.Provider value={ownValue}>
      <DatabaseOwnPageContext.Provider value={!widgetHead}><DatabaseFullPageLinkContext.Provider value={widgetHead ? MY_TASKS_PATH : null}>
        <NativeDatabaseViewMount block={block} source={source} displayTitle={t("title")} dataSources={[source]} filterGraph={graph}
          views={widgetHead ? [activeView] : shownViews} activeView={activeView} properties={shownProperties} rows={rows} databaseViewAdapter={viewAdapter} tableAdapter={tableAdapter} boardAdapter={boardAdapter}
          pageId={sourceId} workspaceId={workspaceId} session={session} embedded={Boolean(widgetHead)} widgetHead={widgetHead ? { slot: widgetHead, mode: "home" } : null}
          onOpenRow={(rowId) => onOpenRow(rowId)} listOnly="tasks" configurationReady={false} />
      </DatabaseFullPageLinkContext.Provider></DatabaseOwnPageContext.Provider>
    </DatabaseOwnValueContext.Provider>
    </DatabaseSearchableContext.Provider>
    </DatabaseRowsAddableContext.Provider>
    </DatabaseEmptyRowsContext.Provider>
    </DatabaseNewRowControlContext.Provider>
    </DatabaseStructureLockContext.Provider>
    </DatabaseLockContext.Provider>
    </DatabaseSettingsFootContext.Provider>
    </DatabaseSourceSettingsContext.Provider>
  );
}
