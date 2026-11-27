import { connect } from "./leafen-app-sdk.js";
import { strings } from "./strings.js";

const app = await connect();
const text = strings(app.context.locale);
document.documentElement.dataset.theme = app.context.theme;
app.onTheme((theme) => { document.documentElement.dataset.theme = theme; });
document.getElementById("title").textContent = text.title;
document.getElementById("lead").textContent = text.lead;
const how = document.getElementById("how");
how.textContent = text.how;
how.addEventListener("click", () => void app.openLink(`${new URL(app.context.homeUrl).origin}/settings?section=import`));
for (const tool of text.tools) {
  const item = document.createElement("li");
  item.textContent = tool;
  document.getElementById("tools").append(item);
}
