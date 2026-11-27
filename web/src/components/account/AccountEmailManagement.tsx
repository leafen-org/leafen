import { FormEvent, Fragment, useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { EnvelopeIcon, InfoIcon, ShieldCheckIcon } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { fetchRuntimeConfig } from "@/lib/runtimeConfigService";
import { useOptionalProductToast } from "@/lib/productToast";
import type { Session } from "@/types";
import { useAccountEmails } from "./useAccountEmails";
import { styles as accountStyles } from "./accountSettingsStyles";
import styles from "./AccountEmailManagement.module.css";
import cardStyles from "./AccountCardDialog.module.css";

type EmailAction = { kind: "add" | "primary" | "remove"; email: string };

export function AccountEmailManagement({ session, onSession }: {
  session: Session; onSession: (session: Session) => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const emails = useAccountEmails(session, onSession);
  const toast = useOptionalProductToast();
  const [action, setAction] = useState<EmailAction | null>(null);
  const addRef = useRef<HTMLButtonElement>(null);
  const returnFocus = useRef<HTMLButtonElement | null>(null);
  // Opened from Settings › 계정's row, which it replaces, it takes no focus:
  // the row's button goes with it (data-focus-leaves), nothing is ringed, a
  // letter typed goes nowhere and Tab starts at the window's first field
  // ("검색 설정"). Notion's, measured 2026-11-16 in two Chrome tabs at 1440,
  // dark, with no script before the press: after "이메일 관리" no ring, "z"
  // went nowhere (the focus the page's body), Tab went to "검색 설정".

  // An address already taken away elsewhere closes its window, and what
  // happened stays said below the list as it is now (useAccountEmails.ts).
  function close(keepNotice = false) {
    setAction(null);
    if (!keepNotice) emails.clearError();
    window.requestAnimationFrame(() => (returnFocus.current?.isConnected ? returnFocus.current : addRef.current)?.focus());
  }

  return <div className={accountStyles.adminConsole} data-account-email-management>
    <section className={styles.section} role="region" aria-label={t("emailManagementTitle")}>
      <h2 className={styles.heading}>{t("emailManagementTitle")}</h2>
      <p className={styles.lead}>{t("emailManagementDescription")}</p>
      {/* An account with no address yet has no primary one to send to (an
          email-less account, 2026-11-06); it shows once one is added. */}
      {emails.state?.emails.length === 0 ? null
        : <p className={`${styles.callout} ${styles.primaryNotice}`}><InfoIcon size="small" aria-hidden="true" />{t("emailPrimaryDescription")}</p>}
      <div className={styles.tableHeader}>
        <button ref={addRef} className={styles.add} type="button" disabled={emails.busy || !emails.state?.canAdd}
          onClick={(event) => { returnFocus.current = event.currentTarget; emails.clearError(); setAction({ kind: "add", email: "" }); }}>
          {t("emailAddress")}
        </button>
      </div>
      <table className={styles.addresses} aria-label={t("emailManagementTitle")} aria-busy={emails.busy}><tbody>
        {emails.state?.emails.map((address) => <tr className={styles.address} key={address.email}>
          <td className={styles.email} colSpan={address.primary ? 2 : undefined}>{address.email}{address.primary ? <span className={styles.badge}> ({t("emailPrimary")})</span> : null}</td>
          {!address.primary ? <td><div className={styles.actions}>
            <button type="button" disabled={emails.busy} onClick={(event) => { returnFocus.current = event.currentTarget; emails.clearError(); setAction({ kind: "primary", email: address.email }); }}>{t("emailMakePrimary")}</button>
            {/* An address goes at once, no window asking first, as Notion's (seen 2026-11-09: "이메일 주소를 성공적으로 제거했습니다."). */}
            <button type="button" disabled={emails.busy} onClick={() => {
              emails.clearError();
              void emails.change(address.email, false).then((done) => {
                // One taken away elsewhere already says so below the list instead.
                if (done === "done") toast?.notify(t("emailRemoved"));
                window.requestAnimationFrame(() => addRef.current?.focus({ preventScroll: true }));
              });
            }}>{t("emailRemove")}</button>
          </div></td> : null}
        </tr>)}
      </tbody></table>
      {emails.state && !emails.state.canAdd ? <p className={styles.description}>{emails.state.emails.length >= emails.state.limit + 1 ? t("emailLimit") : t("emailManaged")}</p> : null}
      {emails.error && !action ? <div className={accountStyles.notice} role="alert">{emails.error}</div> : null}
    </section>
    {action ? <EmailActionDialog action={action} emails={emails} onClose={() => close()} onGone={() => close(true)} /> : null}
  </div>;
}

function EmailActionDialog({ action, emails, onClose, onGone }: {
  action: EmailAction; emails: ReturnType<typeof useAccountEmails>; onClose: () => void; onGone: () => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const toast = useOptionalProductToast();
  const titleId = useId();
  const errorId = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const [email, setEmail] = useState(action.email);
  const [code, setCode] = useState("");
  const [currentCode, setCurrentCode] = useState("");
  // An address already on the account says so, not that another account has it (2026-10-16).
  const [ownAddress, setOwnAddress] = useState(false);
  const verifying = action.kind === "add" && emails.pending?.email === email.trim().toLowerCase();
  // The account's own address took a code too, as Notion's asks for both (seen 2026-11-09).
  const currentEmail = verifying ? emails.pending?.currentEmail ?? "" : "";
  // A server that sends no mail adds the address at once, unconfirmed (2026-10-20).
  const [noMail, setNoMail] = useState(false);
  useEffect(() => {
    if (action.kind !== "add") return;
    let live = true;
    void fetchRuntimeConfig().then(({ config }) => { if (live) setNoMail(!config.emailRecoveryAvailable); });
    return () => { live = false; };
  }, [action.kind]);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: () => { if (!emails.busy) onClose(); } });
  useEffect(() => { inputRef.current?.focus(); }, [verifying]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (verifying) {
      // An address added says so, as Notion's "이메일 주소를 추가했습니다!" (seen 2026-11-09).
      if (await emails.confirm(code, currentCode)) { toast?.notify(t("emailAdded")); onClose(); }
    } else if (action.kind === "add") {
      const typed = email.trim().toLowerCase();
      if (emails.state?.emails.some((address) => address.email.toLowerCase() === typed)) { setOwnAddress(true); return; }
      if (await emails.add(email.trim()) === "done") { toast?.notify(t("emailAdded")); onClose(); }
    } else {
      const done = await emails.change(email, action.kind === "primary");
      if (done === "gone") onGone();
      else if (done) onClose();
    }
  }

  const title = action.kind === "add" ? t("emailAddress") : action.kind === "primary" ? t("emailMakePrimary") : t("enterpriseRemove");
  // The field shown is the one a refusal is about; focus goes back to it (lib/focusThroughWaits.ts).
  const refused = ownAddress || emails.error ? { "aria-invalid": true as const, "aria-describedby": errorId } : {};
  // Notion's email card (seen 2026-10-30, 400px): an envelope over the title and its words, the named field, and its buttons at the end.
  return createPortal(<div className={styles.backdrop} style={{ zIndex }} onMouseDown={(event) => { if (event.target === event.currentTarget && !emails.busy) onClose(); }}>
    <section ref={dialogRef} className={`${styles.dialog} ${cardStyles.card}`} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={emails.busy}>
      {/* Its codes asked for, Notion's "이메일 주소 인증" card: a shield over the title (seen 2026-11-09). */}
      <span className={cardStyles.mark} aria-hidden="true">{verifying ? <ShieldCheckIcon size="card" /> : <EnvelopeIcon size="card" />}</span>
      <h2 id={titleId} className={cardStyles.title}>{verifying ? t("emailVerifyTitle") : title}</h2>
      <p className={cardStyles.words}>{verifying ? t(currentEmail ? "emailCodesDescription" : "emailCodeDescription") : action.kind === "add" ? t(noMail ? "emailAddWithoutMail" : "emailAddDescription")
        : t(action.kind === "primary" ? "emailMakePrimaryOutcome" : "emailRemoveOutcome", { email })}</p>
      <form className={cardStyles.fields} onSubmit={(event) => void submit(event)}>
        {verifying ? <>{[{ id: "field", to: email.trim(), value: code, set: setCode },
          ...(currentEmail ? [{ id: "current", to: currentEmail, value: currentCode, set: setCurrentCode }] : [])].map((field, index) => <Fragment key={field.id}>
          <label className={cardStyles.fieldName} data-strong="true" htmlFor={`${titleId}-${field.id}`}>{t("emailCodeSentTo", { email: field.to })}</label>
          <input id={`${titleId}-${field.id}`} ref={index === 0 ? inputRef : undefined} className={cardStyles.field} data-large="true"
            autoComplete="one-time-code" maxLength={6} placeholder={t("emailCodePlaceholder")} data-email-code={field.id}
            value={field.value} onChange={(event) => { field.set(event.target.value); emails.clearError(); }} disabled={emails.busy} {...refused} />
        </Fragment>)}</> : <>
          {action.kind === "add" ? <><label className={cardStyles.fieldName} data-strong="true" htmlFor={`${titleId}-field`}>{t("emailAddress")}</label>
            <input id={`${titleId}-field`} ref={inputRef} className={cardStyles.field} data-large="true" type="email" autoComplete="email" required
              placeholder={t("emailPlaceholder")} value={email} disabled={emails.busy} {...refused}
              onChange={(event) => { setEmail(event.target.value); setOwnAddress(false); emails.clearError(); }} /></> : null}
        </>}
        {ownAddress || emails.error ? <p id={errorId} className={cardStyles.error} role="alert">{ownAddress ? t("emailAlreadyYours") : emails.error}</p> : null}
        <div className={cardStyles.end}>
          <button type="button" data-kind="quiet" disabled={emails.busy} onClick={onClose}>{t("emailCancel")}</button>
          <button data-kind={action.kind === "remove" ? "danger" : "primary"} type="submit"
            disabled={emails.busy || (verifying && (code.trim().length !== 6 || (Boolean(currentEmail) && currentCode.trim().length !== 6)))}>
            {verifying ? t("emailVerify") : action.kind === "add" ? t(noMail ? "emailAddNow" : "emailAdd") : title}
          </button>
        </div>
      </form>
    </section>
  </div>, document.body);
}
