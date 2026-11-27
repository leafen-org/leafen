"use client";

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import styles from "./ProductLoadingScreen.module.css";
import { pageMarkSource } from "@/lib/pageMark";

// How long from the page's start the screen stays quiet: a returning
// person's cached workspace is drawn inside it (0.4–0.7 s measured
// 2026-10-16), so opening again shows the workspace, as Notion's does, and
// not this screen first.
const STARTUP_QUIET_MS = 1000;
const STEP_QUIET_MS = 240;
// How long one step can leave the screen before the next takes it up.
const STEP_GAP_MS = 120;

/**
 * Each step of starting draws this screen in turn, and they are one showing:
 * it comes up once and stays as it is from step to step (2026-11-05, the
 * app's start: each step faded it in anew, so it blinked). A later one, after
 * the app was on screen, is new.
 */
type Showing = { revealAt: number; onDevice: boolean; mounted: number; leftAt: number };
let showing: Showing | null = null;

function joinShowing(source: string): Showing {
  const now = performance.now();
  if (!showing || (showing.mounted === 0 && now - showing.leftAt > STEP_GAP_MS)) {
    // The page's own screen (index.html's boot-screen) showed the mark from a
    // second after the start while this code came: the first showing carries
    // on from it, already shown, rather than going blank to fade in again.
    const fromPage = !showing && typeof document !== "undefined" && document.getElementById("boot-screen") !== null;
    showing = {
      revealAt: fromPage ? STARTUP_QUIET_MS : now + Math.max(STEP_QUIET_MS, STARTUP_QUIET_MS - now),
      onDevice: false, mounted: 0, leftAt: now,
    };
  }
  showing.onDevice ||= source === "device";
  return showing;
}

/**
 * Minimal first-visit fallback: the mark alone in the middle of the screen,
 * as Notion's (the in-app credit and quotes were taken out, user decision
 * 2026-11-25 — a line that showed for a moment and went before it was read).
 * It stays empty for a quiet moment, so a returning person's workspace
 * replaces it before it shows — what the page showed before stays on screen
 * meanwhile. While the packaged app's own server starts, it says so under the
 * mark, and that it is updating while a new version brings its data up to
 * date; the mark stays where it was.
 */
export function ProductLoadingScreen({
  deferred = true,
  source,
  updating = false,
}: {
  deferred?: boolean;
  source: "auth" | "workspace" | "device";
  updating?: boolean;
}) {
  const { t } = useTranslation(["app", "common"]);
  const [current] = useState(() => joinShowing(source));
  useEffect(() => {
    current.mounted += 1;
    return () => {
      current.mounted -= 1;
      current.leftAt = performance.now();
    };
  }, [current]);
  // Timed as it reaches the screen, so a step taking over carries on from where
  // the last one's fade had got to: past the moment the delay is negative.
  const screen = useRef<HTMLElement>(null);
  useLayoutEffect(() => {
    screen.current?.style.setProperty("--loading-reveal-delay", `${Math.round(current.revealAt - performance.now())}ms`);
  }, [current]);
  // Nothing shows for the quiet moment, and what is not seen is not read out
  // or reached by Tab either (2026-11-05: a link under the mark was in the
  // accessibility tree of the blank first second).
  // It is reachable once its fade has begun, not after a time: a window in
  // the background holds the fade, and the screen with it.
  const content = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const node = content.current;
    if (!node || !deferred) return undefined;
    node.inert = true;
    return () => { node.inert = false; };
  }, [deferred]);
  const label = updating ? t("app:updatingOnDevice") : current.onDevice ? t("app:startingOnDevice") : t("app:loading");

  return (
    <main
      ref={screen}
      className={`${styles.screen}${deferred ? ` ${styles.deferred}` : ""}`}
      data-testid="product-loading-screen"
      data-loading-screen=""
      data-source={source}
      aria-busy="true"
      aria-label={label}
      role="status"
    >
      <div ref={content} className={`${styles.content} ${styles.loadingContent}`}
        onAnimationStart={(event) => { if (event.target === event.currentTarget && content.current) content.current.inert = false; }}>
        <img
          className={styles.mark}
          src={pageMarkSource()}
          alt=""
          aria-hidden="true"
          width="48"
          height="48"
          fetchPriority="high"
          data-testid="product-loading-mark"
        />
        {current.onDevice ? <p className={styles.status}>{label}</p> : null}
      </div>
    </main>
  );
}
