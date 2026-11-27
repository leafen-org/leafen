# Building on Leafen

Everything a person saves on screen can also be done through Leafen's native
API (`/api/leafen/v1`) and its MCP tools, by a script, an agent or another
program. The programs in this guide run outside the Leafen server. This guide
walks through the pieces; [MCP.md](../MCP.md) is the reference for each, and
[the meeting notes connector](../examples/meeting-notes-connector/) puts them
together. Apps that run inside Leafen — screens in Leafen and workers on the
server, which an administrator installs — are covered in
[Building Leafen apps](apps.md); a build without the app platform
(`leafen_noapps`) runs no third-party code. The phone apps' own servers and the
Mac App Store edition are built so: apps run on an always-on server, which the
phone apps reach as any other.

A program that brings another tool's content into Leafen writes a
[workspace bundle](bundle.md) — the format Leafen's workspace export writes and
its import reads — with each block's formatted text in it.

Leafen also keeps Notion's public API (`/v1/*`) and Notion's MCP (`/mcp`) for
programs written for Notion; this guide is about the native surface. That
Notion-compatible surface stays at the Notion API versions it supports now
(`Notion-Version: 2026-03-11`, and `2026-06-01` for the admin API) and does
not follow newer ones; new programs should use the native API and MCP.
It counts requests as Notion's Business and Enterprise plans do, in windows of
a minute: 600 public API requests for each connection, and 600 MCP tool calls
for each person (`notion-search` and `notion-query-data-sources` also 20 in
10 seconds each); there is no workspace-wide limit beside the server's own
limit for each address. Notion also limits each connection's data source
queries to about three a second; this server has no such limit of its own, so
a public API data source query counts only toward the connection's 600 a minute
and the address's limit. Inside them no answer is slowed down. A public API
request past its window is answered at once with `429 rate_limited`, the wait
left in whole seconds in `Retry-After` and `additional_data.retry_after`, and
the limit in `additional_data.rate_limit_reason`; an MCP tool call past its
window waits once when the wait is two seconds or less, and is otherwise the
tool's error with `code: "rate_limited"`, `retry_after_seconds` and
`rate_limit_reason`.

## Choose how to connect

| You are building | Use | It acts as |
| --- | --- | --- |
| A script or agent for yourself | An **MCP token**: Settings → **Agents** → **MCP address and keys** → **Create key**, for the open workspace (or **New MCP token** under the developer tools' **MCP tokens**; `POST /api/leafen/v1/mcp/connections` with one workspace) — or an **agent connection**, as an agent connects (Settings → **Agents**, `leafen connect`) | You: an MCP token in its one workspace with the tools its page leaves on, as Notion's; an agent connection with everything you may in all your workspaces |
| An app other people connect | A **registered app** that acts as the person: the developer tools' **Connections** → **New connection** → **OAuth**, then OAuth | Each person who allows it, "〈person〉 via 〈app〉" |
| A bot for a team | A **registered app with its own account**, installed by a workspace owner | The app's own account — a guest given pages, or a member |
| An agent that speaks MCP | The native MCP endpoint with a key or OAuth | As above; `leafen_operations` and `leafen_call` reach every operation |

The developer tools open from Settings → **Developer** → **Open developer tools**
(`/developers`).

No key of a person's has a grade to choose: it does what its person may, an
MCP token with the tools its page leaves on. What an operation needs reads by
what it does: `read`; `content` (pages, blocks, comments, files, database
rows); `structure` (database properties, views and automations, the sidebar,
teamspaces, sharing, webhooks); `admin` (members, workspace, organization and
server settings) — an MCP token's tools turned off hold its key to what the
tools left on need. A registered app asks for one of these scopes. An
organization can turn keys off, hold what they may do lower and require them
to expire. A Notion-compatible internal integration token (`/v1`) is a bot
of its own — only the pages shared with it, its name in history — and
Leafen's MCP doesn't take it.

## Make a request

Send the key or app token as `Authorization: Bearer …`:

```sh
curl -H "Authorization: Bearer $LEAFEN_TOKEN" https://leafen.example.com/api/leafen/v1/pages/$PAGE_ID
```

- **Bodies and answers are JSON.** A failure answers
  `{ "error": { "code": "LEAFEN_…", "message": "…", "reference": "LF-…" } }`;
  quote the reference when reporting a problem. Anything outside the key's
  workspaces answers 404, as it would on screen.
- **A busy server says so before doing anything.** When more requests come
  than the server works on at once, each waits its turn; one that waits too
  long answers `503` with `LEAFEN_SERVER_BUSY` and `Retry-After` — none of it
  ran, so send it again after that many seconds.
- **Writes are idempotent by their request id.** Operations take a
  `requestId` (or `mutationId` for content-graph changes); sending the same one
  again answers with the first result instead of acting twice, and reusing it
  for a different request is refused. Derive it from what caused the write —
  a webhook event's id, say — so retries are safe. A `mutationId` left out is
  made by the server, new each call, so such a call sent again acts again.
- **Some calls ask first.** A call that can't be undone (permanent deletion,
  sharing and permission changes, member changes, restores) answers
  `428 LEAFEN_USER_KEY_CONFIRMATION_REQUIRED` with a `Leafen-Confirmation`
  header once it checked the call would go through (one it would refuse is
  refused at once with why); send the same request again with that header
  within five minutes.
