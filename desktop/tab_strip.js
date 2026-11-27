// The tab strip of a Mac or Windows app window (tab_strip.html): it draws
// what the app says through leafenStrip.set and tells the app what the
// person does as the window's messages ("leafen-tabs:" and JSON,
// window_tabs_strip.go), with Wails' own for moving, zooming and (Windows)
// resizing the window by its strip.
(function () {
  "use strict";
  const lead = document.getElementById("lead");
  const nav = document.getElementById("nav");
  const list = document.getElementById("list");
  const tabRow = document.getElementById("box");
  const tabs = document.getElementById("tabs");
  const add = document.getElementById("add");
  const trail = document.getElementById("trail");
  const shade = document.getElementById("shade");
  // A tab's room (Notion's tab wrapper): 40 to 204 wide, 40 pinned, with 2px
  // each side of the tab; the tabs' row 2px in at each end; "+" in 40.
  const ROOM = 40, WIDEST = 204, ENDS = 4, ADD = 40;
  let state = null;
  let held = null;
  let dragged = null;

  function post(message) {
    const handlers = window.webkit && window.webkit.messageHandlers;
    const webview = window.chrome && window.chrome.webview;
    if (handlers && handlers.external) handlers.external.postMessage(message);
    else if (webview) webview.postMessage(message);
    else (window.leafenStripSent = window.leafenStripSent || []).push(message);
  }
  const windows = () => Boolean(state && state.platform === "windows");
  function send(message) { post("leafen-tabs:" + JSON.stringify(message)); }
  const groups = window.leafenTabGroups;
  const motion = window.leafenTabMotion;
  const stripMenu = window.leafenStripMenu({ send, state: () => state });
  const ring = window.leafenStripRing({ tabs, nav });
  // The card of the tab the pointer rests on (tab_strip_hover.js).
  const hover = window.leafenStripHover({ tabs, send, state: () => state, dragged: () => Boolean(dragged), menuOpen: () => stripMenu.isOpen() });
  const tabDrag = window.leafenTabDrag({
    tabs, state: () => state, send,
    began() { hover.off(); dragged = true; },
    // A strip the app told meanwhile is drawn now.
    ended() {
      dragged = null;
      if (held) set(held);
    },
  });

  const stroke = 'fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"';
  const ICONS = {
    // The sidebar button: the sidebar drawn in while it is open, a short bar
    // while it is folded (Notion's sidebarLeft and sidebarLeftCollapsed).
    sidebar: `<svg viewBox="0 0 20 20" ${stroke}><rect x="2.25" y="4.25" width="15.5" height="11.5" rx="1.5"/><path d="M7 4.25v11.5" stroke-width="1.1" stroke-linecap="butt"/></svg>`,
    folded: `<svg viewBox="0 0 20 20" ${stroke}><rect x="2.25" y="4.25" width="15.5" height="11.5" rx="1.5"/><path d="M5.25 7.25v5.5"/></svg>`,
    back: `<svg viewBox="0 0 20 20" ${stroke}><path d="M12.15 4.6 6.75 10l5.4 5.4"/></svg>`,
    forward: `<svg viewBox="0 0 20 20" ${stroke}><path d="M7.85 4.6 13.25 10l-5.4 5.4"/></svg>`,
    plus: `<svg viewBox="0 0 20 20" ${stroke}><path d="M4.225 10h11.55M10 4.225v11.55"/></svg>`,
    close: `<svg viewBox="0 0 16 16" ${stroke}><path d="M4.6 4.6l6.8 6.8M11.4 4.6l-6.8 6.8"/></svg>`,
    // A pinned tab's page with no icon of its own (Phosphor's FileText, filled).
    page: `<svg viewBox="0 0 256 256" fill="currentColor"><path d="M213.66,82.34l-56-56A8,8,0,0,0,152,24H56A16,16,0,0,0,40,40V216a16,16,0,0,0,16,16H200a16,16,0,0,0,16-16V88A8,8,0,0,0,213.66,82.34ZM160,176H96a8,8,0,0,1,0-16h64a8,8,0,0,1,0,16Zm0-32H96a8,8,0,0,1,0-16h64a8,8,0,0,1,0,16Zm-8-56V44l44,44Z"/></svg>`,
    // The sidebar's inbox and "신규" over it, the page's own marks (Phosphor's
    // Tray and NotePencil, as its sidebar head draws them: MailIcon, ComposeIcon).
    inbox: `<svg viewBox="0 0 256 256" fill="currentColor"><path d="M208,32H48A16,16,0,0,0,32,48V208a16,16,0,0,0,16,16H208a16,16,0,0,0,16-16V48A16,16,0,0,0,208,32Zm0,16V152h-28.7A15.86,15.86,0,0,0,168,156.69L148.69,176H107.31L88,156.69A15.86,15.86,0,0,0,76.69,152H48V48Zm0,160H48V168H76.69L96,187.31A15.86,15.86,0,0,0,107.31,192h41.38A15.86,15.86,0,0,0,160,187.31L179.31,168H208v40Z"/></svg>`,
    compose: `<svg viewBox="0 0 256 256" fill="currentColor"><path d="M229.66,58.34l-32-32a8,8,0,0,0-11.32,0l-96,96A8,8,0,0,0,88,128v32a8,8,0,0,0,8,8h32a8,8,0,0,0,5.66-2.34l96-96A8,8,0,0,0,229.66,58.34ZM124.69,152H104V131.31l64-64L188.69,88ZM200,76.69,179.31,56,192,43.31,212.69,64ZM224,128v80a16,16,0,0,1-16,16H48a16,16,0,0,1-16-16V48A16,16,0,0,1,48,32h80a8,8,0,0,1,0,16H48V208H208V128a8,8,0,0,1,16,0Z"/></svg>`,
    // The app's mark, tinted as the icons (Notion's notionTintableIcon).
    mark: `<div class="mark"></div>`,
  };

  function element(tag, className, attributes) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    for (const [name, value] of Object.entries(attributes || {})) {
      if (value !== undefined && value !== null) node.setAttribute(name, value);
    }
    return node;
  }
  // A 28×28 button of the strip (Notion's tab bar iconButton).
  function button(kind, label, onPress, options) {
    const node = element("div", `button${options && options.off ? " off" : ""}`,
      { role: "button", "aria-label": label, "aria-disabled": options && options.off ? "true" : undefined, title: options && options.tip });
    node.innerHTML = ICONS[kind];
    node.addEventListener("click", (event) => {
      event.stopPropagation();
      if (!(options && options.off)) onPress(event);
    });
    node.addEventListener("mousedown", (event) => event.stopPropagation());
    // A right or middle press: the back and forward buttons' history menu.
    if (options && options.onMenu) {
      node.addEventListener("contextmenu", (event) => { event.preventDefault(); options.onMenu(); });
      node.addEventListener("auxclick", (event) => { if (event.button === 1) options.onMenu(); });
    }
    return node;
  }

  // The pages' colors (web/src/lib/desktopTabs.ts): the sidebar's, the
  // page's, the sidebar's edge, the tab bar's line, a tab's ground, text,
  // icons and a pressed button's ground.
  function paint() {
    const colors = state.colors || {};
    const root = document.documentElement.style;
    const names = { bar: "--bar", page: "--page", border: "--border", line: "--line", tab: "--tab", text: "--text",
      textSecondary: "--text-secondary", icon: "--icon", iconDim: "--icon-dim", press: "--press" };
    for (const [key, name] of Object.entries(names)) if (colors[key]) root.setProperty(name, colors[key]);
    // The tab menu's look, the page's menus' (tab_strip_menu.js).
    for (const key of ["menuGround", "menuShadow", "menuRadius", "menuDivider", "menuDividerMargin", "menuDanger", "menuIcon",
      "menuChevron", "rowHeight", "rowPadding", "rowRadius", "rowGap", "rowSpace", "rowFont", "rowLine",
      "fieldGround", "fieldRing", "fieldRingFocus", "placeholder", "buttonOutline", "buttonHover", "keyRing", "tipShadow"]) {
      if (colors[key]) root.setProperty(`--m-${key}`, colors[key]);
    }
    document.documentElement.dir = state.rtl ? "rtl" : "ltr";
    root.setProperty("--strip", `${state.strip || 40}px`);
    shade.classList.toggle("on", Boolean(state.shade));
    shade.style.background = state.shade || "";
  }

  // Where the strip's first button starts (Notion's tab bar and its
  // sidebar's head): on a Mac 4px before the end of the traffic lights' room
  // (the lights' span with their own inset after them, none in full screen),
  // 8px at least, or 8px past it with no sidebar button (signing in); on
  // Windows 12px, the app menu's mark, the rest 46px in.
  function leadStart(shown) {
    if (windows()) return state.rtl ? 0 : 12;
    const room = state.fullscreen ? 0 : state.lights || 0;
    return shown ? Math.max(8, room - 4) : room + 8;
  }

  function drawLead() {
    const labels = state.labels;
    // A page without a sidebar (signing in) has no sidebar button, as
    // Notion's tab bar has none then.
    const shown = Boolean(state.sidebar && state.sidebar.shown);
    const expanded = shown && Boolean(state.sidebar.expanded);
    lead.className = `section${expanded ? " over-sidebar" : ""}`;
    lead.style.paddingInlineStart = `${leadStart(shown)}px`;
    lead.style.width = expanded ? `${state.sidebar.width}px` : "";
    const label = expanded ? labels.closeSidebar : labels.openSidebar;
    const toggle = button(expanded ? "sidebar" : "folded", label, () => send({ do: "sidebar" }), { tip: `${label} (${labels.sidebarKey})` });
    const items = shown ? [toggle] : [];
    // The sidebar's head over it, as Notion's tab bar with the switcher at the
    // sidebar's foot: the inbox with its unread count and "신규", at the
    // sidebar's end while it is open and after the sidebar button while it is
    // folded. The page draws what they open (its sidebar's own buttons,
    // SidebarTopActions.tsx).
    const head = shown ? headActions(state.sidebar) : null;
    if (head) {
      if (expanded) items.push(element("div", "spacer"));
      items.push(head);
    }
    // Windows has no menu bar: the app's menu opens from its mark, first,
    // and what follows it starts 46px in (Notion's WindowsAppMenuIconButton).
    if (windows()) {
      const mark = button("mark", labels.appMenu, () => send({ do: "appmenu" }));
      mark.style.marginInlineEnd = `${expanded ? 2 : 4}px`;
      items.unshift(mark);
    }
    lead.replaceChildren(...items);
    // Back and forward: 8px into the tab bar at the sidebar's edge; 2px after
    // what is before them while it is folded, at the start with nothing there.
    nav.className = `section${expanded ? "" : " collapsed"}`;
    nav.style.paddingInlineStart = !expanded && !items.length ? "0px" : "";
    const rtl = Boolean(state.rtl);
    // A ⌘-pressed arrow opens where it leads in a new tab behind; a right or
    // middle press lists the history that way (Notion's).
    const go = (way) => (event) => send({ do: way, meta: Boolean(event.metaKey || event.ctrlKey) });
    const history = (way) => () => send({ do: "history", id: way });
    const back = button(rtl ? "forward" : "back", labels.back, go("back"), { off: !state.back, onMenu: history("back") });
    const forward = button(rtl ? "back" : "forward", labels.forward, go("forward"), { off: !state.forward, onMenu: history("forward") });
    nav.replaceChildren(ring.button(back, "back"), ring.button(forward, "forward"));
    // Past "+", Notion's 54px to move the window by, but in full screen.
    trail.className = `section${state.fullscreen ? " fullscreen" : ""}`;
  }

  // The inbox and "신규" of the page's sidebar head (state.sidebar.inbox and
  // .new: "" none, "on", "open", "off"; "new" also "ask", a guest's that
  // asks to become a member, dimmed as the page's).
  function headActions(sidebar) {
    const labels = state.labels;
    const actions = [];
    if (sidebar.inbox) {
      const inbox = button("inbox", labels.inbox, () => send({ do: "head", id: "inbox" }), { off: sidebar.inbox === "off", tip: labels.inbox });
      if (sidebar.inbox === "open") inbox.classList.add("on");
      // The unread count over the button's corner, "99+" past 99, as the page's.
      const unread = state.unread || 0;
      if (unread > 0) {
        const badge = element("div", "badge");
        badge.textContent = unread > 99 ? "99+" : String(unread);
        inbox.appendChild(badge);
      }
      actions.push(inbox);
    }
    if (sidebar.new) {
      const compose = button("compose", labels.compose, (event) => {
        const box = event.currentTarget.getBoundingClientRect();
        // Where the page's own button stands for its menu: the page starts under the strip.
        const top = window.innerHeight;
        send({ do: "head", id: "new", x: Math.round(box.left), right: Math.round(box.right),
          y: Math.round(box.top) - top, bottom: Math.round(box.bottom) - top });
      }, { off: sidebar.new === "off", tip: labels.compose });
      compose.classList.add("compose");
      if (sidebar.new === "open") compose.classList.add("on");
      if (sidebar.new === "ask") compose.classList.add("asks");
      actions.push(compose);
    }
    if (!actions.length) return null;
    const node = element("div", "head");
    node.replaceChildren(...actions);
    return node;
  }

  // Every tab not pinned and not folded away is as wide as the room
  // allows, its room between 40 and 204 (its tab 36 to 200), and "+"
  // follows the last (Notion's virtual tab bar: the tabs' row grows to that,
  // the groups' heads and ends besides, and no further). A tab at its
  // narrowest shows its ✕ over its middle.
  function fitTabs(fixed) {
    const shown = state.tabs.filter((tab) => !tab.hidden);
    const pinned = shown.filter((tab) => tab.pinned).length;
    const widest = pinned * ROOM + (shown.length - pinned) * WIDEST + ENDS + ADD + fixed;
    tabRow.style.maxWidth = `${widest}px`;
    const free = shown.length - pinned;
    const room = Math.min(list.clientWidth, widest) - ADD - ENDS - pinned * ROOM - fixed;
    const narrow = free > 0 && room / free <= ROOM;
    for (const slot of tabs.querySelectorAll(".slot:not(.pinned)")) slot.classList.toggle("narrow", narrow);
  }

  // The top third of a tab moves the window as the strip's empty places do
  // (Notion's tab top drag strip).
  function tabTop(event) {
    const face = event.target instanceof Element ? event.target.closest(".tab") : null;
    if (!face) return false;
    const box = face.getBoundingClientRect();
    return event.clientY < box.top + box.height / 3;
  }

  function drawTab(tab, group, info) {
    const slot = element("div", ["slot", tab.pinned && "pinned", tab.active && "active", tab.hidden && "hidden"]
      .filter(Boolean).join(" "));
    slot.dataset.id = tab.id;
    const words = tab.title || state.labels.untitled;
    const face = element("div", "tab", { role: "button", tabindex: "-1", "aria-label": words, "aria-selected": tab.active ? "true" : "false",
      title: tab.pinned ? tab.title || undefined : undefined });
    const inner = element("div", "inner");
    // Its page's icon; a pinned tab's page with none, a page mark; one still
    // loading, gray bars for its icon and name.
    if (tab.icon || (tab.pinned && tab.title)) {
      const icon = element("div", "icon");
      if (tab.icon) icon.appendChild(element("img", "", { src: tab.icon, alt: "", draggable: "false" }));
      else icon.innerHTML = ICONS.page;
      inner.appendChild(icon);
    } else if (!tab.title) inner.appendChild(element("div", "skeleton-icon"));
    if (!tab.pinned) {
      const label = element("div", "label");
      if (tab.title) {
        const title = element("div", "title");
        title.textContent = tab.title;
        label.appendChild(title);
      } else label.appendChild(element("div", "skeleton-title"));
      inner.appendChild(label);
    }
    face.appendChild(inner);
    if (!tab.pinned) {
      const room = element("div", "close-room");
      const close = element("div", "close", { role: "button", "aria-label": state.labels.closeTab.replace("{title}", words) });
      close.innerHTML = ICONS.close;
      close.addEventListener("mousedown", (event) => event.stopPropagation());
      close.addEventListener("click", (event) => {
        event.stopPropagation();
        send({ do: "close", id: tab.id });
      });
      room.appendChild(close);
      face.appendChild(room);
    }
    slot.appendChild(face);
    if (group) groups.dress(slot, face, group, info, tab.active);
    // A press on the tab itself, not the room around it, shows it.
    face.addEventListener("mousedown", (event) => {
      hover.off();
      if (event.button === 0 && !tab.active) send({ do: "select", id: tab.id });
    });
    // The pointer resting on a tab not shown: its card under it, which the
    // shown page draws (Notion's tab preview).
    slot.addEventListener("mouseenter", (event) => hover.enter(tab, slot, event));
    slot.addEventListener("mouseleave", () => hover.leave(tab));
    slot.addEventListener("auxclick", (event) => {
      if (event.button === 1) send({ do: "close", id: tab.id });
    });
    slot.addEventListener("contextmenu", (event) => {
      if (tabTop(event)) return;
      event.preventDefault();
      send({ do: "menu", id: tab.id, x: Math.round(event.clientX), y: Math.round(event.clientY) });
    });
    // The menu key on a tab with the keys: its menu under it, from its start (Notion's).
    ring.menuKey(face, () => { const box = face.getBoundingClientRect();
      send({ do: "menu", id: tab.id, x: Math.round(state.rtl ? box.right : box.left), y: Math.round(box.bottom) }); });
    tabDrag.tab(slot, { id: tab.id, pinned: tab.pinned, group: tab.group });
    return slot;
  }

  // A group's head or chevron's right press, or the menu key on it, opens
  // the group's menu under its head (tab_strip_group_menu.js).
  function groupMenu(id, from) {
    const head = [...tabs.querySelectorAll(".ghead")].find((node) => node.dataset.group === id);
    const title = head && head.querySelector(".gtitle");
    if (!title) return;
    const box = title.getBoundingClientRect();
    send({ do: "group-menu", id, from, x: Math.round(box.left), right: Math.round(box.right), y: Math.round(box.top), bottom: Math.round(box.bottom) });
  }

  function groupParts(group, info) {
    const head = groups.head(group, info, state.labels);
    const end = groups.end(group, info, state.labels);
    const fold = (event) => { event.stopPropagation(); hover.off(); motion.pressed(event); send({ do: "fold", id: group.id }); };
    const menu = (event) => { event.preventDefault(); event.stopPropagation(); groupMenu(group.id); };
    for (const button of [head.querySelector(".gtitle"), end.querySelector(".chevron")]) {
      button.addEventListener("mousedown", (event) => event.stopPropagation());
      button.addEventListener("click", fold);
      button.addEventListener("contextmenu", menu);
      ring.menuKey(button, menu);
    }
    tabDrag.group(head, group.id, () => groups.dragPicture(group, info, head));
    return { head, end };
  }

  // The tabs in their order, each group's run led by its head and closed by
  // its chevron; a folded group's tabs but the shown one are out of sight
  // (Notion's getVirtualTabRows).
  function drawTabs() {
    const was = motion.before(tabs);
    const byId = new Map((state.groups || []).map((group) => [group.id, group]));
    const active = state.tabs.find((tab) => tab.active);
    const counts = new Map();
    for (const tab of state.tabs) if (byId.has(tab.group)) counts.set(tab.group, (counts.get(tab.group) || 0) + 1);
    const nodes = [];
    state.tabs.forEach((tab, index) => {
      const group = byId.get(tab.group);
      tab.hidden = Boolean(group && group.collapsed && !tab.active);
      if (!group) {
        nodes.push(drawTab(tab, null, null));
        return;
      }
      const info = { looks: state.groupLooks, colors: state.colors, count: counts.get(group.id),
        open: !group.collapsed || Boolean(active && active.group === group.id) };
      const first = index === 0 || state.tabs[index - 1].group !== group.id;
      const last = index === state.tabs.length - 1 || state.tabs[index + 1].group !== group.id;
      const parts = first || last ? groupParts(group, info) : null;
      if (first) nodes.push(parts.head);
      nodes.push(drawTab(tab, group, info));
      if (last) nodes.push(parts.end);
    });
    tabs.replaceChildren(...nodes);
    add.replaceChildren(button("plus", state.labels.newTab, () => send({ do: "new" })));
    let fixed = 0;
    for (const part of tabs.querySelectorAll(".ghead, .gend")) fixed += part.offsetWidth;
    fitTabs(fixed);
    motion.after(tabs, was);
    hover.stays();
    const shown = tabs.querySelector(".slot.active");
    if (shown) shown.scrollIntoView({ block: "nearest", inline: "nearest" });
  }

  // A tab or a group dragged out of another window and let go over this
  // strip goes where it was let go (window_tabs_moves.go asks).
  function take(carried) {
    const at = tabDrag.placeAt(carried.x, "tab");
    send({ do: "insert", id: carried.id, from: carried.from, index: at.index, group: at.group });
  }
  function takeGroup(carried) {
    const at = tabDrag.placeAt(carried.x, "group");
    send({ do: "insert-group", id: carried.id, from: carried.from, index: at.index });
  }

  // A group just made from a tab's menu opens its menu as it is drawn, its
  // name to type (Notion's new group); the tab (from) takes the keys back.
  function openGroupMenu(id, from) {
    window.requestAnimationFrame(() => groupMenu(id, from));
  }

  // The strip's empty places and the top third of each tab move the window
  // (Notion's drag areas, but while a tab is dragged), and a
  // double press there does what the system says a title bar's does. On
  // Windows the top edge resizes the window, and a right press there opens
  // the window's own menu.
  const RESIZE_EDGE = 4;
  function empty(target) {
    return !(target instanceof Element) || !target.closest(".button, .slot, .gtitle, .chevron, .smenu");
  }
  function grip(event) {
    return !dragged && (empty(event.target) || tabTop(event));
  }
  function resizing(event) {
    return windows() && !state.maximized && !state.fullscreen && event.clientY < RESIZE_EDGE;
  }
  // The window's move takes the first press's release on Windows, so the
  // page tells a double press by its time and place (the system's 500ms).
  let lastGrip = { at: -1e9, x: 0, y: 0 };
  // A press swallowed below does nothing on its way out either: the click it
  // ends in (a tab's "×", a group's head, a menu's row), its middle button's
  // auxclick (a tab's close) or its right button's menu, till the next press.
  let swallowed = false;
  for (const type of ["click", "auxclick", "dblclick", "contextmenu"]) {
    document.addEventListener(type, (event) => { if (swallowed) { event.preventDefault(); event.stopPropagation(); } }, true);
  }
  document.addEventListener("mousedown", (event) => {
    // A press outside the strip's menu closes it; below the strip, where it
    // is grown over the page, it does nothing more, nor anywhere while it
    // closes the page's picker over a group's menu.
    swallowed = false;
    if (stripMenu.pressedOutside(event.target)) {
      if (stripMenu.pressOutside() || event.clientY >= document.getElementById("bar").offsetHeight) {
        event.preventDefault();
        event.stopPropagation();
        swallowed = true;
        return;
      }
    }
    if (event.button !== 0 || !grip(event)) return;
    event.preventDefault();
    event.stopPropagation();
    const again = event.timeStamp - lastGrip.at < 500 && Math.abs(event.screenX - lastGrip.x) < 4 && Math.abs(event.screenY - lastGrip.y) < 4;
    lastGrip = again ? { at: -1e9, x: 0, y: 0 } : { at: event.timeStamp, x: event.screenX, y: event.screenY };
    if (resizing(event)) post("wails:resize:n-resize");
    else if (windows()) {
      if (again) send({ do: "maximize" });
      else post("wails:drag");
    } else post(event.detail > 1 ? "wails:drag:doubleclick" : "wails:drag");
  }, true);
  document.addEventListener("mousemove", (event) => {
    hover.moved(event);
    document.documentElement.style.cursor = resizing(event) ? "ns-resize" : "";
  });
  // A press gives the shown page the keys back, but to a menu of the strip
  // open; a right press, which may ask for one, where it opens none (below).
  document.addEventListener("mouseup", (event) => { if (event.button !== 2 && !stripMenu.isOpen()) send({ do: "focus" }); });
  document.addEventListener("keydown", (event) => { swallowed = false; stripMenu.keydown(event); }, true);
  // A Mac window going behind closes the strip's menu, but to the page's
  // picker over it (a Windows one stays open, as Notion's: there the page
  // under the strip takes the presses outside it, DesktopStripMenus.tsx).
  window.addEventListener("blur", () => { if (stripMenu.isOpen() && !stripMenu.picking() && !windows()) stripMenu.close("blur"); });
  window.addEventListener("focus", () => window.requestAnimationFrame(() => stripMenu.regained()));
  // The web view's own menu (reload, inspect) has no place on the strip; a
  // tab's menu is the app's (window_tabs_strip.go).
  document.addEventListener("contextmenu", (event) => {
    const taken = event.defaultPrevented;
    event.preventDefault();
    if (windows() && !taken && grip(event)) send({ do: "systemmenu", x: Math.round(event.clientX), y: Math.round(event.clientY) });
    else if (!taken && !stripMenu.isOpen()) send({ do: "focus" });
  });
  tabs.addEventListener("wheel", (event) => {
    if (Math.abs(event.deltaY) > Math.abs(event.deltaX)) tabs.scrollLeft += event.deltaY;
  }, { passive: true });
  new ResizeObserver(() => { if (state && !dragged && !motion.moving()) drawTabs(); }).observe(list);

  // What the app tells while a tab is dragged or a fold moves is drawn after.
  let later = 0;
  function set(next) {
    if (dragged || motion.moving()) {
      held = next;
      if (!dragged && !later) later = window.setTimeout(() => { later = 0; if (held) set(held); }, motion.left() + 16);
      return;
    }
    held = null;
    state = next;
    document.documentElement.classList.remove("unset");
    paint();
    drawLead();
    drawTabs();
    stripMenu.refresh(state.groups);
    ring.redrawn();
  }
  // A tab's or a group's menu the app opens (window_tabs_strip_menus.go),
  // drawn over the strip as it grows: see-through below the strip meanwhile.
  // Its opening takes the card down; closing, the card is the tab's the
  // pointer is on (pointer: where the app says it is, or null off the strip).
  function openMenu(menu) {
    ring.clear();
    hover.opened();
    document.documentElement.classList.add("grown");
    stripMenu.open(menu);
  }
  function closeMenu(pointer) {
    stripMenu.close(null);
    document.documentElement.classList.remove("grown");
    ring.shrinks((state && state.strip) || 40);
    hover.closed(pointer || null);
  }
  // The page's icon picker over a group's menu, at a place of the strip, or none.
  function pickerAt(rect) { stripMenu.pickerAt(rect); }
  window.leafenStrip = { set, take, takeGroup, openGroupMenu, openMenu, closeMenu, pickerAt, ring: (target) => ring.set(target) };
  // The page has no Wails runtime of its own: it tells the window it is
  // ready for the app's scripts, then asks for what to show.
  post("wails:runtime:ready");
  send({ do: "ready" });
})();
