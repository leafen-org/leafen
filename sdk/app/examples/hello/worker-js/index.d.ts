// What the Hello worker exports, and the Leafen host functions it calls, for
// the Extism JS PDK's builder (extism-js). Each Leafen host function takes the
// offset of a request in Extism memory and gives the offset of its answer;
// leafen_log takes the line itself. The Go and Rust helpers wrap the same
// functions (../../../go, ../../../rust).

declare module "main" {
  export function greet(): I32;
}

declare module "extism:host" {
  interface user {
    leafen_log(offset: I64): I64;
    leafen_api(offset: I64): I64;
    leafen_kv(offset: I64): I64;
    leafen_notify(offset: I64): I64;
  }
}
