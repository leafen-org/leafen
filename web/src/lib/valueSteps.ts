import { newId } from "@/lib/ids";

/** How long after a value was saved its next save still joins the same ⌘Z step. */
export const TYPED_VALUE_MS = 2000;

/** A value as a row holds it — a table's cell, a row page's property — or none. */
export type HeldValue = { type?: string; value?: unknown; spans?: unknown } | undefined;

// A value holds nothing: none, or a cleared, empty or unchecked one.
function emptyValue(value: HeldValue) {
  const held = value?.value;
  return held === undefined || held === null || held === "" || held === false || (Array.isArray(held) && held.length === 0);
}

// A value as words, its objects' keys in one order: read back, a value's
// objects may list them in another (a row page's read sorts them, so a file
// linked read back was no longer "the same" and its ⌘Z was refused).
function canonical(value: unknown) {
  return JSON.stringify(value ?? null, (_key, held: unknown) => held && typeof held === "object" && !Array.isArray(held)
    ? Object.fromEntries(Object.keys(held).sort().map((key) => [key, (held as Record<string, unknown>)[key]])) : held);
}

/** Two values are the same: type, value and marks. */
export const sameValue = (left: HeldValue, right: HeldValue) => (
  left?.type === right?.type && canonical(left?.value) === canonical(right?.value) && canonical(left?.spans) === canonical(right?.spans)
);

/** Two values hold the same, an empty one as any other empty one. */
export const sameCell = (left: HeldValue, right: HeldValue) => (emptyValue(left) && emptyValue(right)) || sameValue(left, right);

// Each field typed in is one run of typing until it lets go of the focus —
// Enter, Escape, Tab or a press elsewhere ending it, its last save made by
// then — so a value typed again right after, the same cell's too, is a step
// of its own, as Notion's (seen 2026-11-27 in Chrome, a table's text cell:
// "1" and Enter, ↑, "2" and Enter within two seconds, then ⌘Z gave "…1" and
// a second ⌘Z the value before). One run of typing joins whatever it saves.
let typingRuns = 0, typingHeard = false;
function editableField(target: EventTarget | null) {
  return target instanceof HTMLElement && (target.isContentEditable || target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement);
}

/** Which run of typing a save is made in (a field's focus held), to join only saves of the same. */
export function typingRun() {
  if (!typingHeard && typeof document !== "undefined") {
    typingHeard = true;
    // Saves its leaving makes, as it blurs or as its own focusout is heard, come first: the run ends after them.
    document.addEventListener("focusout", (event) => { if (editableField(event.target)) queueMicrotask(() => { typingRuns += 1; }); });
  }
  return typingRuns;
}

/**
 * Saves of values as steps of a page's ⌘Z, wherever a row's values are
 * saved — its table's cells, its peek, its page opened away from its
 * database — as Notion's (seen 2026-10-21, a table; 2026-11-13, a row
 * page): each save is a step as it shows, so a ⌘Z right after it takes it
 * back (2026-11-09: one recorded with the server's answer was not there
 * yet). A value saved again as it is typed in joins its step, as Notion
 * takes a typed value back at once, while that step is still the
 * history's last, the save before came within TYPED_VALUE_MS and both
 * were typed in one run (typingRun). What a save gives back takes its
 * step out again — or, typed, its part of it — for a save the server
 * refused. The history keeps `changes` as they are when the step is taken.
 */
export function valueStepRecorder<Change extends { after: unknown }>(history: {
  /** The value a change saves, which a change of one value types on. */
  cell(change: Change): string;
  record(key: string, changes: readonly Change[]): void;
  isLatest(key: string): boolean;
  drop(key: string): void;
}) {
  let typed: { key: string; cell: string; at: number; run: number; changes: Change[] } | null = null;
  return (changes: readonly Change[]) => {
    const cell = changes.length === 1 ? history.cell(changes[0]) : "", run = typingRun();
    const last = typed;
    if (cell && last?.cell === cell && last.run === run && Date.now() - last.at < TYPED_VALUE_MS && history.isLatest(last.key)) {
      const before = last.changes[0].after;
      last.changes[0] = { ...last.changes[0], after: changes[0].after };
      last.at = Date.now();
      return () => { if (last.changes[0].after === changes[0].after) last.changes[0] = { ...last.changes[0], after: before }; };
    }
    const step = { key: `values/${newId()}`, cell, at: Date.now(), run, changes: [...changes] };
    typed = step;
    history.record(step.key, step.changes);
    return () => {
      history.drop(step.key);
      if (typed === step) typed = null;
    };
  };
}
