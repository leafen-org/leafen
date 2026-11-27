# Architecture

Leafen is a single-server application built with Go, PocketBase, SQLite,
Ygo/Yjs, and React. One executable embeds the Web app and serves every supported
product interface.

## Runtime boundary

One Leafen process owns:

- the browser application and `/api/leafen/v1/*` product API;
- authorized collaboration rooms;
- durable background jobs;
- managed-file reads and writes;
- `/mcp` and its OAuth discovery and authorization endpoints.

The configured Leafen HTTP or HTTPS listener is the only supported public
boundary. PocketBase is linked as an internal Go library. Its installer,
administration console, superuser model, and raw collection APIs are not Leafen
product interfaces.

## Data ownership

The configured data directory is the deployment's durable unit.

| Data | Durable authority | Notes |
| --- | --- | --- |
| Accounts, workspaces, permissions, page records, jobs, and migrations | SQLite | Versioned through ordered migrations. |
| Page bodies (block order, content, and text) and database values | Ygo/Yjs collaborative documents | Persisted and authorized by the Leafen server; SQLite keeps projections of them for queries. |
| Managed file bytes and metadata | Leafen file storage and SQLite | Access follows the same workspace policy as pages. |
| People's pictures | SQLite | A photo is kept with the account (256 pixels square) at an unguessable public address; without one, a person shows a default icon, one of the Phosphor glyphs in `contracts/avatar-icons.json`, given when the account is made. The API's `avatar_url` is either address. |
| Search records, caches, and browser state | Derived projections | Rebuildable from durable authorities. |

Each logical value has one writable authority. A successful write means that
authority survives process restart; caches and delivery events are not commit
proof.

## Content model

Pages, their content, and database entries follow Notion's public block model
in one ID space:

- A page's body is an ordered tree of blocks. A sub-page is a page block in its
  parent's body.
- A database holds data sources, each with its properties and templates, and
  views that each show one data source, in one order. A data source's entries
  are pages whose parent is the data source; an entry's body is an ordinary
  page body.
- A full-page database is a page whose kind is database. It can turn into an
  inline database block and back.
- A synced block's copies show the original block's content.

## Authorization

HTTP routes, files, collaboration rooms, jobs, administration, and MCP share a
central workspace-scoped policy boundary. Missing, stale, or ambiguous
authority is denied. Individual routes do not define weaker local permission
rules.

MCP is a Leafen product interface. Each connection has fixed workspace scope and
is checked against current authorization when used. See [MCP.md](../MCP.md).

A person logged in on several Leafen servers in one browser sees all their
workspaces in the switcher. Each server's page holds, for every other server,
a list key the person allowed on that server: `GET /api/leafen/v1/switcher/list`
answers it with the person's workspace and app list (names, icons, guest,
unread) and nothing else, and no other route accepts it. A full login never
leaves its own server's pages. People see and take back the pages holding
their keys in Settings → Server accounts; the administrator can turn the
lists off. The packaged client asks the same route with the login it keeps
for each server.

The packaged app opens `leafen:` links: `leafen://open?url=<address>`,
`leafen://<host>/<path>` (the scheme of a server it knows, https otherwise),
and a page's own `leafen://page/<id>`; a desktop also takes a server address
as its one argument, and a second start hands its link to the running app.
A link to the active or a known server goes straight there; one to a server
the app hasn't used asks first. iOS and Android register the scheme in their
build files; a desktop installer registers it when it installs the app.

## Frontend contracts

The React app and Go server share machine-readable contracts under
`contracts/`. Generators produce matching Go and TypeScript declarations, so a
wire-shape change cannot be applied to only one side.

Modules under `web/src/` are the executable product interface. They retain
Leafen's established DOM, interaction, keyboard, focus, responsive, and
accessibility behavior while talking only to typed native Leafen adapters.

## Jobs and recovery

Accepted asynchronous work is recorded before the server responds. Jobs are
bounded, duplicate-safe, resumable after restart, and end in an explicit
completed or terminal-failed state. Persistent formats are versioned, and
backup/restore operates on the complete data root.

## Push notifications

A personal notification still unread ten seconds later becomes a desktop push
to each browser the person turned push on in: a Web Push message (RFC 8030)
signed with the server's VAPID key (RFC 8292) and encrypted to the
subscriber's keys (RFC 8291), so the browser vendor's push service carries what
it cannot read. Only those push services are accepted as endpoints. Leafen
runs no push relay of its own: a phone gets the same push in the browser, or
in Leafen added to its home screen. The packaged apps read
`GET /api/leafen/v1/account/desktop-notices` instead: a desktop's while it is
open, a phone's when the system wakes it for a background refresh, the window
having handed over the server, its workspaces and how far it has seen.

## Deployment model

Leafen is a single-server, vertically scaled application. Go schedules
independent request, collaboration, file, and background work across available
CPU resources; bounded queues, continuations, and SQLite transaction boundaries
keep heavy work from becoming an unbounded request. The server does not depend
on a separate application node, cache, message broker, or database service.
