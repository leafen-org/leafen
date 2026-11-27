// The Mail app's screen: the inbox beside the message being read (one at a
// time on a phone) and a new message. Everything goes through the app's
// actions, run as the person. Their mailbox is on the organization's mail
// server — the account they give Leafen in its own window — or in Gmail,
// with an app password given the same way or a Google sign-in through
// Leafen; none reaches the screen.
import { connect } from "./leafen-app-sdk.js";
import { applyTheme, element as h, wordsFor } from "./i18n.js";

const app = await connect();
const w = wordsFor(app.context.locale);
const locale = app.context.locale || "ko";
applyTheme(app);
document.title = w.title;
const root = document.getElementById("app");

let state = "loading"; // loading, choose, ready
let kind = ""; // imap, gmail-imap, gmail
let options = { imap: false, imapOff: false, "gmail-imap": false, gmail: false };
let problem = "";
let messages = [];
let total = 0;
let open = null;
let view = "list"; // list, read, compose
let draft = null;
let notice = "";
let formError = "";
let busy = false;

const errorText = (code) => w.errors[code] ?? w.errors.server;

// Which mailbox the person chose, and what each way stands at.
async function account() {
  const [list, signIns, chosen] = await Promise.all([
    app.credentials(), app.connectedAccounts().catch(() => ({ accounts: [] })), app.storage.get("account").catch(() => null)]);
  options = {
    imap: list.credentials.some((credential) => credential.key === "mail" && credential.connected), imapOff: !list.enabled,
    "gmail-imap": list.credentials.some((credential) => credential.key === "gmail" && credential.connected),
    gmail: signIns.accounts.some((account) => account.key === "google" && account.connected),
  };
  kind = ["imap", "gmail-imap", "gmail"].includes(chosen?.kind) ? chosen.kind
    : ["imap", "gmail-imap", "gmail"].find((each) => options[each]) ?? "";
  return kind && options[kind] ? "ready" : "choose";
}

async function loadInbox() {
  const { output } = await app.action("inbox");
  if (output?.error === "credentials") {
    state = "choose";
    return;
  }
  problem = output?.error ?? "";
  if (!problem) {
    messages = output.messages ?? [];
    total = output.total ?? messages.length;
  }
}

async function start() {
  state = "loading";
  render();
  try {
    state = await account();
    if (state === "ready") await loadInbox();
  } catch {
    state = "ready";
    problem = "server";
  }
  render();
}

// Connects the company account or Gmail's app password in Leafen's window,
// or Gmail through Google's page, and remembers the choice.
async function connectWith(next) {
  notice = "";
  try {
    if (next === "gmail") await app.connectAccount("google");
    else await app.connectCredential(next === "gmail-imap" ? "gmail" : "mail");
    await app.storage.set("account", { kind: next });
  } catch (error) {
    // Off, not ready, or the person closed the window: the screen says where they stand.
    if (String(error?.code || error?.message || error).includes("OAUTH_NOT_READY")) notice = w.gmailNotReady;
  }
  messages = [];
  open = null;
  view = "list";
  await start();
}

async function choose() {
  state = "choose";
  render();
}

async function disconnect() {
  if (kind === "gmail") await app.disconnectAccount("google").catch(() => undefined);
  else await app.disconnectCredential(kind === "gmail-imap" ? "gmail" : "mail").catch(() => undefined);
  await app.storage.remove("account").catch(() => undefined);
  messages = [];
  open = null;
  view = "list";
  await start();
}

async function read(id) {
  view = "read";
  open = { id, loading: true };
  notice = "";
  render();
  try {
    const { output } = await app.action("read", { id });
    if (output?.error) {
      open = null;
      view = "list";
      notice = errorText(output.error);
    } else {
      open = output;
      messages = messages.map((message) => (message.id === id ? { ...message, seen: true } : message));
    }
  } catch {
    open = null;
    view = "list";
    notice = w.errors.server;
  }
  render();
}

