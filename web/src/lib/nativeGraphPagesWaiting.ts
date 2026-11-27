import type {
  NativeGraphLaneEvent,
  NativeGraphMutation,
} from "./nativePageMutationOutbox";
import { tellPagesNotYetMade } from "./pagesNotYetMade";

/**
 * The pages whose structure changes the outbox keeps for the server, and the
 * pages made here it hasn't made yet: a read of such a page waits until they
 * went, so it doesn't take them off screen or find no page.
 */
export interface NativeGraphPagesWaiting {
  has(pageId: string): boolean;
  /** The page's graph changes kept or sent from this tab and not yet answered to it, oldest first. */
  unanswered(pageId: string): readonly string[];
  /**
   * Those, and the page's graph changes the outbox kept before this tab
   * opened — before its reload — and hasn't sent yet: what shows the page
   * here waits for them all before it draws the server's state of it.
   */
  ownUnanswered(pageId: string): readonly string[];
  /** Settles once the pages kept before this tab opened are known. */
  ready(): Promise<void>;
  subscribe(listener: () => void): () => void;
}

/** What the outbox keeps for the server, as one read of its store finds it. */
export interface NativeGraphKept {
  graph: Map<string, unknown[]>;
  made: { pageId: string }[];
}

/**
 * This tab's account of the pages that wait for the server (the graph lane's
 * kept pages and the pages made here not yet made), and of the order of a
 * page's graph changes not yet answered.
 */
export interface NativeGraphPagesTracker {
  pages: NativeGraphPagesWaiting;
  /** What the outbox tells of its graph lane: kept, drained, made or refused. */
  onGraphLane(event: NativeGraphLaneEvent): void;
  subscribeLane(listener: (event: NativeGraphLaneEvent) => void): () => void;
  unsettle(input: NativeGraphMutation): void;
  /**
   * The page's graph changes made in this tab and not yet answered, oldest
   * first, as they were made; null while changes kept before this tab
   * opened wait ahead of them (what goes together as the page goes,
   * nativeLeavingTogether.ts).
   */
  unansweredChanges(pageId: string): NativeGraphMutation[] | null;
  /** The pages that have such changes. */
  pagesUnanswered(): string[];
  /** A change kept before this tab opened, moved into the outbox as it opens (one kept past it as a page went). */
  keptBefore(input: Pick<NativeGraphMutation, "mutationId" | "pageId">): void;
  settle(input: NativeGraphMutation): void;
  /** Whether an older change to the page, kept or sent, hasn't been answered. */
  olderGraphUnsettled(input: NativeGraphMutation): boolean;
  creationKept(pageId: string): void;
  creationAnswered(pageId: string): void;
  forgetCreations(): void;
  /**
   * Tells what asks the server about a page that its making waits in this
   * outbox (pagesNotYetMade), until the returned function or dispose stops it.
   */
  tellNotYetMade(): () => void;
  dispose(): void;
}

/**
 * The data sources a graph change makes or changes (its upsertDataSource
 * steps), and the databases it makes or changes by their blocks, whose IDs
 * are their addresses (inlineDatabaseAddress.ts).
 */
function dataSourcesUpserted(operations: readonly unknown[]): string[] {
  const ids: string[] = [];
  for (const operation of operations) {
    const kind: unknown = operation && typeof operation === "object" ? Reflect.get(operation, "kind") : undefined;
    const value: unknown = kind === "upsertDataSource" ? Reflect.get(operation as object, "dataSource")
      : kind === "upsertBlock" ? Reflect.get(operation as object, "block") : undefined;
    if (!value || typeof value !== "object" || (kind === "upsertBlock" && Reflect.get(value, "type") !== "inline_database")) continue;
    const id: unknown = Reflect.get(value, "id");
    if (typeof id === "string" && id) ids.push(id);
  }
  return ids;
}

