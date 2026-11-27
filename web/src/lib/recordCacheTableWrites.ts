/**
 * What a store last wrote each table with, a few tables at a time, so a table
 * written again with most of its values the very same objects (a graph's row
 * parts, each sealed once; localFirstProductCache.ts) writes only those that
 * changed (2026-11-11: a value another person changed in a table of 100,000
 * rows wrote its hundred sealed parts again, some 20 ms of copying on the
 * page's thread each time). It holds only while the table is as written
 * here: each write marks the records it puts as its own, and a record the
 * last write put that reads otherwise (another tab's write, an eviction, a
 * lock change sealing it again) has the table written whole.
 */
const KEPT_TABLES = 8;

export type RecordCacheTableWrite = {
  /** The values written, by record; an object held weakly, as a value no one holds can't be written again. */
  values: Map<string, unknown>;
  /** The mark the records it put carry, and one of them to check it on. */
  writer: string;
  checkId: string | null;
};

/** What a table write keeps of its records' values. */
export function writtenValues(records: readonly { id: string; value: unknown }[]) {
  return new Map(records.map(({ id, value }) => [
    id, typeof value === "object" && value !== null ? new WeakRef(value) : value,
  ]));
}

/** Whether a table write wrote this very value under the record's id. */
export function wroteValue(write: RecordCacheTableWrite, id: string, value: unknown) {
  if (!write.values.has(id)) return false;
  const kept = write.values.get(id);
  return kept instanceof WeakRef ? kept.deref() === value : kept === value;
}

export function createRecordCacheTableWrites() {
  const instance = Math.random().toString(36).slice(2);
  const tables = new Map<string, RecordCacheTableWrite>();
  let written = 0;
  return {
    /** What the table was last written with here, forgotten until written again. */
    take(table: string) {
      const write = tables.get(table);
      tables.delete(table);
      return write;
    },
    /** A mark for the records a write puts. */
    nextWriter() {
      written += 1;
      return `${instance}:${written}`;
    },
    remember(table: string, write: RecordCacheTableWrite) {
      tables.delete(table);
      tables.set(table, write);
      if (tables.size > KEPT_TABLES) tables.delete(tables.keys().next().value!);
    },
    /** A table written otherwise, or all of them, is written whole next. */
    forget(table?: string) {
      if (table === undefined) tables.clear();
      else tables.delete(table);
    },
  };
}
