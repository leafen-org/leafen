// A change a press makes to a setting shows at once and goes to its owner
// (the server, or the desktop app) behind it, the control free all the while
// (user decision 2026-11-12: a press answers on the next frame; the settings
// of a page, a workspace or an account aren't among the exceptions the server
// decides first). The one way back: a refused change puts back what its owner
// holds — the value shown before, or an earlier change's answer — and its
// place says why. Changes on one lane go out one after another in the order
// they were pressed, so an answer never comes back older than the one before
// it; a change still unanswered after ten seconds says so under its control,
// as a locked control did (lib/pendingChoice.ts, 2026-11-06) — the server's
// words only where the server is the owner: the desktop app's own settings
// answer from this computer (`waitsOn`, as lib/switchTurns.ts's locks).

import { useEffect, useRef, useState } from "react";
import { WAITING_NOTE_MS, waitingNoteText } from "./pendingChoice";

type Key<T> = Extract<keyof T, string>;

export function useShownChanges<T extends object>(waitsOn: "server" | "device" = "server") {
  const [shown, setShown] = useState<Partial<T>>({});
  const [slow, setSlow] = useState<ReadonlySet<string>>(() => new Set());
  // Each key's latest change: an answer to an earlier one leaves it shown.
  const latest = useRef(new Map<string, number>());
  const lanes = useRef(new Map<string, Promise<unknown>>());
  const turns = useRef(0);
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => { live.current = false; };
  }, []);

  function settle(keys: readonly string[], turn: number) {
    const done = keys.filter((key) => latest.current.get(key) === turn);
    for (const key of done) latest.current.delete(key);
    if (!live.current || done.length === 0) return;
    setShown((current) => {
      const next = { ...current } as Record<string, unknown>;
      for (const key of done) delete next[key];
      return next as Partial<T>;
    });
    setSlow((current) => (done.some((key) => current.has(key)) ? new Set([...current].filter((key) => !done.includes(key))) : current));
  }

  return {
    /** The values pressed and not yet answered, over what the owner holds. */
    shown,
    /**
     * Shows `values` at once and sends them with `send` after the lane's
     * earlier changes; its promise is `send`'s, so the caller tells a refusal
     * where its control is. `send` puts the answer where the screen reads it.
     */
    change<R>(values: Partial<T>, send: () => Promise<R>, lane = "settings"): Promise<R> {
      const turn = turns.current += 1;
      const keys = Object.keys(values);
      for (const key of keys) latest.current.set(key, turn);
      setShown((current) => ({ ...current, ...values }));
      const timer = window.setTimeout(() => {
        const waiting = keys.filter((key) => latest.current.get(key) === turn);
        if (waiting.length && live.current) setSlow((current) => new Set([...current, ...waiting]));
      }, WAITING_NOTE_MS);
      const sent = (lanes.current.get(lane) ?? Promise.resolve()).then(send);
      const tail = sent.then(() => undefined, () => undefined);
      lanes.current.set(lane, tail);
      void tail.then(() => {
        window.clearTimeout(timer);
        settle(keys, turn);
        if (lanes.current.get(lane) === tail) lanes.current.delete(lane);
      });
      return sent;
    },
    /** What a control whose change has waited ten seconds says under it. */
    note(key: Key<T>) {
      return slow.has(key) && waitsOn === "server" ? waitingNoteText() : undefined;
    },
  };
}
