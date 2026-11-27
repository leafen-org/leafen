import { request } from "@/apiTransport";
import { heldSignIn, sameSessionSignIn } from "@/lib/authSessionStore";
import type { Session } from "@/types";

// The pages a reader has open, each with what tells the server it was left
// (PagePresence.tsx), for a sign-out to tell first (leaveOpenPages).
const open = new Set<{ session: Session; body: string }>();

function send(body: string, session: Session): Promise<unknown> {
  return request("/page-left", { method: "POST", body, keepalive: true }, session).catch(() => undefined);
}

/**
 * Keeps a page open for its reader. `left` tells the server they left it,
 * which those who open it later see as when they were last here, with the
 * sign-in held when it goes (heldSignIn): renewed, it goes with the new
 * token; signed out, it goes not at all — the sign-out told it already, while
 * its token still answered (2026-11-14: sent after, it was refused twice).
 */
export function openPage(session: Session, target: object) {
  const page = { session, body: JSON.stringify(target) };
  open.add(page);
  return {
    left() {
      const held = heldSignIn(page.session);
      if (held) void send(page.body, held);
    },
    close() { open.delete(page); },
  };
}

/** Tells the server the pages this sign-in has open are left, before the sign-out revokes it. */
export function leaveOpenPages(session: Session): Promise<unknown> {
  return Promise.all([...open].filter((page) => sameSessionSignIn(page.session, session)).map((page) => send(page.body, session)));
}
