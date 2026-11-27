import { reloadKeepingPlace } from "@/lib/reloadKeepingPlace";
import { onServerAnswersAgain, serverAnswersNow, serverAway } from "./serverAnswers";
import { whenStartAnswered } from "./startSettled";
import { requestsInFlight } from "./requestDeadline";
import { newestWorkerInCharge } from "./workerTakeOver";

// What a tab can't mend in place (a new build went up, or it started while
// the server didn't answer and read only part of what it shows) it mends by
// reloading by itself, once that takes nothing from the person: nothing they
// have been at for a while (typing, clicking, scrolling or moving the
// pointer: one reading a page is at it too), no window, menu or field of
// theirs open, nothing being sent or played, and nothing that asks before
// leaving. Text on a page is kept as it is typed, so an editor in hand is no
// reason to wait. Tabs go spread over a minute, not all at once, and a tab
// that keeps coming back here stops, so a server that stays away can't keep
// it reloading.

const QUIET_MS = 10_000;
const CHECK_MS = 5_000;
const SPREAD_MS = 60_000;
const COUNT_KEY = "leafen:rest-reloads";
const MOST_RELOADS = 3;
const COUNT_WINDOW_MS = 10 * 60_000;

let lastActivity = Date.now();
let shownToo = false;
let timer: number | undefined;

function atRest() {
  if (Date.now() - lastActivity < QUIET_MS || requestsInFlight() > 0) return false;
  // One drawn ahead and kept hidden (the page's "…" menu, TopBarPageMenu: React hides it by its style) is no open one.
  if ([...document.querySelectorAll('[role="dialog"],[role="alertdialog"],[role="menu"],[role="listbox"]')]
    .some((open) => !open.closest('[style*="display: none"]'))) return false;
  const active = document.activeElement;
  // A page's text, its title and a database row page's title keep what is
  // typed (the row title saves as the typing pauses and as the tab hides):
  // a caret left in one is no reason to wait (2026-11-07: every row page, a
  // new one's title holding the caret, stayed on the old build for good).
  if (active?.matches('input,textarea,select,[contenteditable="true"]')
    && !active.closest("[data-editor-page],[data-page-title-editor],[data-row-page-title]")) return false;
  if ([...document.querySelectorAll<HTMLMediaElement>("audio,video")].some((media) => !media.paused)) return false;
  // Whatever would ask before the page goes has something unsaved.
  const leaving = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(leaving);
  return !leaving.defaultPrevented;
}

// Why each reload at rest was asked for (2026-11-14: three a minute apart
// left only their times, and their cause had to be found in the server's log).
type RestReload = { at: number; why: string[] };
const asked = new Set<string>();

function mayReloadAgain() {
  try {
    const now = Date.now();
    const kept = JSON.parse(sessionStorage.getItem(COUNT_KEY) ?? "[]") as Array<number | RestReload>;
    const recent = kept.map((entry) => (typeof entry === "number" ? { at: entry, why: [] } : entry))
      .filter(({ at }) => now - at < COUNT_WINDOW_MS);
    if (recent.length >= MOST_RELOADS) return false;
    sessionStorage.setItem(COUNT_KEY, JSON.stringify([...recent, { at: now, why: [...asked] }]));
    return true;
  } catch {
    return false;
  }
}

// Whether the server's worker is still to be asked for since a reload was asked.
let askWorker = false;
function takeAskWorker() {
  const ask = askWorker;
  askWorker = false;
  return ask;
}

function noteActivity() {
  lastActivity = Date.now();
}

/**
 * Reloads this tab when it is at rest: only behind other windows, unless
 * `shown` lets it where it shows too. `why` is kept with the reload's time.
 */
export function reloadWhenAtRest({ shown, why }: { shown: boolean; why: string }) {
  shownToo ||= shown;
  asked.add(why);
  askWorker = true;
  if (timer !== undefined) return;
  for (const event of ["keydown", "pointerdown", "pointermove", "wheel", "scroll", "input"]) {
    window.addEventListener(event, noteActivity, { capture: true, passive: true });
  }
  const check = async () => {
    // A tab that reloads while the server is away may find it can't start: a
    // new build's modules come from the server until they are kept
    // (2026-11-05: a tab behind others went to the error screen in an outage).
    // It lands on the server's build only once that build's offline worker
    // is in charge: made while the worker before still was, as a new one
    // waits for that one's quiet moment, a reload the server was slow to
    // answer came back on the build before from that worker's copy, and
    // reloaded again for the new one some 20 seconds after (2026-11-14). The
    // server's worker is asked for first; one on its way is waited for.
    if ((shownToo || document.visibilityState === "hidden") && atRest() && await serverAnswersNow()
      && await newestWorkerInCharge(takeAskWorker())) {
      if (mayReloadAgain()) reloadKeepingPlace();
      return;
    }
    timer = window.setTimeout(() => void check(), CHECK_MS);
  };
  timer = window.setTimeout(() => void check(), Math.random() * SPREAD_MS);
}

// As long as a tab takes to read what it starts with.
const STARTING_MS = 30_000;

/**
 * A tab whose first reads the server didn't answer reads them all once it
 * does: what the first screen reads once (its path, its sharing, the backup
 * band) is read again nowhere else. One whose start had every answer it asked
 * for (startSettled.ts whenStartAnswered, the server not away then) has
 * nothing to mend: what goes on after its start mends itself as the server
 * answers again, as a tab's that lost it later does, and its reload only
 * took the place and the drawn page from the person (2026-11-19: a server
 * lost 7 s after a table page showed reloaded the tab as it came back).
 */
export function reloadAfterStartWithoutServer() {
  let startAnswered = false;
  whenStartAnswered(() => { startAnswered = !serverAway(); });
  onServerAnswersAgain((awaySince) => {
    if (!startAnswered && awaySince < STARTING_MS) reloadWhenAtRest({ shown: true, why: "start-without-server" });
  });
}
