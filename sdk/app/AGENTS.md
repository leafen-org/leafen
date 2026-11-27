# Writing a Leafen app — notes for agents

This folder is a Leafen app. Leafen's full guide is `docs/apps.md` in Leafen's
source; this is the short version. (Apache-2.0, part of the Leafen app SDK.)

## Layout

- `leafen-app.json` — the manifest; its JSON Schema is the SDK's
  `schema/leafen-app.schema.json`. `id`, `slug` and `version` (semver) identify the
  app; everything it may reach must be declared here, and the administrator
  approves exactly that list.
- `ui/` — the screen. `ui/leafen-app-sdk.js` is the SDK module; import it from a
  module script. Only files under `ui/` load in the frame.
- `worker/` (optional) — a Go module built to `workers/main.wasm`; the manifest's
  `workers[].module` names the built file. `worker/leafen/` is the SDK's helper.
  A Rust worker uses the SDK's `rust/leafen` crate instead and is built by hand
  (`cargo build --release --target wasm32-unknown-unknown`) into `workers/`; a
  JavaScript one is built with the Extism JS PDK's `extism-js` (see the Hello
  example's `worker-js/`).
- `data/` (optional) — setup steps for the app's databases and pages; the
  manifest's `data` lists each file as `{"key": …, "file": "data/….json"}`.
- `templates/` (optional) — blank files a file opener that edits starts new
  documents from (`surfaces.fileOpeners[].newFiles`).

## Commands

- `leafen app validate .` — Leafen's own checks; fix every problem it lists.
- `leafen app dev .` — a throwaway local Leafen that reinstalls on each change;
  log in with the printed account and open `/apps/<slug>`.
- `leafen app build .` — the `.leafenapp` file. `leafen app key` / `leafen app sign`
  sign it (the manifest's `developer.keyFingerprint` must name the key).

## Rules that matter

- The screen runs in a frame with no origin: no cookies, `localStorage` or
  IndexedDB, no top-window navigation, no forms posting elsewhere, no scripts
  from elsewhere. Talk to Leafen only through `connect()` from the SDK:
  `api`, `action`, `relay`, `kv`, `storage`, `room`, `guestLink`, `navigate`,
  `openLink`, `openPage`, `notice`, `connectAccount`; `link(path)` gives a full
  address to share. Keep the screen a single-page app. In place of browser
  storage, `storage` keeps a person's small JSON values on the server; the app
  doesn't work offline.
- `surfaces.blocks` go into pages from the "/" menu and `surfaces.sidePanel`
  opens beside a page; `context.place` says which, `context.pageId` names the
  page. A block keeps its settings with `setBlockProps` (`context.props`,
  `onProps`; untrusted input, 8 KB) and sets its height with `resize`.
- `slashItems` ("/" menu: an action or a block) and `menuActions` (`on`: row,
  page) run an action with `input` `{place, pageId, blockId?}` — accept it in
  the action's input schema.
- `joinRoom(name)` sends to a room's loop or function and hears what it sends
  the room; a guest's frame joins only its link's room.
- Read and write the app's databases by column name with the SDK's table
  helper: `leafen.table(key)` on the screen, `leafen.OpenTable(tenancy, key)` in
  Go, `leafen::data::Table::open` in Rust — `rows`/`find` (conditions, sorts,
  search), `count`, `row(id)`, `add`, `update` (only the named values change;
  options by name). A run has 200 host calls: read with conditions on options,
  people, rows or a checked checkbox (5,000 rows walked a call; otherwise 100)
  and count with `count` instead of reading every row.
- `GET /app-data` (where the data went, with each database's columns),
  `/app-people` (whom it may name, a window at a time: `?cursor=` the
  `nextCursor` while one comes, `?q=` finds, `?ids=` names those a list shows;
  `?groups=` with the manifest's `groups`),
  `/app-groups` (the organization's groups, with `groups`, a window at a
  time: `?cursor=` the `nextCursor` while one comes),
  `/app-workspaces` (the person's workspaces) and `POST /app-storage` (the
  person's store, for a worker running for them) are the app's own routes.
- `propertyEditors` (`types`, `entry`) draw database cells (`place`
  `"propertyCell"`, no clicks) and edit them (`"propertyEditor"`): `setValue` in
  the property's type, `closeEditor`, `onValue`. Values keep their type.
- `api(method, path, body)` calls `/api/leafen/v1` as the person, limited by
  `personAccess` (`read`, `content`, `structure`, `admin`). Some paths are closed
  to apps (publishing, view links, webhooks, automations, administration).
  `GET /workspaces/{id}/page-children?parent=` walks a workspace as a tree, a
  window at a time: `?cursor=` the `nextCursor` while one comes; `my-tasks?limit=`
  and `notifications?limit=` read the same way.
- Put rules in worker actions, not the screen: people can change what the
  screen sends. An action's `input` is a JSON Schema Leafen checks first; the
  worker receives `{action, input, person, tenancy}` (decode with
  `leafen.Action(&input)` in Go).
- Mark actions that are hard to undo `requiresApproval: true`; Leafen asks the
  person outside the frame, listing the input by field — give the fields
  names in each language with `inputLabels`. `tool: true` offers an action to
  agents.
- Outside addresses: list them in `network.direct` (the frame loads them
  directly) or `network.relay` (through Leafen). Secrets go only in relay
  headers as `{{secret:NAME}}` / `{{oauth:KEY}}`; never put a secret in the
  package.
- The temporary store (`kv`) needs a `ttl` on every new key and empties on
  restart; keep lasting data in the app's databases.
- A worker run has 64 MB, 10 seconds and 200 host calls. Host functions:
  `leafen_api`, `leafen_relay`, `leafen_kv`, `leafen_tenancies`, `leafen_log`,
  `leafen_notify` (a person's notice; the manifest declares `notifications`),
  `leafen_files_join` (stored files joined into one, as an endpoint's upload ends),
  `leafen_socket_open/write/read/close` (addresses in `network.sockets`; one the
  administrator sets is `{{setting:KEY}}` of an `address` in `settings`; a
  person's own credentials, declared in `credentials` and named on the entry,
  are written as `{{credential:KEY.FIELD}}` in a run for that person and come
  back masked — the app never reads them) and
  `leafen_wait` (long-running workers: `"longRunning": true` with `run` names
  the function Leafen keeps going; loop, rest with it and end when it says stop),
  `leafen_room_next`/`leafen_room_send` (a room's loop: `realtime.loop`). Know
  who sent a room's message from Leafen (`from`, `senders`, `RoomNextFrom`),
  never from what the message says. A host call Leafen refuses (an
  organization without the app's workspace, the API's error) answers `{error}`
  — an error from the Go and Rust helpers — and the function goes on; handle it.
- Text the app gives Leafen is shown as plain text. Never ask for a person's
  Leafen password.
- `minLeafen` is the oldest Leafen that installs the app; older servers refuse it.
- A `warning:` from `validate` or `build` names something Leafen will remove or
  change; it still works until the next minor release and at least the date
  given. Move off it before then.
