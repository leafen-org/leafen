// Daily Briefing's screens: the page (the briefing and the morning notice's
// settings), the side panel beside any page and the block in a page. The
// briefing comes from the app's worker, run as the person; the settings
// stay in the person's own store, where the morning schedule reads them.
import { connect } from "./leafen-app-sdk.js";

const app = await connect();
const place = document.body.dataset.place;
const ko = String(app.context.locale || "").startsWith("ko");
const w = ko ? {
  title: "하루 브리핑", lead: "내 워크스페이스의 할 일과 멘션을 모았습니다.", overdue: "기한 지난 일", today: "오늘 할 일", soon: "이번 주",
  mentions: "읽지 않은 멘션", nothing: "없습니다.", undated: (n) => `기한 없는 할 일 ${n}개`, settings: "아침 알림", enabled: "알림 받기",
  hour: "시각", hourLabel: (h) => `${h}시`, weekdays: "평일만", save: "저장", saved: "저장했습니다.", failed: "불러오지 못했습니다.",
  more: (n) => `외 ${n}개`, open: "하루 브리핑 열기",
} : {
  title: "Daily Briefing", lead: "Your tasks and mentions across your workspaces.", overdue: "Overdue", today: "Due today", soon: "This week",
  mentions: "Unread mentions", nothing: "Nothing.", undated: (n) => `${n} tasks without a due date`, settings: "Morning notice", enabled: "Send it",
  hour: "Hour", hourLabel: (h) => `${String(h).padStart(2, "0")}:00`, weekdays: "Weekdays only", save: "Save", saved: "Saved.", failed: "Couldn't load.",
  more: (n) => `${n} more`, open: "Open Daily Briefing",
};
document.documentElement.dataset.theme = app.context.theme;
app.onTheme((theme) => { document.documentElement.dataset.theme = theme; });
const root = document.getElementById("app");

function h(tag, attributes = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attributes)) {
    if (key === "on") for (const [event, handler] of Object.entries(value)) node.addEventListener(event, handler);
    else if (value !== undefined && value !== null && value !== false) node.setAttribute(key, value === true ? "" : String(value));
  }
  node.append(...children.flat().filter((child) => child !== null && child !== undefined && child !== false));
  return node;
}

function item(entry, meta) {
  return h("li", {}, h("a", { href: "#", "data-page": entry.pageId, on: { click: (event) => { event.preventDefault(); void app.openPage(entry.pageId); } } },
    h("span", { class: "title" }, entry.title || "—"), h("span", { class: "meta" }, meta)));
}

// A section counts all it has, though its list holds fewer (the worker gives the newest ten mentions of them all).
function list(kind, label, entries, meta, limit, total = entries.length) {
  const shown = limit ? entries.slice(0, limit) : entries;
  return h("section", { "data-section": kind },
    h("h2", { "data-kind": total ? kind : "none" }, label, h("b", {}, String(total))),
    total ? h("ul", {}, shown.map((entry) => item(entry, meta(entry))),
      total > shown.length ? h("li", { class: "meta" }, w.more(total - shown.length)) : null) : h("p", { class: "empty" }, w.nothing));
}

const due = (entry) => [entry.due ? entry.due.slice(5, 10).replace("-", "/") : "", entry.database].filter(Boolean).join(" · ");

// The worker runs on the server's clock: what day it is here is the screen's to say.
const localOffset = () => -new Date().getTimezoneOffset();

// The morning notice and an agent's call have no screen to ask, so the offset is kept.
async function keepOffset() {
  const stored = await app.storage.get("settings").catch(() => null);
  if (stored?.offset === localOffset()) return;
  await app.storage.set("settings", { enabled: true, hour: 8, weekdays: true, ...(stored ?? {}), offset: localOffset() }).catch(() => {});
}

async function settingsCard() {
  let stored = null;
  try { stored = await app.storage.get("settings"); } catch { stored = null; }
  const current = { enabled: true, hour: 8, weekdays: true, ...(stored ?? {}) };
  const enabled = h("input", { type: "checkbox", name: "enabled", ...(current.enabled ? { checked: true } : {}) });
  const hour = h("select", { name: "hour" }, Array.from({ length: 24 }, (_, value) => h("option", { value, ...(value === current.hour ? { selected: true } : {}) }, w.hourLabel(value))));
  const weekdays = h("input", { type: "checkbox", name: "weekdays", ...(current.weekdays ? { checked: true } : {}) });
  const status = h("p", { class: "status", role: "status" });
  const save = h("button", { class: "primary", type: "button", "data-save": true, on: { click: async () => {
    await app.storage.set("settings", { enabled: enabled.checked, hour: Number(hour.value), weekdays: weekdays.checked, offset: localOffset() });
    status.textContent = w.saved;
  } } }, w.save);
  return h("section", { class: "settings", "data-settings": true }, h("h2", {}, w.settings),
    h("label", {}, enabled, w.enabled), h("label", {}, w.hour, hour), h("label", {}, weekdays, w.weekdays), save, status);
}

async function render() {
  let briefing;
  try {
    void keepOffset();
    briefing = (await app.action("today", { offset: localOffset() })).output;
  } catch (error) {
    root.replaceChildren(h("p", { class: "status", role: "alert" }, `${w.failed} ${String(error?.message || error)}`));
    return;
  }
  const mentionMeta = (entry) => [entry.preview !== entry.title ? entry.preview : "", entry.at ? new Date(entry.at).toLocaleDateString(app.context.locale) : ""]
    .filter(Boolean).join(" · ");
  const mentionItems = briefing.mentions.map((entry) => ({ ...entry, title: entry.title || entry.preview }));
  if (place === "block") {
    root.replaceChildren(
      h("div", { class: "counts", "data-counts": true }, h("span", {}, `${w.overdue} `, h("b", {}, String(briefing.overdue.length))),
        h("span", {}, `${w.today} `, h("b", {}, String(briefing.today.length))), h("span", {}, `${w.mentions} `, h("b", {}, String(briefing.unread)))),
      h("ul", { "data-block-list": true }, [...briefing.overdue, ...briefing.today].slice(0, 5).map((entry) => item(entry, due(entry)))));
    // The block is as tall as what it lists.
    await app.resize(Math.max(64, Math.ceil(document.body.getBoundingClientRect().height)));
    return;
  }
  const limit = place === "panel" ? 6 : 0;
  root.replaceChildren(...[
    // Beside a page, Leafen's panel already names the app.
    place === "index" ? h("h1", {}, w.title) : null, place === "index" ? h("p", { class: "lead" }, `${briefing.date} · ${w.lead}`) : null,
    list("overdue", w.overdue, briefing.overdue, due, limit), list("today", w.today, briefing.today, due, limit),
    list("soon", w.soon, briefing.soon, due, limit), list("mentions", w.mentions, mentionItems, mentionMeta, limit, briefing.unread),
    briefing.undated ? h("p", { class: "meta" }, w.undated(briefing.undated)) : null,
    place === "index" ? await settingsCard() : null,
  ].filter(Boolean));
}

await render();
