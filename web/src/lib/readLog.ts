import { request } from "@/apiTransport";
import { readLogOperations, type ReadLog } from "@/readLogWire.generated";
import { loadSession } from "@/lib/authSessionStore";

// Who opened what (the server's read_log.go), kept for every page, item and
// file: a page's log holds the reads of it and all under it. Someone with full access
// to the scope reads its log, unless a compliance administrator left it to
// compliance administrators, who read any and choose; reading it is
// recorded too.

export const OPEN_READ_LOG_EVENT = "leafen:open-read-log";

export type OpenReadLogDetail = { workspaceId: string; scope: `page:${string}` };

export function requestReadLogDialog(detail: OpenReadLogDetail) {
  window.dispatchEvent(new CustomEvent<OpenReadLogDetail>(OPEN_READ_LOG_EVENT, { detail }));
}

function path(template: string, workspaceId: string) {
  return template.replace("{workspaceId}", encodeURIComponent(workspaceId));
}

function sessionOrFail() {
  const session = loadSession();
  if (!session) throw new Error("Login required.");
  return session;
}

export function readReadLog(workspaceId: string, scope: string, cursor = "", signal?: AbortSignal): Promise<ReadLog> {
  const query = new URLSearchParams({ scope, ...(cursor ? { cursor } : {}) });
  return request<ReadLog>(`${path(readLogOperations.list.path, workspaceId)}?${query}`, { signal }, sessionOrFail());
}

export function setReadLogReaders(workspaceId: string, scope: string, readers: ReadLog["readers"]): Promise<ReadLog> {
  return request<ReadLog>(path(readLogOperations.readers.path, workspaceId),
    { method: "PUT", body: JSON.stringify({ scope, readers }) }, sessionOrFail());
}
