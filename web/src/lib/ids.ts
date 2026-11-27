import { nameUUID, randomUUID } from "./browserCrypto";

// Client-generated ids + fractional ordering.
// Leafen native APIs honour a client-provided `id` on insert, so we mint ids locally for
// instant optimistic updates and stable cross-row references.

/** A random UUID, on every origin (browserCrypto.ts). */
export function newId(): string {
  return randomUUID();
}

/**
 * A page's ID derived from its parts, the same for the same parts, as the
 * server derives one (derivedPageID, page_ids.go), so a page a request makes
 * is known before it is sent.
 */
export function derivedPageId(...parts: string[]): Promise<string> {
  return nameUUID(["leafen:page:v1", ...parts].join("\0"));
}

/**
 * Fractional position between two siblings, so inserts never require reindexing.
 * positionBetween(undefined, undefined) -> 1 (first item)
 * positionBetween(a, undefined) -> a + 1 (append)
 * positionBetween(undefined, b) -> b / 2 when positive, otherwise b - 1 (prepend)
 * positionBetween(a, b) -> midpoint
 *
 * Note: repeated midpoint inserts into the *same* gap (~50 in a row) can exhaust
 * double precision and collide. The app overwhelmingly appends (a+1), so this is
 * not hit in practice; a string-based fractional index or sibling reindex would
 * remove the edge entirely if it ever matters.
 */
export function positionBetween(a?: number, b?: number): number {
  if (a == null && b == null) return 1;
  if (a == null) {
    const upper = b as number;
    return upper > 0 ? upper / 2 : upper - 1;
  }
  if (b == null) return a + 1;
  return (a + b) / 2;
}
