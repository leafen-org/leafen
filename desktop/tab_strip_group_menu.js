// A group's menu, which the strip draws over the window as it draws a tab's
// (tab_strip_menu.js), as Notion 7.44.1's tab bar draws its "탭 그룹 작업"
// (its TabGroupMenu, read in its web client's Tabs chunk 2026-11-24, and
// measured in its Windows app the same day): under the group's head — its
// top at the head's foot, 34 down the 40 of the strip, its start at the
// head's — 220 wide: the group's name to type beside its icon, all of it
// chosen as the menu opens, 80 letters at most and kept as it is typed,
// Enter closing the menu; the group's ten colors; then "그룹 해제" and "그룹
// 닫기", off while the group holds every tab of the window — none of them lit
// as it opens, by a press or a key alike, the name having the keys, and the
// one the pointer lit staying lit as it leaves (Notion's, seen in its
// Windows app 2026-11-24). Tab goes round the icon, the name, each color
// and those two lines, which take the keys at the lit one or the first and
// leave none lit as they lose them; only while they hold them do ↑ ↓ move
// over them, Enter act on one and the keys' ring show on the lit one — ↓
// in the name or on a color does nothing (Notion's TabGroupMenu, its list
// captureKeyboardOnlyWhenFocused, measured in its Windows app 2026-11-24,
// twice), Enter on a color picks it (seen once). The keys on the icon or a
// color draw them as under the pointer — the icon on the secondary ground, a
// color on the hover ground — with the page's keyboard ring while the keys
// lead (ringLit, tab_strip_menu.js), a color with its name in a tip under
// it (tips below; Notion's, measured in its Windows app 2026-11-24, twice).
// The icon opens
// the shown page's icon picker under it, over the menu (Notion's picker
// stands centered under the icon; DesktopStripMenus.tsx): the menu leaves
// the picker's place to it (window_tabs_strip_room*), and while the picker is
// open a press outside it closes it alone, as an Escape or a pick does, the
// name then taking the keys again with all of it chosen (seen in Notion's
// Windows app 2026-11-24).
(function () {
  "use strict";
  const WIDTH = 220;
  const MARGIN = 12;
  // A cut out of a box: as far round as its shadow reaches, less the hole.
  const REACH = 48;
  const FOLDER = '<svg viewBox="0 0 16 16" fill="currentColor"><path d="M2 4.6c0-.83.67-1.5 1.5-1.5h2.78c.4 0 .78.16 1.06.44l1.02 1.02h4.14c.83 0 1.5.67 1.5 1.5v5.84c0 .83-.67 1.5-1.5 1.5H3.5c-.83 0-1.5-.67-1.5-1.5z"/></svg>';
  const stroke = 'fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"';
  // "그룹 해제": two stacked layers struck through (the app's own drawing of Notion's).
  const UNGROUP = `<svg viewBox="0 0 20 20" ${stroke}><path d="M7.4 5.2 9.1 3.9a1.5 1.5 0 0 1 1.8 0l5.4 4.1a1.2 1.2 0 0 1 0 1.9l-1 .8M12.6 13.2l-1.7 1.3a1.5 1.5 0 0 1-1.8 0L3.7 10.4a1.2 1.2 0 0 1 0-1.9l1.1-.8"/><path d="M3 4.4 17 15.6"/></svg>`;
  const CLOSE = `<svg viewBox="0 0 20 20" ${stroke}><path d="M5.75 5.75l8.5 8.5M14.25 5.75l-8.5 8.5"/></svg>`;
  const COLORS = ["default", "gray", "brown", "yellow", "orange", "green", "blue", "purple", "pink", "red"];

  // Notion's keyboard mode, which its tips ask of: on at a key but a
  // shortcut or a modifier alone, off at the pointer's next move.
  let keyboardMode = false;
  document.addEventListener("keydown", (event) => {
    if (event.metaKey || event.ctrlKey || event.altKey) return;
    if (["Shift", "Control", "Alt", "Meta", "CapsLock", "Fn", "AltGraph", "OS"].includes(event.key)) return;
    keyboardMode = true;
  }, true);
  document.addEventListener("mousemove", () => { keyboardMode = false; }, true);

  // kit: the tab menu's parts (tab_strip_menu.js) — element, rows, light,
  // step, word, look (a color's), pick, ringLit, send and close.
  window.leafenStripGroupMenu = function (kit) {
    const { element, word } = kit;

    // The group's icon in the menu: its emoji, its picture in its color, or the folder.
    function mark(group) {
      const color = kit.look(group.color).icon || "";
      const node = element("i", "smark gmark");
      if (group.icon && group.icon.startsWith("data:image/svg+xml,")) {
        const shape = element("span", "gpicture");
        shape.style.setProperty("--shape", `url("${group.icon}")`);
        shape.style.background = color || "currentColor";
        node.appendChild(shape);
      } else if (group.icon) {
        node.textContent = group.icon;
        node.classList.add("emoji");
      } else {
        node.innerHTML = FOLDER;
        node.style.color = color;
      }
      return node;
    }

    function paintCell(cell, chosen) {
      const look = kit.look(cell.dataset.groupColor);
      cell.setAttribute("aria-pressed", String(chosen));
      cell.style.boxShadow = chosen && look.swatchRing ? `inset 0 0 0 2px ${look.swatchRing}` : "";
      const circle = cell.firstChild;
      circle.style.background = look.swatch || "";
      circle.style.boxShadow = look.swatchEdge ? `inset 0 0 0 1px ${look.swatchEdge}` : "";
    }

    // Opens the menu of menu.group under menu.head (from the strip's top
    // left); gives back what the tab menu keeps of a menu shown.
    function open(menu) {
      const group = { ...menu.group };
      const box = element("div", "smenu sgroup", { role: "dialog", "aria-label": word("groups.menu", "Tab group actions") });
      box.style.width = `${WIDTH}px`;
      const shown = { kind: "group", group, box, lit: -1, lines: [], picking: false, hole: null, canClose: Boolean(menu.canClose),
        tips: { hover: null, keys: null, keyed: null, taken: false, wait: 0, shown: new Map() } };

      const head = element("div", "gnamerow");
      const iconButton = element("button", "giconbutton", { type: "button", "aria-label": word("groups.changeIcon", "Change icon") });
      iconButton.appendChild(mark(group));
      const input = element("input", "gname-input", { type: "text", maxlength: "80", spellcheck: "false", autocomplete: "off",
        placeholder: word("groups.namePlaceholder", "Name this group"), "aria-label": word("groups.name", "Group name") });
      input.value = group.name || "";
      head.append(iconButton, input);
      shown.iconButton = iconButton;
      shown.input = input;

      const grid = element("div", "gcolors", { role: "group" });
      for (const color of COLORS) {
        const name = word(`groups.colors.${color}`, color);
        const cell = element("button", "gcell", { type: "button", "aria-label": name, "data-group-color": color });
        cell.appendChild(element("span", "gcircle"));
        paintCell(cell, color === group.color);
        cell.addEventListener("mouseenter", () => restOn(shown, cell));
        cell.addEventListener("mouseleave", () => leave(shown, cell));
        cell.addEventListener("mousedown", () => leave(shown, cell));
        cell.addEventListener("click", () => {
          group.color = color;
          for (const each of grid.children) paintCell(each, each === cell);
          kit.send({ do: "group-say", id: "color", group: group.id, color });
        });
        grid.appendChild(cell);
      }
      shown.grid = grid;

      const list = element("div", "glines", { role: "menu", tabindex: "-1", "aria-label": word("groups.menu", "Tab group actions") });
      shown.list = list;
      const ungroupMark = element("i", "smark wide");
      ungroupMark.innerHTML = UNGROUP;
      const closeMark = element("i", "smark wide");
      closeMark.innerHTML = CLOSE;
      shown.lines = kit.rows(list, [[
        { label: word("groups.ungroup", "Ungroup"), mark: ungroupMark, run: () => kit.pick("ungroup", { group: group.id }) },
        { label: word("groups.closeGroup", "Close group"), mark: closeMark, danger: true, off: !shown.canClose,
          run: () => kit.pick("closeGroup", { group: group.id }) },
      ]], shown);

      box.append(head, grid, element("div", "sdivider", { role: "separator" }), list);
      // The keys going elsewhere in the menu (a press on a color, the
      // name given them back) take the lines' ring with them, and their
      // lit line as they leave the lines.
      box.addEventListener("focusin", kit.ringLit);
      list.addEventListener("blur", () => { if (shown.lit >= 0) kit.light(shown, -1); });

      input.addEventListener("input", () => {
        group.name = input.value;
        kit.send({ do: "group-say", id: "rename", group: group.id, name: input.value });
      });
      input.addEventListener("keydown", (event) => {
        if (event.key !== "Enter" || event.isComposing || event.keyCode === 229) return;
        event.preventDefault();
        event.stopPropagation();
        kit.close();
      });
      // The icon opens the page's picker under it, which the app asks for.
      iconButton.addEventListener("click", () => {
        if (shown.picking) return;
        const r = iconButton.getBoundingClientRect();
        shown.picking = true;
        kit.send({ do: "group-icon", group: group.id,
          rects: [{ left: Math.round(r.left), top: Math.round(r.top), right: Math.round(r.right), bottom: Math.round(r.bottom) }] });
      });
      return shown;
    }

    // Under its head, its start at the head's start (end, right to left),
    // kept in the window, on whole pixels of the screen at the zoom (Notion's
    // at 1.2: the head's foot 33.4 CSS pixels down, the menu 40 pixels down).
    function place(shown, headRect, room) {
      const box = shown.box;
      const width = box.offsetWidth, height = box.offsetHeight;
      const pixel = (value) => Math.round(value * window.devicePixelRatio) / window.devicePixelRatio;
      let left = room.rtl ? headRect.right - width : headRect.left;
      left = Math.max(MARGIN, Math.min(left, room.width - MARGIN - width));
      let top = headRect.bottom;
      if (top + height > room.height - MARGIN) top = Math.max(0, room.height - MARGIN - height);
      box.style.left = `${pixel(left)}px`;
      box.style.top = `${pixel(top)}px`;
    }

    // The name to type, all of it chosen (Notion's focusInitial and selectAll).
    function focusName(shown) {
      shown.input.focus({ preventScroll: true });
      shown.input.select();
    }

    // Whether the menu's lines hold the keys (Tab took them there).
    function linesHaveKeys(shown) {
      return document.activeElement === shown.list;
    }

    // The focusable parts in the order Tab goes, round within the menu
    // (Notion's trapFocus): the lines last, as one, taking the keys at the
    // lit one or the first.
    function cycle(shown, event) {
      const parts = [shown.iconButton, shown.input, ...shown.grid.children, shown.list];
      const at = parts.indexOf(document.activeElement);
      const next = at < 0 ? 0 : (at + (event.shiftKey ? -1 : 1) + parts.length) % parts.length;
      if (parts[next] === shown.list && shown.lit < 0) kit.step(shown, 1);
      parts[next].focus({ preventScroll: true });
    }

    // The menu's keys, but while the page's picker has them: Tab round the
    // menu; ↑ ↓ over its lines and Enter on one lit while the lines hold the
    // keys; anything else is the name's or the focused button's own.
    function keydown(shown, event) {
      if (shown.picking) return false;
      if (event.key === "Escape" && shown.tips.keys) {
        // A tip the keys' focus shows takes the Escape alone (Notion's
        // Tooltip captures it while focused), the menu staying.
        shown.tips.taken = true;
        shown.tips.keys = null;
        drawTips(shown);
      } else if (event.key === "Escape") {
        kit.close("escape");
      } else if (event.key === "Tab") {
        cycle(shown, event);
      } else if (!linesHaveKeys(shown)) {
        return false;
      } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        kit.step(shown, event.key === "ArrowDown" ? 1 : -1);
      } else if (event.key === "Enter" && shown.lit >= 0 && !shown.lines[shown.lit].off) {
        shown.lines[shown.lit].run();
      } else return false;
      event.preventDefault();
      event.stopPropagation();
      return true;
    }

    // The page's picker over the menu, at a place of the strip (null: it
    // closed): the menu shows nothing there, its shadow kept round it.
    function pickerAt(shown, rect) {
      const box = shown.box;
      if (!rect) {
        shown.hole = null;
        shown.picking = false;
        box.style.clipPath = "";
        focusName(shown);
        return;
      }
      shown.picking = true;
      shown.hole = rect;
      const r = box.getBoundingClientRect();
      const w = r.width, h = r.height;
      const x1 = rect.left - r.left, y1 = rect.top - r.top, x2 = rect.right - r.left, y2 = rect.bottom - r.top;
      box.style.clipPath = `path(evenodd, "M${-REACH} ${-REACH}H${w + REACH}V${h + REACH}H${-REACH}Z` +
        `M${x1} ${y1}H${x2}V${y2}H${x1}Z")`;
    }

    // What the app tells of the group meanwhile: its icon picked in the
    // page; the menu goes with the group.
    function refresh(shown, groups) {
      const now = (groups || []).find((group) => group.id === shown.group.id);
      if (!now) return false;
      if (now.icon !== shown.group.icon) {
        shown.group.icon = now.icon;
        shown.iconButton.replaceChildren(mark(shown.group));
      }
      return true;
    }

    // The icon or the color the keys are on, or none.
    function keyControl(shown) {
      const at = document.activeElement;
      return at === shown.iconButton || (at && at.parentNode === shown.grid) ? at : null;
    }

    // Tips: a color's name in the page's dark tip, centered 6 under it
    // (Notion's Tooltip, placement bottom, its gap 6, 4 in from the window's
    // edges). The pointer resting on a color shows its tip after 150 ms,
    // unless Notion's keyboard mode was on as it came onto it (its tip asks
    // at the pointer's coming, before the move that ends that mode); leaving
    // the color or a press on it takes it. The keys' focus on a color shows
    // its tip at once while it is ringed (keyTip), till Escape takes it
    // alone or the focus leaves.
    function restOn(shown, cell) {
      window.clearTimeout(shown.tips.wait);
      if (keyboardMode) return;
      shown.tips.wait = window.setTimeout(() => { shown.tips.hover = cell; drawTips(shown); }, 150);
    }
    function leave(shown, cell) {
      window.clearTimeout(shown.tips.wait);
      if (shown.tips.hover !== cell) return;
      shown.tips.hover = null;
      drawTips(shown);
    }

    // The color the keys' focus is on and ringed, or none (ringLit).
    function keyTip(shown, control) {
      const cell = control && control.parentNode === shown.grid ? control : null;
      if (cell !== shown.tips.keyed) {
        shown.tips.keyed = cell;
        shown.tips.taken = false;
      }
      const keys = shown.tips.taken ? null : cell;
      if (keys === shown.tips.keys) return;
      shown.tips.keys = keys;
      drawTips(shown);
    }

    function drawTips(shown) {
      const wanted = new Set([shown.tips.hover, shown.tips.keys].filter(Boolean));
      for (const [cell, tip] of shown.tips.shown) {
        if (wanted.has(cell)) continue;
        shown.tips.shown.delete(cell);
        tip.classList.add("leaving");
        window.setTimeout(() => tip.remove(), 50);
      }
      for (const cell of wanted) {
        if (shown.tips.shown.has(cell)) continue;
        const tip = element("span", "stip", { role: "tooltip" });
        tip.textContent = cell.getAttribute("aria-label");
        shown.box.appendChild(tip);
        const r = cell.getBoundingClientRect(), width = tip.offsetWidth, height = tip.offsetHeight;
        const pixel = (value) => Math.round(value * window.devicePixelRatio) / window.devicePixelRatio;
        const left = Math.max(4, Math.min(r.left + r.width / 2 - width / 2, window.innerWidth - 4 - width));
        const top = r.bottom + 6 + height > window.innerHeight - 4 ? r.top - 6 - height : r.bottom + 6;
        tip.style.left = `${pixel(left)}px`;
        tip.style.top = `${pixel(top)}px`;
        shown.tips.shown.set(cell, tip);
      }
    }

    return { open, place, focusName, keydown, linesHaveKeys, pickerAt, refresh, keyControl, keyTip };
  };
})();
