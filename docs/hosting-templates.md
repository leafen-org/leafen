# One-click hosting templates

Leafen is one container with one data folder, so a hosting platform's
one-click template needs little. Official templates are listed here; others
may say "for Leafen", not "Leafen" ([TRADEMARKS.md](../TRADEMARKS.md)).

## Official templates

| Platform | Template |
| --- | --- |
| Railway | [![Deploy on Railway](https://railway.com/button.svg)](https://railway.com/deploy/leafen?referralCode=clR_3T&utm_medium=integration&utm_source=template&utm_campaign=generic) |

## What every template sets

| Item | Value |
| --- | --- |
| Image | the official Leafen image (published at release) |
| Port | `8090` (HTTP; the platform's proxy adds HTTPS) |
| Command | the image's default: `serve --http=0.0.0.0:8090 --dir=/data` |
| Data | a persistent volume at `/data` — everything Leafen keeps is there |
| Health check | `GET /api/leafen/v1/health` → 200 |
| Memory | 1536 MiB or more; set `LEAFEN_MEMORY_LIMIT` to the plan's size (for example `1536m`) |
| Public address | `LEAFEN_PUBLIC_URL` = the `https://` address the platform gives |
| Routing | WebSockets passed through (collaboration and live updates use them) |
| First administrator | `LEAFEN_SETUP_CODE` = a random code made at install (on Railway `${{secret(24, "0123456789")}}`): the platform shows it among the service's variables. The image also prints a one-time setup link in the logs (`LEAFEN_SETUP_OPEN_MODE=print`, its default) |

On Railway the template also sets `PORT=8090` and `RAILWAY_RUN_UID=0`: Railway
mounts the volume owned by root, and the image runs as a non-root user, so
without it Leafen can't write to `/data`.

Nothing else is required. Leafen's thanks in the app (the workspace menu's GitHub line, and the sponsor list and the row at
the bottom of Home that come in an early update after the first release) stay on by default; a host may set
`LEAFEN_SPONSOR_CREDIT=off`.

## The first administrator on each platform

A server on a hosting platform is opened from the internet, through the
platform's proxy, so its first setup asks for a code; a server opened from
its own network (the same computer, a home or office network, Tailscale)
asks none ([Self-hosting](self-hosting.md#first-administrator)). Opened from
outside, the setup page says where the setup link is, and "Other ways" takes
the code. Either of these works:

- the setup link in the deploy logs, `…/setup#code=…`, which opens the page
  with its code (the link names the platform's address when the template
  sets `LEAFEN_PUBLIC_URL`; otherwise open the same `/setup#code=…` on it);
- the template's `LEAFEN_SETUP_CODE`, typed under "Other ways" → "Enter setup
  code".

Where to find them:

| Platform | The code | The setup link |
| --- | --- | --- |
| Railway | the Leafen service → Variables → `LEAFEN_SETUP_CODE` | the Leafen service → Deployments → the latest deployment → Deploy Logs |
| PikaPods | the pod's settings → Env Vars → `LEAFEN_SETUP_CODE` | the pod's card → More → Show Logs |
| Elestio | the service's overview → Update config (the `.env` file) → `LEAFEN_SETUP_CODE` | the service's overview → View app logs |
| Any other | wherever the platform lists a service's environment variables | the container's or service's logs |

An agent with access to the platform (its CLI or API) can be asked to find
the link: "Find my Leafen setup link." Once the first administrator exists,
neither opens the setup again; a lost device or password is recovered as
[Self-hosting](self-hosting.md#lost-devices-and-forgotten-passwords) says,
with `leafen` commands run in the platform's shell for the service where it
has one.

## Platforms that share revenue with authors (as of 2026-10-17)

| Platform | What the author gets | How to join |
| --- | --- | --- |
| Railway | 15% of the usage cost of deployments from a published template, plus 10% for answering questions in its Template Queue (25% in all); paid in Railway credits or, by choice, cash through Stripe Connect in $100–$10,000 withdrawals ([docs](https://docs.railway.com/templates/kickbacks)) | publish the template to Railway's marketplace from a Railway account |
| PikaPods | a share of the revenue from hosting the app, at a rate PikaPods doesn't publish; not offered for every project | ask PikaPods to list the app and set up the share |
| Elestio | Elestio states 10% of revenue goes to the open-source projects it manages (an older page says 5%), for authors who partner with it | contact Elestio to partner and set up payment |

Railway's template is published. PikaPods and Elestio are not set up yet; their
accounts and payout details are the project owner's to set up.
