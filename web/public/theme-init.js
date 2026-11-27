/* global AbortController, addEventListener, Boolean, clearTimeout, Date, document, fetch, localStorage, location, Math, matchMedia, navigator, Number, performance, sessionStorage, setTimeout, String */

// Every page of the app runs this first, an older build's page too, before
// any of the app's code: what it does can't wait for that code to come.

// The person's light or dark look, before anything is drawn.
(() => {
  try {
    const pref = localStorage.getItem("leafen:theme") || "system";
    const dark =
      pref === "dark" ||
      (pref === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
    const theme = dark ? "dark" : "light";
    document.documentElement.dataset.theme = theme;
    document.documentElement.style.colorScheme = theme;
  } catch {
    document.documentElement.dataset.theme = "light";
  }
})();

// The page's own loading screen (index.html's boot-screen) says so when the
// app doesn't come: a request for its code or styles that failed or stalled
// left the screen with the mark alone and no word (2026-11-25, a NAS page in
// an app's browser; Notion's page keeps its spinner with no word, failed or
// stalled, seen 2026-11-25 for 90 s). Past NOTICE_AFTER_MS with nothing
// drawn, or at once when one of the app's files fails, it says "연결 중…" or,
// the server not answering, "연결할 수 없음", with "다시 시도" — in the
// browser's language, as the app's words aren't here yet. It asks the server
// again by itself, and once it answers after none, the page loads again.
//
// A page a browser kept (a tab Chrome put away and opens again) can be an
// older build's, naming modules this server no longer has: the app never
// starts and the tab stayed blank (2026-11-05). A file of the app's that
// fails while the server answers loads the page once more from it.
(() => {
  // A normal start draws well inside it: a returning person's page in under
  // a second, a first visit to a NAS-class server in 2.9–3.3 s, 4.9 s at
  // most (2026-11-18) — and it is past the 4 s the app gives a silent server
  // before taking it as away (lib/serverAnswers.ts, the offline worker's).
  const NOTICE_AFTER_MS = 6000;
  const ASK_WAIT_MS = 4000;
  // How soon the server is asked again, growing to the most (SignInWaiting.tsx).
  const FIRST_LOOK_MS = 2000;
  const MOST_LOOK_MS = 10000;
  // How long a press shows it is trying, at least.
  const PRESS_SHOWN_MS = 600;
  // A load again by itself goes once in this time (a press goes at once).
  const RELOAD_KEY = "leafen:older-build-reload";
  const RELOAD_ONCE_MS = 30000;
  const WORDS = /^ko\b/i.test(navigator.language || "")
    ? { connecting: "연결 중…", away: "연결할 수 없음", retry: "다시 시도" }
    : { connecting: "Connecting…", away: "Can’t connect", retry: "Try again" };

  let said = "";
  let lookTimer = 0;
  let lookWait = FIRST_LOOK_MS;
  let asking = false;
  let fileFailed = false;
  let pressed = false;
  // Why the page loads again by itself once the server answers: "away", the
  // server gave no answer — kept until it does; "file", a file of the app's
  // failed with the server answering — tried once, so a page that fails
  // again (an older build's, or the server gone wrong) stays, for a press.
  let loadAgain = "";

  const boot = () => document.getElementById("boot-screen");
  // The app drew in its root (main.tsx hides the loading screen as it does).
  const drawn = () => {
    const screen = boot();
    return Boolean(document.getElementById("root")?.hasChildNodes() || screen?.hidden);
  };

  // Any answer of the server's but a proxy's saying it is away, as the app
  // reads its health (lib/serverAnswers.ts): an updating server's too, whose
  // page then says so.
  const serverAnswers = () => {
    const stop = new AbortController();
    const timer = setTimeout(() => stop.abort(), ASK_WAIT_MS);
    return fetch("/api/leafen/v1/health", { cache: "no-store", redirect: "manual", signal: stop.signal })
      .then(async (answer) => {
        if (answer.status < 502 || answer.status > 504) return true;
        if (answer.status !== 503) return false;
        const health = await answer.json().catch(() => null);
        return health?.status === "migrating" || health?.status === "failed";
      }, () => false)
      .finally(() => clearTimeout(timer));
  };

  const reloadOnce = () => {
    try {
      if (Date.now() - Number(sessionStorage.getItem(RELOAD_KEY) || 0) < RELOAD_ONCE_MS) return false;
      sessionStorage.setItem(RELOAD_KEY, String(Date.now()));
    } catch {
      return false;
    }
    location.reload();
    return true;
  };

  const say = (what) => {
    const screen = boot();
    const line = screen?.querySelector("p");
    if (!screen || !line || drawn()) return;
    if (!line.firstChild) {
      const status = document.createElement("span");
      const retry = document.createElement("button");
      retry.type = "button";
      retry.textContent = WORDS.retry;
      retry.addEventListener("click", press);
      line.append(status, retry);
      // Read out and reached once it says something.
      screen.removeAttribute("aria-hidden");
      line.setAttribute("role", "status");
    }
    said = what;
    line.firstChild.textContent = WORDS[what];
    line.lastChild.disabled = pressed;
  };

  const look = async () => {
    clearTimeout(lookTimer);
    if (asking || drawn()) return;
    asking = true;
    const answers = await serverAnswers();
    asking = false;
    if (drawn()) return;
    if (!answers) loadAgain = "away";
    else if (loadAgain && !pressed) {
      if (reloadOnce()) return;
      if (loadAgain === "file") loadAgain = "";
    }
    if (!pressed) say(!answers || fileFailed ? "away" : "connecting");
    lookTimer = setTimeout(look, lookWait);
    lookWait = Math.min(MOST_LOOK_MS, lookWait * 1.5);
  };

  // "다시 시도": the page again once the server answers; else it says so
  // again, having shown it tried for a moment.
  async function press() {
    if (pressed) return;
    pressed = true;
    say("connecting");
    const pressedAt = performance.now();
    const answers = await serverAnswers();
    if (answers) {
      location.reload();
      return;
    }
    setTimeout(() => {
      pressed = false;
      loadAgain = "away";
      lookWait = FIRST_LOOK_MS;
      say("away");
      clearTimeout(lookTimer);
      lookTimer = setTimeout(look, lookWait);
    }, Math.max(0, PRESS_SHOWN_MS - (performance.now() - pressedAt)));
  }

  addEventListener("error", (event) => {
    const element = event.target;
    const source = element && (element.src || element.href);
    if (!source || !/\/assets\//.test(source) || drawn()) return;
    if (!fileFailed && !loadAgain) loadAgain = "file";
    fileFailed = true;
    void look();
  }, true);
  setTimeout(() => {
    if (said || drawn()) return;
    say("connecting");
    void look();
  }, NOTICE_AFTER_MS);
  addEventListener("online", () => { if (said) void look(); });
})();
