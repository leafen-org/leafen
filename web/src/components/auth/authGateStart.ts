import type { Dispatch, SetStateAction } from "react";
import type { TFunction } from "i18next";
import { api } from "@/api";
import { authErrorCodes, type AuthMFAChallenge, type PasskeyCapability } from "@/authWire.generated";
import { completeAuthenticationOnce } from "@/lib/authCompletionService";
import { authErrorLine, authLine, authWords, productErrorLine, type AuthErrorContext, type AuthText } from "@/lib/authErrorPresentation";
import { authProviderService, type AuthProviderOption } from "@/lib/authProviderService";
import { instanceBootstrapService } from "@/lib/instanceBootstrapService";
import { appServerEntry } from "@/lib/appServerEntry";
import { answerWaitMs } from "@/lib/serverAnswers";
import { isServerUnreachable } from "@/lib/serverReach";
import { settleEarlierOwner } from "@/lib/packagedClient";
import { phoneBrowser } from "@/lib/openInApp";
import { fetchRuntimeConfig } from "@/lib/runtimeConfigService";
import { signupClosedNotice } from "@/lib/signupInvitation";
import { tabSuggestedName } from "@/lib/suggestedNames";
import { i18next, returnToDeviceLanguage } from "@/i18n";
import type { Session } from "@/types";
import type { AuthGateStep } from "./AuthGateScreen";
import { spendAuthEntry, unusedAuthEntry } from "./initialAuthEntry";
import { deadResetLink, isSessionCapacity } from "./authGateRounds";
import type { SignupInvitationState } from "./useSignupInvitation";

/**
 * Before a step: "checking" while the start reads the server, and
 * "server-away" once the server can't be reached, is updating, or hasn't
 * answered in a few seconds (SignInWaiting.tsx, which says which and goes on
 * once it answers).
 */
export type AuthGateControllerStep = AuthGateStep | "checking" | "server-away";

// How long the start waits for the server before saying it hasn't reached
// it, or a few of its round trips where longer (answerWaitMs). A start on a
// NAS-class server answers in about a second (2026-11-23).
const SERVER_WAIT_MS = 5_000;

type Set<T> = Dispatch<SetStateAction<T>>;

/** What the sign-in page's start reads and sets of the page (AuthGate). */
export type AuthGateStart = {
  translate: TFunction;
  onReturn?: () => void;
  /** The address's invitation (useSignupInvitation.ts). */
  invitation: SignupInvitationState;
  /** A step opened at the sign-in limit keeps its offer past the step's reset. */
  capacityOfferOpened: { current: boolean };
  /** Set to the start's next try, which the "server-away" screen asks for; it ends with that try. */
  startAgain: { current: () => Promise<void> };
  acceptSession(session: Session): Promise<void>;
  finishProviderAuthentication(result: Session | AuthMFAChallenge): Promise<void>;
  setStep: Set<AuthGateControllerStep>;
  setBusy: Set<boolean>;
  setError: Set<AuthText>;
  setDeviceCode: Set<string>;
  setPassword: Set<string>;
  setPasswordConfirm: Set<string>;
  setResetLinkDead: Set<boolean>;
  setProviders: Set<AuthProviderOption[]>;
  setPasskeyCapability: Set<PasskeyCapability | null>;
  setEmailRecoveryAvailable: Set<boolean>;
  setCompanySignIn: Set<boolean>;
  setSignupPolicy: Set<{ closed: boolean; contact: string; mail: boolean }>;
  setDisplayName: Set<string>;
  setEmail: Set<string>;
  setSessionCapacityOffered: Set<boolean>;
  setSetupCode: Set<string>;
  setSetupCodeNeeded: Set<boolean>;
};

/**
 * The sign-in page's start, once as it opens: an earlier version's owner on
 * the app's own window, a provider's or SAML's return, a link from mail (an
 * email check, a password reset), a device code's link, and otherwise the
 * server's state and runtime settings, which open setup, an invitation or
 * sign-in. A server that doesn't answer, or says it is away or updating,
 * opens "server-away" instead of a step, and what the address asked for is
 * tried again with the start's next try — the newest try alone sets the
 * page (2026-11-23: an app pointed at a server that was off showed the
 * sign-in ways with no word, and pressing them did nothing). It answers the
 * page's cleanup.
 */
