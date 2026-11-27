import { onServerAnswersAgain } from "./serverAnswers";

/**
 * Reads of one address with one sign-in, asked within one task of the page
 * — the parts of a screen that open together — go to the server once, and
 * each asker reads the answer as its own (2026-11-13, NAS-class server: every
 * start asked for its first-run state three times at once and its account
 * state twice). Nothing from the server or the person comes in within a
 * task, so every asker knew the same when it asked, and one answer serves
 * them as separate ones would. The sharing ends a few microtask turns after
 * the first ask, which a task always runs before the next task begins, at the
 * first write asked meanwhile, and once the read is answered. An asker that
 * stops stops only its own wait; the read stops once none waits.
 *
 * A read its callers follow together — the reader's sidebar layout, a page's
 * access summary — is shared besides while it is out (`whileOut`): the parts
 * of a screen that open a moment apart, in other tasks, take the answer of
 * the one already asked (2026-11-13: a database page's sidebar asked for the
 * layout, and its view tabs again 70–85 ms later; its top bar and its
 * database each asked for the page's access summary). Until it is answered,
 * a write asked meanwhile, or the server answering again after a time away.
 */

// Enough for an asker that awaits a few settled steps before it asks.
const SHARING_TURNS = 8;

type SharedRead = { key: string; answer: Promise<string>; stop: AbortController; waiting: number };

let open: Map<string, SharedRead> | null = null;
const out = new Map<string, SharedRead>();

function closeAfterTurns(reads: Map<string, SharedRead>, turns: number) {
  queueMicrotask(() => {
    if (open !== reads) return;
    if (turns > 1) closeAfterTurns(reads, turns - 1);
    else open = null;
  });
}

function forget(read: SharedRead) {
  if (open?.get(read.key) === read) open.delete(read.key);
  if (out.get(read.key) === read) out.delete(read.key);
}

/** Reads asked from now on go on their own, as after a write. */
export function endSharedReads() {
  open = null;
  out.clear();
}

// A read out while the server was away may never be answered: those asked
// once it answers again go on their own.
onServerAnswersAgain(() => out.clear());

function stopped(signal: AbortSignal) {
  return signal.reason ?? new DOMException("Aborted", "AbortError");
}

/** The answer's text, read once for every asker of `key` in this task, and while it is out with `whileOut`. */
export function readSharedInTask(
  key: string,
  read: (signal: AbortSignal) => Promise<string>,
  signal?: AbortSignal | null,
  whileOut = false,
): Promise<string> {
  if (signal?.aborted) return Promise.reject(stopped(signal));
  if (!open) {
    open = new Map();
    closeAfterTurns(open, SHARING_TURNS);
  }
  let shared = open.get(key) ?? (whileOut ? out.get(key) : undefined);
  if (!shared) {
    const made: SharedRead = { key, answer: Promise.resolve(""), stop: new AbortController(), waiting: 0 };
    // Once answered, the same read goes again.
    made.answer = read(made.stop.signal).finally(() => forget(made));
    made.answer.catch(() => undefined);
    open.set(key, made);
    shared = made;
  }
  // Asked while out, it is shared while out from now on too.
  if (whileOut && !out.has(key)) out.set(key, shared);
  const joined = shared;
  joined.waiting += 1;
  if (!signal) return joined.answer;
  return new Promise<string>((resolve, reject) => {
    const leave = () => {
      reject(stopped(signal));
      joined.waiting -= 1;
      if (joined.waiting > 0) return;
      forget(joined);
      joined.stop.abort(stopped(signal));
    };
    signal.addEventListener("abort", leave, { once: true });
    void joined.answer.then(resolve, reject).finally(() => signal.removeEventListener("abort", leave));
  });
}
