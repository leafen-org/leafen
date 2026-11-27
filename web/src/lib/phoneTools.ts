// Where a phone's editing tools stand on screen while they show (the editor's
// NativePhoneToolbar), for what is placed by a line above them to keep clear
// of them (popoverPlacement.ts). Each editor's tools report for themselves.

export type PhoneToolsPlace = { top: number; bottom: number };

/** How far over the foot of what shows of the page the tools stand at the
 * lowest: there over a keyboard where their line is low (NativePhoneToolbar). */
export const PHONE_TOOLS_FOOT_GAP = 8;

const places = new Map<object, PhoneToolsPlace>();
const watchers = new Set<() => void>();

/** The tools of `owner` stand from `top` to `bottom` (where fixed boxes are placed), or show no more (null). */
export function placePhoneTools(owner: object, place: PhoneToolsPlace | null) {
  const was = places.get(owner) ?? null;
  if (was === place || (was && place && was.top === place.top && was.bottom === place.bottom)) return;
  if (place === null) places.delete(owner); else places.set(owner, place);
  for (const watcher of watchers) watcher();
}

/** Where the phone's editing tools stand, while they show. */
export function phoneTools(): PhoneToolsPlace | null {
  for (const place of places.values()) return place;
  return null;
}

/** Calls `moved` whenever the tools come, go or move. */
export function watchPhoneTools(moved: () => void): () => void {
  watchers.add(moved);
  return () => { watchers.delete(moved); };
}
