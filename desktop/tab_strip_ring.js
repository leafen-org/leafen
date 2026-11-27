// What takes the keys on the strip as a menu of it closes, ringed, as Notion
// 7.44.1's tab bar gives them back to what the menu opened from (measured
// in its Windows app 2026-11-24): a tab, a group's head or the back or
// forward button, which the app names (window_tabs_strip_keys.go). The ring
// is the page's keyboard ring (its --keyboard-focus-ring, as the strip is
// told it); it stays on the thing as the strip is drawn again, till a press
// on the strip or the keys leave it. A thing given the keys quiet (after a
// menu's pick by a press) takes them unringed and is ringed at the next key
// pressed on the strip but a shortcut, as a browser's focus-visible does:
// Notion's tab after a pick by a press shows no ring, and a letter typed
// then goes nowhere and rings it (its Windows app 2026-11-24). A menu of the
// strip closing with the strip grown over the window, the ring waits for
// the strip to have shrunk back and drawn a frame at its own height: a
// WebView2 lost a ring drawn before that, and kept not showing it as the
// strip was drawn again around it (the Windows VM 2026-11-24: drawn before
// the strip's resize event, 6 of 8 missing; in the frame of that event, 3 of
// 6; in the frame after it, 6 of 6 shown).
(function () {
  "use strict";
  // parts: the strip's tabs and nav rows (tab_strip.js).
  window.leafenStripRing = function (parts) {
    let ringed = null;
    // Taking the keys unringed till a key is pressed (a target's "quiet").
    let quiet = false;
    // The strip's height as its last resize told it, and, while the strip
    // shrinks back as a menu closes, what ends the wait for it.
    let told = window.innerHeight;
    let shrinking = null;

    function find(target) {
      if (target.tab) {
        const slot = [...parts.tabs.querySelectorAll(".slot")].find((node) => node.dataset.id === target.tab);
        return slot && !slot.classList.contains("hidden") ? slot.querySelector(".tab") : null;
      }
      if (target.group) {
        const head = [...parts.tabs.querySelectorAll(".ghead")].find((node) => node.dataset.group === target.group);
        return head && head.querySelector(".gtitle");
      }
      if (target.button) return parts.nav.querySelector(`[data-nav="${target.button}"]`);
      return null;
    }

    function apply() {
      const node = ringed && !shrinking ? find(ringed) : null;
      for (const each of document.querySelectorAll(".ring")) if (each !== node || quiet) each.classList.remove("ring");
      if (!node) return;
      if (!quiet) node.classList.add("ring");
      if (document.activeElement !== node) node.focus({ preventScroll: true });
    }

    function set(target) {
      ringed = target && (target.tab || target.group || target.button) ? target : null;
      quiet = Boolean(ringed && target.quiet);
      apply();
    }

    // A key pressed on the strip rings what took the keys quiet, if it still
    // has them; not a shortcut (with ⌘, Ctrl or Alt), nor a key alone that
    // only changes others, as Shift.
    function keyed(event) {
      if (!ringed || !quiet || event.metaKey || event.ctrlKey || event.altKey) return;
      if (["Shift", "Control", "Alt", "Meta", "CapsLock", "Fn"].includes(event.key)) return;
      const node = find(ringed);
      if (!node || document.activeElement !== node) return;
      quiet = false;
      apply();
    }

    function clear() {
      if (!ringed) return;
      ringed = null;
      apply();
      if (document.activeElement instanceof HTMLElement && document.activeElement !== document.body) document.activeElement.blur();
    }

    // A menu of the strip closed: grown over the window (more than the
    // strip's own height, band), the ring waits for the strip's next resize
    // smaller and the frame after it — or, should that not come, half a second.
    function shrinks(band) {
      if (shrinking || told <= Math.ceil(band) + 1) return;
      const grown = told;
      const done = () => {
        if (!shrinking) return;
        window.clearTimeout(shrinking.timer);
        window.removeEventListener("resize", resized);
        shrinking = null;
        apply();
      };
      const resized = () => {
        if (window.innerHeight >= grown) return;
        window.removeEventListener("resize", resized);
        window.requestAnimationFrame(() => window.requestAnimationFrame(done));
      };
      shrinking = { timer: window.setTimeout(done, 500) };
      window.addEventListener("resize", resized);
    }

    // A back or forward button, which takes the keys back as its history menu closes.
    function button(node, way) {
      node.dataset.nav = way;
      node.tabIndex = -1;
      return node;
    }

    // The menu key (or ⇧F10) on a part with the keys opens its menu.
    function menuKey(node, open) {
      node.addEventListener("keydown", (event) => {
        if (event.key !== "ContextMenu" && !(event.shiftKey && event.key === "F10")) return;
        event.preventDefault();
        open(event);
      });
    }

    document.addEventListener("mousedown", clear, true);
    document.addEventListener("keydown", keyed, true);
    window.addEventListener("blur", clear);
    window.addEventListener("resize", () => { told = window.innerHeight; });
    return { set, clear, shrinks, button, menuKey, redrawn: () => { if (ringed) apply(); } };
  };
})();
