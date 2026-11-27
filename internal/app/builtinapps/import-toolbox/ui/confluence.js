// The Confluence card's screen, as Notion's Confluence window has it: from
// the person's account through the API (their site, then a space), or from a
// space's HTML export. Either way the toolbox makes a bundle and hands it to
// Leafen.
import { connect } from "./leafen-app-sdk.js";
import { ConfluenceError, ConfluenceSite, confluenceSpaceBundle, siteOrigin } from "./confluence-api.js";
import { confluenceBundle } from "./confluence-convert.js";
import { fill, strings } from "./strings.js";
import { ZipError } from "./zip.js";

const app = await connect();
const text = strings(app.context.locale);
const $ = (id) => document.getElementById(id);
document.documentElement.dataset.theme = app.context.theme;
app.onTheme((theme) => { document.documentElement.dataset.theme = theme; });
$("tab-api").textContent = text.tabApi;
$("tab-file").textContent = text.tabFile;
$("site-label").textContent = text.siteLabel;
$("site").placeholder = text.sitePlaceholder;
$("load").textContent = text.load;
$("api-lead").textContent = text.apiLead;
$("pick-label").textContent = text.pick;
$("choose").textContent = text.choose;
$("lead").textContent = text.confluenceLead;
$("go").textContent = text.go;
const state = (message, tone) => {
  $("state").textContent = message;
  if (tone) $("state").dataset.tone = tone;
  else delete $("state").dataset.tone;
};
const size = (bytes) => bytes < 1024 * 1024 ? `${Math.max(1, Math.round(bytes / 1024))} KB` : `${(bytes / 1024 / 1024).toFixed(1)} MB`;

// The tabs: the API first, as Notion's.
const show = (api) => {
  $("tab-api").setAttribute("aria-selected", String(api));
  $("tab-file").setAttribute("aria-selected", String(!api));
  $("api").hidden = !api;
  $("export").hidden = api;
};
$("tab-api").addEventListener("click", () => show(true));
$("tab-file").addEventListener("click", () => show(false));

let made = null;
const reset = () => {
  made = null;
  $("go").disabled = true;
  $("summary").hidden = true;
};
const summarize = (bundle) => {
  made = bundle;
  $("summary-title").textContent = fill(text.summary, { space: bundle.space });
  $("summary-counts").textContent = fill(text.counts, bundle.counts);
  $("summary-again").textContent = text.again;
  $("summary-again").hidden = !(app.context.importSource?.earlier ?? []).includes(bundle.sourceId);
  $("summary-losses").replaceChildren(...bundle.losses.map((loss) => {
    const item = document.createElement("li");
    item.textContent = `${loss.what} ${loss.count} — ${loss.why}`;
    return item;
  }));
  $("summary").hidden = false;
  $("go").disabled = false;
  state("");
};

// The account: the email and API token are Leafen's to keep.
let given = false;
const account = async (list) => {
  const credentials = list ?? await app.credentials().catch(() => ({ enabled: false, credentials: [] }));
  given = Boolean(credentials.credentials?.find((item) => item.key === "confluence")?.connected);
  $("account").textContent = !credentials.enabled ? text.credentialsOff : given ? text.connected : text.notConnected;
  $("connect").textContent = given ? text.disconnect : text.connect;
  $("connect").hidden = !credentials.enabled;
};
$("connect").addEventListener("click", async () => {
  try {
    await account(given ? await app.disconnectCredential("confluence") : await app.connectCredential("confluence"));
  } catch (error) {
    state(fill(text.failed, { reason: error.message }), "error");
  }
});
await account();
try {
  $("site").value = localStorage.getItem("confluence-site") ?? "";
} catch {
  // A frame without storage starts empty.
}

let site = null;
const apiFailure = (error) => {
  const code = error instanceof ConfluenceError ? error.code : "";
  const detail = error.message.replace(/^\w+: ?/u, "");
  return code === "auth" ? text.authFailed : code === "site" ? `${text.siteFailed}${detail ? ` (${detail})` : ""}`
    : code === "credential" ? text.credentialsMissing
    : fill(text.failed, { reason: error.message });
};
$("load").addEventListener("click", async () => {
  reset();
  $("spaces").hidden = true;
  const origin = siteOrigin($("site").value);
  if (!origin) return state(text.siteMissing, "error");
  try {
    localStorage.setItem("confluence-site", $("site").value.trim());
  } catch {
    // Remembering the site is a convenience.
  }
  $("load").disabled = true;
  state(text.loadingSpaces);
  try {
    site = new ConfluenceSite(app, origin);
    await site.connect();
    const spaces = await site.spaces();
    $("spaces").replaceChildren(...spaces.map((space) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "choice";
      button.setAttribute("role", "radio");
      button.setAttribute("aria-checked", "false");
      button.dataset.space = space.key;
      button.textContent = fill(text.spaceLine, space);
      button.addEventListener("click", () => readSpace(space, button));
      return button;
    }));
    $("spaces").hidden = spaces.length === 0;
    state(spaces.length ? "" : text.noSpaces);
  } catch (error) {
    state(apiFailure(error), "error");
  } finally {
    $("load").disabled = false;
  }
});

async function readSpace(space, button) {
  reset();
  for (const other of $("spaces").children) other.setAttribute("aria-checked", String(other === button));
  for (const other of $("spaces").children) other.disabled = true;
  try {
    summarize(await confluenceSpaceBundle(site, space, {
      labels: text, progress: (done, total) => state(fill(text.readingSpace, { done, total })),
    }));
  } catch (error) {
    state(apiFailure(error), "error");
  } finally {
    for (const other of $("spaces").children) other.disabled = false;
  }
}

// The export: a space's HTML export, as a ZIP.
$("choose").addEventListener("click", () => $("file").click());
$("file").addEventListener("change", async () => {
  const file = $("file").files?.[0];
  $("file").value = "";
  if (!file) return;
  reset();
  $("choose").textContent = fill(text.chosen, { name: file.name, size: size(file.size) });
  try {
    summarize(await confluenceBundle(file, {
      labels: text,
      progress: (done, total) => state(fill(text.reading, { done, total })),
    }));
  } catch (error) {
    const reason = error instanceof ZipError ? (error.message === "not-zip" ? text.notZip : error.message) : error.message;
    state(error.message === text.noIndex ? text.noIndex : fill(text.failed, { reason }), "error");
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
