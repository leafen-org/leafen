import { newId } from "./ids";
import { localBoxIfSettled, localEncryptionMode } from "./localLock";
import { serverScopedName } from "./serverOrigin";
import type { TextSpan } from "./types";

/*
 * What a field types, kept on this device at each key — before its typing
 * pauses and the value goes into the outbox (typedDraftSender.ts) — so a
 * browser killed or a phone's app ended by its system right after the last
 * key loses nothing: the value goes out when Leafen opens again here. The
 * value is put in a store of its own, past the outbox's lock between tabs
 * (deviceStoreLock.ts), as each key is typed. IndexedDB, not localStorage:
 * Chrome writes localStorage to the disk seconds later, and a value written
 * then and the browser killed was gone 6 times in 6 on a phone; one put in
 * IndexedDB stayed each time (2026-11-27, a Galaxy A90's Chrome stopped by
 * force; Notion's web app keeps each key's change in IndexedDB the same way).
 * Under the passphrase lock it is kept sealed only, as the outbox's changes
 * are (localLock.ts), a moment later; with the lock not yet opened it is
 * kept in memory until its field sends it.
 *
 * Each tab holds a lock of its own while it lives; a record of a tab whose
 * lock is gone — a tab closed, reloaded, its browser killed — is taken
 * when Leafen opens again (`takeLeft`); a living tab sends its own.
 */

export type TypedValue = { title: string; spans?: TextSpan[] };

/** A field's value as typed, the change it goes out as named by its ID. */
export type TypedDraft = {
  /**
   * What it writes: "page:<id>:title", "row:<rowId>:<propertyId>", a name
   * typed in its field ("name:<record>"), or a page's content changes
   * collected as typed ("change:<mutationId>", typedDraftSender.ts).
   */
  recordKey: string;
  /**
   * How it goes out when its tab is gone (typedDraftSender.ts): as a page's
   * title, as one of a row's values, or as a change of a page's content.
   */
  kind: "pageTitle" | "rowValue" | "graph";
  /** A row's value: the row, its property and the property's type; its words are `value`'s. */
  row?: { rowId: string; propertyId: string; type: string };
  /** A change of a page's content: what it writes, as it goes to `hostPageId`. */
  operations?: unknown[];
  /** The ID of the change it goes out as: the one its field sends it under, so the server takes it once. */
  mutationId: string;
  /** The page whose changes it goes in turn with. */
  hostPageId: string;
  /** A title's or a row's value, as typed. */
  value?: TypedValue;
  /** What it was typed on, as its change goes out with it. */
  base?: unknown;
};

type Row = { key: string; tabId: string; seq: number; typedAt: number; draft?: TypedDraft; sealed?: unknown };

const STORE = "drafts";
/** This document's own: a reload is a new tab here. */
const TAB_ID = newId();
const aliveLock = (tabId: string) => `leafen:tab-alive:${tabId}`;

function request<T>(made: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    made.onsuccess = () => resolve(made.result);
    made.onerror = () => reject(made.error);
  });
}

function done(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onerror = () => reject(transaction.error);
    transaction.onabort = () => reject(transaction.error);
  });
}

export type TypedDrafts = ReturnType<typeof openTypedDrafts>;

const opened = new Map<string, ReturnType<typeof createTypedDrafts>>();

/** The account's typed records on this device (one store for the account, under the server here). */
export function openTypedDrafts(userId: string) {
  const name = serverScopedName(`leafen-typed:${userId}`);
  let drafts = opened.get(name);
  if (!drafts) {
    drafts = createTypedDrafts(name, userId);
    opened.set(name, drafts);
  }
  return drafts;
}

