import { readDatabaseRowPage, setDatabaseRowPagePresentation, writeDatabaseRowPageValues } from "@/databaseRowPageApi";
import { databaseRowPageErrorCodes, type DatabaseRowPageActionResponse } from "@/databaseRowPageWire.generated";
import { markMyTaskDone } from "@/databaseToolbarConfigApi";
import type { DatabaseMyTaskResult } from "@/databaseToolbarConfigWire.generated";
import type { Session } from "@/types";
import { contentGraphOutboxOf } from "./contentGraphSend";
import { newId } from "./ids";
import { keptChangeRoutes } from "./keptChangeRoutes";
import { whenKeptChangeWent } from "./keptChangeWent";
import type { NativeRowValues } from "./nativePageLaneEntries";
import { pageRowMadeOn, rowNotYetMade, whenRowSaved } from "./rowMadeHere";

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
// row, and a reload took it).

/** A row page's values write as its route takes it; its ID, if given, the kept change's. */
export type RowValuesWrite = NonNullable<NativeRowValues["input"]> & { mutationId?: string };
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
  signal?: AbortSignal): Promise<DatabaseRowPageActionResponse | null> {
  const { mutationId = newId(), ...input } = write;
  return keepRowWrite(session, { rowId, input }, mutationId, pageId, signal) as Promise<DatabaseRowPageActionResponse | null>;
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
  if (!outbox || !place) return sendKeptRowValues(session, { mutationId, rowValues }, signal, false);
  const delivery = await outbox.admitAndSendGraph({ mutationId, operations: [], pageId: place, rowValues }, signal);
  if (delivery.state === "committed") return delivery.result;
  if (!(await whenKeptChangeWent(outbox, place, mutationId))) throw new Error("The row's kept values were refused.");
  return null;
}

/**
 * Sends a row's kept values by its row page's route, or a task's status by 내 작업's (the outbox's routes for them), under the kept
 * change's ID; `kept` false for values the outbox doesn't keep, sent again as the page goes by their own request (writesAsPageGoes.ts).
 */
export function sendKeptRowValues(session: Session, { mutationId, rowValues }: { mutationId: string; rowValues: NativeRowValues },
  signal?: AbortSignal, kept = true): Promise<unknown> {
  const { rowId, input, taskStatus, presentation } = rowValues;
  if (taskStatus) {
    const { workspaceId, ...status } = taskStatus;
    return markMyTaskDone(session, workspaceId, rowId, { ...status, requestId: mutationId }, signal);
  }
  if (presentation) return writePresentationAsHeld(session, rowId, presentation, signal);
  return writeDatabaseRowPageValues(session, rowId, { values: {}, ...input, mutationId }, signal, kept);
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
