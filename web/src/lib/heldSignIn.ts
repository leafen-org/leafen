import { useCallback, useRef } from "react";
import type { Session } from "@/types";

/**
 * The sign-in a person's open window or running task goes on with. What it
 * opened lasts as long as the person: their sign-in handed over from another
 * tab, or its token renewed, keeps it, and its next request goes with the
 * sign-in held then; another person's sign-in starts the app over
 * (AuthenticatedSession is keyed by the person). The getter it returns stays
 * the same for the person, so it can key a read or a task's lifetime where
 * the session or its token used to.
 */
export function useHeldSignIn(session: Session): () => Session {
  const held = useRef(session);
  held.current = session;
  const person = session.user.id;
  // The person alone keys it; their sign-in is read through `held`.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  return useCallback(() => held.current, [person]);
}
