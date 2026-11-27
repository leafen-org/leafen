import { createAccountApi } from "../accountApi";
import type { Session } from "../types";
import { appServerEntry, forgetAppServerEntry, rememberAppServerEntry } from "./appServerEntry";
import { keepSessionInApp, keepSessionsInMemory, loadSession, sameSessionSignIn, sessionFromStored } from "./authSessionStore";
import { saveDownloadsOnDevice } from "./browserFileDownload";
import { followThemeOnDevice } from "./deviceAppearance";
import { desktopBackgroundAvailable, followLanguageOnDevice } from "./desktopBackground";
import { saveFileToDevice, saveUrlToDevice } from "./deviceFiles";
import { gateWebKitKeySteps, packagedCredentials } from "./packagedCredentials";
import { isPackagedClient } from "./packagedOrigins";
import { activeProfileId, LOCAL_PROFILE_ID, profileAddress } from "./profileRegistry";
import { followAddressChange } from "./addressChangeFollow";
import { sendNewWindowsToApp } from "./newWindowRequests";
import { installDesktopClipboard } from "./desktopClipboard";
import { installDesktopTextMenu } from "./desktopTextMenu";
import { startDesktopTabReports } from "./desktopTabs";
import { checkServerKey } from "./serverKeyPin";
import { setServerOrigin } from "./serverOrigin";
import { takeKeptTabSignIn } from "./tabSignIn";


const PROFILE_PATH = "/leafen-local/profile";

// What the shell answers: the server once it answers — with the code that
// opens its first setup and the name to suggest while it isn't set up, and
// the capability that signs an earlier version's owner in while that owner
// waits — why it couldn't start, or neither while it is starting.
type LocalProfile = {
  server?: string;
  setupCode?: string;
  name?: string;
  capability?: string;
  error?: string;
  // Why it couldn't start ("locked", "damaged", "disk_full" or ""), and
  // when its last backup was made (milliseconds), for the recovery screen.
  reason?: string;
  backupAt?: string;
  state?: string;
};

/**
 * How long the app's own server may take to start before the startup
 * recovery screen shows: about ten times a start on data in use (1.4 s on a
 * 2026 Mac Studio; 3.7 s for a first start making its database) and well
 * within the server's own limit of 60 s. The server goes on starting behind
 * the screen, which goes once it answers.
 */
const STARTUP_SLOW_MS = 15_000;

/** The app's own server couldn't start: why, as the app names it, and when its last backup was made. */
export class LocalServerFailedError extends Error {
  readonly reason: string;
  readonly backupAt: number | null;

  constructor(message: string, reason: string, backupAt: number | null) {
    super(message);
    this.name = "LocalServerFailedError";
    this.reason = reason;
    this.backupAt = backupAt;
  }
}

/**
 * The app's own server is off and the window has no other server to open:
 * a first start not yet chosen, or a terminal whose server was taken away.
 * The first screens show (components/firstInstall).
 */
export class FirstRunPending extends Error {
  constructor() {
    super("first run");
    this.name = "FirstRunPending";
  }
}

// Whether this page is the packaged client's own: decided beside its origins.
export { isPackagedClient };

/**
 * Starts the app's page over at a path, as a change of the active server
 * needs. Going where only the fragment differs from here would just move
 * within the page (an invitation link opened from the first screen), so the
 * page then loads again.
 */
export function startPageOverAt(path: string): void {
  const next = new URL(path, window.location.href);
  const fragmentOnly = next.hash !== "" && next.pathname === window.location.pathname && next.search === window.location.search;
  window.location.assign(next.toString());
  if (fragmentOnly) window.location.reload();
}

/**
 * On a phone, the system may end an app it has sent to the background, so
 * what is being typed there is closed first: the focused field gives up its
 * focus, which saves it as leaving it does.
 */
function closeEditsWhenSuspended(): void {
  if (!/Android|iPhone|iPad/u.test(navigator.userAgent)) return;
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState !== "hidden") return;
    const focused = document.activeElement;
    if (focused instanceof HTMLElement && focused !== document.body) focused.blur();
  });
}

/**
 * A server's sign-in — another server's, or the app's own — comes from the
 * app's secret storage and each change goes back there (lib/keptSignIns.ts);
 * one an earlier version kept in the page's storage moves there. Where the
 * app keeps none, it lasts while the window is open.
 */
async function openKeptSignIn(profile: string): Promise<void> {
  const credentials = await packagedCredentials();
  const value = credentials ? await credentials.load(profile).catch(() => "") : "";
  const kept = sessionFromStored(value);
  const earlier = kept ? null : loadSession();
  keepSessionsInMemory({ initial: kept ?? earlier, profile: credentials ? profile : undefined, kept: value });
  if (earlier && credentials) void keepSessionInApp().catch(() => undefined);
}

