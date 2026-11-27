import { useSyncExternalStore } from "react";
import { personValue, storePersonValue } from "./browserStorage";

// The view a person last chose on a database block, as Notion keeps it (seen
// 2026-10-01): in this browser, for that person and block, so a reload shows
// it again while others keep their own; the block's saved view stays the
// default. Choosing a view writes nothing to the page.

const VIEW_KEY = "leafen:database-view";
const PREFIX = `${VIEW_KEY}:`;
const listeners = new Set<() => void>();
// This window's choices, and those it read: kept until the page reloads.
const memory = new Map<string, string>();
// The view a view link opens on, shown by whichever database holds it until
// a view is chosen there.
let linkedViewId: string | null = null;
// The address a view was last chosen at. The view an address names (`?v=`,
// as "보기 링크 복사" makes it) is the one a database draws first when the
// page opens on it, as Notion's opens straight on it (2026-11-15: going in
// the app to a page's `?v=` drew its last chosen view first, then the
// address's); a view chosen while that address stands is the person's.
let chosenAtAddress: string | null = null;
let version = 0;
// The view each block last had added in this window, the view shown as it was added, and whether it has shown since.
const added = new Map<string, { viewId: string; fromId: string; shown: boolean }>();

function key(userId: string | undefined, blockId: string) {
  return `${PREFIX}${userId ?? ""}:${blockId}`;
}

function addressHere() {
  return typeof window === "undefined" ? null : `${window.location.pathname}${window.location.search}`;
}

// The address a closed peek left its view in (closePagePeek).
let leftByPeek: string | null = null;

/**
 * Notes the address a peek closing leaves its view in (`?v=`, as Notion's,
 * seen 2026-11-25): it names the view the peek opened on, which the page
 * under it doesn't take — Notion's database in the page keeps its own (seen
 * that day in two tabs: under that `?v=` and under a peek's address naming
 * another of its views).
 */
export function noteViewLeftByPeek(address: string) {
  leftByPeek = address;
}

/** Whether the address's view is one a closed peek left there (noteViewLeftByPeek). */
export function addressViewLeftByPeek() {
  return leftByPeek !== null && leftByPeek === addressHere();
}

/** The view the address names, until a view is chosen while it stands, unless a closed peek left it. */
function addressViewId() {
  const here = addressHere();
  if (here === null || here === chosenAtAddress || here === leftByPeek) return null;
  return new URLSearchParams(window.location.search).get("v");
}

/**
 * Whether the address peeks a database by its own ID (inlineDatabasePeekHref):
 * its view is the peek's, which the database under it in the page doesn't
 * take, as Notion's keeps its own there (seen 2026-11-25, a peek's address
 * loaded anew at another of its views).
 */
function addressPeeks(blockId: string) {
  return typeof window !== "undefined" && new URLSearchParams(window.location.search).get("p") === blockId;
}

/**
 * The view this person last chose on a database block here, if any. A window
 * keeps the one it first read until a view is chosen in it: one chosen in
 * another window of this browser shows there, and here after a reload, as
 * Notion's windows keep their own (2026-11-06: another window's choice turned
 * this one's view the next time it drew).
 */
export function readDatabaseViewChoice(userId: string | undefined, blockId: string) {
  const shown = key(userId, blockId);
  const kept = memory.get(shown);
  if (kept !== undefined) return kept;
  const stored = userId ? personValue(VIEW_KEY, { userId, part: blockId, previous: [shown] }) : null;
  if (stored) memory.set(shown, stored);
  return stored ?? null;
}

/** Keeps the view this person chose on a database block, and tells the views showing it. */
export function writeDatabaseViewChoice(userId: string | undefined, blockId: string, viewId: string) {
  // Kept for this visit whatever the browser keeps, and on the browser for the person.
  memory.set(key(userId, blockId), viewId);
  linkedViewId = null;
  chosenAtAddress = addressHere();
  if (userId) storePersonValue(VIEW_KEY, viewId, { userId, part: blockId });
  version += 1;
  for (const listener of listeners) listener();
}

/** Opens the databases on the view a view link names, where it is one of theirs. */
export function chooseLinkedDatabaseView(viewId: string | null) {
  linkedViewId = viewId;
  version += 1;
  for (const listener of listeners) listener();
}

/**
 * Rereads whenever a choice changes, while `follow`; the value is only a
 * change counter. The product's shell follows them on a wiki alone: on every
 * page, a view tab's press drew the whole app again, the search window and the
 * page's head too, before its view showed (2026-11-15).
 */
export function useDatabaseViewChoices(follow = true) {
  return useSyncExternalStore((listener) => {
    if (!follow) return () => {};
    listeners.add(listener);
    return () => listeners.delete(listener);
  }, () => follow ? version : 0, () => 0);
}

/**
 * Notes a view added on a block over the view shown then. Taken back by ⌘Z,
 * the view shown before it shows again, and stays chosen when ⌘⇧Z brings the
 * added view back as a tab, as Notion's (seen 2026-11-15 in Chrome in two
 * tabs: a board added over a lone table, a gallery over that board, each
 * ⌘Z once its panel was closed going back to the view before it, ⌘⇧Z
 * bringing the tab back with that view still shown; with the panel open ⌘Z
 * is its name field's own and the view stays).
 */
export function noteDatabaseViewAdded(blockId: string, viewId: string, fromId: string) {
  added.set(blockId, { viewId, fromId, shown: false });
}

/**
 * The chosen view among a block's views: a view link's, else the one the
 * address names, kept as this person's choice, else this person's choice
 * here, else the block's saved view, else the first. One in a peek
 * (`followsAddress` false) goes by the person's choice, whatever `?v=` the
 * peek's address keeps, as Notion's (seen twice 2026-11-25 in Chrome: a side
 * peek's address loaded anew at another view, and gone back to after a view
 * was chosen in it, showed the view last chosen).
 */
export function chosenDatabaseView<View extends { id: string }>(
  views: readonly View[],
  userId: string | undefined,
  blockId: string,
  savedViewId: unknown,
  followsAddress = true,
) {
  let chosen = readDatabaseViewChoice(userId, blockId);
  const add = added.get(blockId);
  // Chosen as it is added, it may be read before it is among the views.
  if (add && views.some((view) => view.id === add.viewId)) add.shown = true;
  else if (add?.shown && chosen === add.viewId && views.length) {
    // The view added is gone again: the one it was added over is chosen once more, kept as a choice is.
    added.delete(blockId);
    memory.set(key(userId, blockId), add.fromId);
    if (userId) storePersonValue(VIEW_KEY, add.fromId, { userId, part: blockId });
    chosen = add.fromId;
  }
  const addressed = followsAddress && !addressPeeks(blockId) ? addressViewId() : null;
  const linked = views.find((view) => view.id === linkedViewId);
  const fromAddress = linked ? undefined : addressed ? views.find((view) => view.id === addressed) : undefined;
  // Opened on the address's view, the person has it as their choice, as a
  // view a link opens on Notion's database page stays its database's for a
  // reload of the page holding it (seen 2026-11-24 in Chrome): a database in
  // a page, whose address a row opened and closed there leaves without the
  // view, opens on it again after a reload.
  if (fromAddress && chosen !== fromAddress.id) {
    memory.set(key(userId, blockId), fromAddress.id);
    if (userId) storePersonValue(VIEW_KEY, fromAddress.id, { userId, part: blockId });
  }
  return linked
    ?? fromAddress
    ?? views.find((view) => view.id === chosen)
    ?? views.find((view) => view.id === savedViewId)
    ?? views[0];
}
