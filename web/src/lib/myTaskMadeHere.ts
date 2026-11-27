import { compareGraphOrder, type GraphProperty, type GraphRow } from "@/contentGraph";
import { createMyTask } from "@/databaseToolbarConfigApi";
import type { DatabaseMyTask, DatabaseMyTaskSource } from "@/databaseToolbarConfigWire.generated";
import { sendContentGraphChange } from "@/lib/contentGraphSend";
import { readDefaultTemplateWhenMade } from "@/lib/fullPageDatabase";
import { graphPositionBetween } from "@/lib/graphPosition";
import { newId } from "@/lib/ids";
import { localFirstProductCache } from "@/lib/localFirstProductCache";
import { makeRowHere, rowPageMadeHere } from "@/lib/rowMadeHere";
import {
  heldDefaultTemplate, madeFromTemplate, noteDefaultRowTemplate, rowTemplateToMakeFrom, rowTemplateValues, rowWithTemplateIcon,
} from "@/lib/rowTemplateHere";
import { serverIdentity } from "@/lib/serverOrigin";
import type { Session } from "@/types";

/**
 * Notes the default template of each source the person may add to, as the
 * server named it with the sources (withMyTaskSourceMaking), and reads its
 * body ahead, so a task made in one shows that template's title, icon, values
 * and body at once though this device never opened its database
 * (2026-11-25: "새 작업" without them until a read after the server had the task).
 */
export function noteMyTaskSourceTemplates(session: Session, sources: readonly DatabaseMyTaskSource[]) {
  for (const source of sources) if (source.canEdit) noteDefaultRowTemplate(session, source.dataSourceId, source.defaultTemplate);
}

// The tasks made here whose template the server has yet to fill them from, by row.
const filling = new Map<string, Promise<void>>();

/**
 * Settles once the server filled a task made here from its template — its
 * icon a read finds only then, the rest made with it — or won't; at once for
 * any other task.
 */
export function afterMyTaskFilled(rowId: string): Promise<void> {
  return filling.get(rowId) ?? Promise.resolve();
}

/**
 * Makes a task in one of "내 작업"'s sources on this device first, as a
 * database's own new rows are (rowMadeHere.ts): its title empty, the person
 * its assignee, its status the source's first To-do option, after the rows
 * this device keeps of it. It shows and its page opens at once, and it goes
 * to the source's page behind, the server filling it from the default
 * template as it makes it (2026-11-12: on a stalled network "새 작업" waited for the server to
 * make the task, and nothing opened), with that template's values, icon and
 * body at once when this device holds it (noteMyTaskSourceTemplates). Gives
 * the task as "내 작업" lists it once the server has it — the one place a task
 * made here is drawn from: its first To-do option's name and colour, the
 * template's title, icon and due date, and what it is called untitled
 * (2026-11-25: its chip read the group's "할 일" till then) — or null when the
 * source doesn't say what a task takes (an older server), when the server
 * makes it first as before.
 */
