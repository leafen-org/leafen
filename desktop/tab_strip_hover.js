// The card of the tab the pointer rests on, which the shown page draws
// under it (Notion's tab preview; window_tabs_page.go previewTab), and the
// strip's menus over it, as Notion 7.44.1's tab bar has them (measured in
// its Windows app 2026-11-24, twice each): a menu opening takes the card
// down, and the strip growing over the window for it, which the web view
// takes for the pointer coming on the tab under it again, brings back none;
// while it is open another tab the pointer moves on shows its card; once it
// closes, the card is the one of the tab the pointer rests on, or none —
// where the pointer is the app says (closeMenu), the strip, shrunk back
// from over the window, having lost it.
// The system tooltips (title) of what is under the resting pointer as a menu
// opens — a pinned tab's name, a group's — stay down while it rests there,
// the menu open or closed, and come back as it moves, over the menu too:
// as Notion's tab bar (measured as above, its pinned tab, twice each), where
// the strip growing over the window brought them back over the menu with the
// pointer still.
(function () {
  "use strict";
  // strip: the tabs' row, send, the strip's state, whether a tab is dragged
  // and whether a menu of the strip is open (tab_strip.js).
  window.leafenStripHover = function (strip) {
    let hovering = null;
    let pointer = { x: -1, y: -1 };
    // Where the pointer was as a menu opened, while it has not moved.
    let still = null;

    function off() {
      if (hovering === null) return;
      hovering = null;
      strip.send({ do: "unhover" });
    }
    function show(tab, slot) {
      const box = (slot.querySelector(".tab") || slot).getBoundingClientRect();
      hovering = tab.id;
      strip.send({ do: "hover", id: tab.id, x: Math.round(box.left), right: Math.round(box.right) });
    }
    // A tab not shown, nor dragged meanwhile, has a card: that tab, or none.
    function carded(id) {
      const state = strip.state();
      const tab = state && state.tabs.find((each) => String(each.id) === String(id));
      return tab && !tab.active && !tab.hidden && !strip.dragged() ? tab : null;
    }
    function point(event) {
      pointer = { x: event.clientX, y: event.clientY };
      if (still && (still.x !== pointer.x || still.y !== pointer.y)) still = null;
      if (quiet && (quiet.x !== pointer.x || quiet.y !== pointer.y)) unhush(null);
    }

    // The parts whose tooltips are down, each with its words, and where the
    // pointer rested as they went down.
    let hushed = [];
    let quiet = null;
    // Those at a point (or, after a redraw, those of these words) go down.
    function hush(at, only) {
      for (const node of document.elementsFromPoint(at.x, at.y)) {
        const words = node.getAttribute("title");
        if (words && (!only || only.includes(words))) {
          hushed.push({ node, words });
          node.removeAttribute("title");
        }
      }
    }
    // The pointer moved (null), or where the app says it is as a menu
    // closes, or off the strip (null): those it is not on get theirs back.
    function unhush(at) {
      hushed = hushed.filter(({ node, words }) => {
        const box = node.getBoundingClientRect();
        const on = Boolean(at) && node.isConnected && at.x >= box.left && at.x < box.right && at.y >= box.top && at.y < box.bottom;
        if (!on) node.setAttribute("title", words);
        return on;
      });
      if (!hushed.length) quiet = null;
    }

    // The pointer comes on a tab's room, or leaves it.
    function enter(tab, slot, event) {
      point(event);
      if (still && strip.menuOpen()) return;
      if (carded(tab.id)) show(tab, slot);
    }
    function leave(tab) {
      if (hovering === tab.id) off();
    }

    // A redraw (a drop, a tab closed or opened) moves the tabs under a
    // resting pointer without telling the old tab it left: its card goes
    // unless its tab is still under the pointer; the parts drawn anew there
    // for those whose tooltips are down keep theirs down.
    function stays() {
      const gone = hushed.filter((each) => !each.node.isConnected).map((each) => each.words);
      if (gone.length) {
        hushed = hushed.filter((each) => each.node.isConnected);
        hush(pointer, gone);
      }
      if (hovering === null) return;
      const slot = [...strip.tabs.querySelectorAll(".slot")].find((node) => node.dataset.id === String(hovering));
      const box = slot && slot.getBoundingClientRect();
      if (!box || pointer.x < box.left || pointer.x >= box.right) off();
    }

    function opened() {
      off();
      still = { ...pointer };
      quiet = { ...pointer };
      hush(pointer);
    }
    // A menu of the strip closed with the pointer at a point of the strip
    // (its CSS pixels from its top left), or off it (null): the card of the
    // tab there, a frame on — no flash of one when another menu opens at
    // once in its place.
    function closed(at) {
      still = null;
      window.requestAnimationFrame(() => {
        if (strip.menuOpen()) return;
        if (at) pointer = { x: at.x, y: at.y };
        if (quiet) unhush(at);
        const slot = at ? [...strip.tabs.querySelectorAll(".slot")].find((node) => {
          const box = node.getBoundingClientRect();
          return at.x >= box.left && at.x < box.right && at.y >= box.top && at.y < box.bottom;
        }) : null;
        const tab = slot ? carded(slot.dataset.id) : null;
        if (!tab) off();
        else if (hovering !== tab.id) show(tab, slot);
      });
    }

    return { off, enter, leave, moved: point, stays, opened, closed };
  };
})();
