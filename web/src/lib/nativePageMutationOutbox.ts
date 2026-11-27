import {
  boundedIdentity, graphMutationOf, metadataMutationOf, pageCreationOf, validateGraphMutation, validateMetadataMutation, validatePageCreation,
  type NativeGraphLaneEvent,
  type NativeGraphMutation,
  type NativeGraphMutationDelivery,
  type NativeGraphMutationOperation,
  type NativeMetadataMutation,
  type NativeMetadataMutationOperation,
  type NativePageCreation,
  type NativePageCreationOperation,
} from "./nativePageLaneEntries";
import { createNativePageTextChains, type NativePageTextChains } from "./nativePageTextChain";
import { isPageBusyRefusal } from "./pageBusy";
import { formattedOutboxEntry, keptOutboxEntry } from "./nativeOutboxEntryFormat";
import { answerWaitMs } from "./serverAnswers";
import { createKeepsOnTheirWay, createNativeLaneAnswers, keepUnansweredWith, type NativeLaneAnswers } from "./nativeLaneKeeping";
import { createNativeLaneSending, MAX_LANE_STEPS } from "./nativeLaneSending";

export { createNativeLaneAnswers, type NativeLaneAnswers } from "./nativeLaneKeeping";

export type { NativeGraphLaneEvent, NativeGraphMutation, NativeGraphMutationDelivery, NativeMetadataMutation, NativePageCreation } from "./nativePageLaneEntries";

export interface NativePageMutation {
  baseCursor: number;
  content: string;
  createdAt: number;
  mutationId: string;
  pageId: string;
}

export interface NativePageMutationBounds {
  maxBaseCursor: number;
  maxContentCharacters: number;
  maxEntriesPerStep: number;
  /** The most structure changes one kept graph mutation holds (the wire's bound). */
  maxGraphOperations?: number;
  maxMutationIdCharacters: number;
  maxPageIdCharacters: number;
}

interface NativePageMutationReceipt {
  entryKey: string;
  generation?: number;
  tabId: string;
  updatedAt: number;
  userId: string;
}

interface NativePageMutationReplayItem {
  entry: {
    entryKey: string;
    seq: number;
    tabId: string;
    updatedAt: number;
    value: unknown;
  };
  receipt: NativePageMutationReceipt;
}

interface NativePageMutationOperation {
  /** When the text was written, which orders it among the page's texts (nativePageTextChain.ts). */
  createdAt?: number;
  id: string;
  kind: "page_update";
  mutationId: string;
  patch: { content: string; cursor: number };
  target: "page";
}

export interface NativePageMutationOutboxPort {
  listReplayStep(input: { afterSeq: number | null; limit: number }): Promise<{
    items: NativePageMutationReplayItem[];
    nextAfterSeq: number | null;
  }>;
  /** The sending lane; `taken` tells when another tab took it over (deviceStoreLock.ts). */
  runReplayExclusive<T>(work: (taken: AbortSignal) => Promise<T>): Promise<T>;
  /** Keeps a change in the format of this app (nativeOutboxEntryFormat.ts). */
  set(entryKey: string, operation: ReturnType<typeof formattedOutboxEntry>): Promise<NativePageMutationReceipt>;
  settle(receipt: NativePageMutationReceipt, outcome: "committed"): Promise<boolean>;
}

export interface NativePageMutationLegacyPort {
  listStep(input: { after: unknown; limit: number }): Promise<{
    mutations: NativePageMutation[];
    nextAfter: unknown;
  }>;
  remove(mutationId: string): Promise<void>;
}

export type NativePageMutationDelivery<TResult> =
  | { result: TResult; state: "committed" }
  | { error: unknown; state: "deferred" }
  /** A newer text of the page, written here meanwhile, replaced it: that one's answer tells. */
  | { state: "superseded" };

export interface NativePageMutationLegacyTurn {
  hasMore: boolean;
  migrated: number;
}

