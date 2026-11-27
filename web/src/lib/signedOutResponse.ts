import { authErrorCodes } from "@/authWire.generated";
import { serverScopedName } from "./serverOrigin";

// A request made with this browser's sign-in that the server answers as
// signed out — the session expired, was revoked or its account was closed —
// ends the sign-in here: the app shows the sign-in screen and, once the same
// person signs in again, opens where they were. A refusal of what the person
// may do (403) is not this.
//
// A write that ends the sign-in it is made with and answers with this
// device's next one — a password change ends every sign-in of the account,
// this device's too — is made before its answer is here: a request out
// meanwhile comes back signed out (2026-11-14: the open page's socket closed
// and its status read was refused, and the device that changed its password
// was signed out before its new sign-in came). While such a handover goes, in
// this tab or another tab of this browser, the refusals wait, and are told
// once it is over, against the sign-in held then: the new one when it came.
// Other devices, which hold no handover, are signed out at once as before.

const SIGNED_OUT_CODES = new Set<string>(["LEAFEN_UNAUTHORIZED", authErrorCodes.sessionInvalid]);
const listeners = new Set<(token: string) => void>();

// What another tab of this browser marks while it hands its sign-in over, and
// how long that holds refusals here at most, had it closed before it was
// over: past its request's own wait for an answer (lib/requestDeadline.ts).
const HANDOVER_KEY = "leafen:v2:sign-in-handover";
const HANDOVER_HOLD_MS = 60_000;

let handoversHere = 0;
const held = new Set<string>();
let stopWatching: (() => void) | null = null;

/** Whether an answer to a signed-in request says that sign-in is over. */
export function isSignedOutResponse(status: unknown, code: unknown) {
  return status === 401 && typeof code === "string" && SIGNED_OUT_CODES.has(code);
}

/**
 * Whether a sign-in is being handed over now, in this tab or another tab of
 * this browser: a signed-out answer meanwhile waits for the next sign-in and
 * says nothing yet of what the reader may have (lib/readDenial.ts).
 */
export function signInHandingOver() {
  return handoversHere > 0 || otherTabHandoverEnd() > 0;
}

/** Tells the app that a request made with `token` was answered as signed out. */
export function reportSignedOutResponse(token: string | undefined, status: number, code: unknown) {
  if (!token || !isSignedOutResponse(status, code)) return;
  if (handoversHere > 0 || otherTabHandoverEnd()) {
    held.add(token);
    tellHeldWhenOver();
    return;
  }
  tell(token);
}

/** Calls `listener` with the token of each request answered as signed out; returns the way to stop. */
export function onSignedOutResponse(listener: (token: string) => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Runs `handover`, a write that ends the sign-in it is made with and keeps
 * this device's next one before it resolves. Refusals told meanwhile, here or
 * in another tab of this browser, wait until it is over.
 */
export async function handOverSignIn<T>(handover: () => Promise<T>): Promise<T> {
  const unmark = markHandover();
  handoversHere += 1;
  try {
    return await handover();
  } finally {
    handoversHere -= 1;
    unmark();
    // Once the caller has taken what it answered, the held refusals are read
    // against the sign-in held then.
    window.setTimeout(tellHeldWhenOver, 0);
  }
}

function tell(token: string) {
  for (const listener of [...listeners]) listener(token);
}

// Held refusals are told once no handover goes: this tab's end tells them, and
// another tab's when its mark goes or would have ended.
function tellHeldWhenOver() {
  stopWatching?.();
  stopWatching = null;
  if (!held.size || handoversHere > 0) return;
  const end = otherTabHandoverEnd();
  if (end) {
    watchOtherTabHandover(end);
    return;
  }
  const tokens = [...held];
  held.clear();
  for (const token of tokens) tell(token);
}

function watchOtherTabHandover(end: number) {
  const key = serverScopedName(HANDOVER_KEY);
  const onStorage = (event: StorageEvent) => {
    if (event.key === null || event.key === key) tellHeldWhenOver();
  };
  const timer = window.setTimeout(tellHeldWhenOver, Math.max(0, end - Date.now()));
  window.addEventListener("storage", onStorage);
  stopWatching = () => {
    window.clearTimeout(timer);
    window.removeEventListener("storage", onStorage);
  };
}

// When the handover another tab marked stops holding refusals, or 0 for none.
function otherTabHandoverEnd(): number {
  try {
    const mark = JSON.parse(localStorage.getItem(serverScopedName(HANDOVER_KEY)) ?? "null") as { until?: unknown } | null;
    const until = typeof mark?.until === "number" ? mark.until : 0;
    return until > Date.now() ? until : 0;
  } catch {
    return 0;
  }
}

// The mark holds no sign-in or credential, and each tab removes only its own.
function markHandover(): () => void {
  const key = serverScopedName(HANDOVER_KEY);
  const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
  try {
    localStorage.setItem(key, JSON.stringify({ id, until: Date.now() + HANDOVER_HOLD_MS }));
  } catch {
    // Without storage the other tabs aren't told; this one still waits.
  }
  return () => {
    try {
      const mark = JSON.parse(localStorage.getItem(key) ?? "null") as { id?: unknown } | null;
      if (mark?.id === id) localStorage.removeItem(key);
    } catch {
      // Nothing kept there to remove; the mark ends by itself.
    }
  };
}
