export interface LocalFirstSnapshot<Value> {
  complete: true;
  key: string;
  principalId: string;
  serverKey: string;
  value: Value;
  version: 1;
}

export interface LocalFirstSnapshotIdentity {
  key: string;
  principalId: string;
  serverKey: string;
}

export interface LocalFirstSnapshotLoad<Value>
  extends LocalFirstSnapshotIdentity {
  /** The cached copy is older than what was shown here: it waits for the server, and shows only if that can't answer. */
  cacheBehind?: () => boolean | Promise<boolean>;
  /**
   * What the screen already holds of it — the open page's graph, a table's
   * row, the sidebar's list — drawn at once (`onSnapshot(value, "held")`),
   * in the caller's own task, before this device's copy is read and the
   * server asked; both still follow (the copy as `cacheBehind` lets it). A
   * held value never stands in for the server: one that can't answer fails
   * the load as before, with what was held left drawn.
   */
  held?: () => Value | null | undefined;
  /**
   * The screen shows the server's answer already, newer than any copy: the
   * copy is not read (2026-11-14: a 47,000-page list read its 37 MB copy
   * again at every read for a change elsewhere, only to leave it unshown).
   */
  copyUnneeded?: boolean;
  isCurrent(): boolean;
  onDefinitiveDenial?: () => void;
  /** Whether the server's answer was kept as the cached copy, once that is known. */
  onNetworkKept?: (kept: boolean, value: Value) => void;
  onSnapshot(
    value: Value,
    source: "held" | "cache" | "network",
  ): boolean | void | Promise<boolean | void>;
  readNetwork?: (identity: LocalFirstSnapshotIdentity) => Promise<Value>;
}

export interface LocalFirstSnapshotLifecycleOptions<Value> {
  dropCache(identity: LocalFirstSnapshotIdentity): Promise<void>;
  isDefinitiveDenial(error: unknown): boolean;
  onCacheError?: (error: unknown, stage: "drop" | "read" | "write") => void;
  readCache(
    identity: LocalFirstSnapshotIdentity,
  ): Promise<LocalFirstSnapshot<Value> | null>;
  readNetwork?: (identity: LocalFirstSnapshotIdentity) => Promise<Value>;
  writeCache(snapshot: LocalFirstSnapshot<Value>): Promise<void>;
}

export type LocalFirstSnapshotLoadResult<Value> = {
  source: "cache" | "network";
  value: Value;
} | null;

// A copy the store refused (another tab held it past the wait, or it was
// full) is kept again a moment later, and again less often, until it is
// kept or something newer replaces it (2026-11-11: a refused copy counted as
// kept, and the page opened offline with the change before the last).
const KEEP_AGAIN_FIRST_MS = 1_500;
const KEEP_AGAIN_MOST_MS = 30_000;

// The server is asked at once, while the kept copy is read: the copy is drawn
// first only if it comes quickly and before the server answered, and the
// server's answer is never held back for it (2026-10-16: with another tab
// holding this device's store, the list waited on the kept copy and never
// asked the server; 2026-11-13: a page's body was asked for only once its
// copy's read ended, some 150 ms after its discussion on a NAS-class server,
// up to the whole wait on a busy device). One that comes later is drawn as it
// comes while the server hasn't answered, and stands in once it comes when
// the server can't answer (2026-11-11: offline, a 100,000-row page's copy
// came after the wait on a busy device, and the page said it couldn't open,
// then opened from that copy on the next try).
const KEPT_COPY_WAIT_MS = 1_500;
const LATE = Symbol("late");

/** The copy as read within the wait, or LATE: after it, or after the server answered first. */
function keptWithin<Value>(read: Promise<Value>, serverAnswered: Promise<void>): Promise<Value | typeof LATE> {
  return new Promise((resolve) => {
    const timer = setTimeout(() => resolve(LATE), KEPT_COPY_WAIT_MS);
    void read.then((value) => { clearTimeout(timer); resolve(value); });
    void serverAnswered.then(() => { clearTimeout(timer); resolve(LATE); });
  });
}

function validIdentity(identity: LocalFirstSnapshotIdentity): boolean {
  return Boolean(identity.key && identity.principalId && identity.serverKey);
}

function eligible<Value>(
  snapshot: LocalFirstSnapshot<Value> | null,
  identity: LocalFirstSnapshotIdentity,
): snapshot is LocalFirstSnapshot<Value> {
  return Boolean(
    snapshot
    && snapshot.version === 1
    && snapshot.complete === true
    && snapshot.key === identity.key
    && snapshot.principalId === identity.principalId
    && snapshot.serverKey === identity.serverKey,
  );
}

