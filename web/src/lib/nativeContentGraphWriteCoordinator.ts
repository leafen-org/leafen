import {
  coalesceGraphOperation,
  graphOperationKey,
  graphOperationSubject,
  graphWireBounds,
  type GraphMutationReceipt,
  type GraphOperation,
} from "@/contentGraph";
import { afterNextPaint } from "@/lib/afterPress";
import { heldRead } from "@/lib/pageBusy";
import { noteBlockChangeSending } from "@/lib/blockChangesInFlight";
import type { BlockChangeText } from "@/lib/blockChangeText";
import { textRidingWith } from "@/lib/textBehindBlockChanges";
import { holdUntilTypingEnds, TYPING_PAUSE_MS } from "@/lib/typedWrites";
import { onTypedRowValueLetGo, sendTypedRowValuesNow, typedRowValueHeldOn } from "@/lib/rowValuesTyped";
import { typedGraphChange, type TypedGraphClaim } from "@/lib/typedDraftSender";
import { typedChangeKeeping } from "@/lib/typedGraphChanges";

/**
 * The pages whose writes wait for the server elsewhere (in the outbox): the
 * coordinator holds their reads as it holds those of its own unsaved writes.
 */
export type ContentGraphPagesWaiting = {
  has(pageId: string): boolean;
  /**
   * This tab's own changes to a page kept or sent and not yet answered, and
   * those the outbox kept before it opened (NativeGraphPagesWaiting).
   */
  ownUnanswered?(pageId: string): readonly string[];
  ready(): Promise<void>;
  subscribe(listener: () => void): () => void;
};

/**
 * Whose writes a read of a page waits for: every writer's the outbox keeps
 * (false), this tab's being written alone (true), or this tab's, those of
 * its the outbox keeps for a server that stayed silent and those it kept
 * before the tab opened — before a reload ("own"). What the page's room
 * tells of waits for the latter, read or drawn: read before them, the page
 * dropped what they made (2026-11-14: a new inline database kept behind a
 * change held past the outbox's wait went off the page, its start panel
 * with it, as the room told of that change, and came back once it was
 * saved), and drawn between two of them it showed the server's state
 * between (2026-11-25: a row trashed and brought back offline, the page
 * reloaded, went for a moment as the two were sent; a line moved twice
 * stood once moved).
 */
export type ReadSettledScope = boolean | "own";

type NativeContentGraphWriteCoordinatorOptions = {
  captureGeneration?: (pageId: string) => number;
  /**
   * A write kept for the server, which couldn't be reached ("kept": not saved
   * yet, not failed), or sent by another tab first ("sent").
   */
  onKept?: (pageId: string, operations: readonly GraphOperation[], generation: number, outcome: "kept" | "sent", mutationId: string) => void;
  pagesWaiting?: ContentGraphPagesWaiting;
  onFailure: (
    error: unknown,
    pageId: string,
    operations: readonly GraphOperation[],
    generation: number,
    /** Whether text written with the change went with it (textBehindBlockChanges.ts). */
    wordsRode: boolean,
  ) => Promise<void>;
  onQueued: (pageId: string, generation: number) => void;
  onSaved: (
    pageId: string,
    operations: readonly GraphOperation[],
    receipt: GraphMutationReceipt,
    generation: number,
  ) => void;
  /**
   * Keeps a write on the device as it is made, though the writes ahead of it
   * aren't answered yet: one waiting behind them was lost when the page went
   * meanwhile (2026-11-04: a table title typed, Enter, and a reload half a
   * second later kept only what an earlier save had sent). `write` sends it
   * later with the same mutation. Answers once it is kept.
   */
  keep?: (pageId: string, mutationId: string, operations: GraphOperation[], text?: BlockChangeText) => Promise<unknown> | void;
  /**
   * Resolves "kept" when the write waits for the server, "sent" when another
   * tab sent it. `text`, the page's text written with its block changes and
   * before them, goes with it (lib/textBehindBlockChanges.ts).
   */
  write: (
    pageId: string,
    mutationId: string,
    operations: GraphOperation[],
    signal: AbortSignal,
    text?: BlockChangeText,
  ) => Promise<GraphMutationReceipt | "kept" | "sent">;
};

