import { buildKeptHere } from "./keptBuild";
import { requestsInFlight } from "./requestDeadline";

// What a page fetches ahead of need — a part a press draws, a window kept
// shut, English behind a complete translation, the offline worker and its
// copy of every screen — waits until the page's start has settled: its first
// screen drawn (no loading screen, as desktopTabs.ts tells the app), none of
// its requests out and no code a drawn part waits for still coming, for a
// moment; a window's code that takes typing as it opens, until the first
// screen is drawn. Fetched at the first idle moment instead, which comes
// while the start waits on the network, ten windows' code, English and the
// worker's copies took the six connections a plain http address gives the
// page: on a NAS's first visit the account's reads waited 650 ms for one and
// the page's own reads shared them with the import window's code
// (2026-11-13). Code this device keeps takes no connection and goes at the
// first idle moment still (fetchAhead): kept as the build's whole list is
// (keptBuild.ts), not as any worker is in charge — the first page of a new
// build starts under the worker of the build before, and its windows' code
// fetched at once shared the link with the page's own (2026-11-18). A start
// still busy after ten seconds has settled as far as this goes.
// The start answered (whenStartAnswered) is only that quiet moment, never
// the ten seconds' end: looked for until the tab's first half minute ends.
const QUIET_MS = 300;
const CHECK_MS = 100;
const MOST_MS = 10_000;
const ANSWERED_MOST_MS = 30_000;

let settled = false;
let answered = false;
let drawn = false;
let checking = false;
let quietSince = -1;
let codeComing = 0;
const waiting: Array<() => void> = [];
const waitingDrawn: Array<() => void> = [];
const waitingAhead: Array<() => void> = [];
const waitingSettled: Array<() => void> = [];
const waitingAnswered: Array<() => void> = [];
let askedKept = false;
let keptHere = false;

/** Code a drawn part waits for (lazyScreen.tsx) keeps the start unsettled until it comes. */
export function holdStartFor<T>(load: Promise<T>): Promise<T> {
  codeComing += 1;
  const done = () => { codeComing -= 1; };
  load.then(done, done);
  return load;
}

/** Runs once the page has nothing else to do, or a second after where it can't say. */
export function whenIdle(run: () => void) {
  if (typeof window === "undefined") return;
  if (typeof window.requestIdleCallback === "function") window.requestIdleCallback(run, { timeout: 5_000 });
  else window.setTimeout(run, 1_000);
}

function firstScreenDrawn() {
  const root = document.getElementById("root");
  return !!root && root.childElementCount > 0 && !document.querySelector("[data-loading-screen]");
}

function check() {
  const now = performance.now();
  const shown = firstScreenDrawn();
  if (shown && !drawn) {
    drawn = true;
    for (const run of waitingDrawn.splice(0)) whenIdle(run);
  }
  if (!shown || requestsInFlight() > 0 || codeComing > 0) quietSince = -1;
  else if (quietSince < 0) quietSince = now;
  const quiet = quietSince >= 0 && now - quietSince >= QUIET_MS;
  if (quiet && !answered) {
    answered = true;
    for (const run of waitingAnswered.splice(0)) run();
  }
  if (!settled && (quiet || now >= MOST_MS)) {
    settled = true;
    drawn = true;
    for (const run of waitingSettled.splice(0)) run();
    for (const run of [...waitingDrawn.splice(0), ...waitingAhead.splice(0), ...waiting.splice(0)]) whenIdle(run);
  }
  if (!settled || (!answered && waitingAnswered.length > 0 && now < ANSWERED_MOST_MS)) {
    window.setTimeout(check, CHECK_MS);
    return;
  }
  checking = false;
}

function watch() {
  if (checking) return;
  checking = true;
  check();
}

/**
 * Code fetched ahead of need: at the first idle moment when this device keeps
 * the page's build, and else once the start has settled — or, for
 * `pressable` code (a window that takes typing as it opens), once the first
 * screen is drawn, where a person can first open it: waiting for the start's
 * last reads, a search opened and typed into as the page showed lost its
 * first letters while its code came (2026-11-13).
 */
export function fetchAhead(run: () => void, pressable = false) {
  if (typeof window === "undefined") return;
  if (keptHere || settled || (pressable && drawn)) {
    whenIdle(run);
    return;
  }
  (pressable ? waitingDrawn : waitingAhead).push(run);
  watch();
  if (askedKept) return;
  askedKept = true;
  void buildKeptHere().then((kept) => {
    keptHere = kept;
    if (kept) for (const ahead of [...waitingDrawn.splice(0), ...waitingAhead.splice(0)]) whenIdle(ahead);
  });
}

/** Runs `run` as soon as the page's start has settled. */
export function whenStartSettled(run: () => void) {
  if (typeof window === "undefined") return;
  if (settled) {
    run();
    return;
  }
  waitingSettled.push(run);
  watch();
}

/**
 * Runs `run` once the page's start has had all it asked for: its first
 * screen drawn, nothing of its out and no code it waits for still coming, for
 * a moment — not as the ten seconds end with something still out. Never
 * where the first half minute passes without that moment. A request the
 * server gave no answer to is out no longer either: whether it was answered
 * is the caller's to ask (serverAnswers.ts serverAway).
 */
export function whenStartAnswered(run: () => void) {
  if (typeof window === "undefined") return;
  if (answered) {
    run();
    return;
  }
  if (performance.now() >= ANSWERED_MOST_MS) return;
  waitingAnswered.push(run);
  watch();
}

// Code fetched ahead in turn (fetchAheadInTurn): this many at once, so that
// what the page reads meanwhile still finds connections free.
const IN_TURN_AT_ONCE = 2;
const inTurn: Array<() => Promise<unknown>> = [];
let inTurnGoing = 0;

function takeTurns() {
  while (inTurnGoing < IN_TURN_AT_ONCE && inTurn.length > 0) {
    const load = inTurn.shift()!;
    inTurnGoing += 1;
    whenIdle(() => {
      // One that fails ends the turns: the browser keeps a module that
      // failed to come as failed (seen 2026-11-14, Chrome), so the rest come
      // as they are opened, as before, not all kept failed while the server
      // was away or a newer build took their files.
      void load().catch(() => { inTurn.length = 0; }).finally(() => {
        inTurnGoing -= 1;
        takeTurns();
      });
    });
  }
}

/**
 * Code fetched ahead once the page's start has settled, from the offline
 * worker's copy too, a couple at a time at idle moments: the many windows a
 * press may open (lazyScreen.tsx), each small, none of them as soon as the
 * page shows, nor all at once.
 */
export function fetchAheadInTurn(load: () => Promise<unknown>) {
  if (typeof window === "undefined") return;
  inTurn.push(load);
  afterStart(takeTurns);
}

/** Runs `run` at an idle moment once the page's start has settled. */
export function afterStart(run: () => void) {
  if (typeof window === "undefined") return;
  if (settled) {
    whenIdle(run);
    return;
  }
  waiting.push(run);
  watch();
}