export function createNativeGraphPagesTracker(options: {
  /** Reads what the outbox keeps, or null when there is no store to read. */
  readKept(): Promise<NativeGraphKept | null>;
  /** Watches the outbox's changes; the returned function stops it. */
  watchChanges(onChange: () => void): () => void;
}): NativeGraphPagesTracker {
  let disposed = false;
  // The pages with kept structure changes, as this tab knows them: a kept
  // write adds its page and a drained lane clears them, both under the
  // replay lock; a read of the outbox (on open, or after another tab
  // changed it) adds what it finds and drops what it no longer finds.
  const waiting = new Set<string>();
  // Pages made here whose making the server hasn't answered — in this tab,
  // or kept before it opened or by another tab, as a read of the outbox
  // finds them: they wait until it is made or refused, not only until a lane
  // drains, which a step listed before the page was kept could tell
  // meanwhile. What asks the server about one waits for it (pagesNotYetMade).
  const creating = new Set<string>();
  const waitingListeners = new Set<() => void>();
  const laneListeners = new Set<(event: NativeGraphLaneEvent) => void>();
  let drains = 0;
  let creationsAnswered = 0;
  let waitingRead: Promise<void> | null = null;
  let rereadTimer: ReturnType<typeof setTimeout> | null = null;
  // A page's graph changes kept or sent from this tab and not yet answered,
  // in the order made: what a change made as the page goes may not overtake.
  const unsettledGraph = new Map<string, string[]>();
  // Those changes as made, by their IDs.
  const unsettledChanges = new Map<string, NativeGraphMutation>();
  // A page's graph changes the outbox kept before this tab opened, as its
  // first read of the outbox found them, until each is answered, the lane
  // drains or a read no longer finds it (another tab sent it). Kept before a
  // reload they are this tab's own: sent one by one as the server answered
  // again, a page read between two of them drew the server's state between,
  // a row trashed then brought back gone for a moment (2026-11-25).
  const keptBeforeOpening = new Map<string, string[]>();
  let openingRead = true;
  // The changes answered while that first read is under way, which it may still find.
  let answeredWhileOpening: Set<string> | null = null;
  // The data sources each graph change kept or sent and not yet answered
  // makes or changes, and its databases by their addresses, by the change:
  // what asks the server about one waits until they are answered
  // (pagesNotYetMade), a database made here being one the server doesn't
  // have till then (2026-11-14: "+ 속성 추가" opened on a database just made
  // asked for its ID property, refused with a 404; 2026-11-18: a full-page
  // database made from "/" and reloaded at once, its address asked for
  // before the outbox sent it, opened "페이지 찾지 못함" for good).
  const sourcesOfChange = new Map<string, readonly string[]>();
  const sourceChanging = (dataSourceId: string) => {
    for (const sources of sourcesOfChange.values()) if (sources.includes(dataSourceId)) return true;
    return false;
  };
  let sourceChangesAnswered = 0;
  const sourceChangeAnswered = (mutationId: string) => {
    if (!sourcesOfChange.delete(mutationId)) return false;
    sourceChangesAnswered += 1;
    return true;
  };
  const unsettle = (input: NativeGraphMutation) => {
    const kept = unsettledGraph.get(input.pageId) ?? [];
    if (!kept.includes(input.mutationId)) kept.push(input.mutationId);
    unsettledGraph.set(input.pageId, kept);
    unsettledChanges.set(input.mutationId, input);
    const sources = dataSourcesUpserted(input.operations);
    if (sources.length && !sourcesOfChange.has(input.mutationId)) sourcesOfChange.set(input.mutationId, sources);
  };
  // A change answered — taken or refused, as the lane answers it, or as its
  // writer hears — is no longer this tab's unanswered change to its page,
  // and what waits for the page's changes is told so: a read the page's room
  // asked for waits for them ("own", nativeContentGraphWriteCoordinator.ts),
  // and one made outside the page's own writes (a sidebar database's icon or
  // name, a menu's change, sendContentGraphChange) never let it go, the open
  // page following no change after it until it was opened again (2026-11-17:
  // an icon given from a favorite never reached the page's database title).
  const settle = (input: Pick<NativeGraphMutation, "mutationId" | "pageId">) => {
    answeredWhileOpening?.add(input.mutationId);
    unsettledChanges.delete(input.mutationId);
    const kept = unsettledGraph.get(input.pageId) ?? [];
    const left = kept.filter((id) => id !== input.mutationId);
    if (left.length) unsettledGraph.set(input.pageId, left);
    else unsettledGraph.delete(input.pageId);
    const before = forgetKeptBefore(input.pageId, (id) => id !== input.mutationId);
    if (sourceChangeAnswered(input.mutationId) || left.length !== kept.length || before) announceWaiting();
  };
  const addKeptBefore = (pageId: string, mutationId: string) => {
    const kept = keptBeforeOpening.get(pageId) ?? [];
    if (kept.includes(mutationId)) return false;
    keptBeforeOpening.set(pageId, [...kept, mutationId]);
    return true;
  };
  // Keeps the page's changes kept before this tab opened that `stays`; whether any went.
  const forgetKeptBefore = (pageId: string, stays: (mutationId: string) => boolean) => {
    const kept = keptBeforeOpening.get(pageId);
    if (!kept) return false;
    const left = kept.filter(stays);
    if (left.length) keptBeforeOpening.set(pageId, left);
    else keptBeforeOpening.delete(pageId);
    return left.length !== kept.length;
  };
  const olderGraphUnsettled = (input: NativeGraphMutation) => {
    const kept = unsettledGraph.get(input.pageId) ?? [];
    const at = kept.indexOf(input.mutationId);
    return (at < 0 ? kept.length : at) > 0 || waiting.has(input.pageId);
  };
  const announceWaiting = () => {
    for (const listener of [...waitingListeners]) listener();
  };
  const onGraphLane = (event: NativeGraphLaneEvent) => {
    if (event.kind === "kept" && !waiting.has(event.pageId)) {
      waiting.add(event.pageId);
      announceWaiting();
    } else if (event.kind === "drained") {
      drains += 1;
      if (waiting.size > 0 || sourcesOfChange.size > 0 || unsettledGraph.size > 0 || keptBeforeOpening.size > 0) {
        unsettledGraph.clear();
        unsettledChanges.clear();
        keptBeforeOpening.clear();
        waiting.clear();
        sourcesOfChange.clear();
        announceWaiting();
      }
    } else if (event.kind === "answered") {
      settle(event);
    }
    // A page's settings refused say nothing of its making.
    if (event.kind === "made" || (event.kind === "rejected" && !event.settings)) {
      creationsAnswered += 1;
      if (creating.delete(event.pageId)) announceWaiting();
    }
    for (const listener of [...laneListeners]) listener(event);
  };

  const readWaiting = async () => {
    for (let attempt = 0; attempt < 3 && !disposed; attempt += 1) {
      const before = new Set(waiting);
      const madeBefore = new Set(creating);
      const drained = drains;
      const answered = creationsAnswered;
      const sourcesAnswered = sourceChangesAnswered;
      if (openingRead) answeredWhileOpening ??= new Set();
      const kept = await options.readKept();
      if (!kept) return;
      const pages = kept.graph;
      const made = kept.made;
      for (const { pageId } of made) pages.set(pageId, pages.get(pageId) ?? []);
      // A lane that drained, or a page made, meanwhile may have sent what was read.
      if (drains !== drained || creationsAnswered !== answered || sourceChangesAnswered !== sourcesAnswered) continue;
      let changed = false;
      // A page made here and kept before this read, no longer kept, was made by another tab.
      for (const pageId of madeBefore) {
        if (!made.some((creation) => creation.pageId === pageId)) changed = creating.delete(pageId) || changed;
      }
      for (const { pageId } of made) {
        if (!creating.has(pageId)) {
          creating.add(pageId);
          changed = true;
        }
      }
      for (const pageId of before) {
        if (!pages.has(pageId)) changed = waiting.delete(pageId) || changed;
      }
      for (const pageId of pages.keys()) {
        if (!waiting.has(pageId)) {
          waiting.add(pageId);
          changed = true;
        }
      }
      // A change making a data source kept before this tab opened or by
      // another tab waits as one made here; one no longer kept, nor on its
      // way from here, went.
      const keptChanges = new Set<string>();
      for (const changes of kept.graph.values()) {
        for (const change of changes) {
          if (!change || typeof change !== "object") continue;
          const mutationId: unknown = Reflect.get(change, "mutationId");
          const operations: unknown = Reflect.get(change, "operations");
          if (typeof mutationId !== "string" || !Array.isArray(operations)) continue;
          keptChanges.add(mutationId);
          const sources = dataSourcesUpserted(operations);
          if (sources.length && !sourcesOfChange.has(mutationId)) {
            sourcesOfChange.set(mutationId, sources);
            changed = true;
          }
        }
      }
      const onItsWay = new Set([...unsettledGraph.values()].flat());
      for (const mutationId of [...sourcesOfChange.keys()]) {
        if (!keptChanges.has(mutationId) && !onItsWay.has(mutationId)) changed = sourcesOfChange.delete(mutationId) || changed;
      }
      // The first read finds the changes kept before this tab opened; a later one, which went meanwhile.
      if (openingRead) {
        const answeredMeanwhile = answeredWhileOpening ?? new Set<string>();
        openingRead = false;
        answeredWhileOpening = null;
        for (const [pageId, changes] of kept.graph) {
          for (const change of changes) {
            const mutationId: unknown = change && typeof change === "object" ? Reflect.get(change, "mutationId") : undefined;
            if (typeof mutationId === "string" && !answeredMeanwhile.has(mutationId)) changed = addKeptBefore(pageId, mutationId) || changed;
          }
        }
      } else {
        for (const pageId of [...keptBeforeOpening.keys()]) changed = forgetKeptBefore(pageId, (id) => keptChanges.has(id)) || changed;
      }
      if (changed) announceWaiting();
      return;
    }
  };
  // Another tab may send what this one kept; once the outbox changes, read
  // it again while pages wait (at most every second). Watched once someone
  // waits on the pages, so making the outbox opens nothing.
  let unsubscribeChanges: (() => void) | null = null;
  const readAgain = () => {
    if ((waiting.size === 0 && creating.size === 0 && sourcesOfChange.size === 0 && keptBeforeOpening.size === 0)
      || rereadTimer !== null || disposed) return;
    rereadTimer = setTimeout(() => {
      rereadTimer = null;
      void readWaiting().catch(() => undefined);
    }, 1000);
  };
  const pages: NativeGraphPagesWaiting = {
    has: (pageId) => waiting.has(pageId) || creating.has(pageId),
    unanswered: (pageId) => [...unsettledGraph.get(pageId) ?? []],
    ownUnanswered: (pageId) => {
      const own = unsettledGraph.get(pageId) ?? [];
      return [...(keptBeforeOpening.get(pageId) ?? []).filter((id) => !own.includes(id)), ...own];
    },
    ready: () => waitingRead ??= readWaiting().catch(() => undefined),
    subscribe(listener) {
      waitingListeners.add(listener);
      if (!disposed && !unsubscribeChanges) {
        unsubscribeChanges = options.watchChanges(readAgain);
        // What another tab sent before this one watched is read again too.
        readAgain();
      }
      return () => waitingListeners.delete(listener);
    },
  };
  // Told only once the outbox is the one in use (tellNotYetMade), not as it
  // is made: one made for a screen drawn and then thrown away was told too,
  // never heard its lane, and held a page made here as not yet made after
  // the lane in use had made it — the lane's next change to that page waited
  // for it with every change behind it, and the reads waiting for this tab's
  // writes with them (2026-11-14: a page made just before a reload, then 29
  // table changes kept and none sent, the title typed in a peek not saved).
  let stopTellingNotYetMade = () => {};

  return {
    pages,
    onGraphLane,
    subscribeLane(listener) {
      laneListeners.add(listener);
      return () => laneListeners.delete(listener);
    },
    unsettle,
    unansweredChanges(pageId) {
      const own = unsettledGraph.get(pageId) ?? [];
      if ((keptBeforeOpening.get(pageId) ?? []).some((id) => !own.includes(id))) return null;
      const changes = own.map((id) => unsettledChanges.get(id));
      return changes.every((change): change is NativeGraphMutation => change !== undefined) ? changes : null;
    },
    pagesUnanswered: () => [...unsettledGraph.keys()],
    keptBefore(input) {
      if (!disposed && addKeptBefore(input.pageId, input.mutationId)) announceWaiting();
    },
    settle,
    olderGraphUnsettled,
    creationKept(pageId) {
      creating.add(pageId);
      announceWaiting();
    },
    creationAnswered(pageId) {
      if (creating.delete(pageId)) announceWaiting();
    },
    forgetCreations() {
      if (creating.size) {
        creating.clear();
        announceWaiting();
      }
    },
    tellNotYetMade() {
      stopTellingNotYetMade();
      if (disposed) return () => {};
      const stop = tellPagesNotYetMade({
        has: (pageId) => creating.has(pageId) || sourceChanging(pageId),
        hasDataSource: sourceChanging,
        ready: pages.ready,
        subscribe: pages.subscribe,
      });
      stopTellingNotYetMade = stop;
      return () => {
        stop();
        if (stopTellingNotYetMade === stop) stopTellingNotYetMade = () => {};
      };
    },
    dispose() {
      disposed = true;
      // What waits for a page made here asks the outbox that follows.
      stopTellingNotYetMade();
      announceWaiting();
      unsubscribeChanges?.();
      if (rereadTimer !== null) clearTimeout(rereadTimer);
      waitingListeners.clear();
      laneListeners.clear();
    },
  };
}
