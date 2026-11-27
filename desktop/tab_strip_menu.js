// A tab's menu, which the strip draws over the window (tab_strip.js), as
// Notion 7.44.1's tab bar draws its "탭 작업" (measured in its Windows app
// 2026-11-24): at the press, over the strip — its top at the press's y,
// about 20 down the 40 of the strip — 250 wide with rows 28 high; "링크 복사",
// "탭 복제", "탭 고정", "탭을 다음 위치로 이동 ›"; then "탭 닫기", "다른 탭 닫기",
// the tabs before and after it (left and right as the screen reads) and
// "마지막으로 닫은 탭 다시 열기", each off with nothing to do and the closes red
// under the pointer. "탭을 다음 위치로 이동" opens beside its row, 220 wide
// and centered on it, 4 over the menu (Notion's TabMoveToMenu): the
// window's groups, the tab's own checked, then "그룹에서 제거" for a grouped
// tab, "+ 신규 그룹" and "새 창" (off with one tab). One line is lit at a
// time: the first as the menu opens, by a press or a key alike (Notion's
// initialFocus 0), then the one the pointer is on or ↑ ↓ reach, kept lit
// as the pointer leaves the menu (Notion's resets it on no mouse out; seen
// in its Windows app 2026-11-24), a close line red while lit; → opens the
// rows beside, ← and Escape close them, Enter acts, Tab does nothing. The
// lit line of the list the keys work in wears the page's keyboard ring
// while the keys lead, as a browser's focus-visible counts them — a key but
// a shortcut or a lone modifier turns it on, a press off, the pointer's
// moves neither — as does a group's menu's icon or color the keys are on;
// and a line the pointer is on is shaded too, red if it
// closes, lit or not: Notion's (its Windows app 2026-11-24, two runs each):
// opened by a right press the first line is shaded unringed, ↓ rings the
// next, a letter rings a line the pointer lit, the pointer then moving
// over another line lights it ringed; opened by the menu key the first
// line is ringed; a row whose rows beside are open is shaded, not ringed.
// The menu looks as the page's menus do, in the colors and words the page
// tells (web/src/lib/desktopTabs.ts). While it is open the
// app grows the strip over the window, see-through where it draws nothing
// (window_tabs_strip_room*.go); what is chosen goes back as "menu-pick",
// saying whether a key chose it (Enter), for the app to ring what takes the
// keys then or leave it unringed for a press (window_tabs_strip_keys.go).
// A group's menu it draws the same way (tab_strip_group_menu.js).
(function () {
  "use strict";
  const MARGIN = 12;
  const stroke = 'fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"';
  const CHEVRON = `<svg viewBox="0 0 16 16" ${stroke}><path d="M6.4 4.2 10.2 8l-3.8 3.8"/></svg>`;
  const CHECK = `<svg viewBox="0 0 16 16" ${stroke}><path d="m3.6 8.4 2.8 2.8 6-6.4"/></svg>`;
  const PLUS = `<svg viewBox="0 0 20 20" ${stroke}><path d="M4.225 10h11.55M10 4.225v11.55"/></svg>`;
  const FOLDER = '<svg viewBox="0 0 16 16" fill="currentColor"><path d="M2 4.6c0-.83.67-1.5 1.5-1.5h2.78c.4 0 .78.16 1.06.44l1.02 1.02h4.14c.83 0 1.5.67 1.5 1.5v5.84c0 .83-.67 1.5-1.5 1.5H3.5c-.83 0-1.5-.67-1.5-1.5z"/></svg>';

  function element(tag, className, attributes) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    for (const [name, value] of Object.entries(attributes || {})) {
      if (value !== undefined && value !== null) node.setAttribute(name, value);
    }
    return node;
  }

  window.leafenStripMenu = function (strip) {
    const layer = document.getElementById("menus");
    let shown = null;
    // A key is being taken (keydown), so a pick now is the key's.
    let keyed = false;
    // Whether the keys lead (a key put down last, not a press): the lit
    // line the keys work on is ringed meanwhile (ringLit).
    let keysLead = false;

    const word = (key, fallback) => (strip.state().words || {})[key] || fallback;
    const pick = (what, extra) => { strip.send({ do: "menu-pick", id: what, from: shown.tab, keys: keyed, ...extra }); close(null); };
    const look = (color) => (strip.state().groupLooks || {})[color] || {};

    // A group's mark in the menu: its emoji, its picture in its color, or the folder.
    function groupMark(group) {
      const look = (strip.state().groupLooks || {})[group.color] || {};
      const mark = element("i", "smark");
      if (group.icon && group.icon.startsWith("data:image/svg+xml,")) {
        const shape = element("span", "gpicture");
        shape.style.setProperty("--shape", `url("${group.icon}")`);
        shape.style.background = look.icon || "currentColor";
        mark.appendChild(shape);
      } else if (group.icon) {
        mark.textContent = group.icon;
        mark.classList.add("emoji");
      } else {
        mark.innerHTML = FOLDER;
        mark.style.color = look.icon || "";
      }
      return mark;
    }

    function rows(box, sections, state) {
      const lines = [];
      sections.forEach((section, at) => {
        if (at > 0) box.appendChild(element("div", "sdivider", { role: "separator" }));
        for (const line of section) {
          const row = element("div", `srow${line.danger ? " danger" : ""}`, { role: "menuitem",
            "aria-disabled": line.off ? "true" : undefined, "aria-haspopup": line.sub ? "menu" : undefined,
            "aria-checked": line.checked === undefined ? undefined : String(line.checked) });
          if (line.mark) row.appendChild(line.mark);
          const label = element("span", "slabel");
          label.textContent = line.label;
          row.appendChild(label);
          if (line.checked) row.insertAdjacentHTML("beforeend", `<i class="scheck">${CHECK}</i>`);
          if (line.sub) row.insertAdjacentHTML("beforeend", `<i class="schevron">${CHEVRON}</i>`);
          const index = lines.length;
          row.addEventListener("mousemove", () => { light(state, index); if (line.sub) openSub(row); else if (state === shown) closeSub(); });
          row.addEventListener("mousedown", (event) => { event.preventDefault(); event.stopPropagation(); });
          row.addEventListener("click", (event) => {
            event.stopPropagation();
            if (line.off) return;
            if (line.sub) openSub(row);
            else line.run();
          });
          box.appendChild(row);
          lines.push({ ...line, row });
        }
      });
      return lines;
    }

    function light(state, index) {
      state.lit = index;
      state.lines.forEach((line, at) => line.row.classList.toggle("lit", at === index));
      ringLit();
    }

    // The list the keys work in: the rows beside a tab's menu while they
    // are open, else the menu's; a group's lines only while they hold the
    // keys (tab_strip_group_menu.js), its name or colors otherwise.
    function keysList() {
      if (!shown) return null;
      if (shown.kind === "group") return groupMenu.linesHaveKeys(shown) ? shown : null;
      return shown.sub && shown.sub.lit >= 0 ? shown.sub : shown;
    }

    // Rings what the keys work on while they lead, as the page's keyboard
    // ring — the lit line of the list the keys work in, or the icon or the
    // color of a group's menu the keys are on, a color then with its name in
    // a tip (tab_strip_group_menu.js) — and nothing else.
    function ringLit() {
      const state = keysLead ? keysList() : null;
      const line = state && state.lit >= 0 ? state.lines[state.lit] : null;
      const control = keysLead && shown && shown.kind === "group" ? groupMenu.keyControl(shown) : null;
      const ringed = line && !line.off ? line.row : control;
      for (const node of layer.querySelectorAll(".keyring")) if (node !== ringed) node.classList.remove("keyring");
      if (ringed) ringed.classList.add("keyring");
      if (shown && shown.kind === "group") groupMenu.keyTip(shown, control);
    }

    // A key but a shortcut (⌘, Ctrl or Alt held) or a modifier alone puts
    // the keys in the lead, as the menu key does before it opens a menu; a
    // press takes it back.
    function keyLeads(event) {
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      if (["Shift", "Control", "Alt", "Meta", "CapsLock", "Fn", "AltGraph", "OS"].includes(event.key)) return;
      keysLead = true;
    }
    document.addEventListener("mousedown", () => {
      if (!keysLead) return;
      keysLead = false;
      ringLit();
    }, true);

    // At the press, its top left (top right right to left); turned the
    // other way where it would leave the window, and up where it would
    // leave its foot.
    function place(box, x, y, room) {
      const width = box.offsetWidth, height = box.offsetHeight;
      let left = room.rtl ? x - width : x;
      if (left + width > room.width - MARGIN) left = x - width;
      if (left < MARGIN) left = room.rtl ? x : Math.max(MARGIN, Math.min(x, room.width - MARGIN - width));
      let top = y;
      if (top + height > room.height - MARGIN) top = Math.max(MARGIN, y - height);
      box.style.left = `${Math.round(left)}px`;
      box.style.top = `${Math.round(top)}px`;
    }

    function open(menu) {
      close(null);
      if (menu.kind === "group") {
        shown = groupMenu.open(menu);
        layer.replaceChildren(shown.box);
        const title = document.querySelector(`.ghead[data-group="${CSS.escape(menu.group.id)}"] .gtitle`);
        groupMenu.place(shown, title ? title.getBoundingClientRect() : menu.head, menu);
        groupMenu.focusName(shown);
        tellRoom();
        return;
      }
      const box = element("div", "smenu", { role: "menu", "aria-label": word("menu.label", "Tab actions") });
      box.style.width = "250px";
      shown = { ...menu, box, lit: -1, lines: [], sub: null };
      const can = menu.can || {};
      const rtl = Boolean(menu.rtl);
      shown.lines = rows(box, [[
        { label: word("menu.copyLink", "Copy link"), run: () => pick("copyLink") },
        { label: word("menu.duplicate", "Duplicate tab"), run: () => pick("duplicate") },
        { label: menu.pinned ? word("menu.unpin", "Unpin tab") : word("menu.pin", "Pin tab"), run: () => pick("pin") },
        { label: word("menu.moveTo", "Move tab to"), sub: true },
      ], [
        { label: word("menu.close", "Close tab"), danger: true, run: () => pick("close") },
        { label: word("menu.closeOthers", "Close other tabs"), danger: true, off: !can.closeOthers, run: () => pick("closeOthers") },
        { label: word(rtl ? "menu.closeRight" : "menu.closeLeft", "Close tabs to the left"), danger: true, off: !can.closeBefore, run: () => pick("closeBefore") },
        { label: word(rtl ? "menu.closeLeft" : "menu.closeRight", "Close tabs to the right"), danger: true, off: !can.closeAfter, run: () => pick("closeAfter") },
        { label: word("menu.reopen", "Reopen last closed tab"), off: !can.reopen, run: () => pick("reopen") },
      ]], shown);
      layer.replaceChildren(box);
      place(box, menu.x, menu.y, menu);
      // Its first line lit, opened by a press or a key (Notion's).
      step(shown, 1);
      tellRoom();
    }

    // "탭을 다음 위치로 이동 ›"'s rows beside it.
    function openSub(row) {
      if (!shown || (shown.sub && shown.sub.row === row)) return;
      closeSub();
      const menu = shown;
      const box = element("div", "smenu", { role: "menu", "aria-label": word("menu.moveTo", "Move tab to") });
      box.style.width = "220px";
      const sub = { row, box, lit: -1, lines: [] };
      const groups = (menu.groups || []).map((group) => ({ label: (group.name || "").trim() || word("groups.untitled", "Untitled group"),
        checked: group.id === menu.group, mark: groupMark(group), run: () => pick("moveToGroup", { group: group.id }) }));
      const plus = element("i", "smark wide");
      plus.innerHTML = PLUS;
      const other = [
        ...(menu.group ? [{ label: word("groups.removeFromGroup", "Remove from group"), run: () => pick("removeFromGroup") }] : []),
        { label: word("groups.newGroup", "New group"), mark: plus, run: () => pick("newGroup") },
        { label: word("groups.newWindow", "New window"), off: !(menu.can || {}).newWindow, run: () => pick("newWindow") },
      ];
      sub.lines = rows(box, groups.length ? [groups, other] : [other], sub);
      layer.appendChild(box);
      menu.sub = sub;
      // Its first row lit as it opens, the row beside it staying lit (Notion's, seen twice 2026-11-24).
      step(sub, 1);
      const parent = menu.box.getBoundingClientRect(), at = row.getBoundingClientRect();
      // Centered on its row, 4 over the menu (Notion's: 686 beside a menu ending at 690).
      const top = Math.min(Math.max(MARGIN, at.top + at.height / 2 - box.offsetHeight / 2), menu.height - MARGIN - box.offsetHeight);
      const width = box.offsetWidth;
      let left = menu.rtl ? parent.left + 4 - width : parent.right - 4;
      if (!menu.rtl && left + width > menu.width - MARGIN) left = parent.left + 4 - width;
      if (menu.rtl && left < MARGIN) left = parent.right - 4;
      box.style.left = `${Math.round(left)}px`;
      box.style.top = `${Math.round(Math.max(MARGIN, top))}px`;
      tellRoom();
    }

    function closeSub() {
      if (!shown || !shown.sub) return;
      shown.sub.box.remove();
      shown.sub = null;
      ringLit();
      tellRoom();
    }

    // The places the app keeps the strip grown over: the menus drawn, but
    // the page's picker over a group's menu; and the menus' boxes as they
    // are, whose shadow the page draws where the strip cannot (Windows).
    function tellRoom() {
      const boxes = [...layer.children].map((box) => box.getBoundingClientRect());
      const rects = boxes.map((r) => ({ left: Math.floor(r.left), top: Math.floor(r.top), right: Math.ceil(r.right), bottom: Math.ceil(r.bottom) }));
      const shadows = boxes.map((r) => ({ left: r.left, top: r.top, width: r.width, height: r.height }));
      const holes = shown && shown.hole ? [shown.hole] : [];
      strip.send({ do: "menu-room", rects, holes, shadows });
    }

    // The page's icon picker over a group's menu (at a place of the strip,
    // a pixel round it for its ring), or none.
    function pickerAt(rect) {
      if (!shown || shown.kind !== "group") return;
      groupMenu.pickerAt(shown, rect && { left: Math.floor(rect.left) - 1, top: Math.floor(rect.top) - 1,
        right: Math.ceil(rect.right) + 1, bottom: Math.ceil(rect.bottom) + 1 });
      tellRoom();
    }

    // The keys back in the strip, a group's menu's name takes them, all of
    // it chosen, as the app gives them for it: a web view given the keys puts
    // its page's focus back where it last was (WebView2) or drops it (WebKit).
    function regained() {
      if (shown && shown.kind === "group" && !shown.picking && !shown.box.contains(document.activeElement)) groupMenu.focusName(shown);
    }

    // What the app tells meanwhile: a group's menu goes with its group.
    function refresh(groups) {
      if (shown && shown.kind === "group" && !groupMenu.refresh(shown, groups)) close("");
    }

    // Closes the menus, telling the app why ("escape", "outside", "blur" or
    // "" — the app gives the keys back to what it opened from, or to the
    // page for "blur"), or saying nothing (null): a pick, or the app's own.
    function close(why) {
      if (!shown) return;
      shown = null;
      layer.replaceChildren();
      if (why !== null) strip.send({ do: "menu-closed", id: why || "" });
    }

    function step(state, by) {
      const lines = state.lines;
      for (let tries = 1, at = state.lit; tries <= lines.length; tries++) {
        at = at < 0 ? (by > 0 ? 0 : lines.length - 1) : (at + by + lines.length) % lines.length;
        if (!lines[at].off) { light(state, at); return; }
      }
    }

    function keydown(event) {
      keyLeads(event);
      if (!shown) return false;
      keyed = true;
      try {
        return shown.kind === "group" ? groupMenu.keydown(shown, event) : tabMenuKeydown(event);
      } finally {
        keyed = false;
        ringLit();
      }
    }

    function tabMenuKeydown(event) {
      const state = shown.sub && shown.sub.lit >= 0 ? shown.sub : shown;
      const back = shown.rtl ? "ArrowRight" : "ArrowLeft", on = shown.rtl ? "ArrowLeft" : "ArrowRight";
      const line = state.lines[state.lit];
      if (event.key === "Escape" || (event.key === back && state !== shown)) {
        if (state !== shown) closeSub(); else close("escape");
      } else if (event.key === "ArrowDown" || event.key === "ArrowUp") step(state, event.key === "ArrowDown" ? 1 : -1);
      else if (event.key === on && line && line.sub) openSub(line.row);
      else if (event.key === "Enter" && line && !line.off) {
        if (line.sub) openSub(line.row); else line.run();
      } else if (event.key !== "Tab") return false;
      event.preventDefault();
      return true;
    }

    // A press outside the menus closes them; on the strip it does what it
    // does there too (Notion's: another tab pressed is shown). While the
    // page's picker is over a group's menu, any press the strip takes is
    // outside the picker: it closes the picker alone and does nothing more
    // (pressOutside says true), nor does the click it ends in (tab_strip.js).
    function pressedOutside(target) {
      return Boolean(shown) && (shown.picking || !(target instanceof Element && target.closest(".smenu")));
    }
    function pressOutside() {
      if (shown && shown.picking) {
        strip.send({ do: "picker-close" });
        return true;
      }
      close("outside");
      return false;
    }

    const groupMenu = window.leafenStripGroupMenu({ element, rows, light, step, word, look, pick, ringLit, send: strip.send,
      close: (why) => close(why || "") });

    return { open, close, keydown, pressedOutside, pressOutside, pickerAt, refresh, regained, isOpen: () => Boolean(shown),
      picking: () => Boolean(shown && shown.picking) };
  };
})();
