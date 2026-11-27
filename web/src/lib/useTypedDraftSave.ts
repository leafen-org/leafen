import { useEffect, useRef } from "react";

// A name field that saves when it is left also saves while it is typed in,
// shortly after the typing stops and when the window is hidden or the page
// left, as the page title does, so an edit isn't lost when the app is ended
// mid-edit; the field keeps its focus. Leaving or confirming still saves at
// once, after settle() stops what is pending; a cancel puts back the value
// the edit began with when a draft was saved meanwhile. A pause while an
// input method still composes (`holding`) saves nothing yet: what the field
// shows typed elsewhere stands over the name it had until the composition ends.

export const TYPED_DRAFT_SAVE_MS = 600;

type DraftState = {
  timer: number;
  saved: boolean;
  initial: string;
  last: string;
  draft: string;
  save: (draft: string) => void;
  holding?: () => boolean;
};

function saveNow(current: DraftState) {
  window.clearTimeout(current.timer);
  if (current.draft === current.last) return;
  current.last = current.draft;
  current.saved = true;
  current.save(current.draft);
}

export function useTypedDraftSave({ active, draft, initial, save, holding }: {
  active: boolean;
  draft: string;
  /** The value the edit began with, which needs no saving. */
  initial: string;
  save: (draft: string) => void;
  /** Whether the pause's save waits (an input method composing); a hidden window or a page left saves at once. */
  holding?: () => boolean;
}) {
  const state = useRef<DraftState>({ timer: 0, saved: false, initial, last: initial, draft, save, holding });
  state.current.draft = draft;
  state.current.save = save;
  state.current.holding = holding;

  // An edit begins from its value as it was.
  const beganWith = useRef(initial);
  beganWith.current = initial;
  useEffect(() => {
    if (!active) return;
    Object.assign(state.current, { saved: false, initial: beganWith.current, last: beganWith.current });
  }, [active]);

  useEffect(() => {
    const current = state.current;
    if (!active || draft === current.last) return undefined;
    const pause = () => { current.timer = window.setTimeout(() => (current.holding?.() ? pause() : saveNow(current)), TYPED_DRAFT_SAVE_MS); };
    pause();
    return () => window.clearTimeout(current.timer);
  }, [active, draft]);

  useEffect(() => {
    if (!active) return undefined;
    const current = state.current;
    const onHidden = () => {
      if (document.visibilityState === "hidden") saveNow(current);
    };
    const onPageHide = () => saveNow(current);
    document.addEventListener("visibilitychange", onHidden);
    window.addEventListener("pagehide", onPageHide);
    return () => {
      document.removeEventListener("visibilitychange", onHidden);
      window.removeEventListener("pagehide", onPageHide);
    };
  }, [active]);

  return {
    /** Stops a pending save: the field saves or cancels now. */
    settle() {
      window.clearTimeout(state.current.timer);
    },
    /** The value to put back on a cancel, or null when nothing was saved. */
    restoreValue(): string | null {
      return state.current.saved ? state.current.initial : null;
    },
  };
}
