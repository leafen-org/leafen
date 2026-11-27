import { type FormEvent, useEffect, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { authErrorCodes, type AuthMFAChallenge, type PasskeyCapability } from "@/authWire.generated";
import { SignInWaiting } from "./SignInWaiting";
import { markAppInteractiveForOfflineWarm } from "@/lib/appInteractive";
import { saveSession } from "@/lib/authSessionStore";
import { addAccountSession, serverAccountPath } from "@/lib/serverAccounts";
import { completeAuthenticationOnce } from "@/lib/authCompletionService";
import { authErrorLine, authLine, authWords, sayAuthLine, type AuthErrorContext, type AuthText } from "@/lib/authErrorPresentation";
import { createAuthOperationLane } from "@/lib/authOperationLane";
import { resetDocumentChrome } from "@/lib/documentChrome";
import { authProviderService, type AuthProviderOption } from "@/lib/authProviderService";
import { instanceBootstrapService } from "@/lib/instanceBootstrapService";
import { newId } from "@/lib/ids";
import { useDeviceOwnerSignIn } from "./useDeviceOwnerSignIn";
import { isPackagedClient } from "@/lib/packagedClient";
import { activeProfileId, LOCAL_PROFILE_ID } from "@/lib/profileRegistry";
import { useWayBackToDevice } from "./useWayBackToDevice";
import { packagedAppName, signInThroughApp } from "@/lib/appSignIn";
import { onBrowserSignInAsked, openInAppLink, takeBrowserSignIn } from "@/lib/openInApp";
import { PackagedLinkOpener } from "@/components/sidebar/PackagedLinkOpener";
import { signInReturnPath } from "@/lib/reauthReturnPath";
import { standaloneConsentPath } from "@/lib/standaloneConsent";
import { signupClosedStepNotice, signupRefusal } from "@/lib/signupInvitation";
import { takeSignupInvitationSigningIn } from "./takeSignupInvitation";
import type { Session } from "@/types";
import { AuthGateScreen, type AuthGateStep } from "./AuthGateScreen";
import { useSignupInvitation } from "./useSignupInvitation";
import { usePasskeyAuthentication } from "./usePasskeyAuthentication";
import { browserPasskeysAvailable } from "@/lib/passkeys";
import { serverIdentity, serverOrigin } from "@/lib/serverOrigin";
import { holdSignInPassword } from "@/lib/signInPasswordHandoff";
import { administratorErrorCodes } from "@/administratorWire.generated";
import { BrowserQrScanner } from "./BrowserQrScanner";
import { useDeviceCodeScan } from "./useDeviceCodeScan";
import { useCompanySignIn } from "./CompanySignIn";
import { authEntryCredential, forgetSetupLinkCode, spendAuthEntry } from "./initialAuthEntry";
import { type AuthGateControllerStep, startAuthGate } from "./authGateStart";
import { tellSetUp } from "@/lib/agentStartRequest";
import { passwordAcceptable } from "@/lib/passwordRule";
import { markFreshAccount } from "@/lib/freshAccount";
import { claimInput, type ClaimedSession, startFirstAdministrator } from "@/lib/firstAdministratorStart";
import { deadResetLink, EMAIL_RE, isMFAChallenge, isSessionCapacity, signInThroughBrowserRound } from "./authGateRounds";

export function AuthGate({ onSession, sessionEnded = false, onReturn, signedInAs = "", addingAccount = false, inWindow = false, pageWall = false }: {
  onSession: (session: Session) => void;
  /** Signed in here already: an auth link's step shows with the way back to the workspace. */
  onReturn?: () => void;
  /** The account signed in here, whose sign-in a reset of its password ends. */
  signedInAs?: string;
  // The server ended this browser's sign-in, which the sign-in screen says.
  sessionEnded?: boolean;
  /** Another account signing in: the one signed in now stays (lib/serverAccounts.ts). */
  addingAccount?: boolean;
  /** Its window over the page, not a screen of its own (AddAccountWindow). */
  inWindow?: boolean;
  /** Not signed in since the page loaded: at a page's address it names that page's workspace. */
  pageWall?: boolean;
}) {
  const { t } = useTranslation("authGate");
  const [step, setStep] = useState<AuthGateControllerStep>("checking");
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<AuthText>(""); // its red line and notice: what to say (lib/authLine.ts)
  // What the last try said that the fields don't decide: an account's
  // password wait, until another address is typed.
  const standing = useRef<AuthText | null>(null);
  const [notice, setNotice] = useState<AuthText>(() => (sessionEnded ? authWords("authGate:sessionEnded") : ""));
  // A reset link that can't be used any more offers a new one in place of its fields.
  const [resetLinkDead, setResetLinkDead] = useState(false);
  // The password was reset: the result signs in with it, the address filled,
  // and a tab signed in to that account no longer offers its workspace (the
  // reset ended its sign-in, 2026-10-26).
  const [passwordReset, setPasswordReset] = useState<{ ended: boolean } | null>(null);
  const [displayName, setDisplayName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [passwordConfirm, setPasswordConfirm] = useState("");
  const [deviceCode, setDeviceCode] = useState("");
  const [setupCode, setSetupCode] = useState("");
  const [setupCodeNeeded, setSetupCodeNeeded] = useState(false);
  const [passkeyCapability, setPasskeyCapability] = useState<PasskeyCapability | null>(null);
  const [providers, setProviders] = useState<AuthProviderOption[]>([]);
  const [companySignIn, setCompanySignIn] = useState(false);
  const [emailRecoveryAvailable, setEmailRecoveryAvailable] = useState(false);
  const [mfaChallenge, setMfaChallenge] = useState<AuthMFAChallenge | null>(null);
  const [mfaCode, setMfaCode] = useState("");
  const [mfaMode, setMfaMode] = useState<"totp" | "recovery">("totp");
  // The password a 2-step sign-in began with, handed on once its code is
  // accepted, as a sign-in without 2 steps hands it on at once (2026-10-29:
  // a temporary password was asked for again after the code).
  const mfaPassword = useRef("");
  const [sessionCapacityOffered, setSessionCapacityOffered] = useState(false);
  // The address's invitation, until the server says it can't be used (useSignupInvitation.ts).
  const invitation = useSignupInvitation({ setStep, setError, setDisplayName });
  const signupInvitation = invitation.token;
  const [signupPolicy, setSignupPolicy] = useState({ closed: false, contact: "", mail: false });
  const loginRequestId = useRef("");
  const operationLane = useRef(createAuthOperationLane());
  // Once a session is handed on the screen stays up, still busy, while the
  // signed-in app gets ready behind it (App keeps it in place): its button is
  // not offered again then.
  const handedOver = useRef(false);
  // Where a reset asked or a second step goes back to: the email step it came
  // from, the email kept (2026-10-28: both went to the first screen).
  const backTo = useRef<AuthGateStep>("signin");
  const initialTranslate = useRef(t);
  // The packaged app signing in on another server.
  const otherServer = isPackagedClient() && activeProfileId() !== LOCAL_PROFILE_ID;
  const wayBack = useWayBackToDevice(otherServer && !inWindow);
  // The app's own server lets its device's owner back in (desktop/device_owner.go).
  const deviceOwner = useDeviceOwnerSignIn({
    local: isPackagedClient() && activeProfileId() === LOCAL_PROFILE_ID,
    joinWithCode: (code) => joinWithCode(code), setError, t,
  });
  const passkeys = usePasskeyAuthentication({ step, otherServer, origin: passkeyCapability?.origin ?? "", run, acceptSession, setNotice, setError, offerCapacity: offerSessionReplacement });
  const { browserScanning, startScan, endBrowserScan, takeScanned } = useDeviceCodeScan({
    setError, onCode: (code) => { setDeviceCode(code); void joinWithCode(code); },
  });

  // No private page identity remains while authentication is required.
  useLayoutEffect(() => { if (!inWindow) resetDocumentChrome(); }, [inWindow]);

  useEffect(() => () => operationLane.current.retire(), []);
  // A link that opened the app here, or came while this shows, asked to sign in through the browser.
  useEffect(() => {
    if (!otherServer) return undefined;
    const asked = () => { if (takeBrowserSignIn(serverIdentity())) startBrowserSignIn(); };
    asked();
    return onBrowserSignInAsked(asked);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once, as the screen opens
  }, []);
  // A step opened at the sign-in limit (a device code link) keeps its offer
  // past this reset (2026-10-25: a link at the limit showed no way on).
  const capacityOfferOpened = useRef(false);
  // The start's next try, which the screen saying the server is away asks for (authGateStart.ts).
  const startAgain = useRef(() => Promise.resolve());
  useEffect(() => {
    loginRequestId.current = "";
    if (capacityOfferOpened.current) { capacityOfferOpened.current = false; return; }
    setSessionCapacityOffered(false);
  }, [email, password, step]);
  useEffect(() => { if (step !== "checking") markAppInteractiveForOfflineWarm(); }, [step]);

  useEffect(() => startAuthGate({
    translate: initialTranslate.current, onReturn, invitation, capacityOfferOpened, startAgain, acceptSession, finishProviderAuthentication,
    setStep, setBusy, setError, setDeviceCode, setPassword, setPasswordConfirm, setResetLinkDead, setProviders,
    setPasskeyCapability, setEmailRecoveryAvailable, setCompanySignIn, setSignupPolicy, setDisplayName, setEmail,
    setSessionCapacityOffered, setSetupCode, setSetupCodeNeeded,
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once, as the screen opens
  }), []);

  async function acceptSession(session: Session) {
    spendAuthEntry();
    const generation = operationLane.current.current();
    const keep = addingAccount || signedInAs ? addAccountSession : saveSession; // joins whoever is signed in here, as Notion
    if (!await keep(session, () => operationLane.current.isCurrent(generation))) return;
    if (!operationLane.current.isCurrent(generation)) return;
    const returnPath = addingAccount || signedInAs ? serverAccountPath(session.user.id) : signInReturnPath(session.user.id);
    // An invitation's link an account signed in through is its own where it holds the address (takeSignupInvitation.ts).
    if (signupInvitation && !addingAccount && !signedInAs && await takeSignupInvitationSigningIn(session, signupInvitation, returnPath)) return;
    // A consent the address asks for is answered first.
    window.history.replaceState(null, "", standaloneConsentPath(window.location.href) ?? returnPath);
    handedOver.current = true;
    onSession(session);
  }

  async function run<T>(
    load: () => Promise<T>,
    publish: (value: T) => void | Promise<void>,
    context: AuthErrorContext,
    onFailure?: (caught: unknown) => boolean,
  ) {
    const generation = operationLane.current.begin();
    setBusy(true);
    setError("");
    try {
      const value = await load();
      if (operationLane.current.isCurrent(generation)) await publish(value);
    } catch (caught) {
      if (!operationLane.current.isCurrent(generation) || invitation.end(caught)) return;
      if (!onFailure?.(caught)) {
        const line = authErrorLine(caught, context);
        standing.current = (caught as { code?: unknown } | null)?.code === authErrorCodes.passwordWait ? line : null;
        setError(line);
      }
    } finally {
      if (operationLane.current.isCurrent(generation) && !handedOver.current) setBusy(false);
    }
  }

  // A field typed in again puts away what the last try said about it, but not
  // what doesn't hang on it (`standing`), and a closed server's sign-up line
  // comes back (2026-10-27).
  function typedAgain(field: "name" | "email" | "password" | "confirm") {
    if (standing.current && error === standing.current && field !== "email") return;
    setError("");
    if (step === "signup" && signupPolicy.closed && !signupInvitation) setNotice(authLine((say) => signupClosedStepNotice(say, signupPolicy.contact, signupPolicy.mail)));
  }

  function validEmail() {
    if (EMAIL_RE.test(email.trim())) return true;
    setError(authWords("authGate:enterValidEmail"));
    return false;
  }

  function switchStep(next: AuthGateStep) {
    operationLane.current.retire();
    setBusy(false);
    setError("");
    setNotice("");
    setSessionCapacityOffered(false);
    // A code given up on isn't there when the step opens again, for the next one to run on from.
    if (step === "device-code" && next !== "device-code") setDeviceCode("");
    // Back at the start, or handed to the company's sign-in, a password typed
    // and left isn't there when email sign-in opens again; the address stays,
    // a help (2026-11-06; 2026-11-07: typed again after the company sign-in's
    // back, it ran on from the old one).
    if (next === "signin" || next === "company") setPassword("");
    if (next === "signin" || next === "email") {
      setMfaChallenge(null);
      setMfaCode("");
      mfaPassword.current = "";
    }
    if (next === "signup" && signupPolicy.closed && !signupInvitation) setNotice(authLine((say) => signupClosedStepNotice(say, signupPolicy.contact, signupPolicy.mail)));
    if (next === "password-reset-request") backTo.current = step === "email" ? "email" : "signin";
    // With an invitation, making an account is giving a name; one that ended is gone (useSignupInvitation.ts).
    setStep(next === "signup" && signupInvitation ? "invitation" : next);
  }

  /** Says the server's password rule before it refuses the password (2026-10-16: a short one got a general error). */
  function longEnough(value: string) {
    if (passwordAcceptable(value)) return true;
    setError(authWords("authGate:passwordTooShort"));
    return false;
  }

  // Whoever set the server up goes on to the first run's steps (the server began
  // them, lib/firstRunSteps.ts) — set up here (`claimed`, with the first screen's language) or by an agent's
  // conversation, or in another tab (not `here`) — and so do the person's other tabs here.
  async function setUpAs(session: ClaimedSession, here = true, claimed = false) {
    forgetSetupLinkCode();
    await startFirstAdministrator(session, here, claimed, async (kept) => { await acceptSession(kept); return handedOver.current; });
    if (here && handedOver.current) tellSetUp(session.user.id);
  }

  async function submitInstanceSetup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = displayName.trim();
    if (!name) {
      setError(authWords("authGate:enterName"));
      return;
    }
    if (setupCodeNeeded && !setupCode.trim()) {
      setError(authWords("authGate:setupCodeRequired"));
      return;
    }
    await run(
      () => instanceBootstrapService.initialize({ name, ...claimInput(), ...(setupCode.trim() ? { setupCode: setupCode.trim() } : {}) }),
      (session) => setUpAs(session, true, true),
      "auth.serverSetup",
      (caught) => {
        // A wrong or missing code opens its field.
        if (([administratorErrorCodes.setupCodeInvalid, administratorErrorCodes.setupCodeRequired] as string[]).includes((caught as { code?: string } | null)?.code ?? "")) setSetupCodeNeeded(true);
        return false;
      },
    );
  }

  async function submitInvitationJoin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = displayName.trim();
    if (!name) {
      setError(authWords("authGate:enterName"));
      return;
    }
    await run(
      () => completeAuthenticationOnce(`invitation:${signupInvitation}:${name}`, () => api.joinWithInvitation(signupInvitation, name)),
      async (session) => {
        invitation.leaveAddress();
        markFreshAccount(session.user.id);
        await acceptSession(session);
      },
      "auth.signUp",
      (caught) => {
        // The invitation's address has an account: it signs in to accept it.
        if ((caught as { code?: string } | null)?.code !== authErrorCodes.accountExists) return false;
        switchStep("email");
        setNotice(authWords("authGate:invitationSignIn"));
        return true;
      },
    );
  }

  async function submitDeviceCode(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await joinWithCode(deviceCode.trim());
  }

  // At the account's sign-in limit the code stays unused and the step offers
  // to end the oldest sign-in, as signing in by password does.
  async function joinWithCode(code: string, replaceOldestSession = false) {
    await run(() => completeAuthenticationOnce(`device:${code}${replaceOldestSession ? ":replace" : ""}`,
      () => api.joinWithDeviceCode(code, replaceOldestSession)), acceptSession, "auth.deviceCode", offerSessionReplacement);
  }

  async function submitPassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (step === "signup" && !displayName.trim()) {
      setError(authWords("authGate:enterName"));
      return;
    }
    if (!validEmail()) return;
    passkeys.reset();
    if (step === "signup") {
      if (!longEnough(password)) return;
      await run(
        () => api.createAccount(email.trim().toLowerCase(), password, displayName.trim(), signupInvitation),
        async (result) => {
          if ("token" in result) {
            invitation.leaveAddress();
            await acceptSession(result);
          } else {
            // The account waits for its address to be proven: the mail's link finishes it.
            setNotice(authWords("authGate:signupConfirmationSent", { email: email.trim().toLowerCase() }));
            setStep("verification-sent");
          }
        },
        "auth.signUp",
        (caught) => {
          const refusal = signupRefusal(caught, signupPolicy.contact, signupPolicy.mail, () => authErrorLine(caught, "auth.signUp"));
          if (refusal) {
            // The refusal says it all: the step's line on who may sign up goes.
            setNotice("");
            setError(refusal);
          }
          return Boolean(refusal);
        },
      );
      return;
    }
    if (!password) {
      setError(authWords("authGate:enterPassword"));
      return;
    }
    if (!loginRequestId.current) loginRequestId.current = newId();
    await run(
      () => api.login(email.trim().toLowerCase(), password, { requestId: loginRequestId.current }),
      (result) => { holdPasswordFor(result); return finishPasswordAuthentication(result); },
      "auth.signIn",
      (caught) => offerSessionReplacement(caught) || company.takeOver(caught, email),
    );
  }

  function holdPasswordFor(result: Session | AuthMFAChallenge) {
    if (isMFAChallenge(result)) mfaPassword.current = password;
    else holdSignInPassword(result.user.id, password);
  }

  async function finishPasswordAuthentication(result: Session | AuthMFAChallenge) {
    setSessionCapacityOffered(false);
    if (isMFAChallenge(result)) {
      setMfaChallenge(result);
      setMfaCode("");
      setMfaMode("totp");
      backTo.current = step === "email" ? "email" : "signin";
      setStep("mfa");
    } else await acceptSession(result);
  }

  // A sign-in provider's sign-in asks an account with 2-step verification
  // for its code next, as a password does.
  async function finishProviderAuthentication(result: Session | AuthMFAChallenge) {
    if (!isMFAChallenge(result)) return acceptSession(result);
    await finishPasswordAuthentication(result);
    setBusy(false);
  }

  function offerSessionReplacement(caught: unknown) {
    if (!isSessionCapacity(caught)) return false;
    setSessionCapacityOffered(true);
    setError("");
    return true;
  }

  async function replaceOldestSessionAndSignIn() {
    if (step === "device-code") { await joinWithCode(deviceCode.trim(), true); return; }
    if (passkeys.hasPending()) { await passkeys.start(true); return; }
    if (!validEmail()) return;
    await run(
      () => api.login(email.trim().toLowerCase(), password, {
        requestId: newId(), replaceOldestSession: true,
      }),
      (result) => { holdPasswordFor(result); return finishPasswordAuthentication(result); },
      "auth.signIn",
      offerSessionReplacement,
    );
  }

  async function submitMfa(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!mfaChallenge) {
      switchStep("signin");
      setError(authWords("authGate:startSignInAgain"));
      return;
    }
    const code = mfaMode === "totp" ? mfaCode.replace(/\D/gu, "") : mfaCode.trim();
    if (!code) {
      setError(authWords(mfaMode === "totp" ? "authGate:enterVerificationCode" : "authGate:enterRecoveryCode"));
      return;
    }
    await run(
      () => mfaMode === "totp"
        ? api.completeTOTPChallenge({ ticket: mfaChallenge.ticket, code })
        : api.completeRecoveryChallenge({ ticket: mfaChallenge.ticket, recoveryCode: code }),
      (session) => {
        if (mfaPassword.current) holdSignInPassword(session.user.id, mfaPassword.current);
        mfaPassword.current = "";
        return acceptSession(session);
      },
      "auth.mfa",
      (caught) => {
        if ((caught as { code?: unknown } | null)?.code !== authErrorCodes.mfaChallengeInvalid) return false;
        // The request ended (timed out, or too many wrong codes): the sign-in
        // starts again at the password, the email kept, said as a notice.
        switchStep("email");
        setNotice(authErrorLine(caught, "auth.mfa"));
        return true;
      },
    );
  }

  async function submitPasswordResetRequest(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!validEmail()) return;
    await run(
      () => api.requestPasswordReset(email.trim().toLowerCase()),
      () => setNotice(authWords("authGate:passwordResetSent")),
      "auth.passwordResetRequest",
    );
  }

  async function submitPasswordReset(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!longEnough(password)) return;
    if (password !== passwordConfirm) {
      setError(authWords("authGate:passwordMismatch"));
      return;
    }
    await run(
      () => api.confirmPasswordReset(authEntryCredential(), password, passwordConfirm),
      (result) => {
        // The reset link is used: signed out after it, the screen doesn't open it again.
        spendAuthEntry();
        setPassword("");
        setPasswordConfirm("");
        if (result?.email) setEmail(result.email);
        setPasswordReset({ ended: Boolean(onReturn) && Boolean(result?.userId) && result.userId === signedInAs });
        setNotice(authWords("authGate:passwordResetComplete"));
        setStep("auth-action-result");
        window.history.replaceState(null, "", "/");
      },
      "auth.passwordResetConfirm",
      (caught) => {
        if (!deadResetLink(caught)) return false;
        setResetLinkDead(true);
        setError(authErrorLine(caught, "auth.passwordResetConfirm"));
        return true;
      },
    );
  }

  // "회사 계정으로 로그인" (CompanySignIn.tsx).
  const company = useCompanySignIn({
    otherServer, setBusy, setError, setNotice, acceptSession, open: () => switchStep("company"),
    failure: (caught) => authErrorLine(caught, "auth.providerExchange"),
  });

  // The app signs in through the system's browser signed in to this server
  // (app_sign_in.go): one press of "register" there.
  function startBrowserSignIn() {
    operationLane.current.retire();
    setError("");
    setNotice(authWords("authGate:appSignIn.browserHint"));
    void signInThroughBrowserRound({
      open: () => signInThroughApp(`${serverOrigin()}/auth/app?app_name=${encodeURIComponent(packagedAppName())}`, "appSignIn"),
      exchange: (code, verifier) => completeAuthenticationOnce(`app:${code}`, () => api.exchangeAppSignIn(code, verifier)),
      finish: acceptSession, setBusy, setNotice, fail: (caught) => setError(authErrorLine(caught, "auth.deviceCode")),
    });
  }

  function startOAuth(provider: string) {
    operationLane.current.retire();
    setError("");
    if (otherServer) {
      // The app signs in in the system's browser, which brings the sign-in
      // back to the app; the screen stays usable meanwhile.
      setNotice(authWords("authGate:finishInBrowser"));
      void signInThroughBrowserRound({
        open: () => signInThroughApp(authProviderService.startURL(provider)),
        exchange: (code, verifier) => completeAuthenticationOnce(`oauth:${code}`, () => authProviderService.exchange(code, verifier)),
        finish: finishProviderAuthentication, setBusy, setNotice,
        fail: (caught) => setError(authErrorLine(caught, "auth.providerExchange")),
      });
      return;
    }
    setBusy(true);
    try {
      authProviderService.start(provider);
    } catch (caught) {
      setError(authErrorLine(caught, "auth.providerExchange"));
      setBusy(false);
    }
  }

  if (step === "checking" || step === "server-away") return <SignInWaiting away={step === "server-away"} inWindow={inWindow} tryAgain={() => startAgain.current()} />;
  // A packaged desktop app has no camera to scan with; a phone app and a browser do.
  const scanAvailable = !isPackagedClient() || /Android|iPhone|iPad/u.test(navigator.userAgent);
  // On another server the packaged app offers that server's providers too
  // (through the system's browser), with the way back to this device where it has one (useWayBackToDevice).
  return (
    <>
    {browserScanning ? <BrowserQrScanner onScanned={takeScanned} onEnd={endBrowserScan} /> : null}
    {/* The app takes the links the system opens it with. */}
    {isPackagedClient() && !inWindow ? <PackagedLinkOpener signedOut /> : null}
    <AuthGateScreen
      inWindow={inWindow} pageWall={pageWall}
      step={step} busy={busy} error={sayAuthLine(error)} notice={sayAuthLine(notice)} resetLinkDead={resetLinkDead}
      // Without mail an invited address can't be proven, so only the invitation's link lets one in.
      signupFormClosed={signupPolicy.closed && !signupPolicy.mail && !signupInvitation}
      displayName={displayName} email={email} password={password} passwordConfirm={passwordConfirm}
      mfaCode={mfaCode} mfaMode={mfaMode} sessionCapacityOffered={sessionCapacityOffered}
      emailRecoveryAvailable={emailRecoveryAvailable} providers={providers}
      companySignIn={companySignIn} company={company}
      onLeaveServer={onReturn && !passwordReset?.ended ? () => {
        spendAuthEntry();
        window.history.replaceState(null, "", "/");
        onReturn();
      } : wayBack.leave}
      leaveServerLabel={onReturn && !passwordReset?.ended ? t("authGate:returnToWorkspace") : wayBack.toStart ? t("authGate:backToStart") : undefined}
      signedIn={!!onReturn && !passwordReset?.ended}
      passwordReset={Boolean(passwordReset)}
      setDisplayName={(value) => { setDisplayName(value); typedAgain("name"); }} setEmail={(value) => { setEmail(value); typedAgain("email"); }}
      setPassword={(value) => { setPassword(value); typedAgain("password"); }} setPasswordConfirm={(value) => { setPasswordConfirm(value); typedAgain("confirm"); }}
      setupCode={setupCode} setupCodeNeeded={setupCodeNeeded} setSetupCode={(value) => { setSetupCode(value); setError(""); }}
      deviceCode={deviceCode} setDeviceCode={(value) => { setDeviceCode(value); setError(""); }}
      submitDeviceCode={(event) => void submitDeviceCode(event)}
      acceptAgentSignIn={(session) => void acceptSession(session)}
      scanAvailable={scanAvailable} startScan={() => void startScan()}
      browserSignIn={otherServer ? startBrowserSignIn : undefined}
      // A browser offers the app. The page's own address is cleared of its code once read: the
      // link carries it again, on the page an invitation was to (2026-10-26: a page invite's link
      // opened the app's home).
      openInApp={isPackagedClient() || inWindow ? undefined : openInAppLink(
        step === "device-code" && deviceCode.trim() ? `${window.location.origin}/device/${encodeURIComponent(deviceCode.trim())}`
          : step === "invitation" && signupInvitation
            ? `${window.location.origin}${window.location.pathname}${window.location.search}#invitation=${encodeURIComponent(signupInvitation)}`
            : window.location.href)}
      setMfaCode={(value) => { setMfaCode(value); setError(""); }}
      setMfaMode={(value) => { setMfaMode(value); setMfaCode(""); setError(""); }}
      switchStep={switchStep}
      back={() => switchStep(backTo.current)}
      dismissSessionCapacity={() => { setSessionCapacityOffered(false); setError(""); }}
      submitInstanceSetup={(event) => void submitInstanceSetup(event)}
      joinAgentStart={(code) => void run(() => api.joinWithDeviceCode(code), setUpAs, "auth.deviceCode")} setUpElsewhere={(session) => void setUpAs(session, false)}
      invitationEmail={invitation.email}
      invitationDetails={invitation.details}
      deviceOwner={deviceOwner}
      submitInvitationJoin={(event) => void submitInvitationJoin(event)}
      submitPassword={(event) => void submitPassword(event)}
      submitMfa={(event) => void submitMfa(event)}
      submitPasswordResetRequest={(event) => void submitPasswordResetRequest(event)}
      submitPasswordReset={(event) => void submitPasswordReset(event)}
      replaceOldestSessionAndSignIn={() => void replaceOldestSessionAndSignIn()}
      startOAuth={startOAuth}
      passkeyAvailable={Boolean(passkeyCapability?.available && (otherServer || (passkeyCapability.origin === window.location.origin && browserPasskeysAvailable())))}
      startPasskey={() => void passkeys.start()}
      passkeyRunning={passkeys.running}
      passkeyReason={passkeyCapability && !passkeyCapability.available ? t("workspaceSettingsDialog:passkeyUnavailable") : passkeyCapability && !otherServer && (passkeyCapability.origin !== window.location.origin || !browserPasskeysAvailable()) ? t("workspaceSettingsDialog:passkeyDeviceUnsupported") : ""}
      cancelPasskey={passkeys.cancel}
    />
    </>
  );
}
