import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ProductLoadingScreen } from "@/components/loading/ProductLoadingScreen";
import { desktopFirstRun } from "@/lib/desktopFirstRun";
import { coverDocumentChrome } from "@/lib/documentChrome";
import { wakeTabsBehind } from "@/lib/desktopTabs";
import { isPackagedClient } from "@/lib/packagedClient";
import { activeProfileId, LOCAL_PROFILE_ID } from "@/lib/profileRegistry";
import { returnToFirstScreenSignIn, returnToLocalProfile } from "@/lib/profileSwitch";
import { answerWaitMs, serverHealthNow, type ServerHealth } from "@/lib/serverAnswers";
import { serverOrigin } from "@/lib/serverOrigin";
import { retryWhenServerMayAnswer } from "@/lib/serverReach";
import styles from "../AuthGate.module.css";
import css from "./SignInWaiting.module.css";
import { pageMarkSource } from "@/lib/pageMark";

// How soon the server is asked again while it is away, growing to the most.
const FIRST_LOOK_MS = 2_000;
const MOST_LOOK_MS = 10_000;
// How long the first look may take before the screen says the server is
// away: an updating server answers within it, and is said so at once.
const FIRST_WORD_MS = 400;
// A start that the server answering takes longer than this says it is slow;
// one taking longer than START_WAIT_MS (or a few round trips) is tried anew.
const SLOW_START_MS = 1_000;
const START_WAIT_MS = 10_000;
// How long "다시 시도" says it is trying, at least, once pressed.
const PRESS_SHOWN_MS = 600;

/** Where the app goes from a server it can't reach: none on the web, or as the device allows. */
type Ways = { phone: boolean; connectOther: boolean; startHere: boolean; backToDevice: boolean };

/**
 * The sign-in screen before a step (AuthGate): the quiet loading screen while
 * the start reads the server, and once the server can't be reached, is
 * updating, or hasn't answered in a few seconds (authGateStart.ts), a card
 * saying so — the address it tried, "다시 시도", and in the app the ways to
 * another server and to starting on this device —, which asks the server
 * again by itself and goes on to sign-in once it answers (2026-11-23: an app
 * pointed at a server that was off showed an empty card, then sign-in ways
 * that did nothing, with no word). A window signed in keeps the top bar's
 * "오프라인" instead.
 */
export function SignInWaiting({ away, inWindow = false, tryAgain }: { away: boolean; inWindow?: boolean; tryAgain: () => Promise<void> }) {
  if (!away) return <ProductLoadingScreen source="auth" />;
  return <ServerAwayCard inWindow={inWindow} tryAgain={tryAgain} />;
}

