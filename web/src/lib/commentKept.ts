import type { CommentActorProfile } from "@/commentWire.generated";

/**
 * A page's comments as a read of them brings them, each one that came back
 * the same as the comment shown kept as the very object shown — its thread's
 * `unread`, which the page works out after (commentThreadUnread.ts), aside —
 * and the people's profiles the same way: a read that changed nothing of a
 * discussion leaves its card undrawn (CommentThreadCard), as the change
 * shown at a press does. Reads come after every change the room announces,
 * this tab's own too (2026-11-25: each drew all 150 cards of "모든 토론").
 */
export function keptComments<T extends { id: string }>(shown: readonly T[], read: T[]): T[] {
  const before = new Map(shown.map((comment) => [comment.id, comment]));
  return read.map((comment) => {
    const known = before.get(comment.id);
    return known && sameValue(known, comment, "unread") ? known : comment;
  });
}

/** The people's profiles a read brings, part by part, kept as shown when they are the same. */
export function keptProfiles(
  shown: Readonly<Record<string, CommentActorProfile>>,
  parts: readonly { actorProfiles: readonly CommentActorProfile[] }[],
): Readonly<Record<string, CommentActorProfile>> {
  const read: Record<string, CommentActorProfile> = {};
  for (const part of parts) for (const profile of part.actorProfiles) read[profile.userId] = profile;
  return sameValue(shown, read) ? shown : read;
}

/** Whether two values read from the server say the same; a key missing and one left undefined are the same. */
function sameValue(left: unknown, right: unknown, skip?: string): boolean {
  if (left === right) return true;
  if (typeof left !== "object" || typeof right !== "object" || left === null || right === null) return false;
  if (Array.isArray(left) || Array.isArray(right)) {
    return Array.isArray(left) && Array.isArray(right) && left.length === right.length
      && left.every((item, index) => sameValue(item, right[index]));
  }
  const a = left as Record<string, unknown>, b = right as Record<string, unknown>;
  for (const key of new Set([...Object.keys(a), ...Object.keys(b)])) {
    if (key !== skip && !sameValue(a[key], b[key])) return false;
  }
  return true;
}
