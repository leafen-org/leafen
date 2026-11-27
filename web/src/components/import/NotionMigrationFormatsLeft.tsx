import { useTranslation } from "react-i18next";
import type { NativeNotionMigration } from "@/notionImportWire.generated";
import type { Session } from "@/types";
import styles from "./ImportDialog.module.css";
import { NotionAgentHandoff } from "./NotionAgentHandoffButton";

/**
 * The end of a Notion import made by hand (decision of 2026-11-12, "6의
 * 보탬"): what Notion's API didn't give and Leafen left at its defaults, by
 * kind — as an agent's leafen_notion_migration_status says them in
 * formats_left_groups (item by item with list formats_left) — and
 * "에이전트에게 맡기기", which copies the words that hand this finished
 * migration to an agent at the guide's part 6, to have Leafen read them in
 * the person's signed-in Notion and match them at once
 * (leafen_notion_migration_formats). Nothing when nothing was left.
 */
export function NotionMigrationFormatsLeft({ migration, session, workspaceId }: {
  migration: NativeNotionMigration;
  session: Session;
  workspaceId: string;
}) {
  const { t } = useTranslation("importDialog");
  const kinds = migration.formatsLeft ?? [];
  if (!kinds.length) return null;
  return (
    <div className={styles.omissionNotice} data-notion-formats-left>
      <strong>{t("finish.title")}</strong>
      <ul>
        {kinds.map(({ kind, count }) => <li key={kind} data-notion-format-left={kind}>{t(`finish.kinds.${kind}`, { count })}</li>)}
      </ul>
      <NotionAgentHandoff session={session} workspaceId={workspaceId} step="finish" body={t("finish.body")}
        move={(origin) => t("handoff.finish", { origin, id: workspaceId, migration: migration.id, interpolation: { escapeValue: false } })}
        copiedMove={t("finish.copied")} />
    </div>
  );
}
