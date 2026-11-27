import { useLayoutEffect, useState } from "react";
import { isTextField } from "./menuKeyboard";

// A window nobody just asked for — a first run's step, an offer, a request
// from outside — doesn't come up over someone writing (2026-11-05: what was
// being typed went to the window that came up, whose first button took a
// space). It waits until the keys have stopped for this long, and then stays.
// Writing is in a field still on screen: one gone with its screen — a
// sign-in's code or email, an account step's name — holds nothing back
// (2026-11-12: the workspace showed for five seconds after a code sign-in
// before the first run's step came over it).
const STILL_WRITING_MS = 5000;

let lastKeyAt = Number.NEGATIVE_INFINITY;
let lastKeyField: Element | null = null;

if (typeof window !== "undefined") {
  window.addEventListener("keydown", ({ target }) => {
    if (isTextField(target) || (target instanceof HTMLElement && target.isContentEditable)) {
      lastKeyAt = performance.now();
      lastKeyField = target as Element;
    }
  }, { capture: true, passive: true });
}

/** How long writing still holds a window back, in ms: none once its field is gone. */
function stillWriting() {
  if (!lastKeyField?.isConnected) return 0;
  return lastKeyAt + STILL_WRITING_MS - performance.now();
}

/** Whether a window that wants to come up on its own may yet. */
export function useAfterTyping(wanted: boolean): boolean {
  const [clear, setClear] = useState(false);
  // Before paint: with nothing being typed the window shows as it's wanted.
  useLayoutEffect(() => {
    if (!wanted) {
      setClear(false);
      return undefined;
    }
    let timer = 0;
    const look = () => {
      const left = stillWriting();
      if (left <= 0) setClear(true);
      else timer = window.setTimeout(look, left);
    };
    look();
    return () => window.clearTimeout(timer);
  }, [wanted]);
  return wanted && clear;
}
