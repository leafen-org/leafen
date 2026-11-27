#!/usr/bin/env node

// The web build: every check and generator check at once, then the type
// check and the bundle at once (one after another where memory is short, see
// below). Run one after another they took 72 seconds;
// at once 14 (measured 2026-11-11 on a busy machine: checks 16 s → 3.5 s,
// the type check 38–47 s on TypeScript 5.9 → 4–6 s on 7, beside vite's 10).
//
// The type check runs TypeScript 7 (the `typescript7` dependency) by its
// path: `typescript` stays 5.9 for the scripts that use its API
// (check-module-cycles.mjs), whose API TypeScript 7 gives from 7.1, and its
// `tsc` keeps the name in node_modules/.bin. That 5.9 `tsc` is not the type
// check: its older DOM types judge some code otherwise (2026-11-11: it took
// navigator.locks.request to answer a promise of the callback's promise), so
// the type check is `typeCheck` below, run alone by `npm run typecheck`
// (node scripts/build.mjs --types).
//
// A check that fails fails the build, its output shown; none waits on
// another, as none writes what another reads. A check in Go runs with the go
// on the path, as the Docker image's web stage has.

import { spawn } from "node:child_process";
import { availableParallelism, totalmem } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const web = resolve(dirname(fileURLToPath(import.meta.url)), "..");

/**
 * Each check, as the arguments to node from the web folder, or as another
 * program, its arguments and the folder it runs in, from the web folder.
 */
