import { request } from "./apiTransport";
import type {
  NativeNotionImportCapabilities,
  NativeNotionImportConnection,
  NativeNotionImportConnectionInput,
  NativeNotionImportConnectionPage,
  NativeNotionImportFileOutcome,
  NativeNotionImportFileOutcomePage,
  NativeNotionImportJob,
  NativeNotionImportScope,
} from "./notionImportWire.generated";
import { nativeNotionImportBounds } from "./notionImportWire.generated";
import type { Session } from "./types";

/**
 * The Notion import endpoints: capabilities, connections and their scope,
 * import jobs, and each job's file outcomes and block issues.
 */
export function createNotionImportApi() {
  return {
    notionImportCapabilities: (
      session: Session,
      workspaceId: string,
      options: { signal?: AbortSignal } = {},
    ) => request<NativeNotionImportCapabilities>(
      `/workspaces/${encodeURIComponent(workspaceId)}/notion-import/capabilities`,
      { signal: options.signal },
      session,
    ),
    createNotionImportConnection: (
      session: Session,
      workspaceId: string,
      input: NativeNotionImportConnectionInput,
      options: { signal?: AbortSignal } = {},
    ) => request<NativeNotionImportConnection>(
      `/workspaces/${encodeURIComponent(workspaceId)}/notion-import/connections`,
      { method: "POST", body: JSON.stringify(input), signal: options.signal },
      session,
    ),
    notionImportConnections: (
      session: Session,
      workspaceId: string,
      cursor = "",
      options: { signal?: AbortSignal } = {},
    ) => request<NativeNotionImportConnectionPage>(
      `/workspaces/${encodeURIComponent(workspaceId)}/notion-import/connections${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`,
      { signal: options.signal },
      session,
    ),
    revokeNotionImportConnection: (
      session: Session,
      workspaceId: string,
      connectionId: string,
    ) => request<NativeNotionImportConnection>(
      `/workspaces/${encodeURIComponent(workspaceId)}/notion-import/connections/${encodeURIComponent(connectionId)}`,
      { method: "DELETE" },
      session,
    ),
    notionImportScope: (
      session: Session,
      workspaceId: string,
      connectionId: string,
      cursor = "",
      options: { signal?: AbortSignal; query?: string } = {},
    ) => {
      // Without a query: what the connection reaches at the top; with one:
      // the pages and databases whose title matches, at any depth.
      const search = new URLSearchParams();
      if (cursor) search.set("cursor", cursor);
      if (options.query?.trim()) search.set("query", options.query.trim());
      const suffix = search.toString() ? `?${search.toString()}` : "";
      return request<NativeNotionImportScope>(
        `/workspaces/${encodeURIComponent(workspaceId)}/notion-import/connections/${encodeURIComponent(connectionId)}/scope${suffix}`,
        { signal: options.signal },
        session,
      );
    },
    notionImportJob: (
      session: Session,
      workspaceId: string,
      jobId: string,
      options: { signal?: AbortSignal } = {},
    ) => request<NativeNotionImportJob>(
      `/workspaces/${encodeURIComponent(workspaceId)}/notion-import/jobs/${encodeURIComponent(jobId)}`,
      { signal: options.signal },
      session,
    ),
    notionImportFileOutcomePage: (
      session: Session,
      workspaceId: string,
      jobId: string,
      cursor = "",
      options: { signal?: AbortSignal } = {},
    ) => {
      const query = new URLSearchParams({ limit: String(nativeNotionImportBounds.fileOutcomeItems) });
      if (cursor) query.set("cursor", cursor);
      return request<NativeNotionImportFileOutcomePage>(
        `/workspaces/${encodeURIComponent(workspaceId)}/notion-import/jobs/${encodeURIComponent(jobId)}/file-outcomes?${query}`,
        { signal: options.signal },
        session,
      );
    },
    retryNotionImportFile: (
      session: Session,
      workspaceId: string,
      jobId: string,
      sourceId: string,
    ) => request<NativeNotionImportFileOutcome>(
      `/workspaces/${encodeURIComponent(workspaceId)}/notion-import/jobs/${encodeURIComponent(jobId)}/file-outcomes/${encodeURIComponent(sourceId)}/retry`,
      { method: "POST" },
      session,
    ),
    cancelNotionImportJob: (session: Session, workspaceId: string, jobId: string) => request<NativeNotionImportJob>(
      `/workspaces/${encodeURIComponent(workspaceId)}/notion-import/jobs/${encodeURIComponent(jobId)}/cancel`,
      { method: "POST" },
      session,
    ),
    retryNotionImportJob: (session: Session, workspaceId: string, jobId: string) => request<NativeNotionImportJob>(
      `/workspaces/${encodeURIComponent(workspaceId)}/notion-import/jobs/${encodeURIComponent(jobId)}/retry`,
      { method: "POST" },
      session,
    ),
  };
}
