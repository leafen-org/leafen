import { flushSync } from "react-dom";
import { graphTextBlockTypes, type ContentGraph } from "@/contentGraph";
import { blockTextUndoManager } from "@/lib/blockTextEditing";
import {
  createEditorHistory, fillEditorHistory, recordEditorHistory, remoteGraphRebase,
  undoEditorHistory, redoEditorHistory,
} from "./editorHistoryModel";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { noteChoiceStepped } from "@/lib/steppedChoiceShift";
import { holdNativeEditorScope, letGoOfNativeEditorScope, nativeEditorScope, recordInNativeEditorScope, type CommandHanded, type NativeEditorHistoryScope } from "./nativeEditorUndoScopes";

type TextStackItem = ReturnType<typeof blockTextUndoManager>["undoStack"][number];
type Action<Selection> = {
  graph: boolean;
  texts: TextStackItem[];
  before?: Selection;
  after?: Selection;
  metadataStep?(forward: boolean): void;
  commandId?: object | string;
  commandStep?(forward: boolean): Promise<boolean>;
  /** Its command's step shows what it does as it is taken: it lands at once (recordShownNativeEditorCommand). */
  shown?: boolean;
};
type Snapshot<Selection> = {
  history: ReturnType<typeof createEditorHistory>;
  undo: Action<Selection>[];
  redo: Action<Selection>[];
  textActions: WeakMap<TextStackItem, Action<Selection>>;
};
export type NativeInputCapture = { key: string; from: number; to: number; time: number };
// A transient view remount must not discard the native half of an existing
// Y.Text history. The actual document and principal own this lifetime.
const documentHistory = new WeakMap<object, { scope: string; snapshot: unknown; detach(): void }>();

/** A server-owned command joins the page's same causal text/structure history. */
export function recordCommittedNativeEditorCommand(scope: string, key: string, step: (forward: boolean) => Promise<boolean>) {
  return recordInNativeEditorScope(scope, key, step);
}

/** A server-owned command leaves the page's history, its step done from another's. */
export function dropNativeEditorCommand(scope: string, key: string) {
  nativeEditorScope(scope)?.drop(key);
}

// One that takes a while or is shown as it is taken, one handed over, a half joined and the blocks a command made here (nativeEditorCommandSteps.ts).
export {
  adoptNativeEditorCommandBlocks, joinCommittedNativeEditorCommand, landingNativeEditorCommand, recordHandedNativeEditorCommand,
  recordShownNativeEditorCommand, startNativeEditorCommand,
} from "./nativeEditorCommandSteps";

/** Whether the page/principal's history ends with this command, with nothing undone since. */
export function nativeEditorCommandIsLatest(scope: string, key: string) {
  return nativeEditorScope(scope)?.latest(key) ?? false;
}

