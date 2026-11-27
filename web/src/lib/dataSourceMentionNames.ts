// A data source's mention is the source's own, as Notion's (user decision
// 2026-11-22): the server keeps it as a database mention named by the
// source's address, collection://<its ID> (data_source_mentions.go), one
// nothing here answers to by the ID written. Its name follows the source's
// renames as Notion's does, read as it comes on screen
// (DataSourceMentionStyles.tsx); a mention reads it from here
// (peopleDirectory.ts withCurrentMentionName), by its address.
const DATA_SOURCE_ADDRESS = "collection://";

let names: ReadonlyMap<string, string> = new Map();
const listeners = new Set<() => void>();

/** The ID a data source's mention names by its address, undefined for any other mention. */
export function dataSourceMentionId(span: { mention?: string; databaseId?: string }) {
  if (span.mention !== "database" || !span.databaseId?.startsWith(DATA_SOURCE_ADDRESS)) return undefined;
  return span.databaseId.slice(DATA_SOURCE_ADDRESS.length) || undefined;
}

/** The address a data source's mention names it by. */
export function dataSourceMentionAddress(dataSourceId: string) {
  return DATA_SOURCE_ADDRESS + dataSourceId;
}

/** The mentioned data sources' names as read now, by their addresses; their mentions follow them. */
export function setDataSourceMentionNames(next: ReadonlyMap<string, string>) {
  if (next.size === names.size && [...next].every(([address, name]) => names.get(address) === name)) return;
  names = next;
  for (const listener of listeners) listener();
}

/** A mentioned data source's name as read, by its mention's address. */
export function dataSourceMentionName(address: string) {
  return names.get(address);
}

/** Whether a data source's mention names nothing here: it came in its reader's not-found marks (closedMentions.ts). */
export function dataSourceMentionNamesNothing(span: { mention?: string; databaseId?: string }, missingMarks: boolean) {
  return missingMarks && dataSourceMentionId(span) !== undefined;
}

/** Hears of each change of the names read (peopleDirectory.ts redraws the mentions). */
export function onDataSourceMentionNames(listener: () => void) {
  listeners.add(listener);
}
