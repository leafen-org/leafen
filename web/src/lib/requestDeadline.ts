// A request waits for the server only so long (2026-10-17: a stalled
// connection left a screen or a save waiting with no end and no reason). The
// answer must start within RESPONSE_WAIT_MS, more for a body being sent —
// never past the server's own five minutes for a request — and its body may
// pause at most BODY_STALL_MS between pieces. Past either, the request fails
// as an unanswered one does, which says the change may or may not have
// landed and to check before trying again.

import { noteAnswerTime } from "./serverAnswers";

const RESPONSE_WAIT_MS = 45_000;
const BODY_STALL_MS = 30_000;
const UPLOAD_BYTES_PER_SECOND = 64 * 1024;
const SERVER_REQUEST_LIMIT_MS = 330_000;

function sentBytes(body: BodyInit | null | undefined): number {
  if (!body) return 0;
  if (typeof body === "string") return body.length;
  if (body instanceof Blob) return body.size;
  if (body instanceof ArrayBuffer) return body.byteLength;
  if (ArrayBuffer.isView(body)) return body.byteLength;
  if (body instanceof URLSearchParams) return body.toString().length;
  if (body instanceof FormData) {
    let total = 0;
    for (const [, value] of body.entries()) total += typeof value === "string" ? value.length : value.size;
    return total;
  }
  // A stream's length isn't known: it gets the server's whole limit.
  return Number.POSITIVE_INFINITY;
}

/** How long a request waits for its answer to start. */
export function responseWaitMs(body: BodyInit | null | undefined) {
  return Math.min(SERVER_REQUEST_LIMIT_MS, RESPONSE_WAIT_MS + (sentBytes(body) / UPLOAD_BYTES_PER_SECOND) * 1000);
}

/** A wait that ran out, as the request's cause. */
export function waitExceeded() {
  return new DOMException("The server didn't answer in time.", "TimeoutError");
}

/**
 * Fetches, failing with a TimeoutError when the answer doesn't start within
 * its wait; the answer's body then fails the same way when it pauses longer
 * than the stall limit. A caller's own signal still cancels it.
 */
// Requests out now, answered or not yet: a control its request locked waits
// while any is (focusThroughWaits.ts). A request that asks the server to hold
// its answer until something changes — a workspace's change probe — is always
// out and waits on no one.
let inFlight = 0;

const addressOf = (input: RequestInfo | URL) => (typeof input === "string" ? input : input instanceof URL ? input.href : input.url);

