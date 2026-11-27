import { useCallback, useEffect, useRef } from "react";

/** What a person does to move a screen themselves, as a page's kept place counts it (pageScrollPlace.ts). */
const PERSON_MOVES = ["wheel", "pointerdown", "keydown", "touchstart"] as const;

/**
 * Whether the person turned the wheel, pressed, touched or pressed a key
 * while `watching` held — a step still loading its content — so that when
 * the content comes the step keeps the place they moved to rather than going
 * back to its top under them (2026-11-19: the first run's address step,
 * scrolled down to "나중에" while the network was measured, went back to its
 * top as the choices came, and the press meant for "나중에" landed on the
 * example). Each start of `watching` starts it over; the answer is taken
 * once, so coming back to the step later goes to its top again.
 */
export function usePersonMoved(watching: boolean): () => boolean {
  const moved = useRef(false);
  useEffect(() => {
    if (!watching) return undefined;
    moved.current = false;
    const mark = () => { moved.current = true; };
    for (const type of PERSON_MOVES) window.addEventListener(type, mark, { capture: true, passive: true });
    return () => { for (const type of PERSON_MOVES) window.removeEventListener(type, mark, true); };
  }, [watching]);
  return useCallback(() => {
    const was = moved.current;
    moved.current = false;
    return was;
  }, []);
}
