import { usePageLoginWall } from "./usePageLoginWall";
import { PageWallBackdrop, PageWallHead, pageWallStyles as wall } from "./PageLoginWall";
import { type FormEventHandler, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AuthProviderOption } from "@/lib/authProviderService";
import type { Session } from "@/types";
import styles from "../AuthGate.module.css";
import { authBounds } from "@/authWire.generated";
import { QrCodeIcon } from "@/icons/leafen/deviceIcons";
import type { DeviceOwnerSignInState } from "./useDeviceOwnerSignIn";
import { CompanySignInStep } from "./CompanySignIn";
import { SetupBackupsOff, SetupRestore } from "./SetupRestore";
import restoreStyles from "./SetupRestore.module.css";
import { SetupCodeField, SetupOutsideGuide, useSetupStart } from "./SetupOutsideGuide";
import { useChosenSuggestion } from "@/lib/useChosenSuggestion";
import { useViewEntryFocus } from "@/lib/viewEntryFocus";
import { CODE_KEYBOARD, inFours } from "./codeKeyboard";
import { useAgentStartSignIn } from "@/lib/agentStartRequest";
import { coverDocumentChrome } from "@/lib/documentChrome";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { AgentSignInStep } from "./AgentSignInStep";
import { NoDeviceHelp } from "./NoDeviceHelp";
import { SignInWays } from "./SignInWays";
import { FirstRunLanguageStep } from "./LanguageStep";
import { firstRunLanguage } from "@/lib/firstRunLanguage";
import { suggestedNameIn } from "@/lib/suggestedNames";
import { invitationLead, type InvitationDetails } from "./invitationLead";
import { SignInLanguage } from "./SignInLanguage";
import { LegalNotice } from "../LegalNotice";
import { AuthBrand } from "./AuthBrand";

export type { InvitationDetails } from "./invitationLead";

export type AuthGateStep =
  | "setup"
  // The ways to sign in: a signed-in device's code first, an agent's
  // approval right under it, then email and passkey (SignInWays.tsx).
  | "signin"
  // A code for the person's agent to approve, this browser signing in (AgentSignInStep.tsx).
  | "agent"
  // The email and password of a backup sign-in (2026-10-20).
  | "email"
  | "signup"
  | "mfa"
  | "verification-sent"
  | "password-reset-request"
  | "password-reset"
  | "auth-action-result"
  // A code another signed-in device shows, to join it (account_device_codes.go).
  | "device-code"
  // An invitation opened: a name, and in (auth_invitation_join.go).
  | "invitation"
  // A work email that finds its company's sign-in (CompanySignIn.tsx).
  | "company";

