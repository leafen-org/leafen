import { administratorErrorCodes } from "@/administratorWire.generated";
import { LeafenApiError, LeafenTransportError } from "@/apiTransport";

const DURABLE_JOB_POLL_INTERVAL_MS = 250;
const DURABLE_JOB_POLL_LIMIT = 24 * 60 * 60 * 4;
// A job outlives its server: while the server can't be reached or is
// starting again, the job is asked after again, less often, and goes on
// where it was (2026-10-16: a copy reported as failed while the server
// restarted was made once it was back).
const DURABLE_JOB_UNREACHABLE_MAX_MS = 5_000;

/** A job's server didn't answer, or answers it is away: the job goes on. */
export function durableJobUnreachable(error: unknown) {
  // A server whose data moved answers, and goes on refusing (2026-11-04: an
  // export there waited on a server said not to answer).
  if (error instanceof LeafenApiError && error.code === administratorErrorCodes.serverMoved) return false;
  return error instanceof LeafenTransportError
    || (error instanceof LeafenApiError && error.status >= 500);
}

function waitForDurableJobPoll(signal: AbortSignal, delay = DURABLE_JOB_POLL_INTERVAL_MS) {
  if (signal.aborted) return Promise.reject(new DOMException("Aborted", "AbortError"));
  return new Promise<void>((resolve, reject) => {
    const timeout = globalThis.setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, delay);
    const onAbort = () => {
      globalThis.clearTimeout(timeout);
      reject(new DOMException("Aborted", "AbortError"));
    };
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

export async function waitForDurableJob<T>(options: {
  initial: T;
  isSameJob: (next: T) => boolean;
  isTerminal: (next: T) => boolean;
  onProgress: (next: T) => void;
  read: () => Promise<T>;
  signal: AbortSignal;
}) {
  let current = options.initial;
  let delay = DURABLE_JOB_POLL_INTERVAL_MS;
  options.onProgress(current);
  for (let attempt = 0; attempt <= DURABLE_JOB_POLL_LIMIT; attempt += 1) {
    if (options.signal.aborted) throw new DOMException("Aborted", "AbortError");
    if (options.isTerminal(current)) return current;
    if (attempt === DURABLE_JOB_POLL_LIMIT) {
      throw Object.assign(new Error("Durable job observation timed out."), {
        code: "LEAFEN_DURABLE_JOB_POLL_TIMEOUT",
      });
    }
    await waitForDurableJobPoll(options.signal, delay);
    let next: T;
    try {
      next = await options.read();
    } catch (error) {
      if (!durableJobUnreachable(error) || options.signal.aborted) throw error;
      delay = Math.min(delay * 2, DURABLE_JOB_UNREACHABLE_MAX_MS);
      continue;
    }
    delay = DURABLE_JOB_POLL_INTERVAL_MS;
    if (!options.isSameJob(next)) {
      throw Object.assign(new Error("Durable job identity changed."), {
        code: "LEAFEN_DURABLE_JOB_RESPONSE_INVALID",
      });
    }
    current = next;
    options.onProgress(current);
  }
  return current;
}
