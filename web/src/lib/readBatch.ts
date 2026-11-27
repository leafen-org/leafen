// A page's start asks for some thirty reads at once, and over a plain http
// address (HTTP/1.1) a browser sends at most six at a time to one server,
// each a whole round trip: on a NAS's address a database page's reads
// waited up to 370 ms for a connection, and the start's last answer came
// 1.3 s in (2026-11-13). Reads asked in the same task go together as one
// request, GET /read-batch?read=…&read=…, which the server answers read by
// read as each is done (read_batch.go); each asker gets the Response it
// would have had alone. A read alone goes alone, and whatever a batch
// leaves unanswered — a server without batches, a batch that failed or was
// cut — goes again alone.

import { holdInFlight } from "./requestDeadline";

const BATCH_PATH = "/read-batch";
const BATCH_TYPE = "application/x-leafen-read-batch";
const MOST = 40;
const MOST_ADDRESS = 6000;
const NO_BODY = new Set([204, 205, 304]);

type Send = (input: string, init: RequestInit) => Promise<Response>;

type Read = {
  read: string;
  signal: AbortSignal | null | undefined;
  alone: () => Promise<Response>;
  resolve: (response: Response) => void;
  reject: (reason: unknown) => void;
  settled: boolean;
  sentAlone?: boolean;
};

type Gathering = { prefix: string; headers: Headers; send: Send; reads: Read[] };

type Answer = { read: number; status: number; headers?: Record<string, string>; length: number };

// "leafen.reads.alone" = "1" in the page's storage sends each read by itself,
// for watching one in the browser's network panel or a script.
const ALONE_KEY = "leafen.reads.alone";

function readsAlone() {
  try {
    return localStorage.getItem(ALONE_KEY) === "1";
  } catch {
    return false;
  }
}

const gathering = new Map<string, Gathering>();
// Addresses whose server has no read batch (an older one).
const withoutBatches = new Set<string>();

// The next task, not held back as a hidden tab's timers are.
function nextTask(run: () => void) {
  const channel = new MessageChannel();
  channel.port1.onmessage = () => {
    channel.port1.close();
    run();
  };
  channel.port2.postMessage(null);
}

function settle(read: Read, outcome: { response: Response } | { reason: unknown }) {
  if (read.settled) return;
  read.settled = true;
  if ("response" in outcome) read.resolve(outcome.response);
  else read.reject(outcome.reason);
}

function goAlone(read: Read) {
  if (read.settled || read.sentAlone) return;
  read.sentAlone = true;
  read.alone().then((response) => settle(read, { response }), (reason: unknown) => settle(read, { reason }));
}

/**
 * A plain read's answer, asked of the server together with the other reads
 * asked in this task under the same sign-in: `asked` is the read's address
 * under `apiRoot`, `send` the page's fetch, `alone` the read sent by itself.
 */
export function readTogether(
  { asked, apiRoot, headers, signal, send, alone }:
  { asked: string; apiRoot: string; headers: Headers; signal?: AbortSignal | null; send: Send; alone: () => Promise<Response> },
): Promise<Response> {
  const at = asked.indexOf(`${apiRoot}/`);
  const prefix = at < 0 ? "" : asked.slice(0, at);
  if (at < 0 || withoutBatches.has(prefix) || signal?.aborted || typeof MessageChannel === "undefined" || readsAlone()) return alone();
  const key = `${prefix}\n${[...headers].map(([name, value]) => `${name}:${value}`).sort().join("\n")}`;
  return new Promise<Response>((resolve, reject) => {
    const read: Read = { read: asked.slice(at + apiRoot.length), signal, alone, resolve, reject, settled: false };
    signal?.addEventListener("abort", () => settle(read, { reason: signal.reason ?? new DOMException("Aborted", "AbortError") }), { once: true });
    let together = gathering.get(key);
    if (!together) {
      together = { prefix, headers, send, reads: [] };
      gathering.set(key, together);
      const asOne = together;
      nextTask(() => {
        gathering.delete(key);
        const live = asOne.reads.filter((each) => !each.settled);
        if (live.length < 2) live.forEach(goAlone);
        else for (const part of parts(live)) void (part.length < 2 ? part.forEach(goAlone) : sendBatch(asOne, part, apiRoot));
      });
    }
    together.reads.push(read);
  });
}

