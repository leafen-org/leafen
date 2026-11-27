import { onServerAnswersAgain } from "./serverAnswers";

// What waits for the server goes again once the server may answer — it
// answers another request, the browser says its network is back, the window
// shows again — but no sooner than a pause after the try that waited: one
// second, doubling with each try in a row that waited too, up to half a
// minute. A request that fails at once by itself (refused on the way while
// the server answers every other one) marks the server away, and the next
// answered request marks it back: that was heard as the server come back and
// the change went again at once, over and over (2026-11-21: a page's title
// whose save failed at once went about 100 times in half a second; measured
// again, 24 in half a second, the page's own reads answered between). The
// server heard answering after an absence at least as long as the pause is
// back for real — stopped and started, or a network that came back — and
// what waits goes at once.
const FIRST_PAUSE_MS = 1_000;
const LONGEST_PAUSE_MS = 30_000;

export interface RetryPacing {
  /**
   * Goes now, or once the pause has passed — `now` for a person's press;
   * asked again meanwhile, it goes once, as last asked.
   */
  request(go: () => void, now?: boolean): void;
  /** A try waited for the server: the next goes a pause after, twice the last one. */
  waited(): void;
  /** A try went through: the next goes as soon as it is asked. */
  answered(): void;
  /** Lets go of what waits, and stops hearing the server. */
  dispose(): void;
}

export function createRetryPacing(): RetryPacing {
  let inRow = 0;
  let waitedAt = 0;
  let over = true;
  let waiting: (() => void) | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const pause = () => (inRow === 0 ? 0 : Math.min(LONGEST_PAUSE_MS, FIRST_PAUSE_MS * 2 ** (inRow - 1)));
  const go = () => {
    clearTimeout(timer);
    timer = undefined;
    const next = waiting;
    waiting = null;
    next?.();
  };
  const stopHearing = onServerAnswersAgain((awaySince) => {
    if (performance.now() - awaySince < pause()) return;
    over = true;
    if (waiting) go();
  });
  return {
    request(next, now = false) {
      waiting = next;
      const left = over || now ? 0 : waitedAt + pause() - performance.now();
      if (left <= 0) go();
      else timer ??= setTimeout(go, left);
    },
    waited() {
      inRow += 1;
      waitedAt = performance.now();
      over = false;
    },
    answered() {
      inRow = 0;
      over = true;
    },
    dispose() {
      clearTimeout(timer);
      timer = undefined;
      waiting = null;
      stopHearing();
    },
  };
}
