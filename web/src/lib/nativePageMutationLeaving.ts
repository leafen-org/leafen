import type {
  NativeGraphLaneEvent,
  NativeGraphMutation,
  NativeMetadataMutation,
} from "./nativePageMutationOutbox";
import { pageLeaving } from "./pageLeaving";
import { serverScopedName } from "./serverOrigin";
import { localBoxIfSettled, localEncryptionMode } from "./localLock";
import { changesGoingTogether, settingsGoingTogether } from "./nativeLeavingTogether";
import {
  leavingMutationId,
  openLeavingChange,
  sealLeavingChange,
  type SealedLeavingChange,
} from "./leavingChangeRecord";

export type LeavingPageChange =
  | { kind: "graph"; input: NativeGraphMutation }
  | { kind: "metadata"; input: NativeMetadataMutation };
type LeavingRecord = LeavingPageChange | SealedLeavingChange;

/** What the page's outbox store takes a change written down as a page went into. */
export interface LeavingChangesStore {
  keepGraph(input: NativeGraphMutation): Promise<unknown>;
  keepMetadata(input: NativeMetadataMutation): Promise<unknown>;
  replay(): Promise<unknown>;
}

/**
 * The page changes that would go with the page: the ones written down as it
 * goes, and the ones on their way to the store or the server meanwhile.
 */
export interface NativePageLeavingChanges {
  /** Writes a change down as the page goes, sealed under the passphrase lock. */
  keep(change: LeavingPageChange): void;
  /** Writes a change down the store couldn't take (nativePageMutationPastStore.ts); whether it could. */
  writeDown(change: LeavingPageChange): Promise<boolean>;
  /** Whether a change is written down, not yet sent nor moved into the store. */
  hasKept(): boolean;
  /** The changes written down that can be read here, oldest first. */
  keptChanges(): Promise<LeavingPageChange[]>;
  /** A change the store hasn't taken and the server hasn't answered yet. */
  nowOnItsWay(change: LeavingPageChange): void;
  noLongerOnItsWay(mutationId: string): void;
  /**
   * A change made as the page goes behind the page's older ones not yet
   * answered here: it joins them, and they go as one change when they can
   * (nativeLeavingTogether.ts).
   */
  goTogether(input: NativeGraphMutation): void;
  /** Sends what was written down straight out while another tab keeps the store. */
  sendPastStore(): Promise<void>;
  /** Moves what was written down into the store once it opens, after what it holds. */
  moveInto(store: LeavingChangesStore): Promise<void>;
  /** The pages of what was written down and not yet moved; "every" for one sealed, whose page isn't read without opening it. */
  pagesKept(): ReadonlySet<string> | "every";
  discard(): void;
  dispose(): void;
}

// A change kept as the page goes has no time for the store: it is written
// down at once and moved into the store, after what it already holds, when
// the store next opens (2026-11-04: a title's last letters typed while its
// earlier save was answered were gone after a reload). A change the store
// couldn't take while another tab keeps it, the server out of reach, is
// written down here too (nativePageMutationPastStore.ts). Under the passphrase
// lock it is written down only sealed (leavingChangeRecord), so what the
// lock keeps never lies on the disk as it is (2026-11-05).
// The most a page's changes going together as it goes carry, in characters.
const LEAVING_TOGETHER_MOST = 32 * 1024;

