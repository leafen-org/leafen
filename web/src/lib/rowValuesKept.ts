import { readDatabaseRowPage, setDatabaseRowPagePresentation, writeDatabaseRowPageValues } from "@/databaseRowPageApi";
import { databaseRowPageErrorCodes, type DatabaseRowPageActionResponse, type DatabaseRowPageValue } from "@/databaseRowPageWire.generated";
import { markMyTaskDone } from "@/databaseToolbarConfigApi";
import type { DatabaseMyTaskResult } from "@/databaseToolbarConfigWire.generated";
import type { Session } from "@/types";
import { contentGraphOutboxOf } from "./contentGraphSend";
import { newId } from "./ids";
import { keptChangeRoutes } from "./keptChangeRoutes";
import { whenKeptChangeWent } from "./keptChangeWent";
import type { NativeRowValues } from "./nativePageLaneEntries";
import { pageRowMadeOn, rowNotYetMade, whenRowSaved } from "./rowMadeHere";
import { isRowValueChanged, rowValueNow } from "./rowTitleSave";
import { sameTitle, type TitleValue } from "./titleMerge";
import { sendTitleOverBase } from "./typedTitleSend";
import { typedRowChange } from "./typedDraftSender";

// A row's values written through its own page's route — a row page opened
// alone (StandaloneRowPage: a task's side peek in 내 작업, a sidebar row, the
// row at its own address), a sidebar row's name, a task's title, date and
// status in 내 작업, a table title written over the one read, a row page's
// icon, cover and style — go to the server this one way: kept in the outbox
// as a change of the page holding the row's database,
// behind the changes kept before it (the row's making among them), and sent
// by the row page's route in turn, as the database's own changes are; one on
// its way as the page goes doesn't go ahead of an older one
// (nativePageMutationLeaving.ts). Whether a send that failed waits for the
// server or was refused is the outbox's one rule (graphWriteWaitsForServer)
// (2026-11-25: sent straight to the server, a title typed in a new task's
// side peek while its making waited for the server waited in the tab for the
// row, and a reload took it). A title or a text typed goes so too, under the
// ID its typing was kept under on the device (typedDraftSender.ts), and is
// merged as it goes with the value the server holds then (sendKeptRowValues).

/** A row page's values write as its route takes it; its ID, if given, the kept change's. */
export type RowValuesWrite = NonNullable<NativeRowValues["input"]> & { mutationId?: string };
/** The server's answer to a row's values, with the value written in place of the one typed when it was merged with another (sendKeptRowValues). */
export type RowValuesAnswer = DatabaseRowPageActionResponse & { merged?: Record<string, DatabaseRowPageValue> };
type TaskStatus = NonNullable<NativeRowValues["taskStatus"]>;
type Presentation = NonNullable<NativeRowValues["presentation"]>;

/**
 * Writes a row's values, kept in the outbox as it is called, so writes made
 * one after another go in that order without waiting on each other's answer:
 * settles with the server's answer, or null once they went after being kept
 * for a server out of reach or behind changes the server hadn't taken yet
 * (whenKeptChangeWent) — meanwhile on the device, the page showing them as
 * written. Refused, it throws, and they are gone. `pageId` is the page
 * holding the row's database; a row made here goes on the page its making
 * went to, behind it, once that went to the outbox. Without an outbox, or a
 * page to keep it on, it goes at once.
 */
export async function sendRowValues(session: Session, rowId: string, write: RowValuesWrite, pageId?: string | null,
  signal?: AbortSignal): Promise<RowValuesAnswer | null> {
  // One value typed and kept on the device goes as the change its record names (typedDraftSender.ts).
  const only = Object.keys(write.values), typed = only.length === 1 && !write.mutationId ? typedRowChange(rowId, only[0]) : undefined;
  const { mutationId = typed?.mutationId ?? newId(), ...input } = write;
  const sent = keepRowWrite(session, { rowId, input }, mutationId, pageId, signal) as Promise<RowValuesAnswer | null>;
  typed?.went(sent);
  return sent;
}

/** Sets a task's status as 내 작업 does (markMyTaskDone), kept and sent as a row's values are (sendRowValues). */
export function sendTaskStatus(session: Session, workspaceId: string, rowId: string, input: Omit<TaskStatus, "workspaceId"> & { requestId: string },
  pageId?: string | null): Promise<DatabaseMyTaskResult | null> {
  const { requestId, ...status } = input;
  return keepRowWrite(session, { rowId, taskStatus: { ...status, workspaceId } }, requestId, pageId) as Promise<DatabaseMyTaskResult | null>;
}

/**
 * Gives a row's page an icon, a cover or a style, kept and sent as a row's
 * values are (sendRowValues): written over the row as the server holds it
 * when it goes (sendKeptRowValues), a row made here having no revision to
 * write over before then.
 */
export function sendRowPresentation(session: Session, rowId: string, presentation: Presentation, pageId?: string | null,
  signal?: AbortSignal): Promise<DatabaseRowPageActionResponse | null> {
  return keepRowWrite(session, { rowId, presentation }, newId(), pageId, signal) as Promise<DatabaseRowPageActionResponse | null>;
}

