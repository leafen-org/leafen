import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { confirmAction } from "@/components/ConfirmActionDialog";
import {
  changeManagedUser,
  logOutManagedUser,
  removeManagedUserFromExternalWorkspaces,
  resetManagedUserPassword,
  setManagedUserSuspended,
  type ManagedUser,
} from "./organizationManagedUsers";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import table from "@/components/settings/DeveloperSettings.module.css";
import { ManagedUserDeletion } from "./ManagedUserDeletion";
import styles from "@/components/settings/SettingsItemRows.module.css";
import { styles as settingsStyles } from "@/components/settings/workspaceSettingsOverviewStyles";
import { activeDateLocale } from "@/lib/i18n";
import { ManagedUserEmails } from "./ManagedUserEmails";

const ERROR_KEYS: Record<string, string> = {
  LEAFEN_MANAGED_USER_PROTECTED: "managedUserProtected",
  LEAFEN_MANAGED_USER_EMAIL_INVALID: "managedUserEmailInvalid",
  LEAFEN_MANAGED_USER_EMAIL_TAKEN: "managedUserEmailTaken",
};

/**
 * One managed user's profile on the dashboard, as Notion's (its help, read
 * 2026-10-02): their workspaces — those of other organizations marked — last
 * activity, 2-step verification and suspension, and the actions on them:
 * log out, suspend or unsuspend, delete (ManagedUserDeletion), change name
 * and email (on a verified domain), reset their password (a new one at
 * their next app start, an email telling them), and remove from external
 * workspaces.
 */
