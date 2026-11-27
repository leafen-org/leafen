import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import { enterpriseSAMLErrorCodes } from "@/enterpriseSAMLWire.generated";
import { signInThroughApp } from "@/lib/appSignIn";
import { authWords, type AuthLine, type AuthText } from "@/lib/authLine";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import type { Session } from "@/types";
import styles from "../AuthGate.module.css";

/**
 * "회사 계정으로 로그인" (the multi-server decision's 2026-10-20 "같은 날 다듬음",
 * as Notion's "Continue with SSO"): a work email's domain finds the
 * organization whose SAML sign-in it starts. An address with none gets one
 * line, whatever domains the server has (organization/enterprise_saml_find.go).
 * A packaged app on another server signs in in the system's browser, as with
 * the server's other sign-in providers.
 */
export function useCompanySignIn(options: {
  otherServer: boolean;
  setBusy(value: boolean): void;
  setError(value: AuthText): void;
  setNotice(value: AuthText): void;
  acceptSession(session: Session): Promise<void>;
  failure(caught: unknown): AuthLine;
  /** Opens this step. */
  open(): void;
}) {
  const [email, setEmail] = useState("");
  const [reason, setReason] = useState("");

  /**
   * A password sign-in the person's organization refuses for single sign-on
   * goes on here with the address kept, as Notion's email step sends an
   * enforced domain to SSO: one press of 계속 left, no address typed again.
   */
  function takeOver(caught: unknown, address: string) {
    if (!(caught instanceof LeafenApiError) || caught.code !== enterpriseSAMLErrorCodes.required) return false;
    options.open();
    setEmail(address.trim());
    setReason("organizationSSO");
    return true;
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!event.currentTarget.checkValidity()) {
      options.setError(authWords("authGate:enterValidEmail"));
      return;
    }
    options.setError("");
    options.setBusy(true);
    let organizationId = "";
    try {
      organizationId = (await api.findCompanySignIn(email.trim())).organizationId;
    } catch (caught) {
      options.setBusy(false);
      options.setError(caught instanceof LeafenApiError && caught.code === enterpriseSAMLErrorCodes.providerNotFound
        ? authWords("authGate:companyNotFound") : options.failure(caught));
      return;
    }
    if (!options.otherServer) {
      // Sign-in comes back to this page's server: the way back is a path
      // (organization/enterprise_saml_protocol.go validateSAMLRelayState).
      window.location.assign(api.authSAMLStartURL(organizationId, "/auth/saml/callback"));
      return;
    }
    options.setBusy(false);
    options.setNotice(authWords("authGate:finishInBrowser"));
    try {
      const returned = await signInThroughApp(api.authSAMLStartURL(organizationId, "/"), "samlCompletion");
      options.setNotice("");
      if (!returned) return;
      options.setBusy(true);
      await options.acceptSession(await api.exchangeSAMLCompletion(returned.code, returned.verifier));
    } catch (caught) {
      options.setNotice("");
      options.setBusy(false);
      options.setError(options.failure(caught));
    }
  }

  return { email, setEmail, submit, takeOver, reason, setReason };
}

export function CompanySignInStep({ busy, email, setEmail, submit, back, rejection, reason }: {
  busy: boolean;
  /** Why a password sign-in came here: said in place of the step's hint. */
  reason?: string;
  email: string;
  setEmail(value: string): void;
  submit(event: FormEvent<HTMLFormElement>): void;
  back(): void;
  /** The field's mark when the sign-in screen's red line is about it. */
  rejection: { "aria-invalid"?: true; "aria-describedby"?: string };
}) {
  const { t } = useTranslation(["authGate", "productErrors"]);
  return (
    <form className={styles.form} noValidate onSubmit={submit} data-auth-company>
      <div className={styles.notice}>
        <strong>{t("signInWithCompany")}</strong>
        <p data-auth-company-reason={reason || undefined}>{reason ? t(`productErrors:correctable.${reason}`) : t("companyHint")}</p>
      </div>
      <label className={styles.label} htmlFor="auth-company-email">{t("companyEmailLabel")}</label>
      <input
        id="auth-company-email"
        className={styles.input}
        type="email"
        autoComplete="email"
        value={email}
        onChange={(event) => setEmail(event.target.value)}
        placeholder="name@company.com"
        disabled={busy}
        autoFocus={fieldFocusOnOpen()}
        required
        {...rejection}
      />
      <button className={styles.primary} type="submit" disabled={busy || !email.trim()}>{t("continue")}</button>
      <button className={styles.linkButton} type="button" onClick={back} disabled={busy}>{t("back")}</button>
    </form>
  );
}
