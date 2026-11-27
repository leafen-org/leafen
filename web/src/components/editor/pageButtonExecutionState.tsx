import { createContext, useContext, useLayoutEffect, useMemo, useState, useSyncExternalStore, type ReactNode } from "react";
import type { GraphBlock } from "@/contentGraph";
import type { ButtonClientOutcome, ButtonConfirmationChallenge } from "@/lib/buttonExecutionContract";

type Run = { current: string | null; phase?: { inserted: string[]; created: number; edited: number; tasks: boolean; first: string; canUndo: boolean; lastWindow: string; outcomes: ButtonClientOutcome[] } };
type State = { running: boolean; challenge: ButtonConfirmationChallenge | null };
const empty: State = { running: false, challenge: null };
function createStore() {
 const entries = new Map<string, State>();
 const runs = new Map<string, Run>();
 const listeners = new Map<string, Set<() => void>>();
 let alive = new Set<string>();
 return {
  read: (id: string) => entries.get(id) ?? empty,
  run(id: string) { let value = runs.get(id); if (!value) { value = { current: null }; runs.set(id,value); } return value; },
  subscribe(id: string, callback: () => void) { const set = listeners.get(id) ?? new Set();set.add(callback);listeners.set(id,set);return () => { set.delete(callback);if (!set.size) listeners.delete(id); }; },
  patch(id: string, next: Partial<State>) {
   const previous = this.read(id);
   if (!alive.has(id) && !previous.running && !previous.challenge) return;
   entries.set(id,{...previous,...next});listeners.get(id)?.forEach(callback => callback());
  },
  prune(blocks: readonly GraphBlock[]) {
   alive = new Set(blocks.filter(block => block.type === "button").map(block => block.id));
   // A copy fence can temporarily clear the visible graph. Its active run
   // survives that projection gap until the completed reread restores it.
   for (const id of new Set([...entries.keys(),...runs.keys()])) {
    const state = entries.get(id);
    if (!alive.has(id) && !state?.running && (blocks.length > 0 || !state?.challenge)) { entries.delete(id);runs.delete(id); }
   }
  },
 };
}
type Scope = { identity: string; store: ReturnType<typeof createStore> };
const Context = createContext<Scope | null>(null);

/** Page/session lifetime owns a running button. PM node views and Y.Doc
 * projections may remount during native copy without losing its question. */
export function PageButtonExecutionScope({ identity, blocks, children }: { identity: string; blocks: readonly GraphBlock[]; children: ReactNode }) {
 const parent = useContext(Context);
 if (parent?.identity === identity) return children;
 return <OwnedScope key={identity} identity={identity} blocks={blocks}>{children}</OwnedScope>;
}
function OwnedScope({ identity, blocks, children }: { identity: string; blocks: readonly GraphBlock[]; children: ReactNode }) {
 const [store] = useState(createStore);
 useLayoutEffect(() => store.prune(blocks),[blocks,store]);
 const value = useMemo(() => ({identity,store}),[identity,store]);
 return <Context.Provider value={value}>{children}</Context.Provider>;
}
export function usePageButtonExecutionState(id: string) {
 const scope = useContext(Context);
 if (!scope) throw new Error("Page buttons require their editor execution scope.");
 const { store } = scope;
 const state = useSyncExternalStore(callback => store.subscribe(id,callback),() => store.read(id),() => empty);
 return { ...state, run: store.run(id), setRunning: (running: boolean) => store.patch(id,{running}), setChallenge: (challenge: ButtonConfirmationChallenge | null) => store.patch(id,{challenge}) };
}
