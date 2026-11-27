import { useTranslation } from "react-i18next";
import { Switch } from "@/components/Switch";
import type { McpTokenSettings } from "@/lib/mcpTokens";
import styles from "./DeveloperTools.module.css";
import own from "./InternalConnection.module.css";
import asks from "./McpTokenAsks.module.css";

/**
 * "모두 맡기기(묻지 않음)" (decided 2026-11-13): on, an agent with this token
 * does what can't be undone without asking; off, as a token starts, it asks
 * the person in their conversation first, allowed once or for an hour
 * (mcp_action_approvals.go). The person turns it here at once; the agent may
 * too, for its own token, asking them once to turn it on
 * (mcp_token_hand_over.go). An administrator's safeguards ask either way.
 * Notion's MCP has no such actions, so it has no such card.
 */
export function McpTokenAsks({ settings, onChange }: { settings: McpTokenSettings; onChange(next: boolean): void }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const title = t("developerTools.mcpToken.handsAllOverTitle");
  return (
    <section className={styles.card} data-mcp-token-card="asks">
      <div className={asks.row}>
        <div>
          <h2 className={styles.cardTitle}>{title}</h2>
          <p className={`${styles.description} ${own.description}`}>{t("developerTools.mcpToken.handsAllOverDescription")}</p>
        </div>
        <Switch on={settings.handsAllOver} aria-label={title} data-mcp-token-asks
          onClick={() => onChange(!settings.handsAllOver)} />
      </div>
    </section>
  );
}
