/**
 * Whether a database item's values take no changes from the reader: its page
 * is locked for them, or its sharing is unlinked from its database's and
 * they may only view it.
 */
export function rowValuesLocked(row: { lockedForMe?: boolean; readOnlyForMe?: boolean } | undefined): boolean {
  return Boolean(row?.lockedForMe || row?.readOnlyForMe);
}
