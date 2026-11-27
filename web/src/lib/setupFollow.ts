import { useEffect, useRef } from "react";
import { requestSettingsWindow } from "@/components/settings/settingsWindowRequest";
import type { Session } from "@/types";
import { firstRunSetupState, followFirstRun, hearFirstRun, type FirstRunState, type SetupOpen } from "./firstRunSteps";

// How often the setup person's window looks while an agent goes on by commands.
const FOLLOW_EVERY = 3000;

const handledKey = (userId: string) => `leafen:v2:setup-opened:${userId}`;

function handledAt(userId: string): number {
  try {
    return Number(window.localStorage.getItem(handledKey(userId)) ?? 0) || 0;
  } catch {
    return 0;
  }
}

function markHandled(userId: string, at: number) {
  try {
    window.localStorage.setItem(handledKey(userId), String(at));
  } catch {
    // Without the browser's storage a screen opened once may open again on reload.
  }
}

/**
 * The setup person's window follows an agent going through the first run by
 * commands (user decision 2026-11-09): while one was taken a short while ago
 * it looks every few seconds — one tab in sight for all of the person's tabs,
 * which hear its answers (followFirstRun) — and as the person comes back to
 * the window, and opens the screen the agent's last command asked for, once
 * — `leafen setup notion`, or an agent waiting for the Notion token, the
 * Notion import, where the person pastes their token into Leafen's own field.
 * It does so while the first run's steps show too, over them (2026-11-19,
 * the s251 round: the window stayed on the agents step's "connected —
 * continue" until it was pressed, while the agent said the import opened);
 * the server passes the agents step an agent is connected for then, and
 * the steps follow that in FirstRunSteps.
 */
export function useSetupFollow(session: Session | null) {
  const userId = session?.user.id ?? "";
  // The session as it is now: its token may be renewed while the window follows.
  const current = useRef(session);
  current.current = session;
  useEffect(() => {
    if (!userId) return undefined;
    let live = true;
    let stopAsking: (() => void) | null = null;
    let handled = handledAt(userId);
    const open = (requested: SetupOpen | undefined) => {
      // Another of the person's tabs may have opened it already.
      handled = Math.max(handled, handledAt(userId));
      if (!requested || requested.at <= handled) return;
      handled = requested.at;
      markHandled(userId, requested.at);
      // `leafen setup notion` is an agent asking for the token.
      if (requested.what === "notion") requestSettingsWindow({ section: "import", importSource: "notion", importArrival: "agent" });
    };
    const heard = (state: FirstRunState) => {
      if (!live) return;
      // Only the setup person's window follows: another's stops looking.
      if (!state.owner) {
        stop();
        return;
      }
      open(state.open);
      if (state.follow && !stopAsking && current.current) stopAsking = followFirstRun(current.current, FOLLOW_EVERY, undefined, () => current.current);
      else if (!state.follow && stopAsking) {
        stopAsking();
        stopAsking = null;
      }
    };
    const stopHearing = hearFirstRun(userId, heard);
    const look = () => {
      const session = current.current;
      // The answer may be one a moment old that another part read (firstRunSteps.ts), told to none.
      if (session) void firstRunSetupState(session).then(heard, () => null);
    };
    const comeBack = () => {
      if (document.visibilityState === "visible") look();
    };
    const stop = () => {
      live = false;
      stopAsking?.();
      stopHearing();
      window.removeEventListener("focus", comeBack);
      document.removeEventListener("visibilitychange", comeBack);
    };
    look();
    window.addEventListener("focus", comeBack);
    document.addEventListener("visibilitychange", comeBack);
    return stop;
  }, [userId]);
}
