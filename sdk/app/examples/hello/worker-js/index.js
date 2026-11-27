// The Hello app's worker in JavaScript, the same as its Go and Rust workers:
// greet answers with a message and counts the greetings in the app's
// temporary store (a new key there needs a lifetime). It runs in the Extism JS
// PDK's engine; build it in place of the Go one (extism-js needs binaryen's
// wasm-merge and wasm-opt on the PATH):
//
//   extism-js index.js -i index.d.ts -o ../workers/hello.wasm

const { leafen_kv, leafen_log } = Host.getFunctions();

// A Leafen host call: the request as JSON in, the answer as JSON out.
function hostJSON(fn, request) {
  const sent = Memory.fromString(JSON.stringify(request));
  const answer = Memory.find(fn(sent.offset));
  try {
    return JSON.parse(answer.readString());
  } finally {
    sent.free();
    answer.free();
  }
}

function greet() {
  let call;
  try {
    call = JSON.parse(Host.inputString());
  } catch {
    // An input that isn't JSON fails as the Go and Rust workers' does.
    throw new Error("the input isn't JSON");
  }
  const name = String(call.input?.name ?? "").trim() || "there";
  const counted = hostJSON(leafen_kv, { command: "INCR", key: "greetings", ttl: 86400 });
  if (counted.error) throw new Error(counted.error);
  const line = Memory.fromString(`greeted ${name}`);
  leafen_log(line.offset);
  line.free();
  Host.outputString(JSON.stringify({ message: `Hello, ${name}!`, count: counted.result }));
  return 0;
}

module.exports = { greet };
