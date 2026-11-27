// The crypto browserCrypto.ts does in script where the page has no browser
// crypto: the audited, widely used @noble implementations. A module of its
// own, fetched only there — its AES tables (1.26 MB) were built on every
// page's start before, where crypto.subtle does the work (2026-11-14).
export { cbc, gcm } from "@noble/ciphers/aes.js";
export { pbkdf2Async } from "@noble/hashes/pbkdf2.js";
export { sha256, sha384, sha512 } from "@noble/hashes/sha2.js";
export { sha1 } from "@noble/hashes/legacy.js";
