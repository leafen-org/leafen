// An outside sign-in Leafen starts — a connected service's, an app's
// account — runs in a window of its own, and this is where that window and
// Leafen's meet. Leafen's pages keep Cross-Origin-Opener-Policy:
// same-origin, so once the window reaches the outside page the browser cuts
// it off from Leafen's: its handle reads closed while it is still open, and
// the page that comes back (sign_in_callback_page.go) has no opener. That
// page says how the sign-in went on a channel of this address; a Leafen
// window waiting for it answers, and the page closes itself. Where no word
// can come — the page came back at another address, or Leafen runs as an
// app at its own — the server is asked. A window the person closed is known
// by Leafen's own being in front again, or pressed, with no word.

/** The callback page's word: its kind, whose sign-in it was, how it went. */
export type SignInWord = { type?: unknown; page?: unknown; [field: string]: unknown };

const CHANNEL = "leafen:sign-in";
const SEEN = "leafen:sign-in-seen";
const TICK_MS = 250;
/** How long Leafen's window is in front, the sign-in window's handle closed, before the person has left it. */
const BACK_MS = 1000;
const ASK_MS = 2000;
/** A sign-in's state lasts this long (mcpServiceStateSeconds, appOAuthStateSeconds): the longest wait, and how long a word may still come after it ended. */
const LATE_MS = 600_000;

/**
 * Hears the callback pages of this address whose word `matches`, answering
 * each so it closes itself; `popup`'s own message counts too, where the
 * browser left it an opener. Returns the way to stop.
 */
export function hearSignIn(matches: (word: SignInWord) => boolean, heard: (word: SignInWord) => void, popup?: Window | null) {
  const channel = typeof BroadcastChannel === "undefined" ? null : new BroadcastChannel(CHANNEL);
  const take = (word: unknown) => {
    if (!word || typeof word !== "object" || !matches(word as SignInWord)) return;
    const page = (word as SignInWord).page;
    if (typeof page === "string") channel?.postMessage({ type: SEEN, page });
    heard(word as SignInWord);
  };
  const onChannel = (event: MessageEvent) => take(event.data);
  const onMessage = (event: MessageEvent) => { if (popup && event.source === popup) take(event.data); };
  channel?.addEventListener("message", onChannel);
  window.addEventListener("message", onMessage);
  let stopped = false;
  return () => {
    if (stopped) return;
    stopped = true;
    channel?.close();
    window.removeEventListener("message", onMessage);
  };
}

export type SignInWait = {
  /** Whether a callback page's word is this sign-in's. */
  matches: (word: SignInWord) => boolean;
  /** Asks the server whether this sign-in has completed. */
  completed?: () => Promise<boolean>;
  /** A word after the wait ended: the sign-in finished in its window after all. */
  late?: () => void;
};

/**
 * Resolves once the sign-in in `popup` came back — its page's word, or the
 * server's — or the person left it: closed it, or came back to Leafen.
 */
export function signInWindowEnded(popup: Window, { matches, completed, late }: SignInWait) {
  return new Promise<void>((resolve) => {
    const opened = Date.now();
    let ended = false;
    let left = false;
    // Leafen's window seen behind the sign-in window, and pressed since.
    let away = false;
    let pressed = false;
    let backSince = 0;
    let asking = false;
    let asked = opened;
    const press = () => { pressed = true; };
    const finish = (leaving: boolean) => {
      if (ended) return;
      ended = true;
      left = leaving;
      window.clearInterval(tick);
      window.removeEventListener("pointerdown", press, true);
      window.removeEventListener("keydown", press, true);
      // A word that still comes closes its page; after the person left, it
      // also says the sign-in finished there after all.
      window.setTimeout(stop, LATE_MS);
      resolve();
    };
    const stop = hearSignIn(matches, () => {
      if (!ended) return finish(false);
      if (left) {
        left = false;
        late?.();
      }
    }, popup);
    window.addEventListener("pointerdown", press, true);
    window.addEventListener("keydown", press, true);
    const tick = window.setInterval(() => {
      const now = Date.now();
      const front = document.visibilityState === "visible" && document.hasFocus();
      if (!front) away = true;
      // The handle reads closed while the window is cut off, so only Leafen
      // in front again (where focus is real) or pressed says the person left
      // the window; the state's lifetime ends the wait in any case.
      if (popup.closed && front && (away || pressed)) {
        backSince ||= now;
        if (now - backSince >= BACK_MS) return finish(true);
      } else backSince = 0;
      if (now - opened >= LATE_MS) return finish(true);
      if (!completed || asking || now - asked < ASK_MS) return;
      asking = true;
      asked = now;
      void completed().then((done) => { if (done) finish(false); }, () => undefined).finally(() => { asking = false; });
    }, TICK_MS);
  });
}
