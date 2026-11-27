// Whether this page is going away: from its "pagehide" (a reload, a closed
// window, another address) until it is shown again from the back-forward
// cache. A change saved then has no time to be kept on the device first, so
// it goes straight out in a request that outlives the page (apiTransport).
// The capturing listener is added as the module loads, before any field's
// own "pagehide" save, so those saves already see the page going.

let leaving = false;

if (typeof window !== "undefined") {
  window.addEventListener("pagehide", () => { leaving = true; }, { capture: true });
  window.addEventListener("pageshow", () => { leaving = false; });
}

export function pageLeaving() {
  return leaving;
}
