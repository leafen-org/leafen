# Leafen app SDK

Tools for writing apps that run inside Leafen (see the app platform design in
the developer guide). Licensed under the [Apache License 2.0](LICENSE), as
Leafen itself is.

| Part | Where | What it is |
|---|---|---|
| Screen module | `dist/index.js` (source `src/index.ts`) | The channel from an app's screen to Leafen |
| Manifest types | `dist/manifest.d.ts` (source `src/manifest.ts`) | The shape of `leafen-app.json` in TypeScript |
| Manifest schema | `schema/leafen-app.schema.json` | JSON Schema of `leafen-app.json` |
| Go worker helper | `go/leafen` (module `leafen.org/leafen/sdk/app/go`) | Inputs, outputs and Leafen's host functions for a Go worker |
| Rust worker helper | `rust/leafen` (crate `leafen`, no other crates) | The same for a Rust worker, with a small JSON module |
| Example | `examples/hello` | A screen and a worker in Go (`worker/`), Rust (`worker-rust/`) or JavaScript (`worker-js/`) |
| Notes for agents | `AGENTS.md` | The short version for coding agents; `leafen app init` puts it in each new app |

The package is not published to npm yet: `leafen app init` writes
`dist/index.js` into a new app as `ui/leafen-app-sdk.js` (and, with
`--worker`, a copy of the Go helper into `worker/leafen/`); otherwise copy it
there yourself and import it from your screen's module.

## The screen

An app's screen is a page in a frame with no origin, loaded from the app's
package. It reaches Leafen only through a private channel:

A page whose first screen is a placeholder until the app has read what it
shows marks it `data-leafen-opening` (`<p data-leafen-opening>…</p>`): Leafen
keeps saying the app is opening where the frame will be, and shows the frame
once the app has drawn over the placeholder (at most ten seconds on).

```js
import { connect } from "./leafen-app-sdk.js";

const leafen = await connect();          // says hello, gets the channel
leafen.context;                          // { locale, theme, path, homeUrl, app: { slug, version } }
const me = await leafen.api("GET", "/account/profile");
const { output } = await leafen.action("greet", { name: "Kim" });
await leafen.kv({ command: "INCR", key: "visits", ttl: 3600 });
await leafen.storage.set("draft", { title: "Q3" });  // this person's, kept on the server
const draft = await leafen.storage.get("draft");
await leafen.navigate("reports/2026");   // Leafen's address bar follows
leafen.onTheme((theme) => { /* "light" | "dark" */ });
leafen.onLocation((path) => { /* back and forward within the app */ });
leafen.link("reports/2026");             // the full address, for a link to share
await leafen.openPage(pageId);           // a Leafen page, opened in Leafen
```

The app's databases — what its data setup made — are read and written by
their columns' names:

```js
const requests = await leafen.table("requests");   // the setup step's key
for await (const row of requests.rows({ where: [{ column: "Status", operator: "equals", value: "Waiting" }] })) {
  console.log(row.text("Title"), row.option("Status"), row.ids("Approver"), row.createdAt);
}
const id = await requests.add({ Title: "Laptop", Status: "Waiting", Amount: 1200 });
await requests.update(id, { Status: "Approved", Decided: "2026-10-10" });
```

`find(query, cursor)` reads one page at a time, `count(query)` only counts,
`row(id)` reads one row; options go by name and an option the column lacks is
refused.

A refused call rejects with a `LeafenError` whose `code` is Leafen's error
code. Build the screen as a single-page app: the channel belongs to one
document and closes if the frame goes to another page.

## A Go worker

```go
package main

import "leafen.org/leafen/sdk/app/go/leafen"

//go:wasmexport greet
func greet() int32 {
	input := struct{ Name string `json:"name"` }{}
	if _, err := leafen.Action(&input); err != nil {
		return leafen.Fail("the input isn't JSON")
	}
	leafen.Log("greeting " + input.Name)
	_ = leafen.OutputJSON(map[string]string{"message": "Hello, " + input.Name})
	return 0
}

func main() {}
```

Build it as a WebAssembly reactor and name it in the manifest's `workers`:

```sh
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o workers/hello.wasm .
```

Each trigger gives its function a call to decode: `ActionCall` (an action
or an agent's tool call), `HookCall`, `ScheduleCall`, `EventCall` and
`RoomCall`. Inside a worker, `API`, `Relay`, `Tenancies`, `KV`, `Log`,
`Notify` (a notice to a person, when the manifest declares `notifications`),
`SocketOpen`/`SocketWrite`/`SocketRead`/`SocketClose` (addresses in
`network.sockets`), `Wait` (a long-running worker's rest) and
`RoomNext`/`RoomNextFrom`/`RoomSend` (a room's loop; `RoomNextFrom` adds who
sent each message) reach Leafen; nothing else leaves the worker.

`OpenTable(tenancy, key)` finds one of the app's databases by its data setup
step's key: `Rows(Query{Where: …, Sort: …})` goes through the rows a query
keeps, `Find` reads one page, `Count` counts them, `Row(id)` reads one row
(`ErrNoRow` when it isn't there), `Add(values)` and `Update(id, values)` write
by column name, and a row reads back by name with `Text`, `Number`,
`Checkbox`, `Option`, `Options`, `IDs` and `Date`. A run has 200 host calls;
reading takes one for every hundred rows Leafen walks — every five thousand
when the conditions name options, people, rows or a checked checkbox — and
one for every hundred it gives back.

## A Rust worker

```rust
use leafen::Value;

#[no_mangle]
pub extern "C" fn greet() -> i32 {
    let call = match leafen::action() {
        Ok(call) => call,
        Err(_) => return leafen::fail("the input isn't JSON"),
    };
    let name = call.input.get("name").and_then(Value::as_str).unwrap_or("there");
    leafen::log(&format!("greeting {name}"));
    leafen::output_json(&Value::object([("message", format!("Hello, {name}").into())]));
    0
}
```

Make the crate a `cdylib` that depends on `rust/leafen` by path, then:

```sh
rustup target add wasm32-unknown-unknown
cargo build --release --target wasm32-unknown-unknown
cp target/wasm32-unknown-unknown/release/<crate>.wasm workers/hello.wasm
```

`leafen::input_json()` reads any trigger's call as JSON; `api`, `relay`,
`tenancies`, `kv`, `log`, `notify`, `socket_open`/`socket_write`/
`socket_read`/`socket_close`, `wait` and `room_next`/`room_send` reach Leafen.
`leafen::data::Table::open(tenancy, key)` reads and writes the app's databases
by column name as the Go helper's `OpenTable` does (`find`, `each`, `count`,
`row`, `add`, `update`, and `row.text(&table, "Title")` and the like).

## A JavaScript worker

The [Extism JS PDK](https://github.com/extism/js-pdk) runs JavaScript in a
WebAssembly engine. `examples/hello/worker-js/` declares `greet` and the Leafen
host functions it calls (`leafen_kv`, `leafen_log`) in `index.d.ts`; each host
function takes the offset of a JSON request in Extism memory
(`Memory.fromString`) and gives the offset of its JSON answer
(`Memory.find(offset).readString()`), as the Go and Rust helpers do; `leafen_log`
takes the line itself. Build it with `extism-js`, which needs binaryen's
`wasm-merge` and `wasm-opt` on the `PATH`:

```sh
extism-js index.js -i index.d.ts -o ../workers/hello.wasm
```

## Building this SDK

From the product repository: `node scripts/generate-app-sdk.mjs` builds
`dist/`, and `go run ./cmd/leafen-app-schema` writes the manifest schema and
types from the manifest's Go structure (`--check` on either fails when the
committed files are stale).
