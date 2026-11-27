# Installing Leafen

This guide is for people and for the agents they ask to install Leafen. Both
follow the same steps. Leafen is a 0.1 pre-release: before 1.0 there is no
guarantee that your data carries over after an update, so try it with data
you can recreate.

Pick one way:

- **Desktop app** (macOS, Windows) — for one person, or a few people on one
  computer that stays on. The app is the server; phones and other devices
  join it.
- **Docker** — for a server that is always on, such as a company server or a
  hosted machine, and for Linux, which has no desktop app: Leafen runs in
  Docker there and is used in a browser.
- **From source** — to build it yourself.

Running a server for others (proxies, backups, restores, upgrades, settings)
is covered in the [self-hosting guide](self-hosting.md); this guide stops once
Leafen is running.

## For agents

Don't stop once Leafen is installed: take the person through its first run
in conversation, asking what Leafen's first screens ask, one thing at a time,
in their language, and doing each step with a command. The person never has
to open Leafen for it: everything from installing to using it can go through
your conversation. A Leafen window that is open follows each step you finish,
so a person who looks sees where things stand.

- **Ask first: start new, log in, or restore.** "Start a new Leafen on this
  computer, log in to a Leafen you use already — a company or team server,
  or another device — or bring back a Leafen you used before from its
  backups?" For logging in, ask for the invite link or the address
  and a one-time code from a device logged in there (the workspace name in
  the sidebar → Add a device; with no device, its administrator gives one);
  install the desktop app, then run
  `leafen setup sign-in <invite link or address> --code <code>`: the app
  logs in with it and opens that Leafen, and the command says whom it
  logged in as, or why the code was refused and what to ask next. Without a
  code the app's window shows that Leafen's login for the person (in a
  browser, open the address). That Leafen's administrator set it up, so
  skip to connecting yourself.
