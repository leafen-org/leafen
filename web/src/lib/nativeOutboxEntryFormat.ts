import { OUTBOX_OPERATION_SCHEMA_VERSION, type OutboxOp } from "./outboxOperationTypes";

// The format of a page's change kept in the outbox, the one place the page
// outbox (nativePageMutationOutbox.ts) writes and reads its entries through.
// A change is kept inside a `native_entry` that names the format of the app
// that kept it (its schemaVersion, stamped as it is kept). An app leaves an
// entry of a format it doesn't know kept, as one it can't read, for an app
// that knows it: sent by an older app, what that app doesn't know of the
// change would go without it (2026-11-18: a block change's `typing`, the text
// typed before it, left out by an app from before it, and the words with it).
// Apps from before entries named their format can't read a `native_entry`
// at all, and leave it too. An entry kept before then reads as it is.

function recordOf(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

/** A page's change as the outbox keeps it, its format stamped as it is kept (normalizeOutboxOperation). */
export function formattedOutboxEntry(entry: object): Extract<OutboxOp, { kind: "native_entry" }> {
  return { entry: entry as Record<string, unknown>, kind: "native_entry" };
}

/**
 * The change an outbox entry keeps, as this app reads it, or "unreadable"
 * for one of a format this app doesn't know (or of no shape), which stays
 * kept for an app that knows it.
 */
export function keptOutboxEntry(stored: unknown): Record<string, unknown> | "unreadable" {
  const value = recordOf(stored);
  if (!value) return "unreadable";
  const format = value.schemaVersion;
  if (value.kind !== "native_entry") {
    // Kept before entries named their format: read as it is.
    return typeof format === "number" && format > OUTBOX_OPERATION_SCHEMA_VERSION ? "unreadable" : value;
  }
  if (!Number.isSafeInteger(format) || (format as number) > OUTBOX_OPERATION_SCHEMA_VERSION) return "unreadable";
  return recordOf(value.entry) ?? "unreadable";
}
