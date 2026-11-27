// The first script of rhwp-studio's page in the Hangul Documents app (the
// build puts it there as leafen-bridge.js). When the app's frame and the
// studio's both have no origin — the app opened without an address of its
// own — the studio would refuse the app's handshake, which comes from the
// origin "null": it is handed on again with the page's address, and only the
// parent's (its ports go along, and everything after travels on them).
// IndexedDB, which such a frame can't open, is hidden so the studio keeps its
// recent files and history in memory.
//
// In any frame, each press that may change the document is told to the app
// at once ({ leafenStudio: "input", edit }; edit when it puts in or takes out
// text): the studio says what changed only when asked, and an app that asked
// now and then let a close right after typing go without asking about the
// unsaved typing (2026-11-12).
//
// The app's bar takes the focus out of the studio's frame (its Save), and
// Chrome leaves the studio's own focus on its body then, not on the input the
// studio types through, so the frame focused again took no keys (2026-11-18).
// The element that had the focus when it left is focused again when the app
// asks ({ leafenStudio: "focus" }), if nothing in the studio has it by then.
(() => {
  if (window.parent === window) return;
  let left = null;
  window.addEventListener("focusout", (event) => { if (event.target instanceof Element) left = event.target; }, true);
  window.addEventListener("message", (event) => {
    if (event.source !== window.parent || event.data?.leafenStudio !== "focus") return;
    event.stopImmediatePropagation();
    const now = document.activeElement;
    if ((!now || now === document.body) && left?.isConnected) left.focus({ preventScroll: true });
  }, true);
  const tell = (edit) => {
    try { window.parent.postMessage({ leafenStudio: "input", edit }, "*"); } catch { /* the app looks again before long */ }
  };
  const typing = (event, key = String(event.key ?? "")) => event.isComposing || key === "Process" ||
    !event.metaKey && !event.ctrlKey && !event.altKey && (key.length === 1 || ["Backspace", "Delete", "Enter", "Tab"].includes(key));
  window.addEventListener("keydown", (event) => tell(typing(event)), true);
  for (const name of ["paste", "cut", "drop", "compositionend"]) window.addEventListener(name, () => tell(true), true);
  window.addEventListener("pointerup", () => tell(false), true);
  if (self.origin !== "null") return;
  try { Object.defineProperty(window, "indexedDB", { value: undefined, configurable: true }); } catch { /* left as it is */ }
  const address = new URL(location.href).origin;
  window.addEventListener("message", (event) => {
    if (event.origin !== "null" || event.source !== window.parent || event.leafenAgain) return;
    event.stopImmediatePropagation();
    const again = new MessageEvent("message", { data: event.data, origin: address, source: event.source, ports: [...event.ports] });
    again.leafenAgain = true;
    window.dispatchEvent(again);
  }, true);
})();
