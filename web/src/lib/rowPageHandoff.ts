import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import type { Page } from "@/lib/types";
import { handPageTextOver } from "@/lib/pageTextAhead";

/**
 * A database page opened whole from its peek (⤢, or the peek's view options)
 * draws at once from what the peek drew, its own read going behind, so the
 * page is there as its frame widens, as Notion's is (seen 2026-11-12 at
 * 1440×723: the row page is drawn in the frame through the whole widening).
 * Read from the server each time, it drew an empty frame for some 300 ms. It
 * is handed over for the page opening next, and only for a moment, with the
 * values the peek shows now: a title just typed there doesn't go back to the
 * one read when it opened.
 */
let handed: { snapshot: DatabaseRowPageSnapshot; at: number; takenAt?: number } | null = null;
const HANDED_FOR_MS = 3000;
// Taken by the page opening, it stays a moment for that same opening drawn
// again, which draws from it too: finding it gone, the second drawing stood
// empty while it read the row (some 300 ms on a NAS, 2026-11-17).
const TAKEN_STAYS_MS = 1000;
/** How long the page handed over waits to read itself again (StandaloneRowPage). */
export const HANDED_PAGE_READ_AFTER_MS = 1000;
/** The identity of a row page drawn from its row while its read is on its way (rowPageWhileReading.tsx). */
export const ROW_PAGE_READING = "reading:";

/**
 * Hands the row page a peek drew, with the values it shows, to its own page
 * opening next; its title as written in the peek's page (`drawn`), though
 * not yet saved.
 */
export function handRowPageOver(snapshot: DatabaseRowPageSnapshot, shown?: Page, drawn?: Element | null) {
  // A peek still reading its page has none to hand over.
  if (snapshot.snapshotIdentity.startsWith(ROW_PAGE_READING)) return;
  const typed = drawn?.querySelector("[data-row-page-title]")?.textContent ?? undefined;
  handed = { snapshot: shown ? withShownValues(snapshot, shown, typed) : snapshot, at: Date.now() };
  // Its body's words go with it (pageTextAhead.ts).
  handPageTextOver(snapshot.row.id);
}

/** The row page a peek handed over for this row, if just now; taking it lets it go a moment later. */
export function rowPageHandedOver(rowId: string, take = false): DatabaseRowPageSnapshot | null {
  const now = Date.now();
  const kept = handed && handed.snapshot.row.id === rowId && now - handed.at < HANDED_FOR_MS
    && (handed.takenAt === undefined || now - handed.takenAt < TAKEN_STAYS_MS) ? handed.snapshot : null;
  if (take && kept && handed) handed.takenAt ??= now;
  else if (take) handed = null;
  return kept;
}

function withShownValues(snapshot: DatabaseRowPageSnapshot, shown: Page, typedTitle?: string): DatabaseRowPageSnapshot {
  if (shown.id !== snapshot.row.id || !shown.properties) return snapshot;
  const values = { ...snapshot.row.values };
  for (const [propertyId, cell] of Object.entries(values)) {
    if (!(propertyId in shown.properties)) continue;
    const title = cell.type === "title";
    const value = shown.properties[propertyId], spans = title ? shown.titleSpans : shown.propertySpans?.[propertyId];
    // A title typed and not yet saved shows as typed, its marks with its save;
    // one holding mentions shows as saved, their names drawn apart from it.
    const typed = title && typedTitle !== undefined && typedTitle !== String(value ?? "") && !spans?.some((span) => span.mention);
    if (typed) values[propertyId] = { type: cell.type, value: typedTitle };
    else values[propertyId] = { type: cell.type, value, ...(spans?.length ? { spans: spans as typeof cell.spans } : {}) };
  }
  return { ...snapshot, row: { ...snapshot.row, values } };
}
