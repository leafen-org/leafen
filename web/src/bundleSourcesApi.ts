import { request } from "./apiTransport";
import type { Session } from "./types";

/**
 * Another tool's bundle as a migration source (docs/bundle.md "Moving from
 * another tool"): a new, empty workspace holds the bundle, and a migration
 * connection reads it as the Notion migration reads Notion.
 */
export type BundleSource = {
  connectionId: string;
  stagingWorkspaceId: string;
  /** The workspace's name while it is there, to confirm deleting it. */
  stagingWorkspaceName?: string;
  label: string;
  /** The tool that made the bundles ("<app id>:<key>" for an app's card). */
  tool?: string;
  /** The source in the tool they come from: the bundle descriptor's source id. */
  origin?: string;
  status: "importing" | "ready" | "failed" | "detached" | "closed";
  importJobId?: string;
  losses: Array<{ what: string; why: string; count: number }>;
};

const sourcesPath = (workspaceId: string, connectionId = "") =>
  `/workspaces/${encodeURIComponent(workspaceId)}/imports/bundle-sources${connectionId ? `/${encodeURIComponent(connectionId)}` : ""}`;

export function createBundleSourcesApi() {
  return {
    /** The person's sources for the workspace that aren't closed, the latest first; with `tool`, that tool's; every window of them. */
    bundleSources: async (session: Session, workspaceId: string, tool = "", signal?: AbortSignal) => {
      const items: BundleSource[] = [];
      let cursor = "";
      do {
        const query = new URLSearchParams();
        if (tool) query.set("tool", tool);
        if (cursor) query.set("cursor", cursor);
        const search = query.toString();
        const list = await request<{ items: BundleSource[]; nextCursor?: string }>(
          `${sourcesPath(workspaceId)}${search ? `?${search}` : ""}`, { method: "GET", signal }, session);
        items.push(...list.items);
        cursor = list.nextCursor ?? "";
      } while (cursor);
      return { items };
    },
    createBundleSource: (session: Session, workspaceId: string,
      input: { requestId: string; stagingWorkspaceId: string; label: string; tool?: string; origin?: string }) =>
      request<BundleSource>(sourcesPath(workspaceId), { method: "POST", body: JSON.stringify(input) }, session),
    bundleSource: (session: Session, workspaceId: string, connectionId: string, signal?: AbortSignal) =>
      request<BundleSource>(sourcesPath(workspaceId, connectionId), { method: "GET", signal }, session),
    /** Points the source at a new, empty workspace for the tool's next bundle. */
    moveBundleSource: (session: Session, workspaceId: string, connectionId: string, stagingWorkspaceId: string) =>
      request<BundleSource>(sourcesPath(workspaceId, connectionId), { method: "PUT", body: JSON.stringify({ stagingWorkspaceId }) }, session),
    closeBundleSource: (session: Session, workspaceId: string, connectionId: string) =>
      request<BundleSource>(sourcesPath(workspaceId, connectionId), { method: "DELETE" }, session),
  };
}
