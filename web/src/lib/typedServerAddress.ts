import { plainAddressRefused } from "@/lib/plainAddress";
import { serverProfileId } from "@/lib/profileRegistry";

// The one way a field reads a Leafen server's address someone typed, pasted
// or scanned (2026-10-26: "계정 추가" refused "127.0.0.1:18961", which the
// first install takes, and the copy from another instance read
// "leafen.example.com" as a path on this server).

/**
 * The server's possible origins and the page an invite or setup link names: an
 * address without a scheme is tried over https, then http (a server on the
 * same network often has no certificate).
 */
export function typedServerAddress(typed: string): { origins: string[]; path: string } {
  const text = typed.trim();
  const schemed = /^[a-z][a-z0-9+.-]*:\/\//iu.test(text);
  const tries = schemed ? [text] : [`https://${text}`, `http://${text}`];
  const origins = tries.map(serverProfileId).filter(Boolean);
  if (origins.length === 0) return { origins: [], path: "" };
  const url = new URL(tries[0]);
  // A setup link's one-time code goes after "#code=" and comes along
  // (2026-11-07: the app's "연결" opened the setup with its code field empty).
  const path = url.pathname !== "/" || url.search || url.hash ? `${url.pathname}${url.search}${url.hash}` : "";
  return { origins, path };
}

/** Whether a Leafen server answers at the origin. */
async function answersAsLeafen(origin: string): Promise<boolean> {
  try {
    const response = await fetch(new URL("/api/leafen/v1/health", origin), { cache: "no-store" });
    if (!response.ok) return false;
    const health = await response.json() as { status?: unknown; collaboration?: unknown };
    return health.status === "ok" && health.collaboration === "ygo";
  } catch {
    return false;
  }
}

/**
 * The first of the origins a Leafen server answers at, or why none does:
 * "plain" where the only ones left are plain addresses this app can't reach.
 */
export async function answeringServer(origins: readonly string[]): Promise<{ origin: string } | { failure: "plain" | "none" }> {
  let refused = false;
  for (const candidate of origins) {
    // A plain address this app can't reach isn't asked: it says so instead of "no server answers".
    if (plainAddressRefused(candidate)) {
      refused = true;
      continue;
    }
    if (await answersAsLeafen(candidate)) return { origin: candidate };
  }
  return { failure: refused ? "plain" : "none" };
}

/**
 * The origin a typed address leads to: the first that answers as Leafen,
 * else the first it could be (whose own check then says why it fails), else
 * the address as typed.
 */
export async function typedLeafenOrigin(typed: string): Promise<string> {
  const { origins } = typedServerAddress(typed);
  if (origins.length === 0) return typed.trim();
  const found = await answeringServer(origins);
  return "origin" in found ? found.origin : origins[0] ?? typed.trim();
}
