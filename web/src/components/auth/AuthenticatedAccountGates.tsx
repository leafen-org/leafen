import { type FormEvent, type ReactNode, startTransition, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { takeFreshAccount } from "@/lib/freshAccount";
import { firstRunLanguage, forgetFirstRunLanguage } from "@/lib/firstRunLanguage";
import { firstRunSetupState, hearFirstRun, readFirstRunStepsAhead, type FirstRunState } from "@/lib/firstRunSteps";
import { followAcrossTabs, hearTabs, tellTabs } from "@/lib/followAcrossTabs";
import { wakeTabsBehind } from "@/lib/desktopTabs";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { authBounds, type AccountLanguagePreference, type AccountLanguageState } from "@/authWire.generated";
import {
  applyAccountLanguage,
  hasCachedLanguagePreference,
  isSelectableLanguagePreference,
  resolvedLanguageForPreference,
  signedOutLanguage,
} from "@/i18n";
import { createAccountLanguageService } from "@/lib/accountLanguageService";
import { settleAccountLanguage } from "@/lib/accountLanguageSettled";
import { useAccountDisplayPreferenceSync } from "@/lib/displayPreferences";
import { accountPasswordService } from "@/lib/accountPasswordService";
import { clearSessionIfCurrent, loadSession } from "@/lib/authSessionStore";
import { revokeCurrentServerSession } from "@/lib/productSignOut";
import { forgetSignInPassword, readSignInPassword } from "@/lib/signInPasswordHandoff";
import {
  readAuthenticatedAccountGateState,
  type AuthenticatedAccountGateState,
} from "@/lib/authenticatedAccountGateState";
import { authErrorMessage } from "@/lib/authErrorPresentation";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import { resetDocumentChrome } from "@/lib/documentChrome";
import { prepareInitialWorkspace } from "@/lib/initialWorkspaceSetup";
import { newId } from "@/lib/ids";
import { openSettled, type SettledLanguage } from "@/lib/firstAdministratorStart";
import { ProductLoadingScreen } from "@/components/loading/ProductLoadingScreen";
import type { Session } from "@/types";
import styles from "../AuthGate.module.css";
import { LanguageStep, LeafenBrand } from "./LanguageStep";
import { passwordAcceptable } from "@/lib/passwordRule";
import { fieldFocusOnOpen } from "@/lib/screenKind";

type Gate = "checking" | "clear" | "password" | "language";

const accountGateTransport = {
  readPasswordState: (session: Session, signal?: AbortSignal) => api.readAccountState(session, signal),
  readLanguageState: (session: Session, signal?: AbortSignal) => api.readAccountLanguage(session, signal),
};

// Where the language step went once done (lib/firstAdministratorStart.ts).
type Settled = SettledLanguage;
// What a tab tells the person's other tabs of the step (followAcrossTabs):
// the language as it read it, or, once it finished the step, with where it
// went, which they follow without asking anything.
type LanguageWord = AccountLanguageState & { settled?: Settled };
const languageTopic = (userId: string) => `account-language:${userId}`;

export function AuthenticatedAccountGates({
  children,
  session,
  onSession,
}: {
  children: ReactNode;
  session: Session;
  onSession(session: Session | null): void;
}) {
  const { t } = useTranslation([
    "authGate",
    "workspaceSettingsDialog",
    "importDialog",
    "settingsErrors",
  ]);
  // The account's display preferences, as this device last saw them and
  // then as the server keeps them.
  useAccountDisplayPreferenceSync(session);
  // The first run's steps are known before the workspace opens on them.
  useEffect(() => { readFirstRunStepsAhead(session); }, [session]);
  const cachedLanguage = hasCachedLanguagePreference(session.user.id);
  // An account made here just now asks its language at once, unless it came
  // with the one chosen on the first screen, which it takes without asking
  // (lib/freshAccount.ts); null when it wasn't made here.
  const [fresh] = useState(() => (cachedLanguage ? null : takeFreshAccount(session.user.id)));
  const freshAccount = fresh !== null;
  const [gate, setGate] = useState<Gate>(cachedLanguage ? "clear" : fresh === "" ? "language" : "checking");
  const [languageRequired, setLanguageRequired] = useState(!cachedLanguage);
  // The temporary password typed at sign-in a moment ago, not asked again.
  const [currentPassword, setCurrentPassword] = useState(() => readSignInPassword(session.user.id));
  const [currentPasswordHanded, setCurrentPasswordHanded] = useState(() => currentPassword !== "");
  const [nextPassword, setNextPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  // The device's language, or the one chosen on the first screen before signing in here.
  const recommendedLanguage = useMemo(signedOutLanguage, []);
  const [selectedLanguage, setSelectedLanguage] = useState<AccountLanguagePreference>(
    () => (firstRunLanguage() || recommendedLanguage) as AccountLanguagePreference,
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useAttemptNotice<string>(currentPassword, nextPassword, confirmPassword, selectedLanguage);
  // The field a refused change is about, marked invalid and read with the red line.
  const [rejectedField, setRejectedField] = useState<"current" | "next" | "confirm">("next");
  const rejection = (field: typeof rejectedField, help?: string) => error && rejectedField === field
    ? { "aria-invalid": true as const, "aria-describedby": [help, "must-change-error"].filter(Boolean).join(" ") }
    : { "aria-describedby": help };
  const operationGeneration = useRef(0);
  const initialWorkspaceRequest = useRef(newId());
  const languageSubmission = useRef<AbortController | null>(null);
  const cleared = useRef(false);

  // Checked still, the screen is loading and names nothing yet.
  useLayoutEffect(() => {
    if (gate !== "clear") resetDocumentChrome(gate === "checking");
    // The account has its language: the first screen's choice is spent (lib/firstRunLanguage.ts).
    else forgetFirstRunLanguage();
  }, [gate]);

  useEffect(() => {
    const controller = new AbortController();
    const generation = ++operationGeneration.current;
    if (!cachedLanguage && !freshAccount) setGate("checking");
    void readAuthenticatedAccountGateState(accountGateTransport, session, controller.signal)
      .then(async (state) => {
        if (controller.signal.aborted || generation !== operationGeneration.current) return;
        // A language chosen before the account was made — by an agent's
        // command (`leafen setup start --language`) or on the first screen,
        // which the server gave the account as it made it — finishes the step
        // as choosing it here does; the first screen's, kept here, where the
        // account has none. One that is hidden now (i18n/languages.ts) can't
        // be saved, so the step asks instead.
        const chosen = state.language?.languageOnboardingCompleted ? state.language.languagePreference : null;
        const hiddenChoice = Boolean(chosen) && !isSelectableLanguagePreference(chosen);
        const carried = chosen ? (hiddenChoice ? null : chosen) : isSelectableLanguagePreference(fresh) ? fresh : null;
        if (freshAccount && carried && !state.mustChangePassword) {
          void saveLanguage(carried as AccountLanguagePreference);
          return;
        }
        // An account without one — invited, or signed in to a server — is asked
        // its language here, in the app as in a browser; the first
        // administrator chose it on the setup's first screen (2026-11-19).
        const language = freshAccount && hiddenChoice ? "required" : await applyLanguageState(session.user.id, state);
        if (controller.signal.aborted || generation !== operationGeneration.current) return;
        if (!state.mustChangePassword) forgetSignInPassword();
        // A reload brings the account's language: the gate stays as it is till then.
        if (language === "reloading") return;
        // Here, it stays for this load; one still to be chosen settles once chosen (goOn).
        if (language === "here") settleAccountLanguage();
        const nextLanguageRequired = language === "required";
        setLanguageRequired(nextLanguageRequired);
        setGate(state.mustChangePassword
          ? "password"
          : nextLanguageRequired ? "language" : "clear");
      });
    return () => {
      operationGeneration.current += 1;
      languageSubmission.current?.abort();
      controller.abort();
    };
  }, [cachedLanguage, fresh, freshAccount, session]);

  // A language an agent chose from the terminal (`leafen setup language`)
  // finishes the step while it shows, as choosing it here does (user
  // decision 2026-11-09). It is looked for only while an agent goes through
  // the first run by commands, as the server says (lib/firstRunSteps.ts
  // `follow`, read ahead as the account opens), and again as the person comes
  // back to the window (2026-11-13: the step read the language every two
  // seconds for anyone, about 25 reads a minute, with no agent at all). Then
  // one of the person's tabs in sight asks for all of them, and a tab behind
  // asks nothing (followAcrossTabs). The tab that read it finishes the step;
  // the person's other tabs, behind ones too, go where it went once it tells
  // them, as they do when the person chose in it (2026-11-13: tabs behind
  // stayed on the step for minutes after the tab in front went on).
  useEffect(() => {
    if (gate !== "language" || busy) return undefined;
    const userId = session.user.id;
    const topic = languageTopic(userId);
    let live = true;
    let stopAsking: (() => void) | null = null;
    const stopHearing = hearTabs(topic, (value, here) => {
      const language = value as LanguageWord | undefined;
      if (!language?.languageOnboardingCompleted || !isSelectableLanguagePreference(language.languagePreference)) return;
      const chosen = language.languagePreference as AccountLanguagePreference;
      if (language.settled) {
        if (!here) goOn(chosen, language.settled);
      } else if (here) void saveLanguage(chosen);
    });
    const follow = (state: FirstRunState) => {
      if (!live) return;
      if (state.follow && !stopAsking) {
        stopAsking = followAcrossTabs(topic, 2000,
          () => accountGateTransport.readLanguageState(session).then((language) => tellTabs(topic, language)),
          () => undefined, { now: true });
      } else if (!state.follow && stopAsking) {
        stopAsking();
        stopAsking = null;
      }
    };
    const look = () => { void firstRunSetupState(session).then(follow, () => undefined); };
    const stopFirstRun = hearFirstRun(userId, follow);
    const comeBack = () => { if (document.visibilityState === "visible") look(); };
    look();
    window.addEventListener("focus", comeBack);
    document.addEventListener("visibilitychange", comeBack);
    return () => {
      live = false;
      stopAsking?.();
      stopHearing();
      stopFirstRun();
      window.removeEventListener("focus", comeBack);
      document.removeEventListener("visibilitychange", comeBack);
    };
    // saveLanguage and goOn read the session they render with.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [busy, gate, session]);

  // The account's language: still to be chosen, brought by a reload, or here
  // (the page reads in it, switched in place where it didn't reload).
  async function applyLanguageState(userId: string, state: AuthenticatedAccountGateState): Promise<"here" | "required" | "reloading"> {
    const language = state.language;
    if (!language) return "here";
    if (!language.languageOnboardingCompleted || !language.languagePreference) return "required";
    return await applyAccountLanguage(userId, language.languagePreference) ? "here" : "reloading";
  }

  // Someone who can't change it now — another person's account, a wrong
  // temporary password — leaves, as the other sign-in screens let them.
  async function signOut() {
    const generation = ++operationGeneration.current;
    setBusy(true);
    setError("");
    try {
      await revokeCurrentServerSession(api, session);
    } catch {
      // The server may keep the session; this browser forgets it either way.
    }
    if (generation !== operationGeneration.current) return;
    await clearSessionIfCurrent(session);
    onSession(loadSession());
  }

  async function submitPassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!passwordAcceptable(nextPassword)) {
      setRejectedField("next");
      setError(t("authGate:passwordTooShort"));
      return;
    }
    if (nextPassword !== confirmPassword) {
      setRejectedField("confirm");
      setError(t("authGate:passwordMismatch"));
      return;
    }
    const generation = ++operationGeneration.current;
    setBusy(true);
    setError("");
    try {
      const nextSession = await accountPasswordService.change(session, {
        currentPassword,
        newPassword: nextPassword,
        newPasswordConfirm: confirmPassword,
      });
      if (generation !== operationGeneration.current) return;
      forgetSignInPassword();
      onSession(nextSession);
      setGate(languageRequired ? "language" : "clear");
    } catch (caught) {
      if (generation !== operationGeneration.current) return;
      // A wrong current password reads as that, not as a failed sign-in.
      setRejectedField("current");
      setError(productErrorMessage(caught, "accountSecurity.password"));
      // A handed-over password the server no longer takes is asked for.
      if (currentPasswordHanded) {
        forgetSignInPassword();
        setCurrentPasswordHanded(false);
        setCurrentPassword("");
      }
    } finally {
      if (generation === operationGeneration.current) setBusy(false);
    }
  }

  async function saveLanguage(selectedLanguage: AccountLanguagePreference) {
    const generation = ++operationGeneration.current;
    languageSubmission.current?.abort();
    const controller = new AbortController();
    languageSubmission.current = controller;
    setBusy(true);
    setError("");
    try {
      const resolved = resolvedLanguageForPreference(selectedLanguage);
      const initial = await prepareInitialWorkspace({ session, language: resolved,
        requestId: initialWorkspaceRequest.current, signal: controller.signal,
        isCurrent: () => generation === operationGeneration.current });
      if (controller.signal.aborted || generation !== operationGeneration.current) return;
      await createAccountLanguageService(session).save(selectedLanguage);
      if (generation !== operationGeneration.current) return;
      const settled = { page: initial.page?.id ?? null, restricted: initial.restricted, joinable: initial.joinable };
      // The tabs behind go where this one went, awake (wakeTabsBehind).
      wakeTabsBehind();
      tellTabs(languageTopic(session.user.id),
        { languagePreference: selectedLanguage, languageOnboardingCompleted: true, settled } satisfies LanguageWord);
      goOn(selectedLanguage, settled);
    } catch (caught) {
      if (generation === operationGeneration.current) {
        setError(authErrorMessage(caught));
        // A language carried from the first start that couldn't be saved is chosen here.
        setLanguageRequired(true);
        setGate("language");
      }
    } finally {
      if (generation === operationGeneration.current && !cleared.current) setBusy(false);
    }
  }

  // The step done, in this tab or another of the person's: on to where it went.
  function goOn(language: AccountLanguagePreference, settled: Settled) {
    if (cleared.current) return;
    operationGeneration.current += 1;
    // An invite link's page stays to be answered (components/join): its
    // person came to join that workspace (2026-10-20: a name-only join
    // landed in a new workspace of their own instead).
    openSettled(settled);
    // The step stays up, still busy, until the product is ready
    // (AuthenticatedSession), which comes in the language chosen: while a
    // reload brings it, nothing behind draws or names the window in the one
    // the page had (2026-11-19: the next step's Korean title showed, kept
    // through the reload, for half a second after English was chosen).
    cleared.current = true;
    setBusy(true);
    void applyAccountLanguage(session.user.id, language).then((here) => {
      if (!here) return;
      settleAccountLanguage();
      startTransition(() => setGate("clear"));
    });
  }

  if (gate === "clear") return <>{children}</>;
  if (gate === "checking") return <ProductLoadingScreen source="auth" />;

  if (gate === "password") {
    return (
      <main className={styles.screen}>
        <section className={styles.panel} aria-busy={busy}>
          <LeafenBrand />
          <form className={styles.form} onSubmit={(event) => void submitPassword(event)} data-testid="must-change-password">
            <div className={styles.notice}>
              <strong>{t("authGate:mustChangeTitle")}</strong>
              <p>{t(currentPasswordHanded ? "authGate:mustChangeBodyHanded" : "authGate:mustChangeBody")}</p>
            </div>
            {currentPasswordHanded ? null : <>
              <label className={styles.label} htmlFor="must-change-current">{t("authGate:temporaryPassword")}</label>
              <input id="must-change-current" className={styles.input} type="password" autoComplete="current-password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} disabled={busy} required
                autoFocus={fieldFocusOnOpen()} {...rejection("current")} />
            </>}
            <label className={styles.label} htmlFor="must-change-next">{t("authGate:newPassword")}</label>
            <input id="must-change-next" className={styles.input} type="password" autoComplete="new-password" value={nextPassword} onChange={(event) => setNextPassword(event.target.value)} disabled={busy} required
              autoFocus={currentPasswordHanded && fieldFocusOnOpen()} {...rejection("next", "must-change-password-help")} />
            <p id="must-change-password-help" className={styles.fieldHelp}>{t("authGate:passwordRequirements", { count: authBounds.minimumPasswordLength })}</p>
            <label className={styles.label} htmlFor="must-change-confirm">{t("authGate:confirmPassword")}</label>
            <input id="must-change-confirm" className={styles.input} type="password" autoComplete="new-password" value={confirmPassword} onChange={(event) => setConfirmPassword(event.target.value)} disabled={busy} required
              {...rejection("confirm")} />
            <button className={styles.primary} type="submit" disabled={busy}>{t("authGate:changePasswordCta")}</button>
            {error ? <p id="must-change-error" className={styles.error} role="alert" aria-live="assertive">{error}</p> : null}
            <button className={styles.linkButton} type="button" onClick={() => void signOut()} disabled={busy} data-must-change-sign-out>
              {t("authGate:signOutInstead")}
            </button>
          </form>
        </section>
      </main>
    );
  }

  return (
    <LanguageStep value={selectedLanguage} recommended={recommendedLanguage} busy={busy} error={error ?? ""} testId="language-onboarding"
      onChange={setSelectedLanguage} onSubmit={() => void saveLanguage(selectedLanguage)} />
  );
}
