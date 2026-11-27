// A sign-out the person asked for on the app's own server, kept on the
// device until a sign-in there ends it: the window then doesn't sign the
// person who set the server up back in by itself as it lands on the sign-in
// page (useDeviceOwnerSignIn), and the owner check is the way back, as
// decided 2026-10-20. Any other landing there — a sign-in lost or ended by
// the server, whatever the cause — signs them back in (user decision
// 2026-11-19 "넣어"). Storage that can't be read keeps no mark, so a window
// that lost its storage signs back in too.
import { PACKAGED_ORIGINS } from "./packagedOrigins";
import { activeProfileId, LOCAL_PROFILE_ID } from "./profileRegistry";

const OWN_SERVER_SIGNED_OUT_KEY = "leafen:app-own-server:signed-out";

/** Whether this page is the app's window on its own server. */
function onOwnServer(): boolean {
  return typeof window !== "undefined" && PACKAGED_ORIGINS.includes(window.location.origin) && activeProfileId() === LOCAL_PROFILE_ID;
}

/** The person signed out on purpose: on the app's own server, it is kept. */
export function markOwnServerSignedOut() {
  if (!onOwnServer()) return;
  try {
    window.localStorage.setItem(OWN_SERVER_SIGNED_OUT_KEY, String(Date.now()));
  } catch {
    // Without storage the window signs its owner back in.
  }
}

/** Whether the last sign-out on the app's own server was the person's. */
export function ownServerSignedOut(): boolean {
  try {
    return window.localStorage.getItem(OWN_SERVER_SIGNED_OUT_KEY) !== null;
  } catch {
    return false;
  }
}

/** A sign-in on the app's own server ends the mark; one on another server leaves it. */
export function forgetOwnServerSignOut() {
  if (!onOwnServer()) return;
  try {
    window.localStorage.removeItem(OWN_SERVER_SIGNED_OUT_KEY);
  } catch {
    // Nothing kept to forget.
  }
}