- **Edits are tracked.** A change made with a key or app names it in the
  page's version history and the organization's audit log, and "Undo
  〈connection〉 changes" in version history takes back what it changed
  without touching anyone else's later edits.

## Find the operations

`GET /api/leafen/v1/operations` answers the groups of what Leafen does —
each named for a task (`members`, `trash`, `backups-server-move` …) with one
line and how many operations your key can run in it; `?group=members` lists
that group's operations, those your key can run (`&all=true` adds the rest,
each with what it lacks); `?query=webhooks` finds operations by English words
in their names, summaries and keywords; `?detail=list` lists every operation
your key may run — its method, path, scope, whether it asks first, and the
wire contract that describes it; and
`?operation=POST%20/pages/{pageId}/view-links` adds the contract's request and
response records. Asked with a key, an operation also says whether that key
can run it (`this_connection`: `can`, `lacks admin`, `not with a key` …) and
the answer gives the key's own grant once (`connection`: its `kind` — agent
connection, MCP token or app —, what it reaches, whether it
reaches all your workspaces, whether you are the server's administrator, and
how to get more). The MCP tool `leafen_operations` answers the same. The
contracts themselves are the JSON files in [`contracts/`](../contracts/).

## Write page content

A page's blocks change through one content-graph mutation:

```http
POST /api/leafen/v1/pages/{pageId}/content-graph/mutations
{
  "mutationId": "meeting-notes:evt_123",
  "operations": [
    { "kind": "upsertBlock", "block": { "id": "<uuid>", "parentId": null, "afterId": null,
      "position": "a0", "type": "heading_2", "content": {} } }
  ],
  "textUpdates": [ { "blockId": "<uuid>", "spans": [ { "text": "Agenda" } ] } ]
}
```

You choose the block ids. A new block goes after `afterId`; with none, its
`position` orders it among the blocks the same mutation adds by position, and
they go after the blocks already there (`a0` puts one first). A text block's words go in `textUpdates`; words
given in its `content` instead (`rich` spans) become its text when it has
none yet, and its content keeps none. `GET /pages/{pageId}/content-graph`
reads the blocks, rows and views back. A mutation with a structural operation
(databases, properties, views) needs the `structure` scope.

A row (`upsertRow`) writes its place and its values. Its icon, cover, full
width and small text are its page's, and change with
`PUT /database-rows/{rowId}/page/presentation`; a row read and sent back may
carry them as it reads, or leave them out, and one carrying others is refused
with `LEAFEN_GRAPH_INVALID` naming the field. What a read tells of a row
besides — when and by whom it was made and last edited, its trash stamps,
`hasContent`, and what you may do with it — the server keeps, and a row sent
back leaves it as it is.

Read with an agent's key, `GET /database-rows/{rowId}/page` gives each
select's, multi-select's and status's options short: the first 10 and those
the row holds, its config's `optionsCut` saying how many it shows, how many
there are and that `?options=all` reads them all. A person's session and an
app's key read them whole. A config read so and written back
(`upsertProperty`) keeps the options it didn't show; one is deleted only with
`"delete": true`.