// A request of the server's API that goes to it and back whatever the
// browser keeps: a write, or a read past its cache. One the cache answered,
// or held behind the same read already out, came back sooner than the server
// could (2026-11-18: 7 ms and 1 s answers from a server 5 s away).
function asksServerItself(input: RequestInfo | URL, init: RequestInit) {
  if (!/\/api\/leafen\//u.test(addressOf(input))) return false;
  const request = typeof Request !== "undefined" && input instanceof Request ? input : null;
  const method = (init.method ?? request?.method ?? "GET").toUpperCase();
  const cache = init.cache ?? request?.cache ?? "default";
  return (method !== "GET" && method !== "HEAD") || cache === "no-store" || cache === "reload" || cache === "no-cache";
}

function holdsAnswer(input: RequestInfo | URL) {
  return /\/change-probe(?:[?#]|$)/u.test(addressOf(input));
}

/** How many of the page's requests wait for an answer now. */
export function requestsInFlight() {
  return inFlight;
}

/**
 * Counts one more request out until the returned release: a read that went
 * in a batch waits for its own answer after the batch's began (readBatch.ts).
 */
export function holdInFlight(): () => void {
  inFlight += 1;
  let held = true;
  return () => {
    if (held) inFlight -= 1;
    held = false;
  };
}

export async function fetchWithDeadline(
  fetcher: typeof globalThis.fetch,
  input: RequestInfo | URL,
  init: RequestInit = {},
  waitMs = responseWaitMs(init.body),
): Promise<Response> {
  // A caller's own signal — given, or a Request's — still cancels it.
  const callerSignal = init.signal ?? (typeof Request !== "undefined" && input instanceof Request ? input.signal : undefined);
  const deadline = new AbortController();
  const forward = () => deadline.abort(callerSignal?.reason);
  if (callerSignal?.aborted) forward();
  else callerSignal?.addEventListener("abort", forward, { once: true });
  let exceeded = false;
  const timer = setTimeout(() => { exceeded = true; deadline.abort(waitExceeded()); }, waitMs);
  const counted = !holdsAnswer(input);
  if (counted) inFlight += 1;
  const askedAt = performance.now();
  try {
    const response = await fetcher(input, { ...init, signal: deadline.signal });
    // How far the server is, as its API's answers find it (serverAnswers.ts):
    // timed here, as they go and come, not behind the app's own queues.
    if (counted && asksServerItself(input, init)) noteAnswerTime(performance.now() - askedAt);
    return stallBoundedResponse(response);
  } catch (cause) {
    if (exceeded && !callerSignal?.aborted) throw waitExceeded();
    throw cause;
  } finally {
    if (counted) inFlight -= 1;
    clearTimeout(timer);
    callerSignal?.removeEventListener("abort", forward);
  }
}

const BODY_MEMBERS = new Set<PropertyKey>(["body", "bodyUsed", "text", "json", "blob", "arrayBuffer", "formData", "bytes", "clone"]);

// Statuses whose answer can't carry a body: a Response made with one throws,
// so they stay as they came (2026-10-19: every 204 — signing out, a password
// reset, ending a session — read as a request the server never answered).
const NULL_BODY_STATUSES = new Set([101, 103, 204, 205, 304]);

/** The response, its body read under the stall limit; its address, status and headers as they were. */
function stallBoundedResponse(response: Response): Response {
  if (!response.body || typeof response.body.getReader !== "function" || NULL_BODY_STATUSES.has(response.status)) return response;
  const bounded = new Response(stallBoundedBody(response.body), {
    status: response.status, statusText: response.statusText, headers: response.headers,
  });
  return new Proxy(response, {
    get(target, property) {
      const source = BODY_MEMBERS.has(property) ? bounded : target;
      const value: unknown = Reflect.get(source, property, source);
      return typeof value === "function" ? (value as (...args: unknown[]) => unknown).bind(source) : value;
    },
  });
}

let installed = false;

/**
 * Whether a request is a call to the app's own shell (the Wails runtime at
 * the page's own /wails/runtime): the app's process answers it, not a
 * server, and some calls wait for the person — a sign-in in the browser, a
 * file dialog — as long as they take (2026-10-21: a browser sign-in longer
 * than the server wait failed).
 */
function callsAppShell(input: RequestInfo | URL): boolean {
  if (typeof location === "undefined") return false;
  try {
    const address = new URL(typeof input === "string" || input instanceof URL ? input : input.url, location.href);
    return address.origin === location.origin && address.pathname === "/wails/runtime";
  } catch {
    return false;
  }
}

/**
 * Bounds every request the page makes — the app's own, its downloads, the
 * libraries' — at one place: the page's fetch (2026-10-17), but for calls
 * to the app's own shell. Installed once, as the app starts.
 */
export function installFetchDeadline() {
  if (installed || typeof globalThis.fetch !== "function") return;
  installed = true;
  const unbounded = globalThis.fetch.bind(globalThis);
  globalThis.fetch = (input: RequestInfo | URL, init?: RequestInit) =>
    callsAppShell(input) ? unbounded(input, init) : fetchWithDeadline(unbounded, input, init);
}

/** A body that fails when the next piece takes longer than the stall limit. */
function stallBoundedBody(body: ReadableStream<Uint8Array>, stallMs = BODY_STALL_MS): ReadableStream<Uint8Array> {
  const reader = body.getReader();
  return new ReadableStream<Uint8Array>({
    async pull(controller) {
      let timer: ReturnType<typeof setTimeout> | undefined;
      const stalled = new Promise<never>((_, reject) => { timer = setTimeout(() => reject(waitExceeded()), stallMs); });
      try {
        const { done, value } = await Promise.race([reader.read(), stalled]);
        if (done) controller.close();
        else controller.enqueue(value);
      } catch (error) {
        reader.cancel(error).catch(() => undefined);
        controller.error(error);
      } finally {
        clearTimeout(timer);
      }
    },
    cancel(reason) {
      return reader.cancel(reason);
    },
  });
}
