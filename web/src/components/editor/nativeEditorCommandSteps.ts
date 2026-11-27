import type { ContentGraph } from "@/contentGraph";
import { catchNativeEditorCommand, nativeEditorScope, recordInNativeEditorScope, type CommandHanded } from "./nativeEditorUndoScopes";

// What reaches a page/principal's history (nativeEditorUndoOwner) of a
// server-owned command from outside its editor: one that takes a while, one
// shown as it is taken, one handed over from another page's history, a half
// joined to the action just made, and the blocks it made here taken in as
// its step's own.

type CommandStep = (forward: boolean) => Promise<boolean>;

/**
 * The step of a server-owned command that takes a while (a database turned
 * into a page, blocks moved to another page), for the history as it starts:
 * a ⌘Z pressed before the server has done it waits for it and takes it
 * back, as Notion's does, rather than the action before it, whose write the
 * page then refused while the command held it (2026-11-11: another block's
 * indent came undone, and the screen kept it apart from what was saved).
 * Landed, the step is the command's own; `fail` gives the step a command
 * that didn't happen takes.
 */
export function landingNativeEditorCommand(fail: () => Promise<boolean> = () => Promise.resolve(false)) {
  let settle: (step: CommandStep) => void = () => {};
  const landing = new Promise<CommandStep>((resolve) => { settle = resolve; });
  let asked = false;
  return {
    step: async (forward: boolean) => { asked = true; return (await landing)(forward); },
    land: (step: CommandStep) => settle(step),
    fail: () => settle(fail),
    /** Whether a ⌘Z or ⌘⇧Z has asked for the step, waiting for it to land or not. */
    get asked() { return asked; },
  };
}

/**
 * A server-owned command that takes a while, one step of the page's history
 * from the moment it starts (landingNativeEditorCommand): `land` once the
 * server has done it, `fail` when it didn't, which takes it out again.
 */
export function startNativeEditorCommand(scope: string, key: string) {
  const command = landingNativeEditorCommand();
  recordInNativeEditorScope(scope, key, command.step);
  return {
    land: command.land,
    fail() { command.fail(); nativeEditorScope(scope)?.drop(key); },
    get asked() { return command.asked; },
  };
}

/**
 * A server-owned command whose step shows what it does as it is taken, the
 * server told behind it (a table's values, lib/valueSteps.ts): taken back or
 * done again it lands at once, so a ⌘Z or a value typed right after it goes
 * on from it, not waiting for the server's answer (2026-11-27: a letter typed
 * again on a cell its ⌘Z had just taken back joined that step, still waiting,
 * and the next ⌘Z found nothing to take back). Refused, it goes back where
 * it was while nothing came after it. Whether a history took it.
 */
export function recordShownNativeEditorCommand(scope: string, key: string, step: CommandStep) {
  return recordInNativeEditorScope(scope, key, step, undefined, true);
}

/**
 * A server-owned command still to come — `named` names it — that the next
 * action of the page's history, made within `within` ms, takes with it as
 * one ⌘Z, the first such that `takes`: a joined choice's items moved to the
 * trash, their step coming with the server's answer, and its blocks removed
 * at the press or once a question about them is answered (pageViewJoin.ts),
 * as Notion's one ⌘Z brings both back (seen 2026-11-27). Its half goes as the
 * action's own is taken, shown as it is taken, not waited for: one taken
 * before the command came goes once it comes. `stepped` hears each step
 * taken, forward or back. False where the page keeps no history to join.
 */
export function joinComingNativeEditorCommand(scope: string, named: (key: string) => boolean,
  stepped: (forward: boolean) => void, takes: (graph: ContentGraph) => boolean, within = 30_000) {
  const history = nativeEditorScope(scope);
  if (!history) return false;
  const coming = landingNativeEditorCommand();
  catchNativeEditorCommand(scope, named, coming.land, within);
  window.setTimeout(coming.fail, within);
  history.joinNext((forward) => { void coming.step(forward); stepped(forward); }, within, takes);
  return true;
}

/**
 * A server-owned command done or undone from another page's history, which
 * brought its blocks here with the caret: this page's ⌘Z takes it back, or
 * its ⌘⇧Z does it again, and what this page may still do again stays.
 */
export function recordHandedNativeEditorCommand(
  scope: string, key: string, step: (forward: boolean) => Promise<boolean>, handed: CommandHanded,
) {
  return recordInNativeEditorScope(scope, key, step, handed);
}

/**
 * A server-owned half of the action just made in the page's history: going
 * back it steps first, going forward last; the action stays one ⌘Z.
 */
export function joinCommittedNativeEditorCommand(scope: string, step: (forward: boolean) => Promise<boolean>) {
  return nativeEditorScope(scope)?.join(step) ?? false;
}

/**
 * Blocks a server-owned command made here, once the page has them, are its
 * step's own: one ⌘Z takes them away with it, a ⌘Z pressed while they were
 * on their way too (a database's ⌘D, as Notion's). False where the command
 * is no longer the history's latest, or made no step.
 */
export function adoptNativeEditorCommandBlocks(scope: string, key: string, ids: readonly string[]) {
  return nativeEditorScope(scope)?.adopt(key, ids) ?? false;
}

/**
 * Blocks the server brought here by a half its step joins (a cut database
 * item pasted, cutPagePaste.ts), which that half takes away and brings back
 * itself: the action they come with doesn't take them in as made here, whose
 * ⌘Z would remove them as blocks, its ⌘⇧Z put them back as blocks of a page
 * that is a database item again. False where the page keeps no history.
 */
export function keepBlocksOutOfNativeEditorHistory(scope: string, ids: readonly string[]) {
  const history = nativeEditorScope(scope);
  history?.keepOut(ids);
  return !!history;
}
