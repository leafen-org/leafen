// The Vault app's screen: the server address a person gives their
// Bitwarden app, preparing their vault, and what they can do with it
// from Leafen. Everything runs through the app's actions; the vault's
// contents never come here.
import { connect } from "./leafen-app-sdk.js";
import { applyTheme, element as h, wordsFor } from "./i18n.js";
import { httpsAddress, outsideAddressNote } from "./outside-address.js";

const app = await connect();
const w = wordsFor(app.context.locale);
applyTheme(app);
document.title = w.title;
const root = document.getElementById("app");
let vault = null;
let message = "";
let error = "";
let editing = false;
let confirming = false;
let busy = false;
let shared = null;
let overview = null;

function failure(caught) {
  const text = String(caught?.message || caught || "");
  const reason = text.includes(": ") ? text.slice(text.indexOf(": ") + 2) : text;
  // The worker says why in English, for agents too; the screen says it in the person's words.
  return w.reasons?.[reason] ?? reason;
}

function when(ms) {
  return new Date(ms).toLocaleString(app.context.locale, { dateStyle: "medium", timeStyle: "short" });
}

function day(ms) {
  return new Date(ms).toLocaleDateString(app.context.locale, { dateStyle: "medium" });
}

// The address is Leafen's own from outside, with the organization the vault is in.
function serverAddress() {
  const origin = new URL(app.context.homeUrl).origin;
  return `${origin}/app-endpoints/vault/bw/${vault.organization}`;
}

// Bitwarden's apps and its web vault take only an https address: one
// judgement for the address box and the web vault.
const secure = () => httpsAddress(serverAddress());

async function run(key, input = {}, done = "") {
  busy = true;
  error = "";
  message = "";
  render();
  try {
    const output = (await app.action(key, input)).output;
    if (key === "link" || key === "unlink") shared = output;
    else vault = output;
    message = done;
    editing = false;
    confirming = false;
    if (vault?.state === "active" && !shared) await loadShared();
  } catch (caught) {
    error = failure(caught);
  } finally {
    busy = false;
    render();
  }
}

// The organization's shared vaults, for its owners and people admins:
// their members and groups, never their items.
async function loadOverview() {
  try {
    overview = (await app.action("orgVaults")).output;
  } catch (caught) {
    error = failure(caught);
  }
}

function overviewLine(item) {
  const members = (item.members ?? []).map((member) => h("li", { "data-vault-overview-member": member.email },
    h("span", {}, member.email), h("span", { class: "meta" }, " · " + (w.overviewRoles[member.role] ?? member.role)
      + (member.state === "confirmed" ? "" : " · " + (w.overviewStates[member.state] ?? member.state)))));
  return h("li", { "data-vault-overview": item.id },
    h("div", { class: "shared-head" }, h("strong", {}, item.name), h("span", { class: "meta" }, w.overviewItems(item.items ?? 0))),
    members.length ? h("ul", { class: "members" }, members) : h("p", { class: "meta" }, w.overviewNoMembers),
    (item.groups ?? []).length ? h("div", { class: "pills" }, item.groups.map((group) => h("span", { class: "pill" }, group.name))) : null);
}

function overviewBox() {
  const vaults = overview?.vaults ?? [];
  return h("section", { class: "box", "data-vault-overview-box": true },
    h("div", { class: "box-head" }, h("h2", {}, w.overview, h("span", { class: "badge" }, w.overviewBadge))),
    vaults.length ? h("ul", { class: "shared" }, vaults.map(overviewLine)) : h("p", { class: "hint empty" }, w.overviewNone),
    h("p", { class: "hint" }, w.overviewNote));
}

async function loadShared() {
  try {
    shared = (await app.action("shared")).output;
  } catch (caught) {
    error = failure(caught);
  }
}

// A shared vault's line: its name, the person's place, and for its owners
// and administrators who waits and the groups it follows. Who waits is a
// fact with where to act, so it stays in the line's grey, as "waiting" does
// for the person's own place and in the organization's overview: the app's
// notes are grey and it has no warning colour.
function sharedLine(item, groups) {
  const role = item.state === "confirmed" ? w.roles[item.role] ?? w.roles[2] : w.waitingMe;
  const pills = (item.groups ?? []).map((group) => h("span", { class: "pill" }, group.name,
    item.manages ? h("button", { class: "pill-x", type: "button", "aria-label": w.unlink(group.name), disabled: busy,
      on: { click: () => void run("unlink", { vault: item.id, link: group.id }, w.unlinked) } }, "×") : null));
  let picker = null;
  const linkedNames = new Set((item.groups ?? []).map((group) => group.name));
  const unlinked = groups.filter((group) => !linkedNames.has(group.name));
  if (item.manages && unlinked.length) {
    const select = h("select", { "aria-label": w.chooseGroup },
      h("option", { value: "" }, w.chooseGroup),
      unlinked.map((group) => h("option", { value: group.id }, group.name)));
    picker = h("div", { class: "actions" }, select,
      h("button", { class: "secondary", type: "button", disabled: busy, "data-vault-link": true,
        on: { click: () => { if (select.value) void run("link", { vault: item.id, group: select.value }, w.linked); } } }, w.link));
  }
  return h("li", { "data-vault-shared": item.id },
    h("div", { class: "shared-head" }, h("strong", {}, item.name), h("span", { class: "meta" }, role)),
    item.manages ? h("p", { class: "meta" }, w.members(item.members ?? 0),
      item.waiting ? h("span", { "data-vault-waiting": item.waiting }, " · " + w.waiting(item.waiting)) : null) : null,
    pills.length ? h("div", { class: "pills" }, pills) : null,
    picker);
}

