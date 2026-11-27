import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import type { AccountEmailState } from "@/authWire.generated";
import type { Session } from "@/types";
import { accountEmailErrorMessage, accountEmailsShownBehind } from "@/components/account/accountEmailErrorMessage";
import { confirmAction } from "@/components/ConfirmActionDialog";
import table from "@/components/settings/DeveloperSettings.module.css";
import styles from "@/components/settings/SettingsItemRows.module.css";
import local from "@/components/settings/SettingsRows.module.css";
import { SettingsRowMenu } from "@/components/settings/SettingsRowMenu";
import { styles as settingsStyles } from "@/components/settings/workspaceSettingsOverviewStyles";
import { Plus } from "@/icons/leafen";
import { ReadFailed } from "@/components/ReadFailed";

/** An organization's administrator manages only its verified addresses. */
export function ManagedUserEmails({ session, organizationId, userId, primaryEmail, onPrimary }: {
  session: Session; organizationId: string; userId: string;
  /** The person's address as the page has it: read again when it changes. */
  primaryEmail: string;
  onPrimary: (email: string) => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [state, setState] = useState<AccountEmailState | null>(null);
  const [email, setEmail] = useState("");
  const [adding, setAdding] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [readTurn, setReadTurn] = useState(0);
  const generation = useRef(0);
  const shownPath = useRef("");
  const path = `/organizations/${encodeURIComponent(organizationId)}/managed-users/${encodeURIComponent(userId)}/emails`;

  useEffect(() => {
    const issued = ++generation.current;
    const controller = new AbortController();
    // Another person's page starts empty; the same person's keeps what it
    // shows until the new read lands, so nothing below it moves.
    if (shownPath.current !== path) {
      shownPath.current = path;
      setState(null);
    }
    setBusy(false);
    setError("");
    setAdding(false);
    setEmail("");
    request<AccountEmailState>(path, { signal: controller.signal }, session).then((result) => {
      if (issued === generation.current) setState(result);
    }).catch((failure) => { if (!controller.signal.aborted && issued === generation.current) setError(accountEmailErrorMessage(failure, (key) => t(key), "accountEmails.read")); });
    return () => { generation.current += 1; controller.abort(); };
  }, [path, readTurn, session, t, primaryEmail]);

  async function change(address: string, method: "POST" | "PUT" | "DELETE") {
    if (!state || busy) return;
    const issued = generation.current;
    setBusy(true);
    setError("");
    try {
      const result = await request<AccountEmailState>(path, { method, body: JSON.stringify({ email: address, baseRevision: state.revision }) }, session);
      if (issued !== generation.current) return;
      setState(result);
      setAdding(false);
      setEmail("");
      if (method === "PUT") onPrimary(address);
    } catch (failure) {
      if (issued !== generation.current) return;
      // A list older than the server's is read again, with what happened (accountEmailErrorMessage.ts).
      if (accountEmailsShownBehind(failure)) {
        const now = await request<AccountEmailState>(path, {}, session).catch(() => null);
        if (issued !== generation.current) return;
        if (now) setState(now);
      }
      setError(accountEmailErrorMessage(failure, (key) => t(key)));
    } finally { if (issued === generation.current) setBusy(false); }
  }

  // Asked first, as the person's own 이메일 관리하기 asks, saying what follows.
  async function confirmed(address: string, primary: boolean) {
    if (!await confirmAction({
      message: t(primary ? "emailMakePrimaryOutcome" : "emailRemoveOutcome", { email: address }),
      confirmLabel: t(primary ? "emailMakePrimary" : "enterpriseRemove"), danger: !primary,
    })) return;
    await change(address, primary ? "PUT" : "DELETE");
  }

  // A row with its addresses below, as 인증된 도메인 above it: "+ 이메일 추가"
  // at the row's end, each address's actions behind its "…". What is shown is
  // read as the person page opens; no 새로고침.
  return <div className={local.rowBlock} data-managed-user-emails>
    <div className={settingsStyles.securityRow}>
      <div className={settingsStyles.securityInstruction}>
        <strong>{t("emailManagementTitle")}</strong>
        <span>{t("emailPrimaryDescription")}</span>
      </div>
      {state?.canAdd && !adding ? (
        <button type="button" className={local.addLink} disabled={busy} data-managed-user-email-add onClick={() => setAdding(true)}>
          <Plus size="small" aria-hidden="true" />{t("emailAdd")}
        </button>
      ) : null}
    </div>
    {adding ? <form className={styles.filters} onSubmit={(event) => { event.preventDefault(); void change(email, "POST"); }}>
      <input type="email" value={email} required disabled={busy} aria-label={t("emailAddress")} placeholder={t("emailAddress")}
        autoFocus onChange={(event) => setEmail(event.target.value)} />
      <button className={table.primary} type="submit" disabled={busy}>{t("managedUserSave")}</button>
      <button className={table.secondary} type="button" disabled={busy} onClick={() => setAdding(false)}>{t("managedUsersCancel")}</button>
    </form> : null}
    {state ? (
      <table className={local.table}>
        <tbody>
          {state.emails.map((address) => (
            <tr key={address.email} data-managed-user-email={address.email}>
              <td>{address.email}{address.primary ? <span className={styles.badge}> ({t("emailPrimary")})</span> : null}</td>
              <td className={local.actions}>
                {address.primary ? null : (
                  <SettingsRowMenu label={t("organizationMemberMenu", { name: address.email })} rows={[
                    { key: "primary", label: t("emailMakePrimary"), disabled: busy, run: () => void confirmed(address.email, true) },
                    { key: "remove", label: t("enterpriseRemove"), danger: true, disabled: busy, run: () => void confirmed(address.email, false) },
                  ]} />
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    ) : null}
    {/* Not read yet: what failed, with a way to read it again. */}
    {error && !state ? <ReadFailed message={error} onRetry={() => setReadTurn((turn) => turn + 1)} />
      : error ? <p className={styles.empty} role="alert">{error}</p> : null}
  </div>;
}