// In the window that adds an account (AddAccountWindow), the card is the window's, not a screen.
function ServerAwayCard({ inWindow, tryAgain }: { inWindow: boolean; tryAgain: () => Promise<void> }) {
  const { t } = useTranslation(["authGate", "common", "firstInstall"]);
  const [health, setHealth] = useState<ServerHealth | null>(null);
  const [trying, setTrying] = useState(false);
  const [startFailed, setStartFailed] = useState(false);
  const ways = useWaysFromHere();
  const looking = useRef<(pressed: boolean) => Promise<void>>(async () => undefined);
  // The window is the sign-in's, as its tab says (AuthGateScreen).
  const title = t("authGate:windowTitle.signin");
  useLayoutEffect(() => (inWindow ? undefined : coverDocumentChrome(title)), [inWindow, title]);

  useEffect(() => {
    let live = true;
    let timer = 0;
    let wait = FIRST_LOOK_MS;
    let busy = false;
    // Asks the server; once it answers, the sign-in's start goes again (it
    // replaces this screen, or leaves it as the server still isn't there). A
    // start that takes long says the server is slow, and the server is asked
    // again past a while, its start then tried anew (the newest try counts).
    const look = async (pressed: boolean) => {
      if (busy) return;
      busy = true;
      window.clearTimeout(timer);
      if (pressed) setTrying(true);
      const pressedAt = performance.now();
      let slow = 0;
      try {
        const now = await serverHealthNow();
        if (!live) return;
        if (now !== "answers") {
          setHealth(now);
          return;
        }
        slow = window.setTimeout(() => { if (live) setHealth("answers"); }, SLOW_START_MS);
        await Promise.race([tryAgain(), new Promise((done) => { window.setTimeout(done, answerWaitMs(START_WAIT_MS)); })]);
      } finally {
        window.clearTimeout(slow);
        busy = false;
        if (live) {
          // A refused try answers at once: the press shows it tried for a moment, not a flicker.
          const shown = performance.now() - pressedAt;
          if (pressed && shown < PRESS_SHOWN_MS) window.setTimeout(() => { if (live) setTrying(false); }, PRESS_SHOWN_MS - shown);
          else setTrying(false);
          wait = pressed ? FIRST_LOOK_MS : Math.min(MOST_LOOK_MS, wait * 1.5);
          timer = window.setTimeout(() => void look(false), wait);
        }
      }
    };
    looking.current = look;
    const word = window.setTimeout(() => setHealth((current) => current ?? "away"), FIRST_WORD_MS);
    void look(false);
    // The network or the window coming back asks at once, not at the next turn.
    const stop = retryWhenServerMayAnswer(() => { wait = FIRST_LOOK_MS; void look(false); });
    return () => {
      live = false;
      window.clearTimeout(timer);
      window.clearTimeout(word);
      stop();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once, as the card shows
  }, []);

  if (health === null) return inWindow ? null : <ProductLoadingScreen source="auth" />;
  const Screen = inWindow ? "div" : "main";
  const kind = health === "updating" ? "updating" : health === "update-failed" ? "updateFailed" : health === "answers" ? "slow" : "away";
  const startHere = async () => {
    setTrying(true);
    setStartFailed(false);
    // The app's other tabs follow the start awake, as the first screen's (FirstInstall).
    wakeTabsBehind();
    try {
      await desktopFirstRun.startHere();
      returnToLocalProfile();
    } catch {
      setTrying(false);
      setStartFailed(true);
    }
  };
  return (
    <Screen className={inWindow ? styles.screenInWindow : styles.screen} data-sign-in-server-away={kind}>
      <section className={styles.panel} aria-busy={trying}>
        <div className={styles.brand}>
          <img className={styles.mark} src={pageMarkSource()} alt="" aria-hidden="true" />
          <div><h1>Leafen</h1></div>
        </div>
        <div className={styles.form}>
          <div className={styles.notice} role="status" aria-live="polite">
            <strong>{t(`authGate:serverAway.${kind}.title`)}</strong>
            <p className={css.address} dir="ltr" data-sign-in-server-address>{serverOrigin()}</p>
            <p>{t(`authGate:serverAway.${kind}.body`)}</p>
          </div>
          <button className={styles.primary} type="button" disabled={trying} autoFocus={!inWindow} onClick={() => void looking.current(true)}
            data-sign-in-server-retry>
            {trying ? t("authGate:serverAway.retrying") : t("common:retry")}
          </button>
          {ways?.connectOther ? (
            <button className={styles.secondary} type="button" disabled={trying} onClick={returnToFirstScreenSignIn} data-sign-in-server-other>
              {t("authGate:serverAway.connectOther")}
            </button>
          ) : null}
          {ways?.startHere ? (
            <button className={styles.secondary} type="button" disabled={trying} onClick={() => void startHere()} data-sign-in-server-start-here>
              {ways.phone ? t("firstInstall:phoneOnly") : t("firstInstall:fresh")}
            </button>
          ) : null}
          {ways?.backToDevice ? (
            <button className={styles.linkButton} type="button" disabled={trying} onClick={returnToLocalProfile} data-sign-in-server-device>
              {t("authGate:backToThisDevice")}
            </button>
          ) : null}
          {startFailed ? <p className={styles.error} role="alert">{t("firstInstall:startFailed")}</p> : null}
        </div>
      </section>
    </Screen>
  );
}

/**
 * The app's ways from another server it can't reach (none on the web): on a
 * device that serves no Leafen of its own, to sign in to another server
 * (the first screen's sign-in) and to start one here; on one that does, back
 * to it; none where the computer's organization named the server and nothing
 * here serves (useWayBackToDevice's rule).
 */
function useWaysFromHere(): Ways | null {
  const [ways, setWays] = useState<Ways | null>(null);
  useEffect(() => {
    if (!isPackagedClient() || activeProfileId() === LOCAL_PROFILE_ID) return undefined;
    let current = true;
    void desktopFirstRun.state().then((state) => {
      if (!current) return;
      const own = state.ownServer !== false;
      const managed = Boolean(state.managed?.serverUrl);
      setWays({
        phone: state.phone,
        connectOther: !own && !managed,
        startHere: !own && !managed,
        backToDevice: own,
      });
    }, () => undefined);
    return () => { current = false; };
  }, []);
  return ways;
}
