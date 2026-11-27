import { type FormEvent, type MutableRefObject, useEffect, useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { confirmAction } from "@/components/ConfirmActionDialog";
import type { useAccountSecurity } from "./useAccountSecurity";
import { PasswordIcon } from "@/icons/leafen";
import { AccountSecurityDialog } from "./AccountSecurityDialog";
import cardStyles from "./AccountCardDialog.module.css";
import { styles } from "./accountSettingsStyles";

/**
 * The password, a backup sign-in (the multi-server decision of 2026-10-20):
 * an account whose person knows none sets one, with no current password and
 * no device signed out; one set is changed with it, or removed, leaving the
 * account to its devices. It signs in by the account's email, which the row
 * asks for where there is none.
 */
export function AccountPasswordSection({ security, email, onOpenChange, addPasskey, emailBelow = false }: {
  security: ReturnType<typeof useAccountSecurity>; email: string; onOpenChange(open: boolean): void;
  /** The email's row comes later, under the account (a phone's preferences hold this row). */
  emailBelow?: boolean;
  /** The passkey row's add, which the card offers under its button, as Notion's offer to set up a passkey for faster sign-in. */
  addPasskey?: MutableRefObject<(() => void) | null>;
}) {
  const fieldId = useId();
  const { t } = useTranslation("workspaceSettingsDialog");
  const [open, setOpen] = useState(false);
  const errorId = useId();
  // The field a refusal is about, tied to the error line.
  const marked = (field: "current" | "new" | "confirm") => security.passwordInvalid === field && security.error
    ? { "aria-invalid": true, "aria-describedby": errorId } : {};
  // Whether one is set, from the account read; its row's buttons wait for that read.
  const set = security.snapshot?.passwordSet ?? true;
  const busy = security.busy === "password:change" || security.busy === "password:set";
  useEffect(() => () => { security.clearPasswordInput(); onOpenChange(false); }, []);
  function changeOpen(next: boolean) {
    security.clearPasswordInput();
    setOpen(next); onOpenChange(next);
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (await (set ? security.changePassword() : security.setPassword())) changeOpen(false);
  }
  async function forget() {
    if (!await confirmAction({ message: t("forgetPasswordConfirm"), detail: t("forgetPasswordConfirmDetail"),
      confirmLabel: t("forgetPasswordButton"), danger: true })) return;
    await security.forgetPassword();
  }
  const title = set ? t("changePasswordButton") : t("setPasswordTitle");
  return <div className={styles.securityPanel} data-account-password={set ? "set" : "unset"}>
    <div className={styles.securityRow}>
      <div className={styles.securityInstruction}>
        <strong>{t("passwordTitle")}</strong>
        {/* Set or not, and its buttons, are the account read's: none until it is in (2026-11-20). */}
        {security.snapshot ? <span>{set ? t("passwordDesc") : t("passwordUnsetDesc")}</span> : null}
        {!email ? <span data-password-needs-email>{t(emailBelow ? "passwordNeedsEmailBelow" : "passwordNeedsEmail")}</span> : null}
      </div>
      {security.snapshot ? <div className={styles.sectionActions}>
        {set ? <button className={styles.secondaryButton} disabled={Boolean(security.busy)} onClick={() => void forget()}>{t("forgetPasswordButton")}</button> : null}
        <button className={styles.secondaryButton} disabled={Boolean(security.busy)} onClick={() => changeOpen(true)}>{set ? title : t("setPasswordButton")}</button>
      </div> : null}
    </div>
    {/* Notion's password card (seen 2026-10-30, 350px): its mark, the rule as its words, the fields, one wide button and the passkey link; × closes it. */}
    {open ? <AccountSecurityDialog title={title} busy={busy} onClose={() => changeOpen(false)}
      card={{ mark: <PasswordIcon size="card" />, words: <span data-password-rule>{t("passwordRule")}</span>, width: 350, closable: true }}>
      <form className={cardStyles.fields} onSubmit={(event) => void submit(event)}>
        {set ? <><label className={cardStyles.fieldName} htmlFor={`${fieldId}-current`}>{t("currentPassword")}</label>
          <input id={`${fieldId}-current`} className={cardStyles.field} type="password" autoComplete="current-password" placeholder={t("currentPassword")} {...marked("current")}
            value={security.passwordCurrent} onChange={(event) => security.setPasswordCurrent(event.target.value)} disabled={busy} /></> : null}
        <label className={cardStyles.fieldName} htmlFor={`${fieldId}-new`}>{t("newPasswordEnter")}</label>
        <input id={`${fieldId}-new`} className={cardStyles.field} type="password" autoComplete="new-password" placeholder={t("newPassword")} {...marked("new")}
          value={security.passwordNew} onChange={(event) => security.setPasswordNew(event.target.value)} disabled={busy} />
        <label className={cardStyles.fieldName} htmlFor={`${fieldId}-confirm`}>{t("confirmNewPassword")}</label>
        <input id={`${fieldId}-confirm`} className={cardStyles.field} type="password" autoComplete="new-password" placeholder={t("confirmPasswordPlaceholder")} {...marked("confirm")}
          value={security.passwordConfirm} onChange={(event) => security.setPasswordConfirm(event.target.value)} disabled={busy} />
        {security.error ? <p id={errorId} className={cardStyles.error} role="alert">{security.error}</p> : null}
        <div className={cardStyles.stack}>
          <button type="submit" className={cardStyles.wide} data-kind="primary" disabled={busy}>{title}</button>
          {addPasskey?.current ? <button type="button" className={cardStyles.wide} data-kind="quiet" disabled={busy}
            onClick={() => { const add = addPasskey.current; changeOpen(false); add?.(); }}>{t("passwordPasskeyHint")}</button> : null}
        </div>
      </form>
    </AccountSecurityDialog> : null}
  </div>;
}
