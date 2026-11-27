//! Helps write a Leafen app worker in Rust (Apache-2.0, see ../../../LICENSE).
//! A worker is a WebAssembly module Leafen runs with the Extism calling
//! convention: each function the manifest's worker lists is exported with
//! `#[no_mangle] pub extern "C" fn name() -> i32`, reads its input, sets its
//! output or its error, and returns 0 (or 1 after `fail`). Build it as a
//! `cdylib` for `wasm32-unknown-unknown`:
//!
//! ```text
//! cargo build --release --target wasm32-unknown-unknown
//! ```
//!
//! Inside, a worker reaches Leafen only through the host functions wrapped
//! here: the native API as the app's account (or the person, for an action
//! run as them), the approved outside addresses through Leafen's relay, the
//! app's organizations and its temporary store, and the run's log. The crate
//! needs nothing else; `json` reads and writes what those calls carry.

pub mod base64;
pub mod data;
pub mod json;

pub use json::Value;

/// An action run from the app's screen, or an agent's call of the action as
/// a tool (then `via` names the connection).
#[derive(Clone, Debug)]
pub struct ActionCall {
    pub action: String,
    pub input: Value,
    /// The person who ran it, and the organization it ran in.
    pub person: String,
    pub tenancy: String,
    pub via: Option<Value>,
    /// Their role in that organization (owner, admin, member, guest…) and
    /// the admin roles they hold there (people, it, compliance, analytics,
    /// billing), as Leafen's settings know them.
    pub role: String,
    pub admin_roles: Vec<String>,
}

/// Reads the call's input as an action.
pub fn action() -> Result<ActionCall, String> {
    let call = input_json()?;
    let text = |key: &str| call.get(key).and_then(Value::as_str).unwrap_or("").to_string();
    Ok(ActionCall {
        action: text("action"),
        input: call.get("input").cloned().unwrap_or(Value::Null),
        person: text("person"),
        tenancy: text("tenancy"),
        via: call.get("via").cloned(),
        role: text("role"),
        admin_roles: call.get("adminRoles").and_then(Value::as_array)
            .map(|roles| roles.iter().filter_map(Value::as_str).map(str::to_string).collect()).unwrap_or_default(),
    })
}

/// Reads the call's input as JSON — for a webhook, schedule, data change or
/// room message, whose shapes the developer guide lists.
pub fn input_json() -> Result<Value, String> {
    Value::parse(&input())
}

/// Sets the call's output to `value` as JSON.
pub fn output_json(value: &Value) {
    output(value.to_json().as_bytes());
}

/// Calls Leafen's native API (paths below /api/leafen/v1): a request
/// `{tenancy?, method, path, query?, body?}` gives `{status, body, text}`.
pub fn api(request: &Value) -> Result<Value, String> {
    host_json(Host::Api, request)
}

/// Lists the organizations the app is installed for: `[{tenancy, workspaceId}]`.
pub fn tenancies() -> Result<Value, String> {
    host_json(Host::Tenancies, &Value::Object(Vec::new()))
}

/// Reaches an approved outside address through Leafen, which fills in the
/// `{{secret:NAME}}` and `{{oauth:KEY}}` placeholders: a request
/// `{method, url, headers?, body?, bodyBase64?}` gives
/// `{status, headers, body, bodyBase64}`.
pub fn relay(request: &Value) -> Result<Value, String> {
    let answer = host_json(Host::Relay, request)?;
    match answer.get("error").and_then(Value::as_str) {
        Some(error) if !error.is_empty() => Err(error.to_string()),
        _ => Ok(answer),
    }
}

/// Runs one command on the app's temporary store (`{command, key, …, ttl}`;
/// a new key needs a `ttl`) and gives its result.
pub fn kv(request: &Value) -> Result<Value, String> {
    let answer = host_json(Host::Kv, request)?;
    match answer.get("error").and_then(Value::as_str) {
        Some(error) if !error.is_empty() => Err(error.to_string()),
        _ => Ok(answer.get("result").cloned().unwrap_or(Value::Null)),
    }
}

/// Adds a line to the run's log, which Leafen keeps with the run.
pub fn log(message: &str) {
    let _ = host_call(Host::Log, message.as_bytes());
}

