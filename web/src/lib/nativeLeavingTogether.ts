import { coalesceGraphOperation, graphOperationKey, graphOperationSubject, graphWireBounds, type GraphOperation } from "@/contentGraph";
import type { DatabaseRowPageNewOption, DatabaseRowPageValue } from "@/databaseRowPageWire.generated";
import { newId } from "@/lib/ids";
import type { NativePageMetadataChange } from "./nativePageMetadata";
import type { NativeGraphMutation, NativeMetadataMutation } from "./nativePageLaneEntries";

// As a page goes, a page's changes made here and not yet answered — one on
// its way, the next waiting in the lane for that one's answer, or one made
// as the page goes — have no time to go one after another: the one waiting
// stayed on the device until Leafen was opened again there, and the server
// held the change before it meanwhile (2026-11-27: a letter typed in a
// table's title cell, a text cell or a row page's title and taken back with
// ⌘Z, the tab closed at once on a server answering in 400 ms; the letter
// stood until the page was opened again in that browser, and for good after
// a private window closed). Sent apart as the page goes, two requests reach
// the server in either order. So they go as one change of their own, with a
// new ID; each stays kept as it was and goes again in its turn after the next
// opening, where the server takes the one it already took once by its ID,
// and the rest after it: the page ends as it was left either way.

type Together = { change: NativeGraphMutation; members: string[] };

// What a change holds besides these makes it go by itself.
const CONTENT = new Set(["mutationId", "operations", "pageId"]);
const ROW_VALUES = new Set(["mutationId", "operations", "pageId", "rowValues"]);
// Operations the server takes in a change of their own (nativeContentGraphWriteCoordinator.ts).
const ALONE = new Set<string>(["deleteDataSource", "deleteProperty", "deleteRow", "restoreDateCommand"]);

const only = (change: NativeGraphMutation, fields: Set<string>) => Object.entries(change).every(([key, value]) => fields.has(key) || value === undefined);

// Content changes go as one change of the content route: their operations in
// the order made, each record once, a row's later values over its earlier
// ones, as the page's writes collect them (nativeContentGraphWriteCoordinator.ts).
function contentTogether(chain: readonly NativeGraphMutation[], least: number): Together | null {
  let operations = new Map<string, GraphOperation>();
  let subjects = new Map<string, string>();
  const members: string[] = [];
  for (const change of chain) {
    if (!only(change, CONTENT) || !Array.isArray(change.operations)) break;
    const next = new Map(operations), nextSubjects = new Map(subjects);
    let fits = true;
    for (const operation of change.operations as GraphOperation[]) {
      const key = graphOperationKey(operation), subject = graphOperationSubject(operation);
      const held = nextSubjects.get(subject);
      if (ALONE.has(operation.kind) || (held !== undefined && (held !== key || operation.kind !== "upsertRow"))) {
        fits = false;
        break;
      }
      next.set(key, coalesceGraphOperation(next.get(key), operation));
      nextSubjects.set(subject, key);
    }
    if (!fits || next.size > graphWireBounds.mutationOperations) break;
    operations = next;
    subjects = nextSubjects;
    members.push(change.mutationId);
  }
  if (members.length < least) return null;
  return { change: { mutationId: newId(), operations: [...operations.values()], pageId: chain[0].pageId }, members };
}

// A row's values written by its page's route (rowValuesKept.ts), each over
// the values the server held as it was made (`expected`), go as one write of
// their row, its later values over its earlier ones, each held to what the
// first of them to write it expected — but the one on its way
// (`oldestOnItsWay`): the server may hold it already, which what it expected
// would refuse.
function rowValuesTogether(chain: readonly NativeGraphMutation[], least: number, oldestOnItsWay: boolean): Together | null {
  const rowId = chain[0]?.rowValues?.rowId;
  const values: Record<string, DatabaseRowPageValue> = {}, expected: Record<string, DatabaseRowPageValue> = {};
  const options = new Map<string, DatabaseRowPageNewOption[]>();
  const members: string[] = [];
  for (const [index, change] of chain.entries()) {
    const row = change.rowValues, input = row?.input;
    if (!only(change, ROW_VALUES) || change.operations.length > 0 || !row || !input || row.rowId !== rowId
      || row.taskStatus || row.presentation || input.restoreDateCommand || input.redo) break;
    const holds = index > 0 || !oldestOnItsWay;
    for (const [property, value] of Object.entries(input.values)) {
      if (!(property in values) && holds && input.expected && property in input.expected) expected[property] = input.expected[property];
      values[property] = value;
      const made = input.options?.[property];
      if (made) options.set(property, made);
      else options.delete(property);
    }
    members.push(change.mutationId);
  }
  if (!rowId || members.length < least) return null;
  const input = { values, ...(Object.keys(expected).length ? { expected } : {}), ...(options.size ? { options: Object.fromEntries(options) } : {}) };
  return { change: { mutationId: newId(), operations: [], pageId: chain[0].pageId, rowValues: { rowId, input } }, members };
}

/**
 * What of a page's changes not yet answered here, oldest first, goes as one
 * change as the page goes, and which of them it holds; null when none can.
 * The oldest may be on its way: it goes with those behind it when it can, or
 * else by itself (nativePageMutationLeaving.ts) and they after it as one.
 * One alone goes as itself would: it may not have gone yet, its turn in the
 * lane still to come (2026-11-27: a ⌘Z's change waited between two steps of
 * the lane as the page went, on a NAS-like server), and sent twice it is
 * taken the same.
 */
export function changesGoingTogether(chain: readonly NativeGraphMutation[]): Together | null {
  if (chain.length === 0) return null;
  if (chain.length === 1) return contentTogether(chain, 1) ?? rowValuesTogether(chain, 1, false);
  const fromOldest = contentTogether(chain, 2) ?? rowValuesTogether(chain, 2, true);
  if (fromOldest) return fromOldest;
  const behind = chain.slice(1);
  return contentTogether(behind, 1) ?? rowValuesTogether(behind, 1, false);
}

/**
 * A page's settings changes not yet answered here, oldest first, as one
 * change as the page goes: on the page the oldest was made on, each one's
 * fields over the ones before it — a title with the marks it was written
 * with — naming them all (`after`), and with a new ID. Sent apart, the one
 * behind was refused for its base cursor once the one ahead of it landed
 * (2026-11-27: a page title's letter on its way and its ⌘Z made as the tab
 * closed; the letter stood until Leafen was opened again). It carries them
 * (`carriesAfter`): the server takes it past that cursor when the page's last
 * change is one of them, and them with it (page_metadata_after_own.go); each
 * stays kept as it was, answered as taken after the next opening. Null for one alone, which goes as itself,
 * and for a wiki's page, whose changes the wiki's route holds to the title
 * each was typed on (sendKeptPageMetadata).
 */
export function settingsGoingTogether(chain: readonly NativeMetadataMutation[]): NativeMetadataMutation | null {
  if (chain.length < 2 || chain.some(({ baseline }) => baseline.wikiDataSourceId)) return null;
  let change: NativePageMetadataChange = {};
  for (const member of chain) {
    // A title written again takes its own marks (none for plain words).
    const { titleSpans: _spans, ...words } = change;
    change = { ...(member.change.title === undefined ? change : words), ...member.change };
  }
  // A place taken only to keep an order shown stays so only when each one was.
  const { keepLastEdit: _kept, ...joined } = change;
  return { baseline: chain[0].baseline, change: chain.every((member) => member.change.keepLastEdit) ? change : joined, mutationId: newId(),
    pageId: chain[0].pageId, after: chain.map(({ mutationId }) => mutationId), carriesAfter: true };
}