/** One causal action owns native structure and local-origin text Undo. */
export function createNativeEditorUndoOwner<Selection>(
  getAdapter: () => NativeEditorGraphAdapter,
  applied: (selection: Selection | undefined) => void,
  selectionNow?: () => Selection | undefined,
  replay?: (forward: boolean) => void,
) {
  const initialDocument = getAdapter().collaborationDocument;
  if (!initialDocument) throw new Error("Native editor Undo requires its collaborative document.");
  const document = initialDocument;
  const adapter = getAdapter();
  const scope = `${adapter.workspaceId}/${adapter.pageId}/${adapter.session?.user.id ?? ""}`;
  const previous = documentHistory.get(document);
  previous?.detach();
  const textUndo = blockTextUndoManager(document);
  const snapshot: Snapshot<Selection> = previous?.scope === scope ? previous.snapshot as Snapshot<Selection> : {
    history: createEditorHistory(adapter.graph), undo: [], redo: [], textActions: new WeakMap(),
  };
  let history = snapshot.history;
  const { undo, redo, textActions } = snapshot;
  // Blocks a command's step took in as they came (adoptCommandBlocks), by that step; and blocks its server half
  // brings and takes away itself, which no step of the page takes in as made here (keepBlocksOutOfNativeEditorHistory).
  const commandBlocks = new Map<string, Action<Selection>>(), keptOut = new Set<string>();
  let active: Action<Selection> | null = null;
  // A graph change made outside run() takes the text changes of the same
  // moment with it — a block's removal its text, a copy the text it copies —
  // as one action (2026-10-25: removed text was a second, empty Undo step).
  let moment: Action<Selection> | null = null;
  function sameMoment(action: Action<Selection>) {
    if (active || moment) return;
    moment = action;
    queueMicrotask(() => { if (moment === action) moment = null; });
  }
  // The action whose server-owned step is on its way: till it has gone it is
  // no step to join (latest), though it still ends the history — a value
  // typed meanwhile is a step of its own, not part of the one being taken
  // back (2026-11-27: a cell's letter joined it, and the next ⌘Z found
  // nothing to take back). A step shown as it is taken lands at once instead.
  let stepping = false, commandPending: Action<Selection> | null = null;
  // ⌘Z and ⌘⇧Z pressed while a server-owned step is on its way, in the
  // order pressed: each goes once the one before it has, as many as were
  // pressed, as Notion's (2026-11-11: those pressed meanwhile were lost).
  // A refused step or a new action of the page's ends the run. What waits for
  // the presses so far to have begun (begun) goes once none waits its turn.
  const pressed: boolean[] = [], waiting: (() => void)[] = [];
  const letWaitingGo = () => { if (!pressed.length) for (const go of waiting.splice(0)) go(); };
  function settled(stepped: boolean) {
    commandPending = null;
    if (!stepped || !canWrite()) pressed.length = 0;
    while (pressed.length && !commandPending) (replay ?? step)(pressed.shift()!);
    letWaitingGo();
  }
  let stopGraph = () => {}, stopFills = () => {}, disposed = false, attached = true;
  let inputCapture: { action: Action<Selection>; capture: NativeInputCapture } | null = null;

  function canWrite() {
    const adapter = getAdapter();
    return !disposed && adapter.collaborationDocument === document
      && documentHistory.get(document)?.scope === scope && !adapter.readOnly && !adapter.signal.aborted && adapter.isCurrent();
  }

  function remember(next: typeof history) { history = next; snapshot.history = next; }

  // A step outside the page's graph and text (an inline database's title) that
  // the next action of this moment takes with it, as one Undo.
  // Asked `within` ms, the next action made then that `takes` (joinNext): a
  // key's blocks removed once a question about them is answered.
  let joining: ((forward: boolean) => void) | null = null, joinTakes: ((graph: ContentGraph) => boolean) | undefined;
  function withNext(step: (forward: boolean) => void, within?: number, takes?: (graph: ContentGraph) => boolean) {
    joining = step; joinTakes = takes;
    const done = () => { if (joining === step) joining = null; };
    if (within) window.setTimeout(done, within); else queueMicrotask(done);
  }

  function push(action: Action<Selection>) {
    if (joining && (!joinTakes || joinTakes(getAdapter().graph))) {
      const step = joining, prior = action.metadataStep;
      joining = null;
      action.metadataStep = prior ? (forward) => { prior(forward); step(forward); } : step;
    }
    if (undo.at(-1) !== action) undo.push(action);
    if (undo.length > 200) undo.shift();
    redo.length = 0;
    pressed.length = 0; letWaitingGo();
  }

  function textAdded(event: { type: "undo" | "redo"; stackItem: TextStackItem }) {
    if (stepping || event.type !== "undo" || textUndo.undoing || textUndo.redoing) return;
    const action = active ?? moment ?? { graph: false, texts: [] };
    sameMoment(action);
    action.texts.push(event.stackItem);
    textActions.set(event.stackItem, action);
    push(action);
  }
  function textUpdated(event: { type: "undo" | "redo"; stackItem: TextStackItem }) {
    if (stepping || event.type !== "undo") return;
    const action = textActions.get(event.stackItem);
    if (action && active) {
      action.before ??= active.before;
      action.after = active.after;
    }
  }
  textUndo.on("stack-item-added", textAdded);
  textUndo.on("stack-item-updated", textUpdated);

  function observeAdapter() {
    stopGraph(); stopFills();
    const adapter = getAdapter();
    stopFills = adapter.observeFilledBlocks?.((blockId, type, content) => {
      if (!disposed) remember(fillEditorHistory(history, blockId, type, content));
    }) ?? (() => {});
    history = snapshot.history;
    remember(remoteGraphRebase(history, adapter.graph));
    stopGraph = adapter.observeLocalGraphChanges((before, after, operations) => {
      if (stepping || disposed) return;
      activate();
      const action = active ?? moment ?? { graph: false, texts: [] };
      sameMoment(action);
      const previous = { ...history, present: before };
      remember(recordEditorHistory(previous, after, 200, action.graph, operations));
      if (history.past === previous.past) return;
      action.graph = true;
      push(action);
    });
  }
  observeAdapter();

  /** Runs a change as one action of the history; the action, when the change made one. */
  function run(change: () => void, structural = false, before?: Selection, after?: () => Selection | undefined, capture?: NativeInputCapture) {
    if (!canWrite()) return null;
    activate();
    if (active) { change(); return active; }
    if (structural) textUndo.stopCapturing();
    const prior = inputCapture;
    // Only adjacent field typing shares the existing text capture interval.
    // Structure, paste, menu actions and IO acknowledgements never use it.
    const combine = !!capture && !!prior && undo.at(-1) === prior.action && !redo.length
      && capture.key === prior.capture.key && capture.from === prior.capture.to
      && capture.time - prior.capture.time < textUndo.captureTimeout;
    const action: Action<Selection> = combine ? prior!.action : { graph: false, texts: [], before };
    active = action;
    try {
      change();
      action.after = after?.();
      inputCapture = capture && action.graph && !action.texts.length ? { action, capture } : null;
      // Text-only typing can extend an existing Yjs capture group. Its action
      // identity is the stack item, rather than a clock window around graph IO.
      if (!action.graph && !action.texts.length) {
        const item = textUndo.undoStack.at(-1);
        const previous = item && textActions.get(item);
        if (previous) { previous.before ??= before; previous.after = action.after; }
      }
    } finally {
      active = null;
      if (structural) textUndo.stopCapturing();
    }
    return undo.at(-1) === action ? action : null;
  }

  /**
   * A change that comes later to an action, as the copies a paste waited for
   * do: one ⌘Z with it while it is the history's latest, or one of its own
   * once another came after it. Null when the action was undone meanwhile,
   * changing nothing.
   */
  function follow(action: Action<Selection>, change: () => void, after?: () => Selection | undefined) {
    if (!canWrite() || !undo.includes(action)) return null;
    if (undo.at(-1) !== action || redo.length || active) return run(change, true, selectionNow?.(), after);
    activate();
    textUndo.stopCapturing();
    active = action;
    try {
      change();
      action.after = after?.() ?? action.after;
    } finally { active = null; textUndo.stopCapturing(); }
    return action;
  }

  /** A change to blocks without text that is no step of the history: what an undone action would have taken in. */
  function aside(change: () => void) {
    if (!canWrite()) return;
    stepping = true;
    try { change(); } finally { stepping = false; }
    remember(remoteGraphRebase(history, getAdapter().graph));
  }

  /**
   * A server-owned command: one step of this history, which its own step
   * takes back. Handed from another history, it goes on top of this one's
   * steps to take back or to do again, leaving the rest as they are.
   */
  function command(key: string, commandStep: (forward: boolean) => Promise<boolean>, handed?: CommandHanded, shown?: boolean) {
    if (!canWrite()) return false;
    if ([...undo, ...redo].some((action) => action.commandId === key)) return true;
    activate(); textUndo.stopCapturing(); inputCapture = null;
    remember({ ...history, present: getAdapter().graph });
    const action: Action<Selection> = { graph: false, texts: [], commandId: key, commandStep, before: selectionNow?.(), shown };
    if (handed === "undone") redo.push(action);
    else if (handed === "done") { undo.push(action); if (undo.length > 200) undo.shift(); }
    else push(action);
    return true;
  }

  function adoptCommandBlocks(key: string, ids: readonly string[]) {
    const action = undo.at(-1), graph = getAdapter().graph;
    if (!canWrite() || action?.commandId !== key || action.graph || redo.length || !ids.length
      || !ids.every((id) => graph.blocks.some((block) => block.id === id))) return false;
    const made = new Set(ids);
    remember(recordEditorHistory({ ...history, present: { ...graph, blocks: graph.blocks.filter((block) => !made.has(block.id)) } }, graph, 200, false));
    action.graph = true;
    for (const id of ids) commandBlocks.set(id, action);
    return true;
  }

  function drop(key: string) {
    for (const stack of [undo, redo]) {
      const index = stack.findIndex((action) => action.commandId === key);
      if (index >= 0) stack.splice(index, 1);
    }
  }

  function join(commandStep: (forward: boolean) => Promise<boolean>) {
    const action = undo.at(-1);
    if (!canWrite() || !action?.graph || action.commandStep) return false;
    action.commandStep = commandStep;
    return true;
  }

  function step(forward: boolean, apply = true) {
    if (!canWrite()) return false;
    if (commandPending) {
      if (apply) pressed.push(forward);
      return true;
    }
    if (apply) activate(); else history = snapshot.history;
    const source = forward ? redo : undo;
    const action = source.at(-1);
    if (!action) return false;
    const textSource = forward ? textUndo.redoStack : textUndo.undoStack;
    const prefixLength = textSource.length - action.texts.length;
    if (prefixLength < 0 || action.texts.some((item, index) => textSource[prefixLength + index] !== item)) return false;
    if (!apply) return true;
    if (action.commandStep && action.graph) {
      const half = action.commandStep;
      commandPending = action;
      // Going back, the history's half is drawn in the frame its server half let go of what it showed (showBlocksBack's
      // `asMoved`), not behind it, the page as the server held it showing between. A step made meanwhile with no text or
      // blocks here (a title typed after the key) stays after it, its redo gone: the half was left undone (2026-11-22).
      void (forward
        ? Promise.resolve(stepInHistory(action, source, forward)).then(() => half(true))
        : half(false).then((saved) => {
          const after = source.slice(source.indexOf(action) + 1);
          if (saved && canWrite() && source.includes(action) && after.every((made) => !made.graph && !made.texts.length)) flushSync(() => {
            source.splice(source.length - after.length);
            try { stepInHistory(action, source, forward); } finally { if (after.length) { source.push(...after); redo.length = 0; } }
          });
          return saved;
        }))
        .then(settled, () => settled(false));
      return true;
    }
    if (action.commandStep && action.shown && !action.graph) {
      // Shown as it is taken, it lands at once, the server told behind it;
      // refused, it goes back where it was while nothing came after it.
      inputCapture = null;
      textUndo.stopCapturing();
      const into = forward ? undo : redo;
      source.pop(); into.push(action);
      void action.commandStep(forward).catch(() => false).then((saved) => {
        if (!saved && into.at(-1) === action) { into.pop(); source.push(action); }
      });
      applied(forward ? action.after : action.before);
      return true;
    }
    if (action.commandStep) {
      commandPending = action;
      inputCapture = null;
      textUndo.stopCapturing();
      void action.commandStep(forward).then((saved) => {
        if (!saved || !canWrite()) return false;
        // Its blocks came while this waited for them: they go, or come back, with it.
        if (action.graph && source.at(-1) === action) return stepInHistory(action, source, forward);
        const index = source.indexOf(action);
        if (index < 0) return true;
        const latest = index === source.length - 1;
        source.splice(index, 1);
        // A new user action while this response was pending owns the new end
        // of the history; an old Undo must not resurrect its discarded Redo.
        if (latest) (forward ? undo : redo).push(action);
        applied(forward ? action.after : action.before);
        return true;
      }).then(settled, () => settled(false));
      return true;
    }
    return stepInHistory(action, source, forward);
  }

  function stepInHistory(action: Action<Selection>, source: Action<Selection>[], forward: boolean) {
    const textSource = forward ? textUndo.redoStack : textUndo.undoStack;
    const prefixLength = textSource.length - action.texts.length;
    inputCapture = null;
    const adapter = getAdapter();
    const current = remoteGraphRebase(history, adapter.graph);
    // Project native history against current peer state before changing either
    // authority. However many operations an Undo publishes, it goes as the
    // action it undoes went: the graph write queue saves it a change of at
    // most 1,000 operations at a time (2026-10-13: past
    // 1,000 operations Undo did nothing, without a word).
    textUndo.stopCapturing();
    stepping = true;
    try {
      // Yjs skips ineffective stack items (for example after an API deletion).
      // Temporarily isolate this causal action in the public Undo stack so a
      // skip cannot consume text belonging to an earlier native graph action.
      const stepTexts = () => {
        const prefix = textSource.splice(0, prefixLength);
        const destination = forward ? textUndo.undoStack : textUndo.redoStack;
        const start = destination.length;
        try {
          while (textSource.length) {
            if (forward) textUndo.redo(); else textUndo.undo();
          }
          action.texts = destination.slice(start);
          for (const item of action.texts) textActions.set(item, action);
        } finally { textSource.unshift(...prefix); }
      };
      // Text steps before structure both ways. The server takes a removed
      // block's text with it, outside this history, so a Redo that removed
      // the block first left its own text removal nothing to do and the
      // next Undo nothing to bring back (2026-10-25: text lost).
      stepTexts();
      if (action.graph) {
        // After text Undo, surviving text on a locally created block belongs
        // to a peer and keeps that block visible.
        const retained = new Set(adapter.graph.blocks.filter(block => graphTextBlockTypes.includes(block.type as never)
          && document.getText(`block:${block.id}`).length).map(block => block.id));
        remember(forward ? redoEditorHistory(current) : undoEditorHistory(current, retained));
        adapter.restoreGraph(history.present, history.publication);
      }
      action.metadataStep?.(forward);
      source.pop();
      (forward ? undo : redo).push(action);
      applied(forward ? action.after : action.before);
      return true;
    } finally { stepping = false; textUndo.stopCapturing(); }
  }

  function detach() {
    attached = false;
    textUndo.off("stack-item-added", textAdded);
    textUndo.off("stack-item-updated", textUpdated);
  }
  function activate() {
    if (attached || !canWrite()) return;
    documentHistory.get(document)?.detach();
    history = snapshot.history;
    inputCapture = null;
    textUndo.stopCapturing();
    textUndo.on("stack-item-added", textAdded); textUndo.on("stack-item-updated", textUpdated);
    attached = true;
    documentHistory.set(document, { scope, snapshot, detach });
    holdNativeEditorScope(scope, outside);
  }
  function latest(key: string) {
    return !redo.length && undo.at(-1)?.commandId === key && undo.at(-1) !== commandPending;
  }
  // The history ends here (forgetNativeEditorHistories): its text and
  // structure steps and the commands in it, both ways, as at the page's opening.
  function forget() {
    if (disposed || active) return;
    undo.length = 0; redo.length = 0; pressed.length = 0; letWaitingGo();
    inputCapture = null; commandBlocks.clear();
    textUndo.stopCapturing(); textUndo.clear();
    remember(createEditorHistory(getAdapter().graph));
  }
  const outside: NativeEditorHistoryScope = { command, drop, latest, join, adopt: adoptCommandBlocks, keepOut: (ids) => { for (const id of ids) keptOut.add(id); }, forget, holds: (key) => undo.some((action) => action.commandId === key), back: () => { (replay ?? step)(false); },
    joinNext: (step, within, takes) => withNext(step, within, takes),
    begun: () => pressed.length ? new Promise<void>((go) => { waiting.push(go); }) : Promise.resolve() };
  function dispose() {
    disposed = true; stopGraph(); stopFills(); detach(); document.off("destroy", dispose); pressed.length = 0; letWaitingGo();
    letGoOfNativeEditorScope(scope, outside);
  }
  document.on("destroy", dispose);
  documentHistory.set(document, { scope, snapshot, detach });
  holdNativeEditorScope(scope, outside);
  if (!previous) document.once("destroy", () => {
    documentHistory.get(document)?.detach();
    documentHistory.delete(document);
  });

  return {
    run,
    follow, followOrRun: (change: () => void, step: unknown) => (step ? follow(step as Action<Selection>, change) : null) ?? run(change, true),
    aside,
    withNext,
    /** What the action just made ended with, once the page has it. */
    noteAfter(after: Selection) { const action = undo.at(-1); if (action) action.after = after; },
    /**
     * Blocks the server made for this person (a database's copy) join the
     * action being run, as if made here; ones a command's step took in
     * already stay its, ending with `after`.
     */
    adoptServerBlocks(ids: readonly string[], after?: Selection) {
      const owned = ids.length ? commandBlocks.get(ids[0]) : undefined;
      if (owned && ids.every((id) => commandBlocks.get(id) === owned)) {
        if (after !== undefined) owned.after = after;
        return;
      }
      if (ids.some((id) => keptOut.delete(id)) || !active) return;
      const made = new Set(ids), graph = getAdapter().graph;
      remember(recordEditorHistory({ ...history, present: { ...graph, blocks: graph.blocks.filter((block) => !made.has(block.id)) } }, graph, 200, active.graph));
      active.graph = true;
      push(active);
    },
    // A step taken leaves its choice taking the first Shift+↑ or ↓ as nothing (steppedChoiceShift.ts).
    undo: (forward: boolean, apply = true) => { const taken = step(forward, apply); if (taken && apply) noteChoiceStepped(); return taken; },
    /** Whether an action is in the history still, not undone. */
    holds: (action: Action<Selection>) => undo.includes(action),
    isCurrent: () => !disposed && documentHistory.get(document)?.scope === scope,
    observeAdapter,
    rebase(graph: ContentGraph) { remember(remoteGraphRebase(history, graph)); },
    dispose,
  };
}
