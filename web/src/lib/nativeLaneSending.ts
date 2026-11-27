// How a page outbox owner (nativePageMutationOutbox.ts) sends a change just
// made: kept on the device as it goes, after every older change the outbox
// keeps, and answered to its writer.
import { isDeviceStoreLockWaitExceeded } from "./deviceStoreLock";
import type { NativeGraphLaneEvent, NativeGraphMutationDelivery } from "./nativePageLaneEntries";
import { watchKeep, type NativeLaneAnswers } from "./nativeLaneKeeping";

// How often a change asks for the lane again after other tabs took it over.
const MAX_LANE_TAKEOVERS = 8;
/** The most steps one turn of the lane reads. */
export const MAX_LANE_STEPS = 64;

interface LaneReceipt {
  entryKey: string;
}

/** What one step of the lane did (nativePageMutationOutbox.ts publishStep). */
interface LaneStep {
  drained: boolean;
  graphWaiting: boolean;
  nextAfterSeq: number | null;
  reached: NativeGraphMutationDelivery | { error: unknown; state: "rejected" } | null;
  takenOver: boolean;
}

export function createNativeLaneSending<Receipt extends LaneReceipt>(owner: {
  answers: NativeLaneAnswers;
  /** Sends a change just made under the wait the server is given (and its caller's signal). */
  answeredInTime: <T>(send: (signal: AbortSignal) => Promise<T>, signal?: AbortSignal) => Promise<T>;
  graphLane: (event: NativeGraphLaneEvent) => void;
  keepsOnTheirWay: { aheadOnTheirWay(entryKey: string): boolean };
  outbox: {
    listReplayStep(input: { afterSeq: number | null; limit: number }): Promise<{ items: { entry: { entryKey: string } }[] }>;
    runReplayExclusive<T>(work: (taken: AbortSignal) => Promise<T>): Promise<T>;
    settle(receipt: Receipt, outcome: "committed"): Promise<boolean>;
  };
  /** Sends what the lane keeps, a step at a time, up to `target` (publishStep). */
  publishStep(target: string, signal: AbortSignal | undefined, afterSeq: number | null, taken: AbortSignal, targetGoing: boolean): Promise<LaneStep>;
  serverSilent(): boolean;
  waitsForServer(error: unknown): boolean;
}) {
  // Another tab keeps the store past the wait (deviceStoreLock.ts), its lock or
  // its database, so what it keeps can't be read: the change goes out by
  // itself meanwhile. It stays kept until the server took it, so one refused
  // now, perhaps for an older change yet to go, or not answered, goes in turn
  // when the lane moves.
  const sendPastHeldLane = async (
    send: (signal: AbortSignal) => Promise<unknown>,
    receipt: Receipt,
    kept: () => void,
  ): Promise<NativeGraphMutationDelivery> => {
    let result: unknown;
    try {
      result = await owner.answeredInTime(send);
    } catch {
      kept();
      return { state: "kept" };
    }
    owner.answers.remember(receipt.entryKey, result);
    void owner.outbox.settle(receipt, "committed").catch(() => undefined);
    return { result, state: "committed" };
  };

  /**
   * Keeps a lane entry and sends it after every older change the outbox
   * keeps; kept() tells that it waits for the server, told() that the server
   * took or refused it. Throws when the server refuses it, which drops it.
   * It goes while it is being kept, from here (2026-11-19: a 100 x 300
   * table's change left 33 s after the paste, behind its megabyte's keep and
   * the read of it back, on a thread busy drawing the table): with nothing
   * in the lane ahead of it, as read before its keep is written — a read the
   * store is asked for first goes before a write asked for later — it isn't
   * read back; with older changes there, they go first and it follows, kept
   * or not yet. It waits for the server only once it is kept.
   */
  return async (
    entryKey: string,
    keep: () => Promise<Receipt>,
    send: (signal?: AbortSignal) => Promise<unknown>,
    kept: () => void,
    told: (refused: boolean) => void,
    signal?: AbortSignal,
  ): Promise<NativeGraphMutationDelivery> => {
    // Another writer's turn sent it already: its writer hears that answer.
    const answered = owner.answers.answered(entryKey);
    if (answered) return { result: answered.result, state: "committed" };
    const keeping = keep();
    const keepState = watchKeep(keeping);
    // Without room to keep it, the change goes out as it is or fails, as
    // it did before the outbox kept such changes.
    const sendUnkept = async (): Promise<NativeGraphMutationDelivery> => {
      const result = await send(signal);
      owner.answers.remember(entryKey, result);
      return { result, state: "committed" };
    };
    // Once kept it waits for the server; not kept, it goes as it is, or fails
    // as it failed already (`failed`) — the caller then keeps it past the store.
    const keptOrSent = (failed?: { error: unknown }) => keeping.then(
      (): NativeGraphMutationDelivery => (kept(), { state: "kept" }),
      () => (failed ? Promise.reject(failed.error) : sendUnkept()),
    );
    // One kept here before it and not in the store yet goes first: this one waits to be kept behind it.
    const behindOwn = owner.keepsOnTheirWay.aheadOnTheirWay(entryKey);
    if (owner.serverSilent() || behindOwn) await keeping.catch(() => undefined);
    if (keepState() === "failed") return sendUnkept();
    if (owner.serverSilent()) return keptOrSent();
    const nothingAhead = behindOwn ? null : owner.outbox.listReplayStep({ afterSeq: null, limit: 1 })
      .then(({ items }) => items.length === 0 || items[0].entry.entryKey === entryKey, () => false);
    // It waits for the server (`waits`), after failing to reach it (`waits.error`) or behind older changes that do.
    type Waits = { waits: { error: unknown } | null };
    const sendWhileKept = async (drained: boolean): Promise<NativeGraphMutationDelivery | Waits> => {
      let result: unknown;
      try {
        result = await owner.answeredInTime((bounded) => send(bounded), signal);
      } catch (error) {
        if (signal?.aborted || owner.waitsForServer(error)) return { waits: { error } };
        told(true);
        // Refused for good, it is dropped once kept, before the lane moves on.
        await keeping.then((receipt) => owner.outbox.settle(receipt, "committed")).catch(() => false);
        throw error;
      }
      owner.answers.remember(entryKey, result);
      told(false);
      if (drained) owner.graphLane({ kind: "drained" });
      void keeping.then((receipt) => owner.outbox.settle(receipt, "committed")).catch(() => undefined);
      return { result, state: "committed" };
    };
    // A change a step of the lane answered — taken, or refused for good — while
    // its keep was on its way to the store: the step listed the store before
    // that keep landed and dropped what it listed, so the change leaves the
    // store by its own keep once that lands, and the lane's drain is told then
    // (2026-11-24: a database's new property, kept as it was made and again
    // as it was sent, stayed in the store answered; its page counted as
    // waiting for the server for good, every read of the page waiting for it,
    // and "작업 데이터베이스로 전환" never closed).
    const dropKept = (drained: boolean) => keeping.then((receipt) => owner.outbox.settle(receipt, "committed"))
      .catch(() => false).then(() => { if (drained) owner.graphLane({ kind: "drained" }); });
    try {
      // Taken over by another tab meanwhile, the lane is asked for again:
      // that tab sends what the outbox keeps in its order, this change too
      // when it was kept before that tab's own, and this one goes on after.
      for (let turn = 0; turn < MAX_LANE_TAKEOVERS; turn += 1) {
        const delivery = await owner.outbox.runReplayExclusive(async (taken): Promise<NativeGraphMutationDelivery | Waits | null> => {
          // Not in the store yet, no other tab can have sent it: it goes without the store read back.
          if (turn === 0 && nothingAhead && (await nothingAhead) && keepState() === "going" && !taken?.aborted) return sendWhileKept(false);
          const going = keepState() !== "kept";
          // What a step leaves (sent and dropped, or a page's text that waits)
          // stays behind it, so the next step goes on after it.
          let afterSeq: number | null = null;
          for (let step = 0; step < MAX_LANE_STEPS; step += 1) {
            const { drained, graphWaiting, nextAfterSeq, reached, takenOver } = await owner.publishStep(entryKey, signal, afterSeq, taken, going);
            // Answered in the step while its keep was on its way, it goes from the store by that keep, the lane's drain told after.
            const dropped = going && (reached?.state === "committed" || reached?.state === "rejected") ? dropKept(drained) : null;
            if (reached?.state === "rejected") {
              await dropped;
              throw reached.error;
            }
            if (reached) return reached;
            if (takenOver) return null;
            if (graphWaiting) break;
            if (nextAfterSeq === null) return going || keepState() === "failed" ? sendWhileKept(drained) : { state: "published" };
            afterSeq = nextAfterSeq;
          }
          return { waits: null };
        });
        if (delivery && "waits" in delivery) return keptOrSent(delivery.waits ?? undefined);
        if (delivery) return delivery;
      }
      return keptOrSent();
    } catch (error) {
      if (!isDeviceStoreLockWaitExceeded(error)) throw error;
      const receipt = await keeping.catch(() => null);
      if (!receipt) return sendUnkept();
      return sendPastHeldLane((bounded) => send(signal ? AbortSignal.any([signal, bounded]) : bounded), receipt, kept);
    }
  };
}
