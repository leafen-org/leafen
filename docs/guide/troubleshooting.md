# Troubleshooting

<!-- guide-sources:
web/src/locales/en/errorBoundary.json
web/src/locales/en/syncStatusBadge.json
web/src/locales/en/authGate.json
web/src/locales/en/serverConsole.json
web/src/locales/en/importDialog.json
web/src/locales/en/workspaceSettingsDialog.json
internal/app/password_backoff.go
-->

This chapter lists the problems people run into most and what to do about
each, with the exact message Leafen shows. It is for everyone; the server
steps are for administrators. To report a problem that isn't here, see
[Reporting a bug](../bug-reports.md).

## Find the cause first

- **The error reference.** An error screen says **Something went wrong.** with
  **Error reference: LF-…**, and failed requests carry the same `LF-…` code.
  Keep it: an administrator finds the request with it in the server's request
  log (Settings → Server → **Audit log** → **Request log**), and a bug report
  asks for it. **Report issue** on that screen opens the bug form with the
  version filled in.
- **Is the server up?** `curl --fail http://127.0.0.1:8090/api/leafen/v1/health`
  on the server's computer answers `status: "ok"` when it is (use your
  server's own address and port).
- **What runs there.** `leafen doctor` reads the data folder and prints its
  database, the memory plan and any settings Leafen doesn't use. `leafen_server_version` tells a
  connected agent the release, its build and the data's schema.

## Logging in

| What you see | What to do |
| --- | --- |
| You have no device that is logged in. | On the code screen, **No device logged in?**: ask an administrator for a one-time code, made with **Device code** on your row in Settings → Server → **Users**. On the server's own computer, `leafen account device-code --name "<your name>"` makes one. |
| **Too many wrong passwords. Try again in N minutes, or log in with a code from a logged-in device.** | Wait the minutes it says (1, then 2, 4, 8, up to 15), or log in with a code or a passkey, which never wait. |
| **This server doesn't send mail, so it can't send you a reset link.** | Ask an administrator for a temporary password; or, on the server's computer, `leafen account reset-password`. |
| **Passkeys work only on an https address.** | Open the server at its https address; see [Outside address](outside-address.md). |
| **Sign-up on this server is closed …** | Ask for an invitation; a server set up now lets in only invited people, people from allowed email domains and SSO. |
| **This account has reached its session limit** | An account is logged in on at most 25 devices. Log out the least recently used one, or log out devices you no longer use in Settings → your name → **Devices**. |
| **Your login expired or was ended.** | Log in again; you go back to where you were. Someone logged this device out, or an organization's session length ran out. |

## Other devices and the outside

| What you see | What to do |
| --- | --- |
| **Only this computer can open this address** under a QR code or link. | The server has no outside address yet. An administrator turns one on in Settings → Server → **Access from outside** ([Outside address](outside-address.md)). |
| A phone can't open the address in the QR code. | The phone must reach that address: on the same network for a local one, or through the outside address. |
| **Couldn't start it** or **The tunnel hasn't opened yet, and it keeps trying.** | The network may block Tailscale or Cloudflare. Go back and choose another way, such as your own domain. |
| Agents and links stopped working after the address changed. | The old address forwards to the new one for 90 days. Agents connected through a quick tunnel, or showing **Reconnect**, need a new connection text from Settings → **Agents**. |
| Windows Explorer won't connect to the Drive. | Windows connects only to https addresses. Turn on an outside address, or use Finder or another WebDAV program ([File apps](file-apps.md)). |

## Agents

| What you see | What to do |
| --- | --- |
| **This address (…) isn't encrypted, so agents can't connect to it.** | Turn on a public address, or, for an agent on the same computer, open Leafen at its localhost address and connect there. |
| **You've reached the agent connection limit** | Each person has up to 32 agent connections. Disconnect ones you no longer use under **Connected agents**. |
| **The organization's key policy doesn't allow this key.** | Choose a sooner expiry, or ask an organization owner. |
| **An admin hasn't approved this connection in …** | The organization allows only approved MCP clients; ask an admin to approve it in Settings → **Identity**. |
| The agent can't see a page. | It works as you, in the workspaces you chose, and only in pages that let agents in; by default only people with full access let them in ([Agents](agents.md)). |

