// When a lost connection's trouble began, if that was before it was found
// out: a live-editing connection that went silent was in trouble from its
// last message, some seconds before its check gave up on it (2026-11-06:
// the notice came about 16 seconds into a network that stopped, the ten
// seconds counted only from then).

const FRESH_MS = 2_000;
let quietSince = 0;
let notedAt = 0;

/** A connection found silent, heard last at `since`, is being started again. */
export function noteConnectionQuietSince(since: number) {
  quietSince = since;
  notedAt = Date.now();
}

/** How long the trouble just begun has lasted already, once. */
export function troubleLastedAlready(now = Date.now()) {
  const since = quietSince;
  const fresh = now - notedAt <= FRESH_MS;
  quietSince = 0;
  return fresh && since > 0 && since < now ? now - since : 0;
}
