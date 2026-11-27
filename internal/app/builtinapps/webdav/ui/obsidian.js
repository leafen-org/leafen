// The Obsidian card's screen: a vault synced to the drive, or a vault
// folder's ZIP, turned into a bundle and handed to Leafen.
import { connect } from "./leafen-app-sdk.js";
import { notesBundle } from "./notes-convert.js";
import { fill, strings } from "./obsidian-strings.js";
import { readZip, ZipError } from "./zip.js";

const app = await connect();
const text = strings(app.context.locale);
const $ = (id) => document.getElementById(id);
document.documentElement.dataset.theme = app.context.theme;
app.onTheme((theme) => { document.documentElement.dataset.theme = theme; });
$("pick").textContent = text.pick;
$("lead").textContent = text.lead;
$("go").textContent = text.go;
const state = (message, tone) => {
  $("state").textContent = message;
  if (tone) $("state").dataset.tone = tone;
  else delete $("state").dataset.tone;
};

/** A vault the drive keeps: its folder pages and their file blocks. */
async function driveVault(vault) {
  const files = [];
  const walk = async (pageId, prefix) => {
    let cursor = "";
    for (;;) {
      const page = await app.api("GET", `/pages/${encodeURIComponent(pageId)}/content-graph${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`);
      for (const item of page.body?.items ?? []) {
        const content = item.kind === "block" ? item.value?.content : null;
        if (!content?.fileId) continue;
        files.push({ path: `${prefix}${content.name}`, size: content.size ?? 0, read: async () => {
          const answer = await app.api("GET", `/files/${encodeURIComponent(content.fileId)}`);
          if (answer.status !== 200 || !(answer.body instanceof ArrayBuffer)) throw new Error(`file ${content.name}: ${answer.status}`);
          return new Uint8Array(answer.body);
        } });
      }
      state(fill(text.listing, { count: files.length }));
      if (!page.body?.hasMore || !page.body?.nextCursor) break;
      cursor = page.body.nextCursor;
    }
    // A folder's pages a window at a time to the last: the first thousand
    // taken for all left the rest of a big folder out of the move.
    let after = "";
    for (;;) {
      const children = await app.api("GET", `/workspaces/${encodeURIComponent(vault.workspaceId)}/page-children?parent=${encodeURIComponent(pageId)}&limit=1000${after ? `&cursor=${encodeURIComponent(after)}` : ""}`);
      if (children.status !== 200) throw new Error(`pages under ${prefix || vault.name}: ${children.status}`);
      for (const child of children.body?.pages ?? []) {
        if (child.title === ".obsidian" || child.title === ".trash") continue;
        await walk(child.id, `${prefix}${child.title}/`);
      }
      const next = children.body?.nextCursor ?? "";
      if (!next || next === after) break;
      after = next;
    }
  };
  await walk(vault.pageId, "");
  return { name: vault.name, files };
}

/** A vault folder's ZIP: its one top folder, if it has one, is the vault. */
async function zipVault(file) {
  const zip = await readZip(file);
  const tops = new Set(zip.names.map((name) => name.split("/")[0]));
  const nested = tops.size === 1 && zip.names.every((name) => name.includes("/"));
  const prefix = nested ? `${[...tops][0]}/` : "";
  return {
    name: nested ? [...tops][0] : file.name.replace(/\.zip$/iu, ""),
    files: zip.names.map((name) => ({ path: name.slice(prefix.length), size: zip.size(name), read: () => zip.bytes(name) })),
  };
}

let made = null;
async function prepare(load) {
  made = null;
  $("go").disabled = true;
  $("summary").hidden = true;
  try {
    const vault = await load();
    if (!vault.files.some((file) => file.path.endsWith(".md"))) throw new Error(text.noNotes);
    made = await notesBundle(vault, { labels: text, sourceId: `obsidian:${vault.name}`, progress: (done, total) => state(fill(text.reading, { done, total })) });
    $("summary-title").textContent = made.name;
    $("summary-counts").textContent = fill(text.counts, made.counts);
    $("summary-again").textContent = text.again;
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
    state(fill(text.failed, { reason: error instanceof ZipError && error.message === "not-zip" ? text.notZip : error.message }), "error");
  }
}

const choose = (button) => {
  for (const other of $("choices").children) other.setAttribute("aria-checked", String(other === button));
};
const vaults = await app.action("vaults").then((result) => result.output?.vaults ?? []).catch(() => []);
for (const vault of vaults.filter((item) => item.pageId)) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "choice";
  button.setAttribute("role", "radio");
  button.setAttribute("aria-checked", "false");
  button.dataset.vault = vault.name;
  button.textContent = fill(text.synced, { name: vault.name });
  button.addEventListener("click", () => { choose(button); void prepare(() => driveVault(vault)); });
  $("choices").append(button);
}
const upload = document.createElement("button");
upload.type = "button";
upload.className = "choice";
upload.setAttribute("role", "radio");
upload.setAttribute("aria-checked", "false");
upload.dataset.upload = "zip";
upload.textContent = `${text.upload} — ${text.choose}`;
upload.addEventListener("click", () => $("file").click());
$("choices").append(upload);
if (vaults.length === 0) state(text.none);
$("file").addEventListener("change", () => {
  const file = $("file").files?.[0];
  $("file").value = "";
  if (!file) return;
  choose(upload);
  upload.textContent = `${text.upload} — ${file.name}`;
  void prepare(() => zipVault(file));
});
$("go").addEventListener("click", async () => {
  if (!made) return;
  $("go").disabled = true;
  state(text.handing);
  try {
    await app.importBundle(made.blob);
    state(text.taken);
  } catch (error) {
    state(fill(text.failed, { reason: error instanceof ZipError && error.message === "not-zip" ? text.notZip : error.message }), "error");
    $("go").disabled = false;
  }
});
