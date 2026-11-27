import { useTranslation } from "react-i18next";
import { useProductPages } from "@/components/product/productPagesContext";
import type { McpTokenSettings } from "@/lib/mcpTokens";
import { WorkspaceMark } from "./DeveloperToolsTables";
import styles from "./DeveloperTools.module.css";
import own from "./InternalConnection.module.css";
import token from "./McpToken.module.css";

/**
 * The workspaces a connection reaches, on its page's token card: an MCP
 * token's one, as Notion's, or an agent connection's — all its person's —
 * each of them (2026-11-23: one reaching several showed its first alone).
 * A call that needs a workspace and names nothing in one says which on a
 * connection reaching several; there is no default to choose
 * (internal/app/connection_person_rights.go).
 */
export function McpTokenWorkspaces({ settings }: { settings: McpTokenSettings }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const icons = new Map((useProductPages()?.workspaces ?? []).map((workspace) => [workspace.id, workspace.icon ?? ""]));
  const several = settings.workspaces.length > 1;
  return (
    <>
      <div className={styles.cardField}>
        <span>{t(several ? "developerTools.mcpToken.workspaces" : "developerTools.mcpToken.workspace")}</span>
        <span className={`${own.chipField} ${several ? token.chips : token.oneLine}`} data-mcp-token-workspaces>
          {settings.workspaces.map((place) => (
            <span key={place.id} className={`${own.chip} ${token.oneLine}`}>
              <WorkspaceMark workspace={{ name: place.name, icon: icons.get(place.id) ?? "" }} />{place.name}</span>
          ))}
        </span>
      </div>
    </>
  );
}
