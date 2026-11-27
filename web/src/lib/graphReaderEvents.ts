// A page's rows are read beyond its own room: by a row's own page, and by the
// pages whose graphs carry its sources (linked databases, relations, rollups).
// Once such rows change, the server tells the open rooms reading them
// (ygo_room_graph_readers.go): "rows" to a row's own page, which reads its row
// again, and "external" to a page carrying another page's rows, which asks for
// their latest changes at once (useExternalGraphRefresh).

const GRAPH_READER_EVENT = "leafen:graph-reader";

export type GraphReaderWord = { kind: "rows" | "external"; pageId: string; revision: number };

// A page open in several places at once hears each word once.
const heard = new Map<string, number>();

/** Tells the readers of an open page that rows it reads changed. */
export function announceGraphReaderWord(word: GraphReaderWord) {
  if (typeof window === "undefined") return;
  const key = `${word.kind}:${word.pageId}`;
  if ((heard.get(key) ?? 0) >= word.revision) return;
  heard.set(key, word.revision);
  window.dispatchEvent(new CustomEvent<GraphReaderWord>(GRAPH_READER_EVENT, { detail: word }));
}

/** Follows the words the server gives open pages about rows they read. */
export function onGraphReaderWord(kind: GraphReaderWord["kind"], pageId: string, listener: () => void): () => void {
  if (typeof window === "undefined") return () => {};
  const handle = (event: Event) => {
    const detail = (event as CustomEvent<GraphReaderWord>).detail;
    if (detail?.kind === kind && detail.pageId === pageId) listener();
  };
  window.addEventListener(GRAPH_READER_EVENT, handle);
  return () => window.removeEventListener(GRAPH_READER_EVENT, handle);
}

/** The revision the server's awareness state gives a word, if it gives one. */
export function announcedGraphReaderRevision(state: unknown, key: "leafenRows" | "leafenExternal"): number {
  const revision = (state as Record<string, { revision?: unknown } | undefined> | null)?.[key]?.revision;
  return typeof revision === "number" && Number.isSafeInteger(revision) && revision > 0 ? revision : 0;
}
