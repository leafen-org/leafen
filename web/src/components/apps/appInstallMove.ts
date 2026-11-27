import type { AppSession } from "@/appPlatformWire.generated";
import { appInstallGone, type AppFrameAnswer } from "./appFrameBridge";

/**
 * A frame's calls that go on past the install the frame opened with, on the
 * app's current one, for a frame a new one would lose something of: a file's
 * window holding changes not saved yet (2026-11-20: after an administrator
 * removed the app and installed it again, the window's save came back with
 * its token refused and could never save; opening the window again would
 * have thrown the changes away). A call whose answer says the install is gone
 * opens the app's session where it is now (`move`) and is made once more with
 * its token; calls at the same time share the one move. An install left with
 * nothing to move to — the app removed for good, or the frame holding nothing
 * to lose — keeps its answer and is asked about again only by a save, the
 * person's own press (the app may have been installed again since).
 */
export function callsOnCurrentInstall(run: AppFrameAnswer, { installId, move, moved }: {
  /** The install the calls go to now. */
  installId: () => string;
  /** The app's session where it is now; null where the frame stays as it is. */
  move: (from: string) => Promise<AppSession | null>;
  /** The calls go on with this session's install and token. */
  moved: (app: AppSession) => void;
}): AppFrameAnswer {
  const moving = new Map<string, Promise<boolean>>();
  const left = new Set<string>();
  const moveFrom = (from: string) => {
    let going = moving.get(from);
    if (!going) {
      going = move(from).then((app) => {
        if (app && installId() === from) moved(app);
        if (installId() === from) left.add(from);
        return installId() !== from;
      }).finally(() => { moving.delete(from); });
      moving.set(from, going);
    }
    return going;
  };
  return async (method, params, ended) => {
    const from = installId();
    let answer: unknown;
    let failed = false;
    try {
      answer = await run(method, params, ended);
    } catch (failure) {
      answer = failure;
      failed = true;
    }
    if (appInstallGone(answer)) {
      // A server that didn't answer the move fails the call, to be made again.
      const goesOn = installId() !== from || ((!left.has(from) || method === "saveFile") && await moveFrom(from));
      if (goesOn) return run(method, params, ended);
    }
    if (failed) throw answer;
    return answer;
  };
}
