import type { AuthAddressHandoffExchange } from "@/authWire.generated";
import { saveSession } from "./authSessionStore";
import { serverOrigin } from "./serverOrigin";

// A browser's sign-in carried over from the server's earlier address
// (internal/app/address_handoff.go): the earlier address's page opens this
// one with a pass in the fragment, which leaves the address bar at once and
// is traded here for this device's sign-in. What the earlier address held
// that this one can't — notifications, passkeys — is offered again once,
// while that sign-in lasts: signed out and in again, the tab's sign-in is
// no longer the one carried over (2026-11-12: the band still said so after
// a sign-out and a code sign-in).

const FRAGMENT = "leafen-handoff";
const FOLLOW_UPS_KEY = "leafen:address-handoff";

export type AddressHandoffFollowUps = { notifications: boolean; passkey: boolean };

/** Trades a pass the address brings for the sign-in, before the app reads it. */
export async function takeAddressHandoff(): Promise<void> {
  const fragment = new URLSearchParams(window.location.hash.slice(1));
  const pass = fragment.get(FRAGMENT);
  if (!pass) return;
  const then = fragment.get("then") ?? "";
  window.history.replaceState(window.history.state, "", `${window.location.pathname}${window.location.search}${then ? `#${then}` : ""}`);
  try {
    const response = await fetch(new URL("/api/leafen/v1/auth/address-handoff/exchange", serverOrigin()), {
      method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ pass }), cache: "no-store",
    });
    if (!response.ok) return;
    const answer = await response.json() as AuthAddressHandoffExchange;
    await saveSession(answer.session);
    window.sessionStorage.setItem(FOLLOW_UPS_KEY, JSON.stringify({
      notifications: answer.notifications, passkey: answer.passkey, userId: answer.session.user.id, sessionId: answer.session.sessionId ?? "",
    }));
  } catch {
    // The device signs in here as any other does.
  }
}

type KeptFollowUps = Partial<AddressHandoffFollowUps> & { userId?: string; sessionId?: string };

/**
 * What the carried-over sign-in offers again, once; null when it didn't come
 * over now, or when the tab's sign-in (its account and server session) is
 * another than the one carried over, which forgets it.
 */
export function addressHandoffFollowUps(userId: string, sessionId: string | undefined): AddressHandoffFollowUps | null {
  try {
    const kept = JSON.parse(window.sessionStorage.getItem(FOLLOW_UPS_KEY) ?? "null") as KeptFollowUps | null;
    if (!kept) return null;
    if (kept.userId !== userId || (kept.sessionId && kept.sessionId !== sessionId)) {
      clearAddressHandoffFollowUps();
      return null;
    }
    return { notifications: kept.notifications === true, passkey: kept.passkey === true };
  } catch {
    return null;
  }
}

export function clearAddressHandoffFollowUps(): void {
  try {
    window.sessionStorage.removeItem(FOLLOW_UPS_KEY);
  } catch {
    // Gone with the tab.
  }
}
