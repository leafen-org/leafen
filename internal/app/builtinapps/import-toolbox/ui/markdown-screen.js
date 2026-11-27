// The Markdown tools card's screen: the person chooses a Markdown export —
// Bear's, Joplin's, Craft's, Roam's — as a ZIP, the toolbox turns it into a
// bundle and hands it to Leafen.
import { connect } from "./leafen-app-sdk.js";
import { markdownToolBundle } from "./markdown-tools.js";
import { fill, strings } from "./strings.js";
import { ZipError } from "./zip.js";

const app = await connect();
const text = strings(app.context.locale);
const $ = (id) => document.getElementById(id);
document.documentElement.dataset.theme = app.context.theme;
app.onTheme((theme) => { document.documentElement.dataset.theme = theme; });
$("pick-label").textContent = text.markdownPick;
$("choose").textContent = text.choose;
$("lead").textContent = text.markdownLead;
$("go").textContent = text.go;
const state = (message, tone) => {
  $("state").textContent = message;
  if (tone) $("state").dataset.tone = tone;
  else delete $("state").dataset.tone;
};

let made = null;
$("choose").addEventListener("click", () => $("file").click());
$("file").addEventListener("change", async () => {
  const file = $("file").files?.[0];
  $("file").value = "";
  if (!file) return;
  made = null;
  $("go").disabled = true;
  $("summary").hidden = true;
  $("choose").textContent = file.name;
  try {
    made = await markdownToolBundle(file, { labels: text, progress: (done, total) => state(fill(text.markdownReading, { done, total })) });
    $("summary-title").textContent = `${made.name} · ${text.markdownTool[made.tool]}`;
    $("summary-counts").textContent = fill(text.counts, made.counts);
    $("summary-again").textContent = text.markdownAgain;
    $("summary-again").hidden = !(app.context.importSource?.earlier ?? []).includes(made.sourceId);
    $("summary-losses").replaceChildren(...made.losses.map((loss) => {
      const item = document.createElement("li");
      item.textContent = `${loss.what} ${loss.count} — ${loss.why}`;
      return item;
    }));
    $("summary").hidden = false;
    $("go").disabled = false;
    state("");
  } catch (error) {
    const reason = error instanceof ZipError ? (error.message === "not-zip" ? text.notZip : error.message) : error.message;
    state(fill(text.failed, { reason }), "error");
  }
});
$("go").addEventListener("click", async () => {
  if (!made) return;
  $("go").disabled = true;
  $("choose").disabled = true;
  state(text.handing);
  try {
    await app.importBundle(made.blob);
    state(text.taken);
  } catch (error) {
    state(fill(text.failed, { reason: error.message }), "error");
    $("go").disabled = false;
    $("choose").disabled = false;
  }
});