// The web vault Leafen's Vault app serves at the server address, where
// shared vaults are made and their people confirmed.
const WEB_VAULT_VERSION = "2026.7.0";

// Without https the address box above says what to do; this says only that the web vault waits for it too.
function webVaultAction() {
  if (!secure()) {
    return h("p", { class: "warning", "data-vault-web-https": true }, w.webVaultHttps);
  }
  return h("button", { class: "secondary", type: "button", "data-vault-web": true,
    on: { click: () => void app.openLink(`${serverAddress()}/`) } }, w.webVault);
}

function sharedBox() {
  const vaults = shared?.vaults ?? [];
  const action = webVaultAction();
  return h("section", { class: "box", "data-vault-shared-box": true },
    h("div", { class: "box-head" }, h("h2", {}, w.shared, h("span", { class: "badge" }, w.beta)), action.tagName === "BUTTON" ? action : null),
    action.tagName === "BUTTON" ? null : action,
    vaults.length
      ? h("ul", { class: "shared" }, vaults.map((item) => sharedLine(item, shared.groups ?? [])))
      : h("p", { class: "hint empty" }, w.noShared),
    h("p", { class: "hint" }, w.sharedNote),
    h("p", { class: "fine" }, w.webVaultNotice(WEB_VAULT_VERSION),
      h("button", { class: "text small", type: "button", on: { click: () => void app.openLink(`${serverAddress()}/leafen/SOURCE.md`) } }, w.source)));
}

function addressBox() {
  const field = h("input", { readonly: true, value: serverAddress(), "aria-label": w.server, "data-vault-address": true });
  const copy = async () => {
    field.select();
    try {
      await navigator.clipboard.writeText(field.value);
    } catch {
      document.execCommand("copy");
    }
    message = w.copied;
    render();
  };
  return h("section", { class: "box", "data-vault-address-box": secure() ? "https" : "http" },
    h("h2", {}, w.server),
    h("div", { class: "address" }, field, h("button", { class: "secondary", type: "button", on: { click: copy } }, w.copy)),
    secure() ? h("p", { class: "hint" }, w.serverHint) : outsideAddressNote(app, h, w.serverHttps, { "data-vault-address-https": true }));
}

function prepareForm() {
  const email = h("input", { type: "email", required: true, maxlength: 254, autocomplete: "email", value: vault.email ?? "", "data-vault-email": true });
  const submit = (event) => {
    event.preventDefault();
    void run("prepare", { email: email.value.trim() });
  };
  return h("section", { class: "box" },
    h("form", { on: { submit } },
      h("label", {}, w.email, email),
      h("div", { class: "actions" }, h("button", { class: "primary", type: "submit", disabled: busy, "data-vault-prepare": true }, w.prepare))),
    h("p", { class: "hint" }, w.masterNote));
}

function pendingBox() {
  return h("section", { class: "box", "data-vault-state": "pending" },
    h("h2", {}, w.prepared(vault.email, when(vault.expires))),
    h("ol", { class: "steps" }, w.steps.map((step) => h("li", {}, step))),
    h("div", { class: "actions" }, h("button", { class: "text", type: "button", on: { click: () => { editing = true; render(); } } }, w.otherEmail)));
}

function activeBox() {
  const devices = vault.devices ?? [];
  const resetBox = confirming
    ? h("div", { class: "confirm", role: "alertdialog" },
      h("p", {}, w.resetWarning),
      h("div", { class: "actions" },
        h("button", { class: "danger", type: "button", disabled: busy, "data-vault-reset-confirm": true, on: { click: () => void run("reset") } }, w.resetConfirm),
        h("button", { class: "secondary", type: "button", on: { click: () => { confirming = false; render(); } } }, w.keep)))
    : null;
  return h("section", { class: "box", "data-vault-state": "active" },
    h("h2", {}, w.made(vault.email, day(vault.created), vault.items ?? 0)),
    h("p", { class: "meta" }, w.devices),
    devices.length
      ? h("ul", { class: "devices" }, devices.map((device) => h("li", {}, h("span", {}, device.name || "—"), h("span", {}, w.seen(when(device.seen))))))
      : h("p", { class: "hint empty" }, w.noDevices),
    h("div", { class: "actions" },
      h("button", { class: "secondary", type: "button", disabled: busy, on: { click: () => void run("signout", {}, w.signedOut) } }, w.signout),
      h("button", { class: "danger-text", type: "button", on: { click: () => { confirming = true; render(); } } }, w.reset)),
    resetBox);
}

function render() {
  const state = vault?.state;
  root.replaceChildren(...[
    h("h1", {}, w.title),
    h("p", { class: "lead" }, w.lead),
    error ? h("div", { class: "error", role: "alert" }, error) : null,
    vault ? addressBox() : null,
    state === "active" ? activeBox() : state === "pending" && !editing ? pendingBox() : vault ? prepareForm() : null,
    state === "active" && shared ? sharedBox() : null,
    overview ? overviewBox() : null,
    message ? h("p", { class: "status", role: "status" }, message) : null,
    h("p", { class: "footnote" }, w.footnote),
  ].filter(Boolean));
}

try {
  vault = (await app.action("status")).output;
  if (vault?.state === "active") await loadShared();
  if (vault?.oversees) await loadOverview();
} catch (caught) {
  error = /organization/u.test(String(caught?.message)) ? w.noOrganization : failure(caught);
}
render();
