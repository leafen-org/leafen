import { plainTitlePieces } from "./closedMentions";
import type { TextSpan } from "./types";

/** A title's pieces, its custom emoji apart (titleCustomEmojiPieces), for a mention to draw them. */
export type MentionTitlePieces = readonly (string | TextSpan)[];

type Kind = "page" | "database";

// The titles of mentioned pages and databases holding a custom emoji, as
// pieces, by "<kind>\0<id>", for their mentions to draw it; kept beside their
// plain titles (peopleDirectory.ts), which announce a change of either.
let titlePieces: ReadonlyMap<string, MentionTitlePieces> = new Map();

const piecesKey = (kind: Kind, id: string) => `${kind}\0${id}`;
const piecesSignature = (pieces: MentionTitlePieces) => pieces.map((piece) => (
  typeof piece === "string" ? piece : `\0${piece.customEmojiId ?? ""}\0${piece.text}\0`)).join("");

/** Keeps one kind's title pieces in place of those it had; whether they changed. */
export function keepTitlePieces(kind: Kind, pieces: ReadonlyMap<string, MentionTitlePieces>) {
  const prefix = piecesKey(kind, "");
  const next = new Map([...titlePieces].filter(([key]) => !key.startsWith(prefix)));
  for (const [id, shown] of pieces) next.set(piecesKey(kind, id), shown);
  const changed = next.size !== titlePieces.size || [...next].some(([key, shown]) => {
    const before = titlePieces.get(key);
    return !before || piecesSignature(before) !== piecesSignature(shown);
  });
  if (changed) titlePieces = next;
  return changed;
}

/**
 * A mentioned page's or database's title in pieces, its custom emoji apart,
 * while its words are the ones its mention reads — as Notion's mention draws
 * the emoji of the title it names (seen 2026-11-11); null otherwise.
 */
export function mentionTitlePieces(kind: Kind, id: string, text: string) {
  const pieces = titlePieces.get(piecesKey(kind, id));
  return pieces && plainTitlePieces(pieces) === text ? pieces : null;
}
