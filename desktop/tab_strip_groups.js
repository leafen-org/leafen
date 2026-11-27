// Groups of tabs in the strip (tab_strip.js; desktop/window_tabs_groups.go),
// drawn as Notion 7.44.1's virtual tab bar draws its tab groups (its web
// client's Tabs chunk, read 2026-11-24, and its Windows app measured beside
// it): a group's tabs stand on a ground of its color, 28 high and rounded at
// its ends, led by its head — its icon, its name (120 wide at most) and a
// short line — and closed by a chevron that turns as it folds. A folded
// group shows its head alone, on its color's chip, or its head and the shown
// tab when that is in it. Its colors are the ones the pages work out from
// Notion's tokens (web/src/lib/tabGroupColors.ts) and tell with theirs.
(function () {
  "use strict";
  // Till a page has told them: the plain color's, from the pages' own.
  function palette(color, looks, colors) {
    const look = looks && (looks[color] || looks.default);
    if (look) return look;
    const c = colors || {};
    return { tray: c.bar || "transparent", chip: "transparent", title: c.textSecondary, titleFolded: c.textSecondary,
      divider: c.line, chevron: c.icon, tabText: c.textSecondary, tabTextActive: c.text, tabHover: c.press,
      tabActive: c.tab, icon: c.icon, close: c.icon };
  }

  const stroke = 'fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"';
  // Notion's folder, a group's icon until it is given another (its noticon
  // "folder", drawn in the app's own shape), and its small chevron.
  const FOLDER = '<svg viewBox="0 0 16 16" fill="currentColor"><path d="M2 4.6c0-.83.67-1.5 1.5-1.5h2.78c.4 0 .78.16 1.06.44l1.02 1.02h4.14c.83 0 1.5.67 1.5 1.5v5.84c0 .83-.67 1.5-1.5 1.5H3.5c-.83 0-1.5-.67-1.5-1.5z"/></svg>';
  const CHEVRON = `<svg viewBox="0 0 16 16" ${stroke}><path d="M6.4 4.2 10.2 8l-3.8 3.8"/></svg>`;

  function element(tag, className, attributes) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    for (const [name, value] of Object.entries(attributes || {})) {
      if (value !== undefined && value !== null) node.setAttribute(name, value);
    }
    return node;
  }

  // The ground behind a group's part: its head's start, a tab's middle or
  // its chevron's end (Notion's TabGroupTray).
  function tray(theme, part, open) {
    const node = element("div", `tray ${part}`, { "aria-hidden": "true" });
    node.style.background = open ? theme.tray : theme.chip;
    return node;
  }

  // A group's icon: its emoji, the app's icon it was given (drawn by its
  // shape in the group's icon color) or Notion's folder.
  function icon(group, theme, folded) {
    const node = element("span", `gicon${folded ? " folded" : ""}`);
    if (group.icon && group.icon.startsWith("data:image/svg+xml,")) {
      const shape = element("span", "gpicture");
      shape.style.setProperty("--shape", `url("${group.icon}")`);
      shape.style.background = theme.icon;
      node.appendChild(shape);
    } else if (group.icon) {
      node.textContent = group.icon;
      node.classList.add("emoji");
    } else {
      node.innerHTML = FOLDER;
      node.style.color = theme.icon;
    }
    return node;
  }

  // A group's head: its icon and name on its ground, then a short line,
  // while it is open (Notion's TabGroupHeader).
  function head(group, info, labels) {
    const theme = palette(group.color, info.looks, info.colors);
    const name = (group.name || "").trim();
    const shownName = name || labels.untitledGroup;
    const titleWords = (info.count === 1 ? labels.groupHeadOne : labels.groupHead)
      .replace("{name}", shownName).replace("{count}", String(info.count));
    const node = element("div", "ghead", { title: titleWords, "data-group": group.id });
    node.appendChild(tray(theme, "start", info.open));
    const button = element("div", `gtitle${info.open ? "" : " shut"}${group.menu ? " pressed" : ""}`, {
      role: "button", tabindex: "-1", "aria-label": titleWords, "aria-expanded": group.collapsed ? "false" : "true",
    });
    button.style.color = group.collapsed ? theme.titleFolded : theme.title;
    button.style.setProperty("--hover", theme.tabHover);
    const label = element("span", "glabel");
    label.appendChild(icon(group, theme, group.collapsed));
    if (name) {
      const text = element("span", "gname");
      text.textContent = name;
      label.appendChild(text);
    }
    button.appendChild(label);
    node.appendChild(button);
    const line = element("span", `gline${info.open ? "" : " shut"}`, { "aria-hidden": "true" });
    line.style.background = theme.divider;
    node.appendChild(line);
    return node;
  }

  // A group's end: the chevron, pointing back while the group is open and
  // on while it is folded (Notion's TabGroupEnd).
  function end(group, info, labels) {
    const theme = palette(group.color, info.looks, info.colors);
    const shownName = (group.name || "").trim() || labels.untitledGroup;
    const node = element("div", `gend${info.open ? " open" : ""}`, { "data-group": group.id });
    node.appendChild(tray(theme, "end", info.open));
    const words = (group.collapsed ? labels.expandGroup : labels.collapseGroup).replace("{name}", shownName);
    const button = element("div", "chevron", { role: "button", tabindex: "-1", "aria-label": words, "aria-expanded": group.collapsed ? "false" : "true" });
    button.style.setProperty("--hover", theme.tabHover);
    const turn = element("span", `turn${group.collapsed ? "" : " open"}`);
    turn.innerHTML = CHEVRON;
    turn.style.color = theme.chevron;
    button.appendChild(turn);
    node.appendChild(button);
    return node;
  }

  // A grouped tab: its ground's middle behind it, and its colors.
  function dress(slot, face, group, info, active) {
    const theme = palette(group.color, info.looks, info.colors);
    slot.insertBefore(tray(theme, "middle", info.open), slot.firstChild);
    slot.classList.add("grouped");
    face.style.setProperty("--tab-ground", active ? theme.tabActive : "transparent");
    face.style.setProperty("--tab-hover", active ? theme.tabActive : theme.tabHover);
    face.style.setProperty("--tab-text", active ? theme.tabTextActive : theme.tabText);
    face.style.setProperty("--close-color", theme.close);
  }

  // The picture a group's head is dragged by: its icon and name on its
  // ground, 28 high (Notion's group drag preview).
  function dragPicture(group, info, headNode) {
    const theme = palette(group.color, info.looks, info.colors);
    const label = headNode.querySelector(".glabel");
    const picture = element("div", "gdrag");
    picture.style.background = theme.tray;
    picture.style.color = theme.title;
    if (label) picture.appendChild(label.cloneNode(true));
    return picture;
  }

  window.leafenTabGroups = { head, end, dress, dragPicture };
})();
