import type { GraphDatabaseDelete } from "@/contentGraphWire.generated";

// A two-way relation to another database is deleted with its other side
// ("delete") or alone, its sync ended ("keep"), as the person chose in the
// confirmation (confirmPropertyDeletion). The choice waits here, by the
// property, for the deletion that sends it, wherever that deletion is made.

type RelatedPropertyChoice = NonNullable<GraphDatabaseDelete["relatedProperty"]>;

const chosen = new Map<string, RelatedPropertyChoice>();

/** Keeps what the person chose for a two-way relation about to be deleted. */
export function chooseRelatedPropertyDeletion(propertyId: string, choice: RelatedPropertyChoice) {
  chosen.set(propertyId, choice);
}

/** A property's deletion, carrying the choice made for its other side, if any. */
export function propertyDeletion(id: string): GraphDatabaseDelete {
  const relatedProperty = chosen.get(id);
  chosen.delete(id);
  return relatedProperty ? { id, relatedProperty } : { id };
}

/** The choices a change's deletions carry, by property. */
export type RelatedChoices = Readonly<Record<string, RelatedPropertyChoice>>;

/** What the deletions among operations chose for their other sides, so ⌘⇧Z sends the same. */
export function relatedChoicesOf(operations: readonly { kind: string; propertyDeletion?: GraphDatabaseDelete }[]): RelatedChoices {
  return Object.fromEntries(operations.flatMap((operation) => (
    operation.kind === "deleteProperty" && operation.propertyDeletion?.relatedProperty
      ? [[operation.propertyDeletion.id, operation.propertyDeletion.relatedProperty]] : []
  )));
}
