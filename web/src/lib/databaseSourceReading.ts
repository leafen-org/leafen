/**
 * The most sources the server gives in one read, all one read of it goes
 * through (database_container_picker.go), so the hundred a window counts
 * come in one round trip, not four.
 */
export const SOURCES_READ_AT_MOST = 100;

/**
 * Reads a workspace's sources to link or relate to, page by page: one page
 * (`limit` left to the reader), or with `upTo` — searched or not — a page
 * after another until that many are read (those `keep` leaves out not
 * counted) or none are left, so a window counts all it holds or found, not
 * one page — the hundred Notion's source windows read (linkableSources'
 * EXISTING_COUNTED). `after` goes on from a page read before; `nextCursor`
 * is where the next page starts.
 */
export async function readSourcePages<T>(
  read: (cursor: string | undefined, limit: number | undefined) => Promise<{ items: T[]; nextCursor?: string }>,
  { upTo, after, keep }: { upTo?: number; after?: string; keep?: (item: T) => boolean } = {},
): Promise<{ items: T[]; nextCursor?: string }> {
  const items: T[] = [];
  let cursor = after;
  do {
    const response = await read(cursor, upTo === undefined ? undefined : Math.min(SOURCES_READ_AT_MOST, upTo - items.length));
    items.push(...(keep ? response.items.filter(keep) : response.items));
    cursor = response.nextCursor;
  } while (upTo !== undefined && cursor && items.length < upTo);
  return { items: upTo === undefined ? items : items.slice(0, upTo), nextCursor: cursor };
}
