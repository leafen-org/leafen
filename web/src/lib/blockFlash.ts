import { useSyncExternalStore } from "react";

/**
 * A block washed as chosen for a moment, nothing chosen meanwhile: the block
 * a margin card is pressed open for, as Notion's (measured 2026-11-26 in
 * Chrome at 1440 dpr 2 in two tabs at different times, a frame watch armed
 * before the press: the block's own selection halo, rgba(35,131,226,.14) 2px
 * in with its 4px round, fading in over 0.2s from the press, held to about
 * 0.75s and gone by about 0.95s — the card pressed, a comment's 😊, a reaction
 * chip or the smiley after them; pressing the commented words, which leaves
 * the caret there, washes nothing). The wash is the block's selection halo
 * (BlockFrame), drawn where a chosen block's is; a block chosen meanwhile
 * shows its own.
 */
export const BLOCK_FLASH_MS = 950;

let flash: { blockId: string; turn: number } | null = null;
let turns = 0;
let timer = 0;
const listeners = new Set<() => void>();

function tell() {
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** Washes the block `blockId` as chosen for a moment, a newer wash taking over from an older one. */
export function flashBlock(blockId: string) {
  window.clearTimeout(timer);
  const turn = ++turns;
  flash = { blockId, turn };
  tell();
  timer = window.setTimeout(() => {
    if (flash?.turn !== turn) return;
    flash = null;
    tell();
  }, BLOCK_FLASH_MS);
}

/** The wash this block shows now, a new number each time one starts; 0 for none. */
export function useBlockFlash(blockId: string) {
  return useSyncExternalStore(subscribe, () => (flash?.blockId === blockId ? flash.turn : 0), () => 0);
}
