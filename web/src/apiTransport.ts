import { createAuthenticatedBinaryResponseRequest } from "./authenticatedBinaryResponse";
import {
  createDiagnosticReference,
  messageWithDiagnosticReference,
  safeDiagnosticReference,
} from "./lib/diagnosticTrace";
import {
  captureErrorFactoryCaller,
  logProductError,
} from "./lib/safeErrorLog";
import { renewSessionToken } from "./lib/authSessionStore";
import { dataEpochFor, reportServerRestored, serverWasRestored } from "./lib/dataEpoch";
import { pageLeaving } from "./lib/pageLeaving";
import { noteServerAnswered, noteServerAway } from "./lib/serverAnswers";
import { noteAnswerAfterUncertainWrite, noteUncertainWrite } from "./lib/uncertainWrites";
import { reportSignedOutResponse } from "./lib/signedOutResponse";
import { noteServerMovedTo, serverChosenForPage, serverPath } from "./lib/serverOrigin";
import { askWhetherServerMoved, forwardedTo } from "./lib/serverMoveCheck";
import { reloadWhenAtRest } from "./lib/reloadAtRest";
import { holdOwnWrite } from "./lib/ownWritesInFlight";
import { afterDataSourceMade, afterPageMade } from "./lib/pagesNotYetMade";
import { pagesOfRequest } from "./lib/requestPages";
import { endSharedReads, readSharedInTask } from "./lib/sameTaskReads";
import { readTogether } from "./lib/readBatch";
import { admissionRefusal, fetchAskingAgain, refusalWait, waitUnlessAborted } from "./lib/serverBusy";
import type { Session } from "./types";
import { workspaceMembershipErrorCodes } from "./workspaceMembershipWire.generated";

export const API_ROOT = "/api/leafen/v1";

export { SERVER_BUSY_CODE } from "./lib/serverBusy";
/** The code of a failed answer that names none: not the server's own, as a proxy's. */
export const REMOTE_ERROR_CODE = "LEAFEN_REMOTE_ERROR";

/**
 * Whether a 502, 503 or 504 says the server is away: a proxy in front of it
 * says so with any of them, and the server's own 503 says it can't serve now
 * (busy, paused). The server's own 502 or 504, which carries its error code,
 * is its answer that something past it didn't answer — a link's site,
 * Notion, an app — and not that it is away (2026-11-14: a picture whose
 * link's site didn't answer read as the server away at each start, and the
 * page reloaded itself a minute later, again and again).
 */
export function statusSaysServerAway(status: number, serverCode: boolean) {
  return status === 503 || ((status === 502 || status === 504) && !serverCode);
}

