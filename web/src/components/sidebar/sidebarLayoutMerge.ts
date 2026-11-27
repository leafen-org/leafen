import type { SidebarLayout } from "@/workspaceMembershipWire.generated";
import { sidebarSectionKey } from "./sidebarLayoutModel";

/**
 * A change made here on a layout another screen, device or agent has since
 * changed, put on what the layout is now (a three-way merge), so neither
 * change is lost: `base` is the layout as this screen last read or kept it,
 * `mine` it with this screen's changes, `theirs` the server's now. In each
 * list — the sections, the shortcuts, the tabs and the teamspaces' and the
 * favorites' order, each item by its key — what this screen took out goes,
 * what it added or moved goes where it put it (after the item before it
 * here), what it changed replaces the item, and the rest is theirs, in their
 * order; any other field this screen changed is its own, else theirs.
 */
export function mergeSidebarLayouts(base: SidebarLayout, mine: SidebarLayout, theirs: SidebarLayout): SidebarLayout {
  const merged: Record<string, unknown> = {};
  const fields = new Set([...Object.keys(base), ...Object.keys(mine), ...Object.keys(theirs)]);
  fields.delete("revision");
  for (const field of fields) {
    const keyOf = LIST_KEYS[field];
    const before = (base as Record<string, unknown>)[field];
    const changed = (mine as Record<string, unknown>)[field];
    const now = (theirs as Record<string, unknown>)[field];
    if (keyOf) {
      merged[field] = mergeList(asList(before), asList(changed), asList(now), keyOf);
    } else {
      merged[field] = sameValue(before, changed) ? now : changed;
    }
  }
  if (theirs.revision !== undefined) merged.revision = theirs.revision;
  return merged as unknown as SidebarLayout;
}

/** Whether two layouts hold the same, whatever their revisions. */
export function sameSidebarLayout(one: SidebarLayout, other: SidebarLayout): boolean {
  return sameValue({ ...one, revision: undefined }, { ...other, revision: undefined });
}

type Keyed = (item: unknown) => string;

const LIST_KEYS: Record<string, Keyed> = {
  sections: (item) => sidebarSectionKey(item as Parameters<typeof sidebarSectionKey>[0]),
  shortcuts: (item) => String((item as { key?: unknown }).key),
  tabs: (item) => String((item as { id?: unknown }).id),
  teamspaceOrder: (item) => String(item),
  favoriteOrder: (item) => String(item),
};

function asList(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function mergeList(base: unknown[], mine: unknown[], theirs: unknown[], keyOf: Keyed): unknown[] {
  const baseItems = new Map(base.map((item) => [keyOf(item), item]));
  const mineItems = new Map(mine.map((item) => [keyOf(item), item]));
  const theirKeys = new Set(theirs.map(keyOf));
  const removed = new Set([...baseItems.keys()].filter((key) => !mineItems.has(key)));
  const edited = new Set([...mineItems].filter(([key, item]) => baseItems.has(key) && !sameValue(baseItems.get(key), item)).map(([key]) => key));
  // Moved: the items both hold outside the longest run they keep in order.
  const mineOrder = mine.map(keyOf).filter((key) => baseItems.has(key));
  const baseOrder = base.map(keyOf).filter((key) => mineItems.has(key));
  const inOrder = new Set(longestCommonRun(baseOrder, mineOrder));
  const placed = new Set(mine.map(keyOf).filter((key) =>
    !baseItems.has(key) || !inOrder.has(key) || (edited.has(key) && !theirKeys.has(key))));
  const result = theirs
    .filter((item) => !removed.has(keyOf(item)) && !placed.has(keyOf(item)))
    .map((item) => (edited.has(keyOf(item)) ? mineItems.get(keyOf(item)) : item));
  mine.forEach((item, index) => {
    if (!placed.has(keyOf(item))) return;
    let at = 0;
    for (let back = index - 1; back >= 0; back -= 1) {
      const before = keyOf(mine[back]);
      const found = result.findIndex((other) => keyOf(other) === before);
      if (found >= 0) {
        at = found + 1;
        break;
      }
    }
    result.splice(at, 0, item);
  });
  return result;
}

/** The longest sequence of keys both lists hold in the same order. */
function longestCommonRun(one: string[], other: string[]): string[] {
  const lengths = Array.from({ length: one.length + 1 }, () => new Array<number>(other.length + 1).fill(0));
  for (let i = one.length - 1; i >= 0; i -= 1) {
    for (let j = other.length - 1; j >= 0; j -= 1) {
      lengths[i][j] = one[i] === other[j] ? lengths[i + 1][j + 1] + 1 : Math.max(lengths[i + 1][j], lengths[i][j + 1]);
    }
  }
  const run: string[] = [];
  for (let i = 0, j = 0; i < one.length && j < other.length;) {
    if (one[i] === other[j]) {
      run.push(one[i]);
      i += 1;
      j += 1;
    } else if (lengths[i + 1][j] >= lengths[i][j + 1]) {
      i += 1;
    } else {
      j += 1;
    }
  }
  return run;
}

/**
 * Whether two values hold the same, a field left out the same as one the
 * server leaves out when empty (an empty text or list, false, 0).
 */
function sameValue(one: unknown, other: unknown): boolean {
  if (isEmpty(one) && isEmpty(other)) return true;
  if (Array.isArray(one) || Array.isArray(other)) {
    return Array.isArray(one) && Array.isArray(other) && one.length === other.length
      && one.every((item, index) => sameValue(item, other[index]));
  }
  if (one && other && typeof one === "object" && typeof other === "object") {
    const a = one as Record<string, unknown>, b = other as Record<string, unknown>;
    return [...new Set([...Object.keys(a), ...Object.keys(b)])].every((key) => sameValue(a[key], b[key]));
  }
  return one === other;
}

function isEmpty(value: unknown): boolean {
  return value === undefined || value === null || value === "" || value === false || value === 0
    || (Array.isArray(value) && value.length === 0);
}
