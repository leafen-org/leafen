import {
  createOutboxBrowserRuntime,
  type OutboxBrowserRuntime,
  type OutboxPendingSettlement,
} from "./outbox";
import {
  createNativePageMutationLegacySource,
} from "./nativePageMutationLegacy";
import {
  createNativeLaneAnswers,
  createNativePageMutationOutbox,
  type NativeGraphLaneEvent,
  type NativeGraphMutation,
  type NativeGraphMutationDelivery,
  type NativeMetadataMutation,
  type NativePageCreation,
  type NativePageMutation,
  type NativePageMutationDelivery,
  type NativePageMutationLegacyPort,
  type NativePageMutationLegacyTurn,
  type NativePageMutationReplayTurn,
} from "./nativePageMutationOutbox";
import type { Page } from "@/workspaceMembershipWire.generated";
import { pageLeaving } from "./pageLeaving";
import { BOOTSTRAP_CACHE, markCacheBehind } from "./cacheStaleness";
import { serverScopedName } from "./serverOrigin";
import { isDeviceStoreLockWaitExceeded } from "./deviceStoreLock";
import { createNativePageLeavingChanges, type LeavingChangesStore, type LeavingPageChange } from "./nativePageMutationLeaving";
import { createNativePageMutationPastStore } from "./nativePageMutationPastStore";
import { createNativeGraphPagesTracker, type NativeGraphPagesWaiting } from "./nativeGraphPagesWaiting";
import { createNativePageTextChains } from "./nativePageTextChain";
import type { PagesWithKeptChanges } from "./nativeOutboxKeptPages";
import { createNativeKeptChangeReads } from "./nativePageMutationKeptReads";

const PAGE_MUTATION_TAB_ID = "native-page-mutations";

const NATIVE_PAGE_MUTATION_BOUNDS = {
  maxBaseCursor: Number.MAX_SAFE_INTEGER,
  maxContentCharacters: Number.MAX_SAFE_INTEGER,
  maxEntriesPerStep: 32,
  maxMutationIdCharacters: 128,
  maxPageIdCharacters: 128,
} as const;

const NATIVE_PAGE_MUTATION_RUNTIME_BOUNDS = {
  coordinator: { maxMutationsPerBatch: 32 },
  database: {
    maxDatabaseNameCharacters: 256,
    storage: {
      maxEntriesPerStep: 32,
      maxEntryKeyCharacters: 192,
      maxMutationsPerBatch: 32,
      maxTabIdCharacters: 64,
      maxTabIdsPerStep: 32,
    },
  },
  // As many parts of the screen as listen (2026-10-13: a ninth threw).
  invalidation: { maxListeners: Number.MAX_SAFE_INTEGER },
  maxUserIdCharacters: 64,
  operation: {
    maxEntries: 32,
    maxEntryKeyCharacters: 192,
    maxNestingDepth: 8,
    maxOperationItems: 32,
  },
  pending: {
    maxActiveUsers: 1,
    maxEntryKeyCharacters: 192,
    maxListeners: Number.MAX_SAFE_INTEGER,
    maxUserIdCharacters: 64,
  },
} as const;

/** What a page's editor needs of the outbox to keep its structure changes. */
export type NativeGraphMutationOutbox = Pick<
  NativePageMutationBrowser<unknown>,
  "admitAndSendGraph" | "graphPagesWaiting" | "keepGraph" | "pendingGraphOperations" | "pendingGraphWrites" | "subscribeGraphLane"
>;

