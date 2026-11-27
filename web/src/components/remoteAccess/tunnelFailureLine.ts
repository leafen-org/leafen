import type { TFunction } from "i18next";

/**
 * The line a screen says for why the server's outside way isn't open, by
 * the kind the server sends with it (internal/app/server_tunnel_failure.go):
 * what happened and what the person can do, in their language. The words it
 * came with — cloudflared's, Tailscale's or the server's own, in English —
 * stay folded under "자세히" (TunnelFailureDetails); a kind this screen
 * doesn't know yet says only that the address isn't open (2026-11-25:
 * Settings › Server › Remote access showed them as its line).
 */
const LINES: Record<string, string> = {
  program: "serverConsole:tunnel.failureKinds.program",
  door: "serverConsole:tunnel.failureKinds.door",
  stopped: "serverConsole:tunnel.failureKinds.stopped",
  token: "serverConsole:tunnel.failureKinds.token",
  "public-address": "serverConsole:tunnel.failureKinds.publicAddress",
  "certificate-elsewhere": "serverConsole:tunnel.failureKinds.certificateElsewhere",
  "own-address": "serverConsole:tunnel.failureKinds.ownAddress",
  "no-tailscale": "serverConsole:tunnel.failureKinds.noTailscale",
  "held-moved": "serverConsole:tunnel.failureKinds.heldMoved",
  "held-waiting": "serverConsole:tunnel.failureKinds.heldWaiting",
  "held-replica-unreachable": "serverConsole:tunnel.failureKinds.heldReplicaUnreachable",
};

export function tunnelFailureLine(t: TFunction, kind: string | undefined): string {
  return t(LINES[kind ?? ""] ?? "serverConsole:tunnel.failureKinds.unknown");
}

/** A hold (remote access waits here on purpose), not a failure. */
export function tunnelHeld(kind: string | undefined): boolean {
  return Boolean(kind?.startsWith("held-"));
}
