import { parentChain } from "@/lib/parentChain";

// What stands for a database after each turn of it: the block a turn put in
// the place of the one it turned — a page's link for the database, or the
// database for its page's link. A ⌘Z step of a turn kept the block it last
// made; once another turn of the same database, another command's, put a new
// block in its place, the step looked for one that was gone and the page sat
// unchanged for seconds (2026-11-14, '페이지로 전환' → ⌘Z → ⌘⇧Z → '인라인
// 데이터베이스로 전환' → ⌘Z → ⌘Z).
const successors = new Map<string, string>();

/** Notes that a turn put `to` where `from` stood. */
export function noteDatabaseTurned(from: string, to: string) {
  if (!from || !to || from === to) return;
  // What stands now has turned into nothing yet, even where an id came back.
  successors.delete(to);
  successors.set(from, to);
}

/** The block that stands now for the one a turn left at `id`, however many turns on; a chain of turns that comes back on itself ends where it would repeat (lib/parentChain.ts). */
export function databaseStandingNow(id: string) {
  const climb = parentChain(id);
  let at = id;
  for (let next = successors.get(at); next !== undefined && climb(next); next = successors.get(at)) at = next;
  return at;
}
