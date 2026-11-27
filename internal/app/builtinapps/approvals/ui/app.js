// The Approvals app's screen: what waits for the person, what they asked
// for, and a new request. Every change goes through the app's actions,
// whose rules run on the server; the screen only shows and asks.
import { connect } from "./leafen-app-sdk.js";
import { applyTheme, element as h, wordsFor } from "./i18n.js";

const app = await connect();
const w = wordsFor(app.context.locale);
applyTheme(app);
document.title = w.title;
const root = document.getElementById("app");
const tabs = ["waiting", "mine", "new"];
let tab = tabs.includes(placeOf(app.context.path)) ? placeOf(app.context.path) : "waiting";
let data = { waiting: [], mine: [], counts: {} };
let people = new Map();
let members = [];
let more = false;
let me = "";
let draft = null;
let message = "";

function placeOf(path) {
  return String(path || "").split(/[?#]/u)[0].replace(/^\/+/u, "");
}

function name(id) {
  return people.get(id) || id || "—";
}

function amount(value) {
  return Number(value || 0).toLocaleString(app.context.locale);
}

function remember(list) {
  for (const person of list) people.set(person.id, person.name);
}

// The people who may use the app here, by name, the first window of them;
// the approver is one of them. When more follow (nextCursor) the form finds
// the approver by name or email instead of offering only those first ones.
async function loadPeople() {
  const listed = await app.api("GET", "/app-people");
  members = listed.body?.people ?? [];
  more = Boolean(listed.body?.nextCursor);
  me = listed.body?.you ?? "";
  remember(members);
}

// The names of those the lists show that the first window didn't hold, a
// hundred at a time; one who may no longer use the app stays an id.
async function loadNames() {
  const ids = [...new Set([...data.waiting.map((item) => item.requester), ...data.mine.map((item) => item.approver)])]
    .filter((id) => id && !people.has(id));
  for (let start = 0; start < ids.length; start += 100) {
    const asked = ids.slice(start, start + 100);
    const listed = await app.api("GET", `/app-people?ids=${asked.map(encodeURIComponent).join(",")}`);
    remember(listed.body?.people ?? []);
    for (const id of asked) if (!people.has(id)) people.set(id, "");
  }
}

async function load() {
  const result = await app.action("overview");
  data = result.output;
  // What waits is oldest first; the person's own requests newest first.
  data.mine.sort((a, b) => String(b.requestedAt).localeCompare(String(a.requestedAt)));
}

function failure(error) {
  const text = String(error?.message || error || "");
  // The person chose not to allow it: nothing went wrong, so nothing is said.
  if (error?.code === "LEAFEN_APP_APPROVAL_DECLINED" || text.startsWith("LEAFEN_APP_APPROVAL_DECLINED")) return "";
  const reason = text.includes(": ") ? text.slice(text.indexOf(": ") + 2) : text;
  // The worker says why in English, for agents too; the screen says it in the person's words.
  return w.reasons?.[reason] ?? reason;
}

function counts() {
  const c = data.counts || {};
  return h("section", { class: "counts", "aria-label": w.title },
    h("div", { class: "count" }, h("b", {}, String(c["대기"] ?? 0)), h("span", {}, w.countWaiting)),
    h("div", { class: "count" }, h("b", {}, String(c["승인"] ?? 0)), h("span", {}, w.countApproved)),
    h("div", { class: "count" }, h("b", {}, String(c["반려"] ?? 0)), h("span", {}, w.countRejected)));
}

// The page a request links to, as the rules wrote it ("/p/<id>"); the rules
// alone say what a page's ID may be.
function linkedPage(item) {
  return /^\/p\/(.+)$/u.exec(String(item?.page || ""))?.[1] ?? "";
}

function pageLink(item) {
  const id = linkedPage(item);
  if (!id) return null;
  return h("a", { class: "link", href: "#", on: { click: (event) => { event.preventDefault(); void app.openPage(id); } } }, w.openPage);
}

function waitingItem(item) {
  const note = h("input", { class: "note", placeholder: w.note, "aria-label": w.note });
  const decide = async (key, button) => {
    button.disabled = true;
    try {
      await app.action(key, { requestId: item.id, ...(note.value.trim() ? { note: note.value.trim() } : {}) });
      message = key === "approve" ? w.approved : w.rejected;
      await refresh();
    } catch (error) {
      message = failure(error);
      render();
    }
  };
  const approve = h("button", { class: "primary", type: "button", "data-approve": item.id }, w.approve);
  const reject = h("button", { class: "secondary", type: "button", "data-reject": item.id }, w.reject);
  approve.addEventListener("click", () => void decide("approve", approve));
  reject.addEventListener("click", () => void decide("reject", reject));
  return h("article", { class: "item", "data-request": item.id },
    h("header", {}, h("h2", {}, item.title || w.untitled), h("span", { class: "chip", "data-status": item.status }, w.statuses[item.status] ?? item.status)),
    h("div", { class: "meta" }, `${w.from(name(item.requester))} · ${w.amount(amount(item.amount))}`),
    item.reason ? h("p", { class: "reason" }, item.reason) : null,
    h("div", { class: "actions" }, note, approve, reject, pageLink(item)));
}

function mineItem(item) {
  const finish = item.status === "초안"
    ? h("button", { class: "secondary", type: "button", "data-finish": item.id, on: { click: () => { draft = item; go("new"); } } }, w.finish)
    : null;
  return h("article", { class: "item", "data-request": item.id },
    h("header", {}, h("h2", {}, item.title || w.untitled), h("span", { class: "chip", "data-status": item.status }, w.statuses[item.status] ?? item.status)),
    h("div", { class: "meta" }, [item.approver ? w.to(name(item.approver)) : null, item.amount ? w.amount(amount(item.amount)) : null, item.decided || null].filter(Boolean).join(" · ")),
    item.note ? h("p", { class: "reason" }, item.note) : null,
    h("div", { class: "actions" }, finish, pageLink(item)));
}

// The approver's choices: those a list holds but the person, the one chosen
// kept while a search shows others.
function fillApprover(approver, list, searched) {
  const chosen = approver.value;
  const shown = list.filter((person) => person.id !== me);
  if (chosen && !shown.some((person) => person.id === chosen)) shown.unshift({ id: chosen, name: name(chosen) });
  approver.replaceChildren(h("option", { value: "" }, searched && !shown.length ? w.noMatch : w.choose),
    ...shown.map((person) => h("option", { value: person.id }, person.name || person.id)));
  approver.value = chosen;
}

// With more people than a window, the approver is found by name or email;
// the latest search alone fills the choices.
function approverSearch(approver) {
  let timer = 0;
  let asked = 0;
  return h("input", { type: "search", name: "approverSearch", placeholder: w.findApprover, "aria-label": w.findApprover, autocomplete: "off",
    on: { input: (event) => {
      const text = event.target.value.trim();
      const turn = ++asked;
      clearTimeout(timer);
      timer = setTimeout(async () => {
        try {
          const listed = await app.api("GET", text ? `/app-people?q=${encodeURIComponent(text)}` : "/app-people");
          if (turn !== asked) return;
          remember(listed.body?.people ?? []);
          fillApprover(approver, listed.body?.people ?? [], Boolean(text));
        } catch {
          // The choices stay as they were; the next letter asks again.
        }
      }, 200);
    } } });
}

function newForm() {
  const title = h("input", { name: "title", required: true, value: draft?.title ?? "" });
  const approver = h("select", { name: "approver", required: true, "aria-label": w.formApprover });
  fillApprover(approver, members, false);
  const others = members.filter((person) => person.id !== me);
  const nobodyElse = !more && !others.length;
  // With one other person there is nothing to choose.
  if (!more && others.length === 1) approver.value = others[0].id;
  const total = h("input", { name: "amount", type: "number", min: 0, step: "any", inputmode: "decimal" });
  const reason = h("textarea", { name: "reason" });
  const error = h("p", { class: "error", role: "alert" });
  const send = h("button", { class: "primary", type: "submit" }, w.send);
  const form = h("form", { "data-new-request": true },
    h("label", {}, w.formTitle, title), h("label", {}, w.formApprover, more ? approverSearch(approver) : null, approver), nobodyElse ? h("p", { class: "status", "data-nobody-else": true }, w.nobodyElse) : null, h("label", {}, w.formAmount, total),
    h("label", {}, w.formReason, reason), draft?.page ? h("div", { class: "meta" }, `${w.formPage}: `, pageLink(draft)) : null, error, h("div", {}, send));
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    send.disabled = true;
    error.textContent = "";
    const pageId = linkedPage(draft);
    try {
      await app.action("submit", {
        title: title.value.trim(), approver: approver.value, amount: Number(total.value || 0), reason: reason.value.trim(),
        ...(draft ? { requestId: draft.id } : {}), ...(pageId ? { pageId } : {}),
      });
      draft = null;
      message = w.sent;
      await refresh();
      go("mine");
    } catch (failed) {
      error.textContent = failure(failed);
      send.disabled = false;
    }
  });
  return form;
}

