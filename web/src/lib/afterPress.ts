/**
 * Work a press sets off that its first frame need not wait for. A press
 * draws its visible reaction at once; what follows from it (sorting,
 * counting, working out, writing down) runs after the next frame is painted,
 * in pieces of a few milliseconds, and is dropped when a newer press comes
 * (user decision 2026-11-12: every press answers on the next frame).
 *
 *   afterPress((signal) => inSlices(rows, place, { signal }))
 *
 * Work that must finish whatever comes next (a save, an outbox write) takes
 * `afterNextPaint` without a signal, or a signal of its own.
 */

type YieldingScheduler = { yield?: () => Promise<void> };

const SLICE_MS = 5;
// A page sent to the background meanwhile paints no frame: what waits for
// one runs anyway after this long (longer than a slow frame).
const UNPAINTED_WAIT_MS = 1_000;

let pressController: AbortController | null = null;
let listening = false;

function newPress(event: Event) {
  // A modifier held alone is not a press (it starts a chord or a shortcut).
  if (event instanceof KeyboardEvent && ["Shift", "Control", "Alt", "Meta"].includes(event.key)) return;
  pressController?.abort();
  pressController = null;
}

/**
 * A signal aborted at the next press — a pointer pressed or a key — after
 * the one being handled now: work the press set off and a newer one makes
 * stale. Taken in a press's own handler, it outlives that press.
 */
export function untilNextPress(): AbortSignal {
  if (!listening && typeof window !== "undefined") {
    listening = true;
    // Capture, so the press is new before any handler of the page takes it.
    window.addEventListener("pointerdown", newPress, { capture: true, passive: true });
    window.addEventListener("keydown", newPress, { capture: true, passive: true });
  }
  pressController ??= new AbortController();
  return pressController.signal;
}

/** A task after the current one, ahead of timers (a message, unlike setTimeout, is not held back 4 ms). */
function nextTask(run: () => void) {
  const channel = new MessageChannel();
  channel.port1.onmessage = () => {
    channel.port1.close();
    run();
  };
  channel.port2.postMessage(null);
}

/**
 * Runs `run` once the frame after now has been painted, unless `signal`
 * aborts or it is called off first (then `onCancel`); returns how to call it
 * off. A frame's callbacks run before it paints, so the work goes to the
 * task after that frame's.
 */
export function afterNextPaint(run: () => void, signal?: AbortSignal, onCancel?: () => void): () => void {
  let done = signal?.aborted ?? false;
  let frame = 0;
  let unpainted: ReturnType<typeof setTimeout> | undefined;
  const stop = () => {
    done = true;
    if (frame) cancelAnimationFrame(frame);
    clearTimeout(unpainted);
    signal?.removeEventListener("abort", cancel);
  };
  const cancel = () => {
    if (done) return;
    stop();
    onCancel?.();
  };
  if (done) {
    onCancel?.();
    return cancel;
  }
  const go = () => {
    if (done) return;
    stop();
    run();
  };
  // Where nothing paints (no window, a page in the background), the next
  // task stands in for the frame.
  const painting = typeof requestAnimationFrame === "function" && !(typeof document !== "undefined" && document.hidden);
  if (painting) frame = requestAnimationFrame(() => nextTask(go));
  unpainted = setTimeout(go, painting ? UNPAINTED_WAIT_MS : 0);
  signal?.addEventListener("abort", cancel, { once: true });
  return cancel;
}

/** The frame after now painted: true, or false when `signal` aborted first. */
export function nextPaint(signal?: AbortSignal): Promise<boolean> {
  return new Promise((resolve) => {
    afterNextPaint(() => resolve(true), signal, () => resolve(false));
  });
}

/**
 * Gives the main thread back for a moment, so a press waiting is handled
 * first: the browser's own scheduler where it has one (it resumes this work
 * ahead of other tasks), else the next task.
 */
export function yieldToPress(): Promise<void> {
  const scheduler = (globalThis as { scheduler?: YieldingScheduler }).scheduler;
  if (typeof scheduler?.yield === "function") return scheduler.yield();
  return new Promise((resolve) => nextTask(resolve));
}

/**
 * Calls `step` for each of `items` in slices of about `sliceMs`, giving the
 * main thread back between slices; true once every item was stepped, false
 * when `signal` aborted first.
 */
export async function inSlices<T>(
  items: Iterable<T>,
  step: (item: T, index: number) => void,
  { signal, sliceMs = SLICE_MS }: { signal?: AbortSignal; sliceMs?: number } = {},
): Promise<boolean> {
  let sliceStart = performance.now();
  let index = 0;
  for (const item of items) {
    if (signal?.aborted) return false;
    step(item, index);
    index += 1;
    if (performance.now() - sliceStart >= sliceMs) {
      await yieldToPress();
      sliceStart = performance.now();
    }
  }
  return !signal?.aborted;
}

/**
 * Runs `work` after the next frame is painted with a signal the next press
 * aborts; returns how to call it off. What `work` does in pieces checks the
 * signal between them (inSlices).
 */
export function afterPress(work: (signal: AbortSignal) => void | Promise<unknown>): () => void {
  const signal = untilNextPress();
  return afterNextPaint(() => {
    void Promise.resolve(work(signal)).catch((error: unknown) => {
      if (!signal.aborted) throw error;
    });
  }, signal);
}
