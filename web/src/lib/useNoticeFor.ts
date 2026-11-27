import { useCallback, useEffect, useState } from "react";

/**
 * A result a screen says (saved, sent, failed) held to what it was about:
 * `about` names that — the settings it tested, the section it was saved in —
 * and once it changes the result goes, so it never reads as the state now
 * (2026-10-26: a test mail's failure stayed after the port was fixed and
 * saved, and "saved" followed the administrator to the next section). The
 * result belongs to `about` as it is once the result is shown, so a save
 * that changes both at once keeps its "saved".
 */
export function useNoticeFor<T>(about: string): [T | null, (value: T | null) => void] {
  const [held, setHeld] = useState<{ value: T; about: string | null } | null>(null);
  useEffect(() => {
    if (!held) return;
    if (held.about === null) setHeld({ value: held.value, about });
    else if (held.about !== about) setHeld(null);
  }, [held, about]);
  // An empty text says nothing, as no result does: screens clear their words
  // with "" as each key is typed, and held as a result it was tied to the
  // fields once more after every key — under fast typing those second passes
  // piled up past React's update limit (2026-11-06: the 51st letter typed
  // into a backup folder was lost, and on the next the settings fell to the
  // error screen).
  const set = useCallback((value: T | null) => setHeld(value === null || value === "" ? null : { value, about: null }), []);
  return [held && (held.about === null || held.about === about) ? held.value : null, set];
}

/**
 * What an action on one row of a list said (a disconnect or a sign-in that
 * failed), shown under that row and held to it as useNoticeFor holds a
 * screen's: `rowState` reads how a row stands now — undefined once it is gone
 * or out of sight — and once the row the notice is about reads otherwise
 * (connected anew, ended, changed in another window) the notice goes
 * (2026-11-20: a service's failed sign-in stayed under the list after the
 * service was connected anew). One at a time: a new notice, or null as the
 * next action starts, takes its place. Set it once the list shows the row as
 * the attempt left it (read again, where the attempt reads the list again),
 * so it is held to that.
 */
export function useRowNotice<T>(rowState: (row: string) => unknown): [(row: string) => T | null, (row: string, value: T | null) => void] {
  const [row, setRow] = useState<string | null>(null);
  const [notice, setNotice] = useNoticeFor<T>(row === null ? "" : JSON.stringify([row, rowState(row) ?? null]));
  const set = useCallback((next: string, value: T | null) => {
    setRow(value === null || value === "" ? null : next);
    setNotice(value);
  }, [setNotice]);
  return [(candidate: string) => (row !== null && candidate === row ? notice : null), set];
}

/**
 * What an attempt with typed values said (a refusal, a mismatch, an address
 * it couldn't reach), held to those values: changing any of them puts it
 * away, as it no longer judges what is in the fields (2026-10-27: "받는
 * 주소를 확인하세요" stayed under the address just typed, and "only a secure
 * address" under 127.0.0.1). A refusal the fields don't decide (a server's
 * policy) is set with `stands` and stays while they change (2026-10-27: a
 * closed server's refusal went as a name was retyped).
 */
export function useAttemptNotice<T>(...inputs: unknown[]): [T | null, (value: T | null, options?: { stands?: boolean }) => void] {
  const [standing, setStanding] = useState<T | null>(null);
  const [notice, setNotice] = useNoticeFor<T>(JSON.stringify(inputs));
  const set = useCallback((value: T | null, options?: { stands?: boolean }) => {
    setStanding(options?.stands ? value : null);
    setNotice(options?.stands ? null : value);
  }, [setNotice]);
  return [standing ?? notice, set];
}
