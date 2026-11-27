import type { GraphRow } from "@/contentGraph";
import { readDatabaseRowPage, restoreDatabaseRowPage, trashDatabaseRowPage } from "@/databaseRowPageApi";
import { databaseRowPageErrorCodes } from "@/databaseRowPageWire.generated";
import { contentGraphOutboxOf } from "./contentGraphSend";
import { newId } from "./ids";
import { keptChangeRoutes } from "./keptChangeRoutes";
import type { NativeRowTrash } from "./nativePageLaneEntries";
import { pageLeaving } from "./pageLeaving";
import { pageRowMadeOn, whenRowSaved } from "./rowMadeHere";
import type { Session } from "@/types";

// Database rows going to the trash, or out of it, wherever the press was —
// a row's menu or Del, a step of ⌘Z, a sidebar view's row, a row page's own
// menu, a task's, a copy taken back — go to the server this one way: kept in
// the outbox as a change of the page holding their database, behind the
// changes kept before it (the row's making among them), and sent by the
// rows' own route in turn. Whether a send that failed waits for the server
// or was refused is the outbox's one rule (graphWriteWaitsForServer): one the
// server can't be reached for stays on the device, the rows shown so, and
// goes once it can (2026-11-25: sent straight to the server, a row sent to
// the trash while it was out of reach came back 7 ms after the press, and ⌘Z
// of a row made took a server out of reach for the row changed elsewhere).

/** Where rows going to or out of the trash are kept: the page holding their database, and the rows as drawn there. */
export type RowTrashPlace = { pageId?: string | null; drawn?: readonly GraphRow[] };

/**
 * Sends rows to the trash, or out of it (`trash` false): "sent" once the
 * server has them so, "kept" while the server is out of reach — on the
 * device until it can take it. Refused, it throws, and it is gone. Kept, it
 * shows so on every copy of the page this device draws (rowTrashShown): the
 * rows, and those `drawn` with them, gone, or back as drawn. A row made
 * here goes once its making is saved or kept, on the page its making went
 * to, behind it, so ⌘Z right after the press that made it doesn't go ahead
 * of the row. Without an outbox it goes at once.
 */
export async function sendRowTrash(session: Session, rowIds: readonly string[], trash: boolean, place: RowTrashPlace = {}): Promise<"sent" | "kept"> {
  const rowTrash: NativeRowTrash = { rowIds: [...rowIds], trash, ...(place.drawn?.length ? { drawn: [...place.drawn] } : {}) };
  await Promise.all(rowIds.map(whenRowSaved));
  const mutationId = newId(), outbox = contentGraphOutboxOf(session);
  const pageId = rowIds.map(pageRowMadeOn).find(Boolean) ?? place.pageId ?? rowIds[0];
  if (!outbox || !pageId) {
    await sendKeptRowTrash(session, { mutationId, rowTrash });
    return "sent";
  }
  const delivery = await outbox.admitAndSendGraph({ mutationId, operations: [], pageId, rowTrash });
  return delivery.state === "kept" ? "kept" : "sent";
}

// How many times a row met changed since it was read is read and sent again.
const ROW_CHANGED_TRIES = 3;

/** A refusal for the row having changed since it was read: moved to the trash or out of it, or written, meanwhile. */
function rowChangedMeanwhile(error: unknown) {
  const code = error && typeof error === "object" ? Reflect.get(error, "code") : undefined;
  return code === databaseRowPageErrorCodes.conflict || code === databaseRowPageErrorCodes.inTrash;
}

/**
 * Sends kept rows to the trash or out of it (the outbox's route for them),
 * each as the server holds it now: read first, one already where it goes —
 * by a send whose answer was lost to a reload, a closed tab or the network,
 * or with the row above it — is left; one changed between the read and the
 * send is read and sent again. The first send of each goes under the kept
 * change's own ID. Refused, it throws. As the page goes there is no time to
 * read: every row goes at once, as the server holds it (asHeld), which
 * leaves one already where it goes as the read would (2026-11-27: three rows
 * sent to the trash and the tab closed at once reached the server only once
 * Leafen was opened again, each row's read never answered).
 */
export async function sendKeptRowTrash(session: Session, input: { mutationId: string; rowTrash: NativeRowTrash }, signal?: AbortSignal) {
  const { mutationId, rowTrash: { rowIds, trash } } = input;
  const send = trash ? trashDatabaseRowPage : restoreDatabaseRowPage;
  if (pageLeaving()) {
    await Promise.all(rowIds.map((rowId) => send(session, rowId, { requestId: mutationId, expectedAuthorityRevision: 0, asHeld: true }, signal)));
    return { rowIds: [...rowIds], trash };
  }
  for (const rowId of rowIds) {
    for (let tries = 1; ; tries += 1) {
      const { row } = await readDatabaseRowPage(session, rowId, { limit: 1, signal });
      if ((row.trashedAt !== "") === trash) break;
      try {
        await send(session, rowId, { requestId: tries === 1 ? mutationId : newId(), expectedAuthorityRevision: row.authorityRevision }, signal);
        break;
      } catch (error) {
        if (!rowChangedMeanwhile(error) || tries >= ROW_CHANGED_TRIES || signal?.aborted) throw error;
      }
    }
  }
  return { rowIds: [...rowIds], trash };
}

// At hand for the outbox as the page goes (keptChangeRoutes.ts).
keptChangeRoutes.rowTrash = sendKeptRowTrash;
