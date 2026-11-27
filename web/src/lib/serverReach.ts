import { LeafenApiError, LeafenTransportError, REMOTE_ERROR_CODE, statusSaysServerAway } from "@/apiTransport";
import { createDiagnosticReference } from "./diagnosticTrace";
import { offlineShown, onOfflineShownChange } from "./offlinePill";
import { createRetryPacing } from "./retryPacing";
import { answerWaitMs, onServerAnswersAgain, onServerAway, serverAway } from "./serverAnswers";
import { isRefusedForNow } from "./serverBusy";

/**
 * Whether a read failed because the server didn't answer: the request never
 * reached it, a proxy in front of it says it is away, or it says it can't
 * serve now (statusSaysServerAway) — refused for now, near its memory's
 * limit too, as busy (lib/serverBusy.ts; 2026-11-23: that refusal, a 429,
 * read as a failure of its own, an imported document's picture left out for
 * good). The product then shows what it kept, or
 * that it isn't available offline, and reads again later instead of
 * reporting a failure, as Notion does offline (its guides "Use pages
 * offline" and "Work offline with Notion", read 2026-10-04).
 */
export function isServerUnreachable(error: unknown): boolean {
  if (error instanceof LeafenTransportError) return true;
  return error instanceof LeafenApiError
    && (statusSaysServerAway(error.status, error.code !== REMOTE_ERROR_CODE) || isRefusedForNow(error));
}

const RETRY_INTERVAL_MS = 30_000;

/**
 * Calls retry when the server may answer again: the browser comes back
 * online, the window shows again, another request finds it answering, the
 * top bar's "오프라인" goes, or half a minute passes — each time no sooner
 * than the pause after the try before, which waited (retryPacing.ts: a
 * request failing at once by itself, the others answered, was tried again
 * as often as the server seemed to come back). Returns the stop.
 */
export function retryWhenServerMayAnswer(retry: () => void): () => void {
  // Asked for after a try that waited: the first retry goes a pause after it.
  const pacing = createRetryPacing();
  pacing.waited();
  const again = () => pacing.request(() => {
    pacing.waited();
    retry();
  });
  const onVisible = () => {
    if (document.visibilityState === "visible") again();
  };
  window.addEventListener("online", again);
  document.addEventListener("visibilitychange", onVisible);
  const timer = window.setInterval(again, RETRY_INTERVAL_MS);
  const stopAnswers = onServerAnswersAgain(() => again());
  const stopBar = onOfflineShownChange((shown) => { if (!shown) again(); });
  return () => {
    pacing.dispose();
    stopAnswers();
    stopBar();
    window.removeEventListener("online", again);
    document.removeEventListener("visibilitychange", onVisible);
    window.clearInterval(timer);
  };
}

/** What answeredWithin gives when the answer didn't come in time. */
export const NOT_ANSWERED = Symbol("not answered");

/**
 * The answer, or NOT_ANSWERED once ms pass without one — or a few of the
 * server's round trips, where those are longer (serverAnswers.ts
 * answerWaitMs): a network that stopped neither answers nor fails, and a
 * read waiting on it without end kept a picture empty and the settings list
 * hidden however long it took (2026-11-07). The request goes on; a later
 * read takes its answer.
 */
export function answeredWithin<Value>(answer: Promise<Value>, ms: number): Promise<Value | typeof NOT_ANSWERED> {
  let timer = 0;
  const late = new Promise<typeof NOT_ANSWERED>((done) => { timer = window.setTimeout(() => done(NOT_ANSWERED), answerWaitMs(ms)); });
  answer.catch(() => undefined);
  return Promise.race([answer, late]).finally(() => window.clearTimeout(timer));
}

/**
 * A list's reading, failing at once as a request the server didn't answer
 * while the app already knows the connection is lost — a request found the
 * server away, or the top bar says "오프라인" — or as it learns so before the
 * answer: the screen says so in seconds, as the top bar does, rather than
 * staying empty until the request gives up (2026-11-15: "앱" stood empty for
 * 55 s on a stalled network, the top bar saying "오프라인" all along). The
 * request goes on; the screen reads again once the server may answer
 * (retryWhenServerMayAnswer).
 */
export function answeredUnlessAway<Value>(answer: Promise<Value>): Promise<Value> {
  answer.catch(() => undefined);
  const stops: (() => void)[] = [];
  const away = new Promise<never>((_resolve, refuse) => {
    const refuseNow = () => refuse(new LeafenTransportError(createDiagnosticReference(), new Error("The connection is lost.")));
    if (serverAway() || offlineShown()) refuseNow();
    else stops.push(onServerAway(refuseNow), onOfflineShownChange((shown) => { if (shown) refuseNow(); }));
  });
  return Promise.race([answer, away]).finally(() => { for (const stop of stops) stop(); });
}

/**
 * Reads, and while the server doesn't answer (isServerUnreachable) reads
 * again once it may (retryWhenServerMayAnswer), giving the answer to `take`;
 * a refusal ends it, as one read's would. A screen's name for what it shows
 * read once, while the server was out of reach, kept its stand-in until the
 * screen opened again (2026-11-25: a task made in "내 작업" with the server
 * down kept "데이터베이스" for its database in its side peek after the server
 * was back). Returns the stop.
 */
export function readUntilServerAnswers<Value>(read: () => Promise<Value>, take: (value: Value) => void): () => void {
  let stopped = false;
  let stopRetry: (() => void) | null = null;
  let reading = false;
  const go = () => {
    if (stopped || reading) return;
    reading = true;
    read().then((value) => {
      reading = false;
      if (stopped) return;
      stopRetry?.();
      stopRetry = null;
      take(value);
    }, (error: unknown) => {
      reading = false;
      if (!stopped && isServerUnreachable(error)) stopRetry ??= retryWhenServerMayAnswer(go);
    });
  };
  go();
  return () => {
    stopped = true;
    stopRetry?.();
  };
}
