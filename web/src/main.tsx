import "@/lib/fetchDeadlineInstall";
import { StrictMode, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { announce } from "react-aria/private/live-announcer/LiveAnnouncer";
import { keepDroppedFilesInApp } from "./lib/fileDropGuard";
import { appStylesReady } from "./lib/appStyles";
import { KeychainPromptNotice } from "@/components/loading/KeychainPromptNotice";
import { ProductLoadingScreen } from "@/components/loading/ProductLoadingScreen";
import { ServerKeyChangedScreen } from "@/components/loading/ServerKeyChangedScreen";
import { StartupRecoveryScreen } from "@/components/loading/StartupRecoveryScreen";
import { loadSession } from "@/lib/authSessionStore";
import { takeAddressHandoff } from "@/lib/addressHandoff";
import { initI18n } from "@/i18n";
import { AppLocale } from "@/lib/appLocale";
import { startLocalBundleFreshnessWatch } from "@/lib/devBundleFreshness";
import { FirstRunPending, LocalServerFailedError, startPackagedClient } from "@/lib/packagedClient";
import { followAgentSignIns } from "@/lib/agentCodeSignIn";
import { followKeychainPrompt, keychainPromptShowing } from "@/lib/keychainPrompt";
import { FirstInstall } from "@/components/firstInstall/FirstInstall";
import { wakeTabsBehind } from "@/lib/desktopTabs";
import { ServerKeyChangedError } from "@/lib/serverKeyPin";
import { registerServiceWorker } from "@/lib/serviceWorker";
import { reloadAfterStartWithoutServer } from "@/lib/reloadAtRest";
import { announceUpdateReady } from "@/lib/updateReadyNotice";
import { watchStaleBuild } from "@/lib/staleBuild";
import { watchOverflowTitles } from "@/lib/overflowTitles";
import { watchIconTips } from "@/lib/iconTips";
import { watchFieldRefusals } from "@/lib/fieldRefusals";
import { watchPointerMoves } from "@/lib/pointerHover";
import { watchTypingFade } from "@/lib/typingFade";
import { startEachInputFresh } from "@/lib/inputUpdateCount";
import { watchFocusThroughWaits } from "@/lib/focusThroughWaits";
import { watchSwitchTurns } from "@/lib/switchTurns";
import { watchWebKitPressFocus } from "@/lib/webkitPressFocus";
import { followDesktopMenuKeys } from "@/lib/desktopMenuKeys";
import { settleViewportKeys } from "@/lib/iosViewport";
import { watchSoftKeyboard } from "@/lib/softKeyboard";
import { watchKeyboardDownEndsEditing } from "@/lib/keyboardDownEndsEditing";
import { forgetRetiredGenerationValues } from "@/lib/browserStorage";
import { absorbSwitcherAddress } from "@/lib/switcherLists";
import { absorbRemoteSignOutAddress, takeRemoteSignOutRequest } from "@/lib/remoteSignOut";
import { forwardOldPageAddress } from "@/lib/router";
import { markQuickSearchWindow } from "@/lib/quickSearchWindow";
import { applyContrast, followContrastChanges, getContrastPref } from "@/lib/contrast";
import { applyTheme, followThemeChanges, getThemePref } from "@/lib/theme";
import { markScreenKind } from "@/lib/screenKind";
import { fetchRuntimeConfig } from "@/lib/runtimeConfigService";
import App from "./App";
import "./app/typography.css";
import "./app/globals.css";
import "./app/sidePanels.css";
import "./app/darkTip.css";
import "./app/olderInk.css";
import "./app/buttonPress.css";
import "./app/numberFields.css";
import "./app/contrast.css";
import "./app/waitingWrites.css";
import "./app/touchHoverCells.css";
import "./app/iconPalette.css";
import "./app/optionColors.css";
import "./app/reds.css";
import "./app/tabGroupColors.css";
import "./styles.css";
import "./app/print.css";

markQuickSearchWindow();
applyTheme(getThemePref());
followThemeChanges();
applyContrast(getContrastPref());
followContrastChanges();
// The body says from the start whether this is a touch screen and a phone.
markScreenKind();
startLocalBundleFreshnessWatch();
// Letters that come fast all reach their fields.
startEachInputFresh();

const rootElement = document.getElementById("root");

/**
 * The page's own loading screen (index.html) goes once the app first draws
 * in the root, in that frame: told here, not by a "#root:not(:empty) ~
 * #boot-screen" rule, which Chrome kept in the set it sweeps under the
 * parent of every element put in anywhere — each new line of a page
 * (2026-11-14, check-css-sibling-invalidation.mjs).
 */
function hideBootScreenOnceDrawn(root: HTMLElement) {
  const boot = document.getElementById("boot-screen");
  if (!boot) return;
  const drawn = () => {
    if (!root.hasChildNodes()) return false;
    boot.hidden = true;
    return true;
  };
  if (drawn()) return;
  const watch = new MutationObserver(() => { if (drawn()) watch.disconnect(); });
  watch.observe(root, { childList: true });
}

if (!rootElement) {
  throw new Error("Missing root element");
}

keepDroppedFilesInApp();
// React Aria's live announcer puts its box at the start of the body the first
// time something is announced (a list opening on an iPhone), and a box put in
// before the page restyles the whole page (overlayRoot.ts): made now, while
// the page is empty, not at the first list a tab opens (iPhone Safari, 38k
// elements: 277–285 ms in the phone's search and filter sheet, 2026-11-13).
announce("", "polite", 0);
const reactRoot = createRoot(rootElement);
hideBootScreenOnceDrawn(rootElement);
// Whatever is drawn waits for the app's styles (appStyles.ts), in the order
// asked; until then the page's own loading screen shows.
const styles = appStylesReady();
// Every screen says when a keychain call waits on macOS's prompt.
let drawn = false;
const root = { render: (node: ReactNode) => {
  drawn = true;
  void styles.then(() => reactRoot.render(<AppLocale>{node}<KeychainPromptNotice /></AppLocale>));
} };
// Before anything is drawn — the app opening another server waits on its
// sign-in kept there — the loading screen comes up to say it.
followKeychainPrompt(() => {
  if (drawn || !keychainPromptShowing()) return;
  const render = () => { if (!drawn) root.render(<StrictMode><ProductLoadingScreen source="auth" /></StrictMode>); };
  void initI18n("").then(render, render);
});

// The packaged client's page first finds its server, saying so while the
// app's own server starts; the sign-in it keeps is that server's.
function boot() {
  // The server's settings are asked for first, while the page's connections
  // are free: asked as the app first drew, they waited behind its code and
  // fonts for a connection, up to 0.9 s on a NAS's first visit (2026-11-13).
  void fetchRuntimeConfig();
  // Other servers' list keys handed over in the address, before routing.
  absorbSwitcherAddress();
  absorbRemoteSignOutAddress();
  forwardOldPageAddress();
  // A sign-in carried over from the server's earlier address comes first.
  void takeAddressHandoff().then(() => {
    const initialSession = loadSession();
    // Another server's page asking this one to sign out finds nobody signed
    // in here: nothing to end, so it goes straight back (a signed-in page
    // signs out first, useProductSignOut).
    if (!initialSession) void takeRemoteSignOutRequest().then((request) => { if (request) window.location.assign(request.back); });
    const renderApp = () => root.render(
      <StrictMode><App initialSession={initialSession} /></StrictMode>,
    );
    void initI18n(initialSession?.user.id ?? "").then(renderApp, renderApp);
  });
}

let started = false;
function showStarting(updating = false) {
  const render = () => {
    if (!started) root.render(<StrictMode><ProductLoadingScreen deferred source="device" updating={updating} /></StrictMode>);
  };
  void initI18n("").then(render, render);
}

// A new version bringing its data up to date says so, not that it is slow.
function showUpdating() {
  showStarting(true);
}

// Slow to start, the startup recovery screen shows what can be done while
// the server goes on starting; the app opens over it once it answers.
function showSlow() {
  const render = () => {
    if (!started) root.render(<StrictMode><StartupRecoveryScreen problem={null} /></StrictMode>);
  };
  void initI18n("").then(render, render);
}

function startPackaged(fromFirstScreen = false) {
  // An agent may add another Leafen to the app from its conversation, past
  // the first screen (which takes its own while it shows).
  followAgentSignIns();
  void startPackagedClient(showStarting, showSlow, showUpdating).then(() => {
    started = true;
    // Started from the first screen, the server answers: the app's other
    // tabs, which follow the start, go on awake (lib/desktopTabs.ts).
    if (fromFirstScreen) wakeTabsBehind();
    boot();
  }, (error: unknown) => {
    started = true;
    if (error instanceof FirstRunPending) {
      // A first start asks before anything; starting fresh here starts the
      // app's own server, and the page then finds it.
      const render = () => root.render(<StrictMode><FirstInstall onStartHere={() => {
        started = false;
        startPackaged(true);
      }} /></StrictMode>);
      void initI18n("").then(render, render);
      return;
    }
    failed(error);
  });
}

// Each start that fails is shown anew: drawn in the same place, the screen
// kept the last try's "다시 시도하는 중…" and took no press (2026-11-06).
let failedStarts = 0;

function failed(error: unknown) {
  // The app's own server couldn't start: why, and what can be done without it.
  if (error instanceof LocalServerFailedError) {
    const problem = { reason: error.reason, message: error.message, backupAt: error.backupAt };
    const attempt = ++failedStarts;
    const render = () => root.render(<StrictMode><StartupRecoveryScreen key={attempt} problem={problem} onStartAgain={() => {
      started = false;
      startPackaged();
    }} /></StrictMode>);
    void initI18n("").then(render, render);
    return;
  }
  if (error instanceof ServerKeyChangedError) {
    const render = () => root.render(<StrictMode><ServerKeyChangedScreen origin={error.origin} pin={error.pin} /></StrictMode>);
    void initI18n("").then(render, render);
    return;
  }
  root.render(error instanceof Error ? error.message : String(error));
}

startPackaged();

// The offline worker, its idle warm-up and the notice of a new build.
registerServiceWorker(announceUpdateReady);
reloadAfterStartWithoutServer();
// A module a click needs that a newer build no longer has says so.
watchStaleBuild();
watchOverflowTitles();
watchIconTips();
watchFieldRefusals();
watchPointerMoves();
watchTypingFade();
watchFocusThroughWaits();
watchSwitchTurns();
watchWebKitPressFocus();
followDesktopMenuKeys();
settleViewportKeys();
watchSoftKeyboard();
watchKeyboardDownEndsEditing();
// What the generation before Leafen kept here goes.
forgetRetiredGenerationValues();