export interface NativePageMutationBrowser<TResult> {
  admitAndSend(input: NativePageMutation): Promise<NativePageMutationDelivery<TResult>>;
  /** Keeps a page's structure changes and sends them after every older change kept. */
  admitAndSendGraph(input: NativeGraphMutation, signal?: AbortSignal): Promise<NativeGraphMutationDelivery>;
  /** Keeps a page's structure changes on the device as they are made, to be sent by admitAndSendGraph in turn. */
  keepGraph(input: NativeGraphMutation): Promise<void>;
  /** Keeps a page's settings change on the device as it is made, to be sent by admitAndSendMetadata in turn. */
  keepMetadata(input: NativeMetadataMutation): Promise<void>;
  /** Keeps a page's settings change (title, icon, …) and sends it after every older change kept. */
  admitAndSendMetadata(input: NativeMetadataMutation, signal?: AbortSignal): Promise<NativeGraphMutationDelivery>;
  /**
   * Keeps a page made here on the device before it opens, and says whether it
   * could: a read of the page waits until the server made it.
   */
  keepPageCreation(input: NativePageCreation): Promise<boolean>;
  /** Sends a page made here, kept with keepPageCreation, after every older change kept. */
  admitAndSendPageCreation(input: NativePageCreation, signal?: AbortSignal): Promise<NativeGraphMutationDelivery>;
  /** The pages made here the server doesn't have yet: what a page list read from it is shown with. */
  pendingNewPages(): Promise<Page[]>;
  graphPagesWaiting: NativeGraphPagesWaiting;
  /**
   * Holds what asks the server about a page made here until this outbox made
   * it (pagesNotYetMade), from when the outbox is the one in use until the
   * returned function or dispose: one made and never used holds nothing.
   */
  tellPagesNotYetMade(): () => void;
  /** The structure changes kept for a page, oldest first, but those of the writes named `shown`. */
  pendingGraphOperations(pageId: string, shown?: readonly string[]): Promise<unknown[]>;
  /** The writes of structure changes kept for a page, oldest first, each with its id. */
  pendingGraphWrites(pageId: string): Promise<{ mutationId: string; operations: unknown[] }[]>;
  /** The settings changes kept for each page, oldest first: what a page list read from the server is shown with until they go. */
  pendingPageMetadata(): Promise<Map<string, NativeMetadataMutation[]>>;
  /** The pages whose changes this device keeps, of any app's format, or "every" when that can't be read now (nativeOutboxKeptPages.ts). */
  pagesWithKeptChanges(): Promise<PagesWithKeptChanges>;
  subscribeGraphLane(listener: (event: NativeGraphLaneEvent) => void): () => void;
  clearEmptyUnderCustody(): Promise<void>;
  discardAll(): Promise<void>;
  dispose(): Promise<void>;
  hasPending(): Promise<boolean>;
  migrateLegacy(): Promise<NativePageMutationLegacyTurn>;
  readPendingCount(): Promise<number>;
  replay(): Promise<NativePageMutationReplayTurn>;
  resetCustody(): Promise<void>;
  runCustodyExclusive<T>(work: () => Promise<T>): Promise<T>;
  subscribeChanges(listener: () => void): () => void;
  subscribePending(listener: (
    pendingCount: number,
    settlement?: OutboxPendingSettlement,
  ) => void): () => void;
}

type NativePageMutationOpenResult<TResult> =
  | { kind: "network-only" }
  | { kind: "unavailable" }
  | {
      kind: "ready";
      legacy: NativePageMutationLegacyPort;
      owner: ReturnType<typeof createNativePageMutationOutbox<TResult>>;
      runtime: OutboxBrowserRuntime;
    };

