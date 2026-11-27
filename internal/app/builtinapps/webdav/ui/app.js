// The Drive app's screen: how to connect Finder, Windows Explorer or
// another WebDAV client to the app's address, and what they show. The
// drive itself is the app's endpoint; the screen only guides.
import { connect } from "./leafen-app-sdk.js";
import { applyTheme, element as h, wordsFor } from "./i18n.js";
import { httpsAddress, outsideAddressNote } from "./outside-address.js";

const app = await connect();
const w = wordsFor(app.context.locale);
applyTheme(app);
document.title = w.title;
const root = document.getElementById("app");
const origin = new URL(app.context.homeUrl).origin;
const address = `${origin}/app-endpoints/webdav/dav/`;
// Remotely Save makes its folder where the address points: the person's own.
const obsidianAddress = `${address}Files/My%20files/`;
let copied = "";

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
  copied = text;
  render();
  setTimeout(() => { if (copied === text) { copied = ""; render(); } }, 1600);
}

const copyButton = (text) => h("button", { class: "secondary", type: "button", on: { click: () => void copy(text) } }, copied === text ? w.copied : w.copy);

const row = (title, description, control) => h("div", { class: "row" },
  h("div", { class: "row-text" }, h("div", { class: "row-title" }, title), h("div", { class: "row-desc" }, description)),
  control ? h("div", { class: "row-control" }, control) : null);

function render() {
  root.replaceChildren(
    h("h1", {}, w.title),
    h("p", { class: "lead" }, w.lead),
    h("section", { class: "box", "data-drive-connect": true },
      h("h2", {}, w.connect),
      row(w.serverAddress, w.serverAddressHint),
      h("div", { class: "address" },
        h("input", { readonly: true, value: address, "aria-label": w.serverAddress, "data-drive-address": true }),
        copyButton(address)),
      row(w.appPassword, w.appPasswordHint,
        h("button", { class: "secondary", type: "button", on: { click: () => void app.openLink(`${origin}/apps#app-passwords`) } }, w.openApps)),
      h("ul", { class: "ways" }, h("li", {}, w.mac), h("li", {}, w.windows)),
      httpsAddress(origin) ? null : outsideAddressNote(app, h, w.notHttps, { "data-drive-not-https": true }),
      httpsAddress(origin) ? null : h("p", { class: "hint" }, w.finderInsecure)),
    h("section", { class: "box" },
      h("h2", {}, w.seen),
      row(w.pagesTitle, w.pages),
      row(w.filesTitle, w.files)),
    h("section", { class: "box" },
      h("h2", {}, w.sync),
      h("details", { "data-drive-obsidian": true },
        h("summary", {}, w.obsidian),
        h("ol", { class: "steps" }, w.obsidianSteps.map((step) => h("li", {}, step))),
        h("div", { class: "address" },
          h("input", { readonly: true, value: obsidianAddress, "aria-label": w.obsidian }),
          copyButton(obsidianAddress))),
      h("details", { "data-drive-rclone": true },
        h("summary", {}, w.rcloneTitle),
        h("p", { class: "hint" }, w.rclone))),
  );
}

render();
