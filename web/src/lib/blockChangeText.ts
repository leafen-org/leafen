/**
 * The page's text a block change carries to the server, each part one Yjs
 * update in base64 (lib/textBehindBlockChanges.ts): `words`, written with the
 * change or after it; `typing`, written before it, which waited for the block
 * changes before it and is stored first, as the typing it is.
 */
export type BlockChangeText = { words?: string; typing?: string };

/** The parts of a block change's text there are, alone. */
export function blockChangeTextOf({ words, typing }: BlockChangeText): BlockChangeText {
  return { ...(words ? { words } : {}), ...(typing ? { typing } : {}) };
}