function compose(reply) {
  const message = reply?.message;
  draft = message
    ? {
      to: message.from?.address ?? "", cc: "", showCc: false, body: "", inReplyTo: message.messageId || "",
      subject: /^re:/iu.test(message.subject || "") ? message.subject : `Re: ${message.subject || ""}`,
    }
    : { to: "", cc: "", showCc: false, subject: "", body: "", inReplyTo: "" };
  formError = "";
  view = "compose";
  render();
}

// What one send carries: Leafen takes an action's input up to 1 MB, and a
// message through Gmail's API goes base64-encoded (a third larger) in a
// request of 1 MB, so about 700 KB there. A long message is told as it nears
// that and kept from sending past it, with the reason.
const inputMax = () => (kind === "gmail" ? 700 : 1024) * 1024;

function sendInput() {
  const input = { to: draft.to.trim(), subject: draft.subject.trim(), body: draft.body };
  if (draft.cc.trim()) input.cc = draft.cc.trim();
  if (draft.inReplyTo) input.inReplyTo = draft.inReplyTo;
  return input;
}

const inputBytes = () => new TextEncoder().encode(JSON.stringify({ input: sendInput() })).length;

async function send() {
  if (inputBytes() > inputMax()) {
    formError = w.tooLong(Math.round(inputMax() / 1024));
    render();
    return;
  }
  busy = true;
  formError = "";
  render();
  const input = sendInput();
  try {
    const { output } = await app.action("send", input);
    if (output?.error) {
      formError = errorText(output.error);
    } else {
      draft = null;
      view = open && !open.loading ? "read" : "list";
      notice = w.sent;
    }
  } catch {
    formError = w.errors.declined;
  }
  busy = false;
  render();
}

function shortDate(iso) {
  if (!iso) return "";
  const date = new Date(iso);
  const now = new Date();
  if (date.toDateString() === now.toDateString()) return date.toLocaleTimeString(locale, { hour: "numeric", minute: "2-digit" });
  if (date.getFullYear() === now.getFullYear()) return date.toLocaleDateString(locale, { month: "short", day: "numeric" });
  return date.toLocaleDateString(locale, { year: "numeric", month: "short", day: "numeric" });
}

const who = (person) => person?.name || person?.address || "—";
const full = (person) => (person?.name && person?.address ? `${person.name} <${person.address}>` : who(person));

function clip(label) {
  const space = "http://www.w3.org/2000/svg";
  const icon = document.createElementNS(space, "svg");
  icon.setAttribute("class", "clip");
  icon.setAttribute("viewBox", "0 0 16 16");
  if (label) {
    icon.setAttribute("role", "img");
    icon.setAttribute("aria-label", label);
  } else {
    icon.setAttribute("aria-hidden", "true");
  }
  const path = document.createElementNS(space, "path");
  path.setAttribute("d", "M10.5 4.5 5.8 9.2a1.3 1.3 0 0 0 1.8 1.8l5-5a2.6 2.6 0 0 0-3.7-3.7L3.8 7.4a3.9 3.9 0 0 0 5.5 5.5l4.2-4.2");
  icon.append(path);
  return icon;
}

