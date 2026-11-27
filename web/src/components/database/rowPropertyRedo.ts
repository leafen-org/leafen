const listeners = new Set<(ids: readonly string[]) => void>();

/**
 * Properties a ⌘⇧Z made again. A row page showing them opens the first's
 * menu at its name, as Notion's row page and side peek do (seen 2026-11-10
 * and 2026-11-11); a table's header opens nothing.
 */
export function noteRowPropertiesRedone(ids: readonly string[]) {
  if (ids.length) for (const listener of listeners) listener(ids);
}

export function listenForRowPropertiesRedone(listener: (ids: readonly string[]) => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}
