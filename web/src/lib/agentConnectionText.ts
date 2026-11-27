import { api } from "@/api";
import { i18next } from "@/i18n";
import { activeDateLocale } from "@/lib/i18n";
import { fetchRuntimeConfig } from "@/lib/runtimeConfigService";
import { linkOrigin, serverOrigin } from "@/lib/serverOrigin";
import type { Session } from "@/types";
import { outsideAddress } from "./addressReach";
import { agentConnectionPrompt } from "./agentConnection";
import { agentServerLatinName } from "./agentEntryNames";
import { personalAccessTokenExpiry, type PersonalAccessTokenLifetime } from "./personalAccessTokens";

/** Where a one-time code is traded for a key, on any of the server's addresses. */
export const CODE_EXCHANGE_PATH = "/api/leafen/v1/mcp/connection-codes/exchange";

type AgentConnectionTextInput = {
  /** The workspace the text is made from, whose start the agent may offer; the connection reaches all the person's. */
  workspaceId: string;
  clientName: string;
  /** How long the code's key lives: a year unless chosen (agent key decision of 2026-11-16). */
  expiresIn?: PersonalAccessTokenLifetime;
  /** The server's public address, written or its tunnel's; "" with none, and one not outside (outsideAddress) counts as none. */
  publicAddress: string;
  /** What the agent does once connected, in place of asking how to start. */
  startWith?: string;
  /** Whether the person may turn on the public address themselves; false names the server's administrator. */
  canTurnOnPublic?: boolean;
  signal?: AbortSignal;
};

/**
 * Makes the one text an agent connects with (lib/agentConnection.ts): a new
 * one-time code for an agent connection — the person's own rights in all
 * their workspaces (internal/app/connection_person_rights.go) —, the address this page reaches Leafen at
 * and, when the server has one agents on the internet reach over HTTPS, that
 * address too — and whether it asks how to start, as the server judged it
 * for the workspace with the code (offerStart).
 */
export async function makeAgentConnection(session: Session, input: AgentConnectionTextInput): Promise<{ text: string; offerStart: boolean }> {
  // The agent names itself as it trades the code; one that doesn't is told
  // apart by when its code was made ("에이전트 · 10월 16일 오후 12:01").
  const made = new Intl.DateTimeFormat(activeDateLocale(), { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })
    .format(new Date());
  const code = await api.createMCPConnectionCode(session, {
    clientName: `${input.clientName} · ${made}`,
    ...(input.expiresIn ? { expiresIn: input.expiresIn } : {}),
  }, input.signal);
  // The day its key ends, as the person reads it; the server counts it from the trade.
  const keyEnds = new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long" })
    .format(personalAccessTokenExpiry(input.expiresIn ?? "1y"));
  const publicOrigin = outsideAddress(input.publicAddress) ? new URL(input.publicAddress).origin : "";
  let exchangePath = CODE_EXCHANGE_PATH;
  try {
    exchangePath = new URL(code.exchangeUrl).pathname;
  } catch {
    // The path every server answers at.
  }
  const { config } = await fetchRuntimeConfig();
  // The text's words, and the move's first words it may give (startInChat).
  await i18next.loadNamespaces(["agentPrompt", "importDialog"]);
  const offerStart = code.offerStart !== false;
  const text = agentConnectionPrompt({
    // The lines name the address the links take (agent_facing_address.go on the server).
    localOrigin: serverOrigin(), publicOrigin, linesOrigin: linkOrigin(),
    code: code.code, exchangePath, expiresAt: code.expiresAt, startWith: input.startWith, canTurnOnPublic: input.canTurnOnPublic,
    serverName: agentServerLatinName({ name: config.serverName ?? "", alias: config.serverAlias ?? "" }), serverCode: config.serverCode ?? "",
    // Whose code it is: a connection kept must work as them (agentEntryReach), and one beside another's names them.
    person: session.user, keyEnds, offerStart,
  });
  return { text, offerStart };
}

/** The text alone (makeAgentConnection). */
export async function makeAgentConnectionText(session: Session, input: AgentConnectionTextInput): Promise<string> {
  return (await makeAgentConnection(session, input)).text;
}
