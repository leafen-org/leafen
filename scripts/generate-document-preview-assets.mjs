// Copies the Office app's light viewer — light.js with its sheet view light-sheet.js, its light.css and the four engines it loads
// — to the server's own document preview (internal/app/documentpreview), which
// shows a Word, Excel or PowerPoint file before any document app opens the
// format. The Office app's files are the one source; --check fails when the
// copy differs, --write makes it again.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const from = path.join(root, "catalog-apps/office/ui");
const to = path.join(root, "internal/app/documentpreview");
const items = ["light.js", "light-sheet.js", "light.css", "vendor/jszip-3.10.1", "vendor/docx-preview-0.4.1", "vendor/xlsx-0.20.3", "vendor/pptx-renderer-1.3.0"];

const mode = process.argv[2];
if (mode !== "--check" && mode !== "--write") throw new Error("Usage: node scripts/generate-document-preview-assets.mjs --check|--write");

function list(base, item) {
  const full = path.join(base, item);
  if (!fs.existsSync(full)) return [];
  if (fs.statSync(full).isFile()) return [item];
  return fs.readdirSync(full).flatMap((name) => list(base, path.join(item, name)));
}

const wanted = new Set(items.flatMap((item) => list(from, item)));
const present = new Set(items.flatMap((item) => list(to, item)));
const differ = [...wanted].filter((file) => !present.has(file)
  || !fs.readFileSync(path.join(from, file)).equals(fs.readFileSync(path.join(to, file))));
const extra = [...present].filter((file) => !wanted.has(file));

if (mode === "--write") {
  for (const file of differ) {
    fs.mkdirSync(path.dirname(path.join(to, file)), { recursive: true });
    fs.copyFileSync(path.join(from, file), path.join(to, file));
  }
  for (const file of extra) fs.rmSync(path.join(to, file));
  console.log(`Document preview assets written: ${differ.length} copied, ${extra.length} removed.`);
} else if (differ.length || extra.length) {
  console.error("The document preview's copy of the Office app's light viewer is stale:");
  for (const file of [...differ, ...extra]) console.error(`  ${file}`);
  console.error("Run: node scripts/generate-document-preview-assets.mjs --write");
  process.exit(1);
} else {
  console.log(`Document preview assets verified: ${wanted.size} files.`);
}