export async function makeMyTaskHere(session: Session, workspaceId: string, source: DatabaseMyTaskSource): Promise<DatabaseMyTask | null> {
  if (!source.canEdit || !source.titlePropertyId || !source.assigneePropertyId) return null;
  const me = session.user.id;
  const kept = await localFirstProductCache.graph.readCache({ key: source.pageId, principalId: me, serverKey: serverIdentity() }).catch(() => null);
  const graph = kept?.value.graph;
  const last = graph?.rows.filter((row) => row.dataSourceId === source.dataSourceId && !row.subitemParentId).sort(compareGraphOrder).at(-1);
  // Its database's properties as the sources were read with them (withMyTaskSourceMaking), so its page shows them with its
  // head and body though this device never opened the database (2026-11-25: 60–100 ms after its body); else as kept here.
  const properties: GraphProperty[] = source.properties?.length
    ? source.properties.map(({ access, ...property }) => ({ ...property, dataSourceId: source.dataSourceId, type: property.type as GraphProperty["type"],
      ...(access ? { access: access as GraphProperty["access"] } : {}) }))
    : graph?.properties.filter(({ dataSourceId }) => dataSourceId === source.dataSourceId)
      ?? [{ id: source.titlePropertyId, dataSourceId: source.dataSourceId, type: "title", name: "", position: "a", config: {} }];
  // It is made with its source's default template's values as held here
  // (rowTemplateHere.ts), under its own assignee and status.
  const template = heldDefaultTemplate(session, source.dataSourceId);
  const templateValues = Object.entries(template ? rowTemplateValues(session, template.template, properties, source.dataSourceId) : {})
    .flatMap(([id, value]) => {
      const property = properties.find((candidate) => candidate.id === id);
      return property ? [[id, { type: property.type, value }] as const] : [];
    });
  const title = templateValues.find(([id]) => id === source.titlePropertyId)?.[1].value;
  const row: GraphRow = {
    id: newId(),
    dataSourceId: source.dataSourceId,
    subitemParentId: null,
    // After its rows as kept here, or after any position made so far.
    position: graphPositionBetween(last?.position ?? "~", undefined),
    values: {
      ...Object.fromEntries(templateValues),
      [source.titlePropertyId]: { type: "title", value: typeof title === "string" ? title : "" },
      [source.assigneePropertyId]: { type: "person", value: [me] },
      ...(source.statusPropertyId && source.firstToDoOptionId
        ? { [source.statusPropertyId]: { type: "status", value: source.firstToDoOptionId } } : {}),
    },
  };
  // Its making names the template it starts from, which the server fills it from as it makes it.
  const making = rowTemplateToMakeFrom(session, template?.templateId, { isDefault: true });
  const { pageReady } = makeRowHere(row, me,
    () => sendContentGraphChange(session, source.pageId, [{ kind: "upsertRow", row }]).then(() => true), () => {}, making.template);
  const made = madeFromTemplate(session, row.id, making);
  const shown = rowWithTemplateIcon(row, template?.template);
  rowPageMadeHere(session, shown, properties, source.pageId, workspaceId);
  void readDefaultTemplateWhenMade(session, source.pageId, source.dataSourceId, made);
  const filled = made.filled.then(() => undefined);
  filling.set(row.id, filled);
  void filled.finally(() => filling.delete(row.id));
  await Promise.all([pageReady, made.shown]);
  // The first To-do option is the first of its group the source lists (myTaskStatus.wire), as the server reads it back.
  const status = source.firstToDoOptionId ? source.statusOptions?.find(({ group }) => group === "todo") : undefined;
  const due = source.dueDatePropertyId ? row.values[source.dueDatePropertyId]?.value : undefined;
  const dueRange: { start?: unknown; end?: unknown } = typeof due === "string" ? { start: due } : due && typeof due === "object" ? due : {};
  return {
    rowId: row.id, pageId: source.pageId, dataSourceId: source.dataSourceId, databaseTitle: source.title,
    title: typeof title === "string" ? title : "", icon: shown.icon ?? "", iconType: (shown.iconType ?? "none") as DatabaseMyTask["iconType"],
    databaseIcon: source.icon, databaseIconType: source.iconType, canEdit: true,
    statusName: status?.name ?? "", statusColor: status?.color ?? "", statusGroup: "todo", done: false,
    dueStart: typeof dueRange.start === "string" ? dueRange.start : "", dueEnd: typeof dueRange.end === "string" ? dueRange.end : "",
    titlePropertyId: source.titlePropertyId, dueDatePropertyId: source.dueDatePropertyId, assignees: [me],
    ...(source.untitledName ? { untitledName: source.untitledName } : {}),
    // Made now, so the views that never placed it show it first among those (myTasksViewOrder.ts).
    created: Date.now(),
  };
}

/** A task made on this device first, or by the server first where the source doesn't say what a task takes. */
export async function makeMyTask(session: Session, workspaceId: string, source: DatabaseMyTaskSource): Promise<DatabaseMyTask> {
  return await makeMyTaskHere(session, workspaceId, source)
    ?? (await createMyTask(session, workspaceId, { requestId: newId(), dataSourceId: source.dataSourceId, title: "" })).task;
}
