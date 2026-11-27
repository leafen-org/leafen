# Building Leafen apps

A Leafen app adds screens, server-side logic, data and agent tools to a Leafen
server without changing Leafen itself. An administrator installs it, approves
exactly what it may reach, and chooses who may use it. This guide covers making
one; [Building on Leafen](developers.md) covers programs that call Leafen from
outside.

The app SDK in [`sdk/app`](../sdk/app) — the screen module, the manifest's
schema and types, the Go and Rust worker helpers and the example — is licensed under the
Apache License 2.0 (its own `LICENSE`), as this guide is; Leafen itself is
AGPL-3.0-only. An app may take any license its author chooses.

## Quick start

The `leafen` executable carries the app commands. They work on an app's
folder and never open a server's data folder.

```sh
leafen app init my-app --worker      # a manifest, a screen and a Go worker
leafen app dev my-app                # try it on a throwaway local Leafen
leafen app validate my-app           # check it as Leafen will
leafen app build my-app              # pack my-app-0.1.0.leafenapp
```

`dev` starts a separate Leafen on this machine's loopback with a new temporary
data folder, a made-up administrator (`developer@example.test` with a printed
random password) and an empty workspace. It installs the app for that
workspace's organization, lets everyone there use it, and installs it again as
`<version>-dev.<n>` whenever a file changes. Ctrl-C stops it and removes the
folder (`--keep` keeps it). Nothing touches another Leafen's data.

To sign a package:

```sh
leafen app key signing-key.json      # prints the key's fingerprint; keep the file secret
# put the fingerprint in the manifest's developer.keyFingerprint, then
leafen app build my-app -o my-app.leafenapp
leafen app sign my-app.leafenapp --key signing-key.json
```

