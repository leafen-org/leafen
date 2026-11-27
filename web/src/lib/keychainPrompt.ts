// The Mac app keeps sign-ins in the keychain (desktop/credentials.go), and
// macOS asks the person before the app reads an item another build kept —
// one signed otherwise, as a development build's or one from before the app
// was signed. The call then waits on macOS's prompt, which the window can't
// see: the window stood blank after "계속" until it was answered (2026-11-05,
// an ad hoc test build). A Developer ID build read its own items and those
// of the build it updated without asking (2026-11-24). A call still
// unanswered after a moment asks the app whether macOS shows its prompt and,
// once it does, the window says what it waits for (KeychainPromptNotice)
// until the call answers, the prompt allowed or denied.

// Long enough for a call that needs no prompt to have answered (a few ms).
const LOOK_AFTER_MS = 250;
const LOOK_EVERY_MS = 400;

const listeners = new Set<() => void>();
// The calls waiting on a prompt macOS was seen showing.
let waiting = 0;

function change(by: number): void {
  waiting += by;
  for (const listener of listeners) listener();
}

/** Whether a call to the keychain waits on macOS's prompt now. */
export function keychainPromptShowing(): boolean {
  return waiting > 0;
}

/** Calls `changed` when a call starts or ends waiting on macOS's prompt; returns the way to stop. */
export function followKeychainPrompt(changed: () => void): () => void {
  listeners.add(changed);
  return () => { listeners.delete(changed); };
}

/**
 * The keychain call `work`, said to be waiting on macOS's prompt from when
 * `promptShowing` (the app's look at the screen) first finds one while it
 * hasn't answered, until it does.
 */
export async function watchKeychainPrompt<T>(work: Promise<T>, promptShowing: () => Promise<boolean>): Promise<T> {
  let settled = false;
  let counted = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const look = async () => {
    const showing = await promptShowing().catch(() => false);
    if (settled) return;
    if (!showing) {
      timer = setTimeout(() => void look(), LOOK_EVERY_MS);
      return;
    }
    counted = true;
    change(1);
  };
  timer = setTimeout(() => void look(), LOOK_AFTER_MS);
  try {
    return await work;
  } finally {
    settled = true;
    clearTimeout(timer);
    if (counted) change(-1);
  }
}
