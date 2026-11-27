import { graphViewUpdates, type ContentGraph, type GraphOperation } from "@/contentGraph";
import { graphOperationRuns } from "./nativeGraphAuthority";
import { newId } from "./ids";
import { typedDraftsKept, type TypedGraphClaim, type TypedKept } from "./typedDraftSender";
import type { TypedValue } from "./typedDrafts";

/*
 * A page's content changes kept on this device as they are typed — the
 * changes its writes collect until the typing pauses (a database's name,
 * nativeContentGraphWriteCoordinator.ts), and a name typed in its field
 * that its owner saves as it is left or closed (typedName.ts) — under the
 * ID each goes out as, in the store of what is typed (typedDrafts.ts), so a
 * browser killed or a phone's app ended right after the last key loses
 * nothing: the change goes into the outbox when Leafen opens here again
 * (typedDraftSender.ts recoverTypedDrafts), and the server takes it once.
 */

/** The record of a page's content changes collected as typed, by the ID they go out as. */
export const typedChangeRecord = (mutationId: string) => `change:${mutationId}`;
/** The record of a name typed in its field, by what it names ("property:<id>", graphOperationSubject). */
export const typedNameRecord = (subject: string) => `name:${subject}`;

/**
 * Keeps a page's content change as it stands now on this device under its
 * record (`recordKey`) and the ID it goes out as; answers its turn, which
 * settleTypedChange takes, or 0 when nothing keeps typing here.
 */
export function keepTypedChange(recordKey: string, mutationId: string, pageId: string, operations: readonly GraphOperation[]): number {
  return typedDraftsKept()?.keep({ recordKey, kind: "graph", mutationId, hostPageId: pageId, operations: [...operations] }) ?? 0;
}

/**
 * A name typed in a rename box whose owner writes it as a change of a page's
 * content (sendContentGraphChange) — a sidebar's database row, a favorite
 * database — kept as that change at each key (`operations`, for what is
 * typed), on the page that change goes to.
 */
export function graphNameKept(record: string, hostPageId: string, operations: (typed: TypedValue) => GraphOperation[] | null): TypedKept {
  return { recordKey: typedNameRecord(record), kind: "graph", hostPageId, base: () => null, operations };
}

/** Takes a kept change off once what keeps it next holds it (the outbox), unless it was kept again since its turn. */
export function settleTypedChange(recordKey: string, turn: number) {
  if (!turn) return;
  void typedDraftsKept()?.settle(recordKey, turn).catch(() => undefined);
}

/** A change made on the open page's graph as drawn (`make`), as its writes send it: the page it goes to, and what it writes. */
export type TypedGraphChangeOf = (make: (graph: ContentGraph) => GraphOperation[] | null) => { pageId: string; operations: GraphOperation[] } | null;

let changeOf: TypedGraphChangeOf | null = null;

/**
 * The open page's writes say how a change made on it goes: to the page
 * holding what it writes (a linked database's source's), as its views read
 * then (useContentGraphWrites). Answers how to stop.
 */
export function sendTypedGraphChangesBy(resolve: TypedGraphChangeOf): () => void {
  changeOf = resolve;
  return () => { if (changeOf === resolve) changeOf = null; };
}

/** How a change made on the open page goes, or null when no page's writes are open, it makes none, or it goes to more than one page. */
export function typedGraphChangeOf(make: (graph: ContentGraph) => GraphOperation[] | null) {
  return changeOf?.(make) ?? null;
}

/**
 * A change made on `graph`, shown on `pageId`, as it goes: to the one page
 * holding what it writes (graphOperationRuns), a view's change written over
 * the view as drawn (graphViewUpdates), as the page's writes send it.
 */
export function typedGraphChangeOn(graph: ContentGraph, pageId: string | null, operations: readonly GraphOperation[] | null) {
  const runs = operations?.length ? graphOperationRuns(graph, pageId, operations) : null;
  return runs?.length === 1 ? { pageId: runs[0].pageId, operations: graphViewUpdates(graph, runs[0].operations) } : null;
}

/**
 * How a page's writes keep what they collect as typed (the write
 * coordinator, nativeContentGraphWriteCoordinator.ts): the changes waiting
 * for the typing to pause kept at each change as the one they go out as — a
 * database's name typed was only in memory until its typing paused, and a
 * browser killed then lost it (2026-11-27, 2 in 2) — and a name's saving
 * sent with them (`claimed`, typedGraphChange). Each stands until the outbox
 * holds the change, or, without one, until it is answered. A block change
 * collected leaves it as it stood: the page's text goes with block changes
 * as they are sent (textBehindBlockChanges.ts), which only the outbox keeps
 * with them, and a change kept here without it doesn't stand for them.
 */
export function typedChangeKeeping() {
  let collection: { mutationId: string; recordKey: string; turn: number } | null = null;
  let claims: TypedGraphClaim[] = [];
  return {
    /** The changes collected for a page, as they stand now; `blockChange`, one just collected a block's. */
    collected(pageId: string, operations: readonly GraphOperation[], blockChange: boolean) {
      collection ??= { mutationId: newId(), recordKey: "", turn: 0 };
      if (blockChange) return;
      collection.recordKey ||= typedChangeRecord(collection.mutationId);
      collection.turn = keepTypedChange(collection.recordKey, collection.mutationId, pageId, operations);
    },
    /** A name's saving that goes with the next change sent. */
    claimed(claim: TypedGraphClaim) {
      claims.push(claim);
    },
    /** The change going out now, what was collected and claimed for it: its ID, and how it stays kept. */
    take() {
      const recorded = collection, taken = claims;
      collection = null;
      claims = [];
      const mutationId = recorded?.mutationId ?? taken[0]?.mutationId ?? newId();
      const recordKey = recorded?.recordKey || (taken[0]?.mutationId === mutationId ? taken[0].recordKey : typedChangeRecord(mutationId));
      let turn = recorded?.turn ?? 0;
      return {
        mutationId,
        /** Kept as it goes, the operations it sends: but with the page's text riding with it (`words`), as it stood. */
        keep(pageId: string, operations: readonly GraphOperation[], words: boolean) {
          if (!words && (turn || taken.length)) turn = keepTypedChange(recordKey, mutationId, pageId, operations);
        },
        /** What keeps it from now (the outbox's keeping, or its answer): then its record goes. */
        kept(onDevice: Promise<unknown>) {
          if (turn) void onDevice.then(() => settleTypedChange(recordKey, turn));
          for (const claim of taken) claim.went(onDevice);
        },
      };
    },
    /** The page's writing ended: what was claimed goes with what is under way. */
    drop(underWay: Promise<unknown>) {
      for (const claim of claims) claim.went(underWay);
      claims = [];
      collection = null;
    },
  };
}
