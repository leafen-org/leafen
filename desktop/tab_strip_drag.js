// A tab or a group dragged in the strip (tab_strip.js), as Notion 7.44.1's
// virtual tab bar drags them (its web client's Tabs chunk, read 2026-11-24,
// and its Windows app measured beside it): the system's own drag — the tab
// itself as its picture, its top left at the pointer (a group by its head's
// icon and name on its ground) — while the tabs stay where they are and a
// blue line 3 wide marks where it would go. Let go over a strip, it goes
// there: among a group's tabs it joins the group, beside the group's head or
// chevron in or out of it by the half pointed at; a group goes only between
// groups and tabs no group holds. Let go anywhere else, a tab — or a group —
// leaves its window: into another window's strip under the pointer, or a new
// window of its own (window_tabs_moves.go). Pinned tabs and the others each
// move among their own.
(function () {
  "use strict";
  // The kinds the drag carries, which a strip can read before the drop.
  const TAB = "application/x-leafen-tab";
  const PINNED = "application/x-leafen-pinned-tab";
  const GROUP = "application/x-leafen-tab-group";
  // How near the strip's ends a drag scrolls the tabs, and by how much.
  const EDGE = 48, STEP = 12;

  window.leafenTabDrag = function (strip) {
    // strip: tabs (the tabs' row), state(), send(message), began(), ended().
    const line = document.createElement("div");
    line.className = "drop-line";
    let dragging = null;

    function carried(event) {
      const types = [...(event.dataTransfer ? event.dataTransfer.types : [])];
      if (types.includes(GROUP)) return { kind: "group" };
      if (types.includes(PINNED)) return { kind: "tab", pinned: true };
      if (types.includes(TAB)) return { kind: "tab", pinned: false };
      return null;
    }

    // A tab's drag starts: its own picture, at the pointer.
    function tab(slot, data) {
      slot.setAttribute("draggable", "true");
      slot.addEventListener("dragstart", (event) => {
        const state = strip.state();
        dragging = { kind: "tab", id: data.id, pinned: data.pinned, group: data.group || "" };
        const said = JSON.stringify({ id: data.id, from: state.window });
        event.dataTransfer.setData(data.pinned ? PINNED : TAB, said);
        event.dataTransfer.effectAllowed = "move";
        event.dataTransfer.setDragImage(slot, 0, 0);
        strip.began();
      });
      slot.addEventListener("dragend", (event) => finish(event, "out"));
    }

    // A group's drag starts from its head: its icon and name on its ground
    // as the picture, where the head's label is.
    function group(head, id, picture) {
      const handle = head.querySelector(".gtitle") || head;
      handle.setAttribute("draggable", "true");
      handle.addEventListener("dragstart", (event) => {
        const state = strip.state();
        dragging = { kind: "group", id };
        event.dataTransfer.setData(GROUP, JSON.stringify({ id, from: state.window }));
        event.dataTransfer.effectAllowed = "move";
        const label = head.querySelector(".glabel");
        const image = picture();
        image.style.position = "fixed";
        image.style.top = "-100px";
        image.style.insetInlineStart = "0";
        document.body.appendChild(image);
        const box = label ? label.getBoundingClientRect() : handle.getBoundingClientRect();
        event.dataTransfer.setDragImage(image, Math.round(event.clientX - (box.left - 6)), Math.round(event.clientY - (box.top + box.height / 2 - 14)));
        window.setTimeout(() => image.remove(), 0);
        strip.began();
      });
      handle.addEventListener("dragend", (event) => finish(event, "group-out"));
    }

    // Let go: on no strip (the system dropped it nowhere), it leaves its
    // window at the pointer; a window's only tab only into another's strip.
    function finish(event, out) {
      const was = dragging;
      dragging = null;
      line.remove();
      strip.ended();
      if (!was || !event.dataTransfer || event.dataTransfer.dropEffect !== "none") return;
      strip.send({ do: out, id: was.id, x: Math.round(event.screenX), y: Math.round(event.screenY) });
    }

    // The strip's parts in their order, each with its place: group heads,
    // tabs and group ends, hidden tabs left out.
    function parts() {
      const state = strip.state();
      const byId = new Map(state.tabs.map((each, index) => [each.id, { tab: each, index }]));
      return [...strip.tabs.querySelectorAll(".ghead, .slot:not(.hidden), .gend")].map((node) => {
        const box = node.getBoundingClientRect();
        if (node.classList.contains("slot")) {
          const found = byId.get(node.dataset.id);
          return { node, box, kind: "tab", tab: found && found.tab, index: found ? found.index : -1 };
        }
        return { node, box, kind: node.classList.contains("ghead") ? "head" : "end", group: node.dataset.group };
      });
    }

    // Where a drag at the pointer would go: the place among the tabs (as the
    // window's tabs are, the dragged ones still in), the group it would be
    // in, and the side of which part the line stands at.
    function target(clientX, what) {
      const state = strip.state();
      const rtl = Boolean(state.rtl);
      const all = parts();
      if (!all.length) return null;
      const tabs = state.tabs;
      const first = (id) => tabs.findIndex((each) => each.group === id);
      const last = (id) => tabs.length - 1 - [...tabs].reverse().findIndex((each) => each.group === id);
      let at = all.findIndex((part) => clientX >= part.box.left && clientX < part.box.right);
      let before;
      if (at < 0) {
        const leftOf = clientX < all[0].box.left;
        at = leftOf !== rtl ? 0 : all.length - 1;
        before = at === 0 && leftOf !== rtl;
      } else {
        const box = all[at].box;
        before = (clientX < box.left + box.width / 2) !== rtl;
      }
      const part = all[at];
      const edge = (node, leading) => ({ node, leading });
      if (what.kind === "group") {
        // Only between groups and loose tabs.
        let group = part.kind === "tab" ? part.tab && part.tab.group : part.group;
        if (!group) return { index: before ? part.index : part.index + 1, mark: edge(part.node, before) };
        const head = all.find((each) => each.kind === "head" && each.group === group);
        const tail = all.find((each) => each.kind === "end" && each.group === group);
        const span = head && tail ? (head.box.left + tail.box.right) / 2 : 0;
        const leading = (clientX < span) !== rtl;
        return leading ? { index: first(group), mark: edge(head.node, true) } : { index: last(group) + 1, mark: edge(tail.node, false) };
      }
      if (part.kind === "head") {
        const shut = part.node.querySelector(".gtitle.shut");
        if (before || shut) return { index: first(part.group), group: "", mark: edge(part.node, true) };
        const firstTab = all.find((each) => each.kind === "tab" && each.tab && each.tab.group === part.group);
        return { index: first(part.group), group: part.group, mark: edge(firstTab ? firstTab.node : part.node, !firstTab) };
      }
      if (part.kind === "end") {
        const shut = !part.node.classList.contains("open");
        if (!before || shut) return { index: last(part.group) + 1, group: "", mark: edge(part.node, false) };
        const lastTab = [...all].reverse().find((each) => each.kind === "tab" && each.tab && each.tab.group === part.group);
        return { index: last(part.group) + 1, group: part.group, mark: edge(lastTab ? lastTab.node : part.node, false) };
      }
      const into = what.pinned || (part.tab && part.tab.pinned) ? "" : (part.tab && part.tab.group) || "";
      return { index: before ? part.index : part.index + 1, group: into, mark: edge(part.node, before) };
    }

    // The blue line at a part's side (Notion's drop indicator: 3 wide, 4 in
    // from the strip's top and foot, at the edge between two parts).
    function mark(at) {
      const node = at.mark.node;
      const box = node.getBoundingClientRect();
      const row = strip.tabs.getBoundingClientRect();
      const rtl = Boolean(strip.state().rtl);
      const leftEdge = at.mark.leading !== rtl;
      const x = (leftEdge ? box.left : box.right) - row.left + strip.tabs.scrollLeft - 1.5;
      line.style.left = `${x}px`;
      if (line.parentNode !== strip.tabs) strip.tabs.appendChild(line);
    }

    // The place among the tabs once the dragged one is out of them.
    function without(index, id) {
      const from = strip.state().tabs.findIndex((each) => each.id === id);
      return from >= 0 && from < index ? index - 1 : index;
    }

    document.addEventListener("dragover", (event) => {
      const what = carried(event);
      if (!what) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = "move";
      // Near an end, the tabs scroll along.
      const row = strip.tabs.getBoundingClientRect();
      if (event.clientX > row.right - EDGE && event.clientX <= row.right) strip.tabs.scrollLeft += STEP;
      else if (event.clientX < row.left + EDGE && event.clientX >= row.left) strip.tabs.scrollLeft -= STEP;
      const at = target(event.clientX, what);
      if (at) mark(at);
      else line.remove();
    });
    document.addEventListener("dragleave", (event) => {
      if (!event.relatedTarget || !document.documentElement.contains(event.relatedTarget)) line.remove();
    });
    document.addEventListener("drop", (event) => {
      const what = carried(event);
      line.remove();
      if (!what) return;
      event.preventDefault();
      const at = target(event.clientX, what);
      if (!at) return;
      let said;
      try {
        said = JSON.parse(event.dataTransfer.getData(what.kind === "group" ? GROUP : what.pinned ? PINNED : TAB));
      } catch (error) {
        return;
      }
      const state = strip.state();
      if (said.from !== state.window) {
        strip.send({ do: what.kind === "group" ? "insert-group" : "insert", id: said.id, from: said.from, index: at.index, group: at.group || "" });
        return;
      }
      if (what.kind === "group") {
        const run = state.tabs.filter((each) => each.group === said.id);
        const start = state.tabs.findIndex((each) => each.group === said.id);
        const index = at.index > start ? at.index - run.length : at.index;
        if (at.index >= start && at.index <= start + run.length) return;
        strip.send({ do: "drop-group", id: said.id, index });
        return;
      }
      const index = without(at.index, said.id);
      const own = state.tabs.find((each) => each.id === said.id);
      if (own && state.tabs.indexOf(own) === index && (own.group || "") === (at.group || "")) return;
      strip.send({ do: "drop", id: said.id, index, group: at.group || "" });
    });

    // A tab or a group carried over from another window's strip, let go
    // here (window_tabs_moves.go asks): the place at the pointer.
    function placeAt(clientX, kind) {
      const at = target(clientX, { kind, pinned: false });
      return at ? { index: at.index, group: at.group || "" } : { index: strip.state().tabs.length, group: "" };
    }

    return { tab, group, placeAt, dragging: () => dragging };
  };
})();
