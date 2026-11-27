import i18next from "i18next";
import { claudeConnectorBlockingPort } from "./claudeConnector";
import { SETUP_RESTORE_API_PATH, SETUP_RESTORE_MCP_PATH } from "./setupRestore";

/**
 * The sentence the person pastes to the agent they leave a first-setup
 * restore to (user decision 2026-11-19, server: setup_restore_agent.go), in
 * the person's language, which the agent then speaks with them; the code,
 * addresses and names stay as they are. The restore MCP endpoint takes no
 * sign-in and its address holds no secret: the code goes in every call, so
 * it stays out of addresses and the logs that keep them. Passwords and keys
 * are not the agent's: it says which field to fill here.
 */
export function setupRestoreAgentSentence(input: {
  /** The address the agent reaches this server at. */
  origin: string;
  code: string;
  minutes: number;
  /** An agent elsewhere, which may be a chat app that adds connectors by hand. */
  elsewhere: boolean;
}): string {
  const t = (key: string, options?: Record<string, unknown>) =>
    String(i18next.t(`authGate:${key}`, { ...options, interpolation: { escapeValue: false } }));
  const origin = input.origin.replace(/\/+$/u, "");
  const connectorPort = claudeConnectorBlockingPort(origin);
  return [
    t("setupRestoreAgentPromptIntro"),
    t("setupRestoreAgentPromptTalk"),
    "",
    t("setupRestoreAgentPromptMcp", { address: `${origin}${SETUP_RESTORE_MCP_PATH}` }),
    t("setupRestoreAgentPromptCode", { code: input.code, minutes: input.minutes }),
    t("setupRestoreAgentPromptApi", { address: `${origin}${SETUP_RESTORE_API_PATH}` }),
    ...(input.elsewhere ? [t("setupRestoreAgentPromptChatApps")] : []),
    // Claude's connector doesn't connect at a port (lib/claudeConnector).
    ...(input.elsewhere && connectorPort ? [t("setupRestoreAgentPromptClaudePort", { port: connectorPort })] : []),
    "",
    t("setupRestoreAgentPromptSteps"),
    t("setupRestoreAgentPromptSecrets"),
  ].join("\n");
}
