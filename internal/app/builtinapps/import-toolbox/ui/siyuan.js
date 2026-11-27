// The SiYuan card's screen: the person chooses a ZIP of their workspace's
// data folder or a notebook's .sy.zip export, the toolbox turns it into a
// bundle and hands it to Leafen.
import { connect } from "./leafen-app-sdk.js";
import { siyuanBundle } from "./siyuan-convert.js";
import { fill, strings } from "./strings.js";
import { ZipError } from "./zip.js";

const app = await connect();
const text = strings(app.context.locale);
const $ = (id) => document.getElementById(id);
document.documentElement.dataset.theme = app.context.theme;
app.onTheme((theme) => { document.documentElement.dataset.theme = theme; });
$("pick-label").textContent = text.siyuanPick;
$("choose").textContent = text.choose;
$("lead").textContent = text.siyuanLead;
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
    made = await siyuanBundle(file, { labels: text, progress: (done, total) => state(fill(text.siyuanReading, { done, total })) });
    $("summary-title").textContent = "SiYuan";
    $("summary-counts").textContent = fill(text.siyuanCounts, { notebooks: made.notebooks, ...made.counts });
    $("summary-again").textContent = text.siyuanAgain;
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
