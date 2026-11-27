// What a page outbox owner (nativePageMutationOutbox.ts) knows of its own
// changes beside its store: what the lane's turns answered, and the changes
// it is keeping on the device that haven't landed in the store yet.

/**
 * What the lane's turns answered, by entry key, outliving an outbox owner as
 * the texts do: an entry one writer kept that another's turn sent and the
 * server answered is gone from the outbox, and its writer keeping it again
 * under the same key would send it twice (2026-11-14: a line's change went
 * again 6 s after its 201). Kept again, it stays answered and its writer
 * hears that answer. The newest few hundred are remembered.
 */
export interface NativeLaneAnswers {
  answered(entryKey: string): { result: unknown } | undefined;
  remember(entryKey: string, result: unknown): void;
}

export function createNativeLaneAnswers(limit = 512): NativeLaneAnswers {
  const answers = new Map<string, unknown>();
  return {
    answered: (entryKey) => answers.has(entryKey) ? { result: answers.get(entryKey) } : undefined,
    remember(entryKey, result) {
      answers.delete(entryKey);
      answers.set(entryKey, result);
      if (answers.size > limit) answers.delete(answers.keys().next().value as string);
    },
  };
}

/**
 * Keeps a change on the device as it is made, ahead of its send (keepGraph,
 * keepMetadata, keepPageCreation), unless a turn answered it already; gives
 * whether it still waits for the server once kept. One the server took while
 * it was being kept — its own send went meanwhile, taking this keep as its
 * own (createKeepsOnTheirWay) and dropping it from the store once landed —
 * waits for nothing, and its page isn't told it waits (2026-11-24: a new
 * page's making, kept just after the server made it, held every request
 * about the page a second, its title's among them, and the database changes
 * made behind that title piled up in the lane).
 */
export function keepUnansweredWith<Receipt>(answers: NativeLaneAnswers) {
  return async (entryKey: string, keep: () => Promise<Receipt>) => {
    if (answers.answered(entryKey)) return false;
    await keep();
    return !answers.answered(entryKey);
  };
}

function plainRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && Object.getPrototypeOf(value) === Object.prototype;
}

/** Whether two kept entries hold the same change: their fields the same values, a list's or a record's two levels down. */
function sameKept(left: unknown, right: unknown, depth = 2): boolean {
  if (left === right) return true;
  if (depth === 0) return false;
  if (Array.isArray(left) && Array.isArray(right)) {
    return left.length === right.length && left.every((item, index) => sameKept(item, right[index], depth - 1));
  }
  if (!plainRecord(left) || !plainRecord(right)) return false;
  const keys = Object.keys(left);
  return keys.length === Object.keys(right).length
    && keys.every((key) => Object.hasOwn(right, key) && sameKept(left[key], right[key], depth - 1));
}

/**
 * The changes an owner is keeping on the device, by entry key, until each
 * landed in its store or failed to. One kept again with the same content
 * while on its way takes that keep: a page's structure change is kept as it
 * is made and again as it is sent (nativeContentGraphWriteCoordinator.ts
 * keep, then write), and a 100 x 300 table's change wrote its megabyte
 * twice, the second behind the page's copy on the store's lock, on a thread
 * busy drawing the table (2026-11-19: its first request left 33 s after the
 * paste). A change being sent learns from them whether one kept here before
 * it may not be in the store yet (nativeLaneSending.ts).
 */
export function createKeepsOnTheirWay<Receipt>() {
  const going = new Map<string, { entry: object; kept: Promise<Receipt>; order: number }>();
  let kepts = 0;
  return {
    keep(entryKey: string, entry: object, write: () => Promise<Receipt>): Promise<Receipt> {
      const same = going.get(entryKey);
      if (same && sameKept(same.entry, entry)) return same.kept;
      let kept: Promise<Receipt>;
      try {
        kept = write();
      } catch (error) {
        kept = Promise.reject(error);
      }
      kepts += 1;
      const record = { entry, kept, order: kepts };
      going.set(entryKey, record);
      const landed = () => {
        if (going.get(entryKey) === record) going.delete(entryKey);
      };
      void kept.then(landed, landed);
      return kept;
    },
    /**
     * Whether a change kept here before `entryKey`'s is still on its way to
     * the store: the store takes them in the order kept, so once that one
     * landed none is.
     */
    aheadOnTheirWay(entryKey: string): boolean {
      const own = going.get(entryKey);
      if (!own) return false;
      for (const record of going.values()) if (record.order < own.order) return true;
      return false;
    },
  };
}

/** Where a change's keep stands: on its way, in the store, or failed (no room). */
export function watchKeep<Receipt>(kept: Promise<Receipt>): () => "going" | "kept" | "failed" {
  let state: "going" | "kept" | "failed" = "going";
  void kept.then(() => { state = "kept"; }, () => { state = "failed"; });
  return () => state;
}
