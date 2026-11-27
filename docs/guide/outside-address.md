# Outside address

<!-- guide-sources:
web/src/components/remoteAccess/RemoteAddressChoice.tsx
web/src/components/remoteAccess/RemoteAddressOwnWay.tsx
web/src/components/admin/AdministratorTunnel.tsx
web/src/locales/en/serverConsole.json
internal/app/server_tunnel.go
internal/app/server_tunnel_tailscale.go
internal/app/server_tunnel_company.go
internal/app/server_tunnel_cloudflared.go
internal/app/server_tunnel_door.go
internal/app/server_tailscale_command.go
internal/app/mcp_tool_remote_access.go
internal/app/connector_reach.go
-->

This chapter is for a server's administrators. It shows how to give Leafen an
address that phones, the people you work with and AI apps on the internet can
open: through Tailscale, your own domain, or a temporary link. What the
server does when the address changes, reverse proxies, the network proxy and
the setup for a closed network are in
[Self-hosting: Remote access for agents](../self-hosting.md#remote-access-for-agents),
[Reverse proxy](../self-hosting.md#reverse-proxy) and
[Network proxy](../self-hosting.md#network-proxy).

## What you start with

Nothing opens to the internet by itself. A new server answers only on this
computer, or on your network at the address it listens on: a desktop app at
`127.0.0.1`, a Docker server at the port it is published on. Agents on the
same computer connect there, and the server's own pages work as usual.

While a server has only that address, places that make a link or a QR code
for another device, such as **Add a device**, invitations and a page's share
link, say that only this computer can open it, with a way to turn on outside
access. Once there is an outside address, they use it.

## Choose an address

Leafen asks once, in the **Open Leafen from anywhere?** window, when something
first needs an outside address: the first run's address step (on a server set
up with `leafen setup`, the first time a browser logs in), choosing to start
with an agent at the first run, or **Turn on a public address** in Settings →
Features → **Agents** → **The text and other ways**. Only administrators are
asked. The window offers:

- **Make an address with Tailscale** (**Recommended**): a fixed public https
  address from your own free Tailscale account. Nothing to install.
- Under **Other ways**:
  - **Use my own domain** (**Recommended for company & home servers**): an
    address you already have, a Cloudflare tunnel of your own, or port
    forwarding.
  - **Try it for a while, no account**: a Cloudflare quick tunnel, whose
    address changes each time it starts.
- **Later**: nothing changes, and the question comes back the next time
  something needs an address.

A company's server is offered **Use a company address** first instead: when
the administrator's email is on a company's domain rather than a common free
mail service, or an organization on the server has verified a domain. When
Tailscale can't be reached from the server, the window says why and offers
**Ask an agent**, which copies a prompt with what the server measured about
its network, or **Do it yourself**, which opens the guide in
[Self-hosting: Do it yourself](../self-hosting.md#do-it-yourself).

When an address works, the window says **Leafen has an internet address**,
with a QR code: scan it with your phone to sign the phone in there.

The same choice is in Settings → Server → **Access from outside** → **How it
connects**, where you can change it or turn it **Off** at any time.

### Tailscale

1. Choose **Make an address with Tailscale**, or choose **Tailscale (free
   public address)** in **How it connects** and press **Apply**.
2. Log in to Tailscale in any browser: **Open Tailscale** opens its login
   page, and **Log in on a phone** shows it as a QR code. From **How it
   connects**, Leafen opens the page in a new tab itself, and **Open again**
   opens it again. Create a Tailscale account there if you have none; until
   you log in, the screen waits at this step. Leafen notices when you are
   done.
3. Press **Open Tailscale** and allow Leafen's address there (Tailscale calls
   this HTTPS and Funnel). **Allow on a phone** shows it as a QR code. The
   screen moves on by itself.
4. Tailscale puts the address on the internet. The first time can take a few
   minutes.

The address looks like `https://<name>.<tailnet>.ts.net`. The first part is the
first workspace's name in plain letters, or `leafen`; the part after it is
your Tailscale network's name, which you can change in Tailscale's DNS
settings before you share links. Connections pass through Tailscale but are
decrypted on your server. **Log out of Tailscale** in **Access from outside**
ends it.

Tailscale's free plan is for personal, non-commercial use; a company needs its
own paid Tailscale plan. On a server without a screen, `leafen tailscale`
prints where to log in and allow the address, and waits.

### Your own domain

Use this when your own reverse proxy or a port forward already serves an https
address for this server.

1. Choose **Use my own domain** (in the window or in **How it connects**),
   type the https address, such as `https://wiki.example.com`, and press
   **Apply**. The window then says **This address is saved**.
2. The server checks the address: that its name resolves, which certificate
   answers there, until when, and whether this computer trusts it. The result
   reads **Checked: …**, for example that the name doesn't resolve yet or
   that nothing answers HTTPS there yet.
3. **This server's address: …** in **Access from outside** shows the address
   in use. Open it on a phone to try it.

For the server to take HTTPS itself with your certificate, run
`leafen address --address https://wiki.example.com --cert chain.pem --key key.pem --port 443`.

An address on another port, such as `https://wiki.example.com:8443`, works for
browsers, Leafen's apps and agents such as Claude Code, but not for Claude's
custom connector, which connects only on port 443. Settings → Features →
**Agents** says so beside the connector's address. For a connector, use
Tailscale, your own Cloudflare tunnel, or an address opened on port 443.
How to set up a proxy, port forwarding or a closed network is in
[Self-hosting](../self-hosting.md#do-it-yourself).

### A temporary link

**Try it for a while, no account** opens a Cloudflare quick tunnel in seconds,
with no account. Its address changes each time the tunnel starts, so agents
connected through it must connect again and browsers log in again at the new
address. It takes up to 200 requests at once, with no guarantee, and it is
decrypted at Cloudflare. Use it to try Leafen from a phone, not for daily use.

### Your own Cloudflare tunnel

**Keep using — your own Cloudflare tunnel** in **How it connects** takes the
**Tunnel token** of a tunnel you made in your Cloudflare account and the
**Address** its route serves. It reconnects by itself when the server starts.
In Cloudflare, set the tunnel's service to the address the screen shows: the
server's tunnel port, the port after its own (`LEAFEN_TUNNEL_DOOR_PORT` sets
another). Only requests through that port carry each person's own IP address;
the screen warns while the tunnel still reaches the server's main port.

## When the address changes

The chosen address is used everywhere: page, share and site links,
invitations and mails, agent connection texts, device codes and the address
to register with a login provider. When it changes to another, the old one
forwards to the new one for 90 days, and **Access from outside** shows until
when and what still uses it. A quick tunnel's address is not forwarded.
Everything else that happens then is in
[Self-hosting: Changing the address](../self-hosting.md#changing-the-address).

A server started with `LEAFEN_PUBLIC_URL` uses that address and no other:
**Access from outside** says **This server uses the address from its start
settings**, and offers only off or your own Cloudflare tunnel serving it.

## The desktop and phone apps

The desktop app's own server is reachable only while the computer is on and
awake. While it serves other devices, closing its window keeps it running in
the menu bar or tray, as [Desktop and phone apps](desktop-and-phone-apps.md)
shows. A phone's own Leafen stops when the app closes, so it opens no outside
address; to reach it from outside, move it to a computer or server that stays
on.

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| How it connects | Off | Off, Tailscale, your own domain, a quick tunnel or your own Cloudflare tunnel. |
| Tailscale address | `<name>.<tailnet>.ts.net` on port 443 | Free plan for personal, non-commercial use only. |
| Quick tunnel | Up to 200 requests at once | New address each start; not forwarded when it changes. |
| Old address after a change | Forwards for 90 days | And 30 days past its last request, up to a year. |
| Tunnel port | The port after the server's own | `LEAFEN_TUNNEL_DOOR_PORT` sets it. |
| `LEAFEN_PUBLIC_URL` | Not set | When set, it is the only address. |

## For agents and the API

An administrator's agent connection (not an MCP token) uses
`leafen_remote_access`: `status` reads how remote access stands; `tailscale`
starts the Tailscale address and answers the pages for the person to log in
and allow it; `set_address` makes
an https address the person and the agent set up the server's own, with a
certificate and key when the server takes HTTPS itself. The person logs in
to Tailscale and pays for anything themselves; the agent asks before each
change to the network, security or money. When the server's address carries
a port other than 443, `status` and `set_address` add that a Claude connector
can't connect there while everything else does, for the agent to tell the
person.

## Differences from Notion

- Notion is a website you open at Notion's address. Leafen runs on your own
  computer or server, and it opens to the internet only when an administrator
  chooses how.

## Related chapters

- [Agents](agents.md)
- [Accounts and devices](accounts-and-devices.md)
- [Desktop and phone apps](desktop-and-phone-apps.md)
- [Self-hosting: Remote access for agents](../self-hosting.md#remote-access-for-agents)
