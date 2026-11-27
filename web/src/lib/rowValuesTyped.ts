import type { DatabaseRowPageValue } from "@/databaseRowPageWire.generated";
import type { Session } from "@/types";
import { sendRowValues } from "./rowValuesKept";
import { onTypedDraftsDiscarded, rowValueRecord, typedField, type TypedField } from "./typedDraftSender";

/*
 * A row's value typed into its field — a table's title, text, URL, email or
 * phone cell, a row's text value in its page — is one edit: it shows at
 * each key, is kept on this device at each key (typedDraftSender.ts), so a
 * browser killed or a phone's app ended mid-word loses nothing, and is saved
 * once, as the typing pauses for TYPING_PAUSE_MS or ends otherwise (Enter,
 * Escape, Tab, the field left, ⌘Z, the page hidden or going), as one change
 * of the row's values through its page's route (rowValuesKept.ts) under the
 * ID its record holds: one save, which a database's automations hear once
 * (typedWrites.ts). A title or a text goes over the value its typing began
 * on, merged with another the server holds by then (sendKeptRowValues).
 */

type Held = { field: TypedField; hostPageId: string; base: DatabaseRowPageValue | null; answers: Array<(saved: boolean) => void>; go?: () => void };
const held = new Map<string, Held>();
// Discarded with what the device kept typed, each held value is given up, its edit unwound.
onTypedDraftsDiscarded(() => {
  const given = [...held.values()];
  held.clear();
  for (const entry of given) for (const answer of entry.answers.splice(0)) answer(false);
  letGo();
});

/**
 * Whether a value typed into a row's field is held still on the page, not
 * yet in the outbox: the page's reads wait for it, as for its other writes
 * on their way (nativeContentGraphWriteCoordinator.ts), so a read meanwhile
 * doesn't put the value it had back under the typing.
 */
export function typedRowValueHeldOn(pageId: string) {
  for (const entry of held.values()) if (entry.hostPageId === pageId) return true;
  return false;
}
const letGoListeners = new Set<() => void>();
/** Hears each held value go — sent, its change on its way as the outbox tells, or given up. */
export function onTypedRowValueLetGo(listener: () => void): () => void {
  letGoListeners.add(listener);
  return () => { letGoListeners.delete(listener); };
}
const letGo = () => { for (const listener of [...letGoListeners]) listener(); };

/** Sends at once the values typed and held on the page: another change it makes goes after them, in the order made (typedWrites.ts). */
export function sendTypedRowValuesNow(pageId: string) {
  for (const entry of [...held.values()]) if (entry.hostPageId === pageId) entry.go?.();
}

/** A row's value as typed now, the page its changes go with, and the value it held before this key (`before`). */
export type TypedRowValue = { rowId: string; propertyId: string; value: DatabaseRowPageValue; hostPageId: string; before?: unknown };

/**
 * Holds a row's value typed: kept on the device now, and `send` — given the
 * value the typing began on, a title's or a text's — run once the typing
 * pauses or ends, for the latest value held alone; each value held answers
 * whether that save was taken, so a refused one unwinds the whole edit.
 * `send` writes it through sendRowValues, which takes the record's ID.
 */
export function holdTypedRowValue(typed: TypedRowValue, send: (base: DatabaseRowPageValue | null) => Promise<boolean>): Promise<boolean> {
  const { rowId, propertyId, value } = typed, key = rowValueRecord(rowId, propertyId);
  const entry = held.get(key) ?? { field: typedField(), hostPageId: typed.hostPageId, base: wordsBase(value.type, typed.before), answers: [] };
  held.set(key, entry);
  return new Promise<boolean>((answer) => {
    entry.answers.push(answer);
    entry.go = () => {
      if (held.get(key) !== entry) return;
      held.delete(key);
      const answers = entry.answers.splice(0);
      const saved = entry.field.send(() => send(entry.base).catch(() => false));
      letGo();
      void saved.then((ok) => { for (const settle of answers.reverse()) settle(ok); });
    };
    entry.field.key({ recordKey: key, kind: "rowValue", hostPageId: typed.hostPageId, row: { rowId, propertyId, type: value.type },
      value: { title: typeof value.value === "string" ? value.value : "", ...(value.spans?.length ? { spans: value.spans.map((span) => ({ ...span })) } : {}) },
      base: entry.base }, entry.go);
  });
}

// The value a title or a text was typed on, which its write goes over; none for any other value, which goes as typed.
function wordsBase(type: string, before: unknown): DatabaseRowPageValue | null {
  if (type !== "title" && type !== "rich_text") return null;
  const held = before && typeof before === "object" ? before as Partial<DatabaseRowPageValue> : {};
  return { type, value: typeof held.value === "string" ? held.value : "", ...(held.spans?.length ? { spans: held.spans.map((span) => ({ ...span })) } : {}) };
}

// The values a person types into their field: words, a link, an email address, a phone number.
const TYPED_VALUE_TYPES = new Set(["title", "rich_text", "url", "email", "phone"]);

/** A row's change that is one value typed into its field, which goes as typed values do (typedRowValueSent); null for any other. */
export function typedRowValueIn(row: { id: string; values: Record<string, { type: string; value: unknown; spans?: unknown }> }) {
  const entries = Object.entries(row.values), [propertyId, value] = entries[0] ?? [];
  return entries.length === 1 && value && TYPED_VALUE_TYPES.has(value.type)
    ? { rowId: row.id, propertyId, value: value as DatabaseRowPageValue } : null;
}

/**
 * A table's cell typed in — a title, a text, a link, an address — held,
 * kept and sent as holdTypedRowValue says, through the row's page's route
 * with the page showing it (`pageId`); answers whether it was saved, a
 * merged one (rowValuesKept.ts) given to `merged` to show.
 */
export function typedRowValueSent(session: Session, typed: NonNullable<ReturnType<typeof typedRowValueIn>>, pageId: string, before: unknown,
  merged: (value: DatabaseRowPageValue) => void): Promise<boolean> {
  const { rowId, propertyId, value } = typed;
  return holdTypedRowValue({ rowId, propertyId, value, hostPageId: pageId, before }, (base) => sendRowValues(session, rowId, {
    values: { [propertyId]: value }, ...(base ? { expected: { [propertyId]: base } } : {}) }, pageId).then((answer) => {
    const written = answer?.merged?.[propertyId];
    if (written) merged(written);
    return true;
  }));
}
