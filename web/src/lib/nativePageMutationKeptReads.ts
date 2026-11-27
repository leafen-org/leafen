import type { Page } from "@/workspaceMembershipWire.generated";
import type { OutboxBrowserRuntime } from "./outbox";
import type { NativeGraphMutation, NativeMetadataMutation, NativePageCreation } from "./nativePageMutationOutbox";
import type { NativePageLeavingChanges } from "./nativePageMutationLeaving";
import { keptPastStoreAfter } from "./nativePageMutationPastStore";
import { pagesWithKeptChanges, type PagesWithKeptChanges } from "./nativeOutboxKeptPages";
import { rowTrashShown } from "./nativePageLaneEntries";

// What this device keeps of a person's page changes, read for the screen: the
// changes the store holds, then those kept past it (nativePageMutationLeaving.ts)
// that are not in it yet. The page outbox (nativePageMutationBrowser.ts) opens
// the store and sends; these only read.

/** The store as these reads see it: opened here, or not (the online-only choice, or none). */
export type NativeKeptStoreOpening =
  | {
      kind: "ready";
      owner: {
        pendingGraph(): Promise<Map<string, NativeGraphMutation[]>>;
        pendingMetadata(): Promise<Map<string, NativeMetadataMutation[]>>;
        pendingPageCreations(): Promise<NativePageCreation[]>;
      };
      runtime: Pick<OutboxBrowserRuntime, "listReplayStep">;
    }
  | { kind: "network-only" | "unavailable" };

export function createNativeKeptChangeReads(options: {
  /** The store as it opens; refused while another tab keeps it past the wait. */
  currentOpening(): Promise<NativeKeptStoreOpening>;
  /** The store as opened, or null while another tab keeps it. */
  openedOrHeld(): Promise<NativeKeptStoreOpening | null>;
  leaving: Pick<NativePageLeavingChanges, "keptChanges" | "pagesKept">;
  maxEntriesPerStep: number;
}) {
  const { currentOpening, openedOrHeld, leaving } = options;
  // What the store keeps for the page, then what was kept past it and isn't
  // in it yet; rows going to or out of the trash as they show (rowTrashShown).
  const pendingGraphWrites = async (pageId: string) => {
    const result = await openedOrHeld();
    const kept = result?.kind === "ready" ? (await result.owner.pendingGraph()).get(pageId) ?? [] : [];
    const past = (await leaving.keptChanges()).flatMap((change) => change.kind === "graph" && change.input.pageId === pageId ? [change.input] : []);
    return keptPastStoreAfter(kept, past).map(({ mutationId, operations, rowTrash }) => ({ mutationId, operations: rowTrash ? rowTrashShown(rowTrash) : operations }));
  };
  return {
    async pendingNewPages(): Promise<Page[]> {
      const result = await currentOpening();
      if (result.kind !== "ready") return [];
      return (await result.owner.pendingPageCreations()).map(({ page }) => page);
    },
    async pendingPageMetadata() {
      const result = await openedOrHeld();
      const pages = result?.kind === "ready" ? await result.owner.pendingMetadata() : new Map<string, NativeMetadataMutation[]>();
      for (const change of await leaving.keptChanges()) {
        if (change.kind === "metadata") pages.set(change.input.pageId, keptPastStoreAfter(pages.get(change.input.pageId) ?? [], [change.input]));
      }
      return pages;
    },
    async pendingGraphOperations(pageId: string, shown?: readonly string[]) {
      const pending = await pendingGraphWrites(pageId);
      return pending.flatMap((mutation) => shown?.includes(mutation.mutationId) ? [] : mutation.operations);
    },
    pendingGraphWrites,
    async pagesWithKeptChanges(): Promise<PagesWithKeptChanges> {
      // A store another tab keeps past the wait can't be read: every page waits.
      const result = await currentOpening().catch(() => null);
      if (!result) return "every";
      const leavingPages = leaving.pagesKept();
      if (result.kind !== "ready") return leavingPages;
      return pagesWithKeptChanges(result.runtime, options.maxEntriesPerStep, leavingPages);
    },
  };
}
