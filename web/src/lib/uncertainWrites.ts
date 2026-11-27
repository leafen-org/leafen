// A write that went out and got no answer (failed, or given up on) may have
// reached the server all the same, once a network that stopped goes again
// (2026-11-05: two settings shown as not saved, one of them a workspace's
// invite link, were on a minute later). Once the server answers again, and
// the write itself has had a moment to land, what the screen shows is read
// again.

import { useCallback, useEffect, useRef, useState } from "react";

const SETTLE_MS = 3_000;
const listeners = new Set<() => void>();
// Makes sent again, the same, once the server answers (useSendAgainWhenAnswered):
// they go first, and the screens read again once they have landed, so what
// they made is in what is read (2026-11-06: a webhook sent again landed after
// the settings had read their list, and its secret, shown once, was lost).
const sendsAgain = new Set<() => Promise<unknown>>();
let pending = false;
let timer: ReturnType<typeof setTimeout> | undefined;

/** A write ended without the server's answer: whether it was taken is not known. */
export function noteUncertainWrite() {
  pending = true;
}

/** The server answered a request: after a write whose outcome wasn't known, the screen reads again. */
export function noteAnswerAfterUncertainWrite() {
  if (!pending) return;
  pending = false;
  clearTimeout(timer);
  timer = setTimeout(() => {
    const again = [...sendsAgain];
    sendsAgain.clear();
    void Promise.allSettled(again.map((send) => send())).then(() => {
      for (const listener of [...listeners]) listener();
    });
  }, SETTLE_MS);
}

/** Whether an error is a write's that ended without the server's answer. */
export function isUncertainWrite(error: unknown) {
  return (error as { writeUncertain?: unknown } | null)?.writeUncertain === true;
}

/** Hears the server answer again after a write whose outcome wasn't known. */
export function onUncertainWritesSettled(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** A count that goes up each time the server answers again after a write
 * whose outcome wasn't known: a screen that shows what it read reads again
 * on it, the settings, the share window and the lists a write there changes. */
export function useReadAgainAfterUncertainWrites() {
  const [count, setCount] = useState(0);
  useEffect(() => onUncertainWritesSettled(() => setCount((value) => value + 1)), []);
  return count;
}

/** Runs a make so one sent without the answer isn't made twice (useMadeWithoutAnswer). */
export type MakeOnce = <R>(make: () => Promise<R>) => Promise<R>;

/**
 * A window that makes something (a group, a webhook, a bookmark…) keeps a
 * make sent without the server's answer from being made twice. The returned
 * function runs the make: the items `items` held as it went out are kept,
 * and if it ends without the answer the list is read again once the server
 * answers (`readAgain`, for a screen that doesn't already); the first item it
 * then holds that wasn't there is that make, and `landed` takes it, as the
 * answer would have (2026-11-06: a version bookmark and an automation were
 * made while their windows stayed open with the button free, and pressing
 * again would have made a second). An answered or refused make keeps nothing.
 */
export function useMadeWithoutAnswer<T>({ items, idOf, readAgain, landed }: {
  items: readonly T[] | null | undefined;
  idOf: (item: T) => string;
  readAgain?: () => void;
  landed: (item: T) => void;
}) {
  const latest = useRef({ items, idOf, readAgain, landed });
  latest.current = { items, idOf, readAgain, landed };
  const before = useRef<Set<string> | null>(null);
  const look = useCallback(() => {
    const seen = before.current;
    const { items: now, idOf: id, landed: take } = latest.current;
    if (!seen || !now) return;
    const made = now.find((item) => !seen.has(id(item)));
    if (made === undefined) return;
    before.current = null;
    take(made);
  }, []);
  useEffect(() => onUncertainWritesSettled(() => { if (before.current) latest.current.readAgain?.(); }), []);
  useEffect(look, [items, look]);
  return useCallback<MakeOnce>(async <R>(make: () => Promise<R>): Promise<R> => {
    const { items: now, idOf: id } = latest.current;
    const seen = new Set((now ?? []).map(id));
    before.current = null;
    try {
      return await make();
    } catch (error) {
      if (isUncertainWrite(error)) {
        before.current = seen;
        look();
      }
      throw error;
    }
  }, [look]);
}

/**
 * A make the server answers the same for the same request — it knows what it
 * made by the request's id — that went without the server's answer is sent
 * again, the same, once the server answers again, and ends as an answered one
 * does (2026-11-06: an emoji's window said the upload failed and left the
 * person to press again). `sendAgain(error, repeat)` says whether it will.
 */
export function useSendAgainWhenAnswered() {
  // A window closed before the server answers sends nothing again.
  const again = useRef<(() => Promise<unknown>) | null>(null);
  useEffect(() => () => {
    if (again.current) sendsAgain.delete(again.current);
    again.current = null;
  }, []);
  return useCallback((error: unknown, repeat: () => Promise<unknown>) => {
    if (!isUncertainWrite(error)) return false;
    if (again.current) sendsAgain.delete(again.current);
    const send = () => {
      if (again.current === send) again.current = null;
      return repeat();
    };
    again.current = send;
    sendsAgain.add(send);
    return true;
  }, []);
}
