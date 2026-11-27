# Self-hosting

Leafen's server is the same in the desktop app and in Docker, for hosting or
a company server: the image carries the server alone (`cmd/leafen`), built
from the same source without the app's window, on a distroless base with no
shell. This guide runs it as a server: one process with one persistent data
directory and one public port.
The same data boundary and operating commands apply to the container and to
the program run without Docker.

## Supported topology

Run one Leafen process against one writable data directory. Put TLS and a public
hostname in front of that listener with a reverse proxy when needed. Do not run
two Leafen processes against the same live data directory or expose PocketBase
administration routes as a separate service.

## Docker

Run Leafen's published image with the launcher, from the repository root:

```sh
./scripts/run-self-hosted.sh
```

It pulls `ghcr.io/leafen-org/leafen:latest`, published for amd64 and arm64 with
each release; `--image ghcr.io/leafen-org/leafen:<version>` stays on one
release. The container listens on port `8090`, runs as the non-root uid/gid
`65532`, and stores durable state under `/data`. Preserve the complete
`leafen-data` volume. Leafen's commands run in the container work on `/data`
without `--dir`: `docker compose exec leafen leafen <command>` with Compose,
`docker exec -it leafen leafen <command>` for the launcher's container.

The published image is a release build (`LEAFEN_BUILD_TAGS=leafen_release` and
the release's version): it trusts only the production signing keys, reads the
update notice and sponsor list from Leafen's list address and lists the
official app catalog. An image built from the source is a development build
(version `0.1.0-dev`), which does none of these by itself:

```sh
docker build -t leafen:local .
./scripts/run-self-hosted.sh --image leafen:local
```

Building needs Docker with 4 GiB of memory or more (the server's main package
compiles in one go, near 3 GB). Colima starts with 2 GiB: `colima start
--memory 4`; Docker Desktop: Settings › Resources › Memory.

The launcher computes a finite default of `max(2 GiB, 50% of nominal host
RAM)`, displays it before container creation, and applies the same value to the
Docker hard and swap limits. Automatic sizing requires at least 4 GiB of host
RAM. Preview the exact command without creating a container:

```sh
./scripts/run-self-hosted.sh --print
```

An operator may tighten or loosen the limit with one finite whole-MiB/GiB
override. The supported minimum verification tier is 1536 MiB:

```sh
LEAFEN_MEMORY_LIMIT=1536m ./scripts/run-self-hosted.sh
```

A container started without a memory limit (a plain `docker run` with no
`--memory`) still serves: it plans as a machine does — `LEAFEN_MEMORY_LIMIT` if
set, else half of the machine's memory — says so in its log, and holds
requests back against that plan.

## Docker Compose

`compose.yaml` in the repository root runs the same published image:

```sh
docker compose up -d
```

- Compose gives the container a memory limit (`LEAFEN_MEMORY_LIMIT`, `2g` by
  default).
- `LEAFEN_IMAGE` names another image or tag
  (`ghcr.io/leafen-org/leafen:<version>` stays on one release), and
  `LEAFEN_PORT` the port it is served on (`8090` by default).
- Write these in a `.env` file beside `compose.yaml`, one `NAME=value` a
  line: Compose reads it each time, run with `sudo` too. A variable set in
  the shell isn't enough there — `sudo` drops it, and
  `LEAFEN_IMAGE=… sudo docker compose up -d` recreates the container on the
  image it had.
- To build the image from the source instead:
  `docker compose -f compose.yaml -f compose.source.yaml up -d --build`.
- To update, back up ([Backup](#backup)), then run `docker compose pull`
  (or write the release's tag in `.env`:
  `LEAFEN_IMAGE=ghcr.io/leafen-org/leafen:<version>`) and
  `docker compose up -d` again — on a NAS, Container Manager's project
  **Build** or Portainer's **Re-pull image and redeploy** does the same
  ([Updating](install.md#updating) in the install guide). Then check what runs:

  ```sh
  docker compose images leafen
  docker compose logs leafen | grep 'leafen: build' | tail -n 1
  ```

  The first shows the image and tag the container runs; the second its
  build, which starts with the release, and the data's schema, as
  `leafen: build <release>+…, data schema <n>` — on the first start of a
  new build with the build it ran before, and with
  `brought up from schema <m> as it started` when that start moved the data
  on.

## Without Docker

The server alone (`go build -o dist/leafen ./cmd/leafen`), or the desktop app
built as the root README says, serves without a window. On Linux, which has no
desktop app, the server alone is how Leafen runs outside Docker, used in a
browser:

```sh
./dist/leafen --dir=/srv/leafen/data serve --http=127.0.0.1:8090
```

Terminate with `SIGTERM` and allow the bounded shutdown to finish.

The server sizes its caches from the memory it plans for: its container's
limit, else `LEAFEN_MEMORY_LIMIT`, else half of the machine's memory (at least
2 GiB). SQLite's page cache shares a quarter of it across the connections (32 MB
each at most), its memory map takes at most a quarter (2 GB at most), and under
2 GiB the server keeps half as many database connections. `leafen doctor`
prints the plan, and the server prints it as it starts.

A setting the server can't use doesn't stop it: as it starts, it prints which
one, why and what it uses instead, and `leafen doctor` lists the same under
`settingsNotUsed`.

## First administrator

On a fresh data directory, Leafen prints a setup URL that carries a one-time
code (after `#code=`). Open it — or, from the server's own network, `/setup`
without it — and give the first administrator a name. That's all: an email
and a password are optional and set later in Settings → Account as a backup
login. On a server that sends no mail an address is added there
at once, without a confirmation: it logs in with the password, but opens
nothing an address decides — an invitation sent to it, a workspace's allowed
domain — until it is proven by a login provider or SSO. An account's first
address becomes its own. The browser that sets the server up is its first device,
logged in until it is logged out. Another device (a phone scanning a QR code)
and a backup login are added in Settings → Account, so that clearing the
browser doesn't lock the server.

From the server's own network the setup asks no code: opened on the same
computer, from a private network (a home's or an office's: `10.0.0.0/8`,
`172.16.0.0/12`, `192.168.0.0/16`, IPv6 `fc00::/7`), a link-local address, or
Tailscale (`100.64.0.0/10`), `/setup` takes a name and makes the first
administrator. Behind a reverse proxy (a NAS's, Caddy, Nginx) the server sees
the proxy's connection: where that connection comes from the same computer or
a private network, every original address the proxy passes on
(`Forwarded`, `X-Forwarded-For`, `X-Real-IP`) has to be in the network too.
Those headers are read only behind such a proxy, and a person outside can't
pass for someone inside through one: the proxy adds their real address after
anything they send. Nothing is set up for this. A proxy that passes no
original address at all makes everyone look like its own network, so add one
(`proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;` in Nginx;
Caddy, Traefik and Synology's reverse proxy add it themselves); so does a port
mapping that hides the client's address (rootless Podman's, or Docker's own
proxy for IPv6 visitors) on a server the internet reaches directly — open its
setup before its port is opened to the internet.

From the outside internet nobody becomes the administrator just by opening a
new server first, as bots look for new servers: the setup page shows no code
field there but says where to go instead — the server's own network (with an
address there, when the server knows one), or the setup link in the server
log (Synology: Container Manager → Container → the Leafen container → Log;
`docker compose logs leafen`; `docker logs <container>`). Under "Other ways"
it takes the code itself: the link's, or the one given in `LEAFEN_SETUP_CODE`
(at least eight characters; hosting templates make one,
[Hosting templates](hosting-templates.md#the-first-administrator-on-each-platform)).
Wrong codes are limited by address. Once the first administrator exists, no
way opens the setup again. The desktop app's own server is set up in its
window alone, which its shell hands the code. Servers run by automation on a
closed network can turn the code off everywhere with
`LEAFEN_SETUP_WITHOUT_CODE=1`.

The URL uses `LEAFEN_PUBLIC_URL` when it is set;
otherwise it uses the address Leafen serves on, with `127.0.0.1` for a server
listening on every interface. A container is told the port it is published on
(`LEAFEN_PUBLISHED_PORT`, which `compose.yaml` and `scripts/run-self-hosted.sh`
set), so its URL opens on this machine, and `leafen setup status`, device
codes' links and the MCP address for agents here say the same; behind another
port mapping, open the same path on the mapped address. From another
computer, open it with the server's name or IP address in place of
`127.0.0.1`, or over SSH (`ssh -L 8090:127.0.0.1:8090 <server>`) as printed.
For a container, retrieve the URL with `docker compose logs leafen` (with
Compose) or:

```sh
docker logs leafen
```

To always print the URL during a native run, use:

```sh
./dist/leafen --setup-open=print --dir=./data \
  serve --http=127.0.0.1:8090
```

## Lost devices and forgotten passwords

Most accounts log in by their devices: a device stays logged in until it is
logged out, and a logged-in device lets another join with a code (the
workspace menu's **Add a device**, also in Settings → Account). Settings →
Account → Backup login adds a way in
from any browser: an email with a password, or a passkey. An account made
with a name only sets its first password there without a current one, and no
device is logged out; changing a password logs the other devices out, and
removing it leaves the account to its devices. Someone who lost every device gets a one-time
code another way: a member from an administrator, who makes one with **Device
code** on the account's row in the server console's users and hands it to
that person only; anyone from the server's machine:

```sh
./dist/leafen --dir=./data account device-code --email=you@example.com
```

`--name` names the account by its name where no other account has it, and
`--id` by its ID. On the desktop app on a Mac or Windows and the phone app on
an iPhone, the app's own server's login page also offers **Log in as this
device's owner**: Touch ID, Face ID or the device's password on Apple devices,
Windows Hello or the Windows account's password on Windows, and the app makes
the code itself (no page can ask for one without that check). The code signs
one device in, once, within ten minutes: open
the address it shows (with `LEAFEN_PUBLIC_URL` set), or choose **Log in
with a code** on the login page and type it. It works while the
server runs, and the server audit log keeps who made it. The desktop app takes
the same command on its own server's data, run as its program:
`Leafen.app/Contents/MacOS/leafen-desktop account device-code --name "Your Name"`
on a Mac.

An administrator is never kept out of their own server, even one made with a
name alone and no password. While none of a server's administrators has it
open on a device, the login page opened on the server's own network (this
computer, a private network, a link-local address or Tailscale, the same rule
as the first setup's) says so in one line under **Log in with a code**, with
**Show how** for where to look, and the server prints a one-time code for its
first administrator in its own log — the terminal it runs in, or `docker
compose logs leafen` — and `leafen setup status` shows the same code. A
login page opened from outside says nothing of it and makes no code. The
code logs one device in, once, within ten minutes, and is never put in an
address; asking again after that prints a new one, and the server audit log
keeps each. On the desktop and phone apps, a window that
lands on its own server's login page without its person having logged out
there signs the person who set that server up back in by itself; after a
logout the person asked for, the owner check above is the way back.

A device that joins with a code isn't asked to replace a temporary password;
the next login with that password is.

Each account in the server console's accounts says where it is: the
workspaces it owns or is a member of, with its role, and those where pages
were shared with it as a guest, with how many pages — the first few and how
many more — or that it is in no workspace. `GET /admin/accounts` gives the
same as `joinedWorkspaces`, `guestWorkspaces` and their counts.

The server console's accounts show, under an account's "Devices", how it came
in — by which invitation (an email invitation, a page shared by email, an
invitation by name, a workspace's or teamspace's invite link, or an allowed
email domain), to which workspace, from whom and when — and each device signed
in to it with its last use and address. Logging one out ends it at once: that
device is back at the login screen, and the audit log keeps it.

With email delivery set up (Server settings), "Forgot password?" on the login
screen mails a single-use reset link. A server that sends no mail says so there
and asks the person to turn to an administrator, who gives their account a
one-time password from the server console's accounts; the person chooses a new
one at the next login. An administrator's agent does the same through
Leafen's MCP: it leaves the password out, Leafen makes the one-time password
and answers it once, and the agent keeps it in a file for the administrator
with `leafen mcp secret PUT /admin/accounts/<id>/password '<the body>'
--save-file temporaryPassword=<file>` (in Windows PowerShell, the body on
standard input: `$OutputEncoding = [Text.UTF8Encoding]::new($false); '<the
body>' | leafen mcp secret PUT /admin/accounts/<id>/password - --save-file
temporaryPassword=<file>`) — or, if it may not handle secrets, leaves
it to a step in Settings that shows it to the administrator alone.

An administrator who forgot their own password, or a server's only
administrator, resets it on the server's machine. Whoever can run Leafen on the
data folder can read all of it anyway, so the command trusts them:

```sh
./dist/leafen --dir=./data account reset-password --email=you@example.com
```

In a container:

```sh
docker compose exec leafen leafen account reset-password --email=you@example.com
```

It works while the server runs. It shows a one-time password once, or takes
yours with `--password-stdin`, ends the account's logins, asks for a new
password at the next login and keeps the reset in the server audit log.
Two-step login stays on; add `--turn-off-two-step` when the authenticator and
its recovery codes are lost too, and turn it on again after logging in.

The desktop and phone apps' own server is set up and logged in to as any
server is: its first setup, in the app's window, takes a name (the person's
name on the device, or a friendly one), and the window keeps its login in
the system's secret storage until it logs out. An account an earlier version
made for the app moves over by itself: the window keeps a login for it, and
it becomes an ordinary account with no email. A data folder from another
server opens in the app too, and its people log in as they did there. The
other way round, an earlier version's app data moved or restored onto a
server becomes the same account there when the server starts, and its person
gets in with `account device-code --name` (above).

## What administrators see

A server's administrators can see everything on the server. In Settings →
Server → Workspaces they open any workspace: its pages, read only —
private pages too — its files, largest first and by person, and what it has
made public. They may turn a site, form or link off, put a page in the trash or
delete it from there, and delete a file no page uses; a legal hold still keeps
what it holds. They don't become members of the workspace. Everything they open
or change there is recorded, and the workspace's owner, and its organization's
administrators, read the record in Settings → Security → Server administrator
log. Sign-up and invitation pages tell people so.

## Shared limits

The server keeps each person to a share of what everyone uses, with no
setting to make:

- Requests are counted per person when they are logged in, wherever they
  connect from, and by address otherwise (an IPv6 address by its /64). People
  behind one company address don't share one count. Searches, writes, link
  reads and starting an export or import count for more than a plain read.
- Password login slows down per account, from whatever addresses the
  attempts come: five wrong passwords within 15 minutes and the account's next
  password login waits a minute, answered at once without checking the
  password; each wrong one after a wait doubles it — 2, 4, 8 minutes — up to
  15. It never locks the account: a logged-in device's code, a passkey or SSO
  still log in at once, the right password starts the count over, and so does
  a quiet quarter hour.
- A person holds at most 100 live page connections at once; one connection
  sends at most 32 MiB in a message, 100 messages and 8 MiB a second (with
  room for a burst). Past that the editor reconnects and syncs again.
- The server reads at most 4 outside links at once for one person and 32 for
  everyone; a linked PDF is passed on as it arrives.
- Background jobs take turns by person, and the same export asked for again
  while it is being made returns the one in progress.
- When the data folder's disk falls under its floor — 5% of the disk, at
  least 2 GiB, plus the databases' size — uploads, imports and exports are
  refused with the reason, editing goes on, and administrators get one notice
  in their inbox. It lifts on its own once there is room.

## How much one server holds

One 10-core server holds up to a 300,000-person organization under
Microsoft's typical capacity-planning user model — about 300 requests a
second, p95 48 ms — and about 180,000 people under its heavy model.
[The benchmark](benchmark.md) has how it was measured, what it leaves out, and
how to measure your own machine with `bench/`.
These are measured guides, not promises: your people's use differs, so watch
the signs at the end of this section rather than the numbers alone.

The figures this section gave before (about 3,000 people on 2 cores, 4,000 on
10) came from an older method — a few hundred accounts shared by simulated
people acting every 10 seconds, ten times Microsoft's typical pace — and
aren't comparable. More cores help reading and search more than writing:
Leafen writes through one database connection, so a server where many people
write at once tops out near the same rate whatever its cores.

**Storage.** Under a heavy run — 400 people at twice the real pace and 125
people typing without pause — the main database grew about 400–470 MB an hour:
page history about 55, the organization audit 27, the graph journal 48,
activity 16 and the collaborative documents 16 MB an hour, with their indexes.
The write-ahead log stops at 200 MB. Size the disk and backups from this rate,
scaled to how much your people write; most teams write far less.

**Memory.** Under that run the process used 0.7–1.7 GB of physical memory,
counting the database pages it maps, and its own heap 0.25–0.75 GB. A minute
after the load stopped the heap was back to 26–123 MB, and nothing grew over
the hours. The caches follow the memory limit (see Without Docker above): the
open documents' cache takes 1/256 of it and the search index's copy of long
pages (64 KB and up) 1/32.

**Long pages.** A page of hundreds of thousands of characters that several
people edit at once costs writing and search indexing in proportion to its
size, once it no longer fits those shares. A higher memory limit raises the
shares; splitting a very long page is better. Ordinary pages, up to about
100 KB, aren't affected.

**Past the limit.** Search slows first (on few cores); then writes slow and
are refused with `LEAFEN_SERVER_BUSY`, which the apps try again. Everything
recovers when the load falls. Search index merging gives way to people's
writes, so while the server stays saturated the index grows, and it is merged
once there is room. To make room: raise the memory limit, give the server more
cores (for reading and search), split very long pages, or move a workspace to
another server ([Moving one workspace](#moving-one-workspace)).

**Near the memory limit.** In a container, past 80% of its memory limit (or
the limit less 256 MiB) the long work — imports, Notion moves, exports,
copies — takes one turn at a time with a short pause between, and people's
edits and reads go on. Only past 95% (or the limit less 64 MiB) are people's
requests refused too, with `LEAFEN_MEMORY_PRESSURE`, which the apps try again
for up to 20 seconds before saying the server is too busy; that lifts a little
under 95%.

**Seeing you are near.** `leafen doctor` prints the memory plan. The request
log keeps every failed request and every one that took a second or more
([Request log](#request-log)): more slow requests, and refusals with
`LEAFEN_SERVER_BUSY`, are the sign.

## Update notice and sponsor list

Once a day the server asks whether Leafen's two signed lists changed, the
release notice and the sponsor list ([release lists](release-lists.md); plain
requests with nothing about the server's workspaces, accounts or use), to tell
administrators when a newer version or a security fix is out, and to keep the
list of the sponsors funding Leafen's development current
([People backing Leafen](sponsors.md)). Without the internet it uses the list
bundled with the release. Settings → Server → Updates turns the daily check
off, and `LEAFEN_UPDATE_CHECK=off` for the whole deployment.

`LEAFEN_SPONSOR_CREDIT=off` takes Leafen's thanks out of the app for the whole
deployment: the workspace menu's "⭐ Support Leafen on GitHub" line (shown to
each person until they press it once) and the sponsor list and the row of top
sponsors at the bottom of Home, which come in an early update after the first
release — for a company's own workspace that names no one else. There is no
switch for it in the app.

## Remote access for agents

Nothing opens to the internet by itself. Agents on the computer running Leafen
connect at its local address. When something first needs an outside address —
choosing to start with an agent in the first-run window, or connecting an agent
on the web in Settings → Features → Agents — an administrator is asked once how the server
is reached from outside:

- **Make an address with Tailscale** (recommended; see
  [Tailscale](#tailscale) below): log in right there, allow the address, done.
- Under **Other ways**, **Use my own domain**: an https address your own
  reverse proxy or port forwarding already serves, or a tunnel of your own
  Cloudflare account — in Settings → Server → Access from outside.
- **Try it for a while, no account**: a Cloudflare quick tunnel, decrypted at
  Cloudflare, whose address changes each time it starts (the people whose
  agents came through an old address are told in their inbox to connect them
  again).
- **Later**: only this computer reaches it; the question comes back the next
  time something needs an outside address.

A company's server is offered its own address first: when the administrator
mails from a domain of their own (not a common free webmail service such as
Gmail, Outlook, iCloud, Naver or Daum) or an organization on the server has
verified a domain or SAML domain, the question opens on **Use a company
address** — handing it to an agent, with the company's domain and
certificate — and Tailscale, whose free plan is for personal use, comes small
below with the rest.

Change or turn off remote access in Settings → Server → Access from outside.
The address chosen there is the server's address everywhere: page, share and
site links, invitations and other mails, agent connection texts, and the
address to register with a login provider. `LEAFEN_PUBLIC_URL`, when set,
comes first. A Tailscale address stays the server's address while Tailscale is
briefly away; a quick tunnel's only while it runs. With none, each request's
own address is used, but for mails: anyone can write any address on a request,
so a password reset or a verification mail links only to an address the server
knows is its own, the one its connection really came in on or one an
administrator's browser has opened it at.

### Changing the address

When the chosen address becomes another — Tailscale's to your own domain, one
domain to another — the old one keeps answering every request with a
permanent redirect (308: same path, query, method and body) to the same place
at the new one — but for an agent's and a program's with a key: MCP and
OAuth requests, and API calls with a key, an app's or agent's token or a
Notion-compatible integration's token, are answered at the old address, as
most clients drop their login on a redirect to another address. A login
in a browser or the apps still gets the redirect. It does so for 90 days,
and while requests still come to it, 30 days past the last one, up to a year
from the change. Access from outside shows each old address, until when it
forwards, and what still uses it (agents, programs with keys, apps, links
and bookmarks, login providers). The server keeps a way in at the old
address where it held it — the old Tailscale device or the old certificate —
but an address behind your own proxy or Cloudflare tunnel forwards only while
that still sends it here. A quick tunnel's address isn't forwarded.

What follows the new address by itself: links, invitations, mails, device
codes and QR codes, agent connection texts, the agents' login documents and
everything else the server makes when it is used; logins and device codes
don't depend on the address at all. What was handed out before — a mail, a
saved link — goes on through the redirect, and an agent set up with the old
address goes on there: its key or OAuth token works at both addresses, and
the OAuth issuer and resource it checks stay the old address's while it
forwards. Settings › Agents marks such an agent as one that can move to the
new address, and says to connect it again only once the address it came
through no longer forwards. About 14 days before an old address stops
forwarding, and again when it stops, each person whose agents, programs with
keys or Notion-compatible integrations still came through it last is told in
their inbox, with their names, to move them to the new address. While the old address forwards, the desktop and
phone apps move their server to the new address when they next start (once
the server there names the old one its own and, if they remembered its
identity, shows the same), keeping the login; the desktop app points this
computer's Claude Code and Codex settings there too; a link to the old
address in a page opens in the app; and a standby keeps its primary's new
address from the primary's beat.

A browser keeps its login by address, so a page opened at the old address
(a bookmark, a link) gets a small page there instead of the redirect while the
address forwards: with the login that browser kept at the old address, it
takes a pass — used once, within two minutes, for that login — and opens the
same page at the new address with the pass in the address's fragment, which
no server or log sees. The new address trades it for the device's login
there and the one at the old address ends; a page with no login there just
goes on. This works however many times the address changed, from every old
address that still forwards. The old address's push subscription ends with
it, and the new address offers to turn notifications on again and, where the
person's passkeys were made for the old address, to make one for the new.
Requests other than page loads are redirected as before. A quick tunnel's
address isn't forwarded, so nothing carries over from it.

Everyone on the server is told of the new address in their inbox, with what
of theirs keeps the old one: bookmarks; notifications and passkeys, as above;
app passwords, which sync apps keep beside the address; and, for
administrators, the apps' webhook addresses written at outside services.

Google and OIDC login go on too: the provider's OAuth client holds the old
redirect address, so while that forwards here, logins go back through it
and the redirect carries them on. Settings › Server › Sign-up and login
(and the administrators' inbox) shows the new redirect address to add at the
provider; once it is added there, say so with "I've added it", and the first
login that comes back to it ends the note. If the provider refuses the new
one, "Go back to the earlier address" sends logins the old way again. An
app's outside logins do the same, from the app's secrets in Settings ›
Server › Apps, and an organization's SAML login from its identity
settings: logins start under the entity ID and come back to the ACS the
identity provider has, and its owners are told the new metadata and ACS
addresses to add. The SAML metadata at either address says that address's
values, so an identity provider can read the new ones from the new address.

What outside programs call — an app's webhooks and endpoints — is answered
at the old address itself rather than redirected, as webhook senders mostly
don't follow redirects. A webhook whose last call came to the old address
says so beside its new address in the app's secrets; give the sender the
new one.

A server used at `localhost` or a home address (the desktop app at home, say) that
gets a proper address keeps both: the local address isn't redirected, so it
still works at home when the internet is down. A browser opened at the local
address offers once to go on at the proper one, logged in and on the same
page (a one-time device code carries the login). While a server has only a
local address, the places that make a link or QR code for another device —
the new-device QR, invitations, a page's share link — say that only this
computer (or only the home network) can open it, and to turn on an outside
address; once there is one, they are made with it.

A service in the service hub goes on through the redirect; its tokens renew
without the address. At its next login, a client the server registered on
the service itself, or one known by the server's client metadata document
(answered at the old address too), moves to the new address by itself, the
old client kept until that login succeeds. A client you made on the
service keeps the address the service has: Settings › Connections shows the
new return address to add there, with "I've added it" and "Go back to the
earlier address", as for Google sign-in.

What still needs a hand, until the follow-ups that take it over land: an
agent connected with a key rather than OAuth on another computer, browser
logins and passkeys at the old address, and web push subscriptions (turn
them on again at the new address). The redirect carries these while it lasts.

What remote access keeps on the server, all in the data directory and in its
backups: Tailscale's device state (`tailscale/`, kept until you sign the device
out or delete the folder), the server's identity key for the apps
(`identity/`), and the certificate-log notes for its address
(`address-watch/`). Leafen runs no service of its own for remote access and
receives none of this; Tailscale or Cloudflare keep what their own terms say
for the way you chose.

The address a person came from (for request limits, login attempts and the
audit log) is taken only from the server's own paths for Tailscale and for
its own certificate, and from the tunnels' door: a listener on this computer's
loopback, the port after the server's own unless it's taken
(`LEAFEN_TUNNEL_DOOR_PORT` sets it), that cloudflared reaches. A tunnel of
your own account names the door as its service, as the settings show; the
screen warns while one still reaches the server's own port. A
`Cf-Connecting-Ip` or `Leafen-Tunnel-Client-Ip` header on any other request is
dropped, so a reverse proxy beside the server can't pass a made-up address on.

### Tailscale

A fixed public address from your own Tailscale account, such as
`studio.tail1234.ts.net`, with nothing to install:

- Choose Tailscale in Settings → Server → Access from outside, run
  `leafen tailscale` (it prints the addresses below and waits), or ask an agent
  connected as an administrator (`leafen_remote_access` with `tailscale`). The
  server runs a Tailscale device of its own inside Leafen (Tailscale's `tsnet`
  library) and shows where to log in — any browser, a phone's too; make an
  account there if you have none — then where to allow the device a public
  address (HTTPS and Funnel) if your tailnet doesn't yet. It notices each step
  by itself and opens the address on port 443 through Tailscale Funnel.
- Connections pass through Tailscale but are decrypted on this server; its
  certificate comes from Let's Encrypt through Tailscale.
- The device's state is kept in the data directory's `tailscale/`, so a backup
  restores the same device and address. The address's first part is the first
  workspace's name in plain letters, or `leafen`; the part after it is your
  tailnet's name, which you can change in Tailscale's DNS settings before you
  share links. Remove an old device of the same name in Tailscale to get the
  same address again.
- Tailscale answers the address's DNS, so the server watches the public
  Certificate Transparency logs for its name about every six hours and closes
  the address when a certificate it didn't get appears, until an
  administrator says it is theirs. The Leafen apps remember the server's own
  signed identity there (`identity/` in the data directory) and stop when
  another answers.
- Tailscale's free plan is for personal, non-commercial use; a company needs
  its own paid Tailscale plan (Tailscale's pricing, read 2026-10-19).
- Tailscale's diagnostic log upload is turned off. Funnel carries a traffic
  limit Tailscale doesn't publish.
- While Tailscale waits for you or can't be reached, the server doesn't switch
  to a quick tunnel by itself. Log the device out in the same settings.

### Do it yourself

When Tailscale can't be used — a closed network with no internet, a country
or company that blocks Tailscale — the address choice says so and offers to
hand the work to an agent, with what the server measured (its system, whether
it reaches the internet and Tailscale and why not, its public IPv4 and global
IPv6, whether its router opens ports by itself). The same ways, by hand:

- **A closed network**: give the server an internal DNS name and use the
  company's certificate (or one from its internal CA). Either put it on the
  reverse proxy the IT team already runs and choose "Use my own domain" in
  Settings → Server → Access from outside, or let the server take HTTPS
  itself: `leafen address --address https://wiki.corp.example --cert
  chain.pem --key key.pem --port 443`.
- **The internet, but no Tailscale**: port forwarding on your router (or its
  automatic port opening), the server's global IPv6 address, a small VPS with
  [frp](https://github.com/fatedier/frp), or a tunnel service that works where
  you are — and a domain name pointing there, with a certificate from Let's
  Encrypt on your proxy. Weigh what each costs, what it exposes and how long it
  lasts.
- **Rules**: HTTPS only; never open Leafen's admin pages without login;
  don't turn off security settings.
- **Last**: make the address the server's own — "Use my own domain" in the
  settings, `leafen address` (with `--cert`, `--key` and `--port` when the
  server takes HTTPS itself; the files are kept in the data directory's
  `address-tls/`), or an admin agent's `leafen_remote_access` with
  `set_address`. The server checks it: that the name resolves, and which
  certificate answers there, until when, and whether this computer trusts it.
  Then open it on a phone.

## Server settings

After setup, Leafen administrators can configure SMTP email delivery, required
email verification, Google login, and a discovery-based OIDC provider from the
Leafen server console. These settings do not require rebuilding the binary or
container.

Keep service credentials out of command history and source files. Back up the
data directory after changing server settings.

A workspace owner who adds a member by an address no account here has invites
it: the invitation waits in the workspace's people until an account verifies
the address — by signing up, confirming it, or an administrator making the
account where sign-up is closed — and then joins as a member. With email
delivery set up the address is told by mail, within the limits page
invitations keep; otherwise the owner copies its link and tells them. An
owner can also invite someone with no email by name, under "Invite by name" in
People: the link is for that person alone, works once, and is copied and
passed on by the owner. Invitations, by email or by name, end seven days after
they are made.

Opening an invitation asks only a name, with a friendly one suggested (an
invitation by name suggests the one its owner wrote): the
person is in on that device, which stays logged in until logged out, with no
password to choose (Settings → Account adds a password or a passkey as a
backup). An email invitation's account takes the address it was sent to,
already confirmed, and is a member at once. A workspace's or teamspace's
invite link makes an account with no email, which then asks to join; an
owner approves it in Requests while the link is on. Accounts made this way
are limited to ten an hour from one address. Someone who already has an
account here logs in instead and the invitation is theirs.

Workspace members can read the workspace's People list. When member additions
require an owner's approval, they send requests from People and owners decide
in Requests. Guests never read that list while awaiting membership.

A new organization starts where Notion's paid plans do: members may ask to
add other members, and anyone, guests included, may request membership. An
organization owner can narrow either in Settings → Security → Members &
guests (to owners adding members themselves; to existing guests, people who
are not guests, or no one requesting); organizations made before these defaults keep
the narrower ones they had. Guests ask from the workspace menu; sending a
request grants no access. Approval checks the current organization and server
admission policies again and commits the membership with the decision; a
request sent from an invite link is approved while that link is on, whatever
those membership request settings say.

An organization's login rules come before device registration. Where its
SAML single sign-on is required, its members log in only through it: a
device code is refused, when made and when used, with that reason. Its owner
may instead require a password and 2-step verification in Security → Members
and guests → Login: the devices its members joined with a code are signed
out, and codes for them are refused. Owners and guests are not asked, so no
owner is locked out.

### Request log

The server records the requests it answers in `auxiliary.db`, for finding out
what went wrong. It always keeps failed requests, requests that take a second
or more, and changes made by administrators, logins and setup; of the other
successful requests it keeps one in a hundred. Settings → Server → Audit log →
**Request log** sets how many days entries are kept (5 unless changed) and
whether successful requests are kept one in a hundred, all, or none. At most
300,000 entries are kept, the oldest going first. The organization audit, the
server administrator log and the read log are kept apart and aren't affected.

### Read log

The server records every page, item and file it gives out — who read what,
when and how — as it records every edit. Settings → Server → Audit log →
**Read log** turns it off for the whole server (on unless changed); turning it
off is kept in the server administrator log, and what was recorded stays. The
server gathers the reads in memory and writes them about once a second, or
sooner when many reads gather, so reading never waits for the log. A person's reads of one thing the same way within ten
minutes are one entry that counts them. Everything gathered is written before
the server stops or restarts for an update, before a backup, and before the
read log or the audit log is shown. A server that stops abruptly — power lost,
the process killed — can lose about the last second of read records.

An organization's audit log and read log are kept: Leafen deletes none of it
unless the organization chooses a period (Settings → Enterprise controls →
**Audit log** → **Audit record retention**, a day or more), and then only
what is older, once a day, apart from what a legal hold covers. The app data
reads apps show are kept as long as the audit log.

The updates feed and page analytics (page views and their viewers, database
row pages included, workspace analytics' member views and searches, and the
page views the Notion-compatible admin API reads) are kept for a year. An
organization sets another period for each, any number of days, or keeps them
(Settings → Organization → **Data retention**): whoever keeps the audit log
sets the feed's, whoever sees analytics sets the analytics'; the change is in
the audit log. A longer period keeps what is already there; a shorter one
lets older records go, analytics at once and the feed within the hour, and
the analytics ranges offered go no further than the period. The audit log's
own record of each change stays by its own period.

None of these periods lets go of what an active legal hold covers (Settings →
Enterprise controls → **Legal holds**). A
hold over the whole organization keeps all its records; over a workspace,
that workspace's; over a page, the audit and read records about that page,
its views and its place in the feed; over a person, what they did and what
they read or viewed. Aggregate counts (a page's views by day) go by the page
and the workspace alone. Once the hold is released, its records go by their
period again at the next sweep. A server running without the organization
module can't read holds, so while any is active its sweeps keep everything.

## Cover image search

The cover gallery is bundled with Leafen. Unsplash search is off until the
server operator supplies `LEAFEN_UNSPLASH_ACCESS_KEY` through the process or
container environment, then restarts the server. Use the access key for the
operator's own Unsplash application; do not place it in a client build or use
one bundled key across unrelated servers. Regular users need no developer
account. Remove the environment value and restart to turn search off again.
Keep the key out of command history, source control and logs. The Docker
launcher forwards this environment variable by name. After supplying it from
your credential store, use the usual `./scripts/run-self-hosted.sh --image leafen:local`;
`--print` shows the variable name only.

The server sends searches and explicit photo selections to `api.unsplash.com`.
Browsers load the photo's API image URL directly from `images.unsplash.com`.
Selection notifies Unsplash's download endpoint before returning a cover;
provider failure leaves the previous cover intact and lets the user retry.
Leafen displays the photographer and Unsplash links in search results and
where a selected cover is shown. Public credit is carried in a versioned URL
fragment with the cover, so history, copies and archives preserve it without
another provider request. The fragment is not sent to the image CDN.

Follow [Unsplash's API guidelines](https://help.unsplash.com/en/articles/2511245-unsplash-api-guidelines)
and the application's terms when enabling the integration. Existing selected
photos remain external images after search is disabled; bundled gallery and
uploaded covers do not require this provider.

## Place search

Place cells always allow entering a name/address and picking a point on the
map. Search is off by default. A server administrator can enable it under
Settings → Server → Map, select Nominatim, Photon, Kakao or NAVER, and save the
endpoint and required credentials. Reads show only the provider and whether
configuration exists; saved endpoint/key inputs become blank. Leave those
inputs blank to retain their values, or use **Remove saved provider settings**
to remove the configuration and disable search. Keep the administrator secret
key together with the data directory when backing up or moving the server.

Use a self-hosted or contracted Nominatim/Photon endpoint. The OSMF public
`nominatim.openstreetmap.org` service is rejected because its
[usage policy](https://operations.osmfoundation.org/policies/nominatim/)
forbids generic place-search integrations. Kakao requires its REST API key;
NAVER requires its Maps API key and key ID. Their official HTTPS endpoints are
used when the endpoint input is blank. A custom keyed endpoint must use HTTPS.
The operator must confirm its provider's terms and permitted use before
enabling it.

Environment configuration overrides saved configuration after a restart:

```sh
LEAFEN_PLACE_SEARCH_PROVIDER=photon
LEAFEN_PLACE_SEARCH_URL=https://geocoder.example.com/api
LEAFEN_PLACE_SEARCH_API_KEY=       # Kakao / NAVER only; load from a secret store
LEAFEN_PLACE_SEARCH_API_KEY_ID=    # NAVER only
LEAFEN_PLACE_SEARCH_DISABLED=1    # force search off, including saved settings
```

Do not set `LEAFEN_PLACE_SEARCH_DISABLED=1` when enabling search. Remove it or
set it to `0`. Supply secrets through the process environment, outside source
control and command history. The Docker launcher forwards these variables by
name; `--print` displays names only. An invalid environment configuration
disables search. Stop and correct the configuration if it cannot be enabled;
never fall back to an unconfigured external service.

Queries leave the server only after **Search** or Enter, or when a map view's
pin mode places a page: the server then asks the same provider for the point's
name (its reverse lookup on the endpoint's host — Nominatim's and Photon's
`reverse`, Kakao's `coord2address`, NAVER's reverse geocoding), which becomes
the page's title and the place's name; without a provider the page has the
point alone. The server sends the
search text to the configured provider without the user's account, browser IP
or authentication token, limits outbound requests to one per second across the
instance, and caches at most 128 searches in memory for one hour. Redirects
are refused, requests time out after eight seconds, and each response is
limited to 1 MiB and ten valid locations. Provider failures preserve the
existing place and allow a retry or direct map selection. Selecting **Current
location** asks the browser for location permission and stores only the point;
it sends no location query to the provider.

## Mobile push

Leafen runs no push relay. Real-time notifications reach a phone as the same
Web Push a desktop browser gets (see `docs/architecture.md`): in the browser
on Android, and on iOS once Leafen is added to the home screen (iOS 16.4 or
later). The Leafen phone apps get no push: while open they keep the unread
count on the app icon, and when the phone lets them check in the background
(about every 15 minutes) they ask the server they last showed what came since
and notify about it. A server at its Tailscale address must show the identity
the app remembers there before the app's login goes to it.

## Logging in on another device

A logged-in device adds another from the workspace menu (the workspace name in
the sidebar) → Add a device, or Settings → Account → Add a device: it
shows a QR code of the server's address with a one-time code, and the same
code to type under "Log in with a code" on the login screen; that
step's "Scan a QR code" also reads the QR with the phone app's scanner or, on an
https address, the browser's camera. A code
works once, for ten minutes; failed tries are limited by address and across
the server. A device that joins stays logged in until it is logged out in
the devices list, and an organization that requires SSO refuses it. The QR
uses the server's public address (`LEAFEN_PUBLIC_URL` or the running
tunnel's), so set one for phones to reach.

## Network proxy

Where only a proxy reaches outside, Leafen goes by it with nothing to set:
the server and the desktop app follow `HTTPS_PROXY`,
`HTTP_PROXY` and `NO_PROXY`, and else the system's proxy settings on macOS
and Windows. An administrator may set one instead in Settings › Server ›
Access from outside › Network proxy — its address, the hosts reached
directly (names, `.corp.example` for its subdomains, addresses and ranges
such as `10.0.0.0/8`) and a user name and password, the password sealed
with the administrator settings key; the desktop app sets its own in
Server accounts › Network proxy. The loopback is always reached directly.

It carries the server's requests out — link previews, webhooks, imports,
the services it connects — which still reach only public addresses: a name
that resolves on the server to a private address is refused, one that
doesn't resolve there goes to the proxy, and the proxy itself is never a
destination.
On macOS and Windows the system's automatic configuration script — at its
address (PAC), or the one the network gives (WPAD) — is followed, as the
system's own evaluator reads it (CFNetwork, WinHTTP), each address's answer
kept a minute; it goes before the fixed proxies, which take over when it
gives none, and the system's exceptions are still reached directly. On
Windows a network that gives no script isn't asked again for ten minutes.
A macOS build without cgo can't read one: the settings say so, and the
address can be set there.

## Reverse proxy

Proxy one public origin to the Leafen listener and preserve the original host,
scheme, and WebSocket upgrade headers. If the proxy cannot preserve the public
origin, set it explicitly for MCP OAuth metadata, view links and the link
previews of shared pages and sites (their title and cover, read by chat apps
and search engines without running a browser). Passkeys always require an
explicit `LEAFEN_PUBLIC_URL`: use the HTTPS origin people open in their browser
(HTTP is supported only on localhost during development). Use a hostname,
because WebAuthn relying-party IDs cannot be IP addresses.

```sh
LEAFEN_PUBLIC_URL=https://leafen.example.com \
  ./dist/leafen --dir=/srv/leafen/data \
  serve --http=127.0.0.1:8090
```

With `LEAFEN_PUBLIC_URL` set, that is the server's address from its first
start: links, invitations and agent connections use it. A way chosen later
under Settings → Server → Access from outside (Tailscale, a quick tunnel, a
different domain), or through an agent or `leafen tailscale`, takes over at
once with no restart; the screen then notes that the start settings still name
the earlier address. An `https://` one forwards to the new address for a while,
as long as your proxy still sends it to the server; an `http://` one, inside
your own network, doesn't. Passkeys stay with the address the variable names.

Expose only the Leafen listener. Keep the public hostname stable: a passkey is
bound to that hostname and cannot log in on a different one. Without the
configured address, account settings explain why adding passkeys is unavailable.
Packaged apps open that server address in the system browser for passkey
registration and login. The server stores public credentials in its database;
the private keys stay with the user's device or password manager, and a full
instance backup preserves the server side of those credentials.

## Health check

```sh
curl --fail http://127.0.0.1:8090/api/leafen/v1/health
```

A healthy server returns JSON with `status: "ok"`.

## Backup

Write backups outside the active data directory:

```sh
./dist/leafen --dir=/srv/leafen/data backup \
  --output=/srv/leafen-backups/leafen-$(date +%Y%m%d-%H%M%S).zip
```

The command returns the absolute path, byte count, and SHA-256 receipt. Keep the
archive and receipt together. Existing output paths are refused.

In a container, write backups to `/backups`: `compose.yaml` and the launcher
mount the `leafen-backups` volume there, beside the data and owned by the
container's user (uid 65532), so it needs nothing set up and outlives the
container. Copy each one off the machine from there:

```sh
docker compose exec leafen leafen backup \
  --output=/backups/leafen-$(date +%Y%m%d-%H%M%S).zip
docker compose cp leafen:/backups/leafen-20260826-120000.zip .
```

(For the launcher's container, `docker exec leafen leafen backup …` and
`docker cp leafen:/backups/… .`) A path elsewhere in the container, such as
`/tmp`, is written as well but gone once the container is made again, as
`docker compose up -d` does after an update. To have them in a folder of the
host instead, mount that folder at `/backups` and give it to uid 65532
(`sudo chown 65532:65532 <folder>`), or stop the container and back up the
whole `leafen-data` volume with the host's storage tools. Never write a
backup inside `/data`.

A backup file can be read by its owner alone, as it holds everything. One
copied out with `sudo` is root's, and one in a host folder is uid 65532's:
copy it with `sudo` and give the copy to yourself before checking or moving
it — `sudo chown "$USER" leafen-20260826-120000.zip`.

A backup holds installed apps too: their packages, approvals, who may use
them, their workspaces and data, schedules, usage and what they read, and their
sealed secrets and logins together with the key that opens them. An app's
temporary store and live rooms are not kept, nor are guests' sessions: a
guest link is kept with the data and works again after a restore until it
runs out, and its guests open it again. If a value can't be opened after a
restore (it was sealed with another key), the app's secrets window says it
needs entering again — or, for an incoming webhook, a new secret — and an
outside login asks its person to log in again; everything else keeps
working.

### Scheduled backups to a folder, a WebDAV server or S3

The server can back itself up on a schedule into a folder it can write (a
cloud drive's synced folder, a NAS share or an external disk keeps the
backups somewhere else too), to a folder on a WebDAV server such as
Nextcloud or Synology, or to a bucket of S3-compatible storage such as AWS
S3, Backblaze B2, Cloudflare R2 or MinIO. Turn it on in Settings → Server →
Backups, or, for a server set up without its screen, with
environment variables (the screen then only shows them):

| Variable | Meaning |
| --- | --- |
| `LEAFEN_BACKUP_FOLDER` | Full path of the backup folder, outside the data directory. Setting it turns scheduled backups on. |
| `LEAFEN_BACKUP_WEBDAV_URL` | Instead of a folder, the `https://` (or `http://`) address of a folder on a WebDAV server. Setting it turns scheduled backups on; `LEAFEN_BACKUP_FOLDER` comes first when both are set. |
| `LEAFEN_BACKUP_WEBDAV_USERNAME` | The WebDAV server's user name. |
| `LEAFEN_BACKUP_WEBDAV_PASSWORD` | The WebDAV server's password; an app password where the server offers them. |
| `LEAFEN_BACKUP_S3_BUCKET` | Instead of a folder or WebDAV, the bucket of S3-compatible storage. Setting it turns scheduled backups on; the folder and WebDAV variables come first. |
| `LEAFEN_BACKUP_S3_ENDPOINT` | The storage's `https://` (or `http://`) endpoint, such as `https://s3.us-east-1.amazonaws.com`. |
| `LEAFEN_BACKUP_S3_REGION` | The bucket's region (`us-east-1` when left empty; R2 takes `auto`). |
| `LEAFEN_BACKUP_S3_PREFIX` | Optional. A prefix inside the bucket the backups go under. |
| `LEAFEN_BACKUP_S3_ACCESS_KEY`, `LEAFEN_BACKUP_S3_SECRET_KEY` | Keys that can read, write, list and delete in that bucket. |
| `LEAFEN_BACKUP_S3_PATH_STYLE` | `true` for storage that puts the bucket in the address's path, as MinIO does. |
| `LEAFEN_BACKUP_INTERVAL` | How often: hours (`12`, `12h`), days (`2d`), `hourly`, `daily` or `weekly`. Default `daily`. |
| `LEAFEN_BACKUP_KEEP` | How many snapshots to keep, 1 to 365. Default 7. |
| `LEAFEN_BACKUP_RECOVERY_CODE` | A recovery code that seals the backups; `leafen backup-code` prints a new one. A place off this computer (WebDAV, S3, a cloud sync folder) takes no backup without this or a password. |
| `LEAFEN_BACKUP_PASSWORD` | A password that seals the backups, with or instead of the recovery code. Choose at least 12 characters, not a common one. |

In a container, the folder `/backups` (above) takes scheduled backups too.

Backups are off until turned on. Once the data is worth keeping — a page of
one's own a week after the first workspace, pages imported from Notion counted — the server's
administrators see a band at the top of the app suggesting it, until backups
are on or an administrator dismisses it. In the desktop app the settings list
the cloud drives' folders found on the computer first (Google Drive, iCloud
Drive, Dropbox and OneDrive, where each app keeps its folder); picking one backs
up there every day, keeping the last seven, starting at once. Keep the data
directory itself out of a synced folder: only the backups go there.

When a run fails it tries again within the hour, and the settings show why.
From the second failure in a row the server's administrators see a band at
the top of the app, and get a mail when the server sends mail (Settings →
Server → Email delivery) — at most one a day while it keeps failing. Both stop
once a backup succeeds.

The first backup runs when backups are turned on. Each snapshot is a folder
`Leafen backups/snapshots/<time>/` holding the whole database (`data.db.gz`) and
`snapshot.json`, which lists every other file of the data directory by its
SHA-256. File contents are stored once in `Leafen backups/blobs/` and shared
between snapshots, so a run adds only new files. Snapshots past the number kept
are removed, with the contents no remaining snapshot lists. A snapshot is
written under a `.incomplete-` name and renamed only once whole.

On a WebDAV server the snapshots take the same layout under `Leafen backups/`
in the folder the address names: each file is uploaded whole, with its length,
and a snapshot is published with `MOVE`. Saving the settings first creates
that folder and writes a test file there, so a wrong address or a refused
password shows at once. Redirects aren't followed, so the credentials go only
to the address given; use `https` unless the server is on the same private
network.

In S3-compatible storage the snapshots take the same layout under
`Leafen backups/` in the bucket (and prefix). Objects can't be renamed, so a
snapshot is copied from its `.incomplete-` prefix to its name with its
`snapshot.json` last, and counts as whole only once that is there. Saving the
settings writes and removes a test object first, so wrong keys or a wrong
endpoint, region or bucket show at once. Give the keys access to that bucket
only.

A snapshot includes the server's secret keys (`.secrets`, `secure`,
`web_push`), without which the database's encrypted settings can't be read.
Settings → Server → Backups → **Backup protection** seals the backups with a
recovery code, a password, or both. **Make a recovery code** shows a code of
32 letters and digits once, to copy, print or write down and keep away from
the server; a password of at least 12 characters, not a common one, can be
chosen with it or instead. A place off this computer — WebDAV, S3 or a cloud
sync folder — takes no backup until one of the two exists; a folder on this
computer may stay unsealed.

Sealed, the database, each snapshot's list and every file are encrypted
(AES-256-GCM in 64 KiB pieces) with a random key of the backups' own, and
contents are named so that their hashes don't show. The code and the password
each wrap that key in `Leafen backups/keys.json` (the code through HKDF, the
password through PBKDF2 at 600,000 rounds), so either opens every backup.
Changing one, or making a new code, wraps the key again: earlier backups open
with the new one, and an old code no longer opens anything. One of the two
always remains. Backups that only a password sealed, from before, still open
with it. Without the code and the password, no one can open the backups. It leaves out logs (`auxiliary.db`), the backup
archives of the section above, the work folders of exports and imports, and
what belongs to the machine rather than the data: downloaded programs (`bin`),
the desktop app's updates and window state, diagnostics and the replica's
working state.

### Continuous replication

With backups on, Settings → Server → Standby server → **Continuous replication**
sends every change to the same place within seconds. If the server is lost,
another server given that place restores from it, a few seconds behind. Set
`LEAFEN_BACKUP_REPLICATION=on` (or `off`) to decide it for a deployment.

- The database goes out through [Litestream](https://litestream.io) as small
  files each second, into `Leafen backups/replica/db/`. The files people put
  in and the secret keys go into the backups' own contents within about a
  second (longer on a very large data folder), listed in
  `Leafen backups/replica/files/`. With backup protection, all of it is
  sealed with the backups' key.
- The settings show how far behind the replica is. Past a minute behind,
  administrators see a band and get a mail once a day while it lasts. The
  server itself keeps working while the place is unreachable; the changes wait
  and go out once it answers again.
- Measured on a development machine with a local folder (25 MB database): the
  changes of the last 0.1–1.3 seconds before the server was killed were lost,
  files none; restoring took 2.6 seconds, then the server started again.
- Only one server writes a replica. `Leafen backups/replica/owner.json` names
  it. Turning replication on where another server's replica is refuses; restore
  from it instead, or choose another place.

To restore on another server, give it the same backup place, then choose the
continuous replica at the top of **Restore from a backup**. Sealed backups
show as not yet readable there until their recovery code or password is
given under the list; the server then keeps it and goes on sealing with the
same key.
The restored server takes the replica over and goes on replicating to it. The
server it replaced, if it comes back, reads that and takes no more changes:
everyone logged in there sees a band pointing to the new server's address
(`LEAFEN_PUBLIC_URL`). An administrator can keep it apart as a server of its
own, with replication off, in the same settings.

From the command line, into a fresh data root:

```sh
./dist/leafen --dir=/srv/leafen-restored restore --replica \
  --input="/mnt/backups/Leafen backups" --recovery-code-file=/root/backup-code
```

Without `--input`, the place the backup environment variables name is used.
A restored server keeps the replaced server's backup settings, except a
folder place, which takes the path this server reached it at. Devices that
knew the replaced server re-sync, since the restored data starts a new
history.

If the place a replicating server writes to goes empty — an unmounted share
leaves an empty folder behind — the server doesn't start a new replica there:
it reports the replica missing until an administrator turns replication off
and on again.

### Standby server

A second Leafen server can follow a replicating server and take over by
itself when it stops answering. Leafen runs no standby for anyone; it is a
server of your own.

1. On the primary, with continuous replication on: Settings → Server →
   Standby server → **Add a standby server** gives a connection code. It holds the backup
   place's access keys and the backups' own key; give it only to the
   standby's administrator.
   The place must show each server's changes to the other at once: S3, or a
   drive both servers mount. A cloud sync folder (Google Drive, iCloud Drive,
   Dropbox, OneDrive) shows them minutes late and is refused; joining also
   checks that a mark the standby writes reaches the primary and its answer
   comes back within 12 seconds.
2. On a fresh server of the same version or newer: Settings → Server →
   Standby server → **Become a standby server**, paste the code. The standby follows the
   replica (the database through Litestream's following restore, the files
   through the replica's list) into `backups/.leafen-standby/` and shows how
   far it has followed. Its own data is replaced when it takes over; a band
   says so to everyone logged in there.

The two never compare clocks. The primary raises a beat in
`Leafen backups/replica/lease.json` every 2 seconds, and the standby its own
in `standby.json`:

- If the primary's beat stands still for 30 seconds by the standby's clock,
  the standby takes over (a switch turns this off). It names itself the
  replica's owner, applies the last changes and starts again as the primary
  with the followed data, as a restore does. It then replicates to the same
  place. Administrators get a mail.
- If the primary can't raise its beat for 10 seconds, it stops taking changes
  until it can (everyone sees a band), so a primary cut off from the place
  never goes on writing while the standby takes over. The changes it took in
  those 10 seconds couldn't reach the replica and are lost if the standby takes
  over; with no standby, a server keeps working when its place is unreachable.
- The two also ask each other directly, at the address each has (the
  primary's in the code, the standby's in its beat), signed with a secret only
  the pair holds. A primary that can't reach the place but hears its standby
  goes on taking changes, and administrators hear that the replica is behind
  (if the primary then fails too, what it took meanwhile isn't in the replica);
  a standby takes over only when it reads the place, the primary's beat stood
  still, and the primary doesn't answer directly or answers that it takes no
  changes. A standby that can't read the place never takes over. Two servers
  that can't reach each other's address (on different private networks, or
  without a public address) judge by the place alone; Standby server shows
  which, under **Direct check**.
- Times are measured by both the steady clock and the wall clock, whichever
  says longer: a primary waking from a paused virtual machine or a sleeping
  computer takes no change until it has read the replica's owner again and
  raised its beat, and its replica sends nothing to a place another server
  took over.
- The old primary, back, finds another owner and takes no more changes (see
  above). Its Tailscale address moves to the server that took over, which
  follows the same Tailscale device, identity and certificates in the
  replica: the address keeps answering, and the Leafen apps see the same
  server. Two copies mustn't run the device at once — Tailscale marks a
  device whose state runs on two machines "Duplicate node key" — so a server
  whose data moved runs no remote access at all, and a paired primary that
  can't raise its beat for about 25 seconds lets go of its Tailscale device,
  Cloudflare tunnel and own certificate (before the standby may take over at
  30) and takes them back when its beat goes out again.

**Take over now** on the standby switches while the primary still answers:
the primary stops taking changes, sends the last ones, and says how far the
replica goes; the standby follows that far before it switches, so nothing is
lost. This is how to update a pair with only a moment's pause:

1. Update the standby to the new version.
2. **Take over now**: the standby starts as the primary and upgrades the data
   as it starts.
3. Update the old primary and, in its **Become a standby server**, paste the
   new primary's code: it follows the new primary, still taking no changes,
   and can take over again the same way. (Restoring the old primary from the
   replica takes it over as well: the old primary takes changes again and the
   other server stops.)

A standby older than its primary can't pair or take over: an older version
can't open the newer data.

The person's agent can set a pair up with them: an agent connected with an
admin key reads how backups, the replica and the pair stand with
`leafen_standby` — never a key, a
password or a code, which the person enters in these screens — rehearses a
takeover and the way back if asked, and leaves a page on what happens in an
accident.

Measured on a development machine with a local folder: the standby took over
39–41 seconds after the primary was killed, losing the changes of its last
second; a switch with **Take over now** took 4–5 seconds and lost nothing; a
primary cut off from the place stopped taking changes after 9 seconds. With
the built-in Tailscale (against a stand-in control server), the standby
answered at the same address 40 seconds after the primary stopped; a primary
cut off from the place let its address go after 24 seconds and took it back 5
seconds after the place returned; a primary woken after the takeover (a machine
asleep past the 30 seconds) closed its address within half a second.

With your own domain, the certificate Leafen serves moves with the data too,
but the name has to lead to the server that answers: give the pair's users
one address in front of both — a load balancer that checks both, or a DNS
record with a short lifetime that you move to the standby. A quick tunnel's
address belongs to the server that opened it, so after a takeover it changes.

Without one address in front, the Leafen apps follow the pair themselves when
each server has its own address (`LEAFEN_PUBLIC_URL`). Each server's settings
name the other — a primary its standby's address, a standby its primary's —
and the desktop and mobile apps remember it with the server. When the server
stops answering, or says it moved to that address, an app asks the other one,
and once that one serves as the primary (it took over) the app moves there,
keeping the login, what it kept offline and its unsent changes, which it
sends as after any takeover. A browser page shows the way instead: a band says
the standby took over, with a link to the same page there, where the person
logs in again if they haven't on that address. Neither moves to a server that
is still a standby, so a server that only this device can't reach keeps its
users.
A mail sent in the last second before a failure may go again from the new
primary.

## Restore

The easiest way is to leave it to your agent. On a new server's first
screen, "Used Leafen before? Restore it from a backup" opens the restore
there, beside "Leave it to an agent": the screen gives a sentence to paste
to an agent, with a restore code that does that one restore on a server with
no administrator, nothing else, and ends when the restore is done or after
30 minutes. The code is the screen's — one only a person who may set the
server up gets — and travels only in request bodies and tool arguments,
never in an address. An agent elsewhere (a chat app on the web) reaches the
server through a way in opened for the restore alone — the built-in tunnel's
one-time link, or an ephemeral Tailscale device — that answers nothing but
the restore and closes when it ends; the server's own outside address comes
back from the backup. The backups' password or recovery code and the
storage's keys are typed on the screen, not given to the agent. The same
restore by hand is on that screen too: where the backups are (a folder,
WebDAV or S3), which one, and its password or recovery code; the browser
then logs in as the restored server's owner.

On a server in use, ask your agent too: connected as an administrator, it
restores a scheduled backup once you say yes, over its MCP connection
(`leafen_call`, [MCP.md](../MCP.md#every-native-operation-through-mcp)) or
with its key on the API — it lists the snapshots
(`GET /api/leafen/v1/admin/backups/schedule/snapshots`) and asks for one
(`POST /api/leafen/v1/admin/backups/schedule/restore` with
`{"snapshot": "<name>"}`). Settings → Server → Restore and move does the
same by hand (below). When the server can't start at all, the command below
is the only way. The settings give it filled in for where the server runs,
under **Restoring from this file** and **Restoring without this screen**; a
phone app's own server, which has no terminal, says instead to restore a
backup on the screen, the file on a computer with Leafen, or to move the whole
server with **Move this server**.

Restore only into a fresh data root, using the SHA-256 from the backup receipt:

```sh
./dist/leafen --dir=/srv/leafen-restored restore \
  --input=/srv/leafen-backups/leafen-20260826-120000.zip \
  --sha256=<64-character-receipt>
```

Start the restored server only after the command reports `status: "restored"`.
With scheduled backups on in the restored data, its next backup comes one
interval after that first start, not at once, as after a restore in Settings,
and no failed backups are counted against it: those the restored data held
were the runs before its backup, so the band and mails about failing backups
or replication wait for the restored server's own runs.
Stopping a server makes no backup. Restore refuses a populated target, and a refused restore leaves the target as
it found it: a folder it had to make goes again, and a data folder keeps its
own files.

A folder snapshot restores the same way, naming the snapshot's folder; its
SHA-256 is that of its `snapshot.json`, shown with the last backup in the
settings, and may be left out:

```sh
./dist/leafen --dir=/srv/leafen-restored restore \
  --input="/srv/leafen-backups/Leafen backups/snapshots/20261004T010000Z" \
  --sha256=<64-character-receipt>
```

Every file is checked against the SHA-256 the snapshot lists before the data
directory is replaced. A sealed snapshot opens with its recovery code, from
`LEAFEN_BACKUP_RECOVERY_CODE` or the first line of a file given with
`--recovery-code-file`, or with its password, from `LEAFEN_BACKUP_PASSWORD` or
`--password-file`; given neither, the restore stops and says so. To restore a backup kept on a WebDAV server or in S3
storage with this command, download its whole `Leafen backups` folder first
and name the snapshot's folder inside it.

In a container, the image restores: its program is `leafen` already, so the
command after the service or image name starts with `restore`. To try a
backup — or a new release on it, as [Upgrades](#upgrades) asks — restore it
into a new volume, mounted at `/data` in place of the live one, and start an
image on that volume at another port:

```sh
docker compose run --rm -v leafen-restored:/data leafen \
  restore --input=/backups/leafen-20260826-120000.zip --sha256=<64-character-receipt>
docker run --rm --name leafen-restored -p 127.0.0.1:8091:8090 \
  -e LEAFEN_PUBLISHED_PORT=8091 -v leafen-restored:/data \
  ghcr.io/leafen-org/leafen:<version>
```

`LEAFEN_PUBLISHED_PORT` tells the copy the port it is published on, so the
links it prints open the copy, not the live server: from another terminal,
`docker exec leafen-restored leafen account device-code --name "<your name>"`
logs a browser in to it at `http://127.0.0.1:8091`. Stop it with Ctrl-C and
remove the copy with `docker volume rm leafen-restored`.
To replace a broken live data volume, stop the server (`docker compose down`,
which keeps the volumes), remove that volume (`docker volume ls` names it
`<folder>_leafen-data`) and restore into the new empty one Compose makes, then
start it:

```sh
docker compose run --rm leafen \
  restore --input=/backups/leafen-20260826-120000.zip --sha256=<64-character-receipt>
docker compose up -d
```

An administrator can also restore a scheduled backup, from a folder, a
WebDAV server or S3 storage, in Settings → Server → Restore and move. The running server
puts the snapshot together beside its data, checking every file, and on its
next start moves the current data aside and the snapshot's in. A server on
Linux or macOS starts itself again; the desktop app's own server and a server
on Windows restore when they are next started. The data from before is kept
for 7 days, or until the next scheduled backup succeeds, and the restore can
be undone from the same place until then.

When the desktop or phone app's own server can't start — its database held by
another program, damaged, or its disk full — or is slow to (15 seconds), the
app's window says why and offers what works without the server: trying again,
copying the details, saving the log and opening the data folder. Where the
database won't open, it offers the last scheduled backup too: the server notes
each one beside its data, so the window restores it as Settings would, the data
from before kept aside the same way, with the backup's recovery code or
password when the data folder doesn't open it.

Any restore, from Settings or with the command, and undoing one, gives the
server's data a new epoch. A device that held copies from before (an open
tab, the desktop or mobile app, edits made offline) names the old epoch, so
the server refuses its writes and live edits instead of letting them carry
the replaced history back in. The device says the server was restored, counts
the changes it never sent, and lets its copies go and takes the restored
content once its person agrees; until then the copies stay on the device.

## Moving to another server

A whole server moves to another Leafen server that no one else uses yet — a
container or hosting after starting on a desktop — with its people,
pages, page history, files, apps, settings and remote address:

1. On the new server, as its administrator: Settings → Server → Restore and
   move → **Move a server here**. It gives a code for an hour, carrying the
   address the new server was opened at and a secret. A server that other
   people use refuses; move a workspace instead.
2. On the old server, in the same place under **Move to another server**:
   paste the code into **Code from the new server**, press **Move this
   server** and confirm.

The old server checks that the new one answers and is of the same version or
newer, then stops taking changes (live edits too) and lets go of its
Tailscale device, and writes a snapshot — the scheduled backups' own, sealed
with the code's secret — straight into the new server over WebDAV at
`/api/leafen/v1/server-move/in/<id>/dav/`, which only the code's secret opens.
The new server checks every file against the snapshot's list and restores it
as a restore from Settings does: a server on Linux or macOS starts again by
itself, the desktop app's and a Windows server when next started, and what the
new server held before is kept aside for 7 days. It then shows what it counted
— people, workspaces, pages, files and edits in page history — beside what the
old server counted. The built-in Tailscale's address answers from the new
server, which runs the same device; apps that opened the old server by
another address switch from the band the old server shows. If the move stops
before the new server has the snapshot, the old server takes changes again.

The old server stays, reading only, pointing to the new one, until **Use this
server on its own** under Continuous replication takes it back; deleting it is
yours to do. Check the new server's backup place afterwards: a folder place
names a path on the old computer. A server that replicated continuously
hands the new server its replica mark, sealed like the snapshot, and the new
server goes on replicating to the same place as that server.
An admin agent follows a move with `leafen_server_move`; the code is always
pasted by the person, never handed to the agent.

### Moving one workspace

A workspace moves on its own to a Leafen server that people already use, into
an empty workspace there:

1. On the new server, the owner of a new, empty workspace: its Settings →
   General → Move to another server → Move a workspace here → **Create a
   code**. The code lasts an hour and opens that workspace only.
2. On the old server, the workspace's owner: **Move this workspace**, paste
   the code, **Check the code**. Each person in the workspace is offered the
   account on the new server with the same email; pair the rest or leave them
   unpaired, then **Move the workspace**.

The old server checks that the new one answers and is of the same version or
newer, then makes a [bundle](bundle.md) of the workspace with its files and
sends it, then the edit history of the pages in it, then the people and their
pairs, to `/api/leafen/v1/workspace-move/in/<id>/`, which only the code's
secret opens. The new server checks the bundle's SHA-256 and imports it as
Settings → Import does, except that pages, databases, files and comments keep
the IDs they had: mentions, person values, comments and who made each page
follow the pairs. A server that already has the workspace's pages
(it was moved or imported there before) refuses it before writing anything.
Once the pages are in, their history goes in place of what the import wrote:
the same versions, at the same times, by the same people — paired, or by name
— each version still opens and restores, and edits on the new server carry on
from the last one. Files only the history names — a picture deleted since, a
file replaced — come after it with the IDs they had, so an old version shows
them and restoring it brings them back. Paired people join the workspace as members. Someone not
paired comes in by name and email, to pair under Settings → People → Imported
people once they have an account there. Both sides then show what they counted — pages, files and
comments.

From the start of the move the workspace takes no changes on the old server,
live edits included: open pages turn reading only and say why, and a band over
the workspace points to the new server once it has moved. Pages only other
members can read stay there with it; deleting it is the owner's to do. If the move stops, the workspace takes changes again. If
the new server's import fails or is cancelled, what the move made there goes
too, and the same empty workspace takes the next code. An
owner's agent follows a move with `leafen_workspace_move`; the code is always
pasted by the person.

## Upgrades

Leafen applies ordered, replay-safe schema migrations when a newer binary opens
an existing supported data root. Before replacing a running build:

1. create a Leafen backup and retain its SHA-256 receipt;
2. restore it into a fresh test root;
3. start the new build against that restored copy and verify the workspace;
4. stop the old process cleanly before starting the replacement on live data;
5. check the line it prints as it starts,
   `leafen: build <release>+…, data schema <n>`: the new release, and on the start that moved the data on,
   `brought up from schema <m> as it started` with where the data from
   before is kept.

While a start brings the data up to date — a new release checking the schema,
keeping the data from before, applying its migrations — a server started with
`leafen serve` on plain HTTP answers on its address at once: its health check
answers `503` with `{"status":"migrating","step":"…","fromSchema":<m>,"toSchema":<n>}`
(the step is `checking`, `keeping`, `migrating` or `starting`), a browser gets
a page saying Leafen is updating, which opens Leafen once it is done, and the
API answers `503` for a moment. A script waiting for the new release takes
`migrating` for progress; once the update is done the health check answers
`200` as always. An update that fails answers `{"status":"failed","code":"…"}`
with the way back — `previousRelease`, the release the data ran before;
`dataChanged`, whether the update had changed the data; `dataBefore`, the
folder it kept; and `wayBack`, the steps in a sentence — and the page says the
same. The server doesn't stop: it goes on saying so until it is stopped, and
its log gives the error in full. Behind HTTPS, or on its own domains, the
address stays silent until the server serves, and the log alone says so.

A failed update isn't tried again by the same build on the same data: started
again — by Docker's `restart: unless-stopped` or a service manager — the
server says at once that it failed there before and waits, rather than running
the same migrations into the same failure. Another build tries it again — a
newer release, or the same one built again — and so does this one once another
release has served the data since (you went back, then updated again) or once
`backups/migrations/leafen_update_failed.json` in the data folder is deleted.
The desktop and phone apps and the server's other commands try it again each
time they start.

The data from before is kept as a data folder of its own in the data folder,
`backups/migrations/leafen_pre_migration_v<m>_to_v<n>/`, with its receipt
`leafen_pre_migration_v<m>_to_v<n>.json` beside it: the database copied as it
was, and the attachments as clones or hard links of the same files on the same
disk (copies where the disk has neither), so keeping it takes about as long as
copying the database and little more room. To go back to it, stop the server
and copy that folder whole as a new data folder — in Docker, into a new
volume — and start the earlier release on it; its first start takes a new
data epoch, as a restore's does. Copy it rather than starting a server on it
in place, so it stays as it was:

```sh
docker run --rm -v leafen-data:/from:ro -v leafen-before:/data alpine \
  cp -a /from/backups/migrations/leafen_pre_migration_v835_to_v847/. /data/
```

An update that failed before it changed the data (`dataChanged` is false)
left the data as it was: the earlier release starts on it as it is, with no
copy.

An update that failed and is tried again for the same versions keeps the
data again beside the earlier folder (`…_v<m>_to_v<n>-2/`, `-3/` …) when it
changed meanwhile — the earlier release was used — so going back always gives
the data as it was right before the latest try; unchanged, the earlier folder
stays the way back.

Only the newest way back is kept: five minutes after the server starts
serving on the new schema, the folders (and the zips of earlier releases) of
updates the data has gone past go; one left by an update that failed stays
until a later update has started.

Follow the release notes for the supported source and target versions. Never
downgrade a live data root unless those notes explicitly provide a downgrade
procedure. A data root, backup, folder snapshot or replica made by a Leafen
from before its schema baseline — whose schema history runs migration by
migration from version 1 — is refused and left unchanged: start the new release
with a new data root and bring the pages over by moving or exporting each
workspace from the earlier one. Leafen does not open data from Hanji, the product it was before
(including Hanji's earlier EdgeBase-based installations): a Hanji data
directory, backup or archive is refused and left unchanged, with no in-place
conversion; use Notion import to bring content into Leafen.

### The desktop app updates itself

The desktop app (macOS, Windows) checks Leafen's signed
desktop update list ([release lists](release-lists.md)) when it starts and
every six hours. On macOS it fetches a newer version in the background and
checks it:

- the file's size and SHA-256 against the list;
- macOS's own checks of who made it: the code signature of Leafen's team and
  Apple's notarization.

A file that fails any check is thrown away. A checked version waits until the
app next starts, or until the person presses **Restart now** on the band at
the top of the window. A security release says so more plainly.

The switch keeps the data safe:

1. The app ends as it does when closed, so its server stops cleanly.
2. A helper copies the database (`data.db` with its `-wal` and `-shm` files)
   and keeps the old app beside it, under `updates/rollback/<old version>/`
   in the app's data folder.
3. The helper puts the new app in place and starts it.
4. If the new version's server doesn't answer within two minutes — or, while
   it says it is bringing the data up to date, within half an hour — the
   helper puts the old app and the database copy back and starts the old app. The
   band then says the update was rolled back, and that version is skipped.

Only the copy from the last switch is kept.

Turn it off in Settings → Server → Updates (**Update the Leafen app
automatically**). Turning it off also drops a version already fetched. For a
whole deployment, a company sets `LEAFEN_DESKTOP_AUTO_UPDATE=off` in the app's
environment, and the switch then only shows that.

Windows updates only through the Microsoft Store, which updates its app.
Leafen has no Windows code signing certificate, so the Windows zip installs
nothing by itself: when the list names a newer version, the band at the top
of the window says so (a security release more plainly) with **Open download
page**, which opens its release page, and **Check for Updates…** in the app
menu says the same. The person quits the app and unzips the new zip in place
of the old folder; the data stays in `%APPDATA%\Leafen`. The zip shows no
update switch; `LEAFEN_DESKTOP_AUTO_UPDATE=off` turns the notice off.

### The desktop app's first start

The app asks before anything, **How would you like to start?**: **Log in**
to a Leafen already in use — an invite link or the server's address, an
address without a scheme tried over https and then http — or **Start new on
this computer**, which makes this computer a server. A computer connected to a server is its terminal and
runs no server of its own; a phone connects by the QR code a logged-in
device shows too, or offers **Try it on this phone** instead — Leafen on the
phone for one person to try, which stops with the app and runs no apps.
Starting fresh sets the new server up in the window, with a name only: the
person's name on the device, or a friendly one where it has none (a phone's),
which they may change; the window is the account's first device and stays logged in. Then
the language, which goes to the account. A computer that started fresh — like
a browser that set a server up — goes on through a few steps, each of which
can wait: its address for other devices, how outside agents connect (a text
with a one-time code to paste into Claude Code, Codex or another agent, or
the address a Claude or ChatGPT app's custom connector takes — Leafen runs no
agents of its own). The workspace then opens on its Getting Started page, as
Notion's does; other devices are added in Settings → Account. Settings → Preferences → Install → **Finish what was
skipped at install** brings back what waited. An app that already has its own workspace
keeps it and isn't asked.

### The desktop app keeps serving behind its window

While the desktop app's computer serves — its workspace reached from other
devices (remote access on) — closing the
window leaves the app running in the menu bar (macOS) or the tray (Windows),
with a line saying what it does, **New Window** and **Quit Leafen**.
Serving nothing, closing the window quits the app as before.

- **Open at login.** While it serves, the app opens when the person logs in:
  a login item on macOS (System Settings → General → Login Items, which may
  ask to allow it) and the user's Run key on Windows, which starts it behind
  its window (`--background`).
- **Keep awake.** When the person allows it (at install, or later), the
  computer doesn't fall asleep while the app serves: a power assertion on
  macOS (`pmset -g assertions` shows "Leafen serves this workspace") and
  `SetThreadExecutionState` on Windows. The display still turns off. On battery the computer sleeps as usual unless
  the person turns on **Also on battery**: the systems' own settings for
  preventing sleep hold on a power adapter only.

The switches are in Settings → Server accounts → **This computer**, in the
desktop app.

### Other devices and people on the desktop app's server

The desktop app's own server opens only in its window and this computer's
browser until it has an outside address (Settings → Server → **Access from
outside**: the built-in Tailscale, a tunnel or the server's own address).
There it answers as any server does:

- **This computer's browser.** Settings → Account → **Open in a browser**
  opens the server in the browser here, at its address on this computer,
  logged in to the account as a new device by a one-time device code.
- **A phone or another computer.** The workspace menu's **Add a device** (also in
  Settings → Account) shows
  a QR code of the outside address with a one-time code, which the phone app
  scans, or the code to type on the other device's login screen.
- **Other people.** An invitation by name or email, or a workspace's invite
  link, lets them in with a name; sign-up stays closed to anyone else, as on a
  server set up now. Their devices, backup logins and two-step verification
  work as on any server.
- **Agents** come in at the outside address with their keys and OAuth as
  before; a web agent's request to connect is allowed in the app's window.

Turning the outside address off closes it again: requests there are refused.
A phone's own server opens no outside address; for other devices and people,
move it to an always-on server.

### Docker servers

A server in Docker never replaces itself: changing its own
container would need control of Docker from inside it. Administrators get the
update notice above. To update:

- with an outside tool that follows the image, such as Watchtower, after a
  backup;
- or by hand: back up, pull the new image and recreate the container on the
  same data volume, following the steps above; with Compose, as
  [Docker Compose](#docker-compose) shows, with the commands that show the
  image and build that run.
