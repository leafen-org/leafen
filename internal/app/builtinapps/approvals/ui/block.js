// The Approvals block in a page: how many requests wait for the person and
// how many of theirs are open, and a way into the app.
import { connect } from "./leafen-app-sdk.js";
import { applyTheme, element as h, wordsFor } from "./i18n.js";

const app = await connect();
const w = wordsFor(app.context.locale);
applyTheme(app);
const root = document.getElementById("app");
try {
  const { output } = await app.action("overview");
  const open = output.mine.filter((item) => item.status === "대기").length;
  root.replaceChildren(
    h("section", { class: "counts" },
      h("div", { class: "count", "data-block-waiting": true }, h("b", {}, String(output.waiting.length)), h("span", {}, w.blockWaiting)),
      h("div", { class: "count", "data-block-open": true }, h("b", {}, String(open)), h("span", {}, w.blockMine))),
    h("button", { class: "secondary", type: "button", on: { click: () => void app.navigate("waiting") } }, w.open));
} catch {
  root.replaceChildren(h("p", { class: "status" }, w.failed));
}
