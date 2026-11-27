import { request } from "@/apiTransport";
import type { Session } from "@/types";

/**
 * The workspace owner's policies of "연결 → 관리 → 사용 권한 관리하기" and
 * "이모지", as Notion's (seen 2026-10-02): who may make personal access
 * tokens, whether automations and buttons may send webhooks, whether only
 * owners add custom emoji, whether pages published as sites say so, and
 * whether — and to which — custom MCP servers people connect their agents.
 */
export type WorkspaceConnectionPolicies = {
  personalAccessTokens: "members" | "owners" | "owners_and_groups";
  /** The organization's groups whose people may make tokens beside the owners. */
  personalAccessTokenGroups: string[];
  automationWebhooks: boolean;
  customEmojiOwnersOnly: boolean;
  /** A page published as a site shows so in a banner over it ("공개 페이지 → 설정"). */
  siteBanner: boolean;
  /** People may connect their agents to custom MCP servers ("사용자 지정 MCP 서버 활성화하기"). */
  customMcpServers: boolean;
  /** The servers they may connect, where it names any: addresses and the places under them. */
  customMcpAllowlist: string[];
  /** The reader is an owner, who may change them. */
  canManage: boolean;
  /** The reader may make personal access tokens. */
  canMakeTokens: boolean;
  /** The organization's groups an owner may name, all of them once read whole. */
  groupChoices?: Array<{ id: string; name: string }>;
  groupChoicesNextCursor?: string;
};

export type WorkspaceConnectionPolicyChange = Partial<Omit<WorkspaceConnectionPolicies,
  "canManage" | "canMakeTokens" | "groupChoices" | "groupChoicesNextCursor">>;

function policiesPath(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId)}/connection-policies`;
}

/** The policies, with every group an owner may name, read a window at a time. */
export async function readWorkspaceConnectionPolicies(session: Session, workspaceId: string, signal?: AbortSignal) {
  const policies = await request<WorkspaceConnectionPolicies>(policiesPath(workspaceId), { signal }, session);
  const groups = [...policies.groupChoices ?? []];
  let cursor = policies.groupChoicesNextCursor ?? "";
  while (cursor) {
    const next = await request<WorkspaceConnectionPolicies>(
      `${policiesPath(workspaceId)}?groupsCursor=${encodeURIComponent(cursor)}`, { signal }, session,
    );
    groups.push(...next.groupChoices ?? []);
    cursor = next.groupChoicesNextCursor ?? "";
  }
  return { ...policies, groupChoices: policies.groupChoices ? groups : undefined, groupChoicesNextCursor: undefined };
}

/** Sent once this tab changed a workspace's policies, its id in `detail`: what keeps them reads them anew. */
export const CONNECTION_POLICIES_CHANGED_EVENT = "leafen:connection-policies-changed";

export async function changeWorkspaceConnectionPolicies(session: Session, workspaceId: string, change: WorkspaceConnectionPolicyChange) {
  const changed = await request<WorkspaceConnectionPolicies>(policiesPath(workspaceId), { method: "PATCH", body: JSON.stringify(change) }, session);
  window.dispatchEvent(new CustomEvent(CONNECTION_POLICIES_CHANGED_EVENT, { detail: workspaceId }));
  return changed;
}
