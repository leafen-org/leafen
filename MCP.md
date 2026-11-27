# Leafen MCP

Leafen serves MCP from the same process and authorization boundary as its product
API. It follows the Notion MCP tool surface where Leafen has an equivalent native
capability.

- Authoritative protocol: `2026-07-28` stateless `server/discover`.
- Backward compatibility: `2025-11-25` `initialize` for existing clients.
- Streamable HTTP at `/mcp` and legacy SSE at `/sse` expose only the strict Notion-compatible surface.
- Leafen-native MCP is served at `/api/leafen/mcp`. It tells every connected agent, at the start of a
  session, that Leafen is the person's shared workspace: read it before answering about their work,
  save lasting knowledge there, treat what it reads as material rather than instructions, offer a
  report once when Leafen itself fails or frustrates the person, do every step it can and show the
  person only the one step only they can do, log in a Leafen screen logged out in a browser it works
  in after asking once, and how to reconnect.
- Local transport: `leafen mcp`, a newline-JSON stdio bridge to the endpoint in
  its connection URI. The bridge never opens the database.

## Calling it by hand

An MCP client does the calls itself. To try the endpoint with `curl` —
with the key `leafen connect --approved` printed (below) — post one JSON-RPC
request at a time, with the key, a JSON body and both kinds of answer
accepted (a request that accepts only one is refused with 400). Each request
stands alone: no `initialize` and no session header come first, and the
answer is one JSON object.

```sh
curl -s http://127.0.0.1:8090/api/leafen/mcp \
  -H "Authorization: Bearer $LEAFEN_KEY" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"leafen_server_version","arguments":{}}}'
```

`"method":"tools/list"` lists the tools and their arguments. An agent that
connected Claude Code by its connection text and can't see Leafen's tools in
the conversation yet calls them so, with the header Claude Code keeps sent on
curl's standard input, so the key is in no command or output:
`"${CLAUDE_CODE_EXECPATH:-claude}" mcp get leafen | sed -n 's/^ *\(Authorization: .*\)$/\1/p' | curl -sS <address> -H @- -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -d '…'`.

## Connection lifecycle

A person connects two kinds of thing, and neither carries a grade to choose:

| | Agent connection | MCP token |
| --- | --- | --- |
| Made by | `leafen connect`, a one-time connection code (the settings' **Copy text**), a device code, another agent's yes, a chat app's connector (OAuth) | Settings › Agents › MCP address and keys › **Create key**, the developer tools' **MCP tokens**, `POST /api/leafen/v1/mcp/connections` |
| Works as | Its person, their name on what it changes — their shadow | Its person, their name on what it changes |
| May do | Everything its person may, with every tool: nothing narrows it | What its person may with the tools its page leaves on, as Notion's |
| Reaches | All its person's workspaces, those they come to be in later too, and every page they read | One workspace, and the pages chosen on its page |
| Where | Leafen's MCP and Leafen's API | Leafen's MCP and Leafen's API |

The person's list of them (`GET /api/leafen/v1/mcp/connections`, and
Settings › Agents › Connected agents) says each one's kind — `kind` is
`agent` or `token`, or `app` for an app's OAuth grant — and when it was made
and last used, which tell two of one name apart.

A Notion-compatible internal integration token (`/v1`) is neither: it is a bot
of its own, reaching only the pages shared with it, its name on what it
changes, and Leafen's MCP doesn't take it. An organization's key policy still
holds both kinds as it holds every key of its people, and what can't be
undone still asks the person in the conversation.

Asked for a connection — "just for this folder", "just for this tool" — an
agent suggests the one that fits what it is for (`leafen_operations`'
`agent-connections` group says the same):

- An agent that works for the person → an agent connection: as them, in all
  their workspaces, by a connection code or `leafen connect`.
- A key to hand to another program, narrowed → an MCP token: one workspace,
  with the tools and pages chosen on its page.
- An external service that uses the Notion-compatible API → an internal
  integration: a bot of its own, which Leafen's MCP doesn't take.

A folder several computers open — a NAS's shared folder, a synced one —
keeps in its own settings (`.mcp.json` and the like) only Leafen's address
and, if it needs a key, a narrowed MCP token: one workspace, the tools it
uses. An agent connection's key belongs to one computer and stays in that
computer's own settings or keychain — `leafen connect` adds it to Claude
Code for the user, or for one folder in that computer's settings, never
into the folder — and wide work there goes through each computer's own
agent connection. A folder copied or passed on whole takes the keys inside
it along.

