import { request } from "@/apiTransport";
import { structureOperations, type RowSendRequest, type RowSendTargets, type RowSent } from "@/structureWire.generated";
import { loadSession } from "@/lib/authSessionStore";

// Passing a database row to a member through one of its person properties
// (database_row_send.go).

function sessionOrFail() {
  const session = loadSession();
  if (!session) throw new Error("Login required.");
  return session;
}

/** A window of the people a row can go to; nextCursor reads on to the last. */
export function readSendTargets(rowId: string, cursor: string, signal?: AbortSignal) {
  return request<RowSendTargets>(structureOperations.sendTargets.path.replace("{rowId}", encodeURIComponent(rowId))
    + (cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""), { signal }, sessionOrFail());
}

export function sendRow(rowId: string, body: RowSendRequest) {
  return request<RowSent>(structureOperations.send.path.replace("{rowId}", encodeURIComponent(rowId)),
    { method: "POST", body: JSON.stringify(body) }, sessionOrFail());
}

export const OPEN_SEND_ITEM_EVENT = "leafen:open-send-item";

export type OpenSendItemDetail = { rowId: string; title: string; workspaceId: string; dataSourceId: string };

/** Opens "보내기" for a database row with a person property (SendItemDialogHost). */
export function requestSendItemDialog(detail: OpenSendItemDetail) {
  window.dispatchEvent(new CustomEvent<OpenSendItemDetail>(OPEN_SEND_ITEM_EVENT, { detail }));
}
