import type { WebsocketProvider } from "y-websocket";
import { noteConnectionQuietSince } from "./connectionQuiet";
import { answerWaitMs, noteAnswerTime } from "./serverAnswers";

// A connection is kept by asking it, not by the clock. y-websocket starts one
// over once it has heard nothing for 30 seconds, which a page out of sight
// can't keep: Chrome wakes its timers only every minute or so, and every
// hidden tab's connection closed and opened again every one to two minutes,
// each time reading the page's status, comments and blocks again
// (2026-11-13). That check is off. The one below asks a connection quiet for
// a while how it is (Hocuspocus Ping, which the server answers with Pong),
// and starts over only one that hasn't answered when it looks again, however
// long its timers slept meanwhile. Out of sight it asks after this long — the
// Pong keeps the room of someone alone on a page, who hears nothing else.
const COLLABORATION_QUIET_MS = 15_000;
// While the page shows, a connection silent for a few seconds is asked, and
// one that says nothing a few seconds more is started again (2026-11-05: a
// network that stopped without failing kept the connection open and silent,
// and the connection notice came some 45 seconds after the stop, not the 10
// the decision gives).
const LIVENESS_CHECK_MS = 1_000;
const LIVENESS_ASK_AFTER_MS = 3_000;
const LIVENESS_ANSWER_WAIT_MS = 4_000;
// A connection that doesn't open within this wait is started again
// (2026-10-17: y-websocket checks a silent connection only once it opened,
// so one held open by a proxy stayed "connecting" for good).
const CONNECT_WAIT_MS = 20_000;
// A far server answers the same, later: its answer is waited for this many
// of its round trips if that is longer (serverAnswers.ts answerWaitMs). Its
// Pong takes one round trip, and a connection opens in two to four (its
// handshake and upgrade, behind another socket opening to the same server):
// 2026-11-18, a server 2 and 5 seconds away, its room opened in 8 and 20
// seconds, the latter at the very end of the wait for it, and a Pong from 5
// seconds away comes after the wait for it.
const ANSWER_ROUND_TRIPS = 3;
const CONNECT_ROUND_TRIPS = 8;
const MESSAGE_PING = 9;
const MESSAGE_PONG = 10;

/**
 * Keeps a page's room connection by asking it, as above, until the returned
 * stop or `stopped` says so, and starts again one that doesn't open in time.
 * One started again was in trouble since it was last heard, or since it
 * began opening (connectionQuiet.ts).
 */
export function keepRoomConnectionAsked(provider: WebsocketProvider, stopped: () => boolean): () => void {
  let askedAt = 0;
  // The Pings out on this connection, by when each went: the server answers
  // them in turn, and a Pong says how far it is, nothing else. A connection
  // heard from asks again before an earlier Pong came, from far away.
  let pings: number[] = [];
  provider.messageHandlers[MESSAGE_PONG] = () => {
    const sent = pings.shift();
    if (sent !== undefined) noteAnswerTime(Date.now() - sent);
  };
  // Its check by the clock gives way to the one below that asks.
  window.clearInterval(provider._checkInterval);
  const timer = window.setInterval(() => {
    const socket = provider.ws;
    if (stopped() || !provider.wsconnected || !socket) {
      askedAt = 0;
      return;
    }
    const now = Date.now();
    const heard = provider.wsLastMessageReceived;
    if (heard >= askedAt) askedAt = 0;
    if (askedAt && now - askedAt > answerWaitMs(LIVENESS_ANSWER_WAIT_MS, ANSWER_ROUND_TRIPS)) {
      askedAt = 0;
      noteConnectionQuietSince(heard);
      provider.disconnect();
      provider.connect();
      return;
    }
    const quiet = window.document.visibilityState === "visible" ? LIVENESS_ASK_AFTER_MS : COLLABORATION_QUIET_MS;
    if (!askedAt && now - heard > quiet) {
      askedAt = now;
      pings.push(now);
      socket.send(new Uint8Array([MESSAGE_PING]));
    }
  }, LIVENESS_CHECK_MS);
  let opening = 0;
  const onStatus = ({ status }: { status: "connected" | "disconnected" | "connecting" }) => {
    window.clearTimeout(opening);
    pings = [];
    if (status !== "connecting" || stopped()) return;
    const began = Date.now();
    opening = window.setTimeout(() => {
      if (stopped() || !provider.wsconnecting) return;
      noteConnectionQuietSince(began);
      provider.disconnect();
      provider.connect();
    }, answerWaitMs(CONNECT_WAIT_MS, CONNECT_ROUND_TRIPS));
  };
  provider.on("status", onStatus);
  return () => {
    window.clearInterval(timer);
    window.clearTimeout(opening);
    provider.off("status", onStatus);
  };
}