export type AuthGateScreenProps = {
  step: AuthGateStep;
  /** Named after the page a link brought someone to, before any sign-in here (usePageLoginWall). */
  pageWall?: boolean;
  busy: boolean;
  error: string;
  /** The reset link can't be used any more: a new one is offered in place of its fields. */
  resetLinkDead?: boolean;
  notice: string;
  /** Sign-up is closed and only an invitation's link lets one in: no form to fill. */
  signupFormClosed?: boolean;
  displayName: string;
  email: string;
  password: string;
  passwordConfirm: string;
  mfaCode: string;
  mfaMode: "totp" | "recovery";
  sessionCapacityOffered: boolean;
  emailRecoveryAvailable: boolean;
  providers: AuthProviderOption[];
  /** Sign-up is open here, so the small "계정 만들기" is offered. */
  /** An organization here signs in with SAML: "회사 계정으로 로그인" is offered. */
  companySignIn: boolean;
  company: { email: string; setEmail(value: string): void; submit: FormEventHandler<HTMLFormElement>; reason: string; setReason(value: string): void };
  /** The packaged app's way back to its own profile from another server's sign-in. */
  /** Adding an account: its window's title in place of the brand. */
  inWindow?: boolean;
  onLeaveServer?: () => void;
  /** What leaving says, where it isn't "this device" (a phone's start screen). */
  leaveServerLabel?: string;
  /** Signed in already, for an auth link's step: a result goes back to the workspace, not to sign-in. */
  signedIn?: boolean;
  /** The password was just reset: the result signs in with it by email. */
  passwordReset?: boolean;
  setDisplayName(value: string): void;
  setEmail(value: string): void;
  setPassword(value: string): void;
  setPasswordConfirm(value: string): void;
  /** The first setup's one-time code, from the server's setup link or typed. */
  setupCode: string;
  setupCodeNeeded: boolean;
  setSetupCode(value: string): void;
  deviceCode: string;
  setDeviceCode(value: string): void;
  submitDeviceCode: FormEventHandler<HTMLFormElement>;
  /** Goes in as the account whose agent approved this browser (AgentSignInStep). */
  acceptAgentSignIn(session: Session): void;
  /** A phone app or a browser can scan the other device's QR code. */
  scanAvailable: boolean;
  /** The packaged app's sign-in through the system's browser, on a server it connects to. */
  browserSignIn?: () => void;
  /** The link opening this page in the Leafen app, in a browser. */
  openInApp?: string;
  startScan(): void;
  setMfaCode(value: string): void;
  setMfaMode(value: "totp" | "recovery"): void;
  switchStep(step: AuthGateStep): void;
  /** Back to the step a reset asked or a second step came from. */
  back(): void;
  dismissSessionCapacity(): void;
  submitInstanceSetup: FormEventHandler<HTMLFormElement>;
  /** Signs in with the one-time code an agent's conversation left for this setup screen once it set the server up. */
  joinAgentStart(code: string): void;
  /** Goes on as the account another of the person's tabs set the server up as (useAgentStartSignIn). */
  setUpElsewhere(session: Session): void;
  /** The address an email invitation was sent to, which the account takes. */
  invitationEmail: string;
  /** Who invited, to what, and their note, as the invitation's mail would say. */
  invitationDetails: InvitationDetails;
  submitInvitationJoin: FormEventHandler<HTMLFormElement>;
  /** "이 기기로 들어가기" on the app's own server (desktop/device_owner.go). */
  deviceOwner: DeviceOwnerSignInState;
  submitPassword: FormEventHandler<HTMLFormElement>;
  submitMfa: FormEventHandler<HTMLFormElement>;
  submitPasswordResetRequest: FormEventHandler<HTMLFormElement>;
  submitPasswordReset: FormEventHandler<HTMLFormElement>;
  replaceOldestSessionAndSignIn(): void;
  startOAuth(provider: string): void;
  passkeyAvailable: boolean;
  passkeyRunning: boolean;
  passkeyReason: string;
  startPasskey(): void;
  cancelPasskey(): void;
};

const ERROR_ID = "auth-gate-error";

