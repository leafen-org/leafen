import { request } from "@/apiTransport";
import type { Session } from "@/types";

// Signing a browser in by the person's agent (internal/app/agent_sign_in_requests.go):
// the sign-in screen asks for a short code, polls with its own secret until
// the agent approves, declines or the code runs out, and gives the request up
// when the person leaves it. The account's switch says whether its agents
// may approve such a sign-in.

export type AgentSignInStart = { code: string; token: string; expiresAt: number; interval: number };
export type AgentSignInWait = { status: "pending" | "declined" | "expired"; expiresAt?: number };

export function isAgentSignInWait(value: Session | AgentSignInWait): value is AgentSignInWait {
  return "status" in value && !("token" in value);
}

export const agentSignInApi = {
  ask: () => request<AgentSignInStart>("/auth/agent-sign-in", { method: "POST", body: "{}" }),
  // At the account's sign-in limit, replaceOldestSession ends its oldest sign-in for this one.
  poll: (token: string, replaceOldestSession = false) => request<Session | AgentSignInWait>(
    "/auth/agent-sign-in/poll", { method: "POST", body: JSON.stringify(replaceOldestSession ? { token, replaceOldestSession } : { token }) },
  ),
  cancel: (token: string) => request<void>("/auth/agent-sign-in/cancel", { method: "POST", body: JSON.stringify({ token }) }),
  readSetting: (session: Session, signal?: AbortSignal) => request<{ allowed: boolean }>("/account/agent-sign-in", { signal }, session),
  setSetting: (session: Session, allowed: boolean) => request<{ allowed: boolean }>(
    "/account/agent-sign-in", { method: "PUT", body: JSON.stringify({ allowed }) }, session,
  ),
};
