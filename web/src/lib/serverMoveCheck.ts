import { reloadWhenAtRest } from "./reloadAtRest";
import { serverPath } from "./serverOrigin";

const ASK_EVERY_MS = 30_000;
let lastAsked = 0;

/**
 * An app window's signed-in requests fail outright at an address its server
 * left: the forward can't answer the question those requests ask first. So a
 * request that went unanswered asks, now and then, in the one way that a
 * forward does answer; starting again then moves the app's profile there
 * (addressChangeFollow.ts).
 */
export function askWhetherServerMoved() {
  const now = Date.now();
  if (now - lastAsked < ASK_EVERY_MS) return;
  lastAsked = now;
  void fetch(serverPath("/api/leafen/v1/runtime-config"), { cache: "no-store" }).then((answer) => {
    if (answer.redirected) reloadWhenAtRest({ shown: true, why: "server-moved" });
  }, () => undefined);
}

/**
 * Where a request's answer came from, when that is the same place at another
 * address: how the server's old address forwards one.
 */
export function forwardedTo(asked: string, answeredAt: string): URL | null {
  const from = new URL(asked, window.location.href);
  const to = new URL(answeredAt);
  return to.origin !== from.origin && to.pathname === from.pathname && to.search === from.search ? to : null;
}
