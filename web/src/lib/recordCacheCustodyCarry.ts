import type { LocalSecretBox } from "./localSecretBox";
import type {
  RecordCacheIndexedDbStorage,
  RecordCacheStoredEntry,
  RecordCacheStoredRecord,
  RecordCacheStoredStep,
} from "./recordCacheIndexedDbStorage";
import { recordCacheValueContext } from "./recordCacheSealedStorage";

// Turning the passphrase lock on or off keeps the device's copies: each kept
// value is opened with the key it was sealed with and sealed again with the
// next one, a step at a time (2026-11-07: they were dropped, so pages read
// before the lock no longer opened offline). Nothing is left readable
// without the passphrase while it's on: a value the old key can't open goes.
//
// A note kept in the clear says the store is mid-way: a store left so by a
// closed tab, or carried for a lock change that wasn't saved after all, is
// sealed in part or wholly with a key the device doesn't use now, and is
// cleared when it next opens (settleRecordCacheCustody).

const CUSTODY_CHANGE_META_KEY = "__recordCacheCustodyChange";
const CARRY_STEP = 100;

/** Which key a store's values are sealed with: the device's, the passphrase's, or none. */
export type RecordCacheCustodyName = "device" | "passphrase" | "plaintext";

export interface RecordCacheCustodySide {
  /** The key, or null where values are kept in the clear. */
  box: LocalSecretBox | null;
  name: RecordCacheCustodyName;
}

interface CustodyChangeNote {
  done: boolean;
  to: RecordCacheCustodyName;
}

function custodyChangeNote(value: unknown): CustodyChangeNote | null {
  if (!value || typeof value !== "object") return null;
  const done = Reflect.get(value, "done");
  const to = Reflect.get(value, "to");
  return typeof done === "boolean" && (to === "device" || to === "passphrase" || to === "plaintext")
    ? { done, to }
    : null;
}

async function resealed(
  value: unknown,
  context: string,
  from: RecordCacheCustodySide,
  to: RecordCacheCustodySide,
): Promise<unknown> {
  const plain = from.box ? await from.box.open(value, context) : value;
  if (plain === undefined) return undefined;
  return to.box ? await to.box.seal(plain, context) : plain;
}

export async function carryRecordCacheCustody(
  storage: RecordCacheIndexedDbStorage,
  change: { from: RecordCacheCustodySide; storageName: string; to: RecordCacheCustodySide },
): Promise<void> {
  const { from, storageName, to } = change;
  await storage.setMeta(CUSTODY_CHANGE_META_KEY, { done: false, to: to.name } satisfies CustodyChangeNote);
  let afterKey: string | null = null;
  do {
    const step: RecordCacheStoredStep<RecordCacheStoredRecord> = await storage.listStoredRecords(afterKey, CARRY_STEP);
    const records: RecordCacheStoredRecord[] = [];
    for (const record of step.entries) {
      const context = recordCacheValueContext(storageName, "record", record.table, record.id);
      records.push({ ...record, value: await resealed(record.value, context, from, to) });
    }
    await storage.rewriteStored({ meta: [], records });
    afterKey = step.nextAfterKey;
  } while (afterKey !== null);
  do {
    const step: RecordCacheStoredStep<RecordCacheStoredEntry> = await storage.listStoredMeta(afterKey, CARRY_STEP);
    const meta: RecordCacheStoredEntry[] = [];
    for (const entry of step.entries) {
      if (entry.key === CUSTODY_CHANGE_META_KEY) continue;
      const context = recordCacheValueContext(storageName, "meta", entry.key);
      meta.push({ key: entry.key, value: await resealed(entry.value, context, from, to) });
    }
    await storage.rewriteStored({ meta, records: [] });
    afterKey = step.nextAfterKey;
  } while (afterKey !== null);
  await storage.setMeta(CUSTODY_CHANGE_META_KEY, { done: true, to: to.name } satisfies CustodyChangeNote);
}

/**
 * Settles a store a lock change carried, as it opens with the key the device
 * uses now: kept once that is the key it was carried to and the carry was
 * finished, cleared otherwise.
 */
export async function settleRecordCacheCustody(
  storage: RecordCacheIndexedDbStorage,
  current: RecordCacheCustodyName,
): Promise<void> {
  const stored = await storage.getMeta(CUSTODY_CHANGE_META_KEY);
  if (stored === undefined) return;
  const note = custodyChangeNote(stored);
  if (note?.done && note.to === current) {
    await storage.removeMeta(CUSTODY_CHANGE_META_KEY);
    return;
  }
  await storage.clear();
}