- **Restoring an old Leafen from its backups.** Install Leafen the way the
  old one ran (below), but don't run `leafen setup start`: the old server's
  name, accounts, workspaces, agent connections and outside address all come
  back from the backup. Have the person open Leafen's first screen and, under
  the name, choose "Used Leafen before? Restore it from a backup" → "Leave it
  to an agent". It asks where you are: on their computer, or elsewhere (a
  chat app on the web), in which case it first opens a way in from outside
  for the restore alone — a one-time link, or Tailscale — and closes it when
  the restore ends. They paste you a sentence with Leafen's restore MCP
  address, which takes no login, and a one-time restore code. Pass the code
  as `restore_code` to every tool: `leafen_restore_status` says what to do
  next, `leafen_restore_find` looks in the place the old Leafen backed up to
  (a folder the server reads, a WebDAV server or S3-compatible storage) and
  `leafen_restore_run` restores what the person chooses (or the same over
  JSON: `POST <address>/api/leafen/v1/setup/restore/agent/status`, `/find`,
  `/run` with `{"restoreCode": "…"}`). Never ask for the backup's password,
  its recovery code or the storage's keys: the tools don't take them — tell
  the person which field on that screen to fill, and they type it there.
  The code does this one restore on a server with no administrator and
  nothing else, and ends when the restore is done or after 30 minutes. Once
  the server starts again on the restored data, the screen signs the person
  in as the old server's owner; ask them to check that their workspaces are
  back, and connect yourself again (step 4) if your old connection doesn't
  answer. When the old data is in a backup zip or a snapshot folder and the
  server can't start at all, `leafen restore` (the
  [self-hosting guide](self-hosting.md#restore)) is the way.
- **Then follow the steps below** for the one way the person chose, or ask
  which way when it isn't clear, and go on with "The first run by commands".
- Do not bypass the operating system's checks. The Mac app is signed and
  notarized by Apple, so its first launch only asks whether to open an app
  downloaded from the internet; the Windows build is not signed yet, so its
  first launch shows a warning. Do not remove quarantine attributes, change
  security settings or disable protections; tell the person what to click
  (below) and wait for them.
- Do not create accounts for the person or enter passwords for them. Leafen
  starts with a name only once the person says yes to it in this
  conversation, in the words `leafen setup start` gives; pass on their own
  yes, never yours. Your connection, and anything Leafen asks before you do
  it, you ask them in the conversation too: never answer for them. The
  person's own hands are only for other services: logging in to Tailscale or
  Notion, and the operating system's question or warning at the first launch
  (below).
- Keep secrets out of what you share: no setup links' codes, tokens or keys
  in files or in messages to anyone but the person. A Notion token you got
  in your browser, or one they paste into this conversation, goes straight
  to Leafen (`leafen_notion_migration_connect`, step 5) — don't repeat it —
  and when the move is done, tell them to delete that connection in Notion
  or make it a new token, since the conversation keeps it. A one-time
  login link or code (`leafen account device-code`) is the person's: give
  it to them alone; one they give you for the app goes only to
  `leafen setup sign-in --code`.
  `leafen setup` prints no secret but one: while no administrator has Leafen
  open on a device, `leafen setup status` shows the administrator's one-time
  login code (its "Logged in:" line), theirs alone as above.
  `leafen connect --approved` hands your
  key to Claude Code without printing it (`--add-to claude-code`), and
  prints another program's once, for its MCP settings only.
- When a step fails, report the exact message and stop rather than trying
  workarounds that change the system.

### The first run by commands

`leafen` below is the program you installed: for the desktop app,
`/Applications/Leafen.app/Contents/MacOS/leafen-desktop` on a Mac and
`.\leafen-desktop` in its folder on Windows (without `.exe`, so the terminal
waits for it and shows what it says), which work on the app's own data; for
Docker, `docker compose exec leafen leafen`; from source,
`./dist/leafen --dir=<its data folder>` (`.\dist\leafen.exe` on Windows).
They work while Leafen runs.
`leafen setup status` says, at any time, what is done and the one thing next.

1. **Language and name.** Ask which language Leafen should use (`ko`, `en`,
   `ja`, `zh-Hans`, …) and the name others will see. Run
   `leafen setup start --name "<their name>" --language <code>`: it answers
   with the question to ask the person, in their language, and the command
   that passes their yes on. Ask it in this conversation and wait for their
   own answer. Only on a yes, run that command
   (`leafen setup start … --confirmation <id>`): Leafen is set up with that
   name as its first administrator, and their first workspace is ready. A
   Leafen window open on its first screen logs itself in and goes on; none
   needs to be. The language can change later: `leafen setup language <code>`.
2. **Logging in.** When the person wants to open Leafen — on this computer or
   on a phone — `leafen account device-code --name "<their name>"` gives a
   one-time link and code for ten minutes: give it to them, and they open it
   on that device (or choose "Log in with a code" on Leafen's login). Until
   Leafen has an address of its own (step 3), the link names this computer's
   (`127.0.0.1`), so another device opens Leafen at the address it reaches
   and enters the code there. The same adds another device later. In the
   desktop app, another Leafen (a company or team server) is added beside
   this one the same way:
   `leafen setup sign-in <address> --code <code>`, with a code from a device
   logged in there.
3. **Public address.** Explain why it matters: phones, the people they work
   with and AI chat apps on the internet (Claude, ChatGPT) reach Leafen by an
   address of its own; for this computer alone it can wait. Ask whether and
   how:
   - **Tailscale** (recommended, a free account): `leafen tailscale`. It
     prints an address to log in to Tailscale, then one to allow the public
     address. With a browser tool that works in their own browser, open them
     in a tab of your own: after one yes from them to add this server to
     their Tailscale and allow its public address, they log in there and you
     go through the rest. Without one, open them in their browser yourself
     (`open <address>` on a Mac): their one step is logging in, then the
     pages' own buttons to connect the server and allow its address, which
     you name as the pages do. `leafen tailscale` notices each step by itself
     and goes on, so don't wait to be told. It ends with the address;
     Leafen's window shows it with a QR code for their phone.
   - **Their own domain**, with their own certificate or proxy:
     `leafen address --address https://…` (see `leafen address --help`).
   - **Later**: `leafen setup skip address`.
4. **Connect yourself, asking first.** Before you run anything to connect,
   ask the person the one question Leafen asks about it: `leafen setup status`
   gives it at this step, in their language — which agent (your program's
   name in place of `<your program's name>`), as whom, what it may do in
   which workspaces. Ask it as it is and wait for their own answer. Ask
   nothing else with it: every folder, all their workspaces and everything
   they may do are Leafen's defaults, not choices to offer (`--this-folder`,
   `--workspace <name>` and `--scope read|content|structure` narrow them if
   the person asks). Only if they say yes, connect:
   - **Claude Code where Leafen's program runs** (the desktop app, from
     source): `leafen connect --name "Claude Code" --approved --add-to claude-code`.
     It connects you with no browser and adds Leafen to Claude Code's
     settings for every folder itself, so the key never shows in your output.
   - **Claude Code with Leafen in Docker**, whose program runs in the
     container, where Claude Code isn't: one line hands the key from the
     container to Claude Code without it showing,
     `claude mcp add-json --scope user leafen "$(docker compose exec -T leafen leafen connect --name "Claude Code" --approved --claude-code-json)"`
     — the same over `ssh` to the server Leafen runs on. A container started
     without `compose.yaml`'s `LEAFEN_PUBLISHED_PORT` and with no public
     address can't tell where its host publishes it: the command then says
     to add `--url <the address Leafen is opened at>`.
   - **Another program**: `leafen connect --name "<your program's name>" --approved`
     connects you and prints Leafen's MCP address and its key, once, with the
     lines to add it (Cursor: the `url` and `headers` under `mcpServers` in
     `~/.cursor/mcp.json`). Don't repeat the key in your messages.

   Outside the first run, once the person asks you to connect,
   `leafen connect --name "<your program's name>"` with nothing added gives
   the same question and changes no settings; `--add-to` and `--approved`
   come only with their yes.

   Then do what its answer says: tell the person, in the words it gives, that
   Leafen is in every folder and that one folder is just a word away, and add
   the instruction lines it prints, without asking, to the file it names
   (`~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md` or your own; a folder's own
   once Leafen is kept to one folder). Go on in the same conversation: Claude
   Code in a terminal loads a server added during a conversation once the
   person types `/reload-plugins` (you can't run it yourself), and that gives
   you no turn, so the answer's words ask them to type it and then send
   “continue”, and you go on when they do; Leafen's tools then show here.
   Never say you'll go on as soon as they type it. Claude Code without a
   terminal (the Claude app) shows them from the next conversation and
   `/reload-plugins` doesn't bring them, so there you go on through
   `leafen mcp call` (alone it lists Leafen's tools, `<tool> --schema` says
   what one takes, `<tool> '<arguments as JSON>'` calls it as you, with the
   key Claude Code holds; in Windows PowerShell, which hands a program its
   argument without the JSON's double quotes, the JSON goes on standard
   input: `$OutputEncoding = [Text.UTF8Encoding]::new($false);
   '<arguments as JSON>' | leafen mcp call <tool> -`) — or, where
   Leafen's program isn't beside you
   (Docker, ssh), by calling Leafen's MCP over HTTP with the line the answer
   gives. Don't send the person to a new conversation. The instruction lines
   hold the address only:

   ```text
   <!-- leafen:begin -->
   Leafen (<address>) is my shared workspace and memory: search and read it before answering about my work, and save lasting knowledge there. When Leafen says its address changed, update the address in these lines.
   <!-- leafen:end -->
   ```

   Codex adds for every folder, so for one folder the instruction lines alone
   keep Leafen to it; its tools show from a new Codex session on. Once you are
   connected, the window's agents step passes; to leave it,
   `leafen setup skip agents`. A chat app (Claude, ChatGPT) can't run
   commands: tell the person to add a custom connector with the public MCP
   address (in Claude, Customize → Connectors → Add → Add custom connector;
   in ChatGPT, Plugins → + → Add custom MCP server) and to log in when Leafen
   asks. Claude's connector connects only to an https address without a port
   (443): at an address on another port, tell the person that only the
   connector fails there — browsers, Leafen's apps and agents such as Claude
   Code work at it as it is — and that a connector takes Leafen's Tailscale,
   their own Cloudflare tunnel, or an address opened on port 443
   (`leafen_remote_access` with `status` says so). An agent on another computer, which can't run Leafen's program,
   connects first with the text from Leafen's Settings → Agents (**Copy
   text**) on a device the person is logged in on: its one-time code goes,
   in one command the text gives, from Leafen straight into your own
   settings — no login and no browser (see MCP.md, Connection lifecycle).
   With no such device at hand, it asks
   to connect with `POST <address>/api/leafen/v1/mcp/device-connections` and
   `{"name": "<its program's name>"}`: it gets a code for the person, who
   says to an agent already connected "Allow the Leafen connection \<code>"
   (that agent asks them once more), and polls the token address it was given
   until its key comes; Claude Code polls with `"check": true`, and once the
   person allows it, runs the line the answer gives, which hands the key to
   Claude Code the same way (see MCP.md, Connection lifecycle).
