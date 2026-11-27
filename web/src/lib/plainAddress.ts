// Where this page can't reach a server over plain http, so it says that
// rather than that no server answers (2026-10-26: the Android app told an
// address its window had refused, http://10.0.2.2, that no Leafen server
// answered there):
// - a page served over https refuses plain addresses but this device's own
//   (the standard's mixed content); the Android app's window, whose page is
//   https://wails.localhost, also lets only 127.0.0.1 through in plain;
// - the iPhone and iPad app refuses plain addresses at domain names and keeps
//   a local network's — an IP address, a one-word name, a .local one — as its
//   App Transport Security does (seen in the iOS Simulator's app).

const ANDROID_APP_ORIGIN = "https://wails.localhost";
const APPLE_APP_ORIGIN = "wails://localhost";
const THIS_DEVICE = /^(?:localhost|.+\.localhost|127(?:\.\d{1,3}){3}|\[::1\])$/u;

function appleMobile() {
  const agent = navigator.userAgent;
  return /iPhone|iPad|iPod/u.test(agent) || (/Macintosh/u.test(agent) && navigator.maxTouchPoints > 1);
}

/** Whether this page refuses to reach the origin over plain http. */
export function plainAddressRefused(origin: string): boolean {
  if (typeof window === "undefined") return false;
  let url: URL;
  try {
    url = new URL(origin);
  } catch {
    return false;
  }
  if (url.protocol !== "http:") return false;
  const host = url.hostname.toLowerCase();
  if (window.location.origin === ANDROID_APP_ORIGIN) return host !== "127.0.0.1";
  if (window.location.protocol === "https:") return !THIS_DEVICE.test(host);
  if (window.location.origin === APPLE_APP_ORIGIN && appleMobile()) {
    const address = /^\d{1,3}(?:\.\d{1,3}){3}$/u.test(host) || host.startsWith("[");
    return !address && host.includes(".") && !host.endsWith(".local");
  }
  return false;
}
