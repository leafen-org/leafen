// A group folding and unfolding in the strip (tab_strip.js), moving as
// Notion 7.44.1's virtual tab bar moves it (its web client's Tabs chunk,
// read 2026-11-24): in 180ms a tab going out of sight narrows to nothing
// (cubic-bezier(0.2, 0.25, 0, 1)) and one coming back widens to its room
// (cubic-bezier(0.2, 0.05, 0.2, 1)), its face keeping its width meanwhile;
// the face going fades and shrinks to 0.92 in the first 40% of that, the
// face coming fades and grows back in the last 70%; the head's name room,
// its line, the chevron's turn and room, the group's ground and the tabs'
// row move along in the same 180ms. A press with ⇧ makes it 8 times slower
// and one with ⇧ and ⌘ or Ctrl 16 times, as Notion's. The strip draws its
// tabs anew each time (tab_strip.js): what moved is read before and moved
// from there after.
(function () {
  "use strict";
  const SPAN = 180;
  const OPENING = "cubic-bezier(0.2, 0.05, 0.2, 1)";
  const CLOSING = "cubic-bezier(0.2, 0.25, 0, 1)";
  const FADE = "cubic-bezier(0.2, 0, 0, 1)";
  // A face fading in: the last 70% of the span; out: the first 40%.
  const IN = { start: 0.3, end: 1 }, OUT = { start: 0, end: 0.4 };
  let scale = 1;
  let running = [];
  let until = 0;

  // A fold's press sets how slow the next motion is (Notion's motion scale).
  function pressed(event) {
    scale = event && event.shiftKey ? (event.metaKey || event.ctrlKey ? 16 : 8) : 1;
  }

  const px = (value) => `${value}px`;
  function styleOf(node, names) {
    const style = window.getComputedStyle(node);
    const kept = {};
    for (const name of names) kept[name] = style.getPropertyValue(name);
    return kept;
  }
  const SLOT = ["flex-grow", "flex-basis", "min-width", "max-width", "padding-inline-start", "padding-inline-end"];
  const TITLE = ["padding-inline-end", "margin-inline-end"];
  const LINE = ["width", "margin-inline-start", "margin-inline-end", "opacity"];
  const END = ["padding-inline-start"];

  // What may move, as the strip stands before it is drawn again.
  function before(tabs) {
    const slots = new Map(), heads = new Map(), ends = new Map(), trays = new Map();
    for (const slot of tabs.querySelectorAll(".slot")) {
      const face = slot.querySelector(".tab");
      slots.set(slot.dataset.id, { hidden: slot.classList.contains("hidden"), style: styleOf(slot, SLOT),
        face: face ? face.offsetWidth : 0 });
      const ground = slot.querySelector(".tray");
      if (ground) trays.set(`slot:${slot.dataset.id}`, ground.style.background);
    }
    for (const head of tabs.querySelectorAll(".ghead")) {
      const title = head.querySelector(".gtitle"), line = head.querySelector(".gline"), ground = head.querySelector(".tray");
      heads.set(head.dataset.group, { title: title && styleOf(title, TITLE), line: line && styleOf(line, LINE) });
      if (ground) trays.set(`head:${head.dataset.group}`, ground.style.background);
    }
    for (const end of tabs.querySelectorAll(".gend")) {
      const turn = end.querySelector(".turn"), ground = end.querySelector(".tray");
      ends.set(end.dataset.group, { style: styleOf(end, END), turn: turn ? window.getComputedStyle(turn).transform : "none" });
      if (ground) trays.set(`end:${end.dataset.group}`, ground.style.background);
    }
    const row = tabs.parentElement;
    return { slots, heads, ends, trays, row: row ? row.style.maxWidth : "" };
  }

  function move(node, frames, options) {
    if (!node || typeof node.animate !== "function") return;
    running.push(node.animate(frames, { duration: SPAN * scale, fill: "backwards", ...options }));
    until = Date.now() + SPAN * scale;
  }
  // Whether a fold is still moving: the strip draws what it is told after.
  const moving = () => Date.now() < until;
  const left = () => Math.max(0, until - Date.now());
  function fade(node, from, to, part) {
    move(node, [from, to], { duration: (part.end - part.start) * SPAN * scale, delay: part.start * SPAN * scale, easing: FADE, fill: "both" });
  }
  // A face's width held as it was or will be while its room changes.
  function hold(face, width) {
    if (!face || !width) return;
    face.style.width = px(width);
    face.style.flexShrink = "0";
    const ends = () => { face.style.width = ""; face.style.flexShrink = ""; };
    window.setTimeout(ends, SPAN * scale);
  }

  // Moves what folded or unfolded from where it stood: only tabs that were
  // drawn before and are out of sight now and not then, or the other way.
  function after(tabs, was) {
    for (const animation of running) animation.cancel();
    running = [];
    until = 0;
    if (!was) return;
    let opening = null;
    for (const slot of tabs.querySelectorAll(".slot")) {
      const old = was.slots.get(slot.dataset.id);
      const hidden = slot.classList.contains("hidden");
      if (!old || old.hidden === hidden) continue;
      opening = !hidden;
      const now = styleOf(slot, SLOT);
      const face = slot.querySelector(".tab");
      const rtl = document.documentElement.dir === "rtl";
      if (face) face.style.transformOrigin = rtl ? "right center" : "left center";
      if (hidden) {
        hold(face, old.face);
        move(slot, [{ ...camel(old.style), visibility: "visible" }, { ...camel(now), visibility: "visible" }], { easing: CLOSING });
        fade(face, { opacity: 1, transform: "none" }, { opacity: 0, transform: "scale(0.92)" }, OUT);
      } else {
        hold(face, face ? face.offsetWidth : 0);
        move(slot, [camel(old.style), camel(now)], { easing: OPENING });
        fade(face, { opacity: 0, transform: "scale(0.92)" }, { opacity: 1, transform: "none" }, IN);
      }
    }
    if (opening === null) return;
    const easing = opening ? OPENING : CLOSING;
    for (const head of tabs.querySelectorAll(".ghead")) {
      const old = was.heads.get(head.dataset.group);
      if (!old) continue;
      const title = head.querySelector(".gtitle"), line = head.querySelector(".gline");
      if (title && old.title) move(title, [camel(old.title), camel(styleOf(title, TITLE))], { easing });
      if (line && old.line) {
        const now = styleOf(line, LINE);
        move(line, [camel({ ...old.line, opacity: undefined }), camel({ ...now, opacity: undefined })], { easing });
        fade(line, { opacity: old.line.opacity }, { opacity: now.opacity }, Number(now.opacity) > Number(old.line.opacity) ? IN : OUT);
      }
      ground(head.querySelector(".tray"), was.trays.get(`head:${head.dataset.group}`), easing);
    }
    for (const end of tabs.querySelectorAll(".gend")) {
      const old = was.ends.get(end.dataset.group);
      if (!old) continue;
      move(end, [camel(old.style), camel(styleOf(end, END))], { easing });
      const turn = end.querySelector(".turn");
      if (turn) move(turn, [{ transform: old.turn }, { transform: window.getComputedStyle(turn).transform }], { easing });
      ground(end.querySelector(".tray"), was.trays.get(`end:${end.dataset.group}`), easing);
    }
    for (const slot of tabs.querySelectorAll(".slot")) {
      ground(slot.querySelector(".tray"), was.trays.get(`slot:${slot.dataset.id}`), easing);
    }
    const row = tabs.parentElement;
    if (row && was.row && row.style.maxWidth && was.row !== row.style.maxWidth) {
      move(row, [{ maxWidth: was.row }, { maxWidth: row.style.maxWidth }], { easing });
    }
  }

  // A group's ground turning from its chip's color to its tray's, or back.
  function ground(node, from, easing) {
    if (node && from && from !== node.style.background) move(node, [{ background: from }, { background: node.style.background }], { easing });
  }

  // CSS names as the keyframes take them; a value left out is not moved.
  function camel(style) {
    const out = {};
    for (const [name, value] of Object.entries(style)) {
      if (value === undefined || value === "") continue;
      out[name.replace(/-([a-z])/g, (_, letter) => letter.toUpperCase())] = value;
    }
    return out;
  }

  window.leafenTabMotion = { pressed, before, after, moving, left };
})();