/// Connects to a TCP or TLS address the manifest's `network.sockets` lists,
/// through Leafen; a run keeps at most four open, closed when it ends.
pub fn socket_open(host: &str, port: u16, tls: bool) -> Result<i64, String> {
    let answer = socket_call(Host::SocketOpen, &Value::object([("host", host.into()), ("port", i64::from(port).into()), ("tls", tls.into())]))?;
    answer.get("socket").and_then(Value::as_f64).map(|socket| socket as i64).ok_or_else(|| "no socket".to_string())
}

/// Sends up to 64 KB.
pub fn socket_write(socket: i64, data: &[u8]) -> Result<(), String> {
    socket_call(Host::SocketWrite, &Value::object([("socket", socket.into()), ("data", base64::encode(data).into())])).map(|_| ())
}

/// Waits up to `wait_ms` (30 seconds at most) for up to `max` bytes (64 KB at
/// most); the flag says the connection ended.
pub fn socket_read(socket: i64, max: i64, wait_ms: i64) -> Result<(Vec<u8>, bool), String> {
    let answer = socket_call(Host::SocketRead, &Value::object([("socket", socket.into()), ("max", max.into()), ("waitMs", wait_ms.into())]))?;
    let data = base64::decode(answer.get("data").and_then(Value::as_str).unwrap_or(""))?;
    Ok((data, answer.get("closed").and_then(Value::as_bool).unwrap_or(false)))
}

/// Ends a connection.
pub fn socket_close(socket: i64) -> Result<(), String> {
    socket_call(Host::SocketClose, &Value::object([("socket", socket.into())])).map(|_| ())
}

fn socket_call(function: Host, request: &Value) -> Result<Value, String> {
    let answer = host_json(function, request)?;
    match answer.get("error").and_then(Value::as_str) {
        Some(error) if !error.is_empty() => Err(error.to_string()),
        _ => Ok(answer),
    }
}

/// Lets a long-running worker rest up to a minute; true means it should end
/// (the app stopped, or its hour is nearly up).
pub fn wait(ms: i64) -> Result<bool, String> {
    let answer = host_json(Host::Wait, &Value::object([("ms", ms.into())]))?;
    Ok(answer.get("stop").and_then(Value::as_bool).unwrap_or(false))
}

/// Gives a room loop the messages its room sent the server — up to 64,
/// waiting up to `wait_ms` (30 seconds at most) for the first — whether to
/// stop (the room emptied, the app stopped) and how many are in the room.
pub fn room_next(wait_ms: i64) -> Result<(Vec<String>, bool, i64), String> {
    let answer = host_json(Host::RoomNext, &Value::object([("waitMs", wait_ms.into())]))?;
    let messages = answer.get("messages").and_then(Value::as_array).unwrap_or(&[]).iter()
        .filter_map(|message| message.as_str().map(str::to_string)).collect();
    let stop = answer.get("stop").and_then(Value::as_bool).unwrap_or(false);
    let peers = answer.get("peers").and_then(Value::as_f64).unwrap_or(0.0) as i64;
    Ok((messages, stop, peers))
}

/// Like `room_next`, with each message's sender as Leafen knows them —
/// `(text, sender)`, the sender `{person, name}` or `{guest, name}`.
pub fn room_next_from(wait_ms: i64) -> Result<(Vec<(String, Value)>, bool, i64), String> {
    let answer = host_json(Host::RoomNext, &Value::object([("waitMs", wait_ms.into())]))?;
    let senders = answer.get("senders").and_then(Value::as_array).unwrap_or(&[]).to_vec();
    let messages = answer.get("messages").and_then(Value::as_array).unwrap_or(&[]).iter().enumerate()
        .filter_map(|(index, message)| message.as_str().map(|text| (text.to_string(), senders.get(index).cloned().unwrap_or(Value::Null))))
        .collect();
    let stop = answer.get("stop").and_then(Value::as_bool).unwrap_or(false);
    let peers = answer.get("peers").and_then(Value::as_f64).unwrap_or(0.0) as i64;
    Ok((messages, stop, peers))
}

/// Sends a message (text up to 64 KB) to everyone in a room loop's room.
pub fn room_send(message: &str) -> Result<(), String> {
    socket_call(Host::RoomSend, &Value::object([("message", message.into())])).map(|_| ())
}

