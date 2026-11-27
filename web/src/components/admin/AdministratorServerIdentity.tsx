import { useTranslation } from "react-i18next";
import { serverDisplayName } from "@/lib/serverName";
import styles from "./AdministratorSettingsPage.module.css";
import { SettingField, SettingRow } from "./AdministratorSettingRows";
import { styles as rows } from "@/components/settings/workspaceSettingsOverviewStyles";
import { useCopyNotice } from "@/lib/copyNotice";

/**
 * The server's name, which people and their agents tell servers apart by —
 * its friendly alias until the administrators name it (server_name.go) —
 * and its own number, which the organization license's notices and its
 * seller name this server by, with a copy.
 */
export function AdministratorServerIdentity({ serverId, name, alias, busy, onNameChange }: {
  serverId: string;
  name: string;
  alias: string;
  busy: boolean;
  onNameChange(name: string): void;
}) {
  const { t } = useTranslation("serverConsole");
  const copy = useCopyNotice();
  if (!serverId) return null;
  const aliasName = serverDisplayName({ name: "", alias });
  // 설정 › 서버 › 일반, as 설정 › 일반's name (2026-11-05: in 업데이트, where a first administrator didn't look).
  return (
    <section className={styles.section} aria-labelledby="administrator-server-identity-title" data-server-identity>
      <div id="administrator-server-identity-title" className={styles.sectionTitle}>{t("identity.group")}</div>
      <div className={styles.settingRows}>
        <div data-server-name>
          <SettingField title={t("identity.name")} description={t("identity.nameMeta", { alias: aliasName })}>
            <input value={name} maxLength={40} placeholder={aliasName} disabled={busy}
              onChange={(event) => onNameChange(event.target.value)} />
          </SettingField>
        </div>
        <SettingRow title={t("identity.title")} description={t("identity.meta")}>
          <span className={rows.domainRecord}>
            <code data-server-id>{serverId}</code>
            <button type="button" className={rows.memberAction} onClick={() => void copy(serverId)}>{t("identity.copy")}</button>
          </span>
        </SettingRow>
      </div>
    </section>
  );
}