// Links in a message open through Leafen, which asks first.
function linked(text) {
  const parts = [];
  let last = 0;
  for (const match of text.matchAll(/https?:\/\/[^\s<>"')\]]+/gu)) {
    parts.push(text.slice(last, match.index));
    const url = match[0];
    parts.push(h("a", { href: "#", on: { click: (event) => { event.preventDefault(); void app.openLink(url); } } }, url));
    last = match.index + url.length;
  }
  parts.push(text.slice(last));
  return parts;
}

function row(message) {
  return h("li", {},
    h("button", {
      type: "button", class: "row", "data-mail-row": message.id, "data-unread": !message.seen, "aria-current": open?.id === message.id ? "true" : false,
      on: { click: () => void read(message.id) },
    },
    h("span", { class: "line" },
      message.seen ? null : h("span", { class: "dot", "aria-label": w.unread, role: "img" }),
      h("span", { class: "from" }, who(message.from)),
      h("span", { class: "date" }, shortDate(message.date))),
    h("span", { class: "line" },
      h("span", { class: "subject" }, message.subject || w.noSubject),
      message.attachment ? clip(w.attachment) : null)));
}

function listPane() {
  return h("section", { class: "list", "aria-label": w.inbox, "data-mail-list": true },
    h("div", { class: "list-head" }, h("h2", {}, w.inbox), total > messages.length ? h("span", { class: "meta" }, w.total(total)) : null),
    messages.length ? h("ul", {}, messages.map(row)) : h("p", { class: "empty" }, w.empty),
    h("div", { class: "account" },
      h("span", { class: "meta", "data-mail-kind": kind }, kind === "imap" ? w.company : w.gmail),
      h("button", { type: "button", class: "plain", "data-mail-account": true, on: { click: () => void choose() } }, w.account),
      h("button", { type: "button", class: "plain", "data-mail-disconnect": true, on: { click: () => void disconnect() } }, w.disconnect)));
}

function readerPane() {
  if (!open) return h("section", { class: "reader placeholder" }, h("p", { class: "empty" }, w.pick));
  if (open.loading) return h("section", { class: "reader" }, h("p", { class: "status", role: "status" }, w.loading));
  const message = { ...open.message, to: open.message?.to ?? [], cc: open.message?.cc ?? [], attachments: open.message?.attachments ?? [] };
  return h("section", { class: "reader" },
    h("article", { class: "message", "data-mail-message": open.id },
      h("div", { class: "message-tools" },
        h("button", { type: "button", class: "secondary back", on: { click: () => { view = "list"; render(); } } }, w.back),
        h("button", { type: "button", class: "secondary", "data-mail-reply": true, on: { click: () => compose(open) } }, w.reply)),
      h("h2", {}, message.subject || w.noSubject),
      h("dl", { class: "meta" },
        h("dt", {}, w.from), h("dd", {}, full(message.from)),
        message.to.length ? [h("dt", {}, w.toLine), h("dd", {}, message.to.map(full).join(", "))] : null,
        message.cc.length ? [h("dt", {}, w.ccLine), h("dd", {}, message.cc.map(full).join(", "))] : null),
      message.date ? h("p", { class: "when" }, new Date(message.date).toLocaleString(locale)) : null,
      open.truncated ? h("p", { class: "status" }, w.truncated) : null,
      h("div", { class: "text", "data-mail-text": true }, linked(message.text || "")),
      message.attachments.length
        ? h("div", { class: "attachments" }, h("h3", {}, w.attachments),
          h("ul", {}, message.attachments.map((item) => h("li", {}, clip(""), `${item.name} · ${Math.max(1, Math.round(item.size / 1024))} KB`))))
        : null));
}

function composePane() {
  const size = h("p", { class: "meta", "data-mail-size": true, "aria-live": "polite" });
  const sendButton = h("button", { type: "submit", class: "primary", disabled: busy, "data-mail-send": true }, busy ? w.sending : w.send);
  // Quiet until the message nears what one send takes.
  const measure = () => {
    const bytes = inputBytes();
    const limit = inputMax();
    const over = bytes > limit;
    size.textContent = over ? w.tooLong(Math.round(limit / 1024)) : bytes >= limit * 0.9 ? w.nearlyLong(Math.ceil(bytes / 1024), Math.round(limit / 1024)) : "";
    size.className = over ? "error" : "meta";
    sendButton.disabled = busy || over;
  };
  const field = (key, label, attributes = {}) => h("label", {}, label,
    h(key === "body" ? "textarea" : "input", {
      name: key, value: key === "body" ? undefined : draft[key], "data-mail-field": key, ...attributes,
      on: { input: (event) => { draft[key] = event.target.value; measure(); } },
    }, key === "body" ? draft.body : null));
  const form = h("form", { class: "compose", "data-mail-compose": true },
    h("h2", {}, draft.inReplyTo ? w.reply : w.compose),
    field("to", w.to, { required: true, autocomplete: "email", placeholder: "name@example.com" }),
    draft.showCc ? field("cc", w.cc, { autocomplete: "email" })
      : h("button", { type: "button", class: "plain", on: { click: () => { draft.showCc = true; render(); } } }, w.addCc),
    field("subject", w.subject, { required: true }),
    field("body", w.body, { rows: 12 }),
    size,
    formError ? h("p", { class: "error", role: "alert" }, formError) : null,
    h("div", { class: "form-tools" },
      h("button", { type: "button", class: "secondary", disabled: busy, on: { click: () => { draft = null; view = open ? "read" : "list"; render(); } } }, w.cancel),
      sendButton));
  form.addEventListener("submit", (event) => { event.preventDefault(); void send(); });
  measure();
  return h("section", { class: "reader" }, form);
}

function render() {
  const head = h("header", { class: "top" }, h("h1", {}, w.title),
    state === "ready" && !problem
      ? h("div", { class: "tools" },
        h("button", { type: "button", class: "primary", "data-mail-new": true, on: { click: () => compose(null) } }, w.compose),
        h("button", { type: "button", class: "secondary", "data-mail-refresh": true, on: { click: () => void start() } }, w.refresh))
      : null);
  let content;
  if (state === "loading") {
    content = h("p", { class: "status", role: "status" }, w.loading);
  } else if (state === "choose") {
    content = h("div", { class: "choices", "data-mail-choose": true },
      h("section", { class: "gate", "data-mail-connect": true }, h("h2", {}, w.connectTitle), h("p", {}, w.connectLead),
        options.imapOff ? h("p", { class: "status", "data-mail-off": true }, w.off)
          : h("button", { type: "button", class: options.imap ? "secondary" : "primary", "data-mail-give": true, on: { click: () => void connectWith("imap") } },
            options.imap ? w.useCompany : w.connect)),
      h("section", { class: "gate", "data-mail-gmail": true }, h("h2", {}, w.gmailTitle), h("p", {}, w.gmailLead),
        h("p", {}, h("a", { href: "#", "data-mail-app-passwords": true,
          on: { click: (event) => { event.preventDefault(); void app.openLink("https://myaccount.google.com/apppasswords"); } } }, w.gmailAppPasswords)),
        options.imapOff ? h("p", { class: "status", "data-mail-off": true }, w.off)
          : h("button", { type: "button", class: options["gmail-imap"] ? "secondary" : "primary", "data-mail-give-gmail": true,
            on: { click: () => void connectWith("gmail-imap") } }, options["gmail-imap"] ? w.useGmail : w.connectGmail),
        h("p", { class: "meta aside" }, w.gmailSignInLead),
        h("button", { type: "button", class: "plain", "data-mail-gmail-sign-in": true, on: { click: () => void connectWith("gmail") } },
          options.gmail ? w.useGmailSignIn : w.gmailSignIn)));
  } else if (problem) {
    content = h("section", { class: "gate", "data-mail-problem": problem }, h("p", {}, errorText(problem)),
      h("div", { class: "form-tools start" },
        problem === "login" ? h("button", { type: "button", class: "primary", on: { click: () => void connectWith(kind || "imap") } }, w.again) : null,
        h("button", { type: "button", class: "secondary", on: { click: () => void choose() } }, w.account),
        h("button", { type: "button", class: "secondary", on: { click: () => void start() } }, w.refresh)));
  } else {
    content = h("div", { class: "panes", "data-view": view }, listPane(), view === "compose" ? composePane() : readerPane());
  }
  root.replaceChildren(...[head, notice ? h("p", { class: "status", role: "status" }, notice) : null, content].filter(Boolean));
}

// New mail shows up while the screen is open.
setInterval(() => {
  if (document.visibilityState !== "visible" || state !== "ready" || problem || view === "compose" || busy) return;
  loadInbox().then(() => { if (view !== "compose") render(); }, () => undefined);
}, 30_000);

await start();