export function ManagedUserProfile({ session, organizationId, user, onChange, onBack }: {
  session: Session;
  organizationId: string;
  user: ManagedUser;
  onChange: (user: ManagedUser) => void;
  onBack: () => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(user.name);
  const [email, setEmail] = useState(user.email);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const external = user.workspaces.filter((workspace) => workspace.external);

  async function run(action: () => Promise<string>) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      setNotice(await action());
    } catch (failure) {
      const key = failure instanceof LeafenApiError ? ERROR_KEYS[failure.code] : undefined;
      setError(key ? t(key) : settingsErrorMessage(failure, t("managedUserActionFailed")));
    } finally {
      setBusy(false);
    }
  }

  // What ends their access or sign-in asks first in the common confirmation,
  // as every settings action that does; focus goes back to the pressed button.
  async function asked(question: { message: string; detail: string; confirmLabel: string }, action: () => Promise<string>) {
    if (await confirmAction({ ...question, danger: true })) await run(action);
  }

  function save(event: FormEvent) {
    event.preventDefault();
    const change: { name?: string; email?: string } = {};
    if (name.trim() !== user.name) change.name = name.trim();
    if (email.trim().toLowerCase() !== user.email.toLowerCase()) change.email = email.trim();
    if (!change.name && !change.email) {
      setEditing(false);
      return;
    }
    void run(async () => {
      const changed = await changeManagedUser(session, organizationId, user.id, change);
      onChange(changed);
      setEditing(false);
      return t("managedUserSaved");
    });
  }

  const lastActive = user.lastActiveAt > 0
    ? new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long" }).format(new Date(user.lastActiveAt))
    : t("managedUsersNever");
  const displayName = user.name || user.email;
  return (
    <div data-managed-user-profile={user.id}>
      <button type="button" className={styles.back} onClick={onBack} data-managed-user-back>{t("managedUserBack")}</button>
      <div className={settingsStyles.sectionTitle}>{displayName}</div>
      <p className={settingsStyles.organizationMeta}>{user.email}</p>
      <table className={table.tokens}>
        <tbody>
          <tr>
            <th scope="row">{t("managedUsersColumnWorkspaces")}</th>
            <td>
              {user.workspaces.length === 0 ? "—" : user.workspaces.map((workspace, index) => (
                <span key={workspace.id}>
                  {index > 0 ? ", " : null}
                  {workspace.name}
                  {workspace.external ? <span className={styles.badge}> ({t("managedUsersExternal")})</span> : null}
                </span>
              ))}
            </td>
          </tr>
          <tr><th scope="row">{t("managedUsersColumnLastActive")}</th><td>{lastActive}</td></tr>
          <tr>
            <th scope="row">{t("managedUsersColumnTwoStep")}</th>
            <td>{t(user.twoStep ? "managedUsersTwoStepTotp" : "managedUsersTwoStepNone")}</td>
          </tr>
          <tr>
            <th scope="row">{t("managedUserStatus")}</th>
            <td data-managed-user-status>
              {t(user.deletionDueAt > 0 ? "managedUserDeletionScheduled" : user.suspended ? "managedUsersSuspended" : "managedUserActive")}
            </td>
          </tr>
        </tbody>
      </table>

      <div className={`${styles.rowStack} ${styles.profileActions}`}>
        <div className={settingsStyles.securityRow}>
          <div className={settingsStyles.securityInstruction}>
            <strong>{t("managedUsersLogout")}</strong>
            <span>{t("managedUserLogoutDescription")}</span>
          </div>
          <button type="button" className={settingsStyles.secondaryButton} disabled={busy} data-managed-user-logout
            onClick={() => void run(async () => {
              await logOutManagedUser(session, organizationId, user.id);
              return t("managedUserLoggedOut", { name: displayName });
            })}>
            {t("managedUsersLogout")}
          </button>
        </div>

        {user.deletionDueAt > 0 ? null : (
          <div className={settingsStyles.securityRow}>
            <div className={settingsStyles.securityInstruction}>
              <strong>{t(user.suspended ? "managedUserUnsuspend" : "managedUserSuspend")}</strong>
              <span>{t(user.suspended ? "managedUserUnsuspendDescription" : "managedUserSuspendDescription")}</span>
            </div>
            {user.suspended ? (
              <button type="button" className={settingsStyles.secondaryButton} disabled={busy} data-managed-user-unsuspend
                onClick={() => void run(async () => {
                  onChange(await setManagedUserSuspended(session, organizationId, user.id, false));
                  return t("managedUserUnsuspended", { name: displayName });
                })}>
                {t("managedUserUnsuspendButton")}
              </button>
            ) : (
              <button type="button" className={settingsStyles.secondaryButton} disabled={busy} data-managed-user-suspend
                onClick={() => void asked({ message: t("managedUserSuspendQuestion", { name: displayName }),
                  detail: t("managedUserSuspendDescription"), confirmLabel: t("managedUserSuspendConfirm") }, async () => {
                  onChange(await setManagedUserSuspended(session, organizationId, user.id, true));
                  return t("managedUserSuspended", { name: displayName });
                })}>
                {t("managedUserSuspendButton")}
              </button>
            )}
          </div>
        )}
        <ManagedUserDeletion session={session} organizationId={organizationId} user={user} onChange={onChange} onNotice={setNotice} />
        <ManagedUserEmails session={session} organizationId={organizationId} userId={user.id} primaryEmail={user.email} onPrimary={(email) => {
          onChange({ ...user, email });
          setEmail(email);
        }} />
        <div className={settingsStyles.securityRow}>
          <div className={settingsStyles.securityInstruction}>
            <strong>{t("managedUserEdit")}</strong>
            <span>{t("managedUserEditDescription")}</span>
          </div>
          {editing ? null : (
            <button type="button" className={settingsStyles.secondaryButton} disabled={busy} data-managed-user-edit
              onClick={() => { setName(user.name); setEmail(user.email); setEditing(true); }}>
              {t("managedUserEditButton")}
            </button>
          )}
        </div>
        {editing ? (
          <form className={styles.filters} onSubmit={save} data-managed-user-edit-form>
            <input className={styles.nameInput} value={name} maxLength={100} aria-label={t("managedUsersColumnName")}
              placeholder={t("managedUsersColumnName")} onChange={(event) => setName(event.target.value)} />
            <input className={styles.nameInput} type="email" value={email} aria-label={t("managedUserEmail")}
              placeholder={t("managedUserEmail")} onChange={(event) => setEmail(event.target.value)} />
            <span className={table.actions}>
              <button type="submit" className={table.primary} disabled={busy || !name.trim() || !email.trim()}>{t("managedUserSave")}</button>
              <button type="button" className={table.secondary} onClick={() => setEditing(false)}>{t("managedUsersCancel")}</button>
            </span>
          </form>
        ) : null}

        <div className={settingsStyles.securityRow}>
          <div className={settingsStyles.securityInstruction}>
            <strong>{t("managedUserPasswordReset")}</strong>
            <span>{t("managedUserPasswordResetDescription")}</span>
          </div>
          <button type="button" className={settingsStyles.secondaryButton} disabled={busy} data-managed-user-password
            onClick={() => void asked({ message: t("managedUserPasswordResetQuestion", { name: displayName }),
              detail: t("managedUserPasswordResetDescription"), confirmLabel: t("managedUserPasswordResetConfirm") }, async () => {
              onChange(await resetManagedUserPassword(session, organizationId, user.id));
              return t("managedUserPasswordResetDone", { name: displayName });
            })}>
            {t("managedUserPasswordResetButton")}
          </button>
        </div>
        <div className={settingsStyles.securityRow}>
          <div className={settingsStyles.securityInstruction}>
            <strong>{t("managedUserExternal")}</strong>
            <span>{external.length === 0 ? t("managedUserExternalNone") : t("managedUserExternalDescription", { count: external.length })}</span>
          </div>
          <button type="button" className={settingsStyles.secondaryButton} disabled={busy || external.length === 0}
            data-managed-user-external onClick={() => void asked({ message: t("managedUserExternalQuestion", { name: displayName }),
              detail: t("managedUserExternalDescription", { count: external.length }), confirmLabel: t("managedUserExternalConfirm") }, async () => {
              const result = await removeManagedUserFromExternalWorkspaces(session, organizationId, user.id);
              onChange(result.user);
              return [t("managedUserExternalRemoved", { count: result.removed }),
                result.owned > 0 ? t("managedUserExternalOwned", { count: result.owned }) : ""].filter(Boolean).join(" ");
            })}>
            {t("managedUserExternalButton")}
          </button>
        </div>
        {notice ? <div className={styles.empty} role="status">{notice}</div> : null}
        {error ? <div className={settingsStyles.errorText} role="alert">{error}</div> : null}
      </div>
    </div>
  );
}