/**
 * An earlier version's owner, signed in on this window: once the app's
 * secret storage keeps the sign-in, the capability sign-in ends and the
 * account is like any other (the multi-server decision of 2026-10-20).
 * Where the app keeps no sign-ins, or this fails, the window signs in with
 * the capability again at the next start and tries again.
 */
export async function settleEarlierOwner(session: Session): Promise<void> {
  const { capability } = appServerEntry();
  if (!capability) return;
  const credentials = await packagedCredentials();
  if (!credentials) return;
  try {
    await keepSessionInApp();
    if (!sameSessionSignIn(sessionFromStored(await credentials.load(LOCAL_PROFILE_ID)), session)) return;
    await createAccountApi().settleLocalOwner(session, capability);
    rememberAppServerEntry({ ...appServerEntry(), capability: undefined });
  } catch {
    // Tried again at the next start.
  }
}

/**
 * Makes the packaged client's page work with its active profile. Another
 * server's profile is worked with as a page that server serves would be,
 * signing in there. The local profile waits for the app's own server, which
 * its shell answers for at an address only this page can read, then points
 * the client at it, its sign-in kept in the app's secret storage as another
 * server's is; what the shell hands for that server's setup or its earlier
 * owner is kept for the sign-in page (appServerEntry). Rejects with
 * LocalServerFailedError when the server couldn't start, or with
 * ServerKeyChangedError for another server showing a key other than the one
 * remembered. While the server is still starting, starting is called once,
 * and slow once it has taken STARTUP_SLOW_MS; while it brings its data up to
 * date (a new version's update), updating is called once instead and the
 * slow screen's time counts from the update's end.
 */
export async function startPackagedClient(starting?: () => void, slow?: () => void, updating?: () => void): Promise<void> {
  if (!isPackagedClient()) return;
  closeEditsWhenSuspended();
  followThemeOnDevice();
  followLanguageOnDevice();
  // A computer's app opens the page's new windows and tabs by its own rule,
  // and a Mac window's strip shows this tab as the page tells it; a Mac
  // page draws its own right-press menu and keeps the HTML it copies.
  if (desktopBackgroundAvailable()) sendNewWindowsToApp();
  installDesktopTextMenu();
  installDesktopClipboard();
  startDesktopTabReports();
  // The window can't download; the app saves files onto the device.
  saveDownloadsOnDevice({
    saveFile: (file, name) => saveFileToDevice(file, name),
    saveUrl: (url, name) => saveUrlToDevice(url, name),
  });
  // The app's runtime comes with its secret storage, as in any Wails app.
  void packagedCredentials();
  // A computer's app reads WebKit's key for the page's stored keys first.
  if (desktopBackgroundAvailable()) gateWebKitKeySteps();
  // A tab of the app starts in the sign-in it showed (tabSignIn.ts).
  await takeKeptTabSignIn();
  const active = activeProfileId();
  if (active !== LOCAL_PROFILE_ID) {
    // What the window kept for its own server is for that server only.
    forgetAppServerEntry();
    // Its standby pair's other server once that took over, or the address its
    // server moved to, kept under the profile's ID.
    const address = await followAddressChange(active, profileAddress(active));
    // A server at its Tailscale address must show the identity the app remembers.
    await checkServerKey(address);
    setServerOrigin(address, active);
    await openKeptSignIn(active);
    return;
  }
  let began = Date.now();
  for (;;) {
    let profile: LocalProfile | null = null;
    try {
      const response = await fetch(PROFILE_PATH, { cache: "no-store" });
      if (response.ok) profile = await response.json() as LocalProfile;
    } catch {
      profile = null;
    }
    if (profile?.server) {
      setServerOrigin(profile.server, LOCAL_PROFILE_ID);
      rememberAppServerEntry({ capability: profile.capability, setupCode: profile.setupCode, name: profile.name });
      await openKeptSignIn(LOCAL_PROFILE_ID);
      // Kept at an earlier start whose settling didn't finish.
      const kept = loadSession();
      if (kept && profile.capability) void settleEarlierOwner(kept);
      return;
    }
    if (profile?.error) {
      throw new LocalServerFailedError(profile.error, profile.reason ?? "", profile.backupAt ? Number(profile.backupAt) : null);
    }
    if (profile?.state === "first-run" || profile?.state === "off") throw new FirstRunPending();
    if (profile?.state === "updating") {
      updating?.();
      updating = undefined;
      starting = undefined;
      began = Date.now();
      await new Promise((resolve) => setTimeout(resolve, 100));
      continue;
    }
    starting?.();
    starting = undefined;
    if (slow && Date.now() - began >= STARTUP_SLOW_MS) {
      slow();
      slow = undefined;
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
}
