import { isPackagedClient, startPageOverAt } from "./packagedClient";
import { listProfiles, moveProfileToPair, notePairAddress } from "./profileRegistry";
import { serverIdentity } from "./serverOrigin";

// A standby pair's two addresses (follow-up of 2026-10-17, for a pair at
// addresses of its own): each server's runtime settings name the other, and
// a client whose server stops answering looks there. Once that one says it
// serves as the primary — it took over — the app moves its profile there,
// keeping the sign-in and what it stored; a page a server serves shows the
// way, as the other address signs in on its own.

const LOOK_TIMEOUT_MS = 5_000;

/** Whether the server at an origin serves as the primary now: neither a standby, moved nor paused. */
export async function servesAsPrimary(origin: string): Promise<boolean> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), LOOK_TIMEOUT_MS);
  try {
    const response = await fetch(new URL("/api/leafen/v1/runtime-config", origin), { cache: "no-store", signal: controller.signal });
    if (!response.ok) return false;
    const said = await response.json() as Record<string, unknown>;
    return !said.standbyOf && said.serverMoved !== true && said.serverPaused !== true;
  } catch {
    return false;
  } finally {
    clearTimeout(timer);
  }
}

/**
 * The active server's pair, keeping what its settings say now: the app keeps
 * it with the profile, a page with the settings it read last.
 */
export function activePair(settingsPair: string, fresh: boolean): string {
  if (!isPackagedClient()) return settingsPair;
  if (fresh) notePairAddress(serverIdentity(), settingsPair);
  return listProfiles().find((profile) => profile.id === serverIdentity())?.pairAddress ?? "";
}

/** Moves the app's active profile to its pair's other server and starts the page again there. */
export function followPair(): boolean {
  if (!isPackagedClient() || !moveProfileToPair(serverIdentity())) return false;
  startPageOverAt(`${window.location.pathname}${window.location.search}${window.location.hash}`);
  return true;
}

/** The address on a pair's other server of the page open here. */
export function pairPageUrl(pair: string): string {
  return new URL(`${window.location.pathname}${window.location.search}${window.location.hash}`, pair).toString();
}

/**
 * Goes on at the server that has this one's data, signed in as this tab is.
 * The data brought this sign-in along, so that server makes it a one-time
 * device code, the way a proper address is gone on to: every sign-in the
 * person has is at the address that went, and the sign-in screen's way of a
 * code from a signed-in device would lead nowhere. One it doesn't know shows
 * the sign-in, as for any other device.
 */
export async function openAtPair(pair: string, token: string): Promise<void> {
  let target = pairPageUrl(pair);
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), LOOK_TIMEOUT_MS);
  try {
    const response = await fetch(new URL("/api/leafen/v1/account/device-codes", pair), {
      method: "POST", headers: { authorization: `Bearer ${token}` }, cache: "no-store", signal: controller.signal,
    });
    if (response.ok) {
      const { code } = await response.json() as { code: string };
      const here = `${window.location.pathname}${window.location.search}${window.location.hash}`;
      target = new URL(`/device/${encodeURIComponent(code)}?next=${encodeURIComponent(here)}`, pair).toString();
    }
  } catch {
    // The sign-in screen there takes it from here.
  } finally {
    clearTimeout(timer);
  }
  window.location.assign(target);
}
