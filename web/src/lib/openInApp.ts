import { isPackagedClient } from "./packagedClient";

// Opening a server's page in the Leafen app from a phone's browser (the
// multi-server decision's 2026-10-20 "같은 날 다듬음"): the app takes
// leafen://open?url=<address> (lib/packagedLinks.ts), the address kept
// whole; one that asks for it starts the app's sign-in through the browser
// on that server when the app isn't signed in there.

const BROWSER_SIGN_IN_KEY = "leafen:browser-sign-in";
const BROWSER_SIGN_IN_ASKED = "leafen:browser-sign-in-asked";

/** Whether the page shows in a phone's browser, not the app or a home-screen Leafen. */
export function phoneBrowser(): boolean {
  if (isPackagedClient()) return false;
  const standalone = window.matchMedia?.("(display-mode: standalone)").matches
    || (navigator as Navigator & { standalone?: boolean }).standalone === true;
  return !standalone && /Android|iPhone|iPad|iPod/u.test(navigator.userAgent);
}

/** Whether that browser is an iPhone's or iPad's Safari, where a home-screen Leafen gets notifications. */
export function iosSafari(): boolean {
  const agent = navigator.userAgent;
  return phoneBrowser() && /iPhone|iPad|iPod/u.test(agent) && /Safari/u.test(agent) && !/CriOS|FxiOS|EdgiOS/u.test(agent);
}

/** The app's link to an address, asking for the browser sign-in where the app isn't signed in. */
export function openInAppLink(address = window.location.href, browserSignIn = false): string {
  return `leafen://open?url=${encodeURIComponent(address)}${browserSignIn ? "&signin=browser" : ""}`;
}

/** Whether a link into the app asks for the browser sign-in. */
export function linkAsksBrowserSignIn(link: string): boolean {
  return /^leafen:\/\/open\?/iu.test(link.trim()) && /[?&]signin=browser(?:&|$)/u.test(link);
}

/** Keeps that the app's sign-in on a server starts through the browser by itself, once. */
export function askBrowserSignIn(server: string): void {
  try {
    window.sessionStorage.setItem(BROWSER_SIGN_IN_KEY, server);
  } catch {
    // The sign-in screen offers it to press.
  }
  window.dispatchEvent(new Event(BROWSER_SIGN_IN_ASKED));
}

/** Calls `listener` when that's asked while a sign-in screen already shows; returns the way to stop. */
export function onBrowserSignInAsked(listener: () => void): () => void {
  window.addEventListener(BROWSER_SIGN_IN_ASKED, listener);
  return () => window.removeEventListener(BROWSER_SIGN_IN_ASKED, listener);
}

/** Takes that ask, for the server the sign-in screen is on. */
export function takeBrowserSignIn(server: string): boolean {
  try {
    const asked = window.sessionStorage.getItem(BROWSER_SIGN_IN_KEY);
    if (asked !== server) return false;
    window.sessionStorage.removeItem(BROWSER_SIGN_IN_KEY);
    return true;
  } catch {
    return false;
  }
}
