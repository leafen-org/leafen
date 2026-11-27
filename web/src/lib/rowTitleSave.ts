import { readDatabaseRowPage } from "@/databaseRowPageApi";
import { databaseRowPageErrorCodes } from "@/databaseRowPageWire.generated";
import { rowNotYetMade } from "@/lib/rowMadeHere";
import type { TitleValue } from "@/lib/titleMerge";
import { sendTitleOverBase } from "@/lib/typedTitleSend";
import type { TextSpan } from "@/lib/types";
import type { Session } from "@/types";

// A row's title — a database row page's, a wiki page's — is its title value,
// written whole. What one window typed on the title it had is merged with
// the title the server holds when it is written, so typing saved meanwhile
// elsewhere (this person's other window, another person) stays with it, as a
// page's title is merged with the title it meets (sendKeptPageMetadata,
// titleMerge.ts; 2026-11-10: two windows typing a row's title at once kept
// only the later). It is written only while the server still holds the
// title it was merged with: one saved in between, the same instant, refuses
// it, and it is merged with that one and sent again (2026-11-11: two saves
// 20ms apart both read the same title, and the later wrote over the other).

/** A row's title (`propertyId` none) or one of its values as words, as the server holds it now; null when it can't be read. */
export async function rowValueNow(session: Session, rowId: string, propertyId?: string): Promise<TitleValue | null> {
  try {
    const { row } = await readDatabaseRowPage(session, rowId, { limit: 1 });
    const cell = propertyId === undefined ? Object.values(row.values).find((value) => value.type === "title") : row.values[propertyId];
    const title = typeof cell?.value === "string" ? cell.value : "";
    return cell?.spans?.length ? { title, spans: cell.spans as TextSpan[] } : { title };
  } catch {
    return null;
  }
}

/** Whether a write was refused for the value it was read with having changed since. */
export function isRowValueChanged(error: unknown) {
  return typeof error === "object" && error !== null && Reflect.get(error, "code") === databaseRowPageErrorCodes.valueChanged;
}

/**
 * Writes a row's title `typed` on `base`, the server's title the typing was
 * made on, by the one rule a typed title goes by (typedTitleSend.ts): merged
 * with the server's title now when it has changed since; as typed when it
 * hasn't or can't be read. `write` is given the title the server held
 * (`read`), which the server holds it to; refused for another title saved
 * since, it is read, merged and written again. The last try, and a write
 * that can't hold the server to a title, writes it as merged.
 * A row made here that the server doesn't have yet has no title elsewhere to
 * meet: it is written as typed, at once (2026-11-12: on a stalled network
 * the title typed in a new row's page waited for the server to be read,
 * and its row in the table stayed untitled). Any other is first written as
 * typed over `base` at once, the server read only once it has refused that:
 * the write is the title's step of the page's ⌘Z, which goes where the
 * typing did (2026-11-11: read first, the step came after the words typed
 * in the body meanwhile, and the first ⌘Z there emptied the title).
 */
export async function writeRowTitle<Result>(
  session: Session, rowId: string, typed: TitleValue, base: TitleValue,
  write: (title: TitleValue, read: TitleValue | null) => Promise<Result>,
): Promise<Result> {
  if (rowNotYetMade(rowId)) return write(typed, null);
  return sendTitleOverBase<TitleValue, Result>({
    base, typed, readNow: () => rowValueNow(session, rowId), titleOf: (now) => now, changed: isRowValueChanged, overLast: true,
    write: (title, now) => write(title, now === undefined ? base : now),
  });
}