export function AuthGateScreen(props: AuthGateScreenProps) {
  const { t } = useTranslation(["authGate", "common", "workspaceSettingsDialog"]);
  const waitingIn = usePageLoginWall(props.pageWall === true);
  const {
    step, busy, error, notice, displayName, email, password, passwordConfirm,
    mfaCode, mfaMode, sessionCapacityOffered, emailRecoveryAvailable,
  } = props;
  // A step opens with its field focused, but on a touch screen, where the
  // field waits for a tap and the keyboard stays down until then (screenKind.ts).
  const focusField = fieldFocusOnOpen();
  // The first admin's and an invited person's name opens chosen: typing replaces it.
  const suggestedName = useChosenSuggestion(displayName);
  // A rejection's field is marked invalid, read with the red line and refocused (lib/focusThroughWaits.ts).
  const rejectedField = !error ? "" : step === "setup" ? (props.setupCodeNeeded && displayName.trim() ? "setup-code" : "setup-display-name")
    : step === "invitation" ? "invitation-display-name" : step === "password-reset-request" ? "auth-reset-email"
      : step === "password-reset" ? "auth-reset-password" : step === "device-code" ? "auth-device-code"
        : step === "mfa" ? "auth-mfa-code" : step === "email" || step === "signup" ? "auth-password"
          : step === "company" ? "auth-company-email" : "";
  // What the panel shows: a step, or a change inside one that replaces the
  // control that had the focus (a sent reset link, a reset's result, the
  // session limit's choice, the device owner's accounts). A new one takes
  // the focus in when nothing in the panel holds it (2026-10-28: it stayed on
  // the page); the first one is left as the page opens it.
  const panelRef = useRef<HTMLElement>(null);
  const [restoring, setRestoring] = useState(false);
  // From outside, without the setup link, where to open it instead of a code field (2026-11-19) — but on a server given a code at its start (2026-11-27), or once "설정 코드 넣기" was pressed there, the code field.
  const [codeChosen, setCodeChosen] = useState(false), codeTyped = (useSetupStart().codeGiven || codeChosen) && props.setupCodeNeeded;
  const outsideGuide = props.setupCodeNeeded && !props.setupCode && !codeTyped;
  // A server's first setup asks its language first, unless the app's first screen did (lib/firstRunLanguage.ts).
  const [languageChosen, setLanguageChosen] = useState(() => firstRunLanguage() !== "");
  // An agent's conversation or another of the person's tabs may set the server up while this shows: the screen then goes on as
  // the administrator it made, or to sign-in — once this tab's own setup (busy: its claim, till kept) is refused, if it ran.
  useAgentStartSignIn(step === "setup" && !restoring, busy, props.setupCode, props.joinAgentStart, () => props.switchStep("signin"),
    props.setUpElsewhere);
  const view = [step, mfaMode, sessionCapacityOffered ? "capacity" : "",
    step === "password-reset-request" && notice ? "sent" : "", props.resetLinkDead ? "dead" : "",
    props.deviceOwner.accounts ? "accounts" : "", step === "setup" && restoring ? "restore" : ""].join("|");
  useViewEntryFocus(panelRef, view, { enterFirst: false });
  // The first run's first step names the window and its tab after its title
  // (its backup restore and its guide from outside after theirs), as its other
  // steps do (coverDocumentChrome); the sign-in and sign-up screens after
  // themselves, as Notion's app names its own ("로그인", "회원가입"; "Log in",
  // "Sign up": its login and signup pages' titles, read 2026-11-14). One inside a window over the app leaves it.
  const screenName = props.inWindow || step === "invitation" ? "" : step === "setup" ? (!languageChosen ? "" : t(restoring ? "authGate:setupRestoreTitle"
    : outsideGuide ? "authGate:setupOutsideTitle" : "authGate:setupTitle"))
      : t(step === "signup" ? "authGate:windowTitle.signup" : "authGate:windowTitle.signin");
  useLayoutEffect(() => (screenName ? coverDocumentChrome(screenName) : undefined), [screenName]);
  const rejection = (id: string, help?: string) => rejectedField === id
    ? { "aria-invalid": true as const, "aria-describedby": [help, ERROR_ID].filter(Boolean).join(" ") }
    : { "aria-describedby": help };

  const errorLine = error ? <p id={ERROR_ID} className={styles.error} role="alert" aria-live="assertive">{error}</p> : null;
  // A form says a refusal over its button, where the eye is (2026-11-05: under its last link, past the fold).
  const errorInForm = ["setup", "invitation", "password-reset", "device-code", "mfa", "email", "signup", "auth-action-result",
    "verification-sent"].includes(step)
    || (step === "password-reset-request" && emailRecoveryAvailable);
  const Screen = props.inWindow ? "div" : "main";
  // A step with its own title (setup, a reset, a result) needs no line under the brand or a page's wall (PageLoginWall.tsx); an invitation's title is the wall's second line, one heading as the wall's.
  const lined = step === "signin" || step === "email" || step === "mfa" || step === "signup";
  // A suggested name not yet changed follows the language chosen.
  if (step === "setup" && !languageChosen) return <FirstRunLanguageStep onChosen={(before, after) => { props.setDisplayName(suggestedNameIn(displayName, before, after)); setLanguageChosen(true); }} />;
  return (
    <Screen className={props.inWindow ? styles.screenInWindow : waitingIn ? wall.screen : styles.screen}>
      {waitingIn && !props.inWindow ? <PageWallBackdrop /> : null}
      <section ref={panelRef} className={waitingIn && !props.inWindow ? `${styles.panel} ${wall.card}` : styles.panel} aria-busy={busy}>
        {props.inWindow ? (
          <div className={styles.windowTitle}>
            <h2 id="add-account-title">{t("authGate:addAccountTitle")}</h2>
            <p>{t("authGate:addAccountBody")}</p>
          </div>
        ) : waitingIn ? <PageWallHead workspace={waitingIn} line={step === "invitation" ? t("authGate:invitationTitle") : !lined ? null : step === "signup" ? t("authGate:createAccount") : "workspace"} /> : (
          <AuthBrand line={lined ? (step === "signup" ? t("authGate:createAccount") : t("authGate:tagline")) : null} />
        )}

        {step === "setup" && restoring ? (
          <SetupRestore setupCode={props.setupCode} setupCodeNeeded={props.setupCodeNeeded} setSetupCode={props.setSetupCode}
            onBack={() => setRestoring(false)} />
        ) : step === "setup" && outsideGuide ? (
          <SetupOutsideGuide onEnterCode={() => setCodeChosen(true)} />
        ) : step === "setup" ? (
          <form className={styles.form} noValidate onSubmit={props.submitInstanceSetup} data-testid="instance-setup">
            <div className={styles.notice}>
              <strong>{t("authGate:setupTitle")}</strong>
              <p>{t(codeTyped ? "authGate:setupBodyCode" : "authGate:setupBody")}</p><SetupBackupsOff />
            </div>
            <label className={styles.label} htmlFor="setup-display-name">{t("authGate:name")}</label>
            <input
              id="setup-display-name"
              ref={suggestedName.ref}
              className={styles.input}
              type="text"
              autoComplete="name"
              value={displayName}
              onChange={(event) => { suggestedName.typed(); props.setDisplayName(event.target.value); }}
              onFocus={suggestedName.onFocus}
              placeholder={t("authGate:namePlaceholder")}
              disabled={busy}
              autoFocus={focusField}
              required
              {...rejection("setup-display-name")}
            />
            <p className={styles.fieldHelp} data-setup-name-note>{t("authGate:setupNameNote")}</p>
            {props.setupCodeNeeded ? <SetupCodeField value={props.setupCode} onChange={props.setSetupCode} disabled={busy}
              chosen={codeChosen} rejection={rejection("setup-code", "setup-code-help")} /> : null}
            {errorLine}
            <button className={styles.primary} type="submit" disabled={busy}>
              {busy ? t("authGate:setupWorking") : t("authGate:setupCta")}
            </button>
            {/* A lost server comes back here from its backups, without a name made first only to go (2026-11-06):
                one dim line under the button, not a choice of the same size (user decision 2026-11-19). */}
            <p className={restoreStyles.restoreLink}>{t("authGate:setupRestoreAsk")}{" "}
              <button type="button" disabled={busy} onClick={() => setRestoring(true)} data-setup-restore-open>
                {t("authGate:setupRestoreOpen")}
              </button>
            </p>
          </form>
        ) : step === "invitation" ? (
          <form className={styles.form} noValidate onSubmit={props.submitInvitationJoin} data-invitation-join>
            <div className={styles.notice}>
              {waitingIn && !props.inWindow ? null : <strong>{t("authGate:invitationTitle")}</strong>}
              {props.invitationDetails.message ? <blockquote className={styles.invitationNote} data-invitation-note>{props.invitationDetails.message}</blockquote> : null}
              <p>{[invitationLead(props.invitationDetails, t),
                props.invitationDetails.asksToJoin ? t("authGate:invitationBodyAsks")
                  : props.invitationEmail ? t("authGate:invitationBodyEmail", { email: props.invitationEmail }) : t("authGate:invitationBody")].filter(Boolean).join(" ")}</p>
            </div>
            <label className={styles.label} htmlFor="invitation-display-name">{t("authGate:name")}</label>
            <input
              id="invitation-display-name"
              ref={suggestedName.ref}
              className={styles.input}
              type="text"
              autoComplete="name"
              value={displayName}
              onChange={(event) => { suggestedName.typed(); props.setDisplayName(event.target.value); }}
              onFocus={suggestedName.onFocus}
              placeholder={t("authGate:namePlaceholder")}
              disabled={busy}
              autoFocus={focusField}
              required
              {...rejection("invitation-display-name")}
            />
            <p className={styles.fieldHelp}>{t("authGate:setupNameNote")}</p>
            {errorLine}
            <button className={styles.primary} type="submit" disabled={busy}>
              {busy ? t("authGate:invitationWorking") : t("authGate:invitationCta")}
            </button>
            <button className={styles.linkButton} type="button" onClick={() => props.switchStep("signin")} disabled={busy}>
              {t("authGate:invitationHaveAccount")}
            </button>
            {props.openInApp ? <a className={styles.linkButton} href={props.openInApp} data-open-in-app>{t("authGate:openInApp")}</a> : null}
          </form>
        ) : step === "password-reset-request" && !emailRecoveryAvailable ? (
          // A server that sends no mail can't send a link: its administrators reset
          // a password, which is the same for every account, so no account is named.
          <div className={styles.form} data-reset-without-mail>
            <div className={styles.notice}>
              <strong>{t("authGate:forgotPasswordTitle")}</strong>
              <p>{t("authGate:forgotPasswordNoMail")}</p>
            </div>
            <button className={styles.linkButton} type="button" onClick={props.back} disabled={busy}>
              {t("authGate:back")}
            </button>
          </div>
        ) : step === "password-reset-request" ? (
          <form className={styles.form} noValidate onSubmit={props.submitPasswordResetRequest}>
            <div className={styles.notice}>
              <strong>{t("authGate:forgotPasswordTitle")}</strong>
              <p>{t("authGate:forgotPasswordBody")}</p>
            </div>
            <label className={styles.label} htmlFor="auth-reset-email">{t("authGate:email")}</label>
            <input
              id="auth-reset-email"
              className={styles.input}
              type="email"
              autoComplete="email"
              value={email}
              onChange={(event) => props.setEmail(event.target.value)}
              placeholder={t("authGate:emailPlaceholder")}
              disabled={busy || Boolean(notice)}
              autoFocus={focusField}
              required
              {...rejection("auth-reset-email")}
            />
            {errorLine}
            {notice ? <p className={styles.success} role="status" aria-live="polite">{notice}</p> : (
              <button className={styles.primary} type="submit" disabled={busy}>{t("authGate:sendResetLink")}</button>
            )}
            <button className={styles.linkButton} type="button" onClick={props.back} disabled={busy}>
              {t("authGate:back")}
            </button>
          </form>
        ) : step === "password-reset" && props.resetLinkDead ? (
          <div className={styles.form}>
            <div className={styles.notice}>
              <strong>{t("authGate:resetPasswordTitle")}</strong>
            </div>
            <button className={styles.primary} type="button" onClick={() => props.switchStep("password-reset-request")} data-reset-new-link>
              {t("authGate:resetLinkNew")}
            </button>
          </div>
        ) : step === "password-reset" ? (
          <form className={styles.form} noValidate onSubmit={props.submitPasswordReset}>
            <div className={styles.notice}>
              <strong>{t("authGate:resetPasswordTitle")}</strong>
            </div>
            <label className={styles.label} htmlFor="auth-reset-password">{t("authGate:newPassword")}</label>
            <input
              id="auth-reset-password"
              className={styles.input}
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(event) => props.setPassword(event.target.value)}
              disabled={busy}
              autoFocus={focusField}
              required
              {...rejection("auth-reset-password", "auth-reset-password-help")}
            />
            <p id="auth-reset-password-help" className={styles.fieldHelp}>{t("authGate:passwordRequirements", { count: authBounds.minimumPasswordLength })}</p>
            <label className={styles.label} htmlFor="auth-reset-password-confirm">{t("authGate:confirmPassword")}</label>
            <input
              id="auth-reset-password-confirm"
              className={styles.input}
              type="password"
              autoComplete="new-password"
              value={passwordConfirm}
              onChange={(event) => props.setPasswordConfirm(event.target.value)}
              disabled={busy}
              required
            />
            {errorLine}
            <button className={styles.primary} type="submit" disabled={busy}>{t("authGate:resetPasswordCta")}</button>
          </form>
        ) : step === "auth-action-result" || step === "verification-sent" ? (
          <div className={styles.notice} role="status" aria-live="polite">
            <strong>{step === "verification-sent" ? t("authGate:checkYourEmail") : t(error ? "authGate:authActionFailedTitle" : "authGate:authActionTitle")}</strong>
            {notice ? <p className={styles.success}>{notice}</p> : errorLine}
            {props.signedIn ? null : (
              <button className={styles.primary} type="button" onClick={() => props.switchStep(props.passwordReset ? "email" : "signin")} disabled={busy}
                data-auth-result-sign-in>
                {t(props.passwordReset ? "authGate:signInWithNewPassword" : "authGate:returnToSignIn")}
              </button>
            )}
          </div>
        ) : step === "company" ? (
          <CompanySignInStep busy={busy} email={props.company.email} setEmail={props.company.setEmail}
            submit={props.company.submit} back={() => { props.company.setReason(""); props.switchStep("signin"); }}
            rejection={rejection("auth-company-email")} reason={props.company.reason} />
        ) : step === "device-code" ? (
          <form className={styles.form} noValidate onSubmit={props.submitDeviceCode} data-auth-device-code>
            <div className={styles.notice}>
              <strong>{t("authGate:deviceCodeTitle")}</strong>
              <p>{t("authGate:deviceCodeHint")}</p>
            </div>
            <label className={styles.label} htmlFor="auth-device-code">{t("authGate:deviceCodeLabel")}</label>
            <input
              id="auth-device-code"
              className={styles.input}
              autoComplete="one-time-code"
              {...CODE_KEYBOARD}
              value={props.deviceCode}
              onChange={(event) => props.setDeviceCode(inFours(event.target.value).toUpperCase().slice(0, 20))}
              placeholder="1234-5678-9012"
              disabled={busy}
              autoFocus={focusField}
              required
              {...rejection("auth-device-code")}
            />
            {errorLine}
            {sessionCapacityOffered ? (
              <div className={styles.notice} data-auth-device-capacity>
                <strong>{t("authGate:sessionCapacityTitle")}</strong>
                <p>{t("authGate:sessionCapacityBody")}</p>
                <button className={styles.primary} type="button" onClick={props.replaceOldestSessionAndSignIn} disabled={busy}>
                  {t("authGate:replaceOldestSession")}
                </button>
                <button className={styles.linkButton} type="button" onClick={props.dismissSessionCapacity} disabled={busy}>
                  {t("authGate:keepCurrentSessions")}
                </button>
              </div>
            ) : <button className={styles.primary} type="submit" disabled={busy || !props.deviceCode.trim()}>{t("authGate:continue")}</button>}
            {props.scanAvailable ? (
              <button className={`${styles.secondary} ${styles.withIcon}`} type="button" onClick={props.startScan} disabled={busy} data-auth-scan-code>
                <QrCodeIcon size="small" />{t("authGate:scanCode")}
              </button>
            ) : null}
            <NoDeviceHelp />
            <div className={styles.stepLinks}>
              <button className={styles.linkButton} type="button" onClick={() => props.switchStep("signin")} disabled={busy}>
                {t("authGate:back")}
              </button>
              {props.openInApp && props.deviceCode.trim() ? <a className={styles.linkButton} href={props.openInApp} data-open-in-app>{t("authGate:openInApp")}</a> : null}
            </div>
          </form>
        ) : step === "mfa" ? (
          <form className={styles.form} noValidate onSubmit={props.submitMfa}>
            <div className={styles.notice}>
              <strong>{t("authGate:enterYourVerificationCode")}</strong>
              <p>{mfaMode === "totp" ? t("authGate:totpHint") : t("authGate:recoveryHint")}</p>
            </div>
            <label className={styles.label} htmlFor="auth-mfa-code">
              {mfaMode === "totp" ? t("authGate:verificationCodeLabel") : t("authGate:recoveryCodeLabel")}
            </label>
            <input
              key={mfaMode}
              id="auth-mfa-code"
              className={styles.input}
              {...(mfaMode === "totp" ? { inputMode: "numeric" as const } : CODE_KEYBOARD)}
              autoComplete="one-time-code"
              value={mfaCode}
              onChange={(event) => props.setMfaCode(mfaMode === "totp"
                ? event.target.value.replace(/\D/gu, "").slice(0, 6)
                : inFours(event.target.value))}
              placeholder={mfaMode === "totp" ? "123456" : t("authGate:recoveryCodeLabel")}
              disabled={busy}
              autoFocus={focusField}
              required
              {...rejection("auth-mfa-code")}
            />
            {errorLine}
            <button className={styles.primary} type="submit" disabled={busy}>{t("authGate:verifyCode")}</button>
            <button
              className={styles.secondary}
              type="button"
              onClick={() => props.setMfaMode(mfaMode === "totp" ? "recovery" : "totp")}
              disabled={busy}
            >
              {mfaMode === "totp" ? t("authGate:useRecoveryCode") : t("authGate:useAuthenticatorCode")}
            </button>
            <button className={styles.linkButton} type="button" onClick={props.back} disabled={busy}>
              {t("authGate:back")}
            </button>
          </form>
        ) : step === "agent" ? (
          <AgentSignInStep busy={busy} onSession={props.acceptAgentSignIn} back={() => props.switchStep("signin")} />
        ) : step === "signin" ? (
          <SignInWays {...props} />
        ) : step === "signup" && props.signupFormClosed ? (
          // Typing a name, an address and a password that can only be refused
          // is lost work (2026-11-05): the reason and the way back only.
          <div className={styles.form}>
            <div className={styles.notice} role="status" aria-live="polite"><p>{notice}</p></div>
            <button className={styles.primary} type="button" onClick={() => props.switchStep("signin")} disabled={busy} autoFocus>
              {t("authGate:signInInstead")}
            </button>
          </div>
        ) : (
          <>
            <form className={styles.form} noValidate onSubmit={props.submitPassword}>
              {/* Sign-up says here why it is closed, and whom to ask (2026-10-18). */}
              {(step === "email" || step === "signup") && notice ? (
                <div className={styles.notice} role="status" aria-live="polite"><p>{notice}</p></div>
              ) : null}
              {step === "signup" ? (
                <>
                  <label className={styles.label} htmlFor="auth-display-name">{t("authGate:name")}</label>
                  <input
                    id="auth-display-name"
                    className={styles.input}
                    type="text"
                    autoComplete="name"
                    value={displayName}
                    onChange={(event) => props.setDisplayName(event.target.value)}
                    placeholder={t("authGate:namePlaceholder")}
                    disabled={busy}
                    // Sign-up opens on its first empty field, the name.
                    autoFocus={focusField && !displayName}
                    required
                  />
                </>
              ) : null}
              <label className={styles.label} htmlFor="auth-password-email">{t("authGate:email")}</label>
              <input
                id="auth-password-email"
                className={styles.input}
                type="email"
                autoComplete="email"
                value={email}
                onChange={(event) => props.setEmail(event.target.value)}
                placeholder={t("authGate:emailPlaceholder")}
                disabled={busy}
                // An address already filled (after a reset) leaves the password to type.
                autoFocus={focusField && (step === "signup" ? Boolean(displayName) && !email : !email)}
                required
              />
              <label className={styles.label} htmlFor="auth-password">{t("authGate:password")}</label>
              <input
                id="auth-password"
                className={styles.input}
                type="password"
                autoComplete={step === "signup" ? "new-password" : "current-password"}
                value={password}
                onChange={(event) => props.setPassword(event.target.value)}
                placeholder={t("authGate:password")}
                disabled={busy}
                autoFocus={focusField && step === "email" && Boolean(email)}
                required
                {...rejection("auth-password", step === "signup" ? "auth-password-help" : undefined)}
              />
              {step === "signup" ? <p id="auth-password-help" className={styles.fieldHelp}>{t("authGate:passwordRequirements", { count: authBounds.minimumPasswordLength })}</p> : null}
              {errorLine}
              {sessionCapacityOffered && step === "email" ? (
                <div className={styles.notice}>
                  <strong>{t("authGate:sessionCapacityTitle")}</strong>
                  <p>{t("authGate:sessionCapacityBody")}</p>
                  <button className={styles.primary} type="button" onClick={props.replaceOldestSessionAndSignIn} disabled={busy}>
                    {t("authGate:replaceOldestSession")}
                  </button>
                  <button className={styles.linkButton} type="button" onClick={props.dismissSessionCapacity} disabled={busy}>
                    {t("authGate:keepCurrentSessions")}
                  </button>
                </div>
              ) : <button className={styles.primary} type="submit" disabled={busy}>{step === "signup" ? t("authGate:createAccount") : t("authGate:continue")}</button>}
              {step === "email" ? (
                <button className={styles.linkButton} type="button" onClick={() => props.switchStep("password-reset-request")} disabled={busy}>
                  {t("authGate:forgotPassword")}
                </button>
              ) : null}
              <button className={styles.linkButton} type="button" onClick={() => props.switchStep("signin")} disabled={busy}>
                {step === "signup" ? t("authGate:signInInstead") : t("authGate:back")}
              </button>
            </form>
            {/* Leafen's own: a server's administrators see all on it. */}
            {step === "signup" ? <p className={styles.serverNotice} data-server-admin-sees>{t("authGate:serverAdminSeesAll")}</p> : null}
          </>
        )}
        {props.onLeaveServer ? (
          // A small action stands centred under the panel (리펜 고유 화면 기준).
          <button className={`${styles.linkButton} ${styles.leaveServer}`} type="button" onClick={props.onLeaveServer} disabled={busy}>
            {props.leaveServerLabel ?? t("authGate:backToThisDevice")}
          </button>
        ) : null}

        {errorInForm ? null : errorLine}{props.inWindow ? null : <LegalNotice place="panel" />}
      </section>
      <SignInLanguage step={step} inWindow={props.inWindow} signedIn={props.signedIn} wall={Boolean(waitingIn)} />
    </Screen>
  );
}
