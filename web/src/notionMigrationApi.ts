import { request } from "./apiTransport";
import type {
  NativeNotionMigration,
  NativeNotionMigrationInput,
  NativeNotionMigrationList,
  NativeNotionMigrationPlan,
  NativeNotionMigrationPlanState,
  NativeNotionMigrationScanItems,
} from "./notionImportWire.generated";
import type { Session } from "./types";

/**
 * A design-first Notion migration's endpoints: start one from a Notion
 * connection and its roots, scan it a step at a time, read and save its
 * plan, and run it.
 */
export function createNotionMigrationApi() {
  const path = (workspaceId: string, rest = "") => `/workspaces/${encodeURIComponent(workspaceId)}/notion-migrations${rest}`;
  const one = (workspaceId: string, migrationId: string, rest = "") => path(workspaceId, `/${encodeURIComponent(migrationId)}${rest}`);
  return {
    /** The person's migrations, latest first, every window of them. */
    notionMigrations: async (session: Session, workspaceId: string, options: { signal?: AbortSignal } = {}) => {
      const items: NativeNotionMigrationList["items"] = [];
      let cursor = "";
      do {
        const list = await request<NativeNotionMigrationList>(path(workspaceId, cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""),
          { signal: options.signal }, session);
        items.push(...list.items);
        cursor = list.nextCursor ?? "";
      } while (cursor);
      return { items, nextCursor: "" } satisfies NativeNotionMigrationList;
    },
    createNotionMigration: (session: Session, workspaceId: string, input: NativeNotionMigrationInput) =>
      request<NativeNotionMigration>(path(workspaceId), { method: "POST", body: JSON.stringify(input) }, session),
    notionMigration: (session: Session, workspaceId: string, migrationId: string, options: { signal?: AbortSignal } = {}) =>
      request<NativeNotionMigration>(one(workspaceId, migrationId), { signal: options.signal }, session),
    scanNotionMigration: (session: Session, workspaceId: string, migrationId: string, options: { signal?: AbortSignal } = {}) =>
      request<NativeNotionMigration>(one(workspaceId, migrationId, "/scan"), { method: "POST", body: "{}", signal: options.signal }, session),
    /** A window of one kind of the scan's findings from the cursor on. */
    notionMigrationScanItems: (session: Session, workspaceId: string, migrationId: string,
      kind: "pages" | "databases" | "issues" | "people", cursor = "", options: { signal?: AbortSignal } = {}) =>
      request<NativeNotionMigrationScanItems>(one(workspaceId, migrationId,
        `/scan-items?kind=${kind}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`), { signal: options.signal }, session),
    notionMigrationPlan: (session: Session, workspaceId: string, migrationId: string, options: { signal?: AbortSignal } = {}) =>
      request<NativeNotionMigrationPlanState>(one(workspaceId, migrationId, "/plan"), { signal: options.signal }, session),
    saveNotionMigrationPlan: (session: Session, workspaceId: string, migrationId: string, baseRevision: number, plan: NativeNotionMigrationPlan) =>
      request<NativeNotionMigrationPlanState>(one(workspaceId, migrationId, "/plan"),
        { method: "PUT", body: JSON.stringify({ baseRevision, plan }) }, session),
    runNotionMigration: (session: Session, workspaceId: string, migrationId: string, requestId: string, planRevision: number) =>
      request<NativeNotionMigration>(one(workspaceId, migrationId, "/run"),
        { method: "POST", body: JSON.stringify({ requestId, planRevision }) }, session),
  };
}
