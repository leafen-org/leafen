// A request the server refused before doing any of it — busy
// (request_admission.go), near its memory's limit, or unable to read its
// memory (memory_pressure.go) — goes again after the wait the server names,
// growing, for so long; one still refused then fails as the server's answer
// says, and a failure the screen tells meanwhile is told beside to have met a
// server too busy (2026-11-23: a guest's row sent to the trash on a NAS short
// of memory came back "couldn't move to the trash" after a few seconds,
// nothing saying why, the refusal never asked again). The one rule for every
// way the app asks the server: its JSON requests (apiTransport.ts), files
// read as they come (authenticatedBinaryResponse.ts), an app's calls and the
// files it saves (components/apps/appCalls.ts, appFileSave.ts). Such a
// refusal reads as the server away while it lasts (serverAnswers.ts,
// serverReach.ts isServerUnreachable), as a busy one always did: what waits
// for the server goes once it takes requests again.

/** The server is busy and did none of the request (request_admission.go). */
export const SERVER_BUSY_CODE = "LEAFEN_SERVER_BUSY";
/** The server is near its memory's limit and did none of the request (memory_pressure.go). */
export const MEMORY_PRESSURE_CODE = "LEAFEN_MEMORY_PRESSURE";
/** The server couldn't read its memory and did none of the request (memory_pressure.go). */
export const MEMORY_SENSOR_UNAVAILABLE_CODE = "LEAFEN_MEMORY_SENSOR_UNAVAILABLE";

const REFUSALS_FOR_NOW = new Set([SERVER_BUSY_CODE, MEMORY_PRESSURE_CODE, MEMORY_SENSOR_UNAVAILABLE_CODE]);

/** How long a request the server refused for now is asked again, in ms. */
const ADMISSION_RETRY_MS = 20_000;

/** How long after such a refusal a failure the screen tells is told beside to have met it, in ms. */
const REFUSAL_TOLD_MS = 3_000;

/** Whether an answer is a refusal of the request before any of it ran, to ask again. */
export async function admissionRefusal(response: Response): Promise<boolean> {
  if (response.status !== 503 && response.status !== 429) return false;
  const body = await response.clone().json().catch(() => null) as { error?: { code?: unknown } } | null;
  return typeof body?.error?.code === "string" && REFUSALS_FOR_NOW.has(body.error.code);
}

/** Whether a failure is the server's refusal for now, which did none of the request. */
export function isRefusedForNow(error: unknown): boolean {
  const code = error && typeof error === "object" ? Reflect.get(error, "code") : undefined;
  return typeof code === "string" && REFUSALS_FOR_NOW.has(code);
}

/**
 * How long to wait before asking again a request whose answer this is, in
 * ms: the server's Retry-After, growing with each try, about so long — or
 * null when the answer is no refusal for now, or the asking has gone on long
 * enough (ADMISSION_RETRY_MS since `started`, a Date.now()). Asking stops,
 * a refusal is noted (refusedForNowLately).
 */
export async function refusalWait(response: Response, attempt: number, started: number): Promise<number | null> {
  if (!await admissionRefusal(response)) return null;
  const wait = Math.min(5, (Number(response.headers.get("retry-after")) || 2) * (1 + attempt / 2)) * 1000 * (0.75 + Math.random() / 2);
  if (Date.now() - started + wait <= ADMISSION_RETRY_MS) return wait;
  noteRefusedForNow();
  return null;
}

/** Waits ms, or fails as the signal ends first. */
export function waitUnlessAborted(ms: number, signal?: AbortSignal | null) {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason ?? new DOMException("Aborted", "AbortError"));
      return;
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      reject(signal?.reason ?? new DOMException("Aborted", "AbortError"));
    };
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

/**
 * Sends a request the app makes with its own fetch, asked again while the
 * server refuses it for now (refusalWait); the last answer, whatever it is.
 * `send` makes the request anew each time, its body too.
 */
export async function fetchAskingAgain(send: () => Promise<Response>, signal?: AbortSignal | null): Promise<Response> {
  const started = Date.now();
  for (let attempt = 0; ; attempt += 1) {
    const response = await send();
    const wait = await refusalWait(response, attempt, started);
    if (wait === null) return response;
    await waitUnlessAborted(wait, signal);
  }
}

let refusedAt = Number.NEGATIVE_INFINITY;

/** Notes a request the server still refused for now once it was asked again long enough. */
export function noteRefusedForNow() {
  refusedAt = performance.now();
}

/** Whether a failure told now likely met the server refusing for now: a request was, a moment ago. */
export function refusedForNowLately(): boolean {
  return performance.now() - refusedAt < REFUSAL_TOLD_MS;
}