## Backups and restores

| What you see | What to do |
| --- | --- |
| **The backup folder must be a full path outside the data folder …** | Give an existing folder's full path that the server can write to, outside its data folder. |
| **Couldn't write to the backup folder.** or **The backup folder is out of space.** | Check that the folder exists, is mounted, has room and that the server may write to it. |
| **The WebDAV server refused access to the backups.** or **The S3 storage refused access to the backups.** | The line beside it names the common cause: a wrong user name and password (or keys), the storage asking to confirm this new device or for an app password, or no permission for that folder or bucket. Put that right, enter the password or keys again and save. |
| **Backups that leave this computer … need a recovery code or a password first.** | Make a recovery code or choose a password under **Backup protection** ([Backups](backups.md)). |
| **This place holds another server's backups …** | Restore from them, or choose another place for this server's backups. |
| **The server's data changed** on an open device. | The server was restored. **Reload from server** loads it; **Later** keeps this device's unsent changes so you can copy them first. |
| A data folder or backup from an older Leafen is refused at start. | Leafen opens only data from its current schema baseline. Start with a new data folder and move each workspace from the old server, as [Self-hosting: Upgrades](../self-hosting.md#upgrades) says. |

## Notion import

| What you see | What to do |
| --- | --- |
| **Notion didn't accept this token.** | Copy the token again from the connection in Notion's developer tools, or make a new one. |
| **Couldn't reach Notion.** | Check that the server reaches the internet; behind a proxy, see [Self-hosting: Network proxy](../self-hosting.md#network-proxy). |
| **Notion asked to wait: too many requests for now.** | Try again in a little while; the import goes on from where it stopped. |
| **The Notion connection can't read comments** | Tick **Read comments** under the connection's **Capabilities** in Notion and scan again. |
| Files **couldn't be brought because Notion no longer gives them.** | Don't delete the Notion originals; restore them in Notion, then **Copy files again**. |
| **The import stopped.** | **Retry** goes on from that step without reading Notion again from the start ([Moving from Notion](moving-from-notion.md)). |

## Working offline

| What you see | What to do |
| --- | --- |
| **Collaboration disconnected — local changes are kept on this device** | Keep working; changes go to the server once it answers again. |
| **Waiting to retry offline changes** | The device sends them by itself when it reconnects. |
| **Can't reach the server. This comes back by itself once it can.** | Check the connection; the server may be stopped, or the computer running the desktop app may be asleep. |

## The desktop app

If the app's own server can't start, the window says why: another program
holds its database, the database is damaged, or the disk is full. It offers to
try again, copy the details, save the log and open the data folder, and,
where the database won't open, to restore the last scheduled backup
([Desktop and phone apps](desktop-and-phone-apps.md)).

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Devices per account | 25 | |
| Password waits | 1, 2, 4, 8, up to 15 minutes | After 5 wrong passwords in 15 minutes. |
| Agent connections | 32 per person | |
| Old address forwarding | 90 days | Not for a quick tunnel. |
| Request log | 5 days | Failed and slow requests always kept. |

## For agents and the API

An agent helping someone with a problem reads the server's state with
`leafen_server_version`, `leafen_standby` (backups and the replica) and
`leafen_remote_access` (the outside address), and, where the problem concerns
them, the person's notices with `leafen_inbox`. It never asks for a password, a recovery code or a token; the
person types those into Leafen's own screens.

## Related chapters

- [Accounts and devices](accounts-and-devices.md)
- [Outside address](outside-address.md)
- [Backups](backups.md)
- [Reporting a bug](../bug-reports.md)