Where a connection reaches several workspaces, a call that needs one and
whose target (a page, a database, a block) doesn't decide it — a page or
database made with no parent, `leafen_catalog` without `workspace_id` —
goes to none by itself: it is refused with the workspaces it reaches (ids
and names) and says to give `workspace_id`, chosen by the page the agent
works on, a search, or what it knows (its folder's rules, its memory), and
to ask the person only when it still can't tell. A connection with one
workspace works in it; a search covers them all. No connection keeps a
default workspace.

An agent on the computer Leafen runs on connects itself with the server's
program, with no browser and no Leafen page:
`leafen connect --name "<its program's name>"`, run with nothing added (it
changes no settings), answers with the question for
its person, in their language (which agent, as whom — everything they may,
in all their workspaces — and until when), which the agent asks in the conversation — in the first run,
`leafen setup status` gives that question itself, so the agent asks before it
runs anything to connect; only on their yes does
it run the command again with `--approved`, which makes an agent connection
for the server's first administrator (`--email` or `--account` for another
account), in all of that account's workspaces with everything it may do. For Claude Code,
`--approved --add-to claude-code` has Leafen run Claude Code's own `claude mcp add-json`
with the key, for every folder (`--this-folder`: Claude Code's settings for
the folder it runs in), and print none; where Claude Code isn't, as with
Leafen in Docker, `--claude-code-json` prints Leafen's entry alone for the
shell to hand to it,
`claude mcp add-json --scope user <name> "$(docker compose exec -T leafen leafen connect … --approved --claude-code-json)"`,
and says the rest on standard error. In a container the entry names the
port the host publishes it on, which `compose.yaml` passes in
(`LEAFEN_PUBLISHED_PORT`); a container not told it names the server's
public address, and with none the command says to give the address Leafen
is opened at with `--url` and makes nothing, rather than the container's own
`127.0.0.1:8090` nothing outside it reaches. Leafen's entry is named after the
server, `leafen-<server>-<code>` — its name in plain letters and its
three-letter code, from the server's own ID and kept through backups,
restores and moves (`leafen-<code>` when its name has no plain letters) —
the same in every folder and on every computer, so two servers of one name
differ; when the agent's settings hold that name already (another person's
connection on the same computer account, or an earlier one), it goes in
beside it as `leafen-<server>-<code>-<person>`, the first word of the
person's name in plain letters, else of their email. Each name keeps
Claude's tool names (`mcp__<entry>__<tool>`) within 64 characters, cutting
the server's name alone, and none is numbered or random; nothing Leafen gives
an agent to run renames, removes or overwrites an entry unasked. Before it takes a key, each command looks at
every entry the agent holds at this Leafen's address, whatever its name: one
whose key Leafen still takes as an agent connection's, for all its person's
workspaces, as the person connecting now
(`GET /api/leafen/v1/mcp/connections/current?all=1&person=<their account ID>`
with that key answers `204`; another person's key answers `403`), is kept,
and the command trades nothing and leaves the one-time code unused; only when
none works does Leafen go in beside them, and the command names those whose
key Leafen refuses. Leafen lists each connection by the agent's program and
the computer it was made on (and the folder it is kept to). An earlier one whose key Leafen
refuses, or one to this Leafen the agent won't use any more, the agent asks
its person once whether to remove; only on their yes it disconnects it with
its own key (`DELETE /api/leafen/v1/mcp/connections/current`) and takes it
out of its settings. A person also disconnects any connection in Leafen's
settings, under Connected agents. Another program gets the native MCP
address and the key with the lines to add them (`--json` for both steps).
Claude Code in a terminal loads a server added during a conversation once
its person types `/reload-plugins`, which gives the agent no turn, so the
answer has the agent ask for it followed by a word (“continue”) and go on in
the same conversation when that comes; Claude Code without a terminal (the
Claude app) shows the tools from the next conversation, and until then
`leafen mcp call [<tool> [<arguments as JSON>]]` calls them from the command
line with the key `claude mcp get leafen` gives (in Windows PowerShell, which
hands a program its argument without the JSON's double quotes, the JSON goes
on standard input as UTF-8:
`$OutputEncoding = [Text.UTF8Encoding]::new($false); '<arguments as JSON>' | leafen mcp call <tool> -`,
and Leafen's texts give that form beside the other; `--entry` names the entry
Leafen went in under beside an earlier one; `--schema` says what a tool
takes; a JSON object alone with a method and a path is `leafen_call`'s, and
any other JSON in the tool's place says the tool's name is missing, with
the call's form); where Leafen's program isn't beside Claude Code (Docker, ssh, another
computer), the answer gives the HTTP call under "Calling it by hand".
Leafen can't tell that yes from the agent's own, as with every question an
agent passes on; the program asking before it runs a command is the person's
too. Leafen hands out no link to the consent page below: it stays for clients
that open MCP's standard login on their own, such as a chat app's custom
connector.

The text a person copies in the settings' Agents (**Copy text**) carries a
one-time connection code (`lcc_…`, ten minutes, once; made with
`POST /api/leafen/v1/mcp/connection-codes`), which every agent that runs
commands trades first, wherever Leafen runs:
`POST /api/leafen/v1/mcp/connection-codes/exchange` with
`{"code": …, "name": "<its program's name>"}` answers with its key (`key`,
with `mcpUrl`); with `"answer": "claude-code"` it answers with Leafen's entry
alone, as `claude mcp add-json` takes it, and with `"answer": "codex"` with
the `url` and `http_headers` lines of a table in Codex's `config.toml`. So one
command trades the code and hands the key straight to the agent's settings,
`claude mcp add-json --scope user <name> "$(curl -sSf <origin>/api/leafen/v1/mcp/connection-codes/exchange -H 'Content-Type: application/json' -d '{"code": "lcc_…", "name": "Claude Code · <computer>", "answer": "claude-code"}')"`,
under the first name the agent's settings don't hold yet — `leafen-<server>-<code>`,
else `leafen-<server>-<code>-<person>` beside the entry there — so no
entry is removed or overwritten and no code is spent on one Claude Code
refuses; with every name taken nothing is traded, and a refused trade keeps
the code. Before that, the command keeps an entry at this Leafen's address,
whatever its name, that already works here — a key Leafen still takes for
all the person's workspaces, as that person — and trades nothing. Each command comes for
a POSIX shell and for Windows PowerShell, and the text says to run the one
for the agent's shell as it is: the PowerShell one hands no program JSON in
an argument, which Windows PowerShell 5.1's quoting breaks — Claude Code
gets `claude mcp add --transport http --scope user <name> <address> --header
"Authorization: Bearer …"`, its output kept back, and Codex's table is
appended to `config.toml` as UTF-8. A used code trades again within its ten
minutes while the key it gave was never used, for the same program's name
from the same address: that key is disconnected in the same step, so a
command whose save failed runs again without a new text. A key used once,
one its person disconnected, or another program's name, leaves the code
used. The
text then goes on in the same conversation — `/reload-plugins` in Claude
Code's terminal, the HTTP call under "Calling it by hand" elsewhere — and,
when nothing has come into the workspace yet (it holds no page but its
welcome page and no Notion move began there; the code's `offerStart`),
asks how to start: a Notion move begins with the agent making the Notion
connection in the same conversation, the key going through it to
`leafen_notion_migration_connect`, never through the import screen. Only a
chat app, which can't run commands, connects by a custom connector that
logs in (below). Claude's connector connects only to an https address
without a port (443): when the public address carries another port, the text
says so to the agent, and Settings' Agents and `leafen_remote_access` say
that only the connector fails there — browsers, Leafen's apps and agents such
as Claude Code work at the address as it is.

An agent already connected connects a folder, or another program, to Leafen
itself, from any computer — `leafen connect --this-folder` where Leafen's
program isn't: `POST /api/leafen/v1/mcp/connection-codes` with its own key
(`clientName`) makes a one-time code for an agent connection, and the
answer's `next` gives one command for a POSIX shell and one for Windows
PowerShell that, run in that folder, trades the code and hands what it gives
straight to Claude Code's settings for that folder alone (local scope), the
key in no output — keeping instead a working entry at this Leafen. The
folder's own rules (its `CLAUDE.md`) say which workspace it works in.
`leafen_operations` lists it in its `this-connection` group (English words
such as "connect folder" find it too). Claude Code's own MCP login is the
road for a person already on that screen.

An agent on another computer with no connection yet connects without a key,
as in OAuth's device authorization (RFC 8628): `POST /api/leafen/v1/mcp/device-connections`
with `{"name": "<its program's name>"}` answers with a `user_code` for its
person (eight digits after the server's code, ten minutes, once), a `device_code` it keeps and polls
`POST /api/leafen/v1/mcp/device-connections/token` with (`{"device_code": …}`,
every `interval` seconds: `authorization_pending`, `slow_down`,
`access_denied`, `expired_token` or `invalid_grant` until it is decided;
`server_error` is the server's failure, and it polls again), and
what to tell its person. The person allows the code in their conversation
with an agent already connected — "Allow the Leafen connection QXR·1234-5678" —
whose `leafen_connection_requests` takes the code (without one, it lists the
agents waiting, never their codes) and asks the person once more, as every
question here is asked (by the agent in the conversation), with the other
agent's name, the address it asked from and when; `decline` declines without
asking. With no agent connected yet, the
person types the code in Leafen's settings — its phone app's too (Agents, The
text and other ways, An agent on another computer) — logged in themselves,
never with a key (`POST /api/leafen/v1/mcp/device-connections/find` and
`/decide`): the confirm shows the same question and is their yes. Either
way the new connection is an agent connection, reaching all their
workspaces with everything they may do, so only an agent connection (one
reaching all its person's workspaces and pages, not an MCP token) allows it
from a conversation. Allowed, the next poll makes the connection and gives `access_token` once, with the native MCP address and
the lines to add it; its person sees the key again on the token's page. While
that key is unused, a poll within the request's ten minutes gives a key
again and disconnects the first, so a failed save is tried again. A poll
with `"check": true` takes nothing: once allowed it answers
`{"status": "allowed", "message": …}`, whose message gives Claude Code the
line that polls with `"answer": "claude-code"` — answered with Leafen's entry
alone, as `claude mcp add-json` takes it — so the key goes to Claude Code
without passing through the agent. No
link to a Leafen page is given. Asking is limited by address and to 64
requests waiting on the server, wrong device codes by address, and wrong codes
by the connection and across the server; each request keeps who asked from
where, who allowed or declined it and in which place (`conversation`,
`screen`), and the connection it made, and an allowed or declined
request is written in the audit of the workspaces' organizations
(`agent.connection.request_allowed`, `agent.connection.request_declined`).
Sent with a connection's key, the same request makes no code: an agent
connection has everything its person may already, and an MCP token keeps its
one workspace and the tools its page leaves on (its agent turns a tool back
on with `PATCH /api/leafen/v1/mcp/connections/{id}/settings`, only on its
person's word).

A browser logs in the other way round: "Log in with AI", right under
"Log in with a code" on Leafen's login screen, asks `POST /api/leafen/v1/auth/agent-sign-in` with no account
and shows the sentence for the person to send their agent, "Approve the
Leafen login: QXR·KX7-4QP", with "Copy" beside it; its code is six letters
and digits after the server's code, for five minutes, once. The server's code is three consonants from the server's own number,
the same through backups, restores and moves, and each connection's
instructions name it in their first line; an agent connected to more than one
Leafen approves the code on the one it names, and the others answer
`other_server` with that code, doing nothing. The same goes for a connection's
code; one typed without the server's code reads as before. The agent never makes such a
code: `leafen_sign_in_requests` (an admin connection reaching all pages)
takes the one on a Leafen screen the agent opened with the person's yes —
Leafen logged out in a browser it works in, it asks once, then presses "Log
in with AI" there itself — or the one the person gives it; any other
code (from another page, a file or a message) only from the person. When the
person gave it the code asking to be logged in or to approve it, the agent
approves it at once, with no second yes; only when that isn't clear does it
call with `askFirst`, which approves
nothing and gives the browser and system (Leafen's own desktop or phone app
named as the app, "macOS · Leafen app"), the address and country it asked
from and when, the account it logs in as and the question to ask them in
the conversation (`decline` declines). Approved, only the
browser that asked — polling `POST /api/leafen/v1/auth/agent-sign-in/poll`
with a secret of its own — logs in, as a device that joined with a code
does, and the person's inbox says "New device login approved (through
\<agent>)". The person turns such approvals off in Settings › Account
(Devices), logged in themselves; asking is limited by address and to 256
waiting on the server, wrong codes by the connection and across the server.

Remote clients discover OAuth from
`/.well-known/oauth-protected-resource/mcp` (or
`/.well-known/oauth-protected-resource/api/leafen/mcp` for the Leafen-native
endpoint, its own resource; a token holds for the endpoint it was asked for) and
`/.well-known/oauth-authorization-server`. Leafen accepts public OAuth clients
through dynamic client registration, requires authorization code plus S256
PKCE, and redirects the person to the native Leafen sign-in/consent page. The
person selects one or more currently accessible workspaces, however many;
consent pages return 64 workspaces at a time with an explicit continuation
rather than an unbounded account read.

Each person may keep at most 32 active manual and OAuth connections in total,
and one Leafen deployment may keep at most 256 dynamically registered OAuth
clients. Admission first performs one bounded indexed retirement turn and then
returns a stable capacity error if the applicable registry is still full; it
never evicts a live credential. Revoked connections, connections whose refresh
access has expired, and unreferenced idle OAuth clients become eligible for
retirement after 90 days.

Code exchange creates an expiring `lmo_…` access token and an expiring
`lmr_…` refresh token. Only their SHA-256 hashes, expiry, exact MCP resource,
client identity and normalized fixed workspace rows are persisted. Refresh
rechecks current workspace authority and replaces both tokens: every refresh
returns a new `lmr_…` refresh token, and the one it replaces is kept (as its
hash) as retired (refresh token rotation, RFC 9700 4.14.2). A client that lost
a refresh response may retry with the replaced token for one minute and gets a
new pair; a replaced token sent after that minute is taken as stolen and
ends the connection — its access and refresh tokens and what it made — so the
person connects again. The refresh token's expiry stays the one set when the
connection was made. The OAuth revocation endpoint durably disables both. When Leafen runs behind a proxy that
does not preserve the public scheme and host, set `LEAFEN_PUBLIC_URL` to the
external origin so discovery, issuer and resource identifiers stay exact.

For manual/stdio use, an authenticated person creates a connection with
`POST /api/leafen/v1/mcp/connections`, providing a unique `requestId` of at
most 255 characters, a client name and a fixed, non-empty `workspaceIds` list.
Retry the exact request with the same `requestId` if the response is lost;
Leafen returns the same connection and rejects reuse for changed input or a
revoked completion. Every selected workspace is checked through the central
Leafen permission boundary. The response exposes the `lmc_…` key inside one
versioned connection URI:
`leafen-mcp+https://host[:port]/mcp?v=1#key=lmc_…`. Each call is checked
against the key's SHA-256 hash; the key itself is kept sealed with the
server's settings key (in `.secrets`, as its other stored secrets), so the
person who made it, logged in, sees it again on its token page
(`POST /api/leafen/v1/mcp/connections/{connectionId}/token/reveal`, never with
a key and never by an administrator). A key made before keys were kept and
never refreshed is sealed as the server upgrades, when its MCP key file
(`.secrets/mcp-oauth.key`) makes the same key again from the connection's id
and its hash matches; any other shows again once it is refreshed. Revoking a
connection drops its sealed key. The
endpoint is an editable routing field; changing it never rebinds the
server-held key to an origin or
audience, so the same key can reach the same Leafen authority through a local,
private, forwarded, or public address until revocation.

Use the URI as the single `LEAFEN_MCP_CONNECTION_URI` value for `leafen mcp`.
Loopback and private IP destinations may use the `leafen-mcp+http` form. Public
destinations require `leafen-mcp+https` and the explicit
`--confirm-public-destination` setup flag. The stdio bridge rejects malformed
values and never follows a credential-bearing cross-origin redirect. An
ordinary Leafen user session is intentionally not an MCP credential. Revoke a connection with
`DELETE /api/leafen/v1/mcp/connections/{connectionId}`; revocation is durable
and applies to both transports on their next request. An open legacy SSE stream
is connection-bound and ends as soon as revocation commits. Reconnecting the
same connection replaces its prior SSE stream, so retained session state never
outgrows the admitted durable connections.

Strict tools keep Notion's schemas, which name no `workspace_id`. Page IDs
resolve their workspace inside the fixed connection scope, while
`notion-search` and `notion-get-users` may take `teamspace_id`: a team
(teamspace) from `notion-get-teams` chooses the workspace that holds it, and
one of that tool's `workspaces` chooses that workspace; an omitted teamspace
resolves automatically when the connection contains one workspace. A tool
that takes no `workspace_id` still takes the connection's one workspace given
as it and drops it; any other is refused with why.

Leafen's own tools name what they share one way: `workspace_id` for the
workspace, never required — left out, the connection's one workspace or the
one the IDs given are in (`leafen_search` alone also takes `workspace_ids`,
several, and searches them all when given neither); `query` for the words to find; `start_cursor` for
the `next_cursor` an answer gave; `page_size` for how many an answer gives.
The server checks its tools against this as it starts. A `workspace_id` given
must be in the immutable connection scope and must still pass current central
authorization. `leafen_list_workspaces` returns only that currently authorized
fixed set.

On a connection reaching several workspaces, `leafen_search` searches the one
named in `workspace_id`, the several in `workspace_ids`, or, naming neither,
every one the connection reaches — where Notion's connection reaches one
workspace. A call searches five of them, those the person opened a page in
last first, and `next_cursor` goes on to the others once those run out;
`notion-search` searching all of them does the same and says so in its
`notice`. Each `leafen_search` result names its `workspace`, and the answer's
`workspaces` gives how many workspaces it has searched so far of those asked
(`searched`, `asked`) and of those the connection reaches (`reachable`), and
its results by workspace. Leafen keeps no choice of workspaces between calls:
the agent names those it knows hold what it looks for, and searches all only
when it doesn't. `leafen_catalog` left without `workspace_id` on such a
connection, as every call that needs a workspace its target doesn't decide,
is refused with the workspaces it reaches (Connection lifecycle above).
`leafen_list_workspaces` lists them, with when each was made, and says in
`note` when a call takes `workspace_id`. A connection text pasted to an agent
that holds a working agent connection already keeps that connection: the
text's commands send its code with that connection's own key to
`POST /api/leafen/v1/mcp/connections/current/code` `{"code": "…"}`, and the
code stays unused.

`notion-search` reports `type: "workspace_search"` for native page title and
content search. Each result gives, as Notion's, its `id`, `title`, `url`,
`type`, `highlight`, `timestamp`, `path` and `verification`, and a database's
page — a row — the `parent_data_source_url` of the data source it is in. A
database is named by its own ID and address, full-page or inline; a result
whose body holds none of the words is highlighted by its title; `timestamp` is
its last edit to the minute. As Notion's, the answer is `results` and `type`
alone; `has_more` and `next_cursor` (Leafen's own) come only while there is
more. It takes the official options: `filters` (`title_only`,
`created_by_user_ids`, `edited_by_user_ids`, `created_date_range`,
`last_edited_date_range`, `teamspace_ids`, and `content_status` of
`all_without_archived` by default, `all_with_archived`, `verified_only`, or
`archived_only`), `page_url` for a page and its sub-pages, `data_source_url`
for one data source's pages, `sort` (`relevance`, `last_edited`, `created`,
newest first), `max_highlight_length` (0 to 500, default 200), and
`page_size` (1 to 50, default 10). Every option is available to every
workspace; user IDs are the ones `notion-get-users` returns, or `self`, and a
team from `notion-get-teams` in `teamspace_id` or `teamspace_ids` keeps search
to that teamspace, and a workspace's own ID there means all of it.
A page's search text includes the words of its pdf, docx, xlsx, pptx, HWP
and HWPX files — a database page's also those its file properties hold —
which the server reads once in the background soon after they are uploaded;
whoever may read the page finds them, and they go with the file. A document
longer than search reads (2,000,000 characters) is found by its beginning
only, and one past 64 MB not by its words; the app's search results and
documents list say so.
Archived pages, and database pages that are archived or sit in an archived
page, stay out of results unless asked for. An empty `query` browses the
workspace by those filters or a time sort, most recently edited first unless
another sort is chosen; with neither it is refused. Set `query_type: "user"` with a
non-empty name or email to route through the same current
Notion-compatible user directory used by `notion-get-users`; that response
reports `type: "user_search"` and, as Notion's, one result whose `text` is
`<users-search-results>` with a `<user id="{{user://…}}" name="…" email="…"/>`
line for each person found. Both modes accept at most 50 results and keep
their continuation bound to the selected fixed workspace, principal, query,
options, and current permission/search revision. One call goes on searching
for up to ten seconds; a search still going on after that answers with no
results, `has_more`, `next_cursor` and a `notice` saying it is still searching
and to call again with `start_cursor` set to `next_cursor` (`leafen_search`
also says `indexing: true`). A `backlog` says recently changed pages are still
waiting for the index and may be missing. Leafen does not reroute
keyword searches to an AI or connected-source service.

The three file tools also resolve an omitted workspace only when the connection
contains exactly one currently authorized workspace. `notion-create-file-upload`
issues a five-minute, connection- and credential-bound URL for one `file`
multipart field of at most 20 MiB; its answer's `upload_request` says how the
file goes there (POST, `multipart/form-data`, the `file` field) with a curl
line. A request to the URL by another method, or whose body isn't one file in
that field, is refused with the same way (`LEAFEN_MCP_FILE_UPLOAD_METHOD`,
405, or `LEAFEN_FILE_UPLOAD_INVALID`, 400). The URL needs no key or header,
so an agent never reads its connection's credentials to send a file; the
native upload operations (`POST /files`, `POST /files/uploads`) say so beside
them in `leafen_operations`. The upload's `file_upload_id` goes in a files
property by the property's name (`{"Files": ["<file_upload_id>"]}`; Notion's
API form `{"files": [...]}` and one file alone are taken too) or, as
`fileId`, in a native row-values write. The upload response and
`notion-create-attachment` return a `file-upload://` Markdown source;
`notion-download-attachment` reads only an at-most-200-KiB UTF-8 text upload
created by the same MCP connection. Direct URL attachments use public HTTPS,
do not follow redirects, send no caller headers or cookies, and are capped at
5 MiB. Leafen's product-wide active-web-file policy still rejects HTML, XML,
SVG, CSS, and executable script types even where Notion accepts a sandboxed
variant; no raw storage path or MCP credential is returned.

Page Markdown has its specification where Notion's MCP keeps its own: the
resource `notion://docs/notion-flavored-markdown-spec`, which `notion-fetch`
also returns for that URI. Leafen serves its own syntax there — the page
Markdown Notion writes now (2026-10-29): CommonMark with GitHub's tables,
and tags around a block that has a color or holds others (`<p color>`,
`<quote>`, `<toggle>`, `<callout>`, `<columns>`, `<tabs>`, `<table>`),
`<b>`/`<i>`/`<s>`/`<u>`/`<code>` in text, `<mention url="…" />`, `<date … />`
and `<p />` for an empty paragraph — and each tool that takes page Markdown
(`notion-create-pages`, `notion-update-page`, `leafen_update_page`) says to
read it first, as Notion's do. The Notion-compatible API's page Markdown
(`markdown` on `POST /v1/pages`, `PATCH /v1/pages/{id}/markdown`) is the same.
What Leafen wrote before (`<details>`/`<summary>`, `{color="…"}` line ends,
`<empty-block/>`, `<mention-page>` and the other `<mention-…>` tags, blocks a
tab further in) still reads; a toggle opened as `<details><summary>…` on one
line and never closed is refused. An equation is the lines between two `$$`
lines; `$$x$$` on one line stays text, and `> [!NOTE]` is a quote, as in
Notion. The native API's `imports/markdown` reads a Markdown file instead —
CommonMark with GitHub's tables, task lists and strikethrough, as other tools
export it.

A block's color is written and read as Notion's page Markdown does: in the
`color` of the tag around the block (`<p color="red">`, `<h2 color="blue_bg">`,
`<ul><li color="green">`, `<toggle color>`, `<callout color>`), a background
named `…_bg`.

Page Markdown holds files as Notion's page Markdown writes them: an image
is `![caption](address)` and a file, PDF, video or audio file
`<file source="address">caption</file>` (`<pdf>`, `<video>`, `<audio>`; `src=`
reads too); a file or PDF block still waiting for its file is
`<file source=""></file>`, and as in
Notion a line neither writes nor takes the block's color. A stored file's
address is a five-minute download URL of the reader's, under the server, with
`#block=…&file=…&name=…` after it naming the block and file. A line written
back as it was read keeps its block, whenever it was read; Markdown that
`notion-create-pages` or `notion-update-page` writes places a new stored file
only from `file-upload://ID`, an upload this connection finished in the same
workspace, or a file already on the page, and refuses any other; a line whose
caption changed keeps its block and changes only its caption, as in Notion. A
web address makes an external file. A caption's mentions are checked as the
block text's are.

`notion-duplicate-page` starts the same durable asynchronous duplication job as
the product API, preserving the source page's current parent. The initial
response returns a preallocated target page ID and task ID but does not claim
that the copy is ready. Poll that task ID with `notion-get-async-task`; only a
`succeeded` result means the target is populated. Both calls recheck the current
fixed workspace scope, write authority, governance, and originating MCP
connection. A page in the trash, or under a page in the trash, is not
duplicated, and sub-pages in the trash are left out of the copy. As with
Notion's API, a lock on the source or its parent does not stop an MCP
duplicate; the copy is never locked. A database page is copied as the app's
duplicate copies it, with its sub-items, into its data source right after it
and titled with " (1)", as Notion's MCP copies one; that copy is made when the
call answers, so the answer is the copy itself (`object` `page`, `status`
`succeeded`) with no task to poll. `page_id` takes any ID or address the other
tools take.

`notion-create-pages` makes one to 100 pages under one authorized parent page,
database or data source, or as private pages at the workspace's top. Pages
without sub-pages or databases in their `content` commit together in one
transaction, through the ordinary page creation receipts, DLP checks, history
baseline, search indexing, legal-hold interaction, parent lock/trash policy and
sibling positioning. A `<page>` or `<database>` tag in the content makes a real
page or database, written right after the page is made as `notion-update-page`
writes it.

`notion-move-pages` supports the corresponding page-parent vertical. Pass one
authorized `new_parent.page_id` and one to 100 unique page IDs in
`page_or_database_ids`. Leafen appends them beneath that parent in caller order
and commits the entire batch in one transaction through the ordinary page
metadata receipt, cursor, hierarchy, lock, trash-snapshot, wiki-fence, history,
search, file-reference, and legal-hold owners. A progress-token retry recovers
the same receipts; changing any page or the destination with that token is
rejected. A data source as `new_parent.data_source_id` takes pages, which
become its items, and a database's items, which move there as rows move into
another database (`POST /database-rows/{rowId}/page/move`), their values kept
where its properties take them; a move that would leave values behind is
refused naming them, and nothing moves, unless the native endpoint's call
gives `drop_values: true`. Database objects are not moved by it.

`notion-query-data-sources` currently supports the official single-source
`rows` vertical through the official `data` envelope. Pass
`data.mode: "rows"`, one `data.data_source_url` in `collection://ID` form,
and an optional `data.limit` from 1 to 100 (default 50). Results give each
page's values as `notion-fetch` gives them, keyed by the columns of the SQLite
table fetch describes for the source (a date as `date:<name>:start`, `:end` and
`:is_datetime`, a checkbox as `"__YES__"`/`"__NO__"`, options by name; empty
values left out, empty text as `""`, a list as its JSON text, a formula's or
rollup's result as it stands), plus `url`. Rows mode also takes Notion's
`data.filter` (a group of property filters, one nested group level) and
`data.sort`. The tool delegates to
the ordinary database query with its fixed page authorization and
computed/relation evaluation: the source's first rows in its own order come
at once. A source whose formulas or rollups read many related rows may
return `state: "pending"` with an empty result and an explicit
`next_action`; repeat the same call to advance the persisted bounded job.
`data.mode: "view"` with a `view://` address in `data.view_url` reads a saved
view's rows under its filters, filter bar and sorts, with `start_cursor` and
`page_size`.
With more rows than `data.limit`, a rows answer has `has_more` and a
`next_cursor`; the same call with `data.start_cursor` set to it gives the next
rows, until `has_more` is false (Leafen's own, as SQL's). The same envelope advertises
the current SQL fields (`mode`, `data_source_urls`, `query`, and optional
`params`). SQL queries the table `notion-fetch` describes for each source: its
`url` and `createdTime`, then each property's columns with the values fetch
gives (formulas and rollups have none). SQL accepts one read-only `SELECT` over
explicitly declared quoted
`collection://` table names, positional `?` parameters, bounded joins, unions,
grouping, aggregation, sorting, and a fixed safe function set. Every source is
authorized against its current page, fixed MCP workspace, exact source
container membership, logical property schema, and page revision before each
turn. Large snapshots return `state: "capturing"`, an empty result, and a
`next_action`; repeat the same call to continue the durable job. A completed
plan keeps every result row and returns them 100 at a time: with more, the
answer has `has_more` and a `next_cursor`, and the same call
with `start_cursor` set to it gives the next 100. It fails instead of
truncating when its 16-MiB staged-value, 1,000,000 estimated row-combination,
256-column, 16-MiB result, or two-second execution budget is exceeded. Client
cancellation, source/page/workspace
deletion, revision changes, restart, and 24-hour expiry cannot publish a mixed
snapshot. SQL output may omit rich-text mention text, link targets, and
formatting, matching the documented Notion SQL limitation; use rows mode or
`notion-fetch` before changing such a property. As Notion's SQL, a query may
start with WITH (inline rows as `WITH "ids"("x") AS (SELECT 'a' UNION ALL
SELECT 'b')`) and use a SELECT in parentheses in FROM, as a value, or in
EXISTS and IN; every FROM and JOIN in them reads only `data_source_urls`, a
WITH table where SQLite reads it as one, a SELECT in parentheses, or
`json_each`/`json_tree` of a column. Physical PocketBase/SQLite tables,
writes, DDL, comments, multiple statements, WITH RECURSIVE, VALUES, windows,
and unapproved functions are rejected. Leafen does not yet advertise rows
filters/sorts or saved-view mode.

`notion-fetch` answers as Notion's fetch does: `metadata.type` names what it
read (`page`, `database`, `data_source` or `view`), and `title`, `url` and
`text` give it, with a page's `icon`, `cover`, `path`, `verification` and
`page_last_edited_at` beside them. A page, database or data source without a
title is called in `path`, in `<ancestor-path>` and in a mention's words what
the screen calls it in the reader's language (`New page` / `새 페이지`, a row
by its database's word for its untitled rows, `New database` /
`새 데이터베이스`, `New data source` / `새 데이터 소스`), where Notion's
path leaves it out and its line says `title=""`; a database in a page or one
whose block keeps no name of its own is called there by its source, as its
path calls it; `notion-search` (where Notion's says `Untitled`),
`leafen_search` and `leafen_catalog` name untitled results alike. The
reader's language is the one Leafen writes that person every text in, by
one rule: the language they chose; for one who follows their device or has
not chosen, the language their own request names (an agent's path carries
none of theirs), else the language they last read Leafen in on a screen,
else the server's language (the one its administrator chose as they set it
up), else English. Where Leafen writes only Korean and English, as here, a
language other than Korean reads English. The questions Leafen puts to them,
their mail and the instructions' first rule read the same — one person
reads one language. A sub-page's or database's tag in the content keeps an untitled one's
title empty, as a tag's title written back renames it. It accepts a page or database ID, its
`/pages/ID` link (as fetch text and Markdown write them), a `database://ID`
URL, a `collection://ID` data source or a `view://ID` saved view. An uploaded
file is named by a stable `notion-file-block://<block>/<file>?space_id=…&name=…`
address, alike in every read, as Notion's: `include_file_urls` adds
`references`, each mapped to a download address, and
`notion-get-file-download-urls` takes the address exactly as written.

- A page's `text` names its URL and last edit, then `<ancestor-path>` (what
  stands above it, nearest first: a database page's data source and database,
  then pages, up to the first the connection can't read), `<properties>` (an
  ordinary page's title; a database page's values as its data source's SQLite
  table holds them, as `notion-update-page` takes them back, with each
  formula's and rollup's value as `notion-query-data-sources` rows mode gives
  it, where Notion's names it by a `formulaResult://` or `rollupResult://`
  address its fetch doesn't open; `leafen_fetch` gives the same values in
  `properties`, and a version's rows from `notion-view-version` leave formulas
  and rollups out),
  `<iconMetadata>` and its Markdown `<content>`. A database page's ends with
  `<parent-data-source-schema>`, its data source's SQLite table given short:
  every column in the table's order, the columns Leafen sets named read-only,
  but an option list's first 10 names and how many more, and no notes on how
  SQL reads a column. Notion's gives the whole table there, every option on
  every row read; Leafen's names the data source's own fetch for it, and
  says the person's time zone only where the table has a date column. A
  page whose title or text value reads escaped (a literal `~`, `_`… or a
  line break) is followed by one line on how such values read and write
  back; the others say nothing of it.
- A database's `text` lists every data source it shows, its own and those it
  links, each whole as its data source fetch gives it, and its saved views in
  tab order, each with its data source, displayed properties, name and type,
  as its view fetch gives it; it doesn't send the agent to fetch them again,
  where Notion's adds that their own fetch gives them whole. A data source's
  table names the person's time zone only while it has a date column or the
  times Leafen sets. The
  read authorizes the containing page and, in the same transaction, the exact
  source database's page of every linked source.
- A data source's `text` gives its configurable state (its properties'
  types, options, status groups and default option, relations, rollups,
  formulas and templates — a template with neither a title nor a name is
  listed as the screen's template menu calls it, by its database's word for
  its untitled pages or `New page` / `새 페이지` in the reader's language, as
  the template's own fetch heads it) and its SQLite table, whose status column notes
  its default (`Default: "…".`, or `Default: none.`) and whose files column
  takes file IDs only —
  Leafen has no folders; its `url` is its database's.
- A view's `text` gives its configuration in Notion's fetch words: a chart's
  `chartConfig` (what it groups and counts, and its `chartFormat`), its filter
  as `advancedFilter` with Notion's operators, a form's `questions`, and
  `showDataSourceTitle: false` where the view hides its source's title — what
  `configure` and the view tools write back.

`notion-create-database` makes a full-page database under `parent.page_id`, or
a private one at the workspace's top without it, as Notion's does; a
`<database inline="true">` tag in page content makes an inline one. `title` is
optional: a database without one shows its reader's words for it. The schema is
a `CREATE TABLE (...)` with quoted property names, and without a `TITLE` column
"Name" is added. The parser accepts Notion's DDL types: `TITLE`, `RICH_TEXT`,
`NUMBER` (with `FORMAT`), `SELECT` and `MULTI_SELECT` (with colored options),
`STATUS` (`'name':color:group`, the group `to_do`, `in_progress` or `complete`;
a bare `STATUS` gets the three options in its maker's language), `DATE`,
`PEOPLE`, `CHECKBOX`, `URL`, `EMAIL`, `PHONE_NUMBER`, `FILES`, `FORMULA`,
`RELATION`, `ROLLUP`, `UNIQUE_ID` (with `PREFIX`), the created and last-edited
time and person types, a `COMMENT` on any column, and what a person makes on
screen beside them: `BUTTON`, `PLACE` and `VERIFICATION`. The page, its data
source, table view and properties commit together through the ordinary Ygo graph
mutation, DLP, history, automation, projection and durable receipt owner.

`notion-update-data-source` accepts a `collection://ID` or bare native source
ID plus an optional non-empty `title` and/or up to 100 semicolon-separated
simple DDL statements. The bounded statement surface is `ADD COLUMN` with a
quoted name and type as create writes one (a name a column already has changes
that column, as `ALTER COLUMN SET` does), `RENAME COLUMN "Old" TO "New"`, and
`ALTER COLUMN "Name" SET <type>`, which may name options, all a select or status
then keeps (one already there keeps its ID and, for a status, its group), or a
formula; add and alter support the same types as create except `TITLE`, and
rename preserves the property's ID, order, type,
configuration, and every row value. `ALTER COLUMN SET` changes a property's
type the same way the public API does: existing cells convert in the
background by one shared value rule, cells the new type cannot read become
empty, and the original cells stay restorable for 30 days. The property takes
the settings the statement names, not those the editor's type menu carries
over or those of a type it had before: a select gets just the options listed,
and a column made a status is a new status, the options listed or, with none,
Notion's three in the asking person's language, its first the default (an
option named as one it had keeps that one's ID, so its rows keep it). Title, place and
relation types do not change this way. The complete batch and optional title
change commit through one ordinary Ygo graph mutation and durable receipt.
Changing a progress-token retry is rejected. `DROP COLUMN` puts a column in
Deleted properties. Trash, inline conversion, relation and rollup changes are
not advertised, because they require product semantics beyond this owner.
`page_layout` and `statements` go in separate calls, properties first: a call
with both is refused.
Both tools answer as Notion's do, `{"result": "Created database: <database …>"}`
or `"Updated data source: <database …>"`: the database as `notion-fetch` gives
it, its data sources' configurable state and SQLite table but not its views.

`notion-create-view` adds a view of one of a database's data sources: with
`database_id` a tab of that database, with `parent_page_id` an inline linked view
at the end of that page, with its own copy of its settings so the original
database's views stay unchanged. Leafen authorizes write access to the page that
takes it; a view of a source it only links needs read access to that source's
page. It starts as a person's new view of its layout does, and `configure` sets
its filters, sorts, grouping and display in the view language,
`notion://docs/view-dsl-spec`, which `notion-fetch` also returns. The types are
table, board, list, calendar, timeline, gallery, feed, form, chart, map and
dashboard. `show_data_source_title: false` hides the source's title above the
view, and with `database_id`, `position` (`start`, `end` or `after_view` with
`view_id`) puts the new tab where the API's view position does.

`notion-update-view` accepts a `view://ID` or bare native view ID and a new
`name`, `configure` in the same language, `show_data_source_title`, or several,
with `CLEAR` removing settings. It keeps the view ID, source, type, order and
all source rows, and a `view://` fetch reads the settings back. On Leafen's own
endpoint, `leafen_delete_view` takes a tab away, as a person's delete in the
tab's menu does; a database keeps one view of each source. Both tools commit through the ordinary
Ygo content-graph, authorization, DLP, history, automation, projection and
durable receipt owner.

On Leafen's own endpoint, `leafen_database_page_layout` reads a database's page
layout — Notion's "Customize layout" for all its pages, which Notion's API
doesn't give — with its revision and the source's property IDs (Notion's
original page until one is applied), and, given `layout` back changed, applies
it as the layout builder does, or `reset` takes it away. It needs a structure
connection and edit rights on the database; a revision older than the current
one is refused.

## Restoring a new server from backups

A new server with no administrator yet answers one more MCP endpoint while
its first screen leaves a restore to an agent ("Used Leafen before? Restore
it from a backup" → "Leave it to an agent"):
`/api/leafen/v1/setup/restore/mcp`, Streamable HTTP, with no login. Its
address holds no secret; every tool takes the one-time `restore_code` the
screen gave the person:

- `leafen_restore_status` — where the restore is, what was found, which of
  the fields the person typed on the screen (never their values), and what to
  do next. On a server set up already, it says so.
- `leafen_restore_find` — looks for the old server's backups in a folder the
  server reads, on a WebDAV server or in S3-compatible storage, and lists its
  continuous replica and snapshots. It takes the place only: the backups'
  password or recovery code and the storage's keys are typed on the screen,
  and a call that names them is refused.
- `leafen_restore_run` — restores the replica or a snapshot by its name; it
  runs on in the background and `leafen_restore_status` follows it.

The code opens nothing else — not the first administrator, nor any other
read or write — and ends once the restore is done, when the server gets an
administrator any other way, when the screen goes back, or after 30 minutes.
The same calls are JSON at `/api/leafen/v1/setup/restore/agent/status`,
`/find` and `/run`, with the code as `restoreCode`. A way in from outside
opened for the restore answers only these and the health check, and closes
with the code.

## More than one Leafen server

An agent can be connected to several Leafen servers — a person's own
computer and their team's server, say — each its own MCP server in the
agent's settings:

- Each is named after its server and its code, `leafen-<its name in plain
  letters>-<its code>` (`leafen-maple-kxb`, `leafen-home-tdm`), so two
  servers of one name differ; another person's on the same computer account
  is `leafen-<server>-<code>-<person>`. An entry made earlier, as plain
  `leafen` or another name, keeps its name. The desktop app's "connect" reads the
  agent's settings and picks the name itself; it changes only the entry of
  the server being connected, never another server's. Settings › Features ›
  Agents shows the name a terminal command takes, and the text for other
  agents tells them the same.
- The agent's instruction file keeps one Leafen line per server, by its
  address, between the `leafen` markers.
- Each server's connection instructions start with its name and address
  (`This Leafen server is named "Maple" and is at https://…`), so the agent
  tells the person which server it means. An agent that reaches the server
  at another address — an earlier one still forwarding, or this computer's —
  reads in that line that the server is at its own address now and that the
  one it reaches works too, and its instruction lines, which say to keep
  their address up to date, follow. Their first rule names the
  person's language by its own name where their Leafen isn't in English
  (`in their language (한국어)`). What Leafen's answers and guides hand on for
  the person is facts and points, never English words to relay: the agent
  says them in its own sentences, in the person's language, and quotes only
  another service's buttons and menus as its screen shows them.
- A server's name is its friendly alias (a plant, given when the server is
  made) until its administrators name it in Settings › Server › General ›
  **Server name**.
  Renaming changes what screens and new connections say; a connection's name
  and the server's Tailscale device name, once made, stay.

## One key for the native API and MCP

A connection's key is also the person's key for the native API. An agent
connection does everything its person may in all their workspaces, with every
tool and page — its page has no tools to turn off and no pages to choose, and
`PUT /api/leafen/v1/mcp/connections/{id}/pages` or a `disabledTools` change in
`PATCH …/settings` is refused for it (`LEAFEN_AGENT_CONNECTION_NOT_NARROWED`);
an MCP token, made by hand for one workspace (`workspaceIds` names exactly one: a
request naming several is refused, one token for each), does what its person
may there with the tools its page leaves on — turning a tool off on its page
holds its key on the API to what the tools left on need. No key has a grade
of its own to choose. A token never expires, as Notion's MCP tokens don't, unless an
organization of its workspaces holds keys to a shorter life (below); its
person ends it by refreshing or deleting it. Its page's "Content access" has
"All pages" on at first — every page its person reads, later ones too;
turned off, it reaches only the pages chosen there and those under them (none
chosen: no page), and tools and API calls that can't be held to pages are
closed. Send the `lmc_…`
value from the connection URI as `Authorization: Bearer lmc_…` to
`/api/leafen/v1/…`, and the call acts as the person who made the key:

- What an operation needs, by what it does: `read` reads; `content` also
  writes pages, blocks, comments, files and database rows; `structure` also
  changes database properties, views and automations, the sidebar,
  teamspaces and sharing; `admin` also manages members, workspace and
  organization settings and the server. A content-graph mutation needs
  `structure` when it carries a structural operation. An agent connection
  reaches all of it; an MCP token what its page's tools leave on; an
  organization's key policy may hold either lower. The same holds for MCP tools.
- The key reaches only its workspaces; anything else reads as not found —
  also where a call copies or moves something to, and the pages or sources
  it names besides the one it acts on. A route no rule names follows the
  person's own permission with the whole scope and a key for all of its
  person's workspaces.
- Login, passwords, emails, MFA, sessions, account deletion, the
  bootstrap list, keys, tokens and connections, first setup, joining and
  making workspaces are reachable with a key too, through the same handlers
  and the confirmations they ask for (the current password, an authentication
  code, the account's email typed out). These, and routes no rule names,
  cross a person's workspaces, so only a connection reaching all of them
  does them: an agent connection does; an MCP token, kept to its one
  workspace as Notion's are, gets `LEAFEN_USER_KEY_CLOSED` saying they take
  an agent connection (by a connection code or `leafen connect`); an app's
  token never does. SCIM's own protocol answers only SCIM tokens.
- What a handler does only for the person logged in is closed to keys up
  front, with its reason, and `leafen_operations` lists it as closed: what
  acts on a browser's own login (adding or removing a passkey, showing the
  current session's 2-step code), a token's key shown again and its pages,
  and another agent's connection allowed by its code (in a conversation,
  `leafen_connection_requests` allows it, asking the person). A connection's
  settings, its key made again and what it reaches besides Leafen — its
  services and apps, each service's tools and how they run — cross the
  person's workspaces like the routes above: an agent connection changes
  them, asking its person in the conversation where a change stops Leafen
  asking them.
- What a key makes — another key or connection, a connection code, an MCP
  client's login it approves, a login session — ends with it: when the key
  is disconnected, those stop working too.
- A person's key reaches what they do logged in, an app's credentials,
  choice, passwords, reads, run history and schedules too (an app's token
  doesn't). Logging in from Leafen's window (setting or
  forgetting a password, device codes, handing an address on, an app's
  browser login, settling a desktop server), a token's key shown again and
  its pages, a screen handed over to a person, and approvals on an app's own screen stay with a
  person logged in: a key there gets `403`
  (`LEAFEN_USER_KEY_CLOSED`) with the reason.
- What a person does in one workspace, a key for that workspace does under
  the same permission: a form is read, answered, sent its files and kept as
  a favorite through its token, within the form's workspace (configuring one
  needs `structure`, reading it `read`, answering it `content`); an item is
  passed on as its menu's Send does (`content`, without its share); the workspace's owner
  moves the workspace between servers (`admin`, sending it away confirmed). The
  server's administration and an organization's settings, reads included,
  take an agent connection, never an MCP token or an app's token; so do the
  `leafen_remote_access`, `leafen_server_move` and `leafen_standby` tools,
  which a token's tool list leaves out and refuses with the way to an agent
  connection. An agent
  connection reaches the workspaces its person makes, joins or is added to
  later too, whether one of their keys or their own screen did it.
  A route that takes only its own code — a move's code,
  a guest link, a hook's secret — answers a key as it answers anyone without
  that code.
- A key opens a page's collaboration socket
  (`/api/leafen/v1/collaboration/{pageId}?token=lmc_…`) within its workspaces,
  read-only below `content`; its edits name its connection in the version
  history.
- A call that can't be undone — permanent deletion, sharing and permission
  changes, member and owner changes, restores — waits for the token's
  person once Leafen checked it would go through (one it would refuse —
  what it names isn't there, a page not in the trash, a body that isn't one
  — is refused at once with why, and no one is asked): it answers `428 LEAFEN_USER_KEY_APPROVAL_REQUIRED` with an
  `approval` (its `approvalId` and the question in the person's words); the
  agent asks its person in the conversation and, only on their yes, sends the
  same request again with the `Leafen-Confirmation` header set to that
  `approvalId` (through `leafen_call`, its `confirmation`), or to
  `<approvalId>:hour` where they said yes for an hour, as the question
  offers when it may. Leafen asks no MCP elicitation form: a program may
  close one unseen. With the token's "Hand it
  all over (don't ask)" on, it answers
  `428 LEAFEN_USER_KEY_CONFIRMATION_REQUIRED` with a `Leafen-Confirmation`
  header instead, and sending the same request again within five minutes with
  that header carries it out. Its person turns that on on the token's page;
  its agent turns it on for its own token with
  `PUT /api/leafen/v1/mcp/connections/current/hands-all-over`
  `{"handsAllOver": true}`, which asks its person once, and off with `false`,
  at once (`GET` reads it); no key changes another token's. An agent
  checks its own connection with that connection's key,
  `GET /api/leafen/v1/mcp/connections/current` (`204`; with `?all=1`,
  `?workspace=` or `?scope=`, `403` when it reaches less), and
  disconnects it with that key,
  `DELETE /api/leafen/v1/mcp/connections/current` —
  only after its person said yes to removing it (an earlier connection
  Leafen went in beside that no longer serves).
- An administrator's safeguards ask the person every time, whatever the
  token's switch ("Hand it all over" too) and never for an hour: the apps'
  oversight and whose answer their tools take, which apps people may add
  (`/admin/apps/allowlist`), the app policies (`/admin/apps/settings`),
  connecting only through the server (`/admin/apps/strict`), and each
  app's running, people, guests, open address and people's credentials
  (`/admin/apps/installs/{id}/status`, `grants`, `guests`,
  `endpoint-public`, `credentials`); an organization's login policy, guests'
  2-step verification, account history, key policy (`user-key-policy`), the
  agents' programs it allows (`mcp-governance`), sharing and security
  policy, managed users' policies, SAML, SCIM, data loss prevention, audit
  log retention and releasing a legal hold; and the server's sign-up policy,
  adding workspace members, sign-up's email verification and its login
  providers (`PATCH /api/leafen/v1/admin/settings` — a change to only its
  other settings, its mail, map, updates or name, goes ahead as before). A
  question lists only the settings a request changes, each as what it holds
  now → what it will hold, and counts those it sends as they already are; it
  never shows a secret the request sends — a password, a client secret, an
  API key — and the request is kept with it hidden. Where the
  organization takes
  only the agent's program's own form answers for its apps (`PUT
  /api/leafen/v1/admin/apps/agent-form-answers`), only that form answers
  these there — a change across the server's apps, where any organization
  does.
- Loosening the person's own login asks them every time too, whatever the
  token's switch and never for an hour: turning off 2-step verification
  (`DELETE /api/leafen/v1/account/mfa/totp`), regenerating its recovery
  codes (`POST /account/mfa/recovery-codes` — the saved ones stop working and
  the agent gets the new ones), and removing a login email
  (`DELETE /account/emails`). The question says what it takes away, as it is
  now → after. A passkey is added or removed only in a browser logged in,
  never with a key. Adding the others, making one of the person's emails
  primary and ending sessions go ahead as before.
- Deleting the person's own account (`POST /api/leafen/v1/account/deletion`),
  which deletes every workspace they own and removes them from the others,
  asks them every time the same way, never for an hour: the question names
  the workspaces it deletes, with their members, and those it leaves, and
  says it can't be undone. A deletion that can't go ahead yet — the server's
  only administrator, a legal hold, an organization holding workspaces other
  people own — isn't asked about: it is refused at once with the reason the
  settings window gives.
- An organization decides, for its workspaces, whether keys are allowed, the
  highest scope and how soon a key must expire (an MCP token made there
  lives the shortest such life)
  (`GET`/`PUT /api/leafen/v1/organizations/{organizationId}/user-key-policy`,
  security administrators). A key above the highest scope is held to it, and a
  call that needs more is refused with the reason (`LEAFEN_USER_KEY_POLICY`);
  keys turned off or living past the policy are refused, also after the
  policy tightens.
- A change made with a key names the key's connection in the page's version
  history and adds a `connection.request` event to the organization's audit
  log.
- What a connection changed on a page can be undone apart from everyone
  else's changes: version history's "Undo 〈connection〉 changes", or
  `POST /api/leafen/v1/pages/{pageId}/history/{versionId}/revert` with
  `{ "requestId", "baseRevision", "connection": true }` from a version the
  connection made (without `connection`, that one version's changes). Each
  block and page field the connection changed goes back to how it was
  before, unless someone else changed it afterwards; the answer names what
  it `kept` and why (`changedLater`, `holdsLaterBlocks`, and `page` or
  `database` for sub-pages, databases and database values, which their own
  pages and databases undo).

OAuth tokens issued to MCP clients stay bound to the MCP resource they were
granted for and don't reach the native API.

### Every native operation through MCP

The native endpoint also lists and runs the native API itself, so an agent can
do whatever a person saves on screen:

- `leafen_operations` is where an agent finds what lies beyond its list's
  tools. The common work — reading, making and changing pages and rows,
  search, the trash — goes with the tools on the list. Called with no
  arguments, `leafen_operations` answers the groups of everything else, each
  named for what a person sets out to do (`members`, `trash`, `backups-server-move`,
  `organization-access` …) with one line and a few example operations, and
  how many of its operations and tools this connection can run (`count`),
  how many more need more permission (`more`) and how many only its person
  does on Leafen's screen (`person_only`). Given a group (`group`), it lists
  that group's operations — id, name and a one-line `summary` — and Leafen's
  tools in it: those this connection can run, and those only its person does,
  each with where (`this_connection`: `not with a key: … at <the screen> —
  <the step there>`), so the agent leads its person there rather than saying
  it can't be done; the rest are one line (`hidden`), and `all: true` lists
  them too, each with what it lacks.
  `query` is a shortcut: English words, found in each operation's and tool's
  name, summary and a few keywords, their endings trimmed (shares, sharing
  and shared are share), the best first with their groups; words in another
  script, or words nothing holds, are answered with the groups. Which group
  each operation and tool is in, its summary and its keywords are data kept
  beside the operations (`internal/app/native_operation_groups.json`), not
  rules that read a request. `detail: list` still lists every operation a
  key may run — its method, path, scope, whether it asks for confirmation,
  and its wire contract and name where one describes it — and those no key
  runs, their scope `closed:` with the reason. Given `operation`
  (`"POST /pages/{id}/content-graph/mutations"`), it returns that contract's
  request and response records; the method and a path with its values filled
  in, or the operation's name as a list shows it (`setStrict`), find it too,
  a name several share lists them, and a name nothing has is answered with
  what its words find. leafen_call given an operation's name as `tool` says
  the method and path to run it with. The list is read from the server's own
  routes, so an operation added later appears without a tool change (in a
  group `other` until the data places it); a route not yet classified for
  keys follows the person's own permission with the whole scope and is named
  once in the server log (a development build doesn't start with one). An
  operation it describes or lists in full says whether this connection can
  run it (`this_connection`: `can`, `lacks admin` or `lacks all workspaces`,
  `not with a key`, `not its person` for an administrator's, or `not a
  call: …` for what no call runs — the restore's own MCP endpoint, a socket
  kept open, a page a person's browser opens, what another program sends
  with a credential of its own — with why and the way that does it, which
  `leafen_call` also refuses it with; a group or a search says it only where
  it can't), and every answer gives the
  connection's own grant once (`connection`: its `kind` — agent connection,
  MCP token or app —, what its tools and organizations leave it, whether it
  reaches all its person's workspaces, whether they are the server's
  administrator, and in `widen` how it comes to do more — for an MCP token,
  an agent connection). `GET /api/leafen/v1/operations` answers
  the same over REST: `?group=`, `?all=true`, `?query=`, `?operation=` and
  `?detail=`.
- `leafen_call` runs one: `method`, `path` below `/api/leafen/v1` with its
  values filled in, an optional `query`, and the JSON `body`. It goes through
  the same router, checks, saving and history as the screen, as the
  connection's person and within its workspaces, and answers with
  the `status` and the JSON `body` (or `text`). An operation that can't be
  undone asks the person first, as above — `confirmation` set to the
  request's id passes their yes on; with "Hand it all over" on, it answers
  `428` with a `confirmation`, and calling again with the same request and
  that `confirmation` within five minutes carries it out. An answer of bytes
  (a file, an export, a backup) comes as `body_base64` up to 200 KiB; a
  larger one gives `download_url`, an address that downloads it without a
  key until `download_expires_at` (five minutes), while the connection holds.

- `leafen_call` also runs one of Leafen's tools by name: `tool` and its
  `arguments` instead of `method` and `path`. The server answers it as that
  tool, through the same scope, room and record checks, and gives the tool's
  answer as the `body`.
- A write whose body takes a `mutationId` (a page's change, its metadata,
  its content graph, a row's values, a database's mutations, automations,
  deleted ones too, and starter, an organization's exports and legal holds)
  takes one left out or empty, sent with a body or none: the server makes a
  new one at the API's edge, before the route reads the body. One given
  dedupes as before; without one, the same call sent again is made again.
  `leafen_operations` marks none of them required, and a refusal never names
  one left out: it names what is wrong.
- A row's values (`POST /database-rows/{rowId}/page/values`, and each row
  of `POST /pages/{pageId}/database-row-values`) are keyed by the property's
  ID, its public ID as the MCP tools give it, or its name where only one
  property has it; a value's `type` left out is its property's.
- A page's content changes (`POST /pages/{id}/content-graph/mutations`)
  answer with the `graphRevision` they made: the number
  `GET /pages/{id}/content-graph` reads as `snapshotRevision` and the next
  change passes as `baseGraphRevision`. The page's own `cursor` — in page
  answers and the receipt's `page` — is what `basePageCursor` takes; the two
  count different things. The receipt still writes `cursor`, the old name of
  `graphRevision`, for clients that read it, as unsyncing a synced block does.
- A version history restore or revert
  (`POST /pages/{pageId}/history/{versionId}/restore` or `/revert`) answers
  with the history's `snapshotFrontier` after it in its `receipt`: the number
  `GET /pages/{pageId}/history` reads as `snapshotFrontier` and the next
  restore or revert passes as `baseRevision`. The answer's `page.revision` is
  the page's own and counts something else. The receipt still writes
  `revision`, the old name of `snapshotFrontier`, for clients that read it.
- `leafen_operations` with `operation` names the record an operation answers
  with as `answer`, read from its handler, with that record's fields where a
  contract holds it.
- Its records — the body's (`input`), those its fields hold, its actions'
  and its `answer` — go by the names the wire contracts give them
  (`ViewLinkCreateRequest`, `ViewLink`), the same in every operation. A record
  no contract holds goes by the server's own name for it, often in lower case:
  it has no other, and the description gives its fields.
- A person's profile (`GET /workspaces/{workspaceId}/people/{userId}/profile`)
  answers with a summary: their ten newest activity entries and pages, with
  `activityCount` and `pageCount` for all of them and `detail` saying so;
  `?detail=full` gives every one, as the profile page reads it.
- `leafen_notion_migration_items` takes `page_size` to read fewer than a
  window (200, databases 50) at a time; `next_cursor` goes on from there.
- A guest invite link (`POST /workspaces/{workspaceId}/page-guest-links`
  with `pageIds`, `role` — `edit`, `comment` or `view` —, `expiresInDays`
  from 1 to 30, seven when left out, and `reusable` for more than one person) lets whoever opens it
  in as a guest of those pages: someone new gives a name and is in on that
  device, someone with an account logs in and takes it. Its answer's `url`
  is the only place its code shows; the server keeps its hash. The person
  making it needs full access to each page as a member of the workspace,
  under the organization's guest rules, and the call asks first, as a share
  does; a key needs the structure scope in that workspace, as for sharing a
  page. `GET /pages/{pageId}/page-guest-links` lists a page's links still
  usable, and `POST /page-guest-links/{linkId}/revoke` turns one off with
  the same scope in the link's workspace; one that isn't there, or that
  only its maker or a workspace owner may turn off, answers
  `LEAFEN_PAGE_GUEST_LINK_NOT_FOUND` naming it by its ID. A page among
  `pageIds` that an operation holds for a while — its blocks being moved,
  its copy, a wiki change — answers 423 `LEAFEN_PAGE_BUSY` naming the page
  (`target`) and what holds it (`cause`), as a page's sharing does; make the
  link again once that's done. Notion has no such link: it is Leafen's own.

## Event webhooks

A workspace's members subscribe to its changes with
`POST /api/leafen/v1/workspaces/{workspaceId}/webhooks`
`{ "requestId", "url", "eventTypes": [...], "description" }` — or in
Settings → Developer → Webhooks. The answer carries the signing secret
(`whsec_…`) once. A subscription can be listed, changed (`PATCH`, including
`active`), removed, sent a `webhook.test` event (`POST …/test`), and its last
50 deliveries read (`GET …/deliveries`, a failed one sent again with
`POST …/deliveries/{deliveryId}/retry`). A key needs the `structure` scope to
make one, and a subscription made with a key stops when the key does.

- Events: `page.created`, `page.content_updated`, `page.properties_updated`,
  `page.moved`, `page.deleted`, `page.undeleted`, `page.locked`,
  `page.unlocked`, `comment.created`,
  `comment.updated`, `comment.deleted`, `data_source.schema_updated`. As
  Notion's do, changes of one kind to one page or data source five seconds
  apart or less are one event — content, properties, a move, a schema —
  listing every block (`updated_blocks`) or field (`updated_properties`)
  they touched; a page's first property change goes at once, and the ones in
  the five seconds after it go together. A page sent to the trash and brought
  back within five seconds, or brought back and sent again, sends neither. A
  page made, a lock and a comment go at once, alone: the changes right after
  a page is made are their own events.
- Each delivery is a `POST` of `{ id, timestamp, workspace_id,
  subscription_id, type, authors, entity, data, attempt_number,
  api_version }`, where an author made through a connection names it. The
  `Leafen-Signature: t=<unix seconds>,v1=<hex>` header holds the HMAC-SHA256,
  under the secret, of `t + "." + body`; `Leafen-Webhook-Id` is the event's
  id.
- A subscription's deliveries go out one at a time, in the order the
  changes happened. A `2xx` answer delivers one; `429`, `5xx` or no answer
  retries it after 10 seconds, 1, 5 and 15 minutes, 1, 3 and 6 hours, then
  fails it; another answer fails it at once. The next one waits until then.
- An event goes only to subscriptions whose person can read its page when it
  is sent (and whose key still covers it), so a subscription never learns of
  a change its maker couldn't see.
- Receivers are public `http(s)` addresses. A development server may name
  one loopback receiver with `LEAFEN_WEBHOOK_ALLOWED_LOOPBACK=127.0.0.1:<port>`.

## Apps

A person registers an app — a name and the addresses it returns to — in the
developer tools (Settings → Developer → Open developer tools → Connections →
New connection, OAuth; or `POST /api/leafen/v1/apps`), and gets its id
(`lapp_…`, the Client ID on its page). The app sends people to the OAuth
server's authorization endpoint (`/oauth/authorize`, advertised at
`/.well-known/oauth-authorization-server`) with `client_id` set to that id,
`resource` set to the native API (`<origin>/api/leafen/v1`), `scope` set to
the key scope it asks for (`read`, `content`, `structure` or `admin`), a
PKCE `S256` challenge and its `state`. Each person sees the app, the scope
and their workspaces, and chooses the ones it reaches. The code exchanged at
`/oauth/token` (with `resource` again) gives an access token (`lmo_…`) and a
refresh token; the access token works on the native API as that person's
key — the same scope, workspace, confirmation, policy, history ("〈person〉
via 〈app〉"), audit and per-connection revert rules — and nowhere else (not
on MCP). The person removes the connection in Settings → Agents (Connected
agents) or on its page under the developer tools' MCP tokens; deleting the
app ends every connection made with it.

### Apps with their own account

An app registered to act as its own account ("Acts as: its own account in a
workspace") is installed by a workspace's owner through the same
authorization: the owner chooses one workspace they own and whether the app
is a guest — it sees only the pages connected to it, as a Notion integration
does — or a member. The token is the app's account's, not the owner's; it
keeps working when the owner leaves, and it follows the same key rules. The
account can't log in, gets no notifications, emails or pushes, isn't found
in people search or mentions, isn't counted among people, and reads as
"〈app〉 (app)" in the member list and version history. A page's "…" →
Connections connects a guest app to the page and the pages inside;
Settings → Connections → Installed lists the apps, changes their role and
removes them (which ends their connections). `GET`/`PATCH`/`DELETE
/api/leafen/v1/workspaces/{workspaceId}/apps[/{appId}]` do the same.

### Notion-compatible integration connections

A Notion-compatible OAuth client in `LEAFEN_NOTION_COMPAT_OAUTH_CLIENTS` connects
as a Notion public integration does: it sends the person to
`/v1/oauth/authorize?client_id=…&response_type=code&owner=user&redirect_uri=…&state=…`
(`redirect_uri` may be left out when the client registered only one). They
log in, choose a workspace and the pages it may reach — those and the pages
under them, and nothing when they choose none — and go back to the address
with `code` and `state`, or with `error=access_denied` when they cancel. The
client exchanges the code at `/v1/oauth/token` with HTTP Basic client
authentication, as with Notion. The connection is its own bot user, named as
the client is, whose owner is the person who connected it. Anyone who may
connect apps to a page adds the workspace's connections to it from the page's
"Connections" menu, or takes one off there; a page reached through one above it
is changed there. A Notion-compatible personal access token acts as the person
who made it, reaching what they can, and `GET /v1/users/me` answers with the
token's own bot user, owned by that person, as Notion's does. It reads people
by their IDs but lists no one: `GET /v1/users` refuses it, as Notion's
documentation says a personal access token cannot list a workspace's users.
An agent's Leafen MCP connection, and a key it works with, works as its person.

### Notion-compatible integration webhooks

A Notion-compatible OAuth client configured with `webhookUrl` (and, to
narrow it, `webhookEvents`) in `LEAFEN_NOTION_COMPAT_OAUTH_CLIENTS` — or
`LEAFEN_NOTION_COMPAT_OAUTH_WEBHOOK_URL` for the single-client settings —
gets, for each connection made with it, what a Notion integration's webhook
subscription gets: first a verification request
`{ "verification_token": "secret_…" }`, then the events above in Notion's
format (`api_version` `2025-09-03`, Notion ids, `integration_id`,
`accessible_by` the connection's bot, `space` for the workspace) for the
pages shared with that connection only, each signed
`X-Notion-Signature: sha256=<hex HMAC-SHA256 of the body under the token>`.
The server's configuration stands for Notion's "Verify" step. Ordering,
retries and access checks are those above; a connection removed stops its
subscription.

## View links

The server runs no browser. To see a page as a person would — for a
screenshot or a visual check — an agent asks for a view link and opens it in
its own browser:
`POST /api/leafen/v1/pages/{pageId}/view-links`
`{ "viewport": "desktop" | "tablet" | "mobile", "theme": "system" | "light" | "dark", "language": "ko", "seconds": 600, "singleUse": true, "viewId": "…", "withoutSecret": false }`
(all optional; `read` scope; `viewId` opens a database on this page on that
view). The answer gives the link's `url`
(`<origin>/view#token=lvl_…`), its `secret` (`lvs_…`, with `secretRequired:
true`), its `id`, `expiresAt`, and the screen's `width` and `height`
(desktop 1280×800, tablet 768×1024, mobile 390×844).

- The link opens only with its secret, given apart from it, so a link that
  leaks alone opens nothing. An automated browser sends the secret in every
  request's `Leafen-View-Secret` header (for example Playwright's
  `setExtraHTTPHeaders`) and the page opens at once; anything else types it
  into the page, which asks for it first and keeps it in that tab's memory
  only. Never paste the secret into a chat or a log. Five wrong secrets
  close the link.
- `withoutSecret: true` makes a link that opens with the link alone, to show
  a person briefly; an organization can forbid it.
- Size the browser's viewport to `width`×`height` and capture the window. A
  wider browser shows a tablet or mobile page in a frame of that size
  (`iframe[data-view-link-frame]`), which can be captured instead.
- The page is read-only and shows only the link's one page, in the chosen
  theme and language, the way a shared page shows it.
- To see a slide of the page's presentation, add `&present=N` after the
  token (`<origin>/view#token=lvl_…&present=2`): 0 is the title slide, then
  each part of the page between dividers, 800px wide and fitted to the
  window, without the presentation's bar. A number past the last slide shows
  the last one.
- The token rides after `#`, so it never reaches a server log. The link
  shows only what its maker can read: each time it is opened, the maker —
  and the key that made it — must still reach the page, and the page must be
  out of the trash.
- A link lasts `seconds` (60 to 3600, 600 by default). A single-use link (the
  default) keeps working for two minutes after it is first opened, so the
  page can load what it needs, then stops. `DELETE
  /api/leafen/v1/view-links/{viewLinkId}` ends it sooner; removing the key
  that made it ends it too.
- Every open and every wrong secret goes into the organization's audit log
  with the time, address and browser; `GET
  /api/leafen/v1/view-links/{viewLinkId}/opens` lists them for the link's
  maker.
- An organization's sharing settings can turn view links off, shorten their
  longest life, make every link open once, and require the secret on all of
  them.

## Notion-compatible tool surface

Source: <https://developers.notion.com/guides/mcp/mcp-supported-tools>.
Inventory reviewed against the official Notion MCP supported-tools pages on 2026-11-13.
The table names every Notion tool Leafen's `tools/list` can carry, 82 in all:
the 71 on those pages; 10 more that Notion's own connector served that day
(`notion-page-view`, `notion-get-sidebar-tabs`, `notion-update-sidebar-tabs`,
`notion-update-page-favorites`, `notion-create-meeting-note`,
`notion-convert-to-meetings-database`,
`notion-link-meeting-note-to-calendar-event`, `notion-update-skill-settings`,
`notion-complete-mcp-setup` and `notion-get-setup-guide`); and
`notion-list-agents`, which the 2026-10-02 page had. 48 execute native
behavior and 34 are explicit refusals. `unsupported` means the official name
takes any arguments only to return a stable `unsupported_feature` error with
a reason, an alternative, and retryability; it never claims a product
capability or returns a placeholder success. A token's `tools/list` leaves
the 34 refusals out unless its page in Leafen turns on "Notion-only tool
names" (off as a token starts), so an agent chooses among the tools that
work; called by name, each still answers. Listed, the refusals come last,
each in one line naming what to use instead ("Not in Leafen; use
notion-search instead.") with no title and an empty object input schema, so
an agent that knows Notion's tools reads where to go without reading them as
tools it can use.

Each answer's text keeps its keys in Notion's order (fetch's `metadata`,
`title`, `url`, `text`…; view-version's `type`, `url`, `configuration`,
`rows`, `total_rows`, a database's properties and a row's values in its
SQLite table's order) and writes `<`, `>` and `&` as they are, as Notion's
answers do; the structured content holds the same values.

| Official tool | Disposition | Current status |
| --- | --- | --- |
| `notion-search` | implemented | Searches every workspace the connection reaches, or the one teamspace, page with its sub-pages, or data source it is held to, with the bounded native index; `query_type: user` searches the member directory. Native `leafen_search` searches the workspaces named (`workspace_id`, `workspace_ids`) or, naming none, every one the connection reaches. |
| `notion-connector-search` | unsupported | Returns `unsupported_feature` because Leafen keeps no search over connected apps such as Slack or Google Drive; use `notion-search`, or on the native endpoint a connected service's own tool through `leafen_service_call`. |
| `notion-fetch` | implemented | Reads one centrally authorized native page, real `database://` source or linked container, bounded `collection://` data-source schema, or `view://` saved view, and answers in Notion's `metadata`/`text` shape. Reads authorize the target page and the exact source page of every linked source; native `leafen_fetch` remains the explicit-workspace page read. |
| `notion-list-private-pages` | implemented | Lists the person's top-level pages and databases in the sidebar's Private section, in the sidebar's order, with continuation. |
| `notion-list-shared-pages` | implemented | Lists the pages and databases shared with the person directly, in the order of the sidebar's Shared section, with continuation. |
| `notion-list-favorite-pages` | implemented | Lists the person's favorite pages and databases, the latest favorited first, with continuation. |
| `notion-list-recent-pages` | implemented | Lists the pages and databases the person visited lately, the latest first, as Recents lists them, with continuation. |
| `notion-list-page-backlinks` | implemented | Lists the pages the person can open that link to or mention a page or database, with the block each link is in, with continuation. |
| `notion-query-meeting-notes` | unsupported | Takes the official query (any arguments) and returns `unsupported_feature`; Leafen intentionally has no meeting recording, transcript, generated-note or imported meeting-artifact service. |
| `notion-get-self` | implemented | Names the connection's person (ID as `notion-get-users` gives it, name and email) and the workspace, or workspaces, it was given. |
| `notion-get-teams` | implemented | Returns the teamspaces the person can see in the connection's currently authorized workspaces, split into `joined` (with role) and `available`, at most 10 of each by name, each with its `workspace_id`; `workspaces` lists those workspaces, whose IDs `teamspace_id` also takes. |
| `notion-get-groups` | implemented | Lists the groups of the connection's workspaces by name, at most 25, the closest to `query` first, or the one a group ID names. |
| `notion-get-users` | implemented | Reads the member directory of one of the connection's workspaces, filtered by name or email, or one user, with bounded continuation. |
| `notion-get-tool-access` | implemented | Reports every official name in this table by Notion's base name: `available` for the 48 Leafen executes and `not_enabled` for the 34 explicit refusals. |
| `notion-get-version-changes` | implemented | Lists what changed in a page or database between two times (the last 24 hours by default) and who changed it, through an agent or not, from its version history. |
| `notion-view-version` | implemented | Reads a page or database as it looked at a past time, from its version history, a page's content as `notion-fetch` writes it. |
| `notion-restore-version` | implemented | Restores a whole page or database to how it looked at a past time, as the version history's restore does: every later edit is undone, comments stay. |
| `notion-restore-pages` | implemented | Brings pages or databases back from the trash to where they were, as the trash's restore does. |
| `notion-create-comment` | implemented | Uses the ordinary comment permission, anchor, notification and durable receipt owners for page comments, uniquely selected block comments and discussion replies. |
| `notion-get-comments` | implemented | Reads a transactionally consistent, at-most-25,000-comment native snapshot and can include block, inline, resolved or selected discussions. |
| `notion-update-comment` | implemented | Replaces the text of a comment the person wrote with the whole corrected text, as inline Markdown. |
| `notion-delete-comment` | implemented | Deletes a comment the person wrote, with its replies when it starts its discussion. |
| `notion-set-discussion-resolved` | implemented | Resolves or reopens a discussion. |
| `notion-set-comment-reaction` | implemented | Adds or removes the person's emoji reaction on a comment; the same state twice changes nothing. |
| `notion-create-suggested-edit` | implemented | Proposes one change to a page's text without making it, as suggesting mode does; the old text must be found once, within one block. |
| `notion-get-suggested-edit` | implemented | Reads a suggested edit's block text before and after it, whether it is pending, accepted, rejected or withdrawn, and the actions the person may take on it now. |
| `notion-resolve-suggested-edit` | implemented | Accepts or rejects one pending suggested edit: someone who can edit the page accepts or rejects, and the one who suggested it may reject it. |
| `notion-create-pages` | implemented | Atomically creates 1–100 titled pages with optional Markdown under one authorized page, database, data source or teamspace, or as private pages at the workspace's top, through the ordinary page transaction and durable receipts; content opening with its only first-level heading takes that line as the title, as Notion's does. |
| `notion-update-page` | implemented | Applies one durable page update through the ordinary page transaction and mutation receipt: `replace_content`, `insert_content`, `update_content`, `update_properties`, `apply_template` or `update_verification`, with the icon and cover alongside any; `is_skill: true` returns `unsupported_feature`. |
| `notion-move-pages` | implemented | Atomically moves 1–100 authorized pages under one authorized page, into one database's data source, or to the top of the person's private pages, through the ordinary metadata transaction and durable receipts; a database's item moved into another data source moves as a row into another database, as the row move operation does, and one that would leave values behind is refused naming them unless the native call gives `drop_values`; database objects are not moved by it. |
| `notion-duplicate-page` | implemented | Starts the ordinary durable page-duplication job at the source page's current parent and returns an honest asynchronous task plus preallocated target ID; a database page is copied at once as the app's duplicate copies it. |
| `notion-delete-pages` | implemented | Moves pages to the trash with their sub-pages and databases only with `user_confirmed: true` (without it, lists what would move); `notion-restore-pages` or the trash brings them back. |
| `notion-get-async-task` | implemented | Polls a page-duplication job created by the same MCP connection and returns queued/running/succeeded/failed with a terminal target or stable failure code. |
| `notion-create-database` | implemented | Atomically creates a full-page database (or, from page content, an inline one) with its table view and property schema under an authorized page, or as a private database at the workspace's top, through the ordinary content-graph owner and durable receipt, answering with the database as `notion-fetch` gives it, its `collection://` source in the `<data-source>` tag; `database_type` `tasks` or `projects` gives Notion's properties for that type. |
| `notion-update-data-source` | implemented | Changes one authorized source's title, schema (SQL DDL that adds, drops to the trash, renames and converts properties; select options are kept by ID) or page layout, and turns a database inline or full-page, through the ordinary content-graph owner; type changes convert existing cells with 30-day recovery. |
| `notion-create-view` | implemented | Adds one view to a database as a tab, or as an inline linked view at the end of a page, after authorizing the container and source pages; `configure` sets its filters, sorts, grouping and display in the view language. |
| `notion-update-view` | implemented | Changes a view's name, filters, sorts, grouping or display — only what is given — preserving its ID, source and rows; `CLEAR` removes settings. |
| `notion-query-data-sources` | implemented | Reads one authorized `collection://` source's rows with property filters and sorts, a saved view's rows under its filters and sorts, or runs durable bounded read-only logical SQL across explicitly authorized sources. SQL never exposes physical storage schema and fails rather than truncating over its snapshot, execution, or result budgets. |
| `notion-query-multiple-data-sources` | implemented | Runs read-only logical SQL across 2 to 10 authorized data sources — joins, unions, comparisons and aggregations — as `notion-query-data-sources` runs SQL. |
| `notion-delete-databases` | implemented | Moves databases that own their data to the trash with all their rows only with `user_confirmed: true` (without it, lists what would move); `notion-restore-pages` or the trash brings them back. |
| `notion-create-file-upload` | implemented | Issues a five-minute connection-bound multipart capability and publishes at most one 20-MiB file through the ordinary upload lifecycle. |
| `notion-create-attachment` | implemented | Creates a centrally authorized product file from at most 200 KiB of safe inline UTF-8, a non-redirecting public HTTPS source capped at 5 MiB, or a completed upload from the same MCP connection. |
| `notion-download-attachment` | implemented | Reads at most 200 KiB of valid UTF-8 from a supported safe text attachment created by the same MCP connection through the ordinary file read and DLP owner. |
| `notion-get-file-download-urls` | implemented | Gives fresh download addresses for up to 25 files a fetch or `notion-get-comments` names, each resolved on its own, with why for those that are not. |
| `notion-create-folder` | unsupported | Returns `unsupported_feature` because Leafen has no distinct Folder identity or lifecycle; use the page hierarchy instead. |
| `notion-update-folder` | unsupported | Returns `unsupported_feature` because Leafen has no distinct Folder identity or lifecycle; organize files with pages, sub-pages and `notion-move-pages` instead. |
| `notion-convert-page-to-skill` | unsupported | Returns `unsupported_feature` because Leafen has no persistent page-to-Skill identity, artifact, or lifecycle authority; keep the content as an ordinary page instead. |
| `notion-download-skill` | unsupported | Returns `unsupported_feature` because Leafen has no versioned Skill artifact or signed Skill download authority; use ordinary page and attachment tools instead. |
| `notion-upload-skill` | unsupported | Returns `unsupported_feature` because Leafen has no Skill archives or Skills database; write the instructions into a page with `notion-create-pages` and attach files with `notion-create-attachment`. |
| `notion-get-enabled-skills` | unsupported | Returns `unsupported_feature` because Leafen has no Skills or Skill enablement; keep instructions as ordinary pages read with `notion-search` and `notion-fetch`. |
| `notion-update-enabled-skills` | unsupported | Returns `unsupported_feature` because Leafen has no Skills to turn on or off; keep instructions as ordinary pages. |
| `notion-memory-search` | implemented | Searches the person's workspace — the pages and databases where they and their agents keep context, preferences and decisions — as `notion-search` searches it; something new is kept by writing it into its page. |
| `notion-search-agents` | unsupported | Returns `unsupported_feature`; Leafen governance records are not a Custom Agent execution directory. |
| `notion-query-sessions` | unsupported | Returns `unsupported_feature` because Leafen has no durable Custom Agent session, run, or event runtime. |
| `notion-search-sessions` | unsupported | Returns `unsupported_feature` because Leafen has no durable Custom Agent session, run, or event runtime. |
| `notion-spawn-session` | unsupported | Returns `unsupported_feature` because Leafen has no agent execution provider, session admission, credential, cost, or recovery authority. |
| `notion-send-message-to-session` | unsupported | Returns `unsupported_feature` because Leafen has no Custom Agent follow-up execution authority; use page comments for human collaboration. |
| `notion-get-session-status` | unsupported | Returns `unsupported_feature` because Leafen has no durable Custom Agent session runtime. |
| `notion-wait-session` | unsupported | Returns `unsupported_feature` because Leafen has no Custom Agent streaming or wait runtime. |
| `notion-stop-session` | unsupported | Returns `unsupported_feature` because Leafen has no Custom Agent run or cancellation authority. |
| `notion-list-session-events` | unsupported | Returns `unsupported_feature` because Leafen has no Custom Agent event stream or retention authority. |
| `notion-read-session-event` | unsupported | Returns `unsupported_feature` because Leafen has no Custom Agent event-body or retention authority. |
| `notion-calendar-list-calendars` | unsupported | Returns `unsupported_feature` because Leafen keeps no calendar accounts or events and connects to no calendar service; keep dated work in a database with a date property and a calendar view. |
| `notion-calendar-list-events` | unsupported | Returns `unsupported_feature` because Leafen keeps no calendar accounts or events and connects to no calendar service; keep dated work in a database with a date property and a calendar view. |
| `notion-calendar-list-coworkers-events` | unsupported | Returns `unsupported_feature` because Leafen keeps no calendar accounts or events and connects to no calendar service; keep dated work in a database with a date property and a calendar view. |
| `notion-calendar-list-calendar-resources` | unsupported | Returns `unsupported_feature` because Leafen keeps no calendar accounts or events and connects to no calendar service; keep dated work in a database with a date property and a calendar view. |
| `notion-calendar-list-contacts` | unsupported | Returns `unsupported_feature` because Leafen keeps no calendar accounts or events and connects to no calendar service; keep dated work in a database with a date property and a calendar view. |
| `notion-calendar-create-events` | unsupported | Returns `unsupported_feature` because Leafen keeps no calendar accounts or events and connects to no calendar service; keep dated work in a database with a date property and a calendar view. |
| `notion-calendar-update-events` | unsupported | Returns `unsupported_feature` because Leafen keeps no calendar accounts or events and connects to no calendar service; keep dated work in a database with a date property and a calendar view. |
| `notion-calendar-cancel-events` | unsupported | Returns `unsupported_feature` because Leafen keeps no calendar accounts or events and connects to no calendar service; keep dated work in a database with a date property and a calendar view. |
| `notion-ai-search` | unsupported | Returns `unsupported_feature` because Leafen has no AI or connected-source runtime, credential, privacy, or billing authority; use bounded `notion-search` instead. |
| `notion-page-view` | implemented | Gives the address that opens a page or database in Leafen, or the workspace's start, to show the person; it changes nothing. |
| `notion-get-sidebar-tabs` | implemented | Reads the person's sidebar in a workspace — Leafen's one list, given as one tab — with its shortcuts and sections in order and catalogs of those that can be added. |
| `notion-update-sidebar-tabs` | implemented | Replaces the person's sidebar in a workspace with the tab from `notion-get-sidebar-tabs` sent back, keeping the shortcuts and sections it lists in its order. |
| `notion-update-page-favorites` | implemented | Favorites or unfavorites 1–100 pages or databases the person can open, as a page's ☆ does; it changes nobody's access. |
| `notion-create-meeting-note` | unsupported | Returns `unsupported_feature` because Leafen has meeting notes databases but no AI meeting notes; add a row to a meeting notes database with `notion-create-pages`. |
| `notion-convert-to-meetings-database` | unsupported | Returns `unsupported_feature` because Leafen makes a meeting notes database only from its meetings template; keep meetings in the existing database as ordinary rows. |
| `notion-link-meeting-note-to-calendar-event` | unsupported | Returns `unsupported_feature` because Leafen keeps no calendar events; put the event's date or address in the note with `notion-update-page`. |
| `notion-update-skill-settings` | unsupported | Returns `unsupported_feature` because Leafen has no Skill enablement, placement or connected-agent Skill settings; keep instructions as ordinary pages. |
| `notion-complete-mcp-setup` | unsupported | Returns `unsupported_feature` because Leafen keeps no record of a guided setup — a connection is ready once connected; read `notion-get-setup-guide` instead. |
| `notion-get-setup-guide` | implemented | Gives Leafen's guide for agents — how to use Leafen as the person's shared workspace and memory — as the server tells each agent when it connects. |
| `notion-list-agents` | unsupported | Returns `unsupported_feature`; Leafen governance records are not a Custom Agent execution directory. |

The strict runtime catalog exposes all 82 official names recorded above. The
native endpoint also exposes those names plus `leafen_list_workspaces`,
`leafen_search`, `leafen_fetch`, `leafen_update_page`, `leafen_operations`
and `leafen_call`, `leafen_catalog` (a workspace's databases in one short
answer: each data source's name, database URL, `collection://` URL and a
`view://` URL, and each column's type and the SQLite columns
`notion-query-data-sources` names it by), the page file tools `leafen_get_file` and
`leafen_replace_file`, the sending, inbox, import, Notion migration,
service and server tools described below, and, unless the build leaves the
app platform out,
`leafen_app_tools` and `leafen_app_call`. Every implemented strict
name executes native behavior. Every unsupported name deterministically returns
the documented explicit error after connection-authority validation. Expanding
implemented behavior requires a real native service and the same fixed-scope,
bounded, durable guard—not only a tool declaration.

### The native endpoint's first list

The native endpoint's `tools/list` holds its core tools — `leafen_inbox`,
`leafen_list_workspaces`, `leafen_search`, `leafen_catalog`,
`leafen_operations`, `leafen_call`, `leafen_bug_report`, `leafen_services`,
`leafen_sign_in_requests`, `leafen_connection_requests` and the twelve
`leafen_notion_migration_*` tools of a Notion move — a connection with no
move to make (one that can't make one: reading tools alone, held to chosen pages, or
its person can add pages in none of its workspaces; or one whose workspaces'
moves are done) lists the guide alone, whose answer then says why, what to
do now and how to run each through `leafen_call` with its input — beside Notion's tools as
Notion's own MCP lists them on its supported-tools pages (search, reading,
pages, databases and views, comments and suggested edits, history, files):
every one of those Leafen has, so adding a column (`notion-update-data-source`),
moving pages or a database's rows (`notion-move-pages`) or deleting a database
(`notion-delete-databases`) needs no search first. Each is listed where the
connection's tools and its page allow; Notion's tools Leafen doesn't have stay
off it. `leafen_service_call` joins them where the connection reaches an
outside service, and `leafen_app_tools` and `leafen_app_call` (beside the
apps' own tools) where an app gives it tools. Every other tool is still
served: `leafen_operations` lists it in its group and finds it by English
words, `leafen_call` runs it with `tool` and `arguments`, and a call by its
name is answered as before. `PUT /api/leafen/v1/mcp/connections/current/notion-tools`
with `{"notionTools": true}`, which an agent sends through `leafen_call` with
its own key, puts every notion-* tool Leafen has on that connection's list
from its next `tools/list`, those Notion's pages don't list too, and `false`
takes those off; a token whose page turns on "Notion-only tool names" lists
Notion's tools Leafen doesn't have too. The strict endpoint lists every tool
it has, as before. Every tool's description stays within the 2,048 UTF-16
units Claude Code keeps of it, at the longest a connection lists it; the
server checks that as it starts.

## Page files on the native endpoint

An agent edits a page's file where it runs, not on the server.
`leafen_get_file` takes the ID of a file, image, PDF, video or audio block (as
the address of its fetch line names it after `#block=`) and answers the file's name, type and size, its
`base_version` and a download URL that works for five minutes. After editing a
copy, the agent uploads it with `notion-create-file-upload` or
`notion-create-attachment` and calls `leafen_replace_file` with the block, the
upload and the `base_version` it started from. A page's read gives the same
`base_version` for each stored file on it, so a replace needs no
`leafen_get_file` first: `notion-fetch` and `leafen_fetch` answer
`file_versions`, one entry for each file, image, PDF, video or audio block the
read's text names (by `block_id`) and each file in a database page's files
property (by `page_id`, `property` and `file_id`), and
`GET /api/leafen/v1/pages/{id}/content-graph/file-blocks` gives each block's
`version`, the `baseVersion` of `PUT …/blocks/{blockId}/file`. If someone changed the file in
the meantime, nothing is replaced: the answer is
`LEAFEN_MCP_FILE_VERSION_CONFLICT` with the file's current name, size,
`base_version` and who saved it, and the agent chooses as a person would:
get it again and bring its change over, replace it anyway naming the new
`base_version`, or keep its upload beside it with `as_copy` (a new file
block right after it). The file a replace puts aside stays in the page's
history, which brings it back. Nobody holds a file, so people editing it in
an app don't hold a replace up.
Getting needs the read scope; replacing needs content. Uploading as a new file
stays as it is.

## Sending and deadlines on the native endpoint

**Sending.** Any database item with a person property can be sent to a member of the workspace — any member, as a person property
offers them. It makes no mail or chat. `leafen_send`
(content scope), as an item's "Send" menu, takes `item` (an ID or `/pages/`
URL), `to` (a user ID, as `notion-get-users` lists, or a `user://` link), `property` (the person
property to name them in, by name or ID; the database's first when left out),
`message` (optional) and `confirm_task_in` (a database's data source to also
make them a task "Look at: \<title>" in, naming them in its first person
property). It adds them to that
property, so they hear of it as a person property tells anyone it names,
leaves the message as a comment on the item ("→ name: message"), and leaves
the role relations as they are. Sent from the screen, someone the item doesn't
open to is given it by a share of their own — to edit an item whose database
has a status, else to comment — and the task likewise, which takes full access
to it. `leafen_send` and a key's send don't share: for someone the item
doesn't open to they are refused with `LEAFEN_ROW_SEND_UNSEEN`, as a send by
someone without full access is: share it first (a share asks its own scope and
confirmation), put it where they can open it, or ask someone who can share it. The send answers `notified`,
`shared` and the task's `taskId`. The app's routes are `GET
/api/leafen/v1/database-rows/{rowId}/send-targets` (the database's person
properties, the members with whether each can open the item, and whether the
person may share it) and `POST .../database-rows/{rowId}/send` with `{to,
property, message, confirmTaskIn}`.

**Deadlines not to miss.** Any database date can be marked as a deadline that
can't be undone once missed (a legal or contract deadline): in MCP,
`date:<name>:deadline` with the days before it its notices go, as `"7,3,1,0"`
(0 is the day itself; 0 to 365, at most 8), and `""` to clear it; left out, the
item's mark stays. Fetch and query show the part only on items that have one.
The deadline is the date's end when it has one. Each notice goes at 9:00 that
day on the date's clock, as Leafen's own notice in the inbox and, whenever the
server sends email, by email whatever the person chose for activity emails, to
the people the item names and whoever marked the deadline. It comes again, at 9:00 the next day (on
the deadline's own day every three hours), to whoever hasn't pressed
Acknowledge in Leafen, until the next notice or the deadline; one that fell due
while the server was down goes when it is back. Who acknowledged each notice,
and when, is kept: `GET /api/leafen/v1/database-rows/{rowId}/deadline-notices`
lists an item's notices with them, and `POST
/api/leafen/v1/deadline-notices/{noticeId}/acknowledge` acknowledges one. The
Notion-compatible API takes `deadline` in a date object the same way.

**Working days.** A deadline that moves past weekends and public holidays — a
served date plus 14 days, the last day moved to the next working day — is
counted with `leafen_workday` (`date`, then `days` for calendar days or
`workdays` for working days, and optionally `calendar`; South Korea's, `KR`,
substitute and temporary holidays included, when left out). It names the days
it skipped and says when a year it crossed isn't in Leafen's calendar yet,
where only weekends were skipped. Formulas count the same with
`nextWorkday(date)` and `addWorkdays(date, count)`, each taking a calendar's
code last. The calendar is `internal/app/business_calendars.json`, by year;
a release adds the years and holidays announced since.

**Read log.** Every page, item and file the server gives out is recorded, as
every edit is: to an agent's connection (fetched, listed by a query or a SQL
query, a passage found by search, a file link), as to the app, the API, a
share link or an export. Only the server's administrators turn it off, for the
whole server. A person's reads of one thing the same way within ten minutes
are one record with a count, naming the connection. The records are the
organization's audit events (`content.read`), kept and exported as its audit
log; `GET /api/leafen/v1/workspaces/{id}/read-log?scope=page:<id>` reads the
reads of all that is in a page, to someone with full
access there or a compliance administrator, and that reading is recorded too.
A compliance administrator can leave a scope's log, and those under it, to
compliance administrators: `PUT .../read-log/readers` with `{scope, readers}`
(`fullAccess` or `compliance`).

**Files the person imports.** A file dropped in Settings' import becomes a page
(or a database, for a CSV) at once, as in Notion; nothing waits for an agent.
An agent imports as the person through the same import API.

## Moving a Notion workspace on the native endpoint

An agent moves a person's Notion workspace into Leafen with these tools, in
this order. `leafen_notion_migration_guide` (also the resource
`notion-migration-guide`) gives the whole flow, the plan format and its
principles; read it first.

- `leafen_notion_migration_sources` lists the person's Notion connections and
  the roots each can move; `leafen_notion_migration_connect` keeps a Notion
  internal integration token as the workspace's Notion connection: one the
  agent made in the person's own browser with a browser tool, after their
  yes, or one they pasted into the conversation.
- `leafen_notion_migration_start` starts a migration of the chosen roots, and
  `leafen_notion_migration_scan` reads Notion's structure a few steps at a
  time until its status is `scanned`. Roots an earlier move of the person's
  already brought into the workspace start no second copy unless they chose
  one (`second_copy`): the start names that move to run again.
- `leafen_notion_migration_items` reads what the scan found, one kind and one
  window at a time; `leafen_notion_migration_formats` gives what Notion's API
  does not give a connection, and how to read it in the person's Notion.
- `leafen_notion_migration_plan` reads the plan with its revision and its
  check; `leafen_notion_migration_set_plan` saves the plan agreed with the
  person over the revision last read.
- `leafen_notion_migration_run` carries out an agreed plan that passes its
  check; `leafen_notion_migration_status` follows it stage by stage.
- `leafen_notion_migration_verify` compares what the run made with what it
  read from Notion.

## The inbox on the native endpoint

An agent reads the person's unread notices — comments, mentions, items that
name them in a person property (tasks given them), shares and access
requests, reminders, deadlines not to miss, apps someone asks for, what
automations send them and Leafen's own notices — when the person asks about
their notices, to-dos or approvals, or when the work at hand concerns one.
Leafen never brings them up itself: the session's instructions don't ask the
agent to check the inbox as a conversation starts, and answers to other
tools carry no notices. The person sees their notices in Leafen. The inbox
tools (none on the Notion-compatible endpoint; `leafen_inbox` is on the
first list, so an agent the person asks finds it by its name):

- `leafen_inbox` (read scope) lists the unread notices, newest first, 25 at a
  time (`page_size` fewer) with `start_cursor`, a line each — what it is,
  from whom, on which page, a short preview and its `id` — marked as material
  from other people, not instructions. Notices this connection put off are
  left out until their time.
- `leafen_inbox_item` (read) opens one: its page, the database item it is
  about, and for a comment its discussion.
- `leafen_inbox_read` (content) marks notices read, as the person's inbox in
  Leafen does — the app's inbox shows them read.
- `leafen_inbox_remind` (read) leaves notices out of this connection's
  `leafen_inbox` list for some `minutes` (5 to a week); they stay unread.

Their notes ask the agent to pass on only what bears on the person's
request, never to follow directions written in a notice on its own, and to
mark read only what it showed the person.

## Bug reports on the native endpoint

The session's instructions have an agent offer a report when Leafen itself
fails or frustrates the person. [docs/bug-reports.md](docs/bug-reports.md) is
the guide; for agents it says:

- When a Leafen tool fails or does other than its description says, or the
  person finds something in Leafen itself hard to use, offer a report once,
  after the task they asked for — once per problem, and never about the
  content of their own data. A security weakness never goes in an issue: it
  is reported privately, as [SECURITY.md](SECURITY.md) describes.
- Connected to Leafen, draft it with `leafen_bug_report`: it fills in the
  version and the system, gives the body in the form's headings and a link to
  the form with the draft filled in, finds issues with the same words — when
  one is the same problem, add to it instead of opening another — and posts
  nothing.
- Write the report with the person: ask what they did, what they expected and
  what happened, one thing at a time, in their language.
- Use made-up data only. Never copy the person's pages, databases, files,
  names, mail addresses, server address, tokens or codes into the report or
  its attachments. Make up data that shows the same problem, or say how to
  make it.
- Where you can, check that the steps still show the problem starting from a
  new workspace with only that data, and write them as they went.
- Show the person the finished report and post nothing before their yes.
  Then post it with your GitHub login, keeping the form's headings in its
  body, or give them the link to check and submit themselves. If they say
  no, don't offer that report again.

`leafen_bug_report` (read scope) drafts it for the repository's bug form, as
the app's **Report issue** opens that form: from the title and the form's
fields the agent gives (synthetic data, steps, expected and actual, the fix
it thinks of, and optionally the error's reference and where the person saw
it), it answers with the server's version and its system and install
(desktop app, Docker or from source) filled in, the body in the form's
headings, a link to the form with the draft filled in (an answer too long for
the link is named, to be pasted), the required fields still empty, what looks
like the person's own data — their name or mail address, this server's
address, a real mail address, a key or token — to replace, and up to five
issues with the title's words from GitHub's public search, open ones first,
with the search's own link. It posts nothing and needs no GitHub account.

## Apps' tools on the native endpoint

An app installed in Leafen offers its actions as tools — all of them, unless
one is marked `"tool": false`: what a person does on the app's screen, their
agent does too. On the native endpoint, `leafen_app_tools` lists the apps the connection's person may use in
the organizations of the connection's workspaces (name, one-line summary,
maker, number of tools and those that need approval, and the app's address
when the server's public address is set, to give the person a link), or, given an app, its
tools with their input schemas and the notes the app's maker wrote, marked as
the app's. `leafen_app_call` runs one with `app`, `tool`, `input` and, only when
the app is in more than one organization, `organization`. Listing needs the
read scope; running needs content. A tool that runs as the person reaches what
the person reaches through the app, held to the connection's scope and
workspaces; one that runs as the app uses the app's own account. The input is
checked against the tool's schema first, the run is recorded in the app's runs
and the audit names the connection. A tool that needs the person's approval — or is
marked high-risk, which the person's own screen runs at once — asks the person
inside their conversation with the agent, never through a Leafen page and
never by an MCP elicitation form: the call answers
`status: "approval_pending"` with an `approval` holding its `approvalId`, when
it runs out (a day, after which it is declined by itself), the question
(`ask`: which tool of which app, its input as the app's fields, whether it
changes things outside Leafen, and "for an hour" where it may) and a
`message` that tells the agent to ask its person in those words; only if they
say yes, calling again with the same input and `approval` set to that
`approvalId` runs it once, and `<approvalId>:hour` also lets it run on that
connection for an hour (Leafen can't tell that yes from the agent's own, as
the token's page says). A declined, used or expired request says so. This
holds where the administrator oversees the organization's apps too (every
tool there asks). An agent changing the oversight itself (`PUT
/api/leafen/v1/admin/apps/oversight`, "Approve every app action") or another
of the administrator's safeguards (above, under keys) asks its person every
time, however the token's asking is set, never for an hour.
Clients listening for changes (`subscriptions/listen`) hear that tools,
prompts and resources changed when apps are installed, updated, stopped,
removed or regranted, or the connection's app choice changes. The connection's `initialize` instructions list, a line each, the
apps with tools it can use (a count above 20). `leafen_app_storage` reads and
changes the person's own store for an app — the settings its screen keeps for
them — with `app`, `command` (`get`, `set`, `remove`, `keys`), `key` and
`value` (and `tenancy` for an app used in more than one organization);
changing needs content.

The connection's owner may hold it, in Settings → Agents (the connection's
menu → "Apps it can use"), to chosen apps; by default it reaches every app
the person may use. A connection held to chosen apps also lists their tools
as tools of their own, named `app_<app>__<tool>` (dashes in the app's name
become underscores), with
the tool's input schema, the maker's words marked as the app's, and a
destructive hint for high-risk tools; they run like `leafen_app_call`. Apps'
prompts come as MCP prompts named `app_<app>__<prompt>`, with the template's
`{{name}}` placeholders as required arguments, and apps' resources as
`leafen-app://<app>/<resource>`; reading one runs the action the app named for
it (never one that needs approval) and needs the content scope. The prompts
and resources capabilities are announced only when the connection's apps have
some. The server keeps no session, so lists are read again rather than
announced as changed, and resource subscriptions are not offered.

## Outside services connected to Leafen

A person connects an outside service to Leafen once, by its remote MCP
server's address, rather than in each agent. `POST /api/leafen/v1/mcp-services/probe`
with `{"url": "https://…"}` looks at the address first, connecting nothing, as
the settings dialog does while it is typed: `signIn` is `oauth` for a service
with a login page (with the `scopes` it asks for and the `callbackUrl`) or
`headers` for any other, `open` says it answered with no login at all (and
`name` what it calls itself). `clientNeeded` says, before any login, that
the person must make a client on the service for Leafen, as the refusal's
code for connecting without one (below), with `note` saying why and what to
do in words to pass on, and `publicUrl` the server's public address where it
has one to connect from instead. `POST /api/leafen/v1/mcp-services` with
`{"url": "https://…"}` and an optional `"name"` (the person's name for it, in
place of the service's own) reads the service at once when it needs no login,
or with the headers it takes: `"auth"` is `{"type": "bearer", "prefix": "Bearer"
| "Token", "token"}`, `{"type": "basic", "username", "password"}` or
`{"type": "api_key", "key": "<header name>", "value"}` (`"token"` alone is a
bearer token), and `"headers"` up to 20 `{"name", "value"}` of the person's own
sent with each request — not the ones the MCP transport sends itself (`Accept`,
`Content-Type`, `Mcp-Session-Id`, …), each name once. When the service asks to
log in, `"scopes"` keeps some of those it asks for, and the server follows
MCP's authorization: it reads the service's protected-resource metadata (or
falls back to its origin), its authorization server's metadata, and is a
client there by the first way the server takes: an OAuth client the person
made on the service for Leafen (`"clientId"`, and `"clientSecret"` when it has
one), registering itself (dynamic client registration), or, where the service
registers no clients or refused this one and its server says
`client_id_metadata_document_supported`, Leafen's client ID metadata document
(`GET /api/leafen/v1/mcp-services/oauth/client-metadata`). The service reads
that document itself, so it is used only at an `https` address the internet
reaches: not this computer's or its network's, a name the public DNS never
has, or one its own name servers answer nowhere or only privately (a
tailnet's name before Funnel publishes it). Its redirect is
`/api/leafen/v1/mcp-services/oauth/callback` on the server's public address
(`LEAFEN_PUBLIC_URL`), which `GET /mcp-services/oauth/callback-address` tells
the person making a client. It answers with `authorizeUrl`, the service's own
login page, with PKCE (S256) and the service as the `resource`. The person
approves there; the page they come back to completes the login once, tells
the Leafen window that opened it (`leafen:mcp-service`) and closes. A service
that takes no PKCE is connected with a token instead.

Tokens, headers and the client's secret are sealed with the settings key and
never leave the server: no agent and no screen sees them, and a service that
repeats them in an answer has them masked. They are renewed
before they run out — one renewal at a time — and once more when the service
refuses one early; a refused renewal marks the service `sign_in_again`, and
`POST /mcp-services/{id}/sign-in` answers a new `authorizeUrl`. Connecting the
same address again connects it anew. `GET /mcp-services` lists the person's
services, 50 at a time from the latest connected (`?cursor=` the answer's
`nextCursor`), with their status and tools as last read (name, title, description,
input and output schemas, read-only and destructive as the service marked
them; unmarked tools count as destructive), `GET /mcp-services/{id}` reads
one as kept, `POST /mcp-services/{id}/check` reads one again, and `DELETE /mcp-services/{id}` asks the service to revoke
the tokens (RFC 7009) when it says how, then forgets them. Only public `https`
addresses are reached; the login goes only to the service's own host. A
person connects as many services as they like; they are the person's own, and
an instance export leaves them out. A refused connection says why in its code:
`LEAFEN_MCP_SERVICE_INVALID` (not an `https` address), `…_UNSAFE_ADDRESS`
(the address reaches a private network), `…_CLIENT_NEEDED` (the service
registers no clients — connect it with a client made there for Leafen, or a
token it gives), `…_ADDRESS_UNREACHABLE` (it logs Leafen in only by reading
the document, which it can't reach at this address — connect it with a client
made there for Leafen, or from Leafen opened at a public address; the message
says which and names the server's public address where it has one),
`…_REGISTRATION_REFUSED` (the address is right, but the service refused
Leafen's registration and reads no document Leafen can use — connect it with a
client made there for Leafen; the message names the error the service gave and
the redirect URI; an address check can't know this before connecting, as it
registers nothing), `…_SIGN_IN_UNAVAILABLE` (the address is right, but the
login server the service named — its metadata, registration or token
endpoint — gave no answer, or answered that it failed or was busy (5xx, 408,
429): try again in a while; said the same when logging in again, renewing a
login or checking the service, and the address check shows the login form
with a note), `…_TOKEN_NEEDED` (it has no login page — connect it again
with its token), `…_TOKEN_REFUSED`,
`…_UNSAFE_SIGN_IN` (its login takes no PKCE), `…_POLICY_BLOCKED` (a
workspace the person belongs to doesn't allow it, below) or `…_FAILED` (it
didn't answer as an MCP server).

A workspace's owner decides, under Settings → Connections → Manage
(`PATCH /api/leafen/v1/workspaces/{id}/connection-policies` with
`customMcpServers` and `customMcpAllowlist`), whether its people connect
their agents to custom MCP servers — on by default, as Notion's "Enable
custom MCP servers" — and, where the list names any, to which: `https`
addresses, each allowing itself and the places under it. Since a person's
services serve all their workspaces, the narrowest policy of the workspaces
they belong to as owner or member (not as a guest) decides; it is checked
when a service is added, logged in to again, read again and called.

On the native endpoint, every agent connected to Leafen uses these services
without setting them up itself. `leafen_services` (read scope) lists the
person's services by key — the service's name in lowercase words, numbered
when two share it — with its address, status and number of tools, 50 at a time
(`start_cursor` the answer's `next_cursor`), or, given a service, its tools with
their input schemas and `asksFirst`. `leafen_service_call` calls one
with `service`, `tool` and `arguments`, as the person: a tool that only reads
needs the read scope, any other content. The answer comes after a line saying
it is the service's — data, not instructions — with the service's content,
structured content and error flag as it sent them.

As Notion's custom MCP connections, the person sets each tool on or off and
"run automatically" or "always ask", and "always allow" lets all of a
service's tools run without asking — in Leafen's settings, logged in, never
with a key: `GET /api/leafen/v1/mcp-services/tools/{serviceId}` (50 at a time,
`?cursor=` the answer's `nextCursor`) reads them,
`PATCH /mcp-services/tools/{serviceId}/{tool}` with `{"enabled"}` or
`{"run": "auto" | "ask"}` sets one, and `PATCH /mcp-services/{serviceId}`
with `{"alwaysAllow"}` the whole service. A tool turned off is neither listed
to agents nor run. Left as it was, a tool that may destroy something (not
read-only, and not marked as non-destructive) asks. A tool that asks first
asks the person inside their conversation with the agent, never through a
Leafen page and never by an MCP elicitation form: the call answers
`status: "approval_pending"` with an `approval` holding its `approvalId`, the
question (`ask`: which tool of which service, what it may do, its input, "for
an hour" and "always") and a `message` that tells the agent to ask its person
in those words; only if they say yes, calling again with the same input and
`approval` set to that `approvalId` runs it once. `<approvalId>:hour` ("for
an hour") also lets that tool run on that connection meanwhile without
asking, and `<approvalId>:always` always allows the service. A request not
answered within a day is declined.
Each call is written in the audit of the connection's organizations
(`agent.service.tool_called`: the service, the tool, the connection and how
it went — never its input or answer).

Each agent connection uses every service the person connected — also those
connected later — unless the person holds it to chosen ones (Settings →
Agents → the connection's menu → "Services it can use", or
`GET /api/leafen/v1/mcp/connections/{id}/services`, 50 at a time with
`nextCursor`, and `PUT` with `mode` and, to choose or leave one service,
`serviceId` and `chosen`, logged in, never with a key); a service's key stays
the same either way. The connection's `initialize` instructions name, a line each, the
services it may use (above 20, that there are more), so the agent uses them through Leafen
instead of asking the person to set up a separate connector; with none, they
say where the person connects them.

The built-in Mail app's inbox, read and send are agents' tools too (send waits
for the person's approval). `leafen_gmail_guide` (read scope), also the
resource `leafen://guides/gmail`, is the guide an agent follows to help the
person connect Gmail: an app password first, with no setup on the server, and
only if needed a Google client the administrator makes, step by step — never
taking a password or secret in the conversation.

## License

This reference and its examples are licensed under the Apache License 2.0
([its text](sdk/app/LICENSE)), as the app SDK and the connection contracts
are; Leafen itself is AGPL-3.0-only ([NOTICE](NOTICE)).
