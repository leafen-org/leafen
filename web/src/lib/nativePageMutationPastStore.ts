import type { LeavingChangesStore, LeavingPageChange, NativePageLeavingChanges } from "./nativePageMutationLeaving";
import type { NativeGraphLaneEvent, NativeGraphMutationDelivery } from "./nativePageMutationOutbox";

// A change made while another tab keeps this device's store (deviceStoreLock.ts)
// and the server can't take it is kept all the same: written down beside the
// store, as a change made as the page goes is (nativePageMutationLeaving.ts),
// and sent from there once the server can take it, or moved into the store
// once this tab can open it, whichever comes first — tried again a while
// later, less often each time, and at once as the browser comes back online
// (2026-11-19: with a tab left holding the store, a table pasted while the
// server was out of reach drew on screen and was gone once it came back).
// One that can't even be written down (no room) stays on screen, its save
// still pending, and is sent again until the store or the server takes it.

const AGAIN_FIRST_MS = 2_000;
const AGAIN_MOST_MS = 30_000;

export interface NativePageMutationPastStore {
  /**
   * What became of a change whose sending (`attempt`) failed with `error`:
   * kept past the store when the server can't take it now, `attempt` made
   * again meanwhile while it can't even be written down; a refusal throws.
   */
  keep(change: LeavingPageChange, error: unknown, attempt: () => Promise<NativeGraphMutationDelivery>): Promise<NativeGraphMutationDelivery>;
  dispose(): void;
}

/** The changes the store keeps, then those kept past it that it doesn't hold yet. */
export function keptPastStoreAfter<Change extends { mutationId: string }>(kept: readonly Change[], past: readonly Change[]): Change[] {
  return [...kept, ...past.filter((change) => !kept.some(({ mutationId }) => mutationId === change.mutationId))];
}

export function createNativePageMutationPastStore(options: {
  leaving: NativePageLeavingChanges;
  /** The store this tab has open, or null while another tab keeps it or there is none. */
  openStore(): Promise<LeavingChangesStore | null>;
  /** Hears a change kept nowhere ("unsaved"), once, to say so. */
  onGraphLane(event: NativeGraphLaneEvent): void;
  waitsForServer(error: unknown): boolean;
}): NativePageMutationPastStore {
  let disposed = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let wait = AGAIN_FIRST_MS;
  let trying: Promise<void> | null = null;
  const tryAgain = () => trying ??= (async () => {
    if (!options.leaving.hasKept()) return;
    const store = await options.openStore().catch(() => null);
    if (store) await options.leaving.moveInto(store);
    if (options.leaving.hasKept()) await options.leaving.sendPastStore();
  })().finally(() => { trying = null; });
  const later = (after: number) => {
    if (disposed || timer !== undefined) return;
    timer = setTimeout(() => {
      timer = undefined;
      void tryAgain().then(() => {
        if (!options.leaving.hasKept()) {
          wait = AGAIN_FIRST_MS;
          return;
        }
        wait = Math.min(wait * 2, AGAIN_MOST_MS);
        later(wait);
      });
    }, after);
  };
  const online = () => {
    wait = AGAIN_FIRST_MS;
    void tryAgain().then(() => { if (options.leaving.hasKept()) later(wait); });
  };
  if (typeof window !== "undefined") window.addEventListener("online", online);
  // What an earlier page left written down goes too.
  if (options.leaving.hasKept()) later(AGAIN_FIRST_MS);
  return {
    async keep(change, error, attempt) {
      for (let pause = AGAIN_FIRST_MS; ; pause = Math.min(pause * 2, AGAIN_MOST_MS)) {
        if (!options.waitsForServer(error)) throw error;
        if (await options.leaving.writeDown(change)) {
          later(AGAIN_FIRST_MS);
          return { state: "kept" };
        }
        if (disposed) throw error;
        if (pause === AGAIN_FIRST_MS) options.onGraphLane({ kind: "unsaved", pageId: change.input.pageId });
        await new Promise((resolve) => setTimeout(resolve, pause));
        try {
          return await attempt();
        } catch (next) {
          error = next;
        }
      }
    },
    dispose() {
      disposed = true;
      clearTimeout(timer);
      if (typeof window !== "undefined") window.removeEventListener("online", online);
    },
  };
}
