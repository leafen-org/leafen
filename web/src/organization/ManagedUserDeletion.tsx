import { useState } from "react";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import {
  previewManagedUserDeletion,
  restoreManagedUser,
  scheduleManagedUserDeletion,
  type ManagedUser,
  type ManagedUserDeletionPreview,
} from "./organizationManagedUsers";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import table from "@/components/settings/DeveloperSettings.module.css";
import styles from "@/components/settings/SettingsItemRows.module.css";
import { styles as settingsStyles } from "@/components/settings/workspaceSettingsOverviewStyles";
import { activeDateLocale } from "@/lib/i18n";

const ERROR_KEYS: Record<string, string> = {
  LEAFEN_MANAGED_USER_DELETION_CONFIRMATION: "managedUserDeleteConfirmMismatch",
  LEAFEN_MANAGED_USER_DELETION_BLOCKED: "managedUserDeleteBlocked",
  LEAFEN_MANAGED_USER_DELETION_STARTED: "managedUserDeletionStarted",
  LEAFEN_MANAGED_USER_PROTECTED: "managedUserProtected",
};

/**
 * A managed user's deletion on their profile, as Notion's (its help, read
 * 2026-10-02): permanent after a 30-day grace period in which it may be
 * cancelled and the person restored. Before scheduling it the admin sees
 * which of the person's workspaces pass to the organization's owner and
 * which go with them, and types their email.
 */
export function ManagedUserDeletion({ session, organizationId, user, onChange, onNotice }: {
  session: Session;
  organizationId: string;
  user: ManagedUser;
  onChange: (user: ManagedUser) => void;
  onNotice: (notice: string) => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [preview, setPreview] = useState<ManagedUserDeletionPreview | null>(null);
  const [email, setEmail] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const name = user.name || user.email;

  async function run(action: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await action();
    } catch (failure) {
      const key = failure instanceof LeafenApiError ? ERROR_KEYS[failure.code] : undefined;
      setError(key ? t(key) : settingsErrorMessage(failure, t("managedUserActionFailed")));
    } finally {
      setBusy(false);
    }
  }

  if (user.deletionDueAt > 0) {
    const date = new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long" }).format(new Date(user.deletionDueAt));
    return (
      <>
        <div className={settingsStyles.securityRow} data-managed-user-deletion="scheduled">
          <div className={settingsStyles.securityInstruction}>
            <strong>{t("managedUserDeletionScheduled")}</strong>
            <span>{t("managedUserDeletionScheduledDescription", { date })}</span>
          </div>
          <button type="button" className={settingsStyles.secondaryButton} disabled={busy} data-managed-user-restore
            onClick={() => void run(async () => {
              onChange(await restoreManagedUser(session, organizationId, user.id));
              onNotice(t("managedUserRestored", { name }));
            })}>
            {t("managedUserRestore")}
          </button>
        </div>
        {error ? <div className={settingsStyles.errorText} role="alert">{error}</div> : null}
      </>
    );
  }

  const blocked = preview !== null && preview.blocker !== "none";
  return (
    <>
      <div className={settingsStyles.securityRow} data-managed-user-deletion="none">
        <div className={settingsStyles.securityInstruction}>
          <strong>{t("managedUserDelete")}</strong>
          <span>{t("managedUserDeleteDescription")}</span>
        </div>
        {preview ? null : (
          <button type="button" className={settingsStyles.secondaryButton} disabled={busy} data-managed-user-delete
            onClick={() => void run(async () => {
              setEmail("");
              setPreview(await previewManagedUserDeletion(session, organizationId, user.id));
            })}>
            {t("managedUserDeleteButton")}
          </button>
        )}
      </div>
      {preview ? (
        <div className={styles.rowStack} data-managed-user-delete-confirm>
          {blocked ? <span className={settingsStyles.errorText}>{t("managedUserDeleteBlocked")}</span> : (
            <span className={styles.empty}>
              {[
                preview.transferred.length > 0 ? t("managedUserDeleteTransferred", { count: preview.transferred.length }) : "",
                preview.deleted.length > 0 ? t("managedUserDeleteDeleted", { count: preview.deleted.length }) : "",
              ].filter(Boolean).join(" ") || t("managedUserDeleteNoWorkspaces")}
            </span>
          )}
          <form className={styles.filters} onSubmit={(event) => {
            event.preventDefault();
            void run(async () => {
              onChange(await scheduleManagedUserDeletion(session, organizationId, user.id, email));
              setPreview(null);
              onNotice(t("managedUserDeleted", { name }));
            });
          }}>
            <input className={styles.nameInput} type="email" value={email} disabled={blocked} placeholder={user.email}
              aria-label={t("managedUserDeleteConfirmLabel")} onChange={(event) => setEmail(event.target.value)} />
            <span className={table.actions}>
              <button type="submit" className={table.danger} data-managed-user-delete-submit
                disabled={busy || blocked || email.trim().toLowerCase() !== user.email.toLowerCase()}>
                {t("managedUserDeleteConfirm")}
              </button>
              <button type="button" className={table.secondary} onClick={() => setPreview(null)}>{t("managedUsersCancel")}</button>
            </span>
          </form>
        </div>
      ) : null}
      {error ? <div className={settingsStyles.errorText} role="alert">{error}</div> : null}
    </>
  );
}