export const buildChecks = [
  ["scripts/check-translation-keys.mjs"],
  ["scripts/check-korean-catalog.mjs"],
  ["scripts/check-korean-copy.mjs"],
  ["scripts/check-english-copy.mjs"],
  ["../scripts/check-server-english-copy.mjs"],
  // A server word holding a format verb is read as a fmt format.
  { command: "go", args: ["run", "./scripts/check-word-formats"], cwd: ".." },
  // A helper that may write the answer says so beside its error.
  { command: "go", args: ["run", "./scripts/check-written-answers"], cwd: ".." },
  // A refusal says its code and why, and a known cause is never told to try again.
  { command: "go", args: ["run", "./scripts/check-refusal-answers"], cwd: ".." },
  ["../scripts/check-screen-words.mjs"],
  ["../scripts/check-statement-values.mjs"],
  // A word the server writes into a contract's field of words is one it names.
  ["../scripts/check-wire-values.mjs"],
  ["../scripts/check-not-found-codes.mjs"],
  // No line points at the maintainers' private notes, which the repository doesn't carry.
  ["../scripts/check-private-doc-markers.mjs"],
  ["scripts/check-raw-error-text.mjs"],
  ["scripts/check-menu-edges.mjs"],
  ["scripts/check-button-line.mjs"],
  ["scripts/check-logical-direction.mjs"],
  ["scripts/check-composition-keys.mjs"],
  ["scripts/check-late-field-focus.mjs"],
  ["scripts/check-overlay-kinds.mjs"],
  ["scripts/check-page-names.mjs"],
  ["scripts/check-css-tokens.mjs"],
  ["scripts/check-css-sibling-invalidation.mjs"],
  ["scripts/check-css-pseudo-invalidation.mjs"],
  ["scripts/check-css-order-overrides.mjs"],
  ["scripts/check-css-reach-into-parts.mjs"],
  ["scripts/check-css-token-roles.mjs"],
  ["scripts/check-css-module-classes.mjs"],
  ["scripts/check-window-heights.mjs"],
  ["scripts/check-module-cycles.mjs"],
  ["scripts/check-current-target-reads.mjs"],
  ["scripts/check-settings-search-index.mjs"],
  ["scripts/generate-editor-styles.mjs", "--check"],
  ["scripts/vendor-typography-fonts.mjs", "--check"],
  ["../scripts/generate-block-move-wire.mjs", "--check"],
  ["../scripts/generate-url-metadata-wire.mjs", "--check"],
  ["../scripts/generate-teamspace-wire.mjs", "--check"],
  ["../scripts/generate-file-transfer-wire.mjs", "--check"],
  ["../scripts/generate-network-reach.mjs", "--check"],
  ["../scripts/generate-collaboration-storage-wire.mjs", "--check"],
  ["../scripts/generate-workspace-welcome.mjs", "--check"],
  ["../scripts/generate-mcp-screen-names.mjs", "--check"],
  ["../scripts/generate-notion-import-steps.mjs", "--check"],
  ["../scripts/generate-export-property-icons.mjs", "--check"],
  ["../scripts/generate-schema-migration-impact.mjs", "--check"],
  ["../scripts/check-schema-baseline.mjs"],
  ["../scripts/generate-cover-image-wire.mjs", "--check"],
  ["../scripts/generate-page-history-wire.mjs", "--check"],
  ["../scripts/generate-emoji-catalog.mjs", "--check"],
  ["../scripts/generate-zone-city-names.mjs", "--check"],
  ["../scripts/generate-native-icon-catalog.mjs", "--check"],
  ["../scripts/generate-avatar-icons.mjs", "--check"],
  ["../scripts/generate-database-property-conversion-wire.mjs", "--check"],
  ["../scripts/generate-database-data-source-wire.mjs", "--check"],
  ["../scripts/generate-page-backlinks-wire.mjs", "--check"],
  ["../scripts/generate-mcp-oauth-wire.mjs", "--check"],
  ["../scripts/generate-mcp-connection-list-wire.mjs", "--check"],
  ["../scripts/generate-file-security-policy.mjs", "--check"],
  ["../scripts/generate-file-lifecycle-wire.mjs", "--check"],
  ["../scripts/generate-file-usage-report-wire.mjs", "--check"],
  ["../scripts/generate-content-graph-wire.mjs", "--check"],
  ["../scripts/generate-database-row-page-wire.mjs", "--check"],
  ["../scripts/generate-database-toolbar-config-wire.mjs", "--check"],
  ["../scripts/generate-database-mutation-wire.mjs", "--check"],
  ["../scripts/generate-comment-wire.mjs", "--check"],
  ["../scripts/generate-workspace-import-wire.mjs", "--check"],
  ["../scripts/generate-form-wire.mjs", "--check"],
  ["../scripts/generate-database-query-wire.mjs", "--check"],
  ["../scripts/generate-page-permanent-delete-wire.mjs", "--check"],
  ["../scripts/generate-page-duplication-wire.mjs", "--check"],
  ["../scripts/generate-wiki-wire.mjs", "--check"],
  ["../scripts/generate-interactive-automation-wire.mjs", "--check"],
  ["../scripts/generate-database-automation-wire.mjs", "--check"],
  ["../scripts/generate-runtime-config-wire.mjs", "--check"],
  ["../scripts/generate-workspace-membership-wire.mjs", "--check"],
  ["../scripts/generate-organization-wire.mjs", "--check"],
  ["../scripts/generate-notion-admin-token-wire.mjs", "--check"],
  ["../scripts/generate-workspace-search-wire.mjs", "--check"],
  ["../scripts/generate-durable-job-wire.mjs", "--check"],
  ["../scripts/generate-notion-import-wire.mjs", "--check"],
  ["../scripts/generate-administrator-wire.mjs", "--check"],
  ["../scripts/generate-auth-wire.mjs", "--check"],
  ["../scripts/generate-page-analytics-wire.mjs", "--check"],
  ["../scripts/generate-public-content-wire.mjs", "--check"],
  ["../scripts/generate-public-mutation-wire.mjs", "--check"],
  ["../scripts/generate-workspace-export-wire.mjs", "--check"],
  ["../scripts/generate-instance-product-export-wire.mjs", "--check"],
  ["../scripts/generate-enterprise-saml-wire.mjs", "--check"],
  ["../scripts/generate-enterprise-dlp-wire.mjs", "--check"],
  ["../scripts/generate-enterprise-mcp-governance-wire.mjs", "--check"],
  ["../scripts/generate-enterprise-legal-hold-wire.mjs", "--check"],
  ["../scripts/generate-enterprise-audit-retention-wire.mjs", "--check"],
  ["../scripts/generate-organization-audit-wire.mjs", "--check"],
  ["../scripts/generate-organization-discovery-wire.mjs", "--check"],
  ["../scripts/generate-event-webhook-wire.mjs", "--check"],
  ["../scripts/generate-app-registration-wire.mjs", "--check"],
  ["../scripts/generate-app-platform-wire.mjs", "--check"],
  ["../scripts/generate-app-sdk.mjs", "--check"],
  ["../scripts/generate-database-starter-wire.mjs", "--check"],
  ["../scripts/generate-workspace-activity-wire.mjs", "--check"],
  ["../scripts/generate-view-link-wire.mjs", "--check"],
  ["../scripts/generate-deadline-wire.mjs", "--check"],
  ["../scripts/generate-read-log-wire.mjs", "--check"],
  ["../scripts/generate-structure-wire.mjs", "--check"],
  ["../scripts/generate-access-request-wire.mjs", "--check"],
  ["../scripts/generate-native-operation-catalog.mjs", "--check"],
  // What each native route without a contract reads, from its Go source.
  { command: "go", args: ["run", "./scripts/native-operation-inputs", "--check"], cwd: ".." },
  ["../scripts/generate-business-calendar.mjs", "--check"],
  ["../scripts/generate-code-languages.mjs", "--check"],
  ["../scripts/generate-document-preview-assets.mjs", "--check"],
  ["../scripts/generate-export-katex.mjs", "--check"],
];

