export type OfflinePagePinReadiness = "pending" | "ready" | "removed";

export interface OfflinePagePinWarmResult {
  complete: boolean;
}

export interface OfflinePagePinServiceOptions {
  getPins(userId: string): Promise<Record<string, true>>;
  setPin(userId: string, pageId: string, pinned: boolean): Promise<unknown>;
  warm(userId: string, pageId: string): Promise<OfflinePagePinWarmResult>;
}

// A retry walks this many pins, the next ones after where the last walk
// ended in a steady order, so every pin gets its turn however many there are.
const MAX_PIN_RETRY_PAGES = 25;

export function createOfflinePagePinService(
  options: OfflinePagePinServiceOptions,
) {
  const lastRetried = new Map<string, string>();
  const retrying = new Map<string, Promise<{ attempted: number; complete: boolean }>>();
  return {
    async read(userId: string, pageId: string): Promise<boolean> {
      if (!userId || !pageId) return false;
      return Boolean((await options.getPins(userId))[pageId]);
    },

    async set(
      userId: string,
      pageId: string,
      pinned: boolean,
    ): Promise<{ pinned: boolean; readiness: OfflinePagePinReadiness }> {
      if (!userId || !pageId) {
        return { pinned: false, readiness: "removed" };
      }
      await options.setPin(userId, pageId, pinned);
      if (!pinned) return { pinned: false, readiness: "removed" };
      const warmed = await options.warm(userId, pageId).catch(() => ({ complete: false }));
      return {
        pinned: true,
        readiness: warmed.complete ? "ready" : "pending",
      };
    },

    // Complete says every pin is ready, which one walk can tell only when it
    // reached them all; one walk runs at a time for a person.
    retry(userId: string): Promise<{ attempted: number; complete: boolean }> {
      if (!userId) return Promise.resolve({ attempted: 0, complete: true });
      const running = retrying.get(userId);
      if (running) return running;
      const walk = (async () => {
        const pinned = Object.keys(await options.getPins(userId)).sort();
        const after = pinned.findIndex((pageId) => pageId > (lastRetried.get(userId) ?? ""));
        const start = after < 0 ? 0 : after;
        const pageIds = [...pinned.slice(start), ...pinned.slice(0, start)].slice(0, MAX_PIN_RETRY_PAGES);
        let complete = true;
        for (const pageId of pageIds) {
          try {
            await options.setPin(userId, pageId, true);
            complete = (await options.warm(userId, pageId)).complete && complete;
          } catch {
            complete = false;
          }
        }
        if (pageIds.length) lastRetried.set(userId, pageIds[pageIds.length - 1]!);
        return { attempted: pageIds.length, complete: complete && pageIds.length === pinned.length };
      })().finally(() => retrying.delete(userId));
      retrying.set(userId, walk);
      return walk;
    },
  };
}