async function keepRowWrite(session: Session, rowValues: NativeRowValues, mutationId: string, pageId?: string | null, signal?: AbortSignal) {
  if (rowNotYetMade(rowValues.rowId) && !pageRowMadeOn(rowValues.rowId)) await whenRowSaved(rowValues.rowId);
  const outbox = contentGraphOutboxOf(session), place = pageRowMadeOn(rowValues.rowId) ?? pageId;
  if (!outbox || !place) return sendKeptRowValues(session, { mutationId, rowValues }, signal);
  const delivery = await outbox.admitAndSendGraph({ mutationId, operations: [], pageId: place, rowValues }, signal);
  if (delivery.state === "committed") return delivery.result;
  if (!(await whenKeptChangeWent(outbox, place, mutationId))) throw new Error("The row's kept values were refused.");
  return null;
}

/**
 * Sends a row's kept values by its row page's route, or a task's status by
 * 내 작업's (the outbox's routes for them), under the kept change's ID. A
 * title or a text written over the value it was typed on (`expected`) meets
 * the server holding another — saved meanwhile elsewhere, or by this
 * device's own write that went already, a tab killed before it heard — and
 * is merged with it and written again over that one, by the one rule a typed
 * title goes by (typedTitleSend.ts), the answer saying what was written
 * (`merged`); the server holding what was typed already, nothing is written.
 */
export function sendKeptRowValues(session: Session, { mutationId, rowValues }: { mutationId: string; rowValues: NativeRowValues },
  signal?: AbortSignal): Promise<unknown> {
  const { rowId, input, taskStatus, presentation } = rowValues;
  if (taskStatus) {
    const { workspaceId, ...status } = taskStatus;
    return markMyTaskDone(session, workspaceId, rowId, { ...status, requestId: mutationId }, signal);
  }
  if (presentation) return writePresentationAsHeld(session, rowId, presentation, signal);
  const write = (values: NonNullable<NativeRowValues["input"]>, id: string) => writeDatabaseRowPageValues(session, rowId, { ...values, mutationId: id }, signal);
  const only = Object.keys(input?.values ?? {}), propertyId = only[0], typed = input?.values[propertyId], base = input?.expected?.[propertyId];
  const words = typed && wordsOf(typed), over = base && wordsOf(base);
  if (!input || only.length !== 1 || !words || !over || Object.keys(input.expected ?? {}).length !== 1 || input.options) {
    return write({ values: {}, ...input }, mutationId);
  }
  let tries = 0;
  const value = (title: TitleValue): DatabaseRowPageValue => ({ type: typed.type, value: title.title,
    ...(title.spans?.length ? { spans: title.spans.map((span) => ({ ...span })) as DatabaseRowPageValue["spans"] } : {}) });
  return sendTitleOverBase<TitleValue, RowValuesAnswer | null>({
    base: over, typed: words, changed: isRowValueChanged, overLast: true,
    readNow: () => rowValueNow(session, rowId, propertyId), titleOf: (now) => now,
    async write(title, now) {
      // Each try after the first goes as a change of its own, the same each time this change is sent again.
      tries += 1;
      const id = tries === 1 ? mutationId : `${mutationId}:${tries}`;
      // The server holding it already, nothing is written; it is shown, when it isn't what was typed.
      if (now && sameTitle(now, title)) return sameTitle(title, words) ? null
        : { rowId, containerPageId: "", authorityRevision: 0, duplicate: true, merged: { [propertyId]: value(title) } };
      const answer: RowValuesAnswer = await write({ values: { [propertyId]: value(title) },
        ...(now === null ? {} : { expected: { [propertyId]: now ? value(now) : base! } }) }, id);
      return tries === 1 ? answer : { ...answer, merged: { [propertyId]: value(title) } };
    },
  });
}

// A title's or a text's words, which a merge takes; none for any other value.
function wordsOf(value: DatabaseRowPageValue): TitleValue | null {
  if ((value.type !== "title" && value.type !== "rich_text") || (value.value !== null && typeof value.value !== "string")) return null;
  return { title: String(value.value ?? ""), ...(value.spans?.length ? { spans: value.spans as TitleValue["spans"] } : {}) };
}

// How many times a look met a row changed since it was read is read and sent again.
const ROW_CHANGED_TRIES = 3;

/**
 * Writes a row page's look over the row as the server holds it now: read
 * first for its revision, and read and sent again when another change
 * landed between the read and the write — the icon picked last stands, as
 * Notion's does (a kept look may go long after the read it was made on).
 */
async function writePresentationAsHeld(session: Session, rowId: string, presentation: Presentation, signal?: AbortSignal) {
  for (let tries = 1; ; tries += 1) {
    const { row } = await readDatabaseRowPage(session, rowId, { limit: 1, signal });
    try {
      return await setDatabaseRowPagePresentation(session, rowId, { ...presentation, expectedAuthorityRevision: row.authorityRevision }, signal);
    } catch (error) {
      const conflict = error && typeof error === "object" && Reflect.get(error, "code") === databaseRowPageErrorCodes.conflict;
      if (!conflict || tries >= ROW_CHANGED_TRIES || signal?.aborted) throw error;
    }
  }
}

// At hand for the outbox as the page goes (keptChangeRoutes.ts).
keptChangeRoutes.rowValues = sendKeptRowValues;
