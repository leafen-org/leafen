// What Bear, Joplin, Craft, Roam and other tools export as a folder of
// Markdown notes (as a ZIP), through the shared notes converter: the tool is
// told by what its export holds, and its own syntax is made Markdown first —
// Bear's TextBundles and ::highlights::, Roam's to-dos, ^^highlights^^,
// __italics__ and ((block references)). Joplin's and Craft's exports are
// Markdown as they are.

import { notesBundle } from "./notes-convert.js";
import { readZip } from "./zip.js";

const TEXT_BUNDLE = /^(.*?)([^/]+)\.textbundle\/(.+)$/u;

/** Which tool made the export, by its folders and its notes. */
export function exportTool(paths, sample) {
  if (paths.some((path) => /\.textbundle\//u.test(path))) return "bear";
  if (paths.some((path) => /(^|\/)_resources\//u.test(path))) return "joplin";
  if (/\{\{\[\[(TODO|DONE)\]\]\}\}|\(\([\w-]{9}\)\)/u.test(sample)) return "roam";
  return "markdown";
}

/** A tool's syntax as Markdown, counting what has no place. */
function rewriter(tool, labels) {
  if (tool === "bear") {
    return (text) => text.replace(/::(?=\S)([^:\n]+?\S)::/gu, "==$1==");
  }
  if (tool === "roam") {
    return (text, path, lose) => text
      .replace(/\{\{\[\[TODO\]\]\}\}\s*|\{\{TODO\}\}\s*/gu, "[ ] ")
      .replace(/\{\{\[\[DONE\]\]\}\}\s*|\{\{DONE\}\}\s*/gu, "[x] ")
      .replace(/^(\s*)-\s+\[( |x)\]\s/gmu, "$1- [$2] ")
      .replace(/\^\^(?=\S)([^\n]+?\S)\^\^/gu, "==$1==")
      .replace(/(^|[^_])__(?=\S)([^_\n]+?\S)__(?!_)/gu, "$1*$2*")
      .replace(/\(\(([\w-]{9})\)\)/gu, () => {
        lose(labels.lossBlockRef, labels.lossRoamRefWhy);
        return "";
      });
  }
  return (text) => text;
}

/** An export's ZIP as a bundle; `progress(done, total)` follows the notes. */
export async function markdownToolBundle(file, { labels, progress = () => undefined }) {
  const zip = await readZip(file);
  const names = zip.names.filter((name) => !name.split("/").some((part) => part.startsWith(".") || part === "__MACOSX"));
  if (!names.some((name) => /\.(md|markdown)$/iu.test(name))) throw new Error(labels.noMarkdown);
  // The export's one top folder, if it has one, is the folder moved.
  const tops = new Set(names.map((name) => name.split("/")[0]));
  const nested = tops.size === 1 && names.every((name) => name.includes("/"));
  const prefix = nested ? `${[...tops][0]}/` : "";
  const name = nested ? [...tops][0] : file.name.replace(/\.zip$/iu, "");
  const sample = (await Promise.all(names.filter((entry) => /\.(md|markdown)$/iu.test(entry)).slice(0, 20).map((entry) => zip.text(entry)))).join("\n");
  const tool = exportTool(names, sample);
  // A Bear TextBundle is a folder holding text.md and its assets: the note and a folder beside it.
  const files = names.map((entry) => {
    let path = entry.slice(prefix.length);
    const bundle = TEXT_BUNDLE.exec(path);
    if (bundle) path = /^text\.(md|markdown)$/iu.test(bundle[3]) ? `${bundle[1]}${bundle[2]}.md` : `${bundle[1]}${bundle[2]}.assets/${bundle[3].replace(/^assets\//u, "")}`;
    return { path: path.replace(/\.markdown$/iu, ".md"), size: zip.size(entry), read: () => zip.bytes(entry) };
  }).filter((entry) => entry.path && !entry.path.endsWith("/info.json"));
  const rewrite = rewriter(tool, labels);
  const textBundles = tool === "bear"
    ? (text, path, lose) => rewrite(text.replace(/\]\((?:\.\/)?assets\//gu, `](${encodeURIComponent(path.split("/").pop().replace(/\.md$/u, ""))}.assets/`), path, lose)
    : rewrite;
  const made = await notesBundle({ name, files }, { labels, sourceId: `markdown:${name}`, rewrite: textBundles, progress });
  return { ...made, tool };
}