export interface NativePageMutationReplayTurn {
  committed: number;
  /** Changes the server refused for good, dropped (the lane told of each). */
  refused?: number;
  deferred: boolean;
  /** What waits, waits only for pages an operation holds for now (isPageBusyRefusal), not for the server. */
  busy?: boolean;
  hasMore: boolean;
  /** Entries this app can't read stay kept, for the app that wrote them; they wait for no server. */
  unreadable?: boolean;
}

function assertPositiveBound(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

function validateBounds(bounds: NativePageMutationBounds): void {
  for (const [label, value] of Object.entries(bounds)) {
    assertPositiveBound(value, label);
  }
}

function validateMutation(
  input: NativePageMutation,
  bounds: NativePageMutationBounds,
): NativePageMutation {
  if (
    !input
    || !Number.isSafeInteger(input.baseCursor)
    || input.baseCursor < 0
    || input.baseCursor > bounds.maxBaseCursor
    || !Number.isSafeInteger(input.createdAt)
    || input.createdAt < 0
    || typeof input.content !== "string"
    || input.content.length > bounds.maxContentCharacters
  ) {
    throw new RangeError("Page mutation exceeds its configured scalar bounds.");
  }
  return {
    baseCursor: input.baseCursor,
    content: input.content,
    createdAt: input.createdAt,
    mutationId: boundedIdentity(
      input.mutationId,
      bounds.maxMutationIdCharacters,
      "mutationId",
    ),
    pageId: boundedIdentity(input.pageId, bounds.maxPageIdCharacters, "pageId"),
  };
}

function operation(input: NativePageMutation): NativePageMutationOperation {
  return {
    createdAt: input.createdAt,
    id: input.pageId,
    kind: "page_update",
    mutationId: input.mutationId,
    patch: { content: input.content, cursor: input.baseCursor },
    target: "page",
  };
}

/** A page's text an outbox entry keeps (keptOutboxEntry), kept at `keptAt`. */
function replayMutation(
  value: Record<string, unknown>,
  keptAt: number,
  bounds: NativePageMutationBounds,
): NativePageMutation {
  const patch = value.patch && typeof value.patch === "object" && !Array.isArray(value.patch)
    ? value.patch as Record<string, unknown>
    : null;
  if (
    value.kind !== "page_update"
    || value.target !== "page"
    || typeof patch?.content !== "string"
    || typeof patch.cursor !== "number"
  ) {
    throw new RangeError("Durable page mutation has an unsupported operation shape.");
  }
  return validateMutation({
    baseCursor: patch.cursor,
    content: patch.content,
    // Kept before texts carried when they were written: when kept.
    createdAt: typeof value.createdAt === "number" ? value.createdAt : keptAt,
    mutationId: String(value.mutationId ?? ""),
    pageId: String(value.id ?? ""),
  }, bounds);
}

// A change just made, kept here already, waits for the server's answer only so
// long, or a few of its round trips from far away (serverAnswers.ts
// answerWaitMs: 2026-11-18, a server 5 s away was taken as silent, its new
// rows kept and the top bar saying "오프라인"), and once the server stayed
// silent the next ones don't wait for it at all for a while: they stay kept,
// shown as a server gone would leave them (2026-11-05: on a network that
// stopped without failing a table's new row stayed off the screen for 45
// seconds and its name could go with it). A server that answers late takes
// the change again by its mutation.
const SILENT_SERVER_WAIT_MS = 6_000;
const SILENT_SERVER_REMEMBERED_MS = 15_000;

export function createNativePageMutationOutbox<TResult>(options: {
  bounds: NativePageMutationBounds;
  /** Hears the lane of graph mutations drain or the server refuse one. */
  onGraphLane?: (event: NativeGraphLaneEvent) => void;
  outbox: NativePageMutationOutboxPort;
  send(input: NativePageMutation, signal?: AbortSignal): Promise<TResult>;
  sendGraph?: (input: NativeGraphMutation, signal?: AbortSignal) => Promise<unknown>;
  sendMetadata?: (input: NativeMetadataMutation, signal?: AbortSignal) => Promise<unknown>;
  sendPageCreation?: (input: NativePageCreation, signal?: AbortSignal) => Promise<unknown>;
  /** The turns of each page's texts, which outlive this owner (nativePageTextChain.ts). */
  texts?: NativePageTextChains<TResult>;
  /** What the lane's turns answered, which outlives this owner (NativeLaneAnswers). */
  answers?: NativeLaneAnswers;
  /** Whether a change of any kind kept here that failed waits for the server rather than being refused. */
  waitsForServer?: (error: unknown) => boolean;
}) {
  validateBounds(options.bounds);
  const texts = options.texts ?? createNativePageTextChains<TResult>();
  const answers = options.answers ?? createNativeLaneAnswers();
  const keepsOnTheirWay = createKeepsOnTheirWay<NativePageMutationReceipt>();
  const sendGraph = options.sendGraph ?? (() => Promise.reject(new Error("Graph mutations have no transport.")));
  const sendMetadata = options.sendMetadata ?? (() => Promise.reject(new Error("Page settings changes have no transport.")));
  const sendPageCreation = options.sendPageCreation ?? (() => Promise.reject(new Error("Page creations have no transport.")));
  const waitsForServer = options.waitsForServer ?? (() => true);
  let silentUntil = 0;
  const serverSilent = () => Date.now() < silentUntil;
  /** Sends a change just made under the wait, and its caller's signal if it has one. */
  const answeredInTime = async <T>(send: (signal: AbortSignal) => Promise<T>, signal?: AbortSignal): Promise<T> => {
    const wait = AbortSignal.timeout(answerWaitMs(SILENT_SERVER_WAIT_MS));
    try {
      const answer = await send(signal ? AbortSignal.any([signal, wait]) : wait);
      silentUntil = 0;
      return answer;
    } catch (error) {
      if (wait.aborted && !signal?.aborted) silentUntil = Date.now() + SILENT_SERVER_REMEMBERED_MS;
      throw error;
    }
  };
  const graphLane = (event: NativeGraphLaneEvent) => {
    try {
      options.onGraphLane?.(event);
    } catch {
      // A listener never changes what the lane did.
    }
  };

  /** A lane entry as it is sent, null for a page's text, or "unreadable". */
  const laneEntryOf = (value: unknown) => {
    const metadata = metadataMutationOf(value, options.bounds);
    const creation = metadata ? null : pageCreationOf(value, options.bounds);
    const graph = metadata || creation ? null : graphMutationOf(value, options.bounds);
    if (metadata === "unreadable" || creation === "unreadable" || graph === "unreadable") return "unreadable";
    if (metadata) return { kind: "metadata", pageId: metadata.pageId, send: (signal?: AbortSignal) => sendMetadata(metadata, signal) } as const;
    if (creation) return { kind: "creation", pageId: creation.pageId, send: (signal?: AbortSignal) => sendPageCreation(creation, signal) } as const;
    if (graph) return { kind: "graph", mutationId: graph.mutationId, pageId: graph.pageId, send: (signal?: AbortSignal) => sendGraph(graph, signal) } as const;
    return null;
  };

  /**
   * Sends what the outbox keeps, oldest first, under the replay lock: a
   * page's text, and the lane's entries — pages made here, graph mutations
   * and settings changes — in the order made. Every kind is judged by one
   * rule (waitsForServer): one the server can't take now keeps every later
   * one of its line waiting (a page's text the texts, the lane the lane);
   * one it refused for good is dropped, its writer hears it, and with no
   * writer here the lane tells of it (2026-11-13: an old page text the server
   * answered 404, its page gone, waited for good under "connection lost").
   * Stops at `target` once it went, and before the next entry once another
   * tab took the lane over (`taken`): that tab sends what is left, in its order.
   * One this app can't read (a newer app's, of a format, kind or shape it doesn't
   * know) stays kept for an app that can, holds nothing and waits for no
   * server: the turn isn't kept waiting by it, nor said to be (2026-11-13:
   * such an entry kept "connection lost" up while the server answered).
   * A `target` still on its way to the store (`targetGoing`), which may land after this step lists it, goes
   * from the store by its own keep and the drain is told, both by its sender (nativeLaneSending.ts).
   */
  async function publishStep(target: string | null, signal?: AbortSignal, afterSeq: number | null = null, taken?: AbortSignal, targetGoing = false) {
    // A store another tab keeps past the wait leaves the entry, which the
    // server takes again by its mutation: what it answered still stands.
    const settled = (receipt: NativePageMutationReceipt) => options.outbox.settle(receipt, "committed").catch(() => false);
    let committed = 0;
    let refused = 0;
    let unsettled = false;
    let pageWaiting = false;
    let graphWaiting = false;
    let heldOnly = true;
    let unreadable = false;
    let takenOver = false;
    let reached: NativeGraphMutationDelivery | { error: unknown; state: "rejected" } | null = null;
    const page = await options.outbox.listReplayStep({
      afterSeq,
      limit: options.bounds.maxEntriesPerStep,
    });
    if (page.items.length > options.bounds.maxEntriesPerStep) {
      throw new RangeError("Page-mutation replay exceeded its step bound.");
    }
    if (
      page.nextAfterSeq !== null
      && (page.items.length === 0 || !Number.isSafeInteger(page.nextAfterSeq))
    ) {
      throw new Error("Page-mutation replay continuation did not advance.");
    }
    for (const item of page.items) {
      if (taken?.aborted) {
        takenOver = true;
        break;
      }
      const ownTarget = item.entry.entryKey === target;
      // What it keeps as this app reads it: one of a format it doesn't know stays kept.
      const value = keptOutboxEntry(item.entry.value);
      const entry = value === "unreadable" ? value : laneEntryOf(value);
      if (entry === "unreadable") {
        unreadable = true;
        continue;
      }
      if (entry) {
        // What the lane tells of an entry's page, for a page made here or a
        // graph mutation, and of a settings change only its refusal.
        const tell = (event: NativeGraphLaneEvent) => {
          if (entry.kind !== "metadata") graphLane(event);
          else if (event.kind === "rejected") graphLane({ ...event, settings: true });
        };
        // One a turn answered already, kept again by its writer meanwhile, doesn't go again.
        const answered = answers.answered(item.entry.entryKey);
        if (answered) {
          unsettled ||= !(await settled(item.receipt)) && !(ownTarget && targetGoing);
          if (ownTarget) {
            reached = { result: answered.result, state: "committed" };
            break;
          }
          continue;
        }
        if (graphWaiting) {
          if (ownTarget) {
            tell({ kind: "kept", pageId: entry.pageId });
            reached = { state: "kept" };
            break;
          }
          continue;
        }
        let result: unknown;
        try {
          // Under the wait a change just made goes under, the replay too: one unanswered waits for the server, kept.
          result = await answeredInTime((bounded) => entry.send(bounded), ownTarget ? signal : undefined);
        } catch (error) {
          if ((ownTarget && signal?.aborted) || waitsForServer(error)) {
            graphWaiting = true;
            heldOnly &&= !(ownTarget && signal?.aborted) && isPageBusyRefusal(error);
            if (ownTarget) {
              tell({ kind: "kept", pageId: entry.pageId });
              reached = { state: "kept" };
              break;
            }
            continue;
          }
          unsettled ||= !(await settled(item.receipt)) && !(ownTarget && targetGoing);
          if (entry.kind === "graph") graphLane({ kind: "answered", mutationId: entry.mutationId, pageId: entry.pageId, refused: true });
          // The writer hears its own refusal as an error.
          if (ownTarget) {
            reached = { error, state: "rejected" };
            break;
          }
          refused += 1;
          tell({ error, kind: "rejected", pageId: entry.pageId, written: value });
          continue;
        }
        answers.remember(item.entry.entryKey, result);
        unsettled ||= !(await settled(item.receipt)) && !(ownTarget && targetGoing);
        committed += 1;
        if (!ownTarget && entry.kind === "graph" && result && typeof result === "object" && Reflect.get(result, "mergedBlockContent") === true) {
          graphLane({ kind: "merged", pageId: entry.pageId });
        }
        if (entry.kind === "creation") graphLane({ kind: "made", pageId: entry.pageId });
        // What waits for a row this mutation makes goes now, before the next entry (rowMadeHere.ts).
        if (entry.kind === "graph") graphLane({ kind: "answered", mutationId: entry.mutationId, pageId: entry.pageId });
        if (ownTarget) {
          reached = { result, state: "committed" };
          break;
        }
        continue;
      }
      if (pageWaiting) continue;
      let text: NativePageMutation | null = null;
      let went: { result: TResult } | null;
      try {
        text = replayMutation(value as Record<string, unknown>, item.entry.updatedAt, options.bounds);
        went = await texts.send(text, (next) => answeredInTime((bounded) => options.send(next, bounded)));
      } catch (error) {
        // One that can't be read stays kept, as the lane's.
        if (!text) {
          unreadable = true;
          continue;
        }
        if (waitsForServer(error)) {
          pageWaiting = true;
          heldOnly &&= isPageBusyRefusal(error);
          continue;
        }
        unsettled ||= !(await settled(item.receipt));
        refused += 1;
        graphLane({ error, kind: "rejected", pageId: text.pageId, written: value });
        continue;
      }
      unsettled ||= !(await settled(item.receipt));
      // One a newer text of its page replaced goes with nothing to read back.
      if (went) committed += 1;
    }
    const last = !takenOver && (reached === null || page.items.at(-1)?.entry.entryKey === target);
    const graphDrained = !graphWaiting && last && page.nextAfterSeq === null && !unsettled;
    if (graphDrained && !targetGoing) graphLane({ kind: "drained" });
    const deferred = pageWaiting || graphWaiting;
    return {
      drained: graphDrained,
      graphWaiting,
      nextAfterSeq: page.nextAfterSeq,
      reached,
      takenOver,
      unsettled,
      turn: {
        committed,
        refused,
        deferred,
        busy: deferred && heldOnly,
        hasMore: deferred || page.nextAfterSeq !== null || unsettled || takenOver,
        unreadable,
      } satisfies NativePageMutationReplayTurn,
    };
  }
  type KeptOperation = NativePageMutationOperation | NativeGraphMutationOperation | NativeMetadataMutationOperation | NativePageCreationOperation;
  // One kept again with the same content while on its way takes that keep (createKeepsOnTheirWay).
  const keepInFormat = (entryKey: string, entry: KeptOperation) => keepsOnTheirWay.keep(entryKey, entry, () => options.outbox.set(entryKey, formattedOutboxEntry(entry)));
  const admit = async (input: NativePageMutation) => {
    const validated = validateMutation(input, options.bounds);
    const entryKey = `native-page:${validated.mutationId}`;
    const receipt = await keepInFormat(entryKey, operation(validated));
    return { receipt, validated };
  };

  // An outbox entry keeps its place when written again under its key; one a
  // turn answered before or while it was kept waits for nothing (keepUnansweredWith).
  const keepUnanswered = keepUnansweredWith<NativePageMutationReceipt>(answers);
  // As validated: its words, typing, page kind and database start only where it has them.
  const keepGraphEntry = (validated: NativeGraphMutation) => keepInFormat(`native-graph:${validated.mutationId}`, {
    ...validated,
    causalityKeys: [`page:${validated.pageId}`],
    kind: "content_graph_mutation",
  });
  const keepMetadataEntry = (validated: NativeMetadataMutation) => keepInFormat(`native-metadata:${validated.mutationId}`, {
    ...validated,
    causalityKeys: [`page:${validated.pageId}`],
    kind: "page_metadata",
  });
  const keepPageCreationEntry = (validated: NativePageCreation) => keepInFormat(`native-page-create:${validated.mutationId}`, {
    ...validated,
    causalityKeys: [`page:${validated.pageId}`],
    kind: "page_create",
  });
  // A change just made goes out as it is kept, in its turn (nativeLaneSending.ts).
  const admitAndSendEntry = createNativeLaneSending<NativePageMutationReceipt>({
    answers, answeredInTime, graphLane, keepsOnTheirWay, outbox: options.outbox, publishStep, serverSilent, waitsForServer,
  });
  /** Every entry the outbox keeps, oldest first, read a step at a time to its end. */
  const eachKept = async (visit: (value: unknown) => void) => {
    let afterSeq: number | null = null;
    for (;;) {
      const page = await options.outbox.listReplayStep({
        afterSeq,
        limit: options.bounds.maxEntriesPerStep,
      });
      for (const item of page.items) visit(keptOutboxEntry(item.entry.value));
      // The steps go on by sequence; one that doesn't move on is the end.
      if (page.nextAfterSeq === null || page.items.length === 0
        || (afterSeq !== null && page.nextAfterSeq <= afterSeq)) break;
      afterSeq = page.nextAfterSeq;
    }
  };
  /** Kept entries of one kind, by page, oldest first. */
  const keptByPage = async <T extends { pageId: string }>(read: (value: unknown) => T | "unreadable" | null) => {
    const pages = new Map<string, T[]>();
    await eachKept((value) => {
      const entry = read(value);
      if (!entry || entry === "unreadable") return;
      const kept = pages.get(entry.pageId);
      if (kept) kept.push(entry);
      else pages.set(entry.pageId, [entry]);
    });
    return pages;
  };
  return {
    /**
     * Keeps a page's text and sends it in its turn among the page's texts
     * (nativePageTextChain.ts); throws when the server refuses it for good,
     * which drops it.
     */
    async admitAndSend(input: NativePageMutation): Promise<NativePageMutationDelivery<TResult>> {
      const { receipt, validated } = await admit(input);
      try {
        // One unanswered within the wait stays kept, as any change just made,
        // and the page's next texts don't wait behind it.
        const went = await texts.send(validated, (text) => answeredInTime((bounded) => options.send(text, bounded)), true);
        await options.outbox.settle(receipt, "committed");
        return went ? { result: went.result, state: "committed" } : { state: "superseded" };
      } catch (error) {
        if (waitsForServer(error)) return { error, state: "deferred" };
        await options.outbox.settle(receipt, "committed").catch(() => false);
        throw error;
      }
    },

    async migrateLegacy(source: NativePageMutationLegacyPort): Promise<NativePageMutationLegacyTurn> {
      let migrated = 0;
      const page = await source.listStep({
        after: null,
        limit: options.bounds.maxEntriesPerStep,
      });
      if (page.mutations.length > options.bounds.maxEntriesPerStep) {
        throw new RangeError("Legacy page-mutation source exceeded its step bound.");
      }
      if (page.nextAfter !== null && page.mutations.length === 0) {
        throw new Error("Legacy page-mutation continuation did not advance.");
      }
      for (const input of page.mutations) {
        const { validated } = await admit(input);
        await source.remove(validated.mutationId);
        migrated += 1;
      }
      return { hasMore: page.nextAfter !== null, migrated };
    },

    /**
     * Keeps a page's structure changes as they are made, before the changes
     * made ahead of them are answered, so a page that goes meanwhile still
     * has them on the device to send; admitAndSendGraph with the same
     * mutation later keeps them in the same place and sends them.
     */
    async keepGraph(input: NativeGraphMutation): Promise<boolean> {
      const validated = validateGraphMutation(input, options.bounds);
      return keepUnanswered(`native-graph:${validated.mutationId}`, () => keepGraphEntry(validated));
    },

    /**
     * Keeps a page's structure changes, then sends them after every older
     * change the outbox keeps. Throws when the server refuses them, which
     * drops them.
     */
    async admitAndSendGraph(input: NativeGraphMutation, signal?: AbortSignal): Promise<NativeGraphMutationDelivery> {
      const validated = validateGraphMutation(input, options.bounds);
      return admitAndSendEntry(`native-graph:${validated.mutationId}`, () => keepGraphEntry(validated),
        (bounded) => sendGraph(validated, bounded), () => graphLane({ kind: "kept", pageId: validated.pageId }),
        (refused) => graphLane({ kind: "answered", mutationId: validated.mutationId, pageId: validated.pageId, ...(refused ? { refused } : {}) }), signal);
    },

    /** Keeps a page's settings change as it is made, as keepGraph. */
    async keepMetadata(input: NativeMetadataMutation): Promise<boolean> {
      const validated = validateMetadataMutation(input, options.bounds);
      return keepUnanswered(`native-metadata:${validated.mutationId}`, () => keepMetadataEntry(validated));
    },

    /**
     * Keeps a page's settings change, then sends it after every older change
     * the outbox keeps, as a structure change. Throws when the server
     * refuses it, which drops it.
     */
    async admitAndSendMetadata(input: NativeMetadataMutation, signal?: AbortSignal): Promise<NativeGraphMutationDelivery> {
      const validated = validateMetadataMutation(input, options.bounds);
      return admitAndSendEntry(`native-metadata:${validated.mutationId}`, () => keepMetadataEntry(validated),
        (bounded) => sendMetadata(validated, bounded), () => undefined, () => undefined, signal);
    },

    /**
     * Keeps a page made here before it opens, so the changes made in it are
     * kept behind it; admitAndSendPageCreation then sends it in turn.
     */
    async keepPageCreation(input: NativePageCreation): Promise<boolean> {
      const validated = validatePageCreation(input, options.bounds);
      return keepUnanswered(`native-page-create:${validated.mutationId}`, () => keepPageCreationEntry(validated));
    },

    /** Sends a page made here after every older change kept, as a structure change. */
    async admitAndSendPageCreation(input: NativePageCreation, signal?: AbortSignal): Promise<NativeGraphMutationDelivery> {
      const validated = validatePageCreation(input, options.bounds);
      return admitAndSendEntry(`native-page-create:${validated.mutationId}`, () => keepPageCreationEntry(validated),
        (bounded) => sendPageCreation(validated, bounded), () => graphLane({ kind: "kept", pageId: validated.pageId }),
        (refused) => { if (!refused) graphLane({ kind: "made", pageId: validated.pageId }); }, signal);
    },

    /** The settings changes (title, icon, …) kept for each page, oldest first. */
    async pendingMetadata(): Promise<Map<string, NativeMetadataMutation[]>> {
      return keptByPage((value) => metadataMutationOf(value, options.bounds));
    },
    /** The graph mutations the outbox keeps, oldest first, by page. */
    async pendingGraph(): Promise<Map<string, NativeGraphMutation[]>> {
      return keptByPage((value) => graphMutationOf(value, options.bounds));
    },
    /** The pages made here the server doesn't have yet, oldest first. */
    async pendingPageCreations(): Promise<NativePageCreation[]> {
      return [...(await keptByPage((value) => pageCreationOf(value, options.bounds))).values()].flat();
    },

    /**
     * Sends what the outbox keeps, a step at a time, on past what stays kept
     * unread (a step after it starts behind it), until something waits for
     * the server or another tab takes the lane over.
     */
    replay(): Promise<NativePageMutationReplayTurn> {
      return options.outbox.runReplayExclusive(async (taken) => {
        let committed = 0, refused = 0, unsettled = false, unreadable = false;
        let afterSeq: number | null = null;
        for (let step = 1; ; step += 1) {
          const result = await publishStep(null, undefined, afterSeq, taken);
          const { turn } = result;
          committed += turn.committed;
          refused += turn.refused ?? 0;
          unsettled ||= result.unsettled;
          unreadable ||= turn.unreadable === true;
          if (turn.deferred || result.takenOver || result.nextAfterSeq === null || step >= MAX_LANE_STEPS) {
            // Past the steps' bound, another turn goes on only after one that sent something.
            const more = result.nextAfterSeq !== null && committed + refused > 0;
            return { ...turn, committed, refused, unreadable, hasMore: turn.deferred || result.takenOver || unsettled || more };
          }
          afterSeq = result.nextAfterSeq;
        }
      });
    },
  };
}
