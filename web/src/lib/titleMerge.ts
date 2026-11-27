import type { TextSpan } from "@/lib/types";

// A title is one line that several people, or one person in several windows,
// may be typing at once. What each typed since the title they started from is
// kept, as the body's shared text keeps it and as Notion's title does (seen
// 2026-11-10: "X " typed at the start in one window and "aa"/"bb" typed at
// once in two places of the other both stayed): a character either side
// removed goes, what either side typed stays, and nothing is replaced whole.
//
// A title or a database's name is written as it is typed, the spaces at its
// ends too, and one of spaces alone is a title: Notion keeps them so, typed
// in its app (seen 2026-11-13: " W886끝 뒤 ", "앱DB 끝 " and "   " read back
// as typed) — only an empty one is untitled (pageTitle.ts).

/** One change of a text: what was between `from` and `to` became `insert`. */
type Change = { from: number; to: number; insert: string[] };

// How many differences the comparison looks for before it calls the changed
// middle one replacement; a title changes by a few characters between saves.
const DIFFERENCE_LIMIT = 128;

/**
 * The changes from `a` to `b`, in order, by their places in `a`. A middle
 * too different to compare in reasonable time is one change.
 */
export function textChanges(a: readonly string[], b: readonly string[]): Change[] {
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) start += 1;
  let endA = a.length, endB = b.length;
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) { endA -= 1; endB -= 1; }
  if (endA === start && endB === start) return [];
  const middleA = a.slice(start, endA), middleB = b.slice(start, endB);
  const changes = shortestChanges(middleA, middleB) ?? [{ from: 0, to: middleA.length, insert: middleB }];
  return changes.map((change) => ({ from: change.from + start, to: change.to + start, insert: change.insert }));
}

// Myers' difference: the fewest removals and insertions from a to b, or null
// past the limit.
function shortestChanges(a: readonly string[], b: readonly string[]): Change[] | null {
  const n = a.length, m = b.length, limit = Math.min(n + m, DIFFERENCE_LIMIT), offset = limit + 1;
  const reach = new Int32Array(2 * limit + 3);
  const trace: Int32Array[] = [];
  for (let d = 0; d <= limit; d += 1) {
    trace.push(reach.slice());
    for (let k = -d; k <= d; k += 2) {
      let x = k === -d || (k !== d && reach[offset + k - 1] < reach[offset + k + 1]) ? reach[offset + k + 1] : reach[offset + k - 1] + 1;
      let y = x - k;
      while (x < n && y < m && a[x] === b[y]) { x += 1; y += 1; }
      reach[offset + k] = x;
      if (x >= n && y >= m) return changesFromTrace(trace, a, b, offset);
    }
  }
  return null;
}

function changesFromTrace(trace: Int32Array[], a: readonly string[], b: readonly string[], offset: number): Change[] {
  // Each step back is one removal of a[at] or one insertion of b[at] before a's place.
  const steps: { kind: "remove" | "insert"; place: number; at: number }[] = [];
  let x = a.length, y = b.length;
  for (let d = trace.length - 1; d > 0; d -= 1) {
    const reach = trace[d], k = x - y;
    const fromK = k === -d || (k !== d && reach[offset + k - 1] < reach[offset + k + 1]) ? k + 1 : k - 1;
    const fromX = reach[offset + fromK], fromY = fromX - fromK;
    while (x > fromX && y > fromY) { x -= 1; y -= 1; }
    if (x === fromX) steps.push({ kind: "insert", place: x, at: fromY });
    else steps.push({ kind: "remove", place: fromX, at: fromX });
    x = fromX; y = fromY;
  }
  const changes: Change[] = [];
  for (const step of steps.reverse()) {
    const last = changes[changes.length - 1];
    if (step.kind === "remove") {
      if (last && last.to === step.place) last.to += 1;
      else changes.push({ from: step.place, to: step.place + 1, insert: [] });
    } else if (last && last.to === step.place) last.insert.push(b[step.at]);
    else changes.push({ from: step.place, to: step.place, insert: [b[step.at]] });
  }
  return changes;
}

// Two people typing at one place: typing the other side already holds, in
// order (a window's earlier save of the same words, which a write made on an
// older title still carries, the other's typing going on before, after or
// between it), is kept once; otherwise both stay, the one the server holds
// first (2026-11-10: " X" saved, then " Z" typed before it elsewhere, and a
// write of " X" on the title before made " Z X X").
function bothTyped(theirs: readonly string[], mine: readonly string[]) {
  const holds = (whole: readonly string[], part: readonly string[]) => {
    let found = 0;
    for (const unit of whole) if (found < part.length && unit === part[found]) found += 1;
    return found === part.length;
  };
  if (!theirs.length || holds(mine, theirs)) return mine;
  if (!mine.length || holds(theirs, mine)) return theirs;
  return [...theirs, ...mine];
}