5. **Notion.** Ask whether to move their Notion workspace here. If so, read
   `leafen_notion_migration_guide` over MCP and follow it: you make the
   Notion connection — `leafen_notion_migration_sources` gives Notion's page
   and the steps there. With a browser — theirs (such as Claude in Chrome)
   or your own (such as the Claude app's, where they log in to Notion
   once) — you say what you'll open and why, and after one yes from them
   (making a connection agrees to Notion's developer terms) you do the steps
   yourself in a tab of your own: the connection they made for Leafen
   before, or a new one that only reads, with all their top pages. Then you
   hand its token to `leafen_notion_migration_connect`, which keeps it
   encrypted in Leafen — no Leafen screen for them. Without a browser, you
   open Notion's page in their browser (`open <address>` on a Mac), go
   through the steps with them one at a time, and they paste the token into
   this conversation for you to hand over the same way. Only if they'd
   rather not paste it here, it goes into Leafen's own field, which
   `leafen setup notion` opens in their window, and
   `leafen_notion_migration_sources` with `wait_seconds` answers once it is
   in. A token that went through this conversation stays in it, so
   when the move is done tell them to delete that connection in Notion or
   make it a new token. Then you start, scan and plan the migration, ask
   the person the plan's questions (which Notion people are which accounts,
   what to change) and run it, following it with
   `leafen_notion_migration_status` (`leafen setup status` shows its state
   too). The migration's tools are your MCP connection's, so connect yourself
   (step 4) first; in a conversation that can't show them yet (step 4), call
   them through `leafen mcp call`, such as
   `leafen mcp call leafen_notion_migration_guide`.

