import { serverChosenForPage, serverPath } from "./serverOrigin";

// Whether the server answers, as the app's own requests find it: a request it
// gave no answer to (or refused for now, a limit to wait out) and the next one
// it did answer are the two ends of a time it was away. This alone says the
// app is offline: the browser's own word on its network (navigator.onLine,
// "offline") is no answer from a server on this computer or its network,
// which goes on answering a browser that says it has none (2026-11-14: with
// the network off, the top bar said "오프라인 — 서버에 연결되어 있지 않습니다"
// while every edit was saved). The browser's word only has the server asked.

let awaySince: number | null = null;
const listeners = new Set<(awaySince: number) => void>();
const awayListeners = new Set<() => void>();

export function noteServerAway() {
  if (awaySince !== null) return;
  awaySince = performance.now();
  for (const listener of [...awayListeners]) listener();
}

export function noteServerAnswered() {
  if (awaySince === null) return;
  const since = awaySince;
  awaySince = null;
  for (const listener of [...listeners]) listener(since);
}

/** Whether the app's requests last found the server away, until one is answered again. */
export function serverAway(): boolean {
  return awaySince !== null;
}

/** Hears the server answer again, with when it stopped, as `performance.now()` read then. */
export function onServerAnswersAgain(listener: (awaySince: number) => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Hears the server stop answering. */
export function onServerAway(listener: () => void) {
  awayListeners.add(listener);
  return () => {
    awayListeners.delete(listener);
  };
}

// How far the server is, as its answers find it: how long the last few took
// to start coming, the quickest of them its round trip (a slower one waited
// for the server's work or a free connection besides). What takes a silent
// server for gone waits a few of its round trips instead where those are
// longer (answerWaitMs): a server 2–5 seconds away answers as a near one
// does, only later (2026-11-18: a page's room was given up while it came,
// and the top bar said "오프라인" while every request was answered). One that
// took longer than any round trip waited out a network that stopped, or the
// server's work: the requests a stop held all answer as it ends, and as
// round trips they would keep the next stop unseen for a minute (2026-11-18:
// 17–29 s answers as a 30 s stop ended).
const ANSWER_SAMPLES = 16;
const LONGEST_ROUND_TRIP_MS = 15_000;
const answerTimes: number[] = [];

/** Notes how long one of the server's answers took to start coming, in ms. */
export function noteAnswerTime(ms: number) {
  if (!(ms >= 0) || ms >= LONGEST_ROUND_TRIP_MS) return;
  answerTimes.push(ms);
  if (answerTimes.length > ANSWER_SAMPLES) answerTimes.shift();
}

/** The server's round trip as its last few answers found it, in ms; 0 before any. */
export function serverRoundTripMs(): number {
  return answerTimes.length ? Math.min(...answerTimes) : 0;
}

/** How long to wait for the server's answer before taking it as none: `least`, or `roundTrips` of its round trips where longer. */
export function answerWaitMs(least: number, roundTrips = 3): number {
  return Math.max(least, roundTrips * serverRoundTripMs());
}

const HEALTH_PATH = "/api/leafen/v1/health";
const ASK_WAIT_MS = 4_000;

/**
 * Gives up a question to the server that has no answer within a few seconds,
 * or a few of its round trips where longer (answerWaitMs): what waits on one
 * with no end — the app's start before its sign-in screen — waited as long as
 * the system holds a connection, with a blank window (2026-11-23).
 */
export function serverAskSignal(): AbortSignal {
  return AbortSignal.timeout(answerWaitMs(ASK_WAIT_MS));
}

/**
 * How the server answers now: "answers"; "updating" while its start brings
 * its data up to date and "update-failed" once that didn't finish — what its
 * health says then (internal/app/schema_migration_answer.go: "migrating",
 * "failed"); "away" with no answer, or a proxy saying it is away.
 */
export type ServerHealth = "answers" | "updating" | "update-failed" | "away";

/**
 * How the server answers now (ServerHealth), asked at its address — the app
 * window's server, not the page's own (2026-11-23: the sign-in screen asked
 * the app's own page, which always answers, and took a server that was off
 * for back) —, within a few seconds or a few of its round trips where longer
 * (answerWaitMs: a server 5 s away can't answer in four, and would be taken
 * for gone): a forward from an address the server left is an answer too (a
 * page's own server may not let it read where that goes; an app window's
 * may).
 */
export async function serverHealthNow(): Promise<ServerHealth> {
  try {
    const answer = await fetch(serverPath(HEALTH_PATH), { cache: "no-store", redirect: serverChosenForPage() ? "follow" : "manual", signal: serverAskSignal() });
    if (answer.status < 502 || answer.status > 504) return "answers";
    if (answer.status !== 503) return "away";
    const said = await answer.json().catch(() => null) as { status?: unknown } | null;
    return said?.status === "migrating" ? "updating" : said?.status === "failed" ? "update-failed" : "away";
  } catch {
    return "away";
  }
}

/** Whether the server answers now (serverHealthNow). */
export async function serverAnswersNow(): Promise<boolean> {
  return await serverHealthNow() === "answers";
}

// As the browser says its network changed, the server is asked, and what it
// says is noted as any request's answer is.
let asking: Promise<void> | null = null;
function askServer() {
  asking ??= serverAnswersNow()
    .then((answers) => { if (answers) noteServerAnswered(); else noteServerAway(); })
    .finally(() => { asking = null; });
}

if (typeof window !== "undefined") {
  window.addEventListener("offline", askServer);
  window.addEventListener("online", askServer);
}

/**
 * What a change kept here for the server says: saved offline where the
 * browser has no network and the server doesn't answer, else that it goes
 * once the server answers again — the browser's word alone, with the server
 * answering, keeps nothing back.
 */
export function keptChangeStatus(): "syncStatusBadge:status.offlineChangesSaved" | "syncStatusBadge:status.retryAfterReconnect" {
  return serverAway() && typeof navigator !== "undefined" && !navigator.onLine
    ? "syncStatusBadge:status.offlineChangesSaved"
    : "syncStatusBadge:status.retryAfterReconnect";
}