type SettledWaiter = { here: ReadSettledScope; resolve(): void };

// Tells a change collected the change it goes in, then the save that takes it.
type BlockChangeSent = { named(mutationId: string): void; saved(save: Promise<boolean>): void };
const unnoted: BlockChangeSent = { named: () => undefined, saved: () => undefined };

function requiresDedicatedDatabaseMutation(operation: GraphOperation) {
  return operation.kind === "deleteDataSource" ||
    operation.kind === "deleteProperty" ||
    operation.kind === "deleteRow" || operation.kind === "restoreDateCommand";
}

export function createNativeContentGraphWriteCoordinator({
  captureGeneration = () => 0,
  keep,
  onFailure,
  onKept,
  onQueued,
  onSaved,
  pagesWaiting,
  write,
}: NativeContentGraphWriteCoordinatorOptions) {
  const pendingOperations = new Map<string, GraphOperation>();
  // The record each waiting change changes, by subject: a different change
  // to a record already waiting goes out after it, as the server takes one
  // change to a record at a time (a new list item indented at once,
  // 2026-10-02).
  const pendingSubjects = new Map<string, string>();
  let pendingPageId: string | null = null;
  let pendingGeneration: number | null = null;
  // Calls off the collection's send; whether it goes after the next frame.
  let stopCollecting: (() => void) | null = null;
  let collectingSoon = false;
  let saveChain: Promise<boolean> = Promise.resolve(true);
  // Changes collected and not yet sent, each told the save that takes it.
  let blockChangesCollected: BlockChangeSent[] = [];
  // Lets go of the typed values collected, held until the typing ends (lib/typedWrites.ts).
  let releaseTyped: (() => void) | null = null;
  // What is collected, kept on this device at each change as the change it goes as (lib/typedGraphChanges.ts).
  const typing = typedChangeKeeping();
  const controllers = new Set<AbortController>();
  let active = true;
  // By page, the writes made here that are being admitted or saved, and how
  // many were made: a read of the page holds a write only once it is saved.
  const working = new Map<string, number>();
  const writesMade = new Map<string, number>();
  // Reads waiting for a page's writes: this tab's alone (`here`), or the outbox's too.
  const settledWaiters = new Map<string, Set<SettledWaiter>>();

  // A row's value typed and held on the page counts too, and goes ahead of the page's next change (rowValuesTyped.ts).
  function unsavedHere(pageId: string) {
    return (working.get(pageId) ?? 0) > 0 || (pendingPageId === pageId && pendingOperations.size > 0) || typedRowValueHeldOn(pageId);
  }

  function unsaved(pageId: string, here: ReadSettledScope = false) {
    if (unsavedHere(pageId)) return true;
    if (here === "own") return (pagesWaiting?.ownUnanswered?.(pageId).length ?? 0) > 0;
    return !here && pagesWaiting?.has(pageId) === true;
  }

  function releaseSettled() {
    for (const [waitingPageId, waiters] of settledWaiters) {
      for (const waiter of waiters) {
        if (unsaved(waitingPageId, waiter.here)) continue;
        waiters.delete(waiter);
        waiter.resolve();
      }
      if (waiters.size === 0) settledWaiters.delete(waitingPageId);
    }
  }
  const stopWaitingPages = pagesWaiting?.subscribe(releaseSettled), stopTypedHeld = onTypedRowValueLetGo(releaseSettled);
  // Changes still being collected go as the page goes, not after it.
  const flushLeaving = () => {
    if (!active || stopCollecting === null || !pendingPageId) return;
    stopCollecting();
    void flush(pendingPageId);
  };
  if (typeof window !== "undefined") window.addEventListener("pagehide", flushLeaving);

  function work(pageId: string, change: 1 | -1) {
    const next = (working.get(pageId) ?? 0) + change;
    if (next > 0) working.set(pageId, next);
    else working.delete(pageId);
    releaseSettled();
  }

  async function settled(pageId: string, here: ReadSettledScope = false) {
    // Those kept before the tab opened are known once the outbox was read.
    if (here !== true) await pagesWaiting?.ready();
    if (!active || !unsaved(pageId, here)) return;
    return new Promise<void>((resolve) => {
      const waiters = settledWaiters.get(pageId) ?? new Set<SettledWaiter>();
      waiters.add({ here, resolve });
      settledWaiters.set(pageId, waiters);
    });
  }

  async function flush(pageId: string) {
    releaseTyped?.();
    releaseTyped = null;
    const operations = [...pendingOperations.values()];
    const collected = blockChangesCollected;
    blockChangesCollected = [];
    const generation = pendingGeneration ?? captureGeneration(pageId);
    const going = typing.take();
    pendingOperations.clear();
    pendingSubjects.clear();
    pendingPageId = null;
    pendingGeneration = null;
    stopCollecting = null;
    collectingSoon = false;
    if (operations.length === 0) {
      for (const sent of collected) sent.saved(saveChain);
      going.kept(saveChain);
      return saveChain;
    }

    const controller = new AbortController();
    controllers.add(controller);
    work(pageId, 1);
    // Collected as typed, or a name's saving, it goes under the ID it was kept on this device as.
    const { mutationId } = going;
    // The text written with the block changes it sends goes with it, kept
    // with it, whichever tab sends it (2026-11-13: sent apart, another tab's
    // outbox sent the change without it, and its text came after).
    for (const sent of collected) sent.named(mutationId);
    const riding = collected.length ? textRidingWith(pageId, mutationId) : null;
    going.keep(pageId, operations, Boolean(riding?.text?.words || riding?.text?.typing));
    const kept = keep?.(pageId, mutationId, operations, riding?.text);
    const save = saveChain.catch(() => false).then(async () => {
      try {
        const receipt = await write(pageId, mutationId, operations, controller.signal, riding?.text);
        riding?.answered(typeof receipt === "object" && receipt.wordsStored === true);
        if (controller.signal.aborted) return false;
        if (receipt === "kept" || receipt === "sent") onKept?.(pageId, operations, generation, receipt, mutationId);
        else onSaved(pageId, operations, receipt, generation);
        // Taken, though the page's writing ended meanwhile (dispose).
        return true;
      } catch (error) {
        riding?.answered(false);
        if (!controller.signal.aborted && active) {
          await onFailure(error, pageId, operations, generation, Boolean(riding)).catch(() => undefined);
        }
        return false;
      } finally {
        controllers.delete(controller);
        work(pageId, -1);
      }
    });
    saveChain = save;
    for (const sent of collected) sent.saved(save);
    going.kept(kept ? Promise.resolve(kept).catch(() => undefined) : save);
    return save;
  }

  async function admit(
    pageId: string,
    operations: GraphOperation[],
    immediate: boolean,
    generation: number,
    claim?: TypedGraphClaim,
  ) {
    if (!active) {
      claim?.went(Promise.resolve());
      return false;
    }
    work(pageId, 1);
    try {
      return await admitOperations(pageId, operations, immediate, generation, claim);
    } finally {
      // Not sent (a change ahead of it refused): its typing goes with it.
      claim?.went(Promise.resolve());
      work(pageId, -1);
    }
  }

  async function admitOperations(
    pageId: string,
    operations: GraphOperation[],
    immediate: boolean,
    generation: number,
    claim?: TypedGraphClaim,
  ) {
    if (pendingPageId && (pendingPageId !== pageId || pendingGeneration !== generation)) {
      const saved = await flush(pendingPageId);
      if (!saved || !active) return false;
    }
    pendingPageId = pageId;
    pendingGeneration = generation;
    for (const operation of operations) {
      if (requiresDedicatedDatabaseMutation(operation) && pendingOperations.size > 0) {
        const saved = await flush(pageId);
        if (!saved || !active) return false;
        pendingPageId = pageId;
        pendingGeneration = generation;
      }
      const key = graphOperationKey(operation);
      const subject = graphOperationSubject(operation);
      const waiting = pendingSubjects.get(subject);
      const previous = pendingOperations.get(key);
      const changedMove = previous?.kind === "moveBlock" && operation.kind === "moveBlock"
        && (previous.move.parentId !== operation.move.parentId || previous.move.afterId !== operation.move.afterId);
      if (waiting !== undefined && (waiting !== key || changedMove)) {
        // Sent now and not waited for: the save chain keeps it ahead of this
        // change and of every change made meanwhile, in the order made.
        // A later move can anchor to a newly inserted block. Replacing an
        // earlier map entry would run it before that anchor is created.
        void flush(pageId);
        pendingPageId = pageId;
        pendingGeneration = generation;
      }
      pendingOperations.set(key, coalesceGraphOperation(pendingOperations.get(key), operation));
      pendingSubjects.set(subject, key);
      if (requiresDedicatedDatabaseMutation(operation)) {
        const saved = await flush(pageId);
        if (!saved || !active) return false;
        pendingPageId = pageId;
        pendingGeneration = generation;
        continue;
      }
      if (pendingOperations.size === graphWireBounds.mutationOperations) {
        const saved = await flush(pageId);
        if (!saved || !active) return false;
        pendingPageId = pageId;
        pendingGeneration = generation;
      }
    }
    // A name's saving goes with the change that sends it, under the ID its typing was kept as.
    if (claim) typing.claimed(claim);
    if (immediate) return flush(pageId);
    return true;
  }

  /**
   * Collects a change to send once typing pauses; `soon`, once the next
   * frame is painted, with what was collected before it. A change of a
   * page's blocks — made, removed, moved — is sent soon: the text it moved
   * went to the others at once through the room, and they showed it without
   * the blocks for half a second, a merged line beside its emptied one, a
   * line put back by an Undo gone again (2026-11-13). The text written with
   * it waits for it to be saved, or goes with the change that saves it
   * (lib/blockChangesInFlight.ts). `typed`, a value typed into a field,
   * also goes at once when the typing ends otherwise (lib/typedWrites.ts).
   * It answers whether the save that takes it was taken.
   */
  function queue(pageId: string, operations: GraphOperation[], soon = false, typed = false): Promise<boolean> {
    if (!active) return Promise.resolve(false);
    sendTypedRowValuesNow(pageId);
    if (operations.length > 0) writesMade.set(pageId, (writesMade.get(pageId) ?? 0) + 1);
    const generation = captureGeneration(pageId);
    onQueued(pageId, generation);
    const admitting = admit(pageId, operations, false, generation);
    let sent = unnoted;
    if (soon && operations.length > 0) {
      let saved: (save: Promise<unknown>) => void = () => undefined;
      const sending = noteBlockChangeSending(pageId, new Promise((resolve) => { saved = resolve; }));
      sent = { named: (mutationId) => sending.sentWith(mutationId), saved: (save) => saved(save) };
    }
    let answer: (save: Promise<boolean>) => void = () => undefined;
    const taken = new Promise<boolean>((resolve) => { answer = (save) => void save.then(resolve, () => resolve(false)); });
    void admitting.then((admitted) => {
      // Collected, it is sent with the rest; else it went in a save made meanwhile.
      if (active && admitted && pendingPageId === pageId && pendingOperations.size > 0) {
        blockChangesCollected.push({ named: sent.named, saved: (save) => { sent.saved(save); answer(save); } });
      } else {
        sent.saved(saveChain);
        answer(admitted ? saveChain : Promise.resolve(false));
      }
      if (!active || !admitted || pendingOperations.size === 0) return;
      if (pendingPageId === pageId) typing.collected(pageId, [...pendingOperations.values()], soon && operations.length > 0);
      const send = () => {
        if (pendingPageId) void flush(pendingPageId);
      };
      if (typed) releaseTyped ??= holdUntilTypingEnds(send);
      // Going soon already, it takes this one too.
      if (collectingSoon && stopCollecting) return;
      stopCollecting?.();
      collectingSoon = soon;
      if (soon) stopCollecting = afterNextPaint(send);
      else {
        const timer = window.setTimeout(send, TYPING_PAUSE_MS);
        stopCollecting = () => window.clearTimeout(timer);
      }
    });
    return taken;
  }

  /**
   * A row's value typed into its field, collected with the rest until the
   * typing ends (queue): one save for what was typed, which a database's
   * automations hear once. It answers whether it was saved.
   */
  const collect = (pageId: string, operations: GraphOperation[]) => queue(pageId, operations, false, true);

  async function commit(pageId: string, operations: GraphOperation[]) {
    if (!active) return false;
    // Taken as the call is made, while a name's field sends its saving (typedName.ts).
    const claim = typedGraphChange(operations);
    sendTypedRowValuesNow(pageId);
    if (operations.length > 0) writesMade.set(pageId, (writesMade.get(pageId) ?? 0) + 1);
    stopCollecting?.();
    stopCollecting = null;
    collectingSoon = false;
    return admit(pageId, operations, true, captureGeneration(pageId), claim);
  }

  function dispose() {
    // Changes still being collected go as this page's writing ends, as they
    // do when the browser page goes (flushLeaving): a database's name typed
    // and left within the typing pause was dropped here (2026-11-11).
    const sending = [...controllers];
    if (active && pendingPageId && pendingOperations.size > 0) {
      stopCollecting?.();
      void flush(pendingPageId);
    }
    active = false;
    stopWaitingPages?.();
    stopTypedHeld();
    if (typeof window !== "undefined") window.removeEventListener("pagehide", flushLeaving);
    releaseTyped?.();
    releaseTyped = null;
    stopCollecting?.();
    stopCollecting = null;
    collectingSoon = false;
    pendingOperations.clear();
    pendingSubjects.clear();
    pendingPageId = null;
    pendingGeneration = null;
    // Sends under way go on with an outbox, whose turn they hold: stopped,
    // one the server had already taken was sent again from it, and what was
    // to follow it (a page it made) waited for that (2026-11-14: a line
    // turned into a page, the page opened in its place). Without one the last
    // changes' own send is all there is: it goes on.
    if (!keep) for (const controller of sending) controller.abort();
    controllers.clear();
    for (const waiters of settledWaiters.values()) for (const waiter of waiters) waiter.resolve();
    for (const sent of blockChangesCollected) sent.saved(saveChain);
    blockChangesCollected = [];
    typing.drop(saveChain);
    settledWaiters.clear();
  }

  /**
   * Reads what the server holds of a page once every write made here to it
   * is saved, and reads again when one is made while it reads, so what it
   * answers holds them all and can stand for the page. `here`, the writes
   * made in this tab alone: those another tab keeps in the outbox reach the
   * page through its room once saved, as another person's do; "own", this
   * tab's kept in the outbox too, before its reload as well
   * (ReadSettledScope). A read refused while an operation holds the page
   * comes to nothing, the page told it is held (lib/pageBusy.ts heldRead),
   * and is read again once that ends.
   */
  async function readSettled<Value>(pageId: string, read: () => Promise<Value | null>, here: ReadSettledScope = false): Promise<Value | null> {
    while (active) {
      await settled(pageId, here);
      if (!active) return null;
      const made = writesMade.get(pageId) ?? 0;
      const value = await read().catch((error: unknown) => heldRead(pageId, error));
      if (value === null) return null;
      if (!unsaved(pageId, here) && (writesMade.get(pageId) ?? 0) === made) return value;
    }
    return null;
  }

  /**
   * Runs a change the server makes to a page for a write here (a paste's
   * database copy, then its place), the page's reads waiting for it as for
   * the page's own writes: one read meanwhile showed the copy where the
   * server first put it (2026-11-08).
   */
  async function holding<Value>(pageId: string, task: () => Promise<Value>): Promise<Value> {
    work(pageId, 1);
    try { return await task(); } finally { work(pageId, -1); }
  }

  /** Whether writes made here to a page are still being collected or saved. */
  const writing = (pageId: string) => active && unsaved(pageId);
  /** Whether this tab's own writes to a page are, not another tab's kept in the outbox. */
  const writingHere = (pageId: string) => active && unsavedHere(pageId);
  /** Whether this tab's own writes to a page are, kept ones too ("own"). */
  const writingOwn = (pageId: string) => active && unsaved(pageId, "own");

  return { collect, commit, dispose, holding, queue, readSettled, writing, writingHere, writingOwn };
}
