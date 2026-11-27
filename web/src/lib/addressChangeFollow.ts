import { listProfiles, LOCAL_PROFILE_ID, moveProfileAddress, profileAt, reachedAt, serverProfileId } from "./profileRegistry";
import { serverAskSignal } from "./serverAnswers";
import { readIdentity, rememberedServerIdentity } from "./serverKeyPin";

/**
 * A server whose address changed forwards its earlier one to the new one
 * for a while (server_address_forwarding.go). The app, finding its profile's
 * server answering from elsewhere through that, moves the profile there —
 * keeping its ID, and with it the sign-in and what it stored — once the
 * server there names the old address one of its own and, where the app
 * remembered the old address's identity, shows the same (2026-10-20, the
 * server's address may change). Anything else leaves the profile where it is.
 */
export async function followAddressChange(profileId: string, address: string): Promise<string> {
  if (profileId === LOCAL_PROFILE_ID) return address;
  let response: Response;
  try {
    response = await fetch(new URL("/api/leafen/v1/runtime-config", address), { cache: "no-store", signal: serverAskSignal() });
  } catch {
    // Off, or not answering: the sign-in screen says so (components/auth/SignInWaiting.tsx).
    return address;
  }
  const moved = serverProfileId(response.url);
  const from = serverProfileId(address);
  if (!response.ok || !response.redirected || !moved || !from || moved === from) return address;
  const earlier = await earlierAddressesOf(response);
  if (!earlier.includes(from) || !(await sameIdentity(from, moved))) return address;
  moveProfileAddress(profileId, moved);
  return moved;
}

/**
 * The known profile a server answering at an origin is (one rule with
 * followAddressChange): the one with that ID or reached there; else, where
 * the origin is an earlier address forwarding to the server's address now,
 * the one reached there; else one whose address the server names as an
 * earlier one of its own, showing the same identity — that profile then
 * moves to where the server answers now. "" for a server not known
 * (2026-11-15: an account added at a server's new address, before the
 * profile had followed, was a second account for the same server).
 */
export async function knownProfileAt(origin: string): Promise<string> {
  const at = serverProfileId(origin);
  if (!at) return "";
  const known = profileAt(at);
  if (known) return known.id;
  let now: string;
  let earlier: string[];
  try {
    const response = await fetch(new URL("/api/leafen/v1/runtime-config", at), { cache: "no-store" });
    if (!response.ok) return "";
    now = serverProfileId(response.url) || at;
    earlier = await earlierAddressesOf(response);
  } catch {
    return "";
  }
  const forwardedTo = now !== at && earlier.includes(at) ? profileAt(now) : null;
  if (forwardedTo) return forwardedTo.id;
  for (const profile of listProfiles()) {
    const from = reachedAt(profile);
    if (profile.id === LOCAL_PROFILE_ID || !earlier.includes(from) || !(await sameIdentity(from, now))) continue;
    moveProfileAddress(profile.id, now);
    return profile.id;
  }
  return "";
}

/** The earlier addresses a server's runtime settings say forward to it. */
async function earlierAddressesOf(response: Response): Promise<string[]> {
  let config: unknown = null;
  try {
    config = await response.json();
  } catch {
    return [];
  }
  const earlier = config && typeof config === "object" && Array.isArray((config as { earlierAddresses?: unknown }).earlierAddresses)
    ? (config as { earlierAddresses: unknown[] }).earlierAddresses : [];
  return earlier.flatMap((value) => typeof value === "string" && serverProfileId(value) ? [serverProfileId(value)] : []);
}

/** Whether the server at `moved` shows the identity the app remembered for `from`, where it remembered one. */
async function sameIdentity(from: string, moved: string): Promise<boolean> {
  const remembered = rememberedServerIdentity(from);
  return !remembered || (await readIdentity(moved)) === remembered;
}
