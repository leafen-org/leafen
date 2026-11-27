import type { ContentGraph } from "@/contentGraph";

/**
 * The page/principal histories open now, by scope
 * (`workspace/page/principal`): what reaches one from outside its editor —
 * a server-owned command, its drop, join or blocks, whether it ends one —
 * goes through the history its editor holds here (nativeEditorUndoOwner).
 */
export type CommandHanded = "done" | "undone";

export type NativeEditorHistoryScope = {
  /** `shown`: its step shows what it does as it is taken, and lands at once (recordShownNativeEditorCommand). */
  command(key: string, step: (forward: boolean) => Promise<boolean>, handed?: CommandHanded, shown?: boolean): boolean;
  drop(key: string): void;
  latest(key: string): boolean;
  join(step: (forward: boolean) => Promise<boolean>): boolean;
  adopt(key: string, ids: readonly string[]): boolean;
  /** Blocks a server-owned half brings and takes away itself, kept out of the step the page takes them in by. */
  keepOut(ids: readonly string[]): void;
  /** Ends the history: nothing done before can be taken back or done again. */
  forget(): void;
  /** Whether a command is a step the history can take back still: in it, and not undone. */
  holds(key: string): boolean;
  /** Takes the history's latest step back, as ⌘Z on its page does. */
  back(): void;
  /** A step outside the page's graph and text that the next action made within `within` ms, and that `takes`, takes with it, as one ⌘Z. */
  joinNext(step: (forward: boolean) => void, within: number, takes?: (graph: ContentGraph) => boolean): void;
  /** Once the ⌘Z and ⌘⇧Z pressed so far have begun: at once, or as the last of those waiting their turn goes. */
  begun(): Promise<void>;
};

const scopes = new Map<string, NativeEditorHistoryScope>();

export function holdNativeEditorScope(scope: string, history: NativeEditorHistoryScope) {
  scopes.set(scope, history);
}

export function letGoOfNativeEditorScope(scope: string, history: NativeEditorHistoryScope) {
  if (scopes.get(scope) === history) scopes.delete(scope);
}

export function nativeEditorScope(scope: string) {
  return scopes.get(scope);
}

/**
 * Once the ⌘Z and ⌘⇧Z pressed so far in the history of a page/principal have
 * begun — at once, or, pressed while a step before them was on its way to
 * the server, as their turn comes (nativeEditorUndoOwner) — for what reads the
 * page as a step left it.
 */
export function nativeEditorStepsBegun(scope: string) {
  return scopes.get(scope)?.begun() ?? Promise.resolve();
}

// The commands that went in a history, the latest of them (a history keeps 200).
const recorded = new Set<string>();

// A server-owned command a history waits for, taken in place of a step of its own (catchNativeEditorCommand).
const catching = new Map<string, { named(key: string): boolean; take(step: (forward: boolean) => Promise<boolean>): void }>();

/**
 * The next server-owned command of a history that `named` names, within
 * `within` ms, goes to `take` and makes no step of its own: it is a half
 * of a step made at the press that set it off (joinComingNativeEditorCommand).
 */
export function catchNativeEditorCommand(scope: string, named: (key: string) => boolean,
  take: (step: (forward: boolean) => Promise<boolean>) => void, within: number) {
  const catcher = { named, take };
  catching.set(scope, catcher);
  window.setTimeout(() => { if (catching.get(scope) === catcher) catching.delete(scope); }, within);
}

/** A server-owned command goes in the history of a page/principal, if one is open. */
export function recordInNativeEditorScope(scope: string, key: string, step: (forward: boolean) => Promise<boolean>, handed?: CommandHanded, shown?: boolean) {
  const catcher = catching.get(scope);
  if (catcher && !handed && catcher.named(key) && scopes.has(scope)) {
    catching.delete(scope);
    catcher.take(step);
    return true;
  }
  if (!scopes.get(scope)?.command(key, step, handed, shown)) return false;
  recorded.add(key);
  if (recorded.size > 400) recorded.delete(recorded.keys().next().value!);
  return true;
}

/**
 * The "실행 취소" of the notice that said a command: ⌘Z of the history that
 * holds it, as Notion's (seen 2026-11-18 in two tabs, no script in the page
 * before the press, judged by the dates read after it): it takes back the
 * history's latest step — the command, or a step made after it (a checkbox
 * ticked two seconds after the date came back alone, the date staying) — and
 * ⌘⇧Z does it again at once, ⌘Z after that taking it back again. Once the
 * history holds the command no more there is no step (Notion's, seen
 * 2026-11-18, two tabs each: the notice of dates moved with a date, pressed
 * after the row's window the date was set in had closed, or once a row's
 * window had opened over the page it was set on, took nothing back; pressed
 * in the window still open it took back the date and the dates moved with
 * it — seen once): it has ended (forget), gone with its page, or taken the
 * step back itself. A command no history took — a page without one — is the
 * notice's `own` to take back.
 */
export function stepBackNativeEditorCommand(key: string, own: () => void) {
  if (!recorded.has(key)) return own();
  for (const history of scopes.values()) if (history.holds(key)) return history.back();
}

/**
 * A row opened in a peek over the page ends every history behind it, as
 * Notion's (seen 2026-11-18 in two tabs: once a side or center peek of a row
 * has opened, ⌘Z there or on the page after it closed takes back nothing done
 * before — a cell, the page's text, a new row — nor what was done in it; what
 * is done after it closed is taken back as before). The row's own page keeps
 * its history (`keep`).
 */
export function forgetNativeEditorHistories(keep: (scope: string) => boolean) {
  for (const [scope, history] of [...scopes]) if (!keep(scope)) history.forget();
}