function run(check, { show = false } = {}) {
  const { command, args, cwd } = Array.isArray(check)
    ? { command: process.execPath, args: check, cwd: web }
    : { ...check, cwd: resolve(web, check.cwd) };
  const label = `${Array.isArray(check) ? "node" : command} ${args.join(" ")}`;
  return new Promise((done) => {
    const child = spawn(command, args, { cwd, stdio: show ? "inherit" : ["ignore", "pipe", "pipe"] });
    let output = "";
    child.stdout?.on("data", (chunk) => { output += chunk; });
    child.stderr?.on("data", (chunk) => { output += chunk; });
    child.on("error", (error) => done({ label, code: 1, output: `${output}${error.message}\n` }));
    child.on("close", (code, signal) => done({ label, code: code ?? (signal ? 1 : 0), output }));
  });
}

async function runAll(list, limit) {
  const results = [];
  let next = 0;
  const lane = async () => {
    while (next < list.length) {
      const args = list[next++];
      results.push(await run(args));
    }
  };
  await Promise.all(Array.from({ length: Math.min(limit, list.length) }, lane));
  return results;
}

function report(failed) {
  for (const result of failed) {
    process.stderr.write(`\n✗ ${result.label} (exit ${result.code})\n${result.output}`);
  }
}

// The type check, over web/tsconfig.json and the files it includes. That
// file's noUnusedLocals and noUnusedParameters fail the build on an import,
// local or parameter nothing reads (a callback's place-held one is named _x).
export const typeCheck = [join(web, "node_modules/typescript7/bin/tsc"), "--noEmit", "-p", join(web, "tsconfig.json")];

if (process.argv.includes("--types")) {
  const result = await run(typeCheck, { show: true });
  process.exit(result.code);
}

const started = Date.now();
const checks = await runAll(buildChecks, Math.max(4, availableParallelism()));
const failedChecks = checks.filter((result) => result.code !== 0);
if (failedChecks.length) {
  report(failedChecks);
  process.stderr.write(`\n${failedChecks.length} of ${buildChecks.length} build checks failed.\n`);
  process.exit(1);
}
const checked = Date.now();
// The type check and the bundle each hold 2.2–3 GB at their peak (measured
// 2026-11-15), so together they don't fit in a 4 GiB Docker, the memory
// docs/self-hosting.md asks for: below 6 GiB they run one after another.
const memory = Math.min(totalmem(), process.constrainedMemory?.() || Infinity);
// What follows `npm run build --` goes to vite build, as when the build was
// vite's alone: a build with hidden source maps in a folder of its own
// passes `--sourcemap hidden --outDir <folder> --emptyOutDir`.
const viteArguments = process.argv.slice(2);
const bundling = () => run([join(web, "node_modules/vite/bin/vite.js"), "build", ...viteArguments]);
const [types, bundle] = memory < 6 * 1024 ** 3
  ? [await run(typeCheck), await bundling()]
  : await Promise.all([run(typeCheck), bundling()]);
process.stdout.write(bundle.output);
const failed = [types, bundle].filter((result) => result.code !== 0);
if (failed.length) {
  report(failed);
  process.exit(1);
}
process.stdout.write(`build: ${buildChecks.length} checks ${((checked - started) / 1000).toFixed(1)}s, type check and bundle ${((Date.now() - checked) / 1000).toFixed(1)}s\n`);
