# Meeting notes connector

An example program that runs outside Leafen and uses the native API: when a
page is created, or renamed, with a title that starts with "Meeting" while it
is still empty, it fills the page with an agenda, notes and action items.

It shows each piece a connector needs — see [the developer guide](../../docs/developers.md):

- a user key or a registered app's token (`authorize`, OAuth with PKCE),
- a signed event webhook subscription (`subscribe`) and its signature check,
- reading a page and its blocks, and writing blocks with an idempotent
  content-graph mutation (`serve`).

Node.js 20 or later; no dependencies.

## Run it with a key

1. In Leafen, open the workspace's Settings → **Agents** → **MCP address and
   keys** → **Create key**, with **Change structure** under **What this key
   may do** (subscribing needs it; filling pages needs only **Edit content**).
   Copy the key under **Key for the Leafen API** (`lmc_…`).
2. Make `serve` reachable from the Leafen server — a public `https` address in
   front of it. On a development server, allow one loopback receiver instead:
   start Leafen with `LEAFEN_WEBHOOK_ALLOWED_LOOPBACK=127.0.0.1:8787`.
3. Subscribe, then serve with the secret it prints once:

```sh
export LEAFEN_URL=https://leafen.example.com LEAFEN_TOKEN=lmc_…
LEAFEN_WORKSPACE_ID=<workspace id> CONNECTOR_PUBLIC_URL=https://hooks.example.com/ \
  node connector.mjs subscribe
CONNECTOR_WEBHOOK_SECRET=whsec_… node connector.mjs serve
```

4. Create a page called "Meeting with design". Its version history shows the
   change by the key's connection name, and "Undo 〈connection〉 changes" takes
   it back.

## Run it as a registered app

1. Settings → **Developer** → **Open developer tools** → **Connections** →
   **New connection**: name it "Meeting notes", choose **OAuth** and give the
   redirect URI `http://127.0.0.1:8787/callback`. Its page shows the
   **Client ID** (`lapp_…`).
2. `LEAFEN_URL=… LEAFEN_APP_ID=lapp_… node connector.mjs authorize`, open the
   address it prints, choose the workspace and allow. The token is saved to
   `.connector-token.json` (keep it out of version control) and used instead
   of `LEAFEN_TOKEN`. Ask for `LEAFEN_SCOPE=structure` to subscribe with it.

Registered as **its own account**, the app is installed by a workspace owner
instead, as a guest that sees only the pages connected to it (a page's "…" →
Connections) or as a member; its changes are its own, not the installer's.

## Settings

| Variable | Used by | Meaning |
| --- | --- | --- |
| `LEAFEN_URL` | all | The Leafen server, such as `https://leafen.example.com` |
| `LEAFEN_TOKEN` | subscribe, serve | A key (`lmc_…`) or app token; otherwise `.connector-token.json` |
| `LEAFEN_APP_ID`, `LEAFEN_SCOPE` | authorize | The registered app's id and the scope it asks for (`content`) |
| `LEAFEN_WORKSPACE_ID`, `CONNECTOR_PUBLIC_URL` | subscribe | The workspace to watch and where `serve` is reachable |
| `CONNECTOR_WEBHOOK_SECRET` | serve | The subscription's secret (`whsec_…`) |
| `CONNECTOR_PORT` | authorize, serve | Local port, 8787 by default |
| `CONNECTOR_TOKEN_FILE` | all | Where `authorize` saves the token |