export function createNativePageLeavingChanges(options: {
  /**
   * Whether an older change to the page, kept or sent, hasn't been answered
   * (nativeGraphPagesWaiting.ts): one behind it doesn't go as the page goes.
   */
  olderGraphUnsettled?: (input: NativeGraphMutation) => boolean;
  /** The page's graph changes made in this tab and not yet answered, oldest first (nativeGraphPagesWaiting.ts). */
  unansweredChanges?: (pageId: string) => NativeGraphMutation[] | null;
  /** The pages with such changes. */
  pagesUnanswered?: () => string[];
  /** Counts a change among its page's not yet answered. */
  unsettle?: (input: NativeGraphMutation) => void;
  onGraphLane(event: NativeGraphLaneEvent): void;
  sendGraph(input: NativeGraphMutation, signal?: AbortSignal): Promise<unknown>;
  sendMetadata(input: NativeMetadataMutation, signal?: AbortSignal): Promise<unknown>;
  userId: string;
  waitsForServer?: (error: unknown) => boolean;
}): NativePageLeavingChanges {
  const leavingKey = serverScopedName(`leafen:leaving-page-changes:${options.userId}`);
  const readLeaving = (): LeavingRecord[] => {
    try {
      const value: unknown = JSON.parse(localStorage.getItem(leavingKey) ?? "[]");
      return Array.isArray(value) ? value as LeavingRecord[] : [];
    } catch {
      return [];
    }
  };
  // Whether it is written down (once: one written down again stays where it was).
  const writeLeaving = (record: LeavingRecord): boolean => {
    try {
      const records = readLeaving();
      if (records.some((other) => leavingMutationId(other) === leavingMutationId(record))) return true;
      localStorage.setItem(leavingKey, JSON.stringify([...records, record]));
      return true;
    } catch {
      // Without storage the change goes as it can, as before.
      return false;
    }
  };
  // Takes a change off what is written down once it went or the store took it; whether it could.
  const dropLeaving = (mutationId: string): boolean => {
    try {
      const rest = readLeaving().filter((other) => leavingMutationId(other) !== mutationId);
      if (rest.length) localStorage.setItem(leavingKey, JSON.stringify(rest));
      else localStorage.removeItem(leavingKey);
      return true;
    } catch {
      return false;
    }
  };
  const lockBox = () => {
    const gate = localBoxIfSettled(options.userId);
    return gate && gate !== "device" && gate !== "pending" ? gate : null;
  };
  const sealedOnItsWay = new Map<string, SealedLeavingChange>();
  // Written at once where it can be — in the clear, or sealed already —
  // before anything is awaited: the page may have no more time.
  const writeDown = async (change: LeavingPageChange): Promise<boolean> => {
    if (localEncryptionMode() !== "passphrase") return writeLeaving(change);
    const sealed = sealedOnItsWay.get(change.input.mutationId);
    if (sealed) return writeLeaving(sealed);
    // Sealing takes a moment the page may not have: it goes by its request then.
    const box = lockBox();
    if (!box) return false;
    try {
      return writeLeaving(await sealLeavingChange(box, leavingKey, change.input.mutationId, change));
    } catch {
      return false;
    }
  };
  const keepLeaving = (change: LeavingPageChange) => {
    void writeDown(change);
  };
  const openLeaving = (record: LeavingRecord): Promise<LeavingPageChange | undefined> => {
    if (record.kind !== "sealed") return Promise.resolve(record);
    const box = lockBox();
    return box ? openLeavingChange<LeavingPageChange>(box, leavingKey, record) : Promise.resolve(undefined);
  };
  const sendChange = (change: LeavingPageChange, signal?: AbortSignal): Promise<unknown> => (
    change.kind === "graph" ? options.sendGraph(change.input, signal) : options.sendMetadata(change.input, signal)
  );
  // A change the store hasn't taken and the server hasn't answered goes with
  // the page unless it is written down as the page goes, and the store may be
  // what it waits for (another tab keeps it, deviceStoreLock.ts; 2026-11-05: a
  // line typed, an image or a table row made then was gone after a reload).
  const onItsWay = new Map<string, LeavingPageChange>();
  const nowOnItsWay = (change: LeavingPageChange) => {
    const { mutationId } = change.input;
    onItsWay.set(mutationId, change);
    hearGoingLast();
    const box = localEncryptionMode() === "passphrase" ? lockBox() : null;
    if (!box) return;
    // Sealed now, for the page's going, when there is no more time.
    void sealLeavingChange(box, leavingKey, mutationId, change).then((sealed) => {
      if (onItsWay.has(mutationId)) sealedOnItsWay.set(mutationId, sealed);
    }, () => undefined);
  };
  const noLongerOnItsWay = (mutationId: string) => {
    // A request out as the page goes fails before the page's going is heard
    // here: that one is still on its way.
    if (pageLeaving()) return;
    onItsWay.delete(mutationId);
    sealedOnItsWay.delete(mutationId);
  };
  // A page's changes not yet answered here go as one change as the page
  // goes, when they can (nativeLeavingTogether.ts); sent again as it goes
  // only with one more among them, made then — a field's save as it goes
  // may come before the page's going is heard here — and anew once the
  // page is shown again from the back-forward cache and goes again.
  const wentTogether = new Map<string, string>();
  const shownAgain = () => wentTogether.clear();
  const sendTogether = (pageId: string): readonly string[] => {
    const chain = options.unansweredChanges?.(pageId);
    const together = chain ? changesGoingTogether(chain) : null;
    // A request that outlives the page carries little (the browser lets such
    // requests carry 64 KiB in all): a larger one goes after the reload as kept.
    if (!together || JSON.stringify(together.change).length > LEAVING_TOGETHER_MOST) return [];
    const members = together.members.join(" ");
    if (wentTogether.get(pageId) === members) return together.members;
    wentTogether.set(pageId, members);
    void options.sendGraph(together.change).catch(() => undefined);
    return together.members;
  };
  // Each goes as the page goes, in a request that outlives it, written down
  // too should it not arrive — but one behind an older change to its page
  // that hasn't been answered is only written down, and goes behind it after
  // the reload: sent beside it, the two reach the server in either order
  // (2026-11-25: a template chosen on a new row's empty body, its making not
  // yet answered, filled the row, then the making arrived and wrote its
  // empty title over the template's). As a change made while the page goes
  // (nativePageMutationBrowser.ts admitAndSendGraph). The page's changes
  // that go together go so first, whether on their way or kept in the store.
  // It hears the page go after what saves as the page goes — a field's
  // words, a write held for its typing to end — that was there when the last
  // change went on its way: such a save joins the changes going together, sent
  // once with the one on its way, instead of that one going by itself first
  // and the two reaching the server in either order (2026-11-27: a row page's
  // title saved as its window went, behind its letter not yet sent).
  // A page's settings changes not yet answered go as one too (settingsGoingTogether).
  const settingsTogether = (): string[] => {
    const byPage = new Map<string, NativeMetadataMutation[]>();
    for (const change of onItsWay.values()) {
      if (change.kind === "metadata") byPage.set(change.input.pageId, [...byPage.get(change.input.pageId) ?? [], change.input]);
    }
    return [...byPage.values()].flatMap((chain) => {
      const joined = settingsGoingTogether(chain);
      if (!joined) return [];
      void options.sendMetadata(joined).catch(() => undefined);
      return joined.after ?? [];
    });
  };
  const leaveOnItsWay = () => {
    const together = new Set([...(options.pagesUnanswered?.() ?? []).flatMap(sendTogether), ...settingsTogether()]);
    for (const change of onItsWay.values()) {
      keepLeaving(change);
      if (together.has(change.input.mutationId) || (change.kind === "graph" && options.olderGraphUnsettled?.(change.input))) continue;
      void sendChange(change).catch(() => undefined);
    }
    onItsWay.clear();
    sealedOnItsWay.clear();
  };
  function hearGoingLast() {
    if (typeof window === "undefined" || pageLeaving()) return;
    window.removeEventListener("pagehide", leaveOnItsWay);
    window.addEventListener("pagehide", leaveOnItsWay);
  }
  if (typeof window !== "undefined") {
    window.addEventListener("pagehide", leaveOnItsWay);
    window.addEventListener("pageshow", shownAgain);
  }
  // Another tab keeps the store, so what a page left can't go through it for
  // now: it goes straight out, each taken off as it is answered, and its
  // page's writer hears it answered as the store's lane tells it. One the
  // server can't be reached for waits, with the ones after it.
  let sendingLeaving: Promise<void> | null = null;
  const sendLeavingPastStore = () => sendingLeaving ??= (async () => {
    for (const record of readLeaving()) {
      const change = await openLeaving(record).catch(() => undefined);
      // Sealed by a lock not open here: it waits for the store.
      if (!change) return;
      const { mutationId, pageId } = change.input;
      try {
        await sendChange(change);
        if (change.kind === "graph") options.onGraphLane({ kind: "answered", mutationId, pageId });
      } catch (error) {
        if (options.waitsForServer?.(error) ?? true) return;
        if (change.kind === "graph") options.onGraphLane({ kind: "answered", mutationId, pageId, refused: true });
        options.onGraphLane({ error, kind: "rejected", pageId, ...(change.kind === "graph" ? {} : { settings: true as const }) });
      }
      if (!dropLeaving(mutationId)) return;
    }
  })().finally(() => { sendingLeaving = null; });
  // Each is taken off once the store keeps it, so one the store doesn't take
  // (another tab keeps it), or the page going meanwhile, leaves it written
  // down for the next time. One moved twice (two tabs opening the store) is
  // the store's one entry of its mutation, taken by the server once.
  let movingLeaving: Promise<void> | null = null;
  const moveLeaving = (store: LeavingChangesStore) => movingLeaving ??= (async () => {
    let moved = 0;
    for (const record of readLeaving()) {
      try {
        const change = await openLeaving(record);
        if (!change) break;
        if (change.kind === "graph") await store.keepGraph(change.input);
        else await store.keepMetadata(change.input);
      } catch {
        break;
      }
      if (!dropLeaving(leavingMutationId(record))) break;
      moved += 1;
    }
    if (moved) void store.replay().catch(() => undefined);
  })().finally(() => { movingLeaving = null; });

  return {
    keep: keepLeaving,
    writeDown,
    hasKept: () => readLeaving().length > 0,
    async keptChanges() {
      const changes: LeavingPageChange[] = [];
      for (const record of readLeaving()) {
        const change = await openLeaving(record).catch(() => undefined);
        if (change) changes.push(change);
      }
      return changes;
    },
    nowOnItsWay,
    noLongerOnItsWay,
    goTogether(input) {
      options.unsettle?.(input);
      sendTogether(input.pageId);
    },
    sendPastStore: sendLeavingPastStore,
    moveInto: moveLeaving,
    pagesKept() {
      const pages = new Set<string>();
      for (const record of readLeaving()) {
        if (record.kind === "sealed") return "every";
        pages.add(record.input.pageId);
      }
      return pages;
    },
    discard() {
      try {
        localStorage.removeItem(leavingKey);
      } catch {
        // Nothing was kept there then.
      }
    },
    dispose() {
      if (typeof window === "undefined") return;
      window.removeEventListener("pagehide", leaveOnItsWay);
      window.removeEventListener("pageshow", shownAgain);
    },
  };
}
