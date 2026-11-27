// The server this client works with. A client a server serves works with
// that server; a packaged client, whose page comes from the app itself,
// works with its active profile's server and sets it here.

let chosen: string | null = null;
// What keeps the chosen server's stored data apart: its origin, or the
// packaged client's own name for a profile whose address may change (the
// local server's port).
let chosenIdentity: string | null = null;
// Where the server serving this page now answers, once its address changed
// under the open page. What the page stores stays under the address it opened
// at until it opens at the new one (reloadAtRest).
let movedTo: string | null = null;

function pageOrigin(): string {
  return typeof window === "undefined" ? "http://localhost" : window.location.origin;
}

/** The active server's origin. */
export function serverOrigin(): string {
  return chosen ?? movedTo ?? pageOrigin();
}

/** Sends this page's own requests to where its server now answers. */
export function noteServerMovedTo(origin: string): void {
  movedTo = origin;
}

/** Whether the app window was pointed at its server, rather than being served by it. */
export function serverChosenForPage(): boolean {
  return chosen !== null;
}

/** The active server's identity for what the client stores of it. */
export function serverIdentity(): string {
  return chosen ? chosenIdentity ?? chosen : pageOrigin();
}

/**
 * Points the client at another server, or back at the one serving the page;
 * a packaged client may name the profile its stored data keeps under.
 */
export function setServerOrigin(origin: string | null, identity?: string): void {
  chosen = origin ? new URL(origin).origin : null;
  if (chosen === pageOrigin()) chosen = null;
  chosenIdentity = chosen && identity ? identity : null;
}

/**
 * An address on the active server, for the client's own requests: relative
 * while that server serves the page, absolute on another server.
 */
export function serverPath(path: string): string {
  const other = chosen ?? movedTo;
  return other ? new URL(path, other).toString() : path;
}

// The address the active server is reached at from anywhere — the one its
// administrator wrote, or its tunnel's — when its settings say one.
let published: { server: string; address: string } | null = null;

/** Records the active server's public address, from its runtime settings. */
export function setPublishedAddress(address: string): void {
  let origin = "";
  try {
    origin = address ? new URL(address).origin : "";
  } catch {
    origin = "";
  }
  published = origin ? { server: serverOrigin(), address: origin } : null;
}

function publishedOrigin(): string {
  return published && published.server === serverOrigin() ? published.address : "";
}

// The active server's earlier addresses that forward to it now (2026-10-20).
let earlier: { server: string; addresses: string[] } | null = null;

/** Records the active server's earlier addresses, from its runtime settings. */
export function setEarlierAddresses(addresses: readonly string[]): void {
  const origins = addresses.flatMap((address) => {
    try {
      return [new URL(address).origin];
    } catch {
      return [];
    }
  });
  earlier = origins.length ? { server: serverOrigin(), addresses: origins } : null;
}

/**
 * Whether an origin is the active server's: its own, its public address, or
 * an earlier one that forwards to it — a link written at the old address
 * opens here.
 */
export function isActiveServerOrigin(origin: string): boolean {
  return origin === serverOrigin() || (origin !== "" && origin === publishedOrigin())
    || (origin !== "" && earlier?.server === serverOrigin() && earlier.addresses.includes(origin));
}

/**
 * An absolute address on the active server, as a link others open needs:
 * at its public address when it has one (tunnel decision of 2026-10-13, 5).
 */
export function serverUrl(path: string): string {
  return new URL(path, linkOrigin()).toString();
}

/** The origin links others open are made at (serverUrl). */
/** Whether the server has a public address of its own: mail it sends can carry links only then. */
export function hasPublishedAddress(): boolean {
  return publishedOrigin() !== "";
}

export function linkOrigin(): string {
  return publishedOrigin() || serverOrigin();
}

/**
 * A storage name for the active server's data: as it is while the serving
 * server is active, the page's own origin keeping servers apart, and marked
 * with the server for a packaged client, whose one origin holds every
 * profile's data.
 */
export function serverScopedName(name: string): string {
  return chosen ? serverScopedNameFor(name, serverIdentity()) : name;
}

/** The storage name another server profile keeps the same data under. */
export function serverScopedNameFor(name: string, identity: string): string {
  return `${name}@${encodeURIComponent(identity)}`;
}

/** The active server's WebSocket address for a path. */
export function serverWebSocketUrl(path: string): string {
  return serverOrigin().replace(/^http/u, "ws") + path;
}
