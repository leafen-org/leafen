import { useSyncExternalStore } from "react";

// One clock for every "2분"-style label on screen, read again each minute
// while any of them is shown.
let now = Date.now();
let timer = 0;
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  if (!timer) {
    now = Date.now();
    timer = window.setInterval(() => {
      now = Date.now();
      for (const each of listeners) each();
    }, 60_000);
  }
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0 && timer) {
      window.clearInterval(timer);
      timer = 0;
    }
  };
}

/** The time now, kept current each minute. */
export function useMinuteNow() {
  return useSyncExternalStore(subscribe, () => now, () => now);
}
