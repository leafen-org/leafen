"use strict";

// brace-expansion 5 fixes the release-blocking expansion bound but exposes a
// named CommonJS API. ESLint's still-supported minimatch 3 dependency expects
// the historical callable export. Adapt only this lint process while retaining
// every named field for modern consumers loaded through the same CJS cache.
const modulePath = require.resolve("brace-expansion");
const loaded = require(modulePath);

if (typeof loaded !== "function") {
  if (!loaded || typeof loaded.expand !== "function") {
    throw new TypeError("Unsupported brace-expansion CommonJS API.");
  }
  const callable = Object.assign(loaded.expand, loaded);
  require.cache[modulePath].exports = callable;
}
