//! The Hello app's worker in Rust, the same as its Go worker: greet answers
//! with a message and counts the greetings in the app's temporary store (a
//! new key there needs a lifetime). Build it in place of the Go one:
//!
//! ```text
//! cargo build --release --target wasm32-unknown-unknown
//! cp target/wasm32-unknown-unknown/release/hello_worker.wasm ../workers/hello.wasm
//! ```

use leafen::Value;

#[no_mangle]
pub extern "C" fn greet() -> i32 {
    let call = match leafen::action() {
        Ok(call) => call,
        Err(_) => return leafen::fail("the input isn't JSON"),
    };
    let name = call.input.get("name").and_then(Value::as_str).map(str::trim).unwrap_or("");
    let name = if name.is_empty() { "there" } else { name };
    let count = match leafen::kv(&Value::object([("command", "INCR".into()), ("key", "greetings".into()), ("ttl", 86400i64.into())])) {
        Ok(count) => count,
        Err(error) => return leafen::fail(&error),
    };
    leafen::log(&format!("greeted {name}"));
    leafen::output_json(&Value::object([("message", format!("Hello, {name}!").into()), ("count", count)]));
    0
}
