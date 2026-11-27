import { type FormEvent, useEffect, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { answeringServer, typedServerAddress } from "@/lib/typedServerAddress";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { type AgentFirstRunRequest, desktopFirstRun, type FirstRunState } from "@/lib/desktopFirstRun";
import { agentSignInRefusal, signInWithAgentCode } from "@/lib/agentCodeSignIn";
import { authErrorMessage } from "@/lib/authErrorPresentation";
import { activeProfileId, addServerProfile, LOCAL_PROFILE_ID } from "@/lib/profileRegistry";
import { openProfile, takeFirstScreenSignIn } from "@/lib/profileSwitch";
import { wakeTabsBehind } from "@/lib/desktopTabs";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { useViewEntryFocus } from "@/lib/viewEntryFocus";
import styles from "../AuthGate.module.css";
import css from "./FirstInstall.module.css";
import { QrCodeIcon } from "@/icons/leafen/deviceIcons";
import { scanCode } from "@/lib/codeScanner";
import { askBrowserSignIn, linkAsksBrowserSignIn } from "@/lib/openInApp";
import { listenForPackagedLinks, packagedLinkTarget } from "@/lib/packagedLinks";
import { firstRunLanguage } from "@/lib/firstRunLanguage";
import { FirstRunLanguageStep } from "@/components/auth/LanguageStep";
import { pageMarkSource } from "@/lib/pageMark";
import { coverDocumentChrome } from "@/lib/documentChrome";

// How often the first screen looks for what an agent chose from the terminal.
const AGENT_REQUEST_EVERY = 1500;

// The app's first start asks its language first, chosen ahead from the
// device's (the multi-server decision of 2026-10-19, 1; user 2026-11-19),
// then how to start (user decisions 2026-11-09 and 2026-11-11): start new on
// this computer — which makes this device the server, movable later; on a
// phone a trial for one person that stops with the app — or sign in to a
// Leafen used already, on a company or team server, another device or run
// apart on this same computer (by 127.0.0.1 and its port), by an invite link
// or its address (on a phone, also by the QR code another device shows),
// which runs no server here. Signing in opens that server's own sign-in,
// where the app can also sign in with the browser. The rest is the browser's
// flow too (the one-program decision of 2026-10-27, 5): the name, which takes
// the language chosen here, then the outside address and agents
// (FirstRunSteps); a server signed in to keeps its accounts' own language. An
// agent going through the first run from the terminal (`leafen setup start`
// or `sign-in`) chooses as the person told it, and this screen follows; given
// a one-time code from a device signed in there, it signs in with it and opens
// that server, nothing left for the person here.

export function FirstInstall({ onStartHere }: { onStartHere: () => void }) {
  const { t } = useTranslation("firstInstall");
  const [state, setState] = useState<FirstRunState | null>(null);
  const [address, setAddress] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useAttemptNotice<string>(address);
  // Signing in, opened from its button on the first screen, or asked for
  // from a server the app couldn't reach (returnToFirstScreenSignIn).
  const [other, setOther] = useState(takeFirstScreenSignIn);
  // The language, asked before anything (lib/firstRunLanguage.ts); kept through a reload.
  const [languageChosen, setLanguageChosen] = useState(() => firstRunLanguage() !== "");
  const panelRef = useRef<HTMLElement>(null);

  useEffect(() => {
    void desktopFirstRun.state().then((current) => {
      setState(current);
      // The server the computer's organization named is filled in, the only choice.
      if (current.managed?.serverUrl) setAddress(current.managed.serverUrl);
    }, () => setState(null));
  }, []);

  // A page's "open in the app" (an invitation, a device code, any page of a
  // server) opens that server's connection without the first question.
  useEffect(() => listenForPackagedLinks((links) => {
    const link = links[links.length - 1];
    const target = packagedLinkTarget(link, "", []);
    if (!target?.server) return;
    void (async () => {
      setBusy(true);
      const profile = addServerProfile(target.server);
      if (linkAsksBrowserSignIn(link)) askBrowserSignIn(target.server);
      await desktopFirstRun.connectedToServer().catch(() => undefined);
      openProfile(profile?.id ?? target.server, "", target.path);
    })();
  }), []);

  async function connect(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await connectTo(address);
  }

  async function connectTo(typed: string) {
    const { origins, path } = typedServerAddress(typed);
    if (origins.length === 0) {
      setError(t("invalidAddress"));
      return;
    }
    setBusy(true);
    setError("");
    const found = await answeringServer(origins);
    if (!("origin" in found)) {
      setBusy(false);
      setError(t(found.failure === "plain" ? "plainAddressRefused" : "notLeafen"));
      return;
    }
    const { origin } = found;
    const profile = addServerProfile(origin);
    // The app's other tabs follow the choice awake (wakeTabsBehind).
    wakeTabsBehind();
    await desktopFirstRun.connectedToServer().catch(() => undefined);
    openProfile(profile?.id ?? origin, "", path);
  }

  // A scanned code goes through the same checks as a pasted one; a camera
  // that is refused or not there leaves pasting.
  async function scan() {
    setBusy(true);
    setError("");
    const scanned = await scanCode();
    if (scanned.outcome === "scanned") {
      setAddress(scanned.text);
      await connectTo(scanned.text);
      return;
    }
    setBusy(false);
    if (scanned.outcome !== "cancelled") setError(t("scanUnavailable"));
  }

  // An agent's sign-in with the one-time code the person got from a device
  // signed in there (user decision 2026-11-13): it signs in as the code's
  // link would and opens that server, and tells the agent's command how it
  // went. A refused code leaves this screen as it was, with why, for the
  // agent to hand another.
  async function signInByAgent(request: AgentFirstRunRequest & { signIn: string; code: string }) {
    setBusy(true);
    setError("");
    const answer = (account: string, refused: string) =>
      desktopFirstRun.answerAgentSignIn(request.at, account, refused).catch(() => undefined);
    const { origins, path } = typedServerAddress(request.signIn);
    const found = origins.length ? await answeringServer(origins) : { failure: "none" as const };
    if (!("origin" in found)) {
      await answer("", found.failure === "plain" ? "plain" : "address");
      setBusy(false);
      setError(t(found.failure === "plain" ? "plainAddressRefused" : "notLeafen"));
      return;
    }
    try {
      const session = await signInWithAgentCode(found.origin, request.code, Boolean(request.signOutLeastUsed));
      const profile = addServerProfile(found.origin);
      await answer(session.user.name || session.user.email || session.user.id, "");
      openProfile(profile?.id ?? found.origin, "", path);
    } catch (caught) {
      await answer("", agentSignInRefusal(caught));
      setBusy(false);
      setError(authErrorMessage(caught, "auth.deviceCode"));
    }
  }

  async function startHere() {
    setBusy(true);
    setError("");
    // The app's other tabs follow the start awake, till the server answers (main.tsx).
    wakeTabsBehind();
    try {
      await desktopFirstRun.startHere();
      onStartHere();
    } catch {
      setBusy(false);
      setError(t("startFailed"));
    }
  }

  const phone = state?.phone ?? false;
  const managedServer = state?.managed?.serverUrl ?? "";
  const managed = Boolean(managedServer);
  const view = managed ? "managed" : other ? "other" : "first";
  // Each view takes the focus as it replaces the other; "뒤로" from signing
  // in gives it back to the "로그인" that opened it, not to the first way
  // (2026-11-16), the first view opening on its own button's autofocus.
  useViewEntryFocus(panelRef, view, { enterFirst: false });
  // Each view names the window and its tab after its head, as the first
  // run's other steps do (coverDocumentChrome): how to start while it asks,
  // signing in while that shows. The language step before it names its own.
  const screenName = view === "first" ? t("choiceHead") : t("connectTitle");
  useLayoutEffect(() => (languageChosen ? coverDocumentChrome(screenName) : undefined), [languageChosen, screenName]);
  // A computer whose organization names its server follows its other tabs
  // as any other does (2026-11-14: a tab behind stayed on this screen after
  // the one in front opened the server); an agent there signs in to that
  // server alone, and starting one here is not for it.
  useAgentRequest(state && !phone ? state : null, {
    busy,
    chosen: onStartHere,
    start: () => { if (!managed) void startHere(); },
    signIn: (request) => {
      if (managed && !sameServer(request.signIn, managedServer)) return;
      setOther(true);
      setAddress(request.signIn);
      void (request.code ? signInByAgent({ ...request, code: request.code }) : connectTo(request.signIn));
    },
  });

  if (!languageChosen) return <FirstRunLanguageStep onChosen={() => setLanguageChosen(true)} />;

  // The first screen asks how to start, each way a button with who it is
  // for under it (user decisions 2026-11-09 and 2026-11-11): on a computer
  // starting new is the big one, focused, signing in the outlined one under
  // it; on a phone signing in is the big one, trying it on the phone the
  // outlined one. Signing in opens its screen in their place, with "뒤로"; a
  // computer whose server is set by its management has only that sign-in.
  const connectPath = (
    <form className={`${styles.form} ${css.path}`} onSubmit={(event) => void connect(event)} data-first-install-connect>
      <div className={styles.notice}>
        <strong>{t("connectTitle")}</strong>
        <p>{phone ? t("phoneConnectBody") : t("connectBody")}</p>
      </div>
      {/* Named over the field and said under it, as Notion's sign-in names its email field. */}
      <label className={styles.label} htmlFor="first-install-address">{t("address")}</label>
      <input id="first-install-address" className={styles.input} value={address} placeholder={t("addressPlaceholder")} autoFocus={fieldFocusOnOpen()}
        autoCapitalize="off" autoCorrect="off" spellCheck={false} disabled={busy} readOnly={Boolean(state?.managed?.serverUrl)}
        aria-invalid={error ? true : undefined} aria-describedby={error ? "first-install-error" : "first-install-address-help"}
        onChange={(event) => setAddress(event.target.value)} />
      {/* A Leafen run apart on this computer is signed in to by this device's address (127.0.0.1, plainAddress.ts). */}
      <p id="first-install-address-help" className={styles.fieldHelp}>{t("addressHelp")}{phone || managed ? null : <> {t("addressThisComputer")}</>}</p>
      <button className={styles.primary} type="submit" disabled={busy || !address.trim()}>{t("connect")}</button>
      {phone ? (
        <button className={`${styles.secondary} ${css.scan}`} type="button" disabled={busy} onClick={() => void scan()} data-first-install-scan>
          <QrCodeIcon size="small" />{t("scanCode")}
        </button>
      ) : null}
      {error ? <p id="first-install-error" className={styles.error} role="alert" aria-live="assertive">{error}</p> : null}
    </form>
  );
  const openSignIn = () => { setError(""); setOther(true); };
  const freshWay = (
    <div className={styles.otherWay} key="fresh">
      <button className={phone ? styles.outlined : styles.primary} type="button" disabled={busy} autoFocus={!phone} onClick={() => void startHere()}
        aria-describedby="first-install-fresh-note" data-first-install-fresh>
        {phone ? t("phoneOnly") : t("fresh")}
      </button>
      <p id="first-install-fresh-note" className={styles.otherWayNote}>{phone ? t("phoneOnlyBody") : t("freshBody")}</p>
    </div>
  );
  const signInWay = (
    <div className={styles.otherWay} key="sign-in">
      <button className={phone ? styles.primary : styles.outlined} type="button" disabled={busy} autoFocus={phone} onClick={openSignIn}
        aria-describedby="first-install-sign-in-note" data-first-install-other="connect">
        {t("otherConnect")}
      </button>
      <p id="first-install-sign-in-note" className={styles.otherWayNote}>{phone ? t("phoneSignInNote") : t("otherConnectNote")}</p>
    </div>
  );
  const choice = (
    <div className={`${styles.form} ${css.path}`} data-first-install-start>
      <h2 className={css.choiceHead}>{t("choiceHead")}</h2>
      {phone ? [signInWay, freshWay] : [freshWay, signInWay]}
      {error ? <p id="first-install-error" className={styles.error} role="alert" aria-live="assertive">{error}</p> : null}
    </div>
  );
  return (
    <main className={styles.screen} data-first-install="server">
      <section ref={panelRef} className={styles.panel} aria-busy={busy}>
        <div className={styles.brand}>
          <img className={styles.mark} src={pageMarkSource()} alt="" aria-hidden="true" />
          <div><h1>Leafen</h1><p>{t("serverSubtitle")}</p></div>
        </div>
        <div className={css.paths} data-first-install-view={view}>
          {managed || other ? connectPath : choice}
          {managed ? (
            <div className={`${styles.form} ${css.path}`} data-first-install-managed>
              <div className={styles.notice}><p>{t("managedServer")}</p></div>
            </div>
          ) : null}
        </div>
        {other && !managed ? (
          <button className={styles.linkButton} type="button" disabled={busy} onClick={() => { setError(""); setOther(false); }}
            data-first-install-other="back">
            {t("common:actions.back")}
          </button>
        ) : null}
      </section>
    </main>
  );
}

/** Whether an address names the same server as another, however each was written. */
function sameServer(typed: string, server: string): boolean {
  const named = typedServerAddress(server).origins[0];
  return Boolean(named) && typedServerAddress(typed).origins.includes(named);
}

/**
 * What an agent chose from the terminal for the person (`leafen setup start`
 * or `leafen setup sign-in`, desktop/first_run.go), each once: the screen goes
 * on as if its button were pressed, or signs in with the code it brings. What
 * another of the app's tabs chose, this one follows, hidden or not, as the
 * first run's later steps do (2026-11-14): started here, it starts as that
 * one did (`chosen`, which finds the server starting); signed in to another
 * Leafen, it opens the one that tab opened.
 */
function useAgentRequest(state: FirstRunState | null,
  act: { busy: boolean; chosen(): void; start(): void; signIn(request: AgentFirstRunRequest & { signIn: string }): void }) {
  const actions = useRef(act);
  actions.current = act;
  const watching = state !== null;
  useEffect(() => {
    if (!watching) return undefined;
    let live = true;
    let timer = 0;
    let handled = 0;
    const look = async () => {
      const now = await desktopFirstRun.state().catch(() => null);
      if (!live) return;
      // This tab's own choice goes on by itself; another tab's opened a server once its profile is the one opened last.
      if (now && !actions.current.busy && (now.choice === "local" || (now.choice === "server" && activeProfileId() !== LOCAL_PROFILE_ID))) {
        actions.current.chosen();
        return;
      }
      const request = now?.request;
      if (request && request.at > handled) {
        handled = request.at;
        if (request.startHere) {
          actions.current.start();
          return;
        }
        if (request.signIn) actions.current.signIn({ ...request, signIn: request.signIn });
      }
      timer = window.setTimeout(() => void look(), AGENT_REQUEST_EVERY);
    };
    void look();
    return () => {
      live = false;
      window.clearTimeout(timer);
    };
  }, [watching]);
}
