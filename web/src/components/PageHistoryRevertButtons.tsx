import { useState } from "react";
import { useTranslation } from "react-i18next";
import { RestoreIcon } from "@/icons/leafen";
import type { PageHistoryGroup } from "@/lib/pageHistoryGroups";
import { revertPageHistoryChanges } from "@/lib/pageHistoryService";
import type { PageHistoryRevertResponse, PageHistoryVersion } from "@/pageHistoryWire.generated";
import type { Session } from "@/types";
import styles from "./UpdatesPanelHistory.module.css";

/**
 * "Undo 〈connection〉 changes" beside "Start restore", for an entry holding
 * changes made through a connection — an agent's key or an app. It undoes
 * what that connection changed on the page from the entry on and keeps what
 * others changed afterwards. Notion's history has no such action; it is the
 * an approved difference.
 */
export function PageHistoryRevertButtons({
  session, pageId, group, versions, snapshotFrontier, disabled, actorLabel, onReverted, onError,
}: {
  session: Session;
  pageId: string;
  group: PageHistoryGroup | undefined;
  actorLabel: (actorId: string) => string;
  versions: readonly PageHistoryVersion[];
  snapshotFrontier: number | null;
  disabled: boolean;
  onReverted: (result: PageHistoryRevertResponse, connection: string) => void;
  onError: (caught: unknown) => void;
}) {
  const { t } = useTranslation("updatesPanel");
  const [busy, setBusy] = useState(false);
  if (!group || snapshotFrontier === null) return null;
  const authors = group.authors.filter((author) => author.connectionName);
  return (
    <>
      {authors.map((author) => {
        // The entry's earliest change through the connection, from which
        // its changes are undone. The version that made the page made the
        // page itself, which stays: an entry of only that has no button.
        const from = versions.filter((version) => group.versionIds.includes(version.id) && version.source !== "baseline"
          && version.actorId === author.actorId && version.connectionName === author.connectionName).at(-1);
        if (!from) return null;
        return (
          <button key={`${author.actorId}:${author.connectionName}`} type="button" className={styles.historyStartRestore}
            disabled={disabled || busy} data-page-history-revert={author.connectionName}
            title={t("versions.revertConnectionHint", { connection: author.connectionName })}
            onClick={async () => {
              setBusy(true);
              try {
                const result = await revertPageHistoryChanges(session, pageId, from.id, {
                  baseRevision: snapshotFrontier, connection: true,
                });
                onReverted(result, author.connectionName);
              } catch (caught) {
                onError(caught);
              } finally {
                setBusy(false);
              }
            }}>
            <RestoreIcon size="small" aria-hidden="true" />
            {authors.filter((other) => other.connectionName === author.connectionName).length > 1
              ? t("versions.revertConnectionBy", { name: actorLabel(author.actorId), connection: author.connectionName })
              : t("versions.revertConnection", { connection: author.connectionName })}
          </button>
        );
      })}
    </>
  );
}

/**
 * What a revert did, in a line: how many it undid and how many stayed. An
 * app's changes are named as the app's ("현장 기록 앱이 바꾼 내용"), as its
 * run history window names what it did; others as the connection's.
 */
export function pageHistoryRevertStatus(
  t: (key: string, options?: Record<string, unknown>) => string,
  result: PageHistoryRevertResponse,
  connection: string,
  by: "connection" | "app" = "connection",
) {
  if (result.reverted === 0 && result.kept.length === 0) return t("updatesPanel:versions.revertNothing");
  const later = result.kept.filter((kept) => kept.reason === "changedLater" || kept.reason === "holdsLaterBlocks").length;
  const own = result.kept.length - later;
  return [
    ...(result.reverted ? [by === "app"
      ? t("updatesPanel:versions.revertedApp", { count: result.reverted, app: connection })
      : t("updatesPanel:versions.revertedConnection", { count: result.reverted, connection })] : []),
    ...(later ? [t("updatesPanel:versions.revertKept", { count: later })] : []),
    ...(own ? [t("updatesPanel:versions.revertKeptStructure", { count: own })] : []),
  ].join(". ");
}
