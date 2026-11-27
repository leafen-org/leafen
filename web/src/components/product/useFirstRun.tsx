import { useCallback, useEffect, useState, type ReactNode } from "react";
import { FirstRunSteps } from "@/components/firstSetup/FirstRunSteps";
import { PhoneOfferLater } from "@/components/firstSetup/PhoneOfferLater";
import { FIRST_RUN_REOPEN_EVENT, firstRunStepsLeft, firstRunStepsLeftNow } from "@/lib/firstRunSteps";
import { useWindowOverProduct } from "@/lib/productWindows";
import { useSetupFollow } from "@/lib/setupFollow";
import type { Session } from "@/types";

// The first run's steps (FirstRunSteps, one flow in the app and in a browser
// since the one-program decision of 2026-10-27, 5) over the product, and
// after them the phone a first setup on a local address held back. The
// workspace then opens on its welcome page, as Notion's: moving from Notion
// is Settings' import, and an outside agent connects in Settings' agents
// (Leafen runs no agents of its own since 2026-10-29). While an agent goes
// through the first run by commands, the window follows it, the steps
// showing or not (useSetupFollow).
export function useFirstRun({
  session,
  workspaceId,
  onSession,
  onCloseSettings,
}: {
  session: Session | null;
  workspaceId: string | null;
  /** Takes the session a backup sign-in set from the later phone offer changed. */
  onSession: (session: Session) => void;
  /** Closes Settings, for the first start's steps it brings back, full-window screens under it. */
  onCloseSettings: () => void;
}): ReactNode {
  const [stepsOver, setStepsOver] = useState(false);
  // Settings brings back what was left for later, and closes for them.
  useEffect(() => {
    const reopen = () => {
      setStepsOver(false);
      onCloseSettings();
    };
    window.addEventListener(FIRST_RUN_REOPEN_EVENT, reopen);
    return () => window.removeEventListener(FIRST_RUN_REOPEN_EVENT, reopen);
  }, [onCloseSettings]);
  const finishSteps = useCallback(() => setStepsOver(true), []);
  // Steps known to be left open with the workspace's first screen, not after
  // its list is read: its cover showed a blank moment between the language
  // and the address step (2026-11-06). They are read ahead while the
  // account's own steps are up, else alongside the list.
  const userId = session?.user.id ?? "";
  const [stepsLeftEarly, setStepsLeftEarly] = useState(() => Boolean(userId && firstRunStepsLeftNow(userId)?.length));
  useEffect(() => {
    if (!session || workspaceId || stepsLeftEarly) return undefined;
    let current = true;
    void firstRunStepsLeft(session).then((left) => { if (current && left.length > 0) setStepsLeftEarly(true); }, () => undefined);
    return () => { current = false; };
  }, [session, stepsLeftEarly, workspaceId]);

  const stepsShowing = !stepsOver && Boolean(session) && Boolean(workspaceId || stepsLeftEarly);
  useSetupFollow(session);
  if (stepsShowing && session) {
    return <HeldBack draw={(held) => <FirstRunSteps session={session} workspaceId={workspaceId} blocked={held} onFinished={finishSteps} />} />;
  }
  return session ? <HeldBack draw={(held) => <PhoneOfferLater session={session} blocked={held} onSession={onSession} />} /> : null;
}

/** Held back by a window over the product, search too: followed here, so that a window's press draws nothing more (lib/productWindows.ts). */
function HeldBack({ draw }: { draw: (held: boolean) => ReactNode }) {
  return draw(useWindowOverProduct());
}