`leafen setup status` then says nothing is left of the first run.

## Desktop app

1. Open the repository's latest release (the **Releases** section of the
   repository page, then **Latest**).
2. Download the file for the computer:

   | System | File |
   | --- | --- |
   | macOS, Apple silicon (M1 or later) | `leafen-desktop-<version>-darwin-arm64.dmg` |
   | macOS, Intel | `leafen-desktop-<version>-darwin-amd64.dmg` |
   | Windows | `leafen-desktop-<version>-windows-amd64.zip` |

   On macOS, Apple silicon or Intel is shown under Apple menu › About This Mac
   (**Chip** or **Processor**). The Mac app needs macOS 13 Ventura or later.
   There is no Linux desktop app: on Linux, use [Docker](#docker) and a
   browser.
3. Open it:
   - **macOS** — open the file and drag **Leafen** onto **Applications**, then
     open Leafen from Applications. The app is signed and notarized by Apple:
     the first time, macOS asks whether to open an app downloaded from the
     internet. Click **Open**.
   - **Windows** — Leafen for Windows is to come from the Microsoft Store,
     which installs and updates it without a warning; it isn't there yet. Until
     then, unzip the folder and run `leafen-desktop.exe`. When "Windows
     protected your PC" appears, click **More info**, then **Run anyway**.
     The zip doesn't update itself: when a new version is out, the app says so
     at the top of its window, and **Open download page** opens the release.
     Quit Leafen, unzip the new zip in place of the old folder and run it
     again; your data stays in `%APPDATA%\Leafen`. Once Leafen is in the
     Microsoft Store, installing it from there opens the same data, and the
     store keeps it up to date.
4. Choose **Start new on this computer** (or **Log in** for a Leafen you use
   already) and enter your name. That is the first account, and the app opens
   your workspace.

To use it from a phone or another computer, choose the workspace name in the
sidebar › **Add a device** (also in Settings › Account) and scan its QR code
with the other device's camera or the Leafen app, or choose **Log in with a
code** on that device's login screen and type the code it shows. To reach it
from outside your network, open the server settings' **Access from outside**
and choose **Tailscale (free public address)**.

## Docker

You need Docker with Compose. On Ubuntu:
`sudo apt install docker.io docker-compose-v2`; on other systems, Docker's
own guide ([docs.docker.com/engine/install](https://docs.docker.com/engine/install/)).
On Linux, Docker's commands take `sudo` (`sudo docker compose up -d`) unless
your user is in the `docker` group (`sudo usermod -aG docker $USER`, then sign
in again) — a group that can do anything as root on that machine. The
commands below are shown without `sudo`.

The server needs no screen and no agent: its first run is a page that opens
in a browser on any computer that reaches it (below), and an agent can do the
same by commands instead ("The first run by commands" above).

1. Put Leafen's `compose.yaml` in a new folder and start the server there:

   ```sh
   mkdir leafen && cd leafen
   curl -fsSLO https://raw.githubusercontent.com/leafen-org/leafen/main/compose.yaml
   docker compose up -d
   ```

   Docker pulls Leafen's image (`ghcr.io/leafen-org/leafen`) for the
   computer's processor. The data lives in the `leafen-data` volume. In a
   copy of the repository, `docker compose up -d` in its folder does the same.
2. Read the setup address from the log:

   ```sh
   docker compose logs leafen
   ```

   It prints a `http://127.0.0.1:8090/setup#code=…` address with a one-time
   code, as the server's own computer opens it. From another computer on
   the same network (a home's or an office's, or Tailscale), open
   `http://<the server's name or IP address>:8090/setup`: from there the
   setup asks no code. Where port 8090 isn't open to that computer, forward
   it over SSH instead and open the address as printed:

   ```sh
   ssh -L 8090:127.0.0.1:8090 <you>@<server>
   ```

   From the outside internet the setup takes the code: keep it to yourself,
   as it makes whoever enters it the administrator.
3. Enter the first administrator's name.

The server listens on port 8090. Settings such as the image's tag or another
port go in a `.env` file beside `compose.yaml` (`LEAFEN_PORT=8091`, one a
line; see the [self-hosting guide](self-hosting.md#docker-compose)). For
phones, other people and AI chat apps, give it an address of its own: the
first run's address step offers Tailscale's, which needs no domain,
certificate or open port. To serve it on your own domain with HTTPS instead,
put a reverse proxy in front as the [self-hosting guide](self-hosting.md)
describes, and write that address in `.env` as
`LEAFEN_PUBLIC_URL=https://…`: the log, links and device codes then use it.
It is the server's first address, not a lock: a way chosen later under
Settings → Server → Access from outside (Tailscale, another domain) takes
over at once, with no new container, and the earlier address forwards to it
while your proxy still sends it to the server.

To build the image from the source instead of pulling it, run this in the
repository's folder; the first build takes several minutes:

```sh
docker compose -f compose.yaml -f compose.source.yaml up -d --build
```

### Updating

When **Settings → Server → Updates** says a new version is out, update in
the folder of `compose.yaml`:

```sh
docker compose pull
docker compose up -d
```

Compose makes the container again from the new image with the same
`leafen-data` and `leafen-backups` volumes and the same `.env`, so the data,
settings and connections stay. As it starts, the new version keeps the data
from before in the data's `backups/migrations/` folder and brings the data up
to date; meanwhile a browser shows that Leafen is updating, and the health
check answers `migrating`. `docker compose images leafen` then shows the image
that runs.

On a NAS the same is a few presses:

- **Synology Container Manager**, with Leafen as a project made from
  `compose.yaml`: **Project** → Leafen's project → **Stop**, then **Build**,
  which pulls the image again and starts the project. A container made from
  the `latest` image alone shows **Update available** under **Image** once
  Container Manager has seen the new one; **Update** there makes it again
  with its settings.
- **Portainer**: **Stacks** → Leafen's stack → **Editor** → **Update the
  stack** with **Re-pull image and redeploy** on; a container made alone:
  **Containers** → Leafen's container → **Recreate** with **Re-pull image**
  on.

Leafen needs no updater of its own beside it: an automatic image updater
updates without the administrators knowing, and Watchtower, the best known,
has been archived since December 2025.

If the update fails, Leafen's address says so, with the version to go back
to and how, and the new version waits rather than trying again each time it is
started; a newer version tries again. Go back as
[Upgrades](self-hosting.md#upgrades) in the self-hosting guide says: the
earlier version (`LEAFEN_IMAGE=ghcr.io/leafen-org/leafen:<version>` in `.env`)
runs on the data as it is when the update didn't change it, and otherwise on a
copy of the folder the update kept.

## From source

You need Go 1.27, Node.js 22.12 or later, and on macOS the Xcode command line
tools the desktop window needs (Windows needs no C tools).

```sh
npm --prefix web ci --ignore-scripts
npm --prefix web run build
go build -trimpath -buildvcs=false -o dist/leafen ./desktop
./dist/leafen
```

On Windows, in Command Prompt or PowerShell:

```bat
npm.cmd --prefix web ci --ignore-scripts
npm.cmd --prefix web run build
go build -trimpath -buildvcs=false -o dist\leafen.exe .\desktop
.\dist\leafen.exe
```

`npm.cmd`, because PowerShell's default execution policy refuses npm's
`npm.ps1`; `leafen.exe`, because Go adds `.exe` only to a name it picks itself.

`./dist/leafen` (`.\dist\leafen.exe` on Windows) opens the desktop app; enter
your name as above. To run the server alone, without a window — as on Linux,
which has no desktop app — build `./cmd/leafen` instead and see the [self-hosting guide](self-hosting.md#without-docker).

Build from a clone as above: `go install leafen.org/leafen/...@latest` does not
work, because the program embeds the web build, which the repository doesn't
carry, and uses the copy of a library kept in `third_party/`.
