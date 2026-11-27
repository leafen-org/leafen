import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { displayDateTime } from "@/lib/displayDate";
import type { Session } from "@/types";
import styles from "./AdministratorSettingsPage.module.css";
import own from "./AdministratorServerBuild.module.css";
import { SettingRow } from "./AdministratorSettingRows";

/** GET /admin/server-version (internal/app/mcp_tool_server_version.go). */
export type ServerVersion = {
  release: string;
  build: string;
  commit?: string;
  commitTime?: string;
  modified?: boolean;
  builtAt?: string;
  builtAtFrom?: "stamp" | "program file";
  schemaVersion: number;
  deployedAt?: string;
  startedAt: string;
  previous?: { build: string; release: string; commit?: string; schemaVersion: number; deployedAt: string; lastStartedAt: string };
  changes: { since?: string; known: boolean; count: number; listed: { commit: string; at: string; subject: string }[]; note?: string };
};

/**
 * The build the server runs, under its version in Settings → Server →
 * Updates (2026-11-16: a server said only "0.1.0-dev", and nobody could tell
 * whether a fix had reached it): the build and its commit, when it was built
 * and first started here, its data's schema version, and what changed since
 * the build it ran before — the product's own commit subjects, as an agent
 * reads them with leafen_server_version.
 */
export function AdministratorServerBuild({ session }: { session: Session }) {
  const { t } = useTranslation("serverConsole");
  const [version, setVersion] = useState<ServerVersion | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    void api.serverVersion(session, controller.signal).then(setVersion, () => undefined);
    return () => controller.abort();
  }, [session]);
  if (!version) return null;
  const { changes, previous } = version;
  const changesLine = !version.commit ? t("build.changesNoCommit")
    : !previous && !changes.since ? t("build.changesNoPrevious")
      : !changes.known ? t("build.changesUnknown")
        : changes.count === 0 ? t("build.changesNone") : t("build.changesCount", { count: changes.count });
  return (
    <section className={styles.section} aria-labelledby="administrator-build-title" data-server-build={version.build}>
      <div id="administrator-build-title" className={styles.sectionTitle}>{t("build.title")}</div>
      <div className={styles.settingRows}>
        <SettingRow title={t("build.build")} description={version.commit
          ? t("build.commit", { commit: version.commit.slice(0, 12), time: version.commitTime ? displayDateTime(version.commitTime) : "" })
          : t("build.noCommit")}>
          <code className={own.value}>{version.build}</code>
        </SettingRow>
        {version.builtAt ? (
          <SettingRow title={t("build.builtAt")} description={version.builtAtFrom === "program file" ? t("build.builtAtProgram") : undefined}>
            <span className={own.value}>{displayDateTime(version.builtAt)}</span>
          </SettingRow>
        ) : null}
        {version.deployedAt ? (
          <SettingRow title={t("build.deployedAt")} description={previous ? t("build.previous", { build: previous.build }) : undefined}>
            <span className={own.value}>{displayDateTime(version.deployedAt)}</span>
          </SettingRow>
        ) : null}
        <SettingRow title={t("build.schema")}><span className={own.value}>{version.schemaVersion}</span></SettingRow>
        <div className={own.changes} data-server-build-changes={changes.count}>
          <strong>{t("build.changes")}</strong>
          <span className={own.changesLine}>{changesLine}</span>
          {changes.listed.length ? (
            <details className={own.list}>
              <summary>{t("build.changesShow")}</summary>
              <ul>
                {changes.listed.map((change) => (
                  <li key={change.commit}><code>{change.commit.slice(0, 9)}</code> {change.subject}</li>
                ))}
              </ul>
              {changes.count > changes.listed.length ? <p className={own.changesLine}>{t("build.changesNewest", { count: changes.listed.length })}</p> : null}
            </details>
          ) : null}
        </div>
      </div>
    </section>
  );
}