// Reads in batches of at most MOST, each address short enough for the
// proxies a server may sit behind (nginx's lines end at 8 KB).
function parts(reads: Read[]): Read[][] {
  const out: Read[][] = [];
  let part: Read[] = [];
  let length = 0;
  for (const read of reads) {
    const more = encodeURIComponent(read.read).length + 6;
    if (part.length && (part.length >= MOST || length + more > MOST_ADDRESS)) {
      out.push(part);
      part = [];
      length = 0;
    }
    part.push(read);
    length += more;
  }
  if (part.length) out.push(part);
  return out;
}

async function sendBatch(together: Gathering, reads: Read[], apiRoot: string) {
  const controller = new AbortController();
  // A batch stops once none of its reads is waiting and an asker gave up;
  // one whose reads were all answered ends by itself.
  const answered = new Set<Read>();
  const stopWhenAllSettled = () => {
    if (reads.every((read) => read.settled) && reads.some((read) => !answered.has(read))) controller.abort();
  };
  for (const read of reads) read.signal?.addEventListener("abort", stopWhenAllSettled, { once: true });
  // Each read is out until its own answer comes (or it goes alone).
  const releases = new Map(reads.map((read) => [read, holdInFlight()]));
  try {
    const query = reads.map((read) => `read=${encodeURIComponent(read.read)}`).join("&");
    const response = await together.send(`${together.prefix}${apiRoot}${BATCH_PATH}?${query}`, {
      headers: together.headers, cache: "no-store", signal: controller.signal,
    });
    const batched = response.ok && !response.redirected && response.headers.get("content-type")?.startsWith(BATCH_TYPE);
    // A server that doesn't know the batch (404, or its page for an unknown address) is asked alone from then on.
    if (response.status === 404 || response.status === 405 || (response.ok && !response.redirected && !batched)) withoutBatches.add(together.prefix);
    if (!batched || !response.body) return;
    await readAnswers(response.body, (answer, body) => {
      const read = reads[answer.read];
      if (!read || read.settled) return;
      // A read the server doesn't hold in a batch (a stream, a file) goes alone.
      if (answer.status < 200 || answer.status > 599) {
        releases.get(read)?.();
        goAlone(read);
        return;
      }
      answered.add(read);
      releases.get(read)?.();
      settle(read, { response: new Response(NO_BODY.has(answer.status) ? null : body, { status: answer.status, headers: answer.headers ?? {} }) });
    });
  } catch {
    // Whatever the batch left unanswered goes alone, below.
  } finally {
    releases.forEach((release) => release());
    reads.forEach(goAlone);
  }
}

// Each answer as it comes: its line, then its body's bytes. Read so, a batch
// often shows in Chrome's network panel as cancelled once its last answer is
// in, though it came whole and its connection is kept for the next request
// (2026-11-13: the same answer read whole by text() showed as finished).
async function readAnswers(body: ReadableStream<Uint8Array>, take: (answer: Answer, body: Uint8Array<ArrayBuffer>) => void) {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  const pieces: Uint8Array[] = [];
  let held = 0;
  const join = (length: number) => {
    const out = new Uint8Array(length);
    let filled = 0;
    while (filled < length) {
      const piece = pieces[0];
      const used = Math.min(piece.length, length - filled);
      out.set(piece.subarray(0, used), filled);
      filled += used;
      if (used === piece.length) pieces.shift();
      else pieces[0] = piece.subarray(used);
    }
    held -= length;
    return out;
  };
  const lineEnd = () => {
    let offset = 0;
    for (const piece of pieces) {
      const at = piece.indexOf(10);
      if (at >= 0) return offset + at;
      offset += piece.length;
    }
    return -1;
  };
  let next: Answer | null = null;
  for (;;) {
    for (;;) {
      if (!next) {
        const end = lineEnd();
        if (end < 0) break;
        const line = decoder.decode(join(end + 1)).trim();
        if (!line) continue;
        next = JSON.parse(line) as Answer;
      }
      if (held < next.length) break;
      take(next, join(next.length));
      next = null;
    }
    const { done, value } = await reader.read();
    if (done) return;
    pieces.push(value);
    held += value.length;
  }
}
