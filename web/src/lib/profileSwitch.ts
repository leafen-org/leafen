import type { Session } from "@/types";
import { forgetSessionOf, otherSessionsIn, sessionFromStored, storedSessionOf } from "./authSessionStore";
import { changeKeptSignIn } from "./keptSignIns";
import { isPackagedClient, startPageOverAt } from "./packagedClient";
import {
  addServerProfile,
  forgetProfile,
  listProfiles,
  LOCAL_PROFILE_ID,
  profileAddress,
  rememberLastPath,
  requestWorkspaceCreate,
  requestWorkspaceOpen,
  setActiveProfileId,
} from "./profileRegistry";
import { endSignIn } from "./serverAccounts";
import { serverIdentity } from "./serverOrigin";
import { handOffUrl } from "./switcherLists";

// Moving between server profiles, one active at a time.

/**
 * Opens another profile where it was last. A packaged client makes it the
 * active profile and starts its page over, which ends everything of the
 * profile before it (requests, files, realtime, documents and the outbox);
 * a page a server serves goes to the other server, which signs in there.
 */
export function openProfile(profileId: string, workspaceId = "", at = "", create = false): void {
  if (typeof window === "undefined") return;
  rememberLastPath(serverIdentity(), window.location.pathname);
  const target = listProfiles().find((profile) => profile.id === profileId);
  const path = at || (workspaceId ? "/" : target?.lastPath || "/");
  if (isPackagedClient()) {
    setActiveProfileId(profileId);
    requestWorkspaceOpen(workspaceId);
    if (create) requestWorkspaceCreate();
    startPageOverAt(path);
    return;
  }
  // The other server's page, where it is reached now, takes the workspace and the list keys this page knows.
  window.location.assign(handOffUrl(profileAddress(profileId), path, workspaceId, create));
}

/**
 * Signs out of a profile that isn't active and drops it from the menu. A
 * packaged client also ends the sign-in it kept there; a page a server
 * serves keeps none of another server's.
 */
export async function signOutOfProfile(profileId: string): Promise<void> {
  if (isPackagedClient() && profileId !== LOCAL_PROFILE_ID) {
    let kept: Session | null = null;
    await changeKeptSignIn(profileId, (now) => {
      kept = sessionFromStored(now);
      return null;
    }).catch(() => undefined);
    const stored = kept ?? storedSessionOf(profileId);
    const address = profileAddress(profileId);
    await Promise.all([stored, ...otherSessionsIn(stored)].map((signIn) => signIn ? endSignIn(address, signIn) : undefined));
    forgetSessionOf(profileId);
  }
  forgetProfile(profileId);
}

/**
 * Goes to the server this one's data moved to (a whole-server move or a
 * standby that took over): the app opens it as one of its profiles, added
 * when it isn't one yet; a page a server serves goes there.
 */
export function openMovedServer(origin: string): void {
  if (typeof window === "undefined") return;
  if (!isPackagedClient()) {
    window.location.assign(origin);
    return;
  }
  const profile = addServerProfile(origin);
  if (profile) openProfile(profile.id);
}

/** After the active server's accounts are all signed out: it leaves the list, and the app goes back to this device. */
export function leaveServerForLocal(): void {
  forgetProfile(serverIdentity());
  returnToLocalProfile();
}

/** The way back to the app's own profile from another server's sign-in. */
export function returnToLocalProfile(): void {
  setActiveProfileId(LOCAL_PROFILE_ID);
  window.location.assign("/");
}

// Asked from a server that can't be reached: the first screen opens at its
// sign-in, for another server's address (components/auth/SignInWaiting.tsx).
const FIRST_SCREEN_SIGN_IN_KEY = "leafen:first-screen-sign-in";

/** Back to the app's first screen, opened at signing in to another server. */
export function returnToFirstScreenSignIn(): void {
  try {
    window.sessionStorage.setItem(FIRST_SCREEN_SIGN_IN_KEY, "1");
  } catch {
    // Kept nowhere, the first screen opens at its first question.
  }
  returnToLocalProfile();
}

let firstScreenSignIn: boolean | null = null;

/** Whether the first screen opens at signing in (returnToFirstScreenSignIn): asked once a page load, the same answer after. */
export function takeFirstScreenSignIn(): boolean {
  if (firstScreenSignIn !== null) return firstScreenSignIn;
  try {
    firstScreenSignIn = window.sessionStorage.getItem(FIRST_SCREEN_SIGN_IN_KEY) === "1";
    window.sessionStorage.removeItem(FIRST_SCREEN_SIGN_IN_KEY);
  } catch {
    firstScreenSignIn = false;
  }
  return firstScreenSignIn;
}
