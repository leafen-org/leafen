import { request } from "@/apiTransport";
import { mcpOAuthOperations } from "@/mcpOAuthWire.generated";
import type { Session } from "@/types";
import type { DatabaseSourceTitle } from "@/components/editor/databaseTitles";

/** Notion's six groups of an MCP token's tools (mcp_token_tools.go). */
const MCP_TOOL_GROUPS = ["readContent", "updateContent", "insertContent", "comments", "users", "tasks"] as const;
export type McpToolGroupId = typeof MCP_TOOL_GROUPS[number];

export type McpTokenTool = { name: string; title: string; enabled: boolean };

export type McpTokenPage = { id: string; title: string; icon: string; iconType: string; pageKind?: string; viewBlock?: boolean; sourceTitles?: DatabaseSourceTitle[] };

/** An MCP token's own page (mcp_token_settings.go); its key in the answer that makes it, or asked for by its person. */
export type McpTokenSettings = {
  id: string;
  clientName: string;
  description: string;
  /** An emoji or a picture's link, or "" for its first letter (connection_icon.go). */
  icon: string;
  /** An agent connected over OAuth, which renews its own access. */
  oauth: boolean;
  workspaces: Array<{ id: string; name: string }>;
  /** An agent connection — its person's in all their workspaces — rather than an MCP token (connection_person_rights.go). */
  agent: boolean;
  createdAt: number;
  expiresAt: number | null;
  tokenMadeAt: number;
  /** Until when the key it had before still works. */
  previousUntil: number | null;
  toolGroups: Array<{ id: McpToolGroupId; tools: McpTokenTool[] }>;
  /** "모두 맡기기(묻지 않음)": actions that can't be undone go ahead without the person's yes (mcp_action_approvals.go). */
  handsAllOver: boolean;
  /** "Notion에만 있는 도구 이름": its list names Notion's tools Leafen doesn't have too (mcp_notion_only_tools.go). */
  notionOnlyTools: boolean;
  /** "모든 페이지": every page its person reads, later ones too; off, only `pages` and what is under them. */
  allPages: boolean;
  pages: McpTokenPage[];
  /** Whether its key is kept to be shown again — not for one made before keys were kept, until it is refreshed. */
  keyKept: boolean;
  key?: string;
};

const path = (id: string, end: string) => `/mcp/connections/${encodeURIComponent(id)}/${end}`;

export function readMcpToken(session: Session, id: string, signal?: AbortSignal) {
  return request<McpTokenSettings>(path(id, "settings"), { signal }, session);
}

export function changeMcpToken(session: Session, id: string,
  change: {
    clientName?: string; description?: string; icon?: string; disabledTools?: string[]; handsAllOver?: boolean; notionOnlyTools?: boolean;
  }) {
  return request<McpTokenSettings>(path(id, "settings"), { method: "PATCH", body: JSON.stringify(change) }, session);
}

/** Its key, for its person to see again, as Notion's eye shows it; a key not kept answers 404. */
export async function revealMcpToken(session: Session, id: string) {
  return (await request<{ key: string }>(path(id, "token/reveal"), { method: "POST", body: "{}" }, session)).key;
}

/** Makes the key again; the old one stops now or after seven days. */
export function refreshMcpToken(session: Session, id: string, revokeOld: "now" | "later") {
  return request<McpTokenSettings>(path(id, "token"), { method: "POST", body: JSON.stringify({ revokeOld }) }, session);
}

/** Turns "모든 페이지" on, or holds the token to these pages — none, no page. */
export function chooseMcpTokenPages(session: Session, id: string, reach: { allPages: true } | { allPages: false; pageIds: string[] }) {
  return request<McpTokenSettings>(path(id, "pages"), { method: "PUT", body: JSON.stringify(reach) }, session);
}

/** Deletes it: its key stops at once, as disconnecting it in 설정 › 에이전트 does. */
export function deleteMcpToken(session: Session, id: string) {
  return request<void>(mcpOAuthOperations.revokeConnection.path.replace("{id}", encodeURIComponent(id)),
    { method: mcpOAuthOperations.revokeConnection.method }, session);
}

// A key made in the new token window is handed to its page, which shows it
// without asking the server again; later it is asked for (revealMcpToken).
const madeKeys = new Map<string, string>();

export function rememberMcpTokenKey(id: string, key: string) {
  madeKeys.set(id, key);
}

export function takeMcpTokenKey(id: string) {
  const key = madeKeys.get(id) ?? "";
  madeKeys.delete(id);
  return key;
}

const CHANGED = "leafen:mcp-connections-changed";

/** Says that the person's MCP connections changed (one disconnected or deleted), for lists showing them. */
export function announceMcpConnectionsChanged() {
  window.dispatchEvent(new Event(CHANGED));
}

/** Calls back when the person's MCP connections changed; returns the unsubscribe. */
export function onMcpConnectionsChanged(callback: () => void) {
  window.addEventListener(CHANGED, callback);
  return () => window.removeEventListener(CHANGED, callback);
}
