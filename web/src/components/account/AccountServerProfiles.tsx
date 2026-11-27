import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { AddServerAccountDialog } from "@/components/sidebar/AddServerAccountDialog";
import { isPackagedClient } from "@/lib/packagedClient";
import { listProfiles, LOCAL_PROFILE_ID, reachedAt, setProfileLabel, subscribeProfiles, type ServerProfile } from "@/lib/profileRegistry";
import { serverDisplayName } from "@/lib/serverName";
import { openProfile, signOutOfProfile } from "@/lib/profileSwitch";
import { beginAddingAccount, listServerAccounts, openOtherAccount, signOutOfOtherAccount, subscribeServerAccounts, type ServerAccount } from "@/lib/serverAccounts";
import { serverIdentity } from "@/lib/serverOrigin";
import { AccountSwitcherKeys } from "./AccountSwitcherKeys";
import { styles } from "./accountSettingsStyles";
import nameStyles from "@/components/settings/ServerNameField.module.css";
import own from "./AccountServerProfiles.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

// The host a server account is reached at now, which its row shows: the
// address its server moved to, not the one it was added at (its ID).
function hostOf(profile: Pick<ServerProfile, "id" | "address">): string {
  try {
    return new URL(reachedAt(profile)).host;
  } catch {
    return reachedAt(profile);
  }
}

/**
 * The server accounts this client knows, where settings adds one,
 * signs in on one again and removes one, and the person names a server for
 * themselves; the workspace menu switches. Each shows as "server · name"
 * (2026-10-20), its email and the address it is reached at now under it.
 */
export function AccountServerProfiles({ userId, onSwitcherKeysSettled }: { userId: string; onSwitcherKeysSettled?: () => void }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [profiles, setProfiles] = useState<ServerProfile[]>([]);
  // The other accounts signed in on this server, each a row beside its server's.
  const [accounts, setAccounts] = useState<ServerAccount[]>([]);
  const [adding, setAdding] = useState(false);
  const [renaming, setRenaming] = useState("");
  const activeId = serverIdentity();
  // The app lists its own server too; a browser is on one.
  const packaged = isPackagedClient();
  const refresh = useCallback(() => {
    setProfiles(listProfiles().filter((profile) => packaged || profile.id !== LOCAL_PROFILE_ID));
    setAccounts(listServerAccounts().filter((account) => account.userId !== userId));
  }, [packaged, userId]);
  useEffect(refresh, [refresh]);
  useEffect(() => subscribeProfiles(refresh), [refresh]);
  useEffect(() => subscribeServerAccounts(refresh), [refresh]);

  return (
    <section className={styles.section} role="region" aria-label={t("navServerAccounts")}>
      <div className={own.rows}>
        {profiles.map((profile) => {
          const local = profile.id === LOCAL_PROFILE_ID;
          const active = profile.id === activeId;
          const serverName = local ? t("serverAccountsThisDevice")
            : serverDisplayName({ name: profile.serverName, alias: profile.serverAlias }) || hostOf(profile);
          const person = local ? "" : profile.name || profile.email;
          const label = [profile.label || serverName, person].filter(Boolean).join(" · ");
          const detail = local ? t("serverAccountsLocal") : [profile.email, hostOf(profile)].filter((part) => part && part !== person).join(" · ");
          return (
            <div key={profile.id} className={styles.securityRow} data-server-profile={profile.id}>
              <div>
                <strong data-server-profile-name>{label}</strong>
                <span>{[active ? t("serverAccountsInUse") : "", detail].filter(Boolean).join(" · ")}</span>
                {renaming === profile.id ? (
                  <input className={`${nameStyles.input} ${nameStyles.stacked}`} defaultValue={profile.label} maxLength={40} placeholder={serverName} autoFocus
                    aria-label={t("serverAccountsLabel")} title={t("serverAccountsLabelHint")} data-server-profile-label
                    onKeyDown={(event) => { if (fieldEnter(event) || event.key === "Escape") event.currentTarget.blur(); }}
                    onBlur={(event) => {
                      setRenaming("");
                      if (event.currentTarget.value.trim() === profile.label) return;
                      setProfileLabel(profile.id, event.currentTarget.value);
                      refresh();
                    }} />
                ) : null}
              </div>
              <div className={styles.sectionActions}>
                <button type="button" className={styles.secondaryButton} title={t("serverAccountsLabelHint")} onClick={() => setRenaming(profile.id)} data-server-profile-rename>
                  {t("serverAccountsRename")}
                </button>
                {active ? null : (
                  <>
                    <button type="button" className={styles.secondaryButton} onClick={() => openProfile(profile.id)}>
                      {profile.email || profile.name || local ? t("serverAccountsOpen") : t("serverAccountsSignIn")}
                    </button>
                    {local ? null : (
                      <button
                        type="button"
                        className={styles.secondaryButton}
                        onClick={() => void signOutOfProfile(profile.id).then(refresh)}
                      >
                        {t("serverAccountsRemove")}
                      </button>
                    )}
                  </>
                )}
              </div>
            </div>
          );
        })}
        {accounts.map((account) => {
          const server = profiles.find((profile) => profile.id === activeId);
          const host = hostOf(server ?? { id: activeId, address: "" });
          const serverName = server?.label || (server ? serverDisplayName({ name: server.serverName, alias: server.serverAlias }) : "") || host;
          return (
            <div key={account.userId} className={styles.securityRow} data-server-account={account.userId}>
              <div>
                <strong data-server-profile-name>{[serverName, account.name || account.email].filter(Boolean).join(" · ")}</strong>
                <span>{[account.email, host].filter(Boolean).join(" · ")}</span>
              </div>
              <div className={styles.sectionActions}>
                <button type="button" className={styles.secondaryButton} onClick={() => void openOtherAccount(account.userId)}>
                  {t("serverAccountsOpen")}
                </button>
                <button type="button" className={styles.secondaryButton} onClick={() => signOutOfOtherAccount(account.userId)}>
                  {t("serverAccountsRemove")}
                </button>
              </div>
            </div>
          );
        })}
        <div className={styles.securityRow} data-server-account-add>
          <div className={styles.securityInstruction}>
            <strong>{t("serverAccountsAddTitle")}</strong>
            <span>{t("serverAccountsAddBody")}</span>
          </div>
          <button type="button" className={styles.secondaryButton} onClick={() => {
            // This device's own server has no sign-in: only one on another server is added from it.
            if (activeId === LOCAL_PROFILE_ID) setAdding(true);
            else beginAddingAccount();
          }}>
            {t("serverAccountsAdd")}
          </button>
        </div>
      </div>
      <AccountSwitcherKeys onSettled={onSwitcherKeysSettled} />
      {adding ? <AddServerAccountDialog onClose={() => setAdding(false)} /> : null}
    </section>
  );
}
