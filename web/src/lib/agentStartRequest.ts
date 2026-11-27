import { useEffect, useRef } from "react";
import { request } from "@/apiTransport";
import type { Session } from "@/types";
import { takeSignInMadeElsewhere } from "./authSessionStore";
import { ownClaim } from "./firstAdministratorStart";
import { wakeTabsBehind } from "./desktopTabs";
import { followAcrossTabs, hearTabs, tellTabs } from "./followAcrossTabs";

const LOOK_EVERY = 2000;

// `own`: the asking tab's own claim set the server up (internal/app/setup_claims.go).
type AgentStart = { configured?: boolean; own?: boolean; signIn?: string };

// The server set up in one of the person's tabs, and as whom.
const SET_UP = "server-set-up";

/**
 * This tab set the server up, signed in as `userId`: the person's other tabs
 * on the setup screen go on as that account, to the step it shows
 * (useAgentStartSignIn), as a step passed in one tab passes in the others.
 */
export function tellSetUp(userId: string): void {
  wakeTabsBehind();
  tellTabs(SET_UP, { userId });
}

/**
 * The setup screen follows a setup done elsewhere while it shows. An agent
 * sets the server up from its conversation (`leafen setup start`,
 * internal/app/first_run_person_ask.go): the person says yes to the name
 * there, not here (user decision 2026-11-13), and this screen goes on as the
 * first administrator it made, with the one sign-in the server leaves for
 * the screen that holds the setup code (`join`). A screen without the code
 * learns only that the server is set up (`configured`). One setup screen in
 * sight asks for all of this browser's tabs; the tab that asked is the one
 * that signs in (followAcrossTabs). Another of the person's tabs that set
 * the server up says so (tellSetUp), and this one, hidden or not, goes on as
 * the account that tab signed in as (`took`) — in the app, whose tabs each
 * keep their own sign-in (2026-11-14: a tab behind stayed on this screen
 * after the one in front went on; a browser's tabs share it already).
 * A tab's own claim sets the server up too, and following it cut the claim's
 * session before it was kept (2026-11-19: the first administrator landed
 * signed out on the sign-in screen, with no way back in). So the server tells
 * the asking tab's own claim from a setup done elsewhere (`own`, asked with
 * the claim's id), and while the screen sets the server up itself
 * (`settingUp`: from its claim until its session is kept, or the claim
 * refused) what it learns of one elsewhere waits, and goes on only once that
 * claim is refused.
 */
export function useAgentStartSignIn(watching: boolean, settingUp: boolean, setupCode: string,
  join: (code: string) => void, configured: () => void, took: (session: Session) => void): void {
  const handlers = useRef({ join, configured, took });
  handlers.current = { join, configured, took };
  const held = useRef(settingUp);
  held.current = settingUp;
  // The first step learned while this screen set the server up, for when that is refused.
  const waiting = useRef<(() => void) | null>(null);
  useEffect(() => {
    if (!watching) return undefined;
    const topic = `setup-agent-start:${setupCode}`;
    let done = false;
    let stop: (() => void) | null = null;
    const goOn = (step: () => void) => {
      if (done) return;
      if (held.current) {
        waiting.current ??= () => goOn(step);
        return;
      }
      done = true;
      stop?.();
      step();
    };
    const following = followAcrossTabs(topic, LOOK_EVERY,
      () => request<AgentStart>("/setup/agent-start", { method: "POST", body: JSON.stringify({ setupCode, claim: ownClaim() }) })
        .then((answer) => tellTabs(topic, answer ?? {})),
      (value, here) => {
        const answer = (value ?? {}) as AgentStart;
        // This tab's own claim set it up: nothing done elsewhere to follow (another tab's own claim is elsewhere here).
        if (!answer.configured || (answer.own && here) || (answer.signIn && !here)) return;
        const code = answer.signIn;
        goOn(code ? () => handlers.current.join(code) : () => handlers.current.configured());
      },
      { now: true });
    stop = following;
    if (done) following();
    const stopHearing = hearTabs(SET_UP, (value, here) => {
      const userId = (value as { userId?: unknown } | null)?.userId;
      if (here || typeof userId !== "string") return;
      void takeSignInMadeElsewhere(userId).then((session) => {
        if (session) goOn(() => handlers.current.took(session));
      }, () => undefined);
    });
    return () => { done = true; waiting.current = null; following(); stopHearing(); };
  }, [watching, setupCode]);
  useEffect(() => {
    if (settingUp) return;
    const next = waiting.current;
    waiting.current = null;
    next?.();
  }, [settingUp]);
}