/// Sends a notice to a person who may use the app — `{person, text, path?,
/// tenancy?}`: one line of text in their inbox (and mail and push, as they
/// chose), opening `path` in the app. The manifest declares
/// `"notifications": true`; at most ten an hour reach one person.
pub fn notify(request: &Value) -> Result<(), String> {
    let answer = host_json(Host::Notify, request)?;
    match answer.get("error").and_then(Value::as_str) {
        Some(error) if !error.is_empty() => Err(error.to_string()),
        _ => Ok(()),
    }
}

/// Joins files the run may read, in order, into a new file in the app's
/// workspace — `{files, name, contentType}` — as a resumable or multipart
/// upload ends, answering the file (`id`, `name`, `size`, `contentType`).
/// `workspaceId` puts it in another workspace the run may write instead.
pub fn join_files(request: &Value) -> Result<Value, String> {
    let answer = host_json(Host::FilesJoin, request)?;
    match answer.get("error").and_then(Value::as_str) {
        Some(error) if !error.is_empty() => Err(error.to_string()),
        _ => answer.get("file").cloned().ok_or_else(|| "no file".to_string()),
    }
}

/// Calls a host function with `request` as JSON; Leafen's refusal
/// (`{"error": "…"}`) is an `Err`.
fn host_json(function: Host, request: &Value) -> Result<Value, String> {
    let answer = Value::parse(&host_call(function, request.to_json().as_bytes())?)?;
    match answer.get("error").and_then(Value::as_str) {
        Some(error) if !error.is_empty() => Err(error.to_string()),
        _ => Ok(answer),
    }
}

#[derive(Clone, Copy)]
enum Host {
    Log,
    Api,
    Relay,
    Tenancies,
    Kv,
    Notify,
    SocketOpen,
    SocketWrite,
    SocketRead,
    SocketClose,
    Wait,
    RoomNext,
    RoomSend,
    FilesJoin,
}

pub use host::{fail, input, output};
use host::{host_call, now_millis, random_bytes};

/// Twelve random bytes as hex, for the IDs a worker makes.
pub fn random_hex() -> String {
    let mut bytes = [0u8; 12];
    random_bytes(&mut bytes);
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}

/// The time now in milliseconds since 1970.
pub fn now() -> u64 {
    now_millis()
}

// The Extism calling convention by hand, so a worker needs no other crate.
#[cfg(target_arch = "wasm32")]
mod host {
    use super::Host;

    #[link(wasm_import_module = "extism:host/env")]
    extern "C" {
        fn input_length() -> u64;
        fn input_load_u8(offset: u64) -> u8;
        fn input_load_u64(offset: u64) -> u64;
        fn alloc(length: u64) -> u64;
        fn store_u8(offset: u64, value: u8);
        fn store_u64(offset: u64, value: u64);
        fn load_u8(offset: u64) -> u8;
        fn load_u64(offset: u64) -> u64;
        fn length(offset: u64) -> u64;
        fn output_set(offset: u64, length: u64);
        fn error_set(offset: u64);
    }

    // Leafen gives every worker WASI's real clock and random numbers.
    #[link(wasm_import_module = "wasi_snapshot_preview1")]
    extern "C" {
        fn random_get(buffer: *mut u8, length: usize) -> u16;
        fn clock_time_get(clock: u32, precision: u64, time: *mut u64) -> u16;
    }

    pub fn random_bytes(out: &mut [u8]) {
        unsafe { random_get(out.as_mut_ptr(), out.len()) };
    }

    pub fn now_millis() -> u64 {
        let mut nanoseconds = 0u64;
        unsafe { clock_time_get(0, 1_000_000, &mut nanoseconds) };
        nanoseconds / 1_000_000
    }

    #[link(wasm_import_module = "extism:host/user")]
    extern "C" {
        fn leafen_log(offset: u64) -> u64;
        fn leafen_api(offset: u64) -> u64;
        fn leafen_relay(offset: u64) -> u64;
        fn leafen_tenancies(offset: u64) -> u64;
        fn leafen_kv(offset: u64) -> u64;
        fn leafen_notify(offset: u64) -> u64;
        fn leafen_socket_open(offset: u64) -> u64;
        fn leafen_socket_write(offset: u64) -> u64;
        fn leafen_socket_read(offset: u64) -> u64;
        fn leafen_socket_close(offset: u64) -> u64;
        fn leafen_wait(offset: u64) -> u64;
        fn leafen_room_next(offset: u64) -> u64;
        fn leafen_room_send(offset: u64) -> u64;
        fn leafen_files_join(offset: u64) -> u64;
    }

