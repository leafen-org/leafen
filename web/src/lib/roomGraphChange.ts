import { compareGraphOrder, type ContentGraph, type GraphBlock } from "@/contentGraph";

/**
 * A change of a page's lines the server told its open room of as the change
 * was committed, ahead of the graph's new position (the server's
 * content_graph_room_change.go): the blocks it made or moved as a read gives
 * them, its parents' blocks in order with their places, the blocks it
 * removed, and the text written with it. A page here that stands at the
 * position just before draws it at once, its blocks and its text in one
 * frame, without reading the page again; one that doesn't reads it as the
 * room's word of the new position says. Read again, the change's text came
 * through the room before its blocks came back, and a line merged elsewhere
 * showed beside its emptied one for a moment (2026-11-13).
 */
export type RoomGraphChange = {
  from: number;
  cursor: number;
  blocks: GraphBlock[];
  orders: { parentId: string | null; children: [string, string][] }[];
  removed: string[];
  /** Puts the text written with the change into the page's text, as the room's own. */
  showWords(): void;
};

/** The tag of the frame the server tells a room a change of its lines with (ygo_room_publication.go). */
export const ROOM_GRAPH_CHANGE_FRAME = 100;

type Listener = (change: RoomGraphChange) => void;
const listeners = new Map<string, Set<Listener>>();

/** Hears the changes the room of a page tells of; returns how to stop. */
export function hearRoomGraphChanges(pageId: string, listener: Listener) {
  let heard = listeners.get(pageId);
  if (!heard) listeners.set(pageId, heard = new Set());
  heard.add(listener);
  return () => {
    heard.delete(listener);
    if (heard.size === 0 && listeners.get(pageId) === heard) listeners.delete(pageId);
  };
}

/**
 * Reads a change frame the page's room sent — y-websocket's decoder, past
 * the frame's tag, holds its payload as a VarString — and tells the page of
 * it; `showWords` puts the change's text into the page's own.
 */
export function hearRoomGraphFrame(pageId: string, decoder: { arr: Uint8Array; pos: number }, showWords: (words: Uint8Array) => void) {
  let change: Omit<RoomGraphChange, "showWords"> & { words?: string };
  try {
    change = JSON.parse(readVarString(decoder));
  } catch {
    return;
  }
  if (!Number.isSafeInteger(change?.from) || change.cursor !== change.from + 1 || !Array.isArray(change.blocks)
    || !Array.isArray(change.orders) || !Array.isArray(change.removed ?? [])) return;
  const words = typeof change.words === "string" && change.words
    ? Uint8Array.from(atob(change.words), (character) => character.charCodeAt(0)) : null;
  const told: RoomGraphChange = {
    from: change.from, cursor: change.cursor, blocks: change.blocks, orders: change.orders, removed: change.removed ?? [],
    showWords: () => { if (words) showWords(words); },
  };
  for (const listener of [...listeners.get(pageId) ?? []]) listener(told);
}

function readVarString(decoder: { arr: Uint8Array; pos: number }) {
  let length = 0;
  for (let shift = 0; ; shift += 7) {
    const byte = decoder.arr[decoder.pos++];
    if (byte === undefined || shift > 28) throw new RangeError("A room frame ended early.");
    length += (byte & 0x7f) * 2 ** shift;
    if (byte < 0x80) break;
  }
  const bytes = decoder.arr.subarray(decoder.pos, decoder.pos + length);
  if (bytes.length !== length) throw new RangeError("A room frame ended early.");
  decoder.pos += length;
  return new TextDecoder().decode(bytes);
}

/**
 * `graph` with the change's blocks, places and removals, each block left as
 * it is that the change didn't move; null when the graph doesn't hold what
 * the change speaks of (it is read again instead).
 */
export function graphWithRoomChange(graph: ContentGraph, change: RoomGraphChange): ContentGraph | null {
  const byId = new Map(graph.blocks.map((block) => [block.id, block]));
  for (const block of change.blocks) byId.set(block.id, block);
  const removed = new Set(change.removed);
  for (let grew = removed.size > 0; grew;) {
    grew = false;
    for (const block of byId.values()) {
      if (block.parentId !== null && removed.has(block.parentId) && !removed.has(block.id)) {
        removed.add(block.id);
        grew = true;
      }
    }
  }
  for (const id of removed) byId.delete(id);
  const placed = new Set<string>();
  const ordered = new Set<string>();
  for (const { parentId, children } of change.orders) {
    ordered.add(parentId ?? "");
    let afterId: string | null = null;
    for (const [id, position] of children) {
      const block = byId.get(id);
      if (!block) return null;
      if (block.parentId !== parentId || block.position !== position || block.afterId !== afterId) {
        byId.set(id, { ...block, parentId, position, afterId });
      }
      placed.add(id);
      afterId = id;
    }
  }
  for (const block of byId.values()) if (ordered.has(block.parentId ?? "") && !placed.has(block.id)) return null;
  return { ...graph, blocks: [...byId.values()].sort(compareGraphOrder) };
}