// The data source a request is about, as the routes name it in their path:
// its own (its ID property, a property's access), a page's database's
// (settings, deleted properties, moves, sprints, layouts) or an
// automation's target; an ID there that is no data source changed here is
// a block's, and goes at once.
const DATA_SOURCE_IN_PATH = /\/(?:data-sources|databases|automation-targets)\/([^/?#]+)/u;

function dataSourceOfRequest(path: string): string | null {
  const named = DATA_SOURCE_IN_PATH.exec(path)?.[1];
  if (!named) return null;
  try {
    return decodeURIComponent(named);
  } catch {
    return null;
  }
}

// A read over the request limit goes again after a wait, a few times, rather
// than leave a part of the screen without it.
const LIMITED_READ_RETRIES = 3;

/** A response body as JSON: none when it is empty or isn't JSON. */
function responseBodyJSON(text: string): unknown {
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return null;
  }
}

// A write that gets no answer goes unanswered for good on a network that
// stopped without failing (2026-11-06: a database automation's "활성화"
// stayed locked for minutes with nothing said; settings writes ended only
// when the server dropped the silent connection). After this long it is
// given up as one whose outcome isn't known: its screen says so, and reads
// again once the server answers (lib/uncertainWrites.ts). A long body gets
// the time to send it besides; an upload or a page leaving has its own way.
const WRITE_ANSWER_LIMIT_MS = 30_000;
const WRITE_BODY_BYTES_PER_SECOND = 10_000;

function writeDeadline(method: string, init: RequestInit) {
  if (method === "GET" || method === "HEAD" || method === "OPTIONS") return null;
  if (init.body != null && typeof init.body !== "string") return null;
  const limit = WRITE_ANSWER_LIMIT_MS + ((init.body as string | undefined)?.length ?? 0) / WRITE_BODY_BYTES_PER_SECOND * 1000;
  const controller = new AbortController();
  const outer = init.signal;
  const follow = () => controller.abort(outer?.reason);
  if (outer?.aborted) follow();
  else outer?.addEventListener("abort", follow, { once: true });
  const timer = setTimeout(() => controller.abort(new DOMException("The server didn't answer in time.", "TimeoutError")), limit);
  return {
    signal: controller.signal,
    clear: () => {
      clearTimeout(timer);
      outer?.removeEventListener("abort", follow);
    },
  };
}

export class LeafenApiError extends Error {
  /** How long the server asked to wait before trying again, in seconds (its Retry-After). */
  retryAfter?: number;
  /** For a refusal from what another server answered (a backup place): that answer, and its common cause. */
  reason?: string;
  causeKey?: string;
  /** The ID of the thing a refusal names (the server's error target), when it names one. */
  targetId?: string;
  /** The request's fields a refusal names (the server's error fields), by name, when it names any. */
  fields?: string[];

  constructor(
    readonly code: string,
    readonly status: number,
    readonly reference: string,
    message: string,
    cause?: unknown,
  ) {
    super(messageWithDiagnosticReference(message, reference), { cause });
    this.name = "LeafenApiError";
    captureErrorFactoryCaller(this, LeafenApiError);
  }
}

export class LeafenTransportError extends Error {
  readonly code = "LEAFEN_TRANSPORT_UNAVAILABLE" as const;
  readonly status: undefined = undefined;

  constructor(readonly reference: string, cause: unknown) {
    super(messageWithDiagnosticReference("Leafen server response unavailable.", reference), { cause });
    this.name = "LeafenTransportError";
    captureErrorFactoryCaller(this, LeafenTransportError);
  }
}

type ErrorEnvelope = {
  error?: {
    code?: unknown;
    reference?: unknown;
    reason?: unknown;
    cause?: unknown;
    target?: { id?: unknown };
    fields?: unknown;
  };
};

type JsonApiTransportOptions = {
  apiRoot?: string;
  createReference?: typeof createDiagnosticReference;
  fetch?: typeof globalThis.fetch;
  log?: typeof logProductError;
  /** Reads asked in one task go as one request (lib/readBatch.ts); by default with the page's own fetch. */
  batchReads?: boolean;
};

export function createJsonApiTransport(options: JsonApiTransportOptions = {}) {
  const apiRoot = options.apiRoot ?? API_ROOT;
  const createReference = options.createReference ?? createDiagnosticReference;
  // The page's fetch is read when a request goes, so the bounds main.tsx
  // installs on it (lib/requestDeadline.ts) hold for every request.
  const fetchResponse = options.fetch ?? ((input: RequestInfo | URL, init?: RequestInit) => globalThis.fetch(input, init));
  const log = options.log ?? logProductError;
  const batchReads = options.batchReads ?? options.fetch === undefined;

  function throwLeafenApiError(
    code: string,
    status: number,
    message: string,
    errorOptions: {
      cause?: unknown;
      causeCode?: string;
      reference?: unknown;
      stage: string;
      retryAfter?: number;
      reason?: unknown;
      causeKey?: unknown;
      targetId?: unknown;
      fields?: unknown;
    },
  ): never {
    const reference = errorOptions.reference === undefined
      ? createReference()
      : safeDiagnosticReference(errorOptions.reference);
    const error = new LeafenApiError(
      code,
      status,
      reference,
      message,
      errorOptions.cause,
    );
    if (errorOptions.retryAfter !== undefined) error.retryAfter = errorOptions.retryAfter;
    if (typeof errorOptions.reason === "string") error.reason = errorOptions.reason;
    if (typeof errorOptions.causeKey === "string") error.causeKey = errorOptions.causeKey;
    if (typeof errorOptions.targetId === "string") error.targetId = errorOptions.targetId;
    if (Array.isArray(errorOptions.fields)) {
      error.fields = errorOptions.fields.flatMap((field: { name?: unknown } | null) => typeof field?.name === "string" ? [field.name] : []);
    }
    captureErrorFactoryCaller(error, throwLeafenApiError);
    log(status >= 500 ? "error" : "warn", "api.request", error, {
      diagnosticReference: reference,
      status,
      errorCode: code,
      causeCode: errorOptions.causeCode ?? code,
      causeStage: errorOptions.stage,
    });
    throw error;
  }

  function throwResponseError(body: ErrorEnvelope | null, status: number, retryAfter?: number): never {
    const code = typeof body?.error?.code === "string"
      ? body.error.code
      : REMOTE_ERROR_CODE;
    return throwLeafenApiError(code, status, `Leafen request failed (${status}).`, {
      causeCode: code,
      reference: body?.error?.reference,
      stage: "response",
      reason: body?.error?.reason,
      causeKey: body?.error?.cause,
      targetId: body?.error?.target?.id,
      fields: body?.error?.fields,
      ...(retryAfter !== undefined ? { retryAfter } : {}),
    });
  }

  // The answer as JSON. The same plain read asked again within this task
  // takes the first one's answer as its own (lib/sameTaskReads.ts), and
  // with `shareWhileOut` while it is out too, for a read its callers follow
  // together; a write ends that.
  async function request<T>(
    path: string,
    init: RequestInit = {},
    session?: Session | null,
    { shareWhileOut = false }: { shareWhileOut?: boolean } = {},
  ): Promise<T> {
    const method = (init.method ?? "GET").toUpperCase();
    if (method !== "GET" && method !== "HEAD" && method !== "OPTIONS") endSharedReads();
    const plain = method === "GET" && Object.keys(init).every((field) => field === "method" || field === "signal");
    const text = plain
      ? await readSharedInTask(`${apiRoot}${path}\n${session?.token ?? ""}`,
        (signal) => requestText(path, { ...init, signal }, session), init.signal, shareWhileOut)
      : await requestText(path, init, session);
    return responseBodyJSON(text) as T;
  }

  // A write is held while it goes, so a read asked for meanwhile from
  // outside waits for it (lib/ownWritesInFlight.ts). A request about a page
  // made on this device goes once the server has made it, and not before,
  // when the server would refuse it, and one about a data source once the
  // changes made here making or changing it are answered
  // (lib/pagesNotYetMade.ts).
  async function requestText(
    path: string,
    init: RequestInit,
    session?: Session | null,
  ): Promise<string> {
    const method = (init.method ?? "GET").toUpperCase();
    const read = method === "GET" || method === "HEAD" || method === "OPTIONS";
    const release = read ? null : holdOwnWrite();
    try {
      const pages = pagesOfRequest(path, init.body);
      const dataSource = dataSourceOfRequest(path);
      if (pages.length || dataSource) {
        await Promise.all([...pages.map((pageId) => afterPageMade(pageId, init.signal)),
          ...dataSource ? [afterDataSourceMade(dataSource, init.signal)] : []]);
        if (init.signal?.aborted) throw init.signal.reason ?? new DOMException("Aborted", "AbortError");
      }
      return await send(path, init, session);
    } finally {
      release?.();
    }
  }

  // The answer's text: a request that failed throws, as its answer says.
  async function send(
    path: string,
    init: RequestInit,
    session?: Session | null,
  ): Promise<string> {
    const requestToken = session?.token;
    const headers = new Headers(init.headers);
    if (init.body && !(init.body instanceof FormData) && !headers.has("content-type")) {
      headers.set("content-type", "application/json");
    }
    if (requestToken) {
      headers.set("authorization", `Bearer ${requestToken}`);
      // The app keeps a renewed sign-in's token, so its requests may renew it.
      headers.set("x-leafen-session-renew", "1");
    }
    // Writes name the history this device's copies are from; a restored
    // server refuses them (lib/dataEpoch.ts).
    const epoch = session?.user?.id ? dataEpochFor(session.user.id) : null;
    if (epoch && !headers.has("x-leafen-data-epoch")) headers.set("x-leafen-data-epoch", epoch);
    // Once the server is known to be restored, a write naming that epoch is
    // refused here as the server would refuse it, without going out
    // (2026-10-29: a tab left on "나중에" asked the server every ten seconds,
    // each refusal a line of the server's log); taking the server's anew
    // names the new epoch. Asking for a file's link changes nothing, and the
    // page left to copy from shows its images.
    const method = (init.method ?? "GET").toUpperCase();
    if (epoch && serverWasRestored() && method !== "GET" && method !== "HEAD" && method !== "OPTIONS" && !/^\/files\/[^/]+\/capability$/u.test(path)) {
      throwResponseError({ error: { code: workspaceMembershipErrorCodes.dataEpochChanged } }, 409);
    }

    // A write made as the page goes outlives it (lib/pageLeaving.ts).
    const outlives = pageLeaving() && method !== "GET" && method !== "HEAD" ? { keepalive: true } : {};
    // A read goes past the browser's own cache, which never answers one of
    // these: Chrome holds a read of an address while another read of it is
    // out, until that one is answered (its cache lock), so a read asked
    // while the same one was out came one whole read after it (2026-11-13,
    // NAS-class server: a database page's sharing summary and sidebar, the
    // settings' profile; Notion's top items came after two whole reads).
    const uncached = (method === "GET" || method === "HEAD") && init.cache === undefined ? { cache: "no-store" as const } : {};
    // A plain read goes with the others asked in this task, as one request.
    const together = batchReads && method === "GET" && Object.keys(init).every((field) => field === "method" || field === "signal");
    let response: Response;
    let followedForward = false;
    const started = Date.now();
    // A busy server answers before doing any of a request, so the request
    // goes again after the wait it names (lib/serverBusy.ts).
    for (let attempt = 0; ; attempt += 1) {
      // Where this try went: another request may learn of a move meanwhile
      // (2026-11-05: the second of two then took the forward's answer, its
      // sign-in left behind on the way, for being signed out).
      const asked = serverPath(`${apiRoot}${path}`);
      const deadline = "keepalive" in outlives ? null : writeDeadline(method, init);
      try {
        const sent = { ...init, ...uncached, ...outlives, headers, ...(deadline ? { signal: deadline.signal } : {}) };
        response = together
          ? await readTogether({ asked, apiRoot, headers, signal: init.signal, send: fetchResponse, alone: () => fetchResponse(asked, sent) })
          : await fetchResponse(asked, sent);
      } catch (cause) {
        // A write may have reached the server though no answer came back,
        // given up on or failed: it is told as not known, and the screen
        // reads again once the server answers.
        const uncertain = method !== "GET" && method !== "HEAD" && method !== "OPTIONS";
        if (uncertain) noteUncertainWrite();
        if (init.signal?.aborted) {
          if (uncertain && cause && typeof cause === "object") Object.assign(cause, { writeUncertain: true });
          throw cause;
        }
        noteServerAway();
        if (serverChosenForPage()) askWhetherServerMoved();
        const reference = createReference();
        const error = new LeafenTransportError(reference, cause);
        if (uncertain) Object.assign(error, { writeUncertain: true });
        captureErrorFactoryCaller(error, request);
        log("warn", "api.request", error, {
          diagnosticReference: reference,
          errorCode: error.code,
          causeCode: error.code,
          causeStage: "transport",
        });
        throw error;
      } finally {
        deadline?.clear();
      }
      // The server's address changed under this page: the old one forwards a
      // request to the same place at the new one, and a browser leaves its
      // sign-in behind on the way, which would read as signed out (2026-11-05:
      // an open tab lost its sign-in and the one its old address hands over).
      // It goes again where the server now answers, and the page moves there
      // when that takes nothing. Only such a forward takes the sign-in along,
      // and never down to a plain address: the page's move hands it over then.
      const moved = response.redirected && !followedForward ? forwardedTo(asked, response.url) : null;
      if (moved) {
        reloadWhenAtRest({ shown: true, why: "server-moved" });
        // An app window is pointed at its server by its profile: starting
        // again moves the profile there once the server there vouches for it
        // (addressChangeFollow.ts), and this answer, made there, stands
        // (2026-11-05: asked the old address again, some 2,000 times a minute)
        // — but a refusal of a request with a sign-in, left behind on the way,
        // fails as unanswered and the window keeps it (2026-11-16: a read just
        // after the owner changed the address came back signed out, and the
        // app forgot its sign-in for good).
        const app = serverChosenForPage();
        if (app && (!requestToken || response.ok)) break;
        if (!app && (moved.protocol === "https:" || window.location.protocol !== "https:")) {
          noteServerMovedTo(moved.origin);
          followedForward = true;
          continue;
        }
        const reference = createReference();
        const error = new LeafenTransportError(reference, new Error(app
          ? "The login was left behind at the server's earlier address." : "The server's new address is a plain one."));
        captureErrorFactoryCaller(error, request);
        throw error;
      }
      const wait = await refusalWait(response, attempt, started);
      if (wait === null && response.status === 429 && method === "GET" && attempt < LIMITED_READ_RETRIES && !await admissionRefusal(response)) {
        const seconds = Math.min(5, Number(response.headers.get("retry-after")) || 2);
        await waitUnlessAborted(seconds * 1000 * (0.75 + Math.random() / 2), init.signal);
        continue;
      }
      if (wait === null) break;
      await waitUnlessAborted(wait, init.signal);
    }
    // A 502 or 504 is told once its body says whose it is (statusSaysServerAway).
    const status = response.status;
    const toldByBody = status === 502 || status === 504;
    const noteAnswer = (serverCode: boolean) => {
      if (status === 429 || statusSaysServerAway(status, serverCode)) noteServerAway();
      else {
        noteServerAnswered();
        noteAnswerAfterUncertainWrite();
      }
    };
    if (!toldByBody) noteAnswer(false);

    const renewed = response.headers.get("x-leafen-session-token");
    if (renewed && session) await renewSessionToken(session, renewed);
    // A body that stopped arriving (lib/requestDeadline.ts; Chrome reports it
    // as a failed fetch) fails as an unanswered request does; one that isn't
    // JSON, or is empty, reads as none. The text is read and parsed here, so
    // no engine's way of refusing an empty body reads as a failure (WebKit
    // rejects `json()` of a 204 with a DOMException, not a SyntaxError —
    // 2026-10-26: a phone's sign-out said the session it ended stayed open).
    // One given up while its body comes fails as given up, as before its
    // answer came: never as an answer of none (2026-11-12: Home's recent
    // edits, read again just after a phone's code sign-in, read null).
    const text = await response.text().catch((cause: unknown) => {
      if (init.signal?.aborted) throw cause;
      if (toldByBody) noteServerAway();
      const reference = createReference();
      const error = new LeafenTransportError(reference, cause);
      captureErrorFactoryCaller(error, request);
      log("warn", "api.request", error, {
        diagnosticReference: reference, errorCode: error.code, causeCode: error.code, causeStage: "body-stall",
      });
      throw error;
    });
    if (!response.ok) {
      const body = responseBodyJSON(text) as ErrorEnvelope | null;
      if (toldByBody) noteAnswer(typeof body?.error?.code === "string");
      if (response.status === 409 && body?.error?.code === workspaceMembershipErrorCodes.dataEpochChanged) {
        reportServerRestored(null);
      }
      reportSignedOutResponse(requestToken, response.status, body?.error?.code);
      const retryAfter = Number(response.headers.get("retry-after"));
      throwResponseError(body, response.status, Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter : undefined);
    }
    return text;
  }

  return { request, throwLeafenApiError, throwResponseError };
}

const jsonApiTransport = createJsonApiTransport();

export const request = jsonApiTransport.request;
export const throwLeafenApiError: (
  code: string,
  status: number,
  message: string,
  options: {
    cause?: unknown;
    causeCode?: string;
    reference?: unknown;
    stage: string;
  },
) => never = jsonApiTransport.throwLeafenApiError;
export const throwResponseError: (
  body: ErrorEnvelope | null,
  status: number,
) => never = jsonApiTransport.throwResponseError;

export const requestBinaryResponse = createAuthenticatedBinaryResponseRequest({
  apiRoot: API_ROOT,
  // Asked again while the server refuses it for now, as JSON requests are (lib/serverBusy.ts).
  fetch: (input, init) => fetchAskingAgain(() => globalThis.fetch(input, init), init?.signal),
  resolveUrl: serverPath,
  throwResponseError,
});