function render() {
  const list = tab === "waiting" ? data.waiting : data.mine;
  root.replaceChildren(...[
    h("h1", {}, w.title), h("p", { class: "lead" }, w.lead), counts(),
    h("nav", { "aria-label": w.title }, tabs.map((key) => h("button", {
      type: "button", "aria-current": key === tab ? "page" : false, "data-tab": key, on: { click: () => go(key) },
    }, w[key]))),
    message ? h("p", { class: "status", role: "status" }, message) : null,
    tab === "new" ? newForm()
      : list.length ? h("section", {}, list.map(tab === "waiting" ? waitingItem : mineItem))
        : h("p", { class: "empty" }, tab === "waiting" ? w.emptyWaiting : w.emptyMine),
  ].filter(Boolean));
}

function go(next) {
  if (next !== "new") draft = null;
  tab = next;
  void app.navigate(next);
  render();
}

async function refresh() {
  try {
    await load();
    await loadNames();
  } catch (error) {
    message = `${w.failed} ${failure(error)}`;
  }
  render();
}

app.onLocation((path) => {
  const next = placeOf(path);
  if (tabs.includes(next)) { tab = next; render(); }
});
try {
  await Promise.all([loadPeople(), load()]);
  await loadNames();
} catch (error) {
  message = `${w.failed} ${failure(error)}`;
}
render();
