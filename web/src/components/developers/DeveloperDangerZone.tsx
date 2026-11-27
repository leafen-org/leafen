import { useTranslation } from "react-i18next";
import styles from "./DeveloperTools.module.css";
import own from "./DeveloperDangerZone.module.css";

/**
 * The 위험 구역 card that closes an internal connection's, an OAuth
 * connection's and an MCP token's page, one shape for all three as Notion's
 * (seen 2026-11-21 on an internal connection): "위험 구역" and what deleting
 * does on one side, "삭제하기" at the end of the row. The press asks first
 * (DeveloperQuestions.askDeveloperDelete) in the page's own onRemove.
 */
export function DeveloperDangerZone({ kind, description, onRemove }: {
  kind: "connection" | "mcpToken";
  description: string;
  onRemove(): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const card = kind === "connection" ? { "data-connection-card": "danger" } : { "data-mcp-token-card": "danger" };
  const action = kind === "connection" ? { "data-connection-remove": "" } : { "data-mcp-token-remove": "" };
  return (
    <section className={styles.card} {...card}>
      <div className={own.row}>
        <div className={own.words}>
          <h2 className={own.title}>{t("developerTools.connection.danger")}</h2>
          <p className={own.description}>{description}</p>
        </div>
        <button type="button" className={own.action} {...action} onClick={onRemove}>{t("developerTools.connection.remove")}</button>
      </div>
    </section>
  );
}
