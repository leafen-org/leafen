# Leafen Web

This directory contains the React interface embedded in the native Leafen Go
binary. It is not deployed separately from the Leafen server.

`src/` is the whole interface. It reaches the Leafen server through typed
persistence, auth, transport, and collaboration adapters.

## Build

From the product repository root:

```sh
npm --prefix web ci --ignore-scripts
npm --prefix web run build
go build -trimpath -buildvcs=false -o dist/leafen ./desktop
```

On Windows, the [development guide](../docs/development.md#build) gives the
same steps for Command Prompt and PowerShell.

`npm run build` verifies the generated Go/TypeScript wire contracts, runs the
TypeScript compiler, and emits `web/dist`. Go embeds that directory into the
single Leafen program (the desktop app's entry, `desktop/`).

## Development server

Run the native server on the port configured by `vite.config.ts`, then Vite:

```sh
go run ./desktop --dir=./data-dev serve --http=127.0.0.1:8787
npm --prefix web run dev
```

Open `http://127.0.0.1:3000/`. Vite proxies `/api` to the native server. The
collaboration and MCP surfaces remain owned by the native process; production
uses the embedded same-origin bundle on Leafen's single public port.

## Active ownership

- `src/components/` owns the UI surfaces.
- `src/api.ts` owns the typed browser API adapter.
- `contracts/` in the repository root is the machine-readable Go/React wire
  authority; generated files must be regenerated together.
- `src/collaboration.ts` attaches Yjs clients to Leafen's authorized Ygo rooms.

The complete application architecture and operating commands are documented in
the repository-root [README](../README.md) and [development guide](../docs/development.md).