function createTypedDrafts(name: string, userId: string) {
  let alive: Promise<void> | null = null;
  // Held while this document lives: other tabs leave its records to it.
  const holdAlive = () => alive ??= new Promise<void>((held) => {
    const locks = typeof navigator === "undefined" ? undefined : navigator.locks;
    if (!locks) return held();
    void locks.request(aliveLock(TAB_ID), () => { held(); return new Promise<void>(() => undefined); }).catch(() => held());
  });
  let database: Promise<IDBDatabase | null> | null = null;
  const open = () => database ??= (async () => {
    if (typeof indexedDB === "undefined") return null;
    await holdAlive();
    const opening = indexedDB.open(name, 1);
    opening.onupgradeneeded = () => {
      if (!opening.result.objectStoreNames.contains(STORE)) opening.result.createObjectStore(STORE, { keyPath: "key" });
    };
    const made = await request(opening);
    // Another tab's newer version, or the store cleared: opened again when next needed.
    made.onversionchange = () => { made.close(); database = null; };
    return made;
  })().catch(() => { database = null; return null; });

  // A record's key in the store: this tab's and its record's, which under the
  // passphrase lock says nothing of what it writes.
  const sealedKeys = new Map<string, string>();
  const rowKey = (recordKey: string) => localEncryptionMode() === "passphrase"
    ? `${TAB_ID}|${sealedKeys.get(recordKey) ?? sealedKeys.set(recordKey, newId()).get(recordKey)!}` : `${TAB_ID}|${recordKey}`;
  const lockBox = () => {
    const gate = localBoxIfSettled(userId);
    return gate && gate !== "device" && gate !== "pending" ? gate : null;
  };

  // The newest value of each record, put together after the key's own task.
  const waiting = new Map<string, Row>();
  let seq = 0;
  let flushing = false;
  const flush = async () => {
    const rows = [...waiting.values()];
    waiting.clear();
    flushing = false;
    const db = await open();
    if (!db || !rows.length) return;
    const sealed = localEncryptionMode() === "passphrase";
    const box = sealed ? lockBox() : null;
    // Without the lock opened it can't be kept sealed: it waits for its field's send.
    if (sealed && !box) return;
    const stored = await Promise.all(rows.map(async (row) => box
      ? { key: row.key, tabId: row.tabId, seq: row.seq, typedAt: row.typedAt, sealed: await box.seal(row.draft, name) } : row));
    const transaction = db.transaction(STORE, "readwrite");
    const store = transaction.objectStore(STORE);
    // A sealed one finished late doesn't write over a newer value.
    for (const row of stored) {
      const reading = store.get(row.key);
      reading.onsuccess = () => {
        const held = reading.result as Row | undefined;
        if (!held || held.seq < row.seq) store.put(row);
      };
    }
    await done(transaction);
  };

  return {
    /** Keeps the record's value as typed now: put after the key's own task, one put a record. Answers its turn (settle). */
    keep(draft: TypedDraft): number {
      seq += 1;
      waiting.set(draft.recordKey, { key: rowKey(draft.recordKey), tabId: TAB_ID, seq, typedAt: Date.now(), draft });
      if (!flushing) {
        flushing = true;
        queueMicrotask(() => { void flush().catch(() => undefined); });
      }
      return seq;
    },
    /** Takes the record off once what it held went, unless it was typed again since (its turn past `upTo`). */
    async settle(recordKey: string, upTo: number) {
      const pending = waiting.get(recordKey);
      if (pending && pending.seq <= upTo) waiting.delete(recordKey);
      const db = await open();
      if (!db) return;
      const key = rowKey(recordKey);
      const transaction = db.transaction(STORE, "readwrite");
      const store = transaction.objectStore(STORE);
      const reading = store.get(key);
      reading.onsuccess = () => {
        const held = reading.result as Row | undefined;
        if (held && held.seq <= upTo) store.delete(key);
      };
      await done(transaction);
    },
    /**
     * The records the tabs gone left — closed, reloaded, their browser killed
     * — oldest first, the newest of each record; sealed ones only with the
     * lock open. `taken` takes them off once they went where they go next.
     */
    async takeLeft(): Promise<{ drafts: TypedDraft[]; taken: () => Promise<void> }> {
      const none = { drafts: [], taken: async () => undefined };
      const db = await open();
      if (!db) return none;
      const rows = await request(db.transaction(STORE, "readonly").objectStore(STORE).getAll()) as Row[];
      const locks = typeof navigator === "undefined" ? undefined : navigator.locks;
      const held = new Set((await locks?.query().catch(() => null))?.held?.map((lock) => lock.name) ?? []);
      const left = rows.filter((row) => row.tabId !== TAB_ID && !held.has(aliveLock(row.tabId)));
      const box = lockBox();
      const read: { key: string; typedAt: number; draft: TypedDraft }[] = [];
      for (const row of left) {
        const draft = row.sealed === undefined ? row.draft : box ? await box.open<TypedDraft>(row.sealed, name).catch(() => undefined) : undefined;
        if (draft?.recordKey && draft.mutationId) read.push({ key: row.key, typedAt: row.typedAt, draft });
      }
      const newest = new Map<string, (typeof read)[number]>();
      for (const item of read.sort((a, b) => a.typedAt - b.typedAt)) newest.set(item.draft.recordKey, item);
      return {
        drafts: [...newest.values()].map(({ draft }) => draft),
        async taken() {
          const transaction = db.transaction(STORE, "readwrite");
          for (const { key } of read) transaction.objectStore(STORE).delete(key);
          await done(transaction);
        },
      };
    },
    /** Forgets every record (the device's changes discarded, signed out). */
    async discard() {
      waiting.clear();
      const db = await open();
      if (!db) return;
      const transaction = db.transaction(STORE, "readwrite");
      transaction.objectStore(STORE).clear();
      await done(transaction);
    },
  };
}
