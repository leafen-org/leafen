import * as Y from "yjs";
import type { WebsocketProvider } from "y-websocket";
import { announcedGraphCursor } from "./graphCursorWord";

// The longest the room's text waits, as a connection opens, for the page's blocks.
const JOIN_WAIT_LIMIT_MS = 1500;
// y-websocket's sync message, and in it the room's answer and its updates (y-protocols/sync).
const MESSAGE_SYNC = 0;
const SYNC_STEP_2 = 1;
const SYNC_UPDATE = 2;

type Decoder = { arr: Uint8Array; pos: number };

function readVarUint(decoder: Decoder) {
  let value = 0;
  for (let shift = 0; decoder.pos < decoder.arr.length; shift += 7) {
    const byte = decoder.arr[decoder.pos++];
    value += (byte & 0x7f) * 2 ** shift;
    if (byte < 0x80) return value;
  }
  throw new RangeError("A sync message ended early.");
}

function readVarUint8Array(decoder: Decoder) {
  const length = readVarUint(decoder);
  if (decoder.pos + length > decoder.arr.length) throw new RangeError("A sync message ended early.");
  const bytes = decoder.arr.slice(decoder.pos, decoder.pos + length);
  decoder.pos += length;
  return bytes;
}

/**
 * The text a page's room brings as its connection opens is drawn once the
 * page shows its blocks where the room's stood then (`shownAt`, the
 * position of the blocks drawn), not before: the room holds the text of
 * every change committed up to now, the page's blocks are those of its read,
 * and a page opened in a run of merges and Undos elsewhere drew the merged
 * line beside its emptied one, or an Undo's line gone, until its read caught
 * up (2026-11-13: one tab in two, some 30–140 ms on a NAS-class server). It
 * waits for the room's word of where its blocks stand, then for the page to
 * draw them there (`shown`, told as it draws), at most JOIN_WAIT_LIMIT_MS.
 * Returns how to look again as the page draws, and how to stop.
 */
export function holdRoomTextOnJoin(provider: WebsocketProvider, document: Y.Doc, shownAt: () => number) {
  type SyncHandler = (encoder: unknown, decoder: Decoder, room: WebsocketProvider, emitSynced: boolean, messageType: number) => void;
  const handlers = provider.messageHandlers as unknown as SyncHandler[];
  const handle = handlers[MESSAGE_SYNC];
  let joining = false;
  // Where the room's blocks stood as it opened; null till it says.
  let joinedAt: number | null = null;
  let held: Uint8Array[] = [];
  let limit = 0;
  const release = () => {
    joining = false;
    joinedAt = null;
    window.clearTimeout(limit);
    const updates = held;
    held = [];
    if (updates.length) Y.applyUpdate(document, updates.length === 1 ? updates[0] : Y.mergeUpdates(updates), provider);
  };
  const shown = () => {
    if (joining && joinedAt !== null && shownAt() >= joinedAt) release();
  };
  handlers[MESSAGE_SYNC] = (encoder, decoder, room, emitSynced, messageType) => {
    const start = decoder.pos;
    const kind = joining ? readVarUint(decoder) : -1;
    if (kind !== SYNC_STEP_2 && kind !== SYNC_UPDATE) {
      decoder.pos = start;
      handle(encoder, decoder, room, emitSynced, messageType);
      return;
    }
    held.push(readVarUint8Array(decoder));
    // Synced all the same: what follows the sync goes on (a return reads the page again).
    if (emitSynced && kind === SYNC_STEP_2 && !room.synced) room.synced = true;
  };
  const opened = ({ status }: { status: string }) => {
    if (status !== "connected") return;
    joining = true;
    joinedAt = null;
    window.clearTimeout(limit);
    limit = window.setTimeout(release, JOIN_WAIT_LIMIT_MS);
  };
  // The room's first word of where its blocks stand; a room with none waits for nothing.
  const heard = (_changes: unknown, origin: unknown) => {
    if (origin === "local" || !joining || joinedAt !== null) return;
    let furthest = 0;
    for (const state of provider.awareness.getStates().values()) furthest = Math.max(furthest, announcedGraphCursor(state));
    if (furthest === 0) release();
    else {
      joinedAt = furthest;
      shown();
    }
  };
  provider.on("status", opened);
  provider.awareness.on("update", heard);
  return {
    shown,
    stop() {
      provider.off("status", opened);
      provider.awareness.off("update", heard);
      handlers[MESSAGE_SYNC] = handle;
      release();
    },
  };
}
