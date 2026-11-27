// The Web Clips app's screen: how to point a clipper at the app's address,
// and the person's newest clips. Clips arrive at the endpoint, signed in
// with the person's app password; the screen only shows and guides.
import { connect } from "./leafen-app-sdk.js";
import { applyTheme, element as h, wordsFor } from "./i18n.js";
import { outsideAddressNote, publicHttpsAddress } from "./outside-address.js";

const app = await connect();
const w = wordsFor(app.context.locale);
applyTheme(app);
document.title = w.title;
const root = document.getElementById("app");
const origin = new URL(app.context.homeUrl).origin;
const address = `${origin}/app-endpoints/web-clip/clip/`;
const template = JSON.stringify({
  title: "{title}", url: "{pageURL}", content: "{content}", excerpt: "{excerpt}", byline: "{byline}", keywords: "{keywords}", source: "MarkSnip",
}, null, 2);
let table = null;
let clips = [];
let error = "";
let message = "";

async function copy(text) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    const field = h("textarea", {}, text);
    document.body.append(field);
    field.select();
    document.execCommand("copy");
    field.remove();
  }
  message = w.copied;
  render();
}

function copyRow(value, label) {
  return h("div", { class: "address" },
    h("input", { readonly: true, value, "aria-label": label }),
    h("button", { class: "secondary", type: "button", on: { click: () => void copy(value) } }, w.copy));
}

function when(value) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "" : date.toLocaleString(app.context.locale, { dateStyle: "medium", timeStyle: "short" });
}

function site(value) {
  try {
    return new URL(value).hostname;
  } catch {
    return "";
  }
}

function connectBox() {
  return h("section", { class: "box", "data-clip-connect": true },
    h("h2", {}, w.connect),
    h("ol", { class: "steps" },
      h("li", {}, w.stepPassword, " ",
        h("button", { class: "text", type: "button", on: { click: () => void app.openLink(`${origin}/apps#app-passwords`) } }, w.openApps)),
      h("li", {}, w.stepAddress, copyRow(address, w.stepAddress)),
      h("li", {}, w.stepHeader, copyRow(w.headerValue, "Authorization"))),
    // MarkSnip sends only to public https host names.
    publicHttpsAddress(origin) ? null : outsideAddressNote(app, h, w.notPublic, { "data-clip-not-public": true }),
    h("h2", { style: "margin-top: 16px" }, w.template),
    h("pre", { class: "template", "data-clip-template": true }, template),
    h("div", { class: "actions" }, h("button", { class: "secondary", type: "button", on: { click: () => void copy(template) } }, w.copy)),
    h("p", { class: "hint" }, w.templateHint));
}

function recentBox() {
  return h("section", { class: "box", "data-clip-recent": true },
    h("h2", {}, w.recent),
    clips.length
      ? h("ul", { class: "clips" }, clips.map((row) => h("li", {},
        h("button", { type: "button", on: { click: () => void app.openPage(row.id) } }, row.text("제목") || "—"),
        h("span", { class: "meta" }, [site(row.text("주소")), when(row.createdAt)].filter(Boolean).join(" · ")))))
      : h("p", { class: "hint empty" }, w.none),
    table?.pageId
      ? h("div", { class: "actions" }, h("button", { class: "text", type: "button", on: { click: () => void app.openPage(table.pageId) } }, w.openDatabase))
      : null);
}

function render() {
  root.replaceChildren(...[
    h("h1", {}, w.title),
    h("p", { class: "lead" }, w.lead),
    error ? h("div", { class: "error", role: "alert" }, error) : null,
    connectBox(),
    recentBox(),
    message ? h("p", { class: "status", role: "status" }, message) : null,
  ].filter(Boolean));
}

try {
  table = await app.table("clips");
  clips = (await table.find({ sort: [{ column: "받은 때", descending: true }], limit: 10 })).rows;
} catch (caught) {
  const text = String(caught?.message || caught || "");
  error = /organization|no_table/u.test(text) ? w.noOrganization : text;
}
render();