export function createLocalFirstSnapshotLifecycle<Value>(
  options: LocalFirstSnapshotLifecycleOptions<Value>,
) {
  /** Keeps `value` as the cached copy; whether the store took it. */
  async function keep(identity: LocalFirstSnapshotIdentity, value: Value): Promise<boolean> {
    if (!validIdentity(identity)) return false;
    try {
      await options.writeCache({ complete: true, ...identity, value, version: 1 });
      return true;
    } catch (error) {
      options.onCacheError?.(error, "write");
      return false;
    }
  }

  return {
    async load(
      input: LocalFirstSnapshotLoad<Value>,
    ): Promise<LocalFirstSnapshotLoadResult<Value>> {
      const identity: LocalFirstSnapshotIdentity = {
        key: input.key,
        principalId: input.principalId,
        serverKey: input.serverKey,
      };
      if (!validIdentity(identity) || !input.isCurrent()) return null;

      // What the screen holds, drawn before anything is awaited; the copy is
      // read meanwhile and drawn after it, the server asked beside it.
      const shown = input.held?.();
      const heldDrawn = shown == null ? undefined : input.onSnapshot(shown, "held");
      const readNetwork = input.readNetwork ?? options.readNetwork;
      let networkEnded: { ok: true } | { ok: false; error: unknown } | null = null;
      const network = (readNetwork
        ? readNetwork(identity)
        : Promise.reject(new Error("A local-first network reader is required."))).then((value) => {
        networkEnded = { ok: true };
        return value;
      }, (error: unknown) => {
        networkEnded = { ok: false, error };
        throw error;
      });
      network.catch(() => undefined);
      // The copy isn't drawn once the server answered, or refused for good.
      const serverAhead = () => networkEnded !== null
        && (networkEnded.ok || options.isDefinitiveDenial(networkEnded.error));
      const serverAnswered = network.then(() => undefined, (error: unknown) => (
        options.isDefinitiveDenial(error) ? undefined : new Promise<void>(() => undefined)));
      const copy: Promise<LocalFirstSnapshot<Value> | null> = input.copyUnneeded
        ? Promise.resolve(null)
        : options.readCache(identity);
      const reading = copy.then(
        (stored) => (eligible(stored, identity) ? stored : null),
        (error: unknown) => {
          options.onCacheError?.(error, "read");
          return null;
        },
      );
      if (heldDrawn !== undefined) {
        await heldDrawn;
        if (!input.isCurrent()) return null;
      }
      const quick = await keptWithin(reading, serverAnswered);
      if (!input.isCurrent()) return null;
      let cached = quick === LATE ? null : quick;
      let cachedAccepted = false;
      let copyHeldBack = Boolean(cached && await input.cacheBehind?.());
      if (!input.isCurrent()) return null;
      if (cached && !copyHeldBack && !serverAhead()) {
        cachedAccepted = await input.onSnapshot(cached.value, "cache") !== false;
        if (!input.isCurrent()) return null;
      }
      // A copy that comes after the wait, drawn as it comes until the server answers.
      let answered = false;
      let drawing: Promise<void> = Promise.resolve();
      const lateCopy = quick !== LATE ? drawing : reading.then((late) => {
        if (!late || answered || serverAhead() || !input.isCurrent()) return;
        cached = late;
        drawing = (async () => {
          copyHeldBack = Boolean(await input.cacheBehind?.());
          if (copyHeldBack || answered || serverAhead() || !input.isCurrent()) return;
          cachedAccepted = await input.onSnapshot(late.value, "cache") !== false;
        })();
        return drawing;
      });

      let value: Value;
      try {
        value = await network;
      } catch (error) {
        if (!input.isCurrent()) return null;
        if (!options.isDefinitiveDenial(error)) {
          // Without the server, a copy still being read stands in once it comes.
          await lateCopy;
          if (!input.isCurrent()) return null;
          if (cached && copyHeldBack) {
            cachedAccepted = await input.onSnapshot(cached.value, "cache") !== false;
            if (!input.isCurrent()) return null;
          }
          if (cached && cachedAccepted) {
            return { source: "cache", value: cached.value };
          }
          throw error;
        }
        answered = true;
        await drawing;
        if (!input.isCurrent()) return null;
        input.onDefinitiveDenial?.();
        try {
          await options.dropCache(identity);
        } catch (dropError) {
          options.onCacheError?.(dropError, "drop");
        }
        throw error;
      }
      answered = true;
      await drawing;
      if (!input.isCurrent()) return null;

      await input.onSnapshot(value, "network");
      if (!input.isCurrent()) return null;
      // Kept for next time without holding up what waits on the answer.
      void Promise.resolve().then(() => keep(identity, value)).then((kept) => input.onNetworkKept?.(kept, value));
      return input.isCurrent() ? { source: "network", value } : null;
    },
    keep,
    /**
     * Keeps what is shown as the cached copy, again a while later each time
     * the store refuses it, until it is kept (`onKept`) or `stop` is called.
     */
    keepUntilKept(identity: LocalFirstSnapshotIdentity, value: Value, onKept: () => void): () => void {
      let stopped = false;
      let timer: ReturnType<typeof setTimeout> | undefined;
      const attempt = async (wait: number) => {
        // A write that went through counts even when something newer came meanwhile.
        if (await keep(identity, value)) onKept();
        else if (!stopped) timer = setTimeout(() => void attempt(Math.min(wait * 2, KEEP_AGAIN_MOST_MS)), wait);
      };
      void attempt(KEEP_AGAIN_FIRST_MS);
      return () => {
        stopped = true;
        clearTimeout(timer);
      };
    },
  };

}
