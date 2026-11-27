import { useTranslation } from "react-i18next";
import { Switch } from "@/components/Switch";
import type { McpTokenSettings } from "@/lib/mcpTokens";
import styles from "./DeveloperTools.module.css";
import own from "./InternalConnection.module.css";
import asks from "./McpTokenAsks.module.css";

/**
 * "Notion에만 있는 도구 이름" (decided 2026-11-16): Notion's MCP tools Leafen
 * doesn't have — folders, skills, agents, sessions, calendar, meeting notes,
 * AI search — only answer that Leafen doesn't have them, so a token's list
 * leaves them out until this is turned on, for an agent written against
 * Notion's own list (mcp_notion_only_tools.go), and then names the Notion
 * tools Leafen has too, which the list otherwise leaves to its core tools
 * (mcp_core_tools.go). Called by name, each still says what to use instead.
 */
export function McpTokenNotionOnlyTools({ settings, onChange }: { settings: McpTokenSettings; onChange(next: boolean): void }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const title = t("developerTools.mcpToken.notionOnlyToolsTitle");
  return (
    <section className={styles.card} data-mcp-token-card="notion-only-tools">
      <div className={asks.row}>
        <div>
          <h2 className={styles.cardTitle}>{title}</h2>
          <p className={`${styles.description} ${own.description}`}>{t("developerTools.mcpToken.notionOnlyToolsDescription")}</p>
        </div>
        <Switch on={settings.notionOnlyTools} aria-label={title} data-mcp-token-notion-only-tools
          onClick={() => onChange(!settings.notionOnlyTools)} />
      </div>
    </section>
  );
}
