// A class a sheet composes from another file (`composes: x from
// "./Other.module.css"`) brings that file's stylesheet once, where the file
// itself ships, not a copy of it in every sheet that composes from it. The
// CSS modules step on its own puts the whole other file in front of each
// such sheet: on 2026-11-17 39 KB of the build's styles were such copies (322
// rules from 10 sheets; WorkspaceSettingsChrome's 565 lines three times over
// for one button), and a copy loaded later than a third sheet re-applied its
// rules over that sheet's, a look that depended on which screen opened
// first. So the sheet drops the copy (composedCopiesDropped, a postcss
// plugin that runs right after the CSS modules step), and its module imports
// the other file instead (composedSheetImports, a Vite plugin): the other
// file's module runs first, so its styles come ahead of the sheet's, in the
// same chunk or one it loads first, and the sheet's own rules still win.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";

const composesFrom = /composes\s*:[^;{}]*?\bfrom\s+(["'])([^"']+)\1/gu;
const moduleSheet = /\.module\.css$/u;

/** The other files a CSS module composes from, as absolute paths. */
function composedFiles(path) {
  const text = readFileSync(path, "utf8").replace(/\/\*[\s\S]*?\*\//gu, "");
  return [...new Set([...text.matchAll(composesFrom)].map((match) => resolve(dirname(path), match[2])))];
}

/** A postcss plugin: the rules the CSS modules step copied in from composed files leave the sheet. */
export function composedCopiesDropped() {
  const own = new WeakMap();
  return {
    postcssPlugin: "leafen-composed-copies-dropped",
    // The sheet's own nodes before the CSS modules step ends.
    Once(root) {
      own.set(root, new Set(root.nodes));
    },
    // After it: what it put in front of them is the copied files, parsed
    // from text and so without a place in this sheet's source.
    OnceExit(root) {
      if (!moduleSheet.test(root.source?.input.file ?? "")) return;
      const kept = own.get(root);
      if (!kept) return;
      root.each((node) => {
        if (!kept.has(node) && !node.source) node.remove();
      });
    },
  };
}
composedCopiesDropped.postcss = true;

/** A Vite plugin: a CSS module imports the files it composes from, and those files' styles always ship. */
export function composedSheetImports(src) {
  let composed = new Set();
  const sheets = (dir, out = []) => {
    for (const name of readdirSync(dir)) {
      const path = join(dir, name);
      if (statSync(path).isDirectory()) sheets(path, out);
      else if (moduleSheet.test(name)) out.push(path);
    }
    return out;
  };
  return {
    name: "leafen-composed-sheet-imports",
    enforce: "post",
    buildStart() {
      composed = new Set(sheets(src).flatMap(composedFiles));
    },
    transform(code, id) {
      if (!moduleSheet.test(id)) return null;
      const imports = composedFiles(id).map((path) => `import ${JSON.stringify(path)};\n`).join("");
      // A file only composed from exports nothing a script reads; its styles
      // ship all the same.
      const shipped = composed.has(id);
      if (!imports && !shipped) return null;
      return { code: imports + code, map: { mappings: "" }, ...(shipped ? { moduleSideEffects: "no-treeshake" } : {}) };
    },
  };
}
