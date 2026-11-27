import type { GraphOperation, GraphProperty, GraphRowValue } from "@/contentGraph";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { restoreDatabaseProperty } from "@/lib/databaseDeletedProperties";
import type { Session } from "@/types";
import { notePageHistoryStep } from "../editor/pageUndo";
import { noteRowPropertiesRedone } from "./rowPropertyRedo";
import { relatedChoicesOf } from "@/lib/relatedPropertyDeletion";
import { rowsWhileTypeChanges } from "@/lib/propertyTypeChangeRows";

let steps = 0;

/**
 * The steps of a row page's ⌘Z that a change to its database's properties,
 * made on the row page away from its database, leaves, as a table's do
 * (useContentGraphWrites): a property made goes again, and one deleted
 * comes back from the database's deleted properties, as Notion's (seen
 * 2026-11-06, a row page's "+ 속성 추가" and "속성 삭제"); ⌘⇧Z makes or
 * deletes it again, a property made again opening its menu
 * (rowPropertyRedo). Each step reads the row page again.
 */
export function notePropertySteps({ session, rowId, holderPageId, operations, known, write, reload }: {
  session: Session;
  rowId: string;
  /** The page holding the database, whose properties change. */
  holderPageId: string;
  operations: readonly GraphOperation[];
  /** The properties there were before the change. */
  known: readonly GraphProperty[];
  /** Sends a change to the properties, and shows it on the row page. */
  write: (operations: GraphOperation[]) => Promise<unknown>;
  reload: () => Promise<unknown>;
}) {
  const made = operations.flatMap((operation) => (
    operation.kind === "upsertProperty" && !known.some(({ id }) => id === operation.property.id) ? [operation.property.id] : []
  ));
  const deleted = operations.flatMap((operation) => (
    operation.kind === "deleteProperty" && operation.propertyDeletion ? [operation.propertyDeletion.id] : []
  ));
  const taken = (change: () => Promise<unknown>) => async () => {
    try {
      await change();
      return true;
    } catch {
      return false;
    }
  };
  // A deletion goes alone, as the server takes it; a two-way relation as it went.
  const related = relatedChoicesOf(operations);
  const remove = (ids: readonly string[]) => taken(async () => {
    for (const id of ids) {
      await write([{ kind: "deleteProperty", propertyDeletion: related[id] ? { id, relatedProperty: related[id] } : { id } }]);
    }
  });
  const restore = (ids: readonly string[]) => taken(async () => {
    for (const id of ids) await restoreDatabaseProperty(session, holderPageId, id);
    await reload();
  });
  if (made.length) {
    notePageHistoryStep(rowId, `row-property-create/${made.join(",")}/${++steps}`, async (forward) => {
      if (!forward) return remove(made)();
      const restored = await restore(made)();
      if (restored) noteRowPropertiesRedone(made);
      return restored;
    });
  }
  if (deleted.length) {
    notePageHistoryStep(rowId, `row-property-delete/${deleted.join(",")}/${++steps}`, (forward) => (forward ? remove(deleted) : restore(deleted))());
  }
}

/**
 * A row page as read, with the changes to its database's properties made
 * since: a property's new type shows its value as the type will keep it, as
 * a table's cells do till the server's answer.
 */
export function withPropertyChanges(snapshot: DatabaseRowPageSnapshot, operations: readonly GraphOperation[]): DatabaseRowPageSnapshot {
  const saved = operations.flatMap((operation) => operation.kind === "upsertProperty" ? [operation.property] : []);
  const [row] = rowsWhileTypeChanges([snapshot.row as unknown as { values: Record<string, GraphRowValue> }],
    snapshot.properties as unknown as GraphProperty[], saved);
  let properties = snapshot.properties;
  for (const operation of operations) {
    if (operation.kind === "deleteProperty" && operation.propertyDeletion) {
      const { id } = operation.propertyDeletion;
      properties = properties.filter((property) => property.id !== id);
    } else if (operation.kind === "upsertProperty" && operation.property.dataSourceId === snapshot.row.dataSourceId) {
      const { id, name, type, config, position } = operation.property;
      const changed = { ...properties.find((property) => property.id === id), id, name, type, config: config ?? {}, position };
      properties = properties.some((property) => property.id === id)
        ? properties.map((property) => property.id === id ? changed : property) : [...properties, changed];
    }
  }
  return { ...snapshot, row: row as unknown as DatabaseRowPageSnapshot["row"], properties };
}
