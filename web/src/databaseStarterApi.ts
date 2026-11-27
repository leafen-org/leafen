import { request } from "@/apiTransport";
import { databaseStarterOperations, type DatabaseStarterApplyInput, type DatabaseStarterSourceViewsResponse, type DatabaseStarterApplyResponse, type DatabaseStarterUsedResponse } from "@/databaseStarterWire.generated";
import type { Session } from "@/types";

export function applyDatabaseStarter(session: Session, pageId: string, input: DatabaseStarterApplyInput, signal?: AbortSignal) {
  return request<DatabaseStarterApplyResponse>(databaseStarterOperations.apply.path.replace("{pageId}", encodeURIComponent(pageId)),
    { method: databaseStarterOperations.apply.method, body: JSON.stringify(input), signal }, session);
}

export function readDatabaseStarterSourceViews(session: Session, containerId: string, dataSourceId: string, cursor: string | undefined, signal: AbortSignal) {
  const query = new URLSearchParams({ dataSourceId });
  if (cursor) query.set("cursor", cursor);
  return request<DatabaseStarterSourceViewsResponse>(databaseStarterOperations.sourceViews.path.replace("{containerId}", encodeURIComponent(containerId)) + "?" + query,
    { signal }, session);
}

/** The templates the page holding a new database already shows (database_starter_origin.go). */
export function readDatabaseStarterUsed(session: Session, pageId: string, signal: AbortSignal) {
  return request<DatabaseStarterUsedResponse>(databaseStarterOperations.used.path.replace("{pageId}", encodeURIComponent(pageId)), { signal }, session);
}
