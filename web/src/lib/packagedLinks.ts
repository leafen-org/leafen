import { leafenPagePath } from "./pageLinks";

// Links into the packaged app (docs/app-platform.md 17): a leafen: link
// another app opened, or a Leafen address the desktop app was started with.
// The shell keeps them until the page takes them — Wails' bindings on a
// desktop and on iOS, the app's own messages on Android — and says when one
// comes.

/** What a link opens: an address on a server, "" being the active one. */
export type PackagedLinkTarget = { server: string; path: string };

const OPEN_LINK_EVENT = "leafen:open-link";
const MAX_LINK = 4096;

function webTarget(address: URL, activeOrigin: string): PackagedLinkTarget | null {
  if ((address.protocol !== "https:" && address.protocol !== "http:") || !address.host) return null;
  const path = `${address.pathname || "/"}${address.search}${address.hash}`;
  return { server: address.origin === activeOrigin ? "" : address.origin, path };
}

/**
 * Where a link leads: leafen://open?url=<address>, leafen://<host>/<path>
 * as Notion's own links are (the scheme of a server this app knows, https
 * otherwise), a page's own leafen://page/<id> on the active server at its
 * path, a block's anchor kept (leafenPagePath), or a server's address itself.
 */
export function packagedLinkTarget(link: string, activeOrigin: string, knownServers: readonly string[]): PackagedLinkTarget | null {
  const raw = link.trim();
  if (!raw || raw.length > MAX_LINK) return null;
  const leafen = /^leafen:\/\//iu.test(raw);
  let address: URL;
  try {
    // A leafen: link is read as a web address would be: browsers differ on
    // the host of an address in a scheme of their own (older WebViews keep
    // it in the path).
    address = new URL(leafen ? `http://${raw.slice("leafen://".length)}` : raw);
  } catch {
    return null;
  }
  if (!leafen) return webTarget(address, activeOrigin);
  const host = address.host;
  if (host === "page") {
    const path = leafenPagePath(raw);
    return path ? { server: "", path } : null;
  }
  if (host === "open") {
    try {
      return webTarget(new URL(address.searchParams.get("url") ?? ""), activeOrigin);
    } catch {
      return null;
    }
  }
  if (!host.includes(".") && !host.includes(":") && host !== "localhost") return null;
  const known = [activeOrigin, ...knownServers].find((origin) => {
    try {
      return new URL(origin).host === host;
    } catch {
      return false;
    }
  });
  try {
    return webTarget(new URL(`${address.pathname}${address.search}${address.hash}`, known ?? `https://${host}`), activeOrigin);
  } catch {
    return null;
  }
}

type AndroidLinks = {
  postMessage(message: string): void;
  onmessage: ((event: { data: string }) => void) | null;
};

// A link the app found itself, such as a scanned QR code's.
const FOUND_LINK_EVENT = "leafen:found-link";

/** Takes a link the app found itself as one it was opened with, so it opens the same way. */
export function openFoundLink(link: string): void {
  window.dispatchEvent(new CustomEvent(FOUND_LINK_EVENT, { detail: [link] }));
}

/**
 * Hands the page the links the app was opened with, and the ones it found
 * itself, as they come, the latest last; nothing in a page a server serves.
 */
export function listenForPackagedLinks(take: (links: string[]) => void): () => void {
  const accept = (value: unknown) => {
    const links = Array.isArray(value) ? value.filter((link): link is string => typeof link === "string" && link.length <= MAX_LINK) : [];
    if (links.length) take(links);
  };
  const found = (event: Event) => accept((event as CustomEvent<unknown>).detail);
  window.addEventListener(FOUND_LINK_EVENT, found);
  const android = (window as unknown as { leafenLinks?: AndroidLinks }).leafenLinks;
  if (android && typeof android.postMessage === "function") {
    android.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data) as { links?: unknown; waiting?: boolean };
        if (message.waiting) android.postMessage(JSON.stringify({ op: "take" }));
        accept(message.links);
      } catch {
        // Not a message about links.
      }
    };
    android.postMessage(JSON.stringify({ op: "take" }));
    return () => {
      window.removeEventListener(FOUND_LINK_EVENT, found);
      android.onmessage = null;
    };
  }
  let stopped = false;
  let stop: (() => void) | undefined;
  void import("@wailsio/runtime").then(({ Call, Events }) => {
    if (stopped) return;
    const drain = () => { void Call.ByName("main.Links.Take").then(accept, () => undefined); };
    stop = Events.On(OPEN_LINK_EVENT, drain);
    drain();
  }, () => undefined);
  return () => {
    window.removeEventListener(FOUND_LINK_EVENT, found);
    stopped = true;
    stop?.();
  };
}
