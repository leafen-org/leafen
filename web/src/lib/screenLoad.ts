// A screen's code that doesn't arrive within the wait fails as code that
// couldn't load (2026-10-17: a stalled connection could leave a screen on its
// placeholder for good); the boundary around it says so where the screen
// would be (components/ScreenLoadBoundary.tsx).
const SCREEN_LOAD_WAIT_MS = 30_000;

/**
 * The load, failing when it takes longer than the wait. The wait counts only
 * while the tab shows: a tab behind others that a new build reloaded is given
 * little of a busy machine, and its code came after the wait there, leaving
 * it on "화면을 불러오는 데 시간이 걸리고 있습니다" with an error reference,
 * which a moment in front would have drawn (2026-11-12, tabs reloaded behind
 * others on a new build).
 */
export function withLoadDeadline<T>(load: Promise<T>, waitMs = SCREEN_LOAD_WAIT_MS): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let left = waitMs, since = 0;
  let late: () => void = () => {};
  const lateness = new Promise<never>((_, reject) => {
    late = () => reject(Object.assign(new Error("A screen's code didn't arrive in time."), {
      code: "LEAFEN_SCREEN_LOAD_TIMEOUT" as const,
      stillComing: load,
    }));
  });
  const hidden = () => typeof document !== "undefined" && document.visibilityState === "hidden";
  const count = () => {
    if (timer !== undefined || hidden()) return;
    since = Date.now();
    timer = setTimeout(late, left);
  };
  const follow = () => {
    if (!hidden()) {
      count();
      return;
    }
    if (timer === undefined) return;
    clearTimeout(timer);
    timer = undefined;
    left = Math.max(0, left - (Date.now() - since));
  };
  count();
  if (typeof document !== "undefined") document.addEventListener("visibilitychange", follow);
  return Promise.race([load, lateness]).finally(() => {
    clearTimeout(timer);
    if (typeof document !== "undefined") document.removeEventListener("visibilitychange", follow);
  });
}

/**
 * The load a screen gave up on, which may yet arrive. On a machine busy enough
 * every tab that reloaded for a new build sat on the error screen with its
 * code there a little later (2026-11-05: 53 seconds, 18 tabs).
 */
export function screenCodeStillComing(error: unknown): Promise<unknown> | null {
  const coming = (error as { code?: string; stillComing?: Promise<unknown> } | null);
  return coming?.code === "LEAFEN_SCREEN_LOAD_TIMEOUT" ? coming.stillComing ?? null : null;
}

const SCREEN_LOAD_FAILED = "leafenScreenLoadFailed";

/** What a screen's code load threw, marked as such: no fault of the screen, only its code not here. */
export function screenLoadFailure(error: unknown): Error {
  const failure = error instanceof Error ? error : new Error(String(error));
  try {
    Object.defineProperty(failure, SCREEN_LOAD_FAILED, { value: true });
  } catch {
    // A frozen error stays unmarked; its message may still name the file.
  }
  return failure;
}

/** Whether an error is a screen's code not loading, rather than a screen failing to draw. */
export function isScreenLoadFailure(error: unknown) {
  return error instanceof Error
    && ((error as unknown as Record<string, unknown>)[SCREEN_LOAD_FAILED] === true || screenCodeStillComing(error) !== null);
}
