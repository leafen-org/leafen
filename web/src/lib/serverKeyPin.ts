import { deviceValue, storeDeviceValue } from "./browserStorage";
import { serverAskSignal } from "./serverAnswers";

// The packaged app remembers a server at an address whose DNS someone else
// answers — its Tailscale address (ts.net) — and refuses another one later
// (decided 2026-10-17; 2026-10-19 moved from the TLS key to the server's
// signed identity, since Tailscale makes a new certificate key at each
// renewal). The app's shell reads, over one
// connection, the certificate key the server shows and the server's own
// identity key that signed it; the window remembers the identity the first
// time the server says the address is its Tailscale address. A server
// elsewhere (its own domain, a Cloudflare tunnel) isn't remembered.

const IDENTITIES_KEY = "leafen:v2:server-identities";

/**
 * A server that isn't the one the app remembered: another identity, or
 * (identity "") a certificate the server's identity didn't sign.
 */
export class ServerKeyChangedError extends Error {
  constructor(readonly origin: string, readonly pin: string) {
    super("The server's key changed.");
  }
}

function identities(): Record<string, string> {
  try {
    const parsed: unknown = JSON.parse(deviceValue(IDENTITIES_KEY) ?? "{}");
    return parsed && typeof parsed === "object" ? parsed as Record<string, string> : {};
  } catch {
    return {};
  }
}

/** The identity the app remembers for a server, "" for none (a phone's background refresh checks it too). */
export function rememberedServerIdentity(origin: string): string {
  return identities()[origin] ?? "";
}

function keepIdentity(origin: string, identity: string): void {
  storeDeviceValue(IDENTITIES_KEY, JSON.stringify({ ...identities(), [origin]: identity }));
}

/** The server's identity, "" when it can't be reached, null when it didn't sign the certificate shown. */
export async function readIdentity(origin: string): Promise<string | null> {
  const { Call } = await import("@wailsio/runtime");
  try {
    const identity: unknown = await Call.ByName("main.ServerKeys.Identity", origin);
    return typeof identity === "string" ? identity : "";
  } catch (failure) {
    return String(failure instanceof Error ? failure.message : failure).includes("unvouched") ? null : "";
  }
}

/** Whether the server at an origin says it is at its Tailscale address. */
async function atRememberedAddress(origin: string): Promise<boolean> {
  try {
    // Not waited on past a few seconds: the sign-in screen says a server that doesn't answer.
    const response = await fetch(new URL("/api/leafen/v1/runtime-config", origin), { cache: "no-store", signal: serverAskSignal() });
    const config: unknown = await response.json();
    const hostname = new URL(origin).hostname;
    const said = config && typeof config === "object" ? config as { tailscaleAddress?: unknown } : {};
    return typeof said.tailscaleAddress === "string" && said.tailscaleAddress !== "" && said.tailscaleAddress === hostname;
  } catch {
    return false;
  }
}

/**
 * Checks another server before the app works with it: a remembered identity
 * must be the one that signs now (else ServerKeyChangedError), and a server
 * at its Tailscale address has its identity remembered the first
 * time. A server the app can't reach now is let through; the window's own
 * connection fails anyway.
 */
export async function checkServerKey(origin: string): Promise<void> {
  if (!origin.startsWith("https://")) return;
  const remembered = identities()[origin];
  if (!remembered && !(await atRememberedAddress(origin))) return;
  const shown = await readIdentity(origin);
  if (shown === null) {
    if (remembered) throw new ServerKeyChangedError(origin, "");
    return;
  }
  if (!shown) return;
  if (!remembered) keepIdentity(origin, shown);
  else if (remembered !== shown) throw new ServerKeyChangedError(origin, shown);
}

/** Takes the identity a server shows now as the one to remember. */
export function acceptServerKey(origin: string, identity: string): void {
  if (identity) keepIdentity(origin, identity);
}