    /// The call's input.
    pub fn input() -> Vec<u8> {
        unsafe {
            let size = input_length();
            let mut data = vec![0u8; size as usize];
            let mut index = 0u64;
            while index + 8 <= size {
                data[index as usize..index as usize + 8].copy_from_slice(&input_load_u64(index).to_le_bytes());
                index += 8;
            }
            while index < size {
                data[index as usize] = input_load_u8(index);
                index += 1;
            }
            data
        }
    }

    /// Sets the call's output.
    pub fn output(data: &[u8]) {
        unsafe { output_set(store(data), data.len() as u64) }
    }

    /// Sets the call's error; return its result from the exported function.
    pub fn fail(message: &str) -> i32 {
        unsafe { error_set(store(message.as_bytes())) };
        1
    }

    fn store(data: &[u8]) -> u64 {
        unsafe {
            let offset = alloc(data.len() as u64);
            let mut chunks = data.chunks_exact(8);
            let mut index = offset;
            for chunk in &mut chunks {
                store_u64(index, u64::from_le_bytes(chunk.try_into().unwrap()));
                index += 8;
            }
            for byte in chunks.remainder() {
                store_u8(index, *byte);
                index += 1;
            }
            offset
        }
    }

    fn load(offset: u64) -> Vec<u8> {
        unsafe {
            let size = length(offset);
            let mut data = vec![0u8; size as usize];
            let mut index = 0u64;
            while index + 8 <= size {
                data[index as usize..index as usize + 8].copy_from_slice(&load_u64(offset + index).to_le_bytes());
                index += 8;
            }
            while index < size {
                data[index as usize] = load_u8(offset + index);
                index += 1;
            }
            data
        }
    }

    pub fn host_call(function: Host, payload: &[u8]) -> Result<Vec<u8>, String> {
        let offset = store(payload);
        let answer = unsafe {
            match function {
                Host::Log => leafen_log(offset),
                Host::Api => leafen_api(offset),
                Host::Relay => leafen_relay(offset),
                Host::Tenancies => leafen_tenancies(offset),
                Host::Kv => leafen_kv(offset),
                Host::Notify => leafen_notify(offset),
                Host::SocketOpen => leafen_socket_open(offset),
                Host::SocketWrite => leafen_socket_write(offset),
                Host::SocketRead => leafen_socket_read(offset),
                Host::SocketClose => leafen_socket_close(offset),
                Host::Wait => leafen_wait(offset),
                Host::RoomNext => leafen_room_next(offset),
                Host::RoomSend => leafen_room_send(offset),
                Host::FilesJoin => leafen_files_join(offset),
            }
        };
        Ok(load(answer))
    }
}

// Outside a Leafen worker there is no host: these let a worker's other code
// build and be tested on its own.
#[cfg(not(target_arch = "wasm32"))]
mod host {
    use super::Host;

    /// Nothing, outside a worker.
    pub fn input() -> Vec<u8> {
        Vec::new()
    }

    /// Does nothing outside a worker.
    pub fn output(_data: &[u8]) {}

    /// Returns 1 outside a worker.
    pub fn fail(_message: &str) -> i32 {
        1
    }

    pub fn host_call(_function: Host, _payload: &[u8]) -> Result<Vec<u8>, String> {
        Err("leafen: runs only inside a Leafen worker (wasm32-unknown-unknown)".to_string())
    }

    pub fn random_bytes(out: &mut [u8]) {
        use std::hash::{BuildHasher, Hasher};
        for (index, chunk) in out.chunks_mut(8).enumerate() {
            let mut hasher = std::collections::hash_map::RandomState::new().build_hasher();
            hasher.write_usize(index);
            chunk.copy_from_slice(&hasher.finish().to_le_bytes()[..chunk.len()]);
        }
    }

    pub fn now_millis() -> u64 {
        std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|time| time.as_millis() as u64).unwrap_or(0)
    }
}