export function createNativePageMutationOutboxBrowser<TResult>(options: {
  /** The cursor the server's answer to a page's text gave the page: the next text written here goes on it. */
  answeredCursor?: (result: TResult) => number | null | undefined;
  factory?: IDBFactory;
  send(input: NativePageMutation, signal?: AbortSignal): Promise<TResult>;
  sendGraph?: (input: NativeGraphMutation, signal?: AbortSignal) => Promise<unknown>;
  sendMetadata?: (input: NativeMetadataMutation, signal?: AbortSignal) => Promise<unknown>;
  sendPageCreation?: (input: NativePageCreation, signal?: AbortSignal) => Promise<unknown>;
  userId: string;
  /** Whether a change kept here that failed waits for the server rather than being refused (nativePageMutationFailure.ts graphWriteWaitsForServer). */
  waitsForServer?: (error: unknown) => boolean;
  /** Puts in the store, as it opens, what a tab gone kept as it was typed (typedDraftSender.ts recoverTypedDrafts). */
  recoverTyped?: (store: Pick<LeavingChangesStore, "keepGraph" | "keepMetadata" | "replay"> & { pendingGraph(): Promise<Map<string, NativeGraphMutation[]>> }) => Promise<void>;
}): NativePageMutationBrowser<TResult> {
  let disposed = false;
  // Each page's texts take turns however the store opens, or without it.
  const texts = createNativePageTextChains<TResult>(options.answeredCursor);
  // What the lane answered outlives an owner too, so no writer sends it twice.
  const answers = createNativeLaneAnswers();
  const sendGraph = options.sendGraph ?? (() => Promise.reject(new Error("Graph mutations have no transport.")));
  const sendMetadata = options.sendMetadata ?? (() => Promise.reject(new Error("Page settings changes have no transport.")));
  const sendPageCreation = options.sendPageCreation ?? (() => Promise.reject(new Error("Page creations have no transport.")));
  const tracker = createNativeGraphPagesTracker({
    async readKept() {
      const result = await currentOpening();
      if (result.kind !== "ready") return null;
      return { graph: await result.owner.pendingGraph(), made: await result.owner.pendingPageCreations() };
    },
    watchChanges: (onChange) => subscribeReady((runtime) => runtime.subscribeChanges(onChange)),
  });
  const { onGraphLane } = tracker;
  const leaving = createNativePageLeavingChanges({
    olderGraphUnsettled: tracker.olderGraphUnsettled,
    unansweredChanges: tracker.unansweredChanges,
    pagesUnanswered: tracker.pagesUnanswered,
    unsettle: tracker.unsettle,
    onGraphLane,
    sendGraph,
    sendMetadata,
    userId: options.userId,
    ...(options.waitsForServer ? { waitsForServer: options.waitsForServer } : {}),
  });
  const runtimeSubscriptions = new Set<{
    attach(runtime: OutboxBrowserRuntime): void;
    dispose(): void;
  }>();
  const open = (): Promise<NativePageMutationOpenResult<TResult>> => {
    let pending!: Promise<NativePageMutationOpenResult<TResult>>;
    pending = createOutboxBrowserRuntime({
      bounds: NATIVE_PAGE_MUTATION_RUNTIME_BOUNDS,
      ...(options.factory === undefined ? {} : { factory: options.factory }),
      onCustodyInvalidated: () => {
        if (!disposed && opening === pending) opening = open();
      },
      scope: serverScopedName(""),
      tabId: PAGE_MUTATION_TAB_ID,
      userId: options.userId,
    }).then((result) => {
      if (result.kind !== "ready") return result;
      const legacy = createNativePageMutationLegacySource(
        NATIVE_PAGE_MUTATION_BOUNDS,
        options.factory,
      );
      const owner = createNativePageMutationOutbox({
        bounds: NATIVE_PAGE_MUTATION_BOUNDS,
        onGraphLane,
        outbox: result.runtime,
        send: options.send,
        sendGraph,
        sendMetadata,
        sendPageCreation,
        texts,
        answers,
        ...(options.waitsForServer ? { waitsForServer: options.waitsForServer } : {}),
      });
      for (const subscription of runtimeSubscriptions) {
        subscription.attach(result.runtime);
      }
      // A change kept past the store as a page went is one kept before this tab opened (ownUnanswered), and so is a value a tab gone typed.
      const keptBefore = { keepMetadata: owner.keepMetadata, replay: owner.replay,
        keepGraph: (input: NativeGraphMutation) => owner.keepGraph(input).then((kept) => { if (kept) tracker.keptBefore(input); return kept; }) };
      void leaving.moveInto(keptBefore);
      void options.recoverTyped?.({ ...keptBefore, pendingGraph: owner.pendingGraph }).catch(() => undefined);
      return { ...result, legacy, owner };
    });
    // Another tab kept the store past the wait (deviceStoreLock.ts): it is
    // asked for again next time.
    void pending.catch((error: unknown) => {
      if (!isDeviceStoreLockWaitExceeded(error)) return;
      if (opening === pending) opening = null;
      if (!pageLeaving()) void leaving.sendPastStore();
    });
    return pending;
  };
  let opening: Promise<NativePageMutationOpenResult<TResult>> | null = null;
  const currentOpening = () => opening ??= open();
  // A change made meanwhile goes straight to the server, as without a store,
  // and is kept past it when the server can't take it (`held`).
  const openingOrServer = () => currentOpening().catch((error: unknown) => {
    if (isDeviceStoreLockWaitExceeded(error)) return { held: true, kind: "network-only" } as const;
    throw error;
  });
  const pastStore = createNativePageMutationPastStore({
    leaving,
    openStore: async () => {
      const result = await currentOpening();
      return result.kind === "ready" ? result.owner : null;
    },
    onGraphLane,
    waitsForServer: options.waitsForServer ?? (() => true),
  });
  /**
   * Sends a page's structure or settings change through the store, or past
   * it while another tab keeps it; one neither the store nor the server can
   * take now is kept past the store (nativePageMutationPastStore.ts). Without
   * a store here (the online-only choice) it goes as it is or fails.
   */
  const sendKeeping = async (change: LeavingPageChange, signal?: AbortSignal) => {
    let keepsHere = false;
    const attempt = async (): Promise<NativeGraphMutationDelivery> => {
      const result = await openingOrServer();
      keepsHere = result.kind === "ready" || "held" in result;
      if (result.kind === "ready") {
        return change.kind === "graph"
          ? result.owner.admitAndSendGraph(change.input, signal)
          : result.owner.admitAndSendMetadata(change.input, signal);
      }
      const sent = change.kind === "graph" ? sendGraph(change.input, signal) : sendMetadata(change.input, signal);
      return { result: await sent, state: "committed" };
    };
    try {
      return await attempt();
    } catch (error) {
      if (!keepsHere) throw error;
      return pastStore.keep(change, error, attempt);
    }
  };

  const requireAvailable = () => new Error(
    "Browser outbox durable storage is unavailable.",
  );
  const subscribeReady = (
    subscribe: (runtime: OutboxBrowserRuntime) => () => void,
  ) => {
    let active = true;
    let runtime: OutboxBrowserRuntime | null = null;
    let unsubscribe: (() => void) | null = null;
    const subscription = {
      attach(nextRuntime: OutboxBrowserRuntime) {
        if (!active || runtime === nextRuntime) return;
        unsubscribe?.();
        runtime = nextRuntime;
        unsubscribe = subscribe(nextRuntime);
      },
      dispose() {
        if (!active) return;
        active = false;
        runtime = null;
        unsubscribe?.();
        unsubscribe = null;
        runtimeSubscriptions.delete(subscription);
      },
    };
    runtimeSubscriptions.add(subscription);
    void currentOpening().then((result) => {
      if (!active || result.kind !== "ready") return;
      subscription.attach(result.runtime);
    }, () => {
      // The initial durable count read reports the same opening failure to the
      // visible projection; subscriptions cannot publish an asynchronous error.
    });
    return subscription.dispose;
  };

  // The store as opened, or null while another tab keeps it.
  const openedOrHeld = () => currentOpening().catch((error: unknown) => {
    if (isDeviceStoreLockWaitExceeded(error)) return null;
    throw error;
  });
  // What this device keeps, in the store and past it, read for the screen (nativePageMutationKeptReads.ts).
  const keptReads = createNativeKeptChangeReads({
    currentOpening, openedOrHeld, leaving, maxEntriesPerStep: NATIVE_PAGE_MUTATION_BOUNDS.maxEntriesPerStep,
  });

  return {
    // A store this browser can't keep (no IndexedDB, or its key couldn't be
    // made) keeps nothing, as the online-only choice: a change goes straight
    // to the server, and nothing waits here (2026-10-26: such a page refused
    // the change and said the connection was lost).
    async admitAndSend(input) {
      const result = await openingOrServer();
      if (result.kind === "ready") return result.owner.admitAndSend(input);
      const went = await texts.send(input, (text) => options.send(text), true);
      return went ? { result: went.result, state: "committed" } : { state: "superseded" };
    },
    // As the page goes there is no time to keep a change in the store: it
    // goes straight out, in a request that outlives the page (pageLeaving) —
    // unless an older change to the page hasn't reached the server. Sent
    // ahead of it, it would be overwritten when that one is sent again after
    // the reload, and not sent again itself, the server having it already
    // (2026-11-07: a new row's title typed and entered just before a reload
    // was overwritten by the row's making, its title empty). Then it is kept
    // for the reload, after the older one — as the changes on their way are
    // as it goes (nativePageMutationLeaving.ts leaveOnItsWay): written down
    // too should it not arrive, and older for the next one the page makes as
    // it goes, unanswered meanwhile — and goes with the older ones, as one
    // change, when they can go so (nativeLeavingTogether.ts).
    async admitAndSendGraph(input, signal) {
      if (pageLeaving()) {
        leaving.keep({ kind: "graph", input });
        if (tracker.olderGraphUnsettled(input)) { leaving.goTogether(input); return { state: "kept" }; }
        tracker.unsettle(input);
        try {
          return { result: await sendGraph(input, signal), state: "committed" };
        } finally {
          tracker.settle(input);
        }
      }
      leaving.nowOnItsWay({ kind: "graph", input });
      tracker.unsettle(input);
      // Answered, or refused (thrown: the change is gone), it no longer waits; kept, it does.
      let kept = false;
      try {
        const delivery = await sendKeeping({ kind: "graph", input }, signal);
        kept = delivery.state === "kept";
        return delivery;
      } finally {
        if (!kept) tracker.settle(input);
        leaving.noLongerOnItsWay(input.mutationId);
      }
    },
    async keepGraph(input) {
      if (pageLeaving()) {
        leaving.keep({ kind: "graph", input });
        // Behind an older one not yet answered, it goes with it as admitAndSendGraph does.
        if (tracker.olderGraphUnsettled(input)) leaving.goTogether(input);
        return;
      }
      tracker.unsettle(input);
      leaving.nowOnItsWay({ kind: "graph", input });
      const result = await openingOrServer();
      if (result.kind !== "ready") return;
      // One another writer's turn sent already is answered, not kept again.
      if (!(await result.owner.keepGraph(input))) tracker.settle(input);
      leaving.noLongerOnItsWay(input.mutationId);
    },
    // Kept, a page's settings change stays on its way until admitAndSendMetadata hears it answered: as the page goes, the
    // page's changes not yet answered go as one (nativeLeavingTogether.ts), one made as it goes among them.
    async keepMetadata(input) {
      if (pageLeaving()) leaving.keep({ kind: "metadata", input });
      leaving.nowOnItsWay({ kind: "metadata", input });
      if (pageLeaving()) return;
      const result = await openingOrServer();
      if (result.kind === "ready") await result.owner.keepMetadata(input);
    },
    async admitAndSendMetadata(input, signal) {
      // The lists' cached copy shows the page as it was until the answer is kept there (cacheStaleness).
      markCacheBehind(options.userId, BOOTSTRAP_CACHE, Date.now());
      if (pageLeaving()) return { result: await sendMetadata(input, signal), state: "committed" };
      leaving.nowOnItsWay({ kind: "metadata", input });
      try {
        return await sendKeeping({ kind: "metadata", input }, signal);
      } finally {
        leaving.noLongerOnItsWay(input.mutationId);
      }
    },
    async keepPageCreation(input) {
      const result = await openingOrServer();
      if (result.kind !== "ready") return false;
      // Made already by another writer's turn, it isn't kept again: its sending hears the answer.
      if (!(await result.owner.keepPageCreation(input))) return false;
      tracker.creationKept(input.pageId);
      return true;
    },
    async admitAndSendPageCreation(input, signal) {
      const result = await openingOrServer();
      if (result.kind !== "ready") return { result: await sendPageCreation(input, signal), state: "committed" };
      const delivery = await result.owner.admitAndSendPageCreation(input, signal).catch((error: unknown) => {
        tracker.creationAnswered(input.pageId);
        throw error;
      });
      if (delivery.state !== "kept") tracker.creationAnswered(input.pageId);
      return delivery;
    },
    async clearEmptyUnderCustody() {
      const result = await currentOpening();
      if (result.kind === "ready") return result.runtime.clearEmptyUnderCustody();
      throw requireAvailable();
    },
    async discardAll() {
      leaving.discard();
      tracker.forgetCreations();
      const result = await currentOpening();
      if (result.kind === "ready") return result.runtime.discardAllEntries();
    },
    async dispose() {
      disposed = true;
      tracker.dispose();
      pastStore.dispose();
      leaving.dispose();
      for (const subscription of [...runtimeSubscriptions]) subscription.dispose();
      if (!opening) return;
      const result = await opening;
      if (result.kind === "ready") await result.runtime.dispose();
    },
    graphPagesWaiting: tracker.pages,
    tellPagesNotYetMade: () => tracker.tellNotYetMade(),
    async hasPending() {
      const result = await currentOpening();
      if (result.kind === "ready") return result.runtime.hasAnyEntries();
      return false;
    },
    async migrateLegacy() {
      // With the store held by another tab, that tab sends what it keeps.
      const result = await openingOrServer();
      if (result.kind === "ready") {
        return result.runtime.runReplayExclusive(() => result.owner.migrateLegacy(result.legacy));
      }
      return { hasMore: false, migrated: 0 };
    },
    pendingNewPages: keptReads.pendingNewPages,
    pendingPageMetadata: keptReads.pendingPageMetadata,
    pendingGraphOperations: keptReads.pendingGraphOperations,
    pendingGraphWrites: keptReads.pendingGraphWrites,
    pagesWithKeptChanges: keptReads.pagesWithKeptChanges,
    async readPendingCount() {
      const result = await currentOpening();
      if (result.kind === "ready") return result.runtime.durablePendingCount();
      return 0;
    },
    async replay() {
      const result = await openingOrServer();
      if (result.kind === "ready") return result.owner.replay();
      return { committed: 0, deferred: false, hasMore: false };
    },
    async resetCustody() {
      const previous = currentOpening();
      const result = await previous;
      if (result.kind === "ready") await result.runtime.dispose();
      if (!disposed && opening === previous) opening = open();
      await opening;
    },
    async runCustodyExclusive(work) {
      const result = await currentOpening();
      if (result.kind === "ready") return result.runtime.runCustodyExclusive(work);
      throw requireAvailable();
    },
    subscribeChanges(listener) {
      return subscribeReady((runtime) => runtime.subscribeChanges(listener));
    },
    subscribeGraphLane(listener) {
      return tracker.subscribeLane(listener);
    },
    subscribePending(listener) {
      return subscribeReady((runtime) => runtime.subscribePending(listener));
    },
  };
}