## Comment

A comment's body has one shape, `CommentBody` in
[`contracts/comment-wire.json`](../contracts/comment-wire.json): its words as
`rich` runs, each `{ "text": … }` with optional marks, and what a comment may
also hold (`attachmentIds`, a `quote`). A thread on a block's words names them
by `quoteStart` and `quoteEnd` in its block's text — in its caption with
`quoteField: "caption"` — and words chosen across blocks list the further
blocks in `quoteBlocks`; the words are marked as the thread's while it is
open. You choose the comment's `id` and the request's `requestId`:

```http
POST /api/leafen/v1/pages/{pageId}/comments
{ "id": "standup-2026-10-18", "requestId": "standup-2026-10-18",
  "body": { "rich": [ { "text": "Ready for review" } ] } }
```

A body of another shape is refused with `LEAFEN_COMMENT_BODY_SHAPE` and what
is wrong with it. A database row's page takes comments the same way by its
id, which is its row's — as Notion's API does — and so do its comments'
edits, reactions and thread states (`/database-rows/{rowId}/comments` is the
same place).

## React to changes

Subscribe a workspace to events — `page.created`, `page.content_updated`,
`page.properties_updated`, `page.moved`, `page.deleted`, `page.undeleted`,
`page.locked`, `page.unlocked`, `comment.created`, `comment.updated`,
`comment.deleted`, `data_source.schema_updated` — with
`POST /api/leafen/v1/workspaces/{workspaceId}/webhooks`, or in Settings →
Developer → Webhooks. The answer holds the signing secret once.

Check every delivery before trusting it: `Leafen-Signature: t=<unix
seconds>,v1=<hex>` is the HMAC-SHA256, under the secret, of `t + "." + body`.

```js
import { createHmac, timingSafeEqual } from 'node:crypto'

function verified(header, body, secret) {
  const parts = Object.fromEntries(header.split(',').map((part) => part.split('=', 2)))
  if (Math.abs(Date.now() / 1000 - Number(parts.t)) > 300) return false
  const expected = createHmac('sha256', secret).update(`${parts.t}.${body}`).digest()
  const given = Buffer.from(parts.v1 ?? '', 'hex')
  return given.length === expected.length && timingSafeEqual(given, expected)
}
```

Answer `2xx` once you've handled an event; a `429`, `5xx` or no answer is
delivered again after 10 seconds, then 1, 5 and 15 minutes, then 1, 3 and 6
hours. A subscription's events arrive one at a time, in the order the changes
happened, and only for pages its maker can still read.

## Register an app

1. The developer tools' **Connections** → **New connection**: a name,
   **OAuth** as the authentication method, its redirect URIs, and whether it
   acts as the person who allows it or as its own account (**Acts as**). Its
   page shows its **Client ID** (`lapp_…`).
2. Send people to `/oauth/authorize` with `client_id` (`lapp_…`),
   `redirect_uri`, `response_type=code`, `scope` (`read`, `content`,
   `structure` or `admin`), `resource=<origin>/api/leafen/v1`, a PKCE `S256`
   `code_challenge` and a `state`.
3. Exchange the returned `code` at `/oauth/token` (`grant_type=authorization_code`,
   the same `redirect_uri`, `client_id`, `code_verifier` and `resource`) for an
   access token and a refresh token.

People remove an app's connection in Settings → **Agents** (**Connected
agents**, its menu → **Disconnect**) or on its page under the developer tools'
**MCP tokens**; deleting the app (its page → **Delete**) ends every connection
made with it.

## See a page

The server runs no browser. For a screenshot or a visual check, ask for a
short-lived view link (`POST /pages/{pageId}/view-links` with a screen size,
theme, language and, for a database, a view) and open it in your own browser
at the width and height it gives back, sending the `secret` it gives apart
from the link in the `Leafen-View-Secret` header (MCP.md, "View links"); `&present=N` after the token shows one slide of the page's
presentation. Shared pages and published sites also
carry their title and cover in link previews, without a browser.

## License

This guide and its examples are licensed under the Apache License 2.0
([its text](../sdk/app/LICENSE)), as the app SDK and the connection contracts
are; Leafen itself is AGPL-3.0-only ([NOTICE](../NOTICE)).
