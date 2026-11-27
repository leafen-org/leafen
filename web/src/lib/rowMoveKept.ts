import { api } from "@/api";
import { moveDatabaseRowPage, readDatabaseRowPage } from "@/databaseRowPageApi";
import { databaseRowPageErrorCodes, type DatabaseRowPageActionResponse } from "@/databaseRowPageWire.generated";
import { LeafenApiError } from "@/apiTransport";
import type { Session } from "@/types";
import { contentGraphOutboxOf } from "./contentGraphSend";
import { newId } from "./ids";
import { keptChangeRoutes } from "./keptChangeRoutes";
import { whenKeptChangeWent } from "./keptChangeWent";
import type { NativeRowMove } from "./nativePageLaneEntries";
import { pageLeaving } from "./pageLeaving";
import { pageRowMadeOn, whenRowSaved } from "./rowMadeHere";

// A row's page moved — out under a page or to the top of a place, or into
// another database — goes to the server this one way, as rows going to the
// trash do (rowTrashKept.ts): kept in the outbox as a change of the page
// holding its database, behind the changes kept before it, and sent by the
// row's own move route in turn. Whether a send that failed waits for the
// server or was refused is the outbox's one rule (graphWriteWaitsForServer)
// (2026-11-27: sent straight from the window after reading the row, a row's
// page moved and the page reloaded, its tab reloaded or closed at once was
// left where it was, 2 times in 2 each).

/**
 * Moves a row's page as the row's move route takes it (`target`), kept in
 * the outbox as it is called: settles with the server's answer, or null once
 * it went after being kept for a server out of reach or behind changes the
 * server hadn't taken yet (whenKeptChangeWent). Refused, it throws, and it is
 * gone. `pageId` is the page holding the row's database, if known; without
 * an outbox it goes at once.
 */
export async function sendRowMove(session: Session, rowId: string, target: NativeRowMove["target"], pageId?: string | null,
  signal?: AbortSignal): Promise<DatabaseRowPageActionResponse | null> {
  await whenRowSaved(rowId);
  const mutationId = newId(), outbox = contentGraphOutboxOf(session), rowMove: NativeRowMove = { rowId, target };
  const place = pageRowMadeOn(rowId) ?? pageId ?? rowId;
  if (!outbox) return sendKeptRowMove(session, { mutationId, rowMove }, signal) as Promise<DatabaseRowPageActionResponse | null>;
  const delivery = await outbox.admitAndSendGraph({ mutationId, operations: [], pageId: place, rowMove }, signal);
  if (delivery.state === "committed") return delivery.result as DatabaseRowPageActionResponse | null;
  if (!(await whenKeptChangeWent(outbox, place, mutationId))) throw new Error("The row's kept move was refused.");
  return null;
}

// How many times a row met changed since it was read is read and sent again.
const ROW_CHANGED_TRIES = 3;

const changedMeanwhile = (error: unknown) => Boolean(error && typeof error === "object"
  && Reflect.get(error, "code") === databaseRowPageErrorCodes.conflict);

/**
 * Whether a row's page is where its move sends it already — by a send whose
 * answer was lost to a reload, a closed tab or the network: a page now, under
 * the page or at the top it was sent to, or a row of the database it was sent to.
 */
async function wentAlready(session: Session, { rowId, target }: NativeRowMove, signal?: AbortSignal) {
  if (target.targetDataSourceId) return false;
  const page = await api.readPage(session, rowId, signal).catch(() => null);
  return Boolean(page && !page.databaseRow && (target.targetTopLevel ? !page.parentId : page.parentId === target.targetPageId));
}

/**
 * Sends a kept move of a row's page (the outbox's route for it) on the row as
 * the server holds it now: read first, one already where it goes is left; one
 * changed between the read and the send is read and sent again. The first
 * send goes under the kept change's own ID. As the page goes there is no time
 * to read: it goes at once as the server holds the row (asHeld), as rows going
 * to the trash do. Refused, it throws.
 */
export async function sendKeptRowMove(session: Session, input: { mutationId: string; rowMove: NativeRowMove }, signal?: AbortSignal) {
  const { mutationId, rowMove: { rowId, target } } = input;
  if (pageLeaving()) {
    return moveDatabaseRowPage(session, rowId, { requestId: mutationId, expectedAuthorityRevision: 0, asHeld: true, ...target }, signal);
  }
  for (let tries = 1; ; tries += 1) {
    let row;
    try {
      ({ row } = await readDatabaseRowPage(session, rowId, { limit: 1, signal }));
    } catch (error) {
      // No row now: a page already where it went is left as it is.
      if (error instanceof LeafenApiError && error.status === 404 && await wentAlready(session, input.rowMove, signal)) return null;
      throw error;
    }
    if (target.targetDataSourceId && row.dataSourceId === target.targetDataSourceId) return null;
    try {
      return await moveDatabaseRowPage(session, rowId, {
        requestId: tries === 1 ? mutationId : newId(), expectedAuthorityRevision: row.authorityRevision, ...target,
      }, signal);
    } catch (error) {
      if (!changedMeanwhile(error) || tries >= ROW_CHANGED_TRIES || signal?.aborted) throw error;
    }
  }
}

// At hand for the outbox as the page goes (keptChangeRoutes.ts).
keptChangeRoutes.rowMove = sendKeptRowMove;