export function startAuthGate(gate: AuthGateStart): () => void {
  const {
    translate, onReturn, invitation, capacityOfferOpened, startAgain, acceptSession, finishProviderAuthentication,
    setStep, setBusy, setError, setDeviceCode, setPassword, setPasswordConfirm, setResetLinkDead, setProviders,
    setPasskeyCapability, setEmailRecoveryAvailable, setCompanySignIn, setSignupPolicy, setDisplayName, setEmail,
    setSessionCapacityOffered, setSetupCode, setSetupCodeNeeded,
  } = gate;
  const signupInvitation = invitation.token;
  let mounted = true;
  let tries = 0;
  const start = async (again = false) => {
    const attempt = ++tries;
    // A try the page left, or a newer one took over from, sets nothing.
    const live = () => mounted && attempt === tries;
    const late = setTimeout(() => {
      if (live()) setStep((current) => (current === "checking" ? "server-away" : current));
    }, answerWaitMs(SERVER_WAIT_MS));
    const entry = unusedAuthEntry();
    let errorContext: AuthErrorContext = "auth.bootstrap";
    // What the app's shell handed for its own server (appServerEntry).
    const appServer = appServerEntry();
    try {
      if (appServer.capability && !onReturn) {
        // An earlier version's hidden owner signs in on its app's window
        // once more; the app keeps the sign-in and the capability ends.
        const earlierOwner = appServer.capability;
        try {
          const session = await completeAuthenticationOnce(
            `local-owner:${earlierOwner}`,
            () => api.exchangeLocalOwnerCapability(earlierOwner),
          );
          if (live()) {
            await acceptSession(session);
            void settleEarlierOwner(session);
          }
          return;
        } catch {
          // Settled meanwhile: the account signs in as any other.
        }
      }
      if (entry.pathname === "/auth/callback" && entry.failure) {
        // Shown on the sign-in page, which the rest of the start opens; a
        // closed sign-up says so once the server's contact is read.
        if (entry.failure.code !== authErrorCodes.signupRestricted) {
          setError(productErrorLine(Object.assign(new Error(entry.failure.code), entry.failure),
            "auth.providerExchange", { receipt: true }));
        }
      } else if (entry.pathname === "/auth/callback") {
        errorContext = "auth.providerExchange";
        if (!entry.credential) {
          setError(authWords("authGate:linkMissingToken"));
          setStep("auth-action-result");
          setBusy(false);
          return;
        }
        const code = entry.credential;
        const result = await completeAuthenticationOnce(
          `oauth:${code}`,
          () => authProviderService.exchange(code),
        );
        if (live()) await finishProviderAuthentication(result);
        return;
      }
      if (entry.pathname === "/verify-email") {
        errorContext = "auth.emailVerification";
        if (!entry.credential) {
          setError(authWords("authGate:actionMissingToken"));
          setStep("auth-action-result");
          setBusy(false);
          return;
        }
        const session = await completeAuthenticationOnce(
          `email:${entry.credential}`,
          () => api.confirmEmailVerification(entry.credential),
        );
        if (live()) await acceptSession(session);
        return;
      }
      // A code that can't join leaves its own step open with why, once the page is ready.
      let deviceFailure: AuthText = "";
      // At the account's sign-in limit the step offers to end the oldest one.
      let deviceAtCapacity = false;
      // A phone's browser asks first: this browser, or the app with the
      // code (the multi-server decision's 2026-10-20 "같은 날 다듬음").
      let deviceChoice = false;
      if (entry.pathname === "/device" && phoneBrowser()) {
        setDeviceCode(entry.credential);
        deviceChoice = true;
      } else if (entry.pathname === "/device") {
        const code = entry.credential;
        setDeviceCode(code);
        try {
          const session = await completeAuthenticationOnce(`device:${code}`, () => api.joinWithDeviceCode(code));
          if (live()) await acceptSession(session);
          return;
        } catch (caught) {
          deviceAtCapacity = isSessionCapacity(caught);
          deviceFailure = deviceAtCapacity ? "" : authErrorLine(caught, "auth.deviceCode");
        }
      }
      if (entry.pathname === "/auth/saml/callback") {
        errorContext = "auth.providerExchange";
        if (!entry.credential) {
          setError(authWords("authGate:actionMissingToken"));
          setStep("auth-action-result");
          setBusy(false);
          return;
        }
        const session = await completeAuthenticationOnce(
          `saml:${entry.credential}`,
          () => api.exchangeSAMLCompletion(entry.credential),
        );
        if (live()) await acceptSession(session);
        return;
      }
      if (entry.pathname === "/auth/saml") {
        errorContext = "auth.providerExchange";
        if (!entry.organization) throw new Error(translate("authGate:providerNotReady"));
        // Sign-in comes back to the address it started at, which takes the
        // way back as a path (2026-10-20: a whole address was refused).
        window.location.assign(api.authSAMLStartURL(entry.organization, "/auth/saml/callback"));
        return;
      }
      if (entry.pathname === "/reset-password") {
        if (!live()) return;
        setPassword("");
        setPasswordConfirm("");
        setStep(entry.credential ? "password-reset" : "auth-action-result");
        setError(entry.credential ? "" : authWords("authGate:actionMissingToken"));
        setBusy(false);
        // The link is checked as the page opens, not after both fields are typed (2026-10-16).
        if (entry.credential) {
          api.checkPasswordReset(entry.credential).catch((caught: unknown) => {
            if (!live() || !deadResetLink(caught)) return;
            setResetLinkDead(true);
            setError(authErrorLine(caught, "auth.passwordResetConfirm"));
          });
        }
        return;
      }

      const [status, providerOptions, runtime, passkeyOptions] = await Promise.all([
        instanceBootstrapService.readStatus(),
        authProviderService.listOptions().catch(() => []),
        // A later try reads them anew: one the server never answered may still be waiting.
        fetchRuntimeConfig(again),
        api.passkeyCapability(),
      ]);
      // The address's link has had its one go here — a refused device code
      // or provider return says why below, a setup code fills its field —
      // so a sign-out later in this load doesn't try it again.
      spendAuthEntry();
      if (!live()) return;
      setProviders(providerOptions);
      setPasskeyCapability(passkeyOptions);
      setEmailRecoveryAvailable(runtime.config.emailRecoveryAvailable);
      setCompanySignIn(runtime.config.companySignIn ?? false);
      setSignupPolicy({ closed: runtime.config.signupClosed, contact: runtime.config.signupContact, mail: runtime.config.emailRecoveryAvailable });
      if (entry.failure?.code === authErrorCodes.signupRestricted) {
        setError(authLine((t) => signupClosedNotice(t, runtime.config.signupContact, runtime.config.emailRecoveryAvailable)));
      }
      // An invitation in the address opens sign-up, with the invited
      // address where it is an email invitation, as Notion's link does.
      if (!status?.setupAvailable && signupInvitation) {
        // An invitation asks only a name (the multi-server decision of 2026-10-20).
        setDisplayName((current) => current || tabSuggestedName(i18next.language));
        setStep("invitation");
        api.signupInvitation(signupInvitation).then((found) => {
          if (!live()) return;
          // An invitation by name starts with the name its inviter wrote.
          if (found.name) setDisplayName(found.name);
          invitation.setDetails({ inviterName: found.inviterName, workspaceName: found.workspaceName,
            pageTitle: found.pageTitle, message: found.message, asksToJoin: found.asksToJoin, pageId: found.pageId });
          if (!found.email) return;
          invitation.setEmail(found.email);
          setEmail(found.email);
        }, (caught: unknown) => {
          // One used, withdrawn or old makes no account and leaves (useSignupInvitation.ts): its
          // person may have one. Another failure says what it was, the invitation kept to try again.
          if (!live() || invitation.end(caught, { suggestedName: true })) return;
          setStep("signin");
          setError(authErrorLine(caught, "auth.signUp"));
        });
      } else if ((deviceFailure || deviceChoice || deviceAtCapacity) && !status?.setupAvailable) {
        setStep("device-code");
        setError(deviceFailure);
        capacityOfferOpened.current = deviceAtCapacity;
        setSessionCapacityOffered(deviceAtCapacity);
      } else if (status?.setupAvailable) {
        // Nobody has signed in on a server not set up: an account this
        // browser remembers is another server's and doesn't set the language.
        await returnToDeviceLanguage().catch(() => undefined);
        if (!live()) return;
        // A friendly name to start with, which the person changes at will;
        // the code comes from the setup link or is typed.
        // In the app's window, the person's name on the device and the
        // code its shell handed.
        setDisplayName((current) => current || appServer.name || tabSuggestedName(i18next.language));
        const linked = entry.pathname === "/setup" ? entry.credential : appServer.setupCode ?? "";
        setSetupCode(linked);
        setSetupCodeNeeded(status.setupAuthorizationRequired && !linked);
        setStep("setup");
      } else setStep("signin");
      setBusy(false);
    } catch (caught) {
      if (!live()) return;
      // No answer, or the server says it is away or updating: the link the
      // address carries waits for the next try, and the screen says so
      // (SignInWaiting.tsx) — not a line to reload under ways that can't work.
      if (isServerUnreachable(caught)) {
        setStep("server-away");
        setBusy(false);
        return;
      }
      // A link that failed (an email check, a provider's return) isn't tried again either.
      spendAuthEntry();
      setError(authErrorLine(caught, errorContext));
      setStep("signin");
      setBusy(false);
    } finally {
      clearTimeout(late);
    }
  };
  startAgain.current = () => start(true);
  void start();
  return () => { mounted = false; };
}