// The units of a title with both sides' edits of `base`: removals from either
// side go, the typing of both stays.
function mergeUnits(base: readonly string[], mine: readonly string[], theirs: readonly string[]): string[] {
  const removed = new Uint8Array(base.length);
  const typed = new Map<number, { mine: string[]; theirs: string[] }>();
  const note = (side: "mine" | "theirs", units: readonly string[]) => {
    for (const change of textChanges(base, units)) {
      removed.fill(1, change.from, change.to);
      if (!change.insert.length) continue;
      const at = typed.get(change.from) ?? { mine: [], theirs: [] };
      at[side] = change.insert;
      typed.set(change.from, at);
    }
  };
  note("theirs", theirs);
  note("mine", mine);
  const merged: string[] = [];
  for (let place = 0; place <= base.length; place += 1) {
    const at = typed.get(place);
    if (at) merged.push(...bothTyped(at.theirs, at.mine));
    if (place < base.length && !removed[place]) merged.push(base[place]);
  }
  return merged;
}

/**
 * A title with its spans — mentions, equations, links, marks — where it has
 * any (page_title_spans.go): spans that don't spell the title aren't its.
 */
export type TitleValue = { title: string; spans?: readonly TextSpan[] };

// A title is merged unit by unit: each character with its formatting, and a
// mention or an equation whole, so a mention typed in one window and words
// typed in another both stay, and a mention is never cut in two.
const ATOM = "\u0001", FORMATTED = "\u0000";

function isAtom(span: TextSpan) {
  return Boolean(span.mention || span.equation || span.template);
}

// A span's formatting and mention as one string, its keys in order, without
// what a reader's screen adds (unavailable) or what says nothing.
function attributes(span: TextSpan) {
  const kept = Object.entries(span)
    .filter(([key, value]) => key !== "text" && key !== "unavailable" && value !== undefined && value !== false && value !== "")
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  return kept.length ? JSON.stringify(Object.fromEntries(kept)) : "";
}

/** A title's units, in order. */
export function titleUnits({ title, spans }: TitleValue): string[] {
  if (!spans?.length || spans.map((span) => span.text).join("") !== title) return Array.from(title);
  const units: string[] = [];
  for (const span of spans) {
    const formatting = attributes(span);
    if (isAtom(span)) units.push(ATOM + span.text + FORMATTED + formatting);
    else for (const character of span.text) units.push(formatting ? character + FORMATTED + formatting : character);
  }
  return units;
}

/** How many characters a unit's words hold; a mention or equation is one place in the field. */
export function titleUnitText(unit: string): { text: string; atom: boolean } {
  const atom = unit.startsWith(ATOM), split = unit.indexOf(FORMATTED);
  return { text: unit.slice(atom ? 1 : 0, split < 0 ? undefined : split), atom };
}

/** The title units spell, with spans when any of them holds more than words. */
export function titleFromUnits(units: readonly string[]): TitleValue {
  const spans: TextSpan[] = [];
  let formatted = false;
  for (const unit of units) {
    const { text, atom } = titleUnitText(unit), split = unit.indexOf(FORMATTED);
    const formatting = split < 0 ? "" : unit.slice(split + 1);
    const previous = spans.at(-1);
    if (!atom && previous && !isAtom(previous) && attributes(previous) === formatting) {
      previous.text += text;
      continue;
    }
    formatted ||= Boolean(formatting);
    spans.push({ ...(formatting ? JSON.parse(formatting) as Partial<TextSpan> : {}), text });
  }
  const title = spans.map((span) => span.text).join("");
  return formatted ? { title, spans } : { title };
}

/**
 * Whether two titles — or a name's, a cell's words — hold the same words with
 * the same formatting and mentions, however their spans are shaped: a flag
 * spelled out as false, keys in another order or runs split elsewhere say
 * the same (2026-11-16: a row page's title holding a mention, handed to its
 * field with every flag false, read back without them and written again as
 * it was left, its edited time moving though nothing changed).
 */
export function sameTitle(a: TitleValue, b: TitleValue) {
  if (a.title !== b.title) return false;
  if (a.spans === b.spans || JSON.stringify(a.spans ?? null) === JSON.stringify(b.spans ?? null)) return true;
  const left = titleUnits(a), right = titleUnits(b);
  return left.length === right.length && left.every((unit, index) => unit === right[index]);
}

/**
 * A title with its spans merged as mergeTitleEdits merges words: the
 * mentions and formatting either side made stay with them.
 */
export function mergeTitleValues(base: TitleValue, mine: TitleValue, theirs: TitleValue): TitleValue {
  if (sameTitle(mine, theirs) || sameTitle(theirs, base)) return mine;
  if (sameTitle(mine, base)) return theirs;
  return titleFromUnits(mergeUnits(titleUnits(base), titleUnits(mine), titleUnits(theirs)));
}