A Go worker in the folder's `worker/` (a Go module) is built to
`workers/main.wasm` by `validate`, `build` and `dev`; it needs Go 1.24 or
later. Apps may write workers in any language that compiles to WebAssembly with
the Extism calling convention (see [Workers](#workers)); Go and Rust have
helpers today, and a Rust or JavaScript worker is built by hand before
`validate`.

## What an app is

An app is a `.leafenapp` file: a zip of

| Path | What it holds |
|---|---|
| `leafen-app.json` | The manifest: who made the app, where it shows and everything it may reach |
| `ui/` | The screen's files, served only to the app's own frame |
| `workers/` | WebAssembly modules that run on the server |
| `data/` | Setup files for the app's data structure |
| `templates/` | Blank files a file opener starts new documents from (`newFiles`) |
| `site/` | A web app a public endpoint with `pages` serves beside its API — a file may be kept compressed as `<name>.gz` |
| `signature.json` | An optional Ed25519 signature |

At most 2,000 files, 1 GB per file (16 MB for a `data/` file), 2 GB packed and
4 GB unpacked; a server's administrator may hold packages to less (Settings →
Apps → largest app package). Leafen reads a package from disk and keeps its
files in the server's file storage, so a large one never sits whole in memory
or in the database. An installed version is fixed by its fingerprint: a SHA-256 over every file's path,
size and digest, the signature aside. Leafen never loads code from elsewhere —
the screen runs only its package's scripts, and workers only its WebAssembly.

Apps act in three ways:

1. **As the person.** The screen calls Leafen's native API through its channel.
   Leafen gives the frame's shell a ten-minute token whose rights are the
   person's rights narrowed to what the administrator approved and to the
   organization the app was opened for. History records "the person, through
   the app".
2. **As the app.** The screen runs an action; a worker function runs on the
   server as the app's account, with the person who asked as an input. Rules
   belong in the worker: people can change what their screen sends.
3. **On its own.** Schedules, incoming webhooks, data changes and room messages
   run workers as the app's account.

Each organization that uses an app gets an app workspace that the app's
account owns, with the app's data. People who may use the app become its
members.

## The manifest

`leafen-app.json` follows [`sdk/app/schema/leafen-app.schema.json`](../sdk/app/schema/leafen-app.schema.json)
(TypeScript: `sdk/app/src/manifest.ts`), which is generated from Leafen's own
manifest structure. Leafen's checks are the authority; `leafen app validate`
runs them and lists every problem.

| Field | Meaning |
|---|---|
| `schema` | Always `1` |
| `id` | A reverse domain name, such as `com.example.approvals`; stays the same across versions |
| `slug` | The app's address, `/apps/<slug>`: 2–40 lowercase letters, digits or dashes |
| `version` | Semantic version, such as `1.2.0` |
| `minLeafen` | The oldest Leafen that may install it (see [Versions](#versions-and-compatibility)) |
| `name`, `description` | Text by language tag (`{"en": …, "ko": …}`); names up to 60, descriptions up to 300 characters |
| `developer` | `name`, an `https` `url`, and `keyFingerprint` when packages are signed |
| `icon`, `category`, `tags`, `license` | `icon` is a file under `ui/`; `category` is one of `work`, `connectors`, `mail`, `data`, `agents`, `public`, `games`, `personal`, `developer`; up to 8 tags |
| `surfaces` | Where the app shows: `fullPage.entry`, its own page; `blocks` (`key`, `name`, `entry`, `defaultHeight` up to 2,000 pixels), blocks people put in pages; `sidePanel.entry`, beside a page (see [Blocks and panels](#blocks-and-panels)); `importSources` (`key`, `name`, `description`, `entry`), cards on the import screen for the tools the app moves content from (see [Moving from another tool](#moving-from-another-tool)); `fileOpeners` (`key`, `name`, `types` — extensions such as `.docx` or types such as `application/pdf`, 1–20 — and `entry`), screens that open a file from its block's menu ("Open with"): the screen gets `context.file` (`pageId`, `blockId`, `fileId`, `name`, `contentType`, `size`, `canEdit`) and reads the file's bytes, as an `ArrayBuffer`, with `api("GET", "/files/" + fileId)` as the person. With `edit: true` (and `personAccess` openedFile, content or more) the screen also saves: nobody holds a file — anyone who may change the page edits it, and Leafen shows, outside the screen, who else has it open — and `saveFile(bytes)` puts a new file in the block over the version opened; the old file stays in the page's history. When someone saved first, Leafen asks the person, outside the screen, to save over it, keep theirs as a copy in a new block right below (`copied`, where later saves go) or open the latest (the screen opens again with it). While the screen has changes it hasn't saved, it says so with `setUnsaved(true)` (and `setUnsaved(false)` once saved): Leafen asks before its window or the browser tab closes. When the person can no longer change the page, the window stays open, says saving has ended, and saves are refused. When an administrator removes the app and installs it again while the window holds changes, the window stays and its calls go on with the app as installed now; removed for good, the window says so and offers what the screen last tried to save as a file to download. An opener that edits may list `newFiles` (`type`, one of its extensions such as `.docx`; `name`; `template`, a file under `templates/` of at most 10 MB), up to 10: the library's Documents offers them under "New document", copies the template as the person into the end of a page they may change — by default their own private "Documents" page, made if they have none — and opens it with the opener. Entries are files under `ui/` |
| `personAccess` | What the person's token may do through the app: `openedFile`, `read`, `content`, `structure` or `admin`. `openedFile`, the narrowest, is for file openers: the token a screen gets when a person opens a file with it reads that file, uploads, and saves to that block or to a copy it keeps beside it — nothing else of the person's — and the app's own storage; anywhere else the app has no token. An injected script in such a screen stops at the document it opened |
| `data` | The data structure's setup files: each entry is `key`, `file` (its setup file under `data/`) and, if needed, `locked`, `hidden` or `perPerson` (see [Data](#data)) |
| `network` | Outside addresses (see [Reaching outside](#reaching-outside)) |
| `device` | Browser features for the frame: `camera`, `microphone`, `geolocation`, `clipboard-write`, `fullscreen` |
| `realtime`, `kv`, `guests` | Live rooms, the temporary store, public guests |
| `workers`, `schedules`, `webhooks`, `events`, `actions` | Server-side logic and what runs it |
| `endpoints` | Standard protocol servers for programs outside Leafen (see [Reaching outside](#reaching-outside)) |
| `slashItems`, `menuActions` | Lines in Leafen's own menus (see [Menu lines](#menu-lines)) |
| `propertyEditors` | How database cells of some types are shown and entered (see [Property editors](#property-editors)) |
| `agent` | What the app tells agents: `summary`, `instructions`, `resources`, `prompts` |
| `notifications` | `true` lets the app's workers send notices to the people who use it (see [Workers](#workers)) |
| `groups` | `true` lets the app read the organization's groups — made by hand or kept by SCIM — and which of the people who may use it are in each (`GET /app-groups`, `GET /app-people?groups=`), as a shared vault follows a group |
| `webAssembly` | `true` lets the screen compile WebAssembly (a document converter, an engine): its frame's scripts take `'wasm-unsafe-eval'`, and the administrator sees it in the review. Without it the frame refuses to compile |
| `scriptEval` | `true` lets the screen run scripts it makes as it goes — `eval`, `new Function`, inline scripts and handlers — as an office editor does: its frame's scripts take `'unsafe-eval' 'unsafe-inline'`, in the frame with no origin or at an address of its own. The administrator sees it in the review as a high risk. Other limits — the sandbox, the addresses it may reach — stay the same |
| `settings` | What the administrator gives when installing: an `address` (`key`, `name`, `help`, `defaultPort`) a socket names as `{{setting:KEY}}` (see [Reaching outside](#reaching-outside)) |
| `credentials` | What each person gives the app for themselves, such as a mail account: `key`, `name`, `help` and 1–4 `fields` (`key`, `name`, `secret`) (see [Reaching outside](#reaching-outside)) |
| `pwa` | The app on a home screen: `shortName` (up to 12 characters) and `themeColor` (`#rrggbb`); `offline` is going (see below) |

`pwa.offline` does nothing and is going: an app's frame has no origin, so no service worker reaches it or its
files, and an app doesn't work offline; `validate` warns.

While a person has an app open, the browser's "install" adds that app — its
name in their language, its `icon` (PNG, JPEG, WebP or SVG; else its first
letter on its color) and `pwa.themeColor` — as an app of its own that starts
at and stays within `/apps/<slug>/` and logs in like Leafen. Administrators
can turn home screen apps off for the server. A review warns when an app's
name passes for Leafen's or a built-in app's.

The administrator approves the list Leafen derives from these fields. A new
version whose list differs is not installed until it is approved again.

In each organization the administrator lets everyone use an app, or chosen
groups and people. A group is named, not its people: whoever is in it now may
use the app, so a group SCIM keeps follows the directory.

## The screen

The screen is a page from `ui/` in a frame with no origin: it can't read
Leafen's storage or cookies, reach the top window or open a form elsewhere, and
`localStorage`, IndexedDB and service workers aren't available — keep what a
person needs between visits in `storage`. Its scripts must come from the
package's files — an inline `<script>` or `onclick=` doesn't run, and
`validate`, `build` and `dev` name each one; it may `fetch` its own files (a WebAssembly module, an engine's data,
by addresses relative to the page) and the blobs and `data:` addresses it made, frame its own pages (which
have no origin either, so Leafen doesn't say who may frame them), and
reach only the addresses the manifest's `network.direct` names. A worker starts from a `blob:` copy of its script, as
a classic worker (the frame's own address isn't its origin, and module workers
don't start there). Build it as a single-page app — the channel belongs to one
document.

A person can open the screen in its full window: Leafen leaves out its
sidebar and page header and keeps only its own bar (the app's name, "App" and
the way back). Opened from its home-screen icon, the app starts there. The
frame only grows — nothing in `context` changes, and the address stays the
app's.

Every app opens this way, on the desktop app and on a server alike, with
nothing for an administrator to set up. An engine written for an origin of its
own — one that frames its own pages and talks to them, or starts workers from
its files — still runs here: its pages have no origin either, so they reach
each other only by `postMessage` to `"*"`, telling who sent a message by
`event.source` rather than `event.origin`, and a worker starts from a `blob:`
copy. A small script loaded first in the engine's pages can hand its messages
on that way, as the office app's `ui/bridge.js` (`catalog-apps/office`) and the
HWP app's `studio-bridge.js` (`builtin-apps/hwp-viewer`) do.

Copy `sdk/app/dist/index.js` into `ui/` (`leafen app init` writes it as
`ui/leafen-app-sdk.js`) and load your script as a module:

```js
import { connect } from "./leafen-app-sdk.js";

const leafen = await connect();
leafen.context;   // { locale, theme, screen, path, homeUrl, app: { slug, version }, place, guest?, … }
```

| Call | What it does |
|---|---|
| `api(method, path, body?)` | Leafen's native API (paths below `/api/leafen/v1`) as the person; answers `{status, body}` |
| `action(name, input?)` | Runs one of the app's actions; answers `{output, runId}` |
| `relay(request)` | An approved outside address through Leafen |
| `kv(request)` | The app's temporary store |
| `storage.get(key)`, `storage.set(key, value)`, `storage.remove(key)`, `storage.keys()` | The person's own lasting store for the app, kept on the server: up to 200 keys of 1–100 characters, each value JSON (not null) of up to 16 KB, 256 KB in all; it goes with the person and the app |
| `room(name)`, `guestLink(room, minutes?)`, `reportGuest(guest, options?)` | A ticket to a live room; a public link to a guest room; reporting (and blocking) a guest |
| `joinRoom(name)` | Joins a room for its messages: `send(text)` to the server (the room's loop or function), `onMessage`, `onClose`, `close` |
| `connectAccount(provider)`, `connectedAccounts()`, `disconnectAccount(provider)` | Outside logins; call `connectAccount` from a click |
| `connectCredential(key)`, `credentials()`, `disconnectCredential(key)` | A person's own credentials: Leafen asks for them in its own window (call it from a click) and answers only which are given, `{enabled, credentials: [{key, connected}]}`; `LEAFEN_APP_CREDENTIALS_OFF` while the administrator has them off |
| `navigate(path)` | Moves within the app; Leafen's address follows, and back and forward reach `onLocation` |
| `link(path)` | The full public address of a place in the app (`context.homeUrl` plus the path), for mail and messages |
| `openPage(pageId)` | Opens one of Leafen's pages in Leafen itself |
| `openLink(url)` | Opens an outside page in a new tab after Leafen asks the person; an address on the Leafen server itself (its `homeUrl`'s origin) opens without asking — one of Leafen's screens, such as `/apps#app-passwords`, in place, a settings address (such as `/admin/settings?section=tunnel`) in the settings window over the app, which stays as it was, anything else the server serves in a new tab |
| `notice(message)` | A short message Leafen shows outside the frame |
| `printDocument(pdf)` | Prints a PDF the app made (up to 96 MB) in Leafen's page — an app's frame can't open a print dialog — once the person has used the page |
| `onTheme(listener)`, `onLocation(listener)` | Theme changes; back and forward within the app |

A refused call rejects with a `LeafenError` whose `code` is Leafen's error code.
A frame may make bursts of 100 calls, then 20 a second, with 16 out at once;
a call past that waits its turn, in the order calls came, so a screen that reads
a lot one call after another just goes at that pace. With 100 calls already
waiting a call fails with `LEAFEN_APP_BRIDGE_LIMITED`, and a frame that keeps
flooding (200 refusals within 10 seconds) is stopped until the person opens it
again.
Some native API paths are closed to app tokens: anything that reaches outside
addresses or publishes content (link previews, event webhooks, database
automations, Notion import connections, view links, publishing, forms), as well
as login, keys and administration.

Leafen draws the app's header, confirmations and notices outside the frame. An
app never asks for a person's Leafen password.

### Blocks and panels

An app's `surfaces.blocks` appear in the "/" menu of pages in organizations
where the app is installed, under "Apps", each with Leafen's "App" mark and the
app's name. The block shows its `entry` in a frame under a header Leafen draws
— the block's name, the mark and the app's name — `defaultHeight` pixels high
(320 if unset). People who may not use the app, and published pages, see the
block's name in its place. Exports and Notion's API keep only that name.

`surfaces.sidePanel` adds "Open … beside the page" to the page's "…" menu; the
panel docks at the page's right, where discussions dock.

In both, the screen connects as usual and `context` adds `place` (`"page"`,
`"block"` or `"sidePanel"`) and `pageId`; a block also has `blockId`, `block`
(its key), `props` and `editable`. Read the page with `api` within what the app
may read. A block's calls:

| Call | What it does |
|---|---|
| `setBlockProps(props)` | Keeps the block's settings in the block: a JSON object of up to 8 KB; refused when the person may not edit the page |
| `resize(height)` | The frame's height, 48–2,000 pixels |
| `onProps(listener)` | The settings changed, here or by someone else |

Anyone who may edit the page can change a block's settings, through Leafen's
API too: treat them as input. `navigate(path)` in a block or panel opens the
app's own page there.

### Menu lines

`slashItems` add items to the "/" menu: `key`, `name`, up to 8 `keywords`
(1–24 characters) and either `action` (one of the app's actions) or `block`
(one of its `surfaces.blocks`, put in the page under the block's name).
`menuActions` add lines to "…" menus: `key`, `name`, `on` (`row` — a
database row's menu — `page`, or both) and `action`. Leafen draws each with
the apps' icon, an "App" mark and the app's name; they appear only in pages
of organizations where the person may use the app, are on the list the
administrator approves (changing them asks again), and go when the app stops.

Choosing one runs the action as the person, in the app's organization, asking
the person first when the action needs approval. Its `input` says where it was
chosen — make the action's input schema accept it:

| Chosen from | `input` |
|---|---|
| A "/" item | `{"place": "slash", "pageId": …, "blockId": …}` (the line it was typed on) |
| A page's "…" menu | `{"place": "pageMenu", "pageId": …}` |
| A database row's menu | `{"place": "rowMenu", "pageId": …}` (the row's page) |

Leafen tells the person when the action finished or failed.

### Property editors

`propertyEditors` draw database cells and enter their values: `key`, `name`,
`types` (some of `rich_text`, `number`, `select`, `status`, `multi_select`,
`checkbox`, `url`, `email`, `phone`), `entry` (a file under `ui/`) and
`height` (the editor's, up to 480 pixels; 240 if unset). Someone who may change
a database's structure picks one in a property's settings ("Shown by"). While
people may use the app, each cell shows the entry in a frame the size of the
cell that takes no clicks; choosing the cell opens the entry again in a box
Leafen draws under it, headed by the editor's name and the app's. The value
keeps the property's type, so when the app stops or goes, the cells are
Leafen's own again with nothing lost.

`context.place` is `"propertyCell"` or `"propertyEditor"`, with `rowId`,
`propertyId`, `type`, `value`, `options` (a select's `id`, `name` and `color`)
and `editable`. In the editor:

| Call | What it does |
|---|---|
| `setValue(value)` | Writes the value in the property's type: text, a number or null, true or false, an option's `id` or null, a list of option ids; anything else is refused |
| `closeEditor()` | Closes the editor |
| `onValue(listener)` | The value or the options changed, here or by someone else (in a cell too) |

`context.screen` is `"phone"` (a touch screen narrower than 600px),
`"touch"` (a wider one) or `"desktop"`, by Leafen's own rule: the frame's own
width doesn't say, as it may stand beside Leafen's sidebar.

A frame's document is drawn on white unless it says otherwise; add
`<meta name="color-scheme" content="light dark">` and follow `context.theme`
so a cell or block matches Leafen's dark theme.

### Moving from another tool

`surfaces.importSources` names the tools an app moves content from, up to 8:
`key`, `name`, `description` (one line, up to 80 characters) and `entry` (a
file under `ui/`). Each is a card under "Import from other tools" on the import
screen, with Leafen's "App · 〈app〉" mark. The card opens a window Leafen draws
— the tool's name and line, then the entry — where the screen does what the
tool needs (an account to connect, an export to choose) and makes a workspace
bundle of it ([bundle.md](bundle.md)), its losses in the descriptor.

`context.place` is `"importSource"`, with `importSource` — the tool's `key`, and
`earlier`, the sources the person moved from this tool into this workspace
before: each the source id a bundle's descriptor named (`source.workspaceId`).
`importBundle(blob)` hands Leafen the bundle, up to 5 GB; it settles once
Leafen has taken it. Leafen puts it in a new workspace of its own, then the
window goes on with the migration — the "as is" draft, where it goes, the run
and the report, which names the tool and lists its losses — and deletes that
workspace when the run is done. A bundle whose source id is one of `earlier`
runs that migration's plan again: what is new comes in, what the tool changed
is updated, and what the person changed in Leafen stays; any other source id
starts a migration of its own. So name the source by what stays the same in
the tool (a vault, a space's key) and keep the bundle's IDs the same for the
same things in it (a note's path, a page's id). An agent moves a bundle the same way through the HTTP routes
and the MCP migration tools ([bundle.md](bundle.md#moving-from-another-tool)).

## Budgets

Every call an app makes to Leafen's native API — from its screen, its workers
or agents using its tools — draws on the app's budget: 600 calls a minute, of
which 120 may be writes, with bursts up to those numbers. A call made as a
person (their screen, their endpoint requests, their schedules, an agent acting
for them) draws on the app's budget for that person, so the people using a
drive don't share one; the app's own account has a budget of its own. The
server also bounds all apps together. When a budget runs short a call waits
its turn, in the order calls came, for up to two seconds; one that would wait
longer fails at once with `LEAFEN_APP_BUDGET_LIMITED` (HTTP 429, `Retry-After`
the seconds until there is room). A screen's call or file save turned away so
is asked again after that wait, for up to a minute in all, before the screen
gets that answer. An app's writes take turns one at a time across the server,
so people's own saving stays ahead, and each write's body is at most 256 KB
(`LEAFEN_APP_WRITE_TOO_LARGE`) — write in parts. An app refused more than 100
times a minute for ten minutes in a row is stopped, and the administrators get
a notice and see why. Administrators see each app's last day of use in its usage window and can
download its log without anyone's data.

## What people see

Each read an app makes with a person's access — from its screen, an action or
schedule run as them, or an agent using its tools for them — is counted by day
and by what it read. The person sees the last 30 days from the app's bar
("Data it read"); the rows stay as long as their organization keeps its audit
log (a year where it set none) and go with the app or the person.

## Actions and approvals

An action names a worker function and how it runs:

```json
{ "key": "submit", "name": { "en": "Submit for approval" }, "runAs": "app",
  "worker": "main", "function": "submit", "risk": "medium",
  "input": { "type": "object", "properties": { "amount": { "type": "number" } }, "required": ["amount"] },
  "requiresApproval": true, "externalEffect": false, "tool": true }
```

- `input` is a JSON Schema; Leafen checks the input before the worker runs.
  An action's input is JSON of up to 1 MB, as large as the platform's other
  request bodies; a screen that sends what a person writes says so as a long
  input nears that.
- `runAs: "app"` runs as the app's account; `"person"` runs with the person's
  own app token, so the worker reaches only what they may.
- `requiresApproval` makes Leafen ask the person, outside the frame, before each
  run; the approval is single-use, bound to that input and lasts ten minutes.
  When they don't allow it, nothing runs and the screen's call fails with
  `LEAFEN_APP_APPROVAL_DECLINED` (its message says so in the person's words);
  an agent's call answers `declined`.
  `risk: "high"` adds a line saying the app marks the action as important. The confirmation lists the
  input field by field — named by `inputLabels` (`{ "amount": { "en":
  "Amount", "ko": "금액" } }`), else the schema's `title`, else the key — with
  a person's ID shown as their name and a page of the app's own workspace
  (a request kept as a database row, say) shown as its title. The person may allow the action for an
  hour instead of once: on the app's screen, or on the one agent connection
  that asked, it then runs without asking until the hour ends. An
  administrator who oversees an organization makes every action there ask
  first, with no hour on the app's screen.
- Every action is also offered to agents (see [Agents](#agents)): what a
  person does on the app's screen, their agent does too. `tool: false` keeps
  one from agents, and `leafen app validate` names each such action.
- Database automations and buttons may run an action too ("Run an app action",
  with its input set there): only actions without `requiresApproval`, and none
  where the administrator oversees the organization. It is queued and retried
  as the app's other calls are, for the person whose change set it off (a
  button: who pressed it); the worker's input adds `via: { kind:
  "automation" | "button", automation, database, page }`.
- `undo` names another action that undoes this one: people see what an app did
  for them (the app's top bar, "What it did") and run it there; it gets
  `{ run, input }`, `input` being what the undone action ran with. Leafen also
  lets them revert, page by page, what the app changed in Leafen during a run.

## Workers

A worker is a WebAssembly module under `workers/` that Leafen runs with the
[Extism](https://extism.org) calling convention in a separate process with no
files, network or environment. Each function the manifest's worker lists is an
export that reads its input, sets its output or error and returns 0 (or 1 after
an error).

In Go, with the helper in `sdk/app/go/leafen`:

```go
//go:wasmexport submit
func submit() int32 {
	input := struct{ Amount float64 `json:"amount"` }{}
	call, err := leafen.Action(&input)
	if err != nil {
		return leafen.Fail("the input isn't JSON")
	}
	leafen.Log("submitted by " + call.Person)
	_ = leafen.OutputJSON(map[string]any{"ok": true})
	return 0
}

func main() {}
```

Build with `GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o workers/main.wasm .`.

An error the function sets itself (`leafen.Fail`, `leafen::fail`, Extism's error
output) is its reason, and reaches whoever called it: an action's screen
gets `422 LEAFEN_APP_ACTION_REFUSED` with the reason as the message (one line,
up to 500 characters), and an agent's tool call says "the app refused:" and the
reason — so write it for the person. A trap, a panic, running out of time or
memory is the call's failure: `502 LEAFEN_APP_ACTION_FAILED` "The action
failed.", its words kept in the run's record for the app's administrator. In an
app's screen, `app.action` gives what Leafen says of a run that failed, ran past
its time, found the app busy or stopped, in the person's language, so a screen
can show `error.message` whole; `error.code` stays the same.

In Rust, with the helper crate in `sdk/app/rust/leafen` (no other crates; its
`json` module reads and writes the calls), as a `cdylib`:

```rust
use leafen::Value;

#[no_mangle]
pub extern "C" fn submit() -> i32 {
    let call = match leafen::action() {
        Ok(call) => call,
        Err(_) => return leafen::fail("the input isn't JSON"),
    };
    let amount = call.input.get("amount").and_then(Value::as_f64).unwrap_or(0.0);
    leafen::log(&format!("submitted by {}", call.person));
    leafen::output_json(&Value::object([("ok", true.into()), ("amount", amount.into())]));
    0
}
```

Build with `cargo build --release --target wasm32-unknown-unknown` (after
`rustup target add wasm32-unknown-unknown`) and copy the `.wasm` from
`target/wasm32-unknown-unknown/release/` to the manifest's module path.

In JavaScript, with the [Extism JS PDK](https://github.com/extism/js-pdk)'s
builder `extism-js` (which needs binaryen's `wasm-merge` and `wasm-opt` on the
`PATH`): declare the exported functions and the Leafen host functions you call
in an `index.d.ts`, call each host function with the request's JSON in Extism
memory and read its answer the same way, and build with
`extism-js index.js -i index.d.ts -o workers/main.wasm`. The Hello example's
`worker-js/` shows `greet` with the store and the log.

What a function receives depends on what ran it:

| Trigger | Input |
|---|---|
| Action, or an agent's tool call | `{action, input, person, tenancy, role?, adminRoles?, via?}` — `role` is the person's role in the organization (`owner`, `admin`, `member`, `guest`…) and `adminRoles` the admin roles they hold there (`people`, `it`, `compliance`, `analytics`, `billing`), as Leafen's settings know them; `via` names the agent's connection |
| Incoming webhook | `{hook, delivery, contentType, body (base64), receivedAt, tenancy}` (`tenancy` for a call to an organization's address) |
| Schedule | `{schedule, at, tenancy, person}` (`person` for `perPerson` schedules) |
| Data change | `{event, change: {type, entityId, entityType, pageId, actor, workspaceId, data}, tenancy, receivedAt}` |
| Room message | `{room, tenancy, message, from}` — `from` is who sent it as Leafen knows them: `{person, name}` or `{guest}` (never what the message says); the output goes to the whole room |

Host functions (module `extism:host/user`, one memory offset in and out, JSON):

| Function | Request → answer |
|---|---|
| `leafen_api` | `{tenancy, method, path, query, body}` → `{status, body, text}` or `{error}`: the native API as the app's account in that organization's app workspace, or as the person for an action run as them; an answer of bytes (a file, an export) comes as `body_base64` with `content_type`, `bytes` and `filename`, up to 200 KiB, and a larger one as its `bytes` and a `note` on downloading it |
| `leafen_relay` | A relay request → the response, or `{error}` |
| `leafen_kv` | A store command (with `tenancy` when the run has none) → `{result}` or `{error}` |
| `leafen_tenancies` | → `[{tenancy, workspaceId}]` or `{error}` |
| `leafen_log` | A line for the run's log (16 KB per run, kept with the run) |
| `leafen_socket_open` | `{host, port, tls}` → `{socket}` or `{error}`: a connection to an address `network.sockets` lists, through Leafen (checked where the name resolves; private addresses only for an entry marked `privateNetwork`; at most four open in a run, all closed when it ends; each recorded); `host: "{{setting:KEY}}"` opens the address the administrator set |
| `leafen_socket_write`, `leafen_socket_read`, `leafen_socket_close` | `{socket, data}` (base64, up to 64 KB); `{socket, max, waitMs}` → `{data, closed}` (waits up to `waitMs`, at most 30 seconds, for bytes it may hand out — what may be the start of a credential the run wrote waits for the rest — so empty `data` means the wait passed); `{socket}` |
| `leafen_room_next`, `leafen_room_send` | `{waitMs}` → `{messages, senders, stop, peers}` (up to 64 messages, waiting up to 30 seconds for the first; `senders[i]` is who sent `messages[i]`, as in `from`; Go `leafen.RoomNextFrom`, Rust `room_next_from`); `{message}` (text up to 64 KB, to everyone in the room) — room loops only |
| `leafen_wait` | `{ms}` → `{stop}`: a long-running worker rests up to a minute and learns whether to end |
| `leafen_notify` | `{person, text, path?, tenancy?}` → `{ok}` or `{error}`: a notice in the person's inbox (and mail and push, as they chose) — one line of up to 200 characters shown as text, opening `/apps/<slug>/<path>`; only with `"notifications": true`, only to someone who may use the app in that organization, at most ten an hour to one person |
| `leafen_files_join` | `{files, name, contentType, tenancy?}` → `{file}` or `{error}`: writes stored files the run may read — in the app's workspace, or one the person's app connection reaches — in order into one new file in the app's workspace (up to 640 files, 5 GB), as a resumable or multipart upload ends |

When Leafen refuses a host call — an organization the app has no workspace
in, a request it doesn't take, the native API's own error — the answer is
`{error}`: Go's helpers return it as a `*leafen.Error`, Rust's as an `Err`,
and a JavaScript worker reads `error` from the answer. The function goes on and
decides what to answer. Only a run past its bounds below ends.

Each run has 64 MB of memory, 10 seconds, 200 host calls and 1 MB of output
(16 MB for an endpoint's answer), unless its worker declares `memoryMb` (up to
256) or `timeoutSeconds` (up to 60) for itself. An action past its time
answers `LEAFEN_APP_ACTION_TIMED_OUT` (HTTP 504), so its screen can say so.
Its clock is the server's and its random numbers are fresh for each run.
On a server or a desktop each worker is a process of its own.
A worker marked `"longRunning": true` names a function in `run` that Leafen
keeps running in each organization the app has a workspace in — for a
connection with no webhooks, such as IMAP IDLE or MQTT. Each run is a worker
process of its own for up to an hour with 600 host calls a minute; it loops,
resting with `leafen_wait` (Go `leafen.Wait`, Rust `leafen::wait`), and ends
when told to stop. Leafen starts it again a minute after it returns, at once
after its hour and after a growing pause when it fails, and stops it with
the app; administrators see it in the jobs window.
A worker marked `"resident": true` keeps its instance between its endpoints'
calls instead of starting anew for each: a large program starts once, and
what it holds in memory is still there for the next request — as a cache
only, since the instance may end at any time (after a failed call, when its
process is idle five minutes, at an update or stop) and other requests may
reach another of the app's worker processes. Keep what lasts in Leafen. One
instance answers every person who uses the app, so keep what you hold for
each person apart, by the request's `person`. Its other calls (actions,
schedules, webhooks) still start anew.
A
failed webhook, schedule or change run is tried again after 30 seconds, then
1, 2 and 4 minutes. Administrators see each app's schedules, triggers and
recent runs in its jobs window. An emergency stop ends the app's running
calls at once (they show as ended by it); a webhook or schedule call it cut
waits until the app runs again. A plain stop lets running calls finish. While
an app is stopped its calls answer `LEAFEN_APP_STOPPED` (HTTP 403), and Leafen
replaces its open screen with that notice.

Schedules use five-field cron, at least five minutes apart, in the time zone
the administrator chose in the jobs window, else the app's `timeZone` (an IANA
name such as `Asia/Seoul`), else each person's own for a `perPerson` schedule,
else the server's. `perPerson` runs once for each person who used the app and
may still use it, unless they turned it off for themselves on their apps page —
give such a schedule a `name` ({ "en": …, "ko": … }) they recognize it by.
`events` lists the event webhook types (see [Building on Leafen](developers.md))
that run a function when the app's workspace changes; the app's own changes
don't. With `dataSource` (one of the app's data keys) an event runs only for
rows of that data source; `properties` narrows `page.properties_updated` to
changes of the properties named, and `where` to rows whose values match —
each `{ "property": "Status", "equals": "Done" }`, `"notEquals"`, or
`"empty": true|false`, compared as the value reads (an option's name, `true`
or `false`, the text or number, a date's start).

```json
"events": [{ "key": "done", "types": ["page.properties_updated"], "worker": "main", "function": "onDone",
  "dataSource": "tasks", "properties": ["Status"], "where": [{ "property": "Status", "equals": "Done" }] }]
```

## Data

`data` entries point to setup files under `data/`; each is a list of steps
that runs once, as the app's account, when an organization's app workspace is
first made. The manifest names each file with the key of the step that makes
the entry:

```json
"data": [ { "key": "requests", "file": "data/requests.json" } ]
```

and `data/requests.json` holds the steps:

```json
{ "steps": [
  { "key": "requests", "tool": "create-database",
    "args": { "parent": { "page_id": "{{root}}" }, "title": "Requests", "schema": "CREATE TABLE (\"Name\" TITLE, \"Amount\" NUMBER)" } },
  { "key": "board", "tool": "create-view",
    "args": { "database_id": "{{requests.id}}", "data_source_id": "{{requests.data_source_id}}", "name": "Board", "type": "board" } }
] }
```

A step is the input of Leafen MCP's `create-database`, `create-view` or
`create-pages`, so calls made while designing on a test server can be copied
in. A schema's columns take Notion's options: `SELECT('Draft':gray, 'Done':green)`
and `MULTI_SELECT(…)` with colors (default, gray, brown, orange, yellow, green,
blue, purple, pink, red), `NUMBER FORMAT 'dollar'` (Notion's format names) and
`COMMENT 'description'` on any column, and columns that refer to others as Notion
writes them: `FORMULA('prop("Points") * 2')`, `RELATION('{{projects.data_source_id}}')`
(`, DUAL` or `, DUAL 'Tasks'` for a two-way one), `ROLLUP('Project', 'Budget', 'sum')`
by names, and `UNIQUE_ID PREFIX 'TK'` (2-10 characters, a letter first, then letters,
digits or hyphens, as Notion takes it). `{{root}}` is where the entry's data goes; `{{key.id}}`,
`{{key.data_source_id}}` and `{{key.view_id}}` refer to earlier steps. A `data`
entry's `key` is the key of the step in its file that makes it.

A setup file's steps are written in one language, its `language`, and
`names` says what a written name becomes in each other language — a title,
a column, an option, a comment, a value a step writes:

```json
{ "language": "en",
  "names": { "ko": { "Requests": "요청", "Name": "이름", "Amount": "금액" } },
  "steps": [ … ] }
```

What an organization shares is made in its owner's language and a person's
own data in theirs, Korean or English as Leafen's own texts are; a language
without names takes the English ones, else the steps as written. Every
string value and `properties` key that is a written name, and in a schema
the quoted column names, the quoted texts that are a written name and the
columns a formula's `prop("…")` names, take that language's name; no two
names of a language may be the same. The app keeps working with the names
it wrote: `GET /app-data` answers with them, and an option a query's
condition names and the columns and options a data event waits for are read
as written. The permission lines and an update's data changes in the
package review name the data in the reader's language.
What was made keeps its names; an update's new columns take the language
their database was made in.

An entry marked `locked` goes where people only view it (the app's account writes it;
a write to it as a person — a `runAs: "person"` action or the person's own
call — is refused with 403 `LEAFEN_PERMISSION_DENIED`, "You can view this
database row but not change it."), `hidden` stays the app account's own, and
the rest goes where people edit.
Pages the app manages are marked so. The app's workspace and the teamspace
people edit take its `name` in the language of the organization's owner; the
view-only teamspace adds "· view only", and the root pages are named by what
they hold — "Shared data", "Data the app keeps" and, in a person's own pages,
"〈name〉 · my data" in that person's language — in Korean or English as
Leafen's own texts are, the name falling back to the English one. People join
an app's workspace by being allowed to use the app, so it offers no invitation. Entries marked `perPerson` (a setup file's
entries are all per-person or none) are made for each person the first time
the app is reached for them in an organization — their first opening of it,
an endpoint they log in to with an app password, or an agent's tool — as
that person, under a private page of theirs titled with the app's name
(`{{root}}` in those steps) — so only they see them; `GET /app-data` from
their session lists them with `place: "person"`, after the organization's. A
new version's new per-person steps are made the next time the app is reached
for them; what was made stays as it is. While a person's couldn't be made,
the app's row in Settings → Server → Apps says for how many people and why,
until the next time the app is reached for them makes it.

A new version's steps are compared with what each workspace was made from
(by key): a new step is made; a database's new columns are added and changed
ones converted where Leafen's conversion keeps the values (otherwise the old
column goes and a new one comes); a step that is gone, and a column a version
drops, go to the trash (Deleted properties for a column) — rows a step added
and views stay. A view or page whose arguments changed is left as it is,
since people may have changed it. The administrator sees all of it in the
package review — for a workspace an earlier update didn't finish in, what it
gets from what it was made from, marked so — and an update that removes
anything there or elsewhere installs only once they
confirm (`acceptDataRemovals`); each workspace catches up right after, or the
next time the app opens there. Until it has, the app's row in Settings → Server
→ Apps says why it couldn't, as it does for data that couldn't be made the
first time. Step keys can't begin `root-` or `teamspace-`.

`GET /app-data` (from the screen with `api`, or from a worker with
`leafen_api`) says where each step's data went in the organization the call
acts in: `{workspaceId, items: [{key, pageId, blockId, dataSourceId, viewId,
place, properties}]}` — for a database, its block, data source and first view,
the page it is on and, when the call may read that page, its columns in order
(`properties`: `{id, name, type, position, config}`, a select's options in
`config.options`, named as the steps wrote them); for pages, the page; for a view, the view. `place` is
`edit`, `view`, `hidden` or `person`.

The SDK's table helper reads and writes a database by its columns' names —
`leafen.table(key)` on the screen, `leafen.OpenTable(tenancy, key)` in Go,
`leafen::data::Table::open(tenancy, key)` in Rust:

```go
requests, err := leafen.OpenTable(call.Tenancy, "requests")
for row, err := range requests.Rows(leafen.Query{Where: []leafen.Where{
	{Column: "Approver", Operator: "contains", Value: call.Person},
	{Column: "Status", Operator: "equals", Value: "Waiting"},
}}) { /* row.Text("Title"), row.Option("Status"), row.IDs("Approver"), row.CreatedAt */ }
waiting, err := requests.Count(leafen.Query{Where: []leafen.Where{{Column: "Status", Operator: "equals", Value: "Waiting"}}})
id, err := requests.Add(map[string]any{"Title": "Laptop", "Status": "Waiting", "Amount": 1200})
err = requests.Update(id, map[string]any{"Status": "Approved", "Decided": "2026-10-10"})
err = requests.UpdateMany(map[string]map[string]any{id: {"Status": "Paid"}, other: {"Status": "Paid"}})
err = requests.Remove(other) // to the trash, where people can restore it
```

`UpdateMany` (`updateMany`, `update_many`) writes a hundred rows to a call;
`Remove` (`remove`) takes two. A relation column is written by `Update` too,
as the app's account edits its table's page.

Underneath are the routes anyone may call: rows come from
`POST /pages/{pageId}/database-query` (`dataSourceId`, `filterGroup`, `sorts`,
`search`, `limit` up to 100 and `cursor`; filters take an option's name or ID,
each row carries `createdAt`, `createdBy`, `updatedAt` and `lastEditedBy`, and
formulas and rollups come in `computed`). A query in the table's own order
(no sorts, no search) whose filter is empty or a top-level `and` naming
options of a select, status or multi-select, people or rows of a person or
relation, or a checked checkbox — rows found by their kept values — answers
at once, a page a call, each `nextCursor` going on after the page's last row;
`total` comes with the last page. Any other query walks the table 100 rows
per call — 5,000 with such conditions — and while it walks it answers `202`
with `state: "pending"`, no rows and a `nextCursor`; send the same query
again with that cursor until the rows come. Once they do, `total` says how
many rows the query keeps. So does a query in the table's order that reaches
a row whose formulas or rollups read many related rows, going on after the
last row it gave. With `countOnly: true` it gives just `total` — and when
every condition is one of those above (no search, no nested groups) it counts
5,000 rows a call without reading them.
One row is `GET /database-rows/{rowId}/page`; `POST
/database-rows/{rowId}/page/values` (`{mutationId, values: {property: {type,
value}}}`, a property by its ID or its name, `type` and `mutationId` optional)
changes only the values it names and keeps the row's place
(relations only for one who edits the database's page, not through a row
page's own share); `POST /pages/{pageId}/database-row-values` (`{mutationId,
rows: [{rowId, values}]}`, up to 100 rows of that database page) changes many
in one change; a new row is an `upsertRow` in `POST
/pages/{pageId}/content-graph/mutations`, whose `icon` and `cover` are read
only there; a row's page, like any page, takes its icon, cover and style with
`PATCH /pages/{rowId}/metadata` (`{mutationId, baseCursor, icon, iconType}`,
`baseCursor` the `cursor` `GET /pages/{rowId}` reads); `POST
/database-rows/{rowId}/page/trash` (`{requestId, expectedAuthorityRevision}`,
the row's `authorityRevision` as read) moves one to the trash. A
value is what the column's type keeps: text, a number, true or false, an
option's ID (a list of them for a multi-select), a list of people's or rows'
IDs, a date `{start, end}` (or `"start/end"`), `null` to clear. A worker run's
200 host calls bound what it reads at once: one call for each part of the
table the query walks and one for each hundred rows it gives back — so read
with conditions like those above, count with `Count` (`count` on the screen
and in Rust) rather than reading every row, and keep lists a person reads to
the rows that are theirs.

`GET /app-people?q=` lists the people the app may name there — whom a task
goes to — by `id` and `name` (never emails), by name a window at a time (100,
or `?limit=`): call again with `?cursor=` the `nextCursor` while one comes, so
read on or find someone with `q`, never take the first window for all; from
the screen `you` says which of them the app acts for. `?ids=<id>,<id>` (up to
100) asks after particular people, as for the names of those a list shows:
only those who may still use the app there come back. `?email=` asks
who of them logs in with that address, as the Vault app does before it gives
someone an address for their vault. An app whose
manifest sets `groups` may add `?groups=<id>,<id>` (up to 100) to keep those
in any of the groups, each with `groups`: which of those they are in, and
lists the groups themselves with `GET /app-groups`
(`{groups: [{id, name, managedBy}], nextCursor}`, `manual` or `scim`, by name
a window at a time: call again with `?cursor=` the `nextCursor` while one
comes) — never a group's people otherwise.

`GET /app-workspaces` lists the workspaces the call reaches — the person's in
that organization, for their screen or a run for them (an action run as the
person, a per-person schedule), with the app's own marked `app` and other apps'
marked `otherApp` — so an app can read a person's `/workspaces/{id}/my-tasks` or
inbox in each, each a window at a time to the last: `my-tasks?limit=` (up to
500) gives the person's tasks by database and each database's own order (with
`scope=sources`, the tasks of My tasks' sources, none until the person picks
them, then everyone's; with `assignee=me` too, only the person's own there), and
`notifications?state=unread&limit=` (up to 200) the unread newest first; call
again with `?cursor=` the `nextCursor` while one comes. Without `limit`,
`my-tasks` is the list My tasks shows: its first 500 tasks, sorted, `truncated`
past them. `GET /workspaces/{id}/page-children?parent=<page>` lists the pages
under a page, or at the workspace's top without `parent`, that the person sees,
as their sidebar lists them — not archived pages, a button's template pages, or
pages hanging on a table row's cells, a caption, or a line or title that no
longer mentions them (`{pages: [{id, title, icon, iconType, kind, created,
updated, hasChildren}], more, nextCursor}`, a window at a time in the
sidebar's order, `limit` up to 1000: call again with `?cursor=` the
`nextCursor` while one comes, never take the first window for all — one may
hold fewer pages than the limit, or none, while more follow; `hasChildren`
counts only such pages) — how an app walks a workspace as a tree. `POST /app-storage`
is the person's own store (what `storage` reads on the screen) for a worker
running for them; the app's own account has none.

## Reaching outside

- **Directly** — `network.direct` lists `https` addresses and what the frame may
  load from each (`connect`, `img`, `font`, `media`, `frame`). The administrator
  sees that the app may send data there. The server's strict mode (on
  screen, "Apps connect only through this server") turns direct access off
  for every app; with it off, an administrator can turn it on for some
  organizations only, and their frames lose direct access while the others
  keep it.
- **Through Leafen** — `network.relay` lists addresses (scheme, host, port and
  path prefix) the screen and workers may call through Leafen. For a service
  that gives each customer a site of its own, the host may start with `*.`:
  `https://*.atlassian.net/wiki/` takes `acme.atlassian.net`, one name more
  and no deeper, over a public domain of two names or more. Leafen checks the
  address at connection time, follows at most three redirects (each checked),
  allows 1 MB requests, 5 MB responses, 30 seconds, 120 calls a minute per app
  and person and 8 at once per app, and records host, path, status and size.
  Private-network addresses need `privateNetwork` and the administrator's
  approval. This server's own machine (`localhost`, `127.0.0.1`, `::1`),
  link-local addresses and a cloud's metadata address are never reached, even
  with `privateNetwork`; `validate` refuses a relay or socket naming one, and a
  name that resolves to one is refused when called, saying which. Secrets go
  only in headers, by name: `{{secret:NAME}}` for a secret
  the manifest lists and the administrator sets, `{{oauth:KEY}}` for an outside
  login token. Neither reaches the app, and responses have them masked.
  A call made for an organization with data rules (DLP) has its body and query
  checked against the organization's sensitive terms; one that matches doesn't
  leave (`403 ORGANIZATION_DLP_POLICY_BLOCKED`, recorded as `dlp`).
  An app that reaches none of the person's data (no `personAccess`) holds no
  token; its screen's `relay` goes through Leafen's window with the person's
  own login, for the organization the screen opened in — the same entries,
  secrets, credentials and limits — and so do its outside logins below.
- **Addresses the administrator sets** — for a server each organization has its
  own of, such as its mail server, declare a setting (`settings`: `key`,
  `type: "address"`, `name`, `help`, `defaultPort`) and a socket entry
  `{"host": "{{setting:KEY}}", "tls": …, "privateNetwork": …}` without a port.
  The approval lists it as an address the administrator sets; the administrator
  gives it (`host` or `host:port`) when installing or in the app's settings, and
  until then the socket doesn't open. A worker opens it by the setting's name —
  `leafen_socket_open {host: "{{setting:KEY}}", tls}` — and reaches that address
  alone, through the same checks as any socket.
- **A person's own credentials** — for an account each person has there, such
  as their mailbox, declare it in `credentials` (`key`, `name`, `help`,
  `fields`; mark a password `secret`) and name it on the socket or relay
  entries that take it: `"credentials": ["KEY"]`. The approval lists both.
  Approving the install turns it on, unless the administrator turned it off
  in the review; they can turn it off or revoke it all later in the app's
  settings (an app a person adds themselves starts with it off). Then the
  screen's `connectCredential(KEY)` opens Leafen's own window, where the person
  types it, and Leafen seals it. The app never sees it: in a run made for the
  person (an action with `runAs: "person"`, or a `perPerson` schedule), a
  worker writes `{{credential:KEY.FIELD}}` to such a socket, or the screen or a
  worker puts it in such a relay entry's header, and Leafen fills it in.
  `{{credential:KEY.FIELD|quoted}}` gives it as a quoted string (`"` and `\`
  escaped — an IMAP `LOGIN`), `|base64` in base64 (SMTP `AUTH LOGIN`), and
  `{{credential:KEY.USER:PASSWORD|base64}}` two fields joined by `:` in base64,
  HTTP Basic's form (`Authorization: Basic …`). Fields
  marked `secret` (at least four characters) never come back: a socket gives
  as many asterisks in their place, so a protocol counting bytes still can,
  and holds back an end that may be the start of one until the rest comes; the
  relay gives `[secret]`. Other fields — a user name, often in the mail itself
  — come back as they are. Anywhere else — another address, a relay body or
  address, a run for the app — it's refused. A server the credential goes to
  may pass it on (mail sent through it), so approve this only for apps trusted
  with the account. People see and
  remove theirs on their apps page; the administrator sees how many gave it and
  takes them all back; giving, use (at most once a minute), removing and taking
  back are in the app's audit. Suggest an app password where the service has
  one.
- **Outside logins** — `network.oauth` declares a provider (`key`,
  `authorizeUrl`, `tokenUrl`, `scopes`, `owner: "person" | "app"`, and
  `authorizeParams` for the provider's own login parameters, such as
  Google's `{"access_type": "offline", "prompt": "consent"}` for a token that
  renews — Leafen's own, like `state` or `client_id`, can't be set); the
  administrator sets `KEY_CLIENT_ID` and `KEY_CLIENT_SECRET` and registers the
  return address the app's secrets window shows. The screen's
  `connectAccount(KEY)` opens the provider's page in a window of its own;
  `connectedAccounts()` and `disconnectAccount(KEY)` list and remove them.
  Leafen keeps the tokens — each person's own, or one for the app with
  `owner: "app"` — and fills `{{oauth:KEY}}` in a relay made for the person,
  with or without `personAccess`.
- **Incoming webhooks** — `webhooks` entries receive at
  `/api/leafen/v1/apps/<slug>/hooks/<key>` with `verify: "token"` (`?token=` or
  `X-Leafen-Token`) or `"hmac-sha256"` (`X-Leafen-Timestamp` and
  `X-Leafen-Signature: v1=hex(HMAC(secret, timestamp "." body))` within five
  minutes, or `X-Hub-Signature-256`). Bodies up to 1 MB, 60 calls a minute;
  repeats of a delivery are accepted once. The administrator makes the secret.
  The administrator may also make an address for one organization
  (`…/hooks/<key>?org=<organization>`, with its own secret); calls there run
  for that organization, with `tenancy` in the worker's input. A webhook's
  optional `name` gives what people read it as in languages
  (`{"en": "Payment notices", "ko": "결제 알림"}`), which the review, the
  consent window and the app's settings window show instead of its key.
  Naming a webhook keeps the app's approval.
- **Endpoints** — `endpoints` entries (`key`, `name`, `worker`, `function`,
  optional `methods` — HTTP's own and WebDAV's, CalDAV's and CardDAV's, such as
  `PROPFIND` or `MKCOL` — and `public`) declare a standard protocol server, a
  WebDAV folder or a Bitwarden-compatible API, that programs outside Leafen
  reach at `/app-endpoints/<slug>/<key>/` under the server's address. The
  review shows each one; a public one is a high risk and opens only once the
  administrator turns on "Open without logging in" for it in the app's usage
  window — until an update changes the app's permissions. An opened endpoint
  leaves logging in to the app: its requests run with no `person` (and
  `tenancy` only when they name `?org=<organization>` where the app has a
  workspace; the app may also read the organization from its own path), and
  their `authorization` header comes to the function, as a password manager's
  token does. People sign the program in with their email and an
  app password they make for the app on their Apps page ("App passwords"):
  shown once, up to 20 for an app, taken back whenever they like, and good for
  nothing but that app's endpoints — never Leafen's login, API or MCP.
  The program sends HTTP Basic with the email and the password (or the
  password as a bearer); ten wrong ones from the same place and name wait 15
  minutes. Every path and declared method below the endpoint runs its worker
  function as the person through the app — `leafen_api` reaches what their
  app token would — with `{"endpoint", "method", "path", "query", "headers",
  "body", "person", "tenancy", "address"}`: `path` starts with `/`, `headers`
  are lowercased without cookies (and without the login, but on an opened
  public endpoint), `address` is the endpoint's as people are shown it,
  `body` is base64 up to 1 MB; a
  larger body (up to 5 GB) is kept as a file in the app's workspace, uploaded
  as the person — the organization's storage limit and data rules apply, and
  a file nothing keeps is cleaned away later, at once when the function
  refuses the request (a status of 400 or more) or fails; one it no longer
  needs it deletes with `DELETE /files/{id}` — and comes as `bodyFile` (`id`,
  `name`, `size`, `contentType`, `sha256`) instead. The function answers
  `{"status", "headers", "body"}` (`bodyBase64` for bytes), or `{"file": id}`
  for a stored file the run may read, which Leafen sends with ranges and
  `If-None-Match`/`If-Modified-Since` (pass an `ETag` header for those). The
  host function `leafen_files_join` (Go `leafen.JoinFiles`, Rust
  `leafen::join_files`) writes files the run may read, in order, into one new
  file — how a resumable or multipart upload ends; with `workspaceId` (Go
  `leafen.JoinFilesInto`) the file goes to another workspace the run may
  write, where a page's file block can take it, so joining one file copies it.
  An endpoint's `fileBodies` (`["PUT"]`) names methods whose bodies always
  come as a stored file, however small — a file server's uploads. A body sent
  with `X-OC-Mtime` (Unix seconds, as rclone and ownCloud's and Nextcloud's
  clients send a file's own time) keeps that time on its file, and a copy
  `leafen_files_join` makes of one file keeps it too; a page's
  `content-graph/file-blocks` gives it as each block's `modifiedAt`.
  Leafen passes on the headers a protocol needs (`Content-Type`, `ETag`,
  `Last-Modified`, `DAV`, `Allow`, `Content-Range`, `Lock-Token`,
  `X-OC-Mtime`, a `Location` inside the endpoint and the like), drops the rest, and sends every answer
  sandboxed (`Content-Security-Policy: sandbox`), unsniffed and without
  cookies or CORS: a page an endpoint serves runs no script and can't read
  Leafen. A public endpoint with `pages: true` may also answer
  `{"packageFile": name}`, a file under the package's `site/` — a web app of
  its own beside its API, as the Vault app serves Bitwarden's web vault (that
  one isn't in the package: Bitwarden's release stays apart from Leafen's
  executable, under its own GPL, so the server fetches the pinned release the first time it's
  opened, checks its SHA-256 and keeps it in the data folder; a server without
  the internet takes the archive put in `<data>/app-page-packs/<sha256>.tar.gz`):
  Leafen sends the package's file (`<name>.gz` compressed as kept when the
  browser takes gzip), never a body the worker made, under a page policy —
  still sandboxed, with no origin, so it can't read Leafen's storage or
  cookies; scripts, styles and requests only from the endpoint's address,
  WebAssembly allowed, nothing framing it — and, since a page with no origin
  has no storage, `localStorage` and `sessionStorage` kept in memory by a
  script Leafen puts first in each HTML page. The page's requests carry the
  origin `null`, which such an endpoint answers for, preflights and all, with
  credentials (its login is the app's own bearer token, never Leafen's, so
  pages are refused on a logged-in endpoint); the review shows it as a high
  risk. A browser opens several requests at once: one waits its turn up to ten
  seconds for another to finish, and up to ten more for one of the app's
  workers, before the endpoint says it is busy; requests waiting go in the
  order they came. A stopped app answers 503, a busy one 503 with `Retry-After`. By default each person makes up to
  600 requests a minute (then 429 with `Retry-After`) and the app answers 4
  at once; files its endpoints keep count against the organization's storage
  limit. The administrator raises or lowers both for the app, and may give it
  a storage limit for each person (then 507), in the app's usage window,
  which also counts the last day's requests, refusals, bytes and kept files.

## Rooms, the temporary store and guests

- **Rooms** — `realtime.maxParticipants` (1–200), `realtime.maxRooms` (rooms open at
  once, 1–500, 50 if not given; a new one past it is refused). `room(name)` gives a ticket for
  ten minutes, or `LEAFEN_APP_ROOM_FULL` with the reason when the room already seats as many
  people (or guests) as the manifest lets it, so the screen can say so; join with y-websocket at `{serverUrl}/{roomName}?ticket=…`, or
  with `joinRoom(name)` for the room's messages alone (y-websocket's stateless
  messages: the type 5, the length and the text). A room
  keeps awareness and a shared document while open; keep anything lasting in
  the app workspace. With `realtime.worker` and `function`, stateless messages
  sent to the server run that function. With `"loop": true` too, the function
  is a room's loop — a game's referee: Leafen runs it once for each room while
  people are in it, starting with the first and telling it to stop when the
  last leaves; it reads the room's messages to the server with
  `leafen_room_next` (also its clock) and sends to everyone with
  `leafen_room_send`, keeping its game in its own memory (an hour at most,
  started again while the room stays occupied — keep what must survive in the
  store). The server runs up to 32 loops at once; past that a room's messages to
  the server go unanswered. Trust `from`/`senders` to know who sent a message,
  not anything inside it.
- **WebRTC** — only with `realtime.webrtc: true`, which the administrator sees
  among the app's permissions. Without it Leafen removes peer connections from
  the app's pages and takes away frames the page makes itself (`srcdoc`,
  blank, `data:` or `blob:`); an app that declares it signals through its
  rooms' messages and asks for the camera or microphone with `device`.
- **Temporary store** — `kv.maxBytes` (up to 64 MB) and `kv.maxTtlSeconds` (up to
  7 days) per organization, in memory: strings, hashes, lists and sorted sets.
  A new key needs a `ttl`; a full store refuses writes; a restart empties it.
- **Guests** — `guests.rooms` and `maxPerRoom` (1–100), with `realtime`;
  `guests.names` gives each room's name in languages as the app's screen calls
  it (`{"table-1": {"en": "Table 1", "ko": "테이블 1"}}`), which the review,
  the consent window and the guest links show — rooms without names are
  counted there instead. Naming rooms keeps the app's approval. Once the
  administrator allows guests, `guestLink(room, minutes)` (5–1440) makes a
  public link; guests get only that room, the store and notices, for up to two
  hours. Someone using the app reports a guest with `reportGuest(guest,
  { reason, block })` — `guest` is the ID in `from` — and the administrator
  sees it; `block` also ends the guest's session and keeps its address out of
  that link.

## Agents

Agents connected to Leafen's own MCP server find apps with `leafen_app_tools`
and call its actions (all but those marked `tool: false`) with `leafen_app_call`; when a connection's
owner chooses apps, their tools are also listed as `app_<slug>__<key>`. An action
that needs approval asks the person inside their conversation with the agent,
never through a link and never by an MCP elicitation form: the agent is told
to ask its person in the conversation in the words given and, only on their
yes, to call again with the request's id within a day (with `:hour` after it
where they allowed it for an hour). That holds where the administrator
oversees the organization's apps too. An agent turning the oversight itself
on or off, or changing another of the administrator's safeguards over apps —
which apps people may add, the app policies, connecting only through the
server, and each app's running, people, guests, open address and people's
credentials — asks its person every time, either way and however its
token's asking is set ("Hand it all over" too), with no hour. `agent.instructions` and `agent.summary` reach agents as the app's own
words; `agent.prompts` become MCP prompts (`{{name}}` in a template is a required
argument) and `agent.resources` become `leafen-app://<slug>/<key>` resources
that run the named action. A prompt's or resource's `name` is text or, like the
app's other names, an object of language codes to text; agents read the English one. An action marked `risk: "high"` asks the person
before an agent runs it, as an approval does, though their own screen runs it
at once. An agent reads and changes the person's own store for the app — the
settings its screen keeps in `storage` — with `leafen_app_storage`, or the
native API's `POST /api/leafen/v1/apps/{installId}/storage` with the person's
key (`?tenancy=` for a person who uses the app in more than one organization).

## Distribution

- **Files** — an administrator installs a `.leafenapp` from Settings → Server → Apps.
  Unsigned packages, and those signed by a key no catalog vouches for, need the
  administrator to confirm they trust the maker.
- **Catalogs** — an administrator trusts a catalog by its address and list key,
  or, on a closed network, by a bundle file. A catalog is `catalog.json` signed by
  its Ed25519 list key in `catalog.json.sig` (base64 of the signature over
  `"leafen-catalog-v1\n"` followed by the index's bytes). The index lists apps
  (`id`, `slug`, `name`, `summary`, `developer`, `category`, `tags`, `version`,
  `languages`, `network`, `risk`, `paid`, `reviewed`, `icon` — the package's
  icon as a `data:` address, PNG, JPEG, WebP or SVG of at most 16 KiB, which
  the lists and the install review show before the app is installed; without
  one they show the name's first letter — and the package's `url`,
  `sha256`, `packageHash` and `size`), `featured` ids and `revoked` versions
  (`id`, `packageHash`, `reason`). Leafen reads it every six hours, fetches
  exactly the file and version an entry names — to disk, going on with a range
  request when a download is cut (three tries, a minute of silence ending one),
  checking its SHA-256 before reading it — and stops any installed version
  a trusted catalog revokes, telling the server's administrators in their
  inbox. A bundle is a zip of `catalog.json`,
  `catalog.json.sig` and `packages/*.leafenapp`.
- **Making a catalog** — `leafen app publish <signed package> --package-url …`
  writes the app's entry, `apps/<id>/app.json`, with the package's address and
  hashes, into a catalog's folder and copies the package beside it as its
  release file, which stays a file of its own however large. `leafen app
  catalog <folder> --key <list key>` makes `catalog.json` and
  `catalog.json.sig` from a folder's entries (with its `featured.json` and
  `revoked.json`), reads them back as a server would and prints the list key.
  Serve each package at its entry's address and the two index files side by
  side; administrators add the catalog by the address of its `catalog.json`
  and its list key.
- **The official catalog** — Leafen makes and signs it the same way at each
  release, and takes no pull requests to it: another maker publishes a
  catalog of their own. A release build names the official catalog — the
  `catalog.json` of Leafen's latest release on GitHub — with its list key,
  and trusts it on every server, as it lists the built-in apps; a development
  build names none, and an administrator can add it by its address and list
  key.
- **Built-in apps** — apps shipped inside Leafen appear in "Browse apps" as
  Leafen's own, none installed at first.
- **Who installs** — administrators choose: only administrators (the
  default); people ask for a catalog or built-in app on their Apps page and an
  administrator reviews, installs and approves; or people add the apps
  administrators allowed themselves, after agreeing to the app's permissions —
  asked again when an update changes them. Administrators can also refuse apps
  nobody signed.

## Versions and compatibility

- The manifest's format is `schema: 1`. A change that breaks it comes with a new
  schema number.
- `minLeafen` is checked at install: a server older than it refuses the app and
  its review says why. A prerelease of Leafen counts as its release.
- The SDK follows semantic versioning (now 0.1.0); its screen module and the
  channel it speaks change together.
- Before a manifest field, channel call, host function or SDK interface is
  removed or changes meaning, the release notes say so and `leafen app validate`
  and `build` warn each app that uses it, and the old form keeps working until
  the next minor release and for at least 90 days. A manifest change that breaks
  the format comes only with a new `schema` number, and the old number is
  accepted for as long. Security fixes may close something at once, with notice.

## Security notes

- Treat everything the screen sends as untrusted; check it in the worker.
- Keep secrets out of the package and the screen: name them in
  `network.relay[].secrets` and use `{{secret:NAME}}` in headers.
- Text an app shows through Leafen (names, descriptions, notices, agent
  instructions) is drawn as plain text.
- Declare `requiresApproval` for actions that send mail, change permissions or
  do anything hard to undo, and mark `externalEffect` when they reach outside
  Leafen.
- Keep the signing key out of the app's folder and repository.

## Error codes

App calls fail with Leafen's usual error body, `{"error": {"code", "message"}}`.
The app platform's codes start with `LEAFEN_APP_` — for example
`LEAFEN_APP_PACKAGE_INVALID`, `LEAFEN_APP_MANIFEST_INVALID`,
`LEAFEN_APP_PERMISSIONS_CHANGED`, `LEAFEN_APP_NEEDS_NEWER_LEAFEN`,
`LEAFEN_APP_RELAY_DENIED`, `LEAFEN_APP_APPROVAL_REQUIRED`, `LEAFEN_APP_APPROVAL_DECLINED`,
`LEAFEN_APP_ACTION_INVALID`, `LEAFEN_APP_ACTION_REFUSED`, `LEAFEN_APP_ACTION_FAILED`, `LEAFEN_APP_KV_REFUSED`,
`LEAFEN_APP_ROOM_REFUSED`, `LEAFEN_APP_ROOM_FULL` and `LEAFEN_APP_REVOKED`; the full list is in
`contracts/app-platform-wire.json`.

A screen's call can also be refused in the browser before it reaches the
server; the SDK's error then has one of these codes and a sentence saying why:
`LEAFEN_APP_BRIDGE_LIMITED` (too many calls waiting), `LEAFEN_APP_BRIDGE_UNKNOWN_CALL`
(a call this place doesn't take), `LEAFEN_APP_BRIDGE_INVALID_CALL` (input it doesn't take),
`LEAFEN_APP_BRIDGE_POPUP_BLOCKED` and `LEAFEN_APP_BRIDGE_NEEDS_CLICK` (make it from a click),
`LEAFEN_APP_BRIDGE_NO_DATA_ACCESS` (no access to Leafen's data here),
`LEAFEN_APP_BRIDGE_NOT_FOR_GUESTS` (a guest has only the link's room, the store and notices),
`LEAFEN_APP_BRIDGE_READ_ONLY`, `LEAFEN_APP_BRIDGE_TOO_LARGE` and `LEAFEN_APP_BRIDGE_BLOCK_GONE`
(a block's `setProps`), `LEAFEN_APP_CREDENTIALS_OFF` and `LEAFEN_APP_APPROVAL_DECLINED`.
