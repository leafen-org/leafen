// What the build ships keeps three rules, checked on the bundle itself so a
// way around the source checks fails the build too (2026-11-17: rules a sheet
// composed from another file came in after the hover and height transforms
// had run, and a page under a template brought the database views into the
// signed-in first screen):
//
// - Hover styles apply where the pointer hovers (postcss.config.mjs): every
//   :hover selector stands under @media (hover: hover), or under a touch
//   screen's own (hover: none) query, where src/app/touchHoverCells.css
//   writes the grey cell a tap leaves.
// - Heights follow the window a person sees: no vh length ships, only dvh
//   (postcss.config.mjs).
// - The keys' own mark draws as the keyboard's ring does: every :focus-visible
//   ships beside data-key-focus (postcss.config.mjs), and none inside :has(),
//   where a row asks its own data-keys-within instead (lib/lastPress.ts
//   keysFocusHolder).
// - A stylesheet ships once: no CSS module rule comes twice, the way a sheet
//   composed from another file once came in a copy before every sheet that
//   composed from it (composed-sheets-once.mjs).
// - The first screens download only what they draw: the boot path carries no
//   signed-in product, editor, equation, collaboration or database code; the
//   signed-in product's first screen carries no database views, row pages,
//   library, settings, search or import; a shared page carries no database
//   views and, unless it is a database row, no row properties. Each of these
//   arrives with its own chunk when it is first shown.
import { relative } from "node:path";

const BOOT_LEAVES = ["node_modules/katex/", "node_modules/yjs/", "node_modules/@tiptap/core/",
  "src/components/auth/AuthenticatedSession.tsx", "src/components/product/AuthenticatedProduct.tsx",
  "src/components/page/TopBar.tsx", "src/components/database/InlineDatabase.tsx"];
const PRODUCT_LEAVES = ["src/components/database/InlineDatabase.tsx", "src/components/database/TableView.tsx",
  "src/components/database/StandaloneRowPage.tsx", "src/components/library/LibraryView.tsx",
  "src/components/settings/SettingsWindow.tsx", "src/components/search/SearchDialog.tsx",
  "src/components/import/ImportDialog.tsx"];
const SHARED_LEAVES = ["src/components/database/InlineDatabase.tsx", "src/components/database/PublicRowPageProperties.tsx",
  "src/components/database/PropertyCell.tsx"];
const OWN_CHUNKS = ["src/components/database/InlineDatabase.tsx", "src/components/settings/SettingsWindow.tsx",
  "src/components/database/PublicRowPageProperties.tsx", "src/components/auth/AuthenticatedSession.tsx"];

// Each block's prelude in a style sheet, with the preludes it stands in; and
// as each block closes, its text from its prelude on.
function* blocks(css) {
  const stack = [];
  let start = 0;
  let quote = "";
  for (let index = 0; index < css.length; index++) {
    const char = css[index];
    if (quote) {
      if (char === "\\") index++;
      else if (char === quote) quote = "";
    } else if (char === '"' || char === "'") quote = char;
    else if (char === "/" && css[index + 1] === "*") {
      const end = css.indexOf("*/", index + 2);
      index = end < 0 ? css.length : end + 1;
      start = index + 1;
    } else if (char === "{") {
      const raw = css.slice(start, index);
      const prelude = raw.trim();
      yield { prelude, within: stack.map((block) => block.prelude) };
      if (stack.length) stack[stack.length - 1].nested = true;
      stack.push({ prelude, at: start + raw.length - raw.trimStart().length, nested: false });
      start = index + 1;
    } else if (char === "}") {
      const block = stack.pop();
      if (block && !block.nested) yield { closed: css.slice(block.at, index + 1), within: stack.map((outer) => outer.prelude) };
      start = index + 1;
    } else if (char === ";") start = index + 1;
  }
}

// A CSS module's class as the build names it: _local_hash_line.
const moduleClass = /\._[A-Za-z][\w-]*_[a-z0-9]{5}_\d+/u;

// A selector list's selectors, split at its own commas only.
function selectors(list) {
  const out = [];
  let depth = 0;
  let start = 0;
  for (let index = 0; index < list.length; index++) {
    if (list[index] === "(") depth++;
    else if (list[index] === ")") depth--;
    else if (list[index] === "," && depth === 0) {
      out.push(list.slice(start, index));
      start = index + 1;
    }
  }
  return [...out, list.slice(start)];
}

// ":not(:hover)" is what is not hovered, a touch screen's every element too.
const hovers = (selector) => selector.replace(/:not\([^()]*:hover[^()]*\)/gu, "").includes(":hover");

// A :focus-visible outside :has() that the keys' own mark doesn't come with.
const keysRing = /:is\(:focus-visible,\s*:where\(\[data-key-focus\]\):focus\)/gu;
const lonelyFocusVisible = (selector) => {
  let out = selector;
  for (let at = out.indexOf(":has("); at >= 0; at = out.indexOf(":has(")) {
    let depth = 0, end = at + 4;
    for (; end < out.length; end++) {
      if (out[end] === "(") depth++;
      else if (out[end] === ")" && --depth === 0) break;
    }
    out = out.slice(0, at) + out.slice(end + 1);
  }
  return /:focus-visible(?![\w-])/u.test(out.replace(keysRing, ""));
};

// A :focus-visible a :has() asks for.
const hasFocusVisible = (selector) => {
  for (let at = selector.indexOf(":has("); at >= 0; at = selector.indexOf(":has(", at + 1)) {
    let depth = 0, end = at + 4;
    for (; end < selector.length; end++) {
      if (selector[end] === "(") depth++;
      else if (selector[end] === ")" && --depth === 0) break;
    }
    if (/:focus-visible(?![\w-])/u.test(selector.slice(at, end))) return true;
  }
  return false;
};

export function shippedStyleRules() {
  return {
    name: "leafen-shipped-style-rules",
    apply: "build",
    enforce: "post",
    generateBundle(_, bundle) {
      const failures = [];
      const shipped = new Map();
      for (const file of Object.values(bundle)) {
        if (file.type !== "asset" || !file.fileName.endsWith(".css")) continue;
        const css = typeof file.source === "string" ? file.source : new TextDecoder().decode(file.source);
        for (const { prelude, within, closed } of blocks(css)) {
          if (closed !== undefined) {
            if (!moduleClass.test(closed.slice(0, closed.indexOf("{")))) continue;
            const rule = [...within, closed].join(" ");
            if (shipped.has(rule)) failures.push(`${file.fileName}: ${closed.slice(0, 120)} — ships again (also in ${shipped.get(rule)}): a stylesheet copied in, not imported once`);
            else shipped.set(rule, file.fileName);
            continue;
          }
          if (!prelude.startsWith("@") && selectors(prelude).some(lonelyFocusVisible)) {
            failures.push(`${file.fileName}: ${prelude.slice(0, 120)} — a :focus-visible without the keys' own mark (postcss.config.mjs)`);
          }
          if (!prelude.startsWith("@") && selectors(prelude).some(hasFocusVisible)) {
            failures.push(`${file.fileName}: ${prelude.slice(0, 120)} — a :has() asking for :focus-visible: the row's data-keys-within draws it (lib/lastPress.ts keysFocusHolder)`);
          }
          if (prelude.startsWith("@") || !selectors(prelude).some(hovers)) continue;
          if (within.some((outer) => outer.startsWith("@media") && /hover:\s*(?:hover|none)/u.test(outer))) continue;
          failures.push(`${file.fileName}: ${prelude.slice(0, 120)} — a :hover outside @media (hover: hover)`);
        }
        for (const match of css.matchAll(/[^;{}]*\dvh\b[^;{}]*/gu)) {
          failures.push(`${file.fileName}: ${match[0].trim().slice(0, 120)} — a vh height (dvh, as postcss.config.mjs writes them)`);
        }
      }
      if (failures.length) this.error(`The shipped styles break the build's own rules:\n  ${failures.join("\n  ")}`);
    },
  };
}

export function firstScreenSplit(webRoot) {
  // A module's path from the web folder, a package's from its node_modules.
  const pathOf = (id) => {
    const packaged = id.lastIndexOf("/node_modules/");
    return packaged >= 0 ? id.slice(packaged + 1) : relative(webRoot, id);
  };
  return {
    name: "leafen-first-screen-split",
    apply: "build",
    enforce: "post",
    generateBundle(_, bundle) {
      const chunks = Object.values(bundle).filter((file) => file.type === "chunk");
      const byFile = new Map(chunks.map((chunk) => [chunk.fileName, chunk]));
      const reach = (starts, found = new Set()) => {
        for (const chunk of starts) {
          if (!chunk || found.has(chunk.fileName)) continue;
          found.add(chunk.fileName);
          reach(chunk.imports.map((file) => byFile.get(file)), found);
        }
        return found;
      };
      const failures = [];
      const facade = (path) => {
        const chunk = chunks.find((candidate) => candidate.facadeModuleId && pathOf(candidate.facadeModuleId) === path);
        if (!chunk) failures.push(`no chunk starts at ${path}`);
        return chunk;
      };
      const entry = reach(chunks.filter((chunk) => chunk.isEntry));
      const carried = (files) => {
        const found = new Set();
        for (const file of files) {
          for (const [id, module] of Object.entries(byFile.get(file).modules)) {
            if (module.renderedLength > 0 && !id.startsWith("\0")) found.add(pathOf(id));
          }
        }
        return found;
      };
      const carries = (found, path) => [...found].some((source) => source === path || source.startsWith(path));
      const beyondEntry = (...paths) => carried([...reach(paths.map(facade))].filter((file) => !entry.has(file)));
      const boot = carried(entry);
      for (const path of BOOT_LEAVES) if (carries(boot, path)) failures.push(`the boot path carries ${path}`);
      const product = beyondEntry("src/components/auth/AuthenticatedSession.tsx", "src/components/product/AuthenticatedProduct.tsx");
      if (!carries(product, "src/components/editor/Editor.tsx")) failures.push("the signed-in first screen no longer carries the editor: the check reads the chunks wrongly");
      for (const path of PRODUCT_LEAVES) if (carries(product, path)) failures.push(`the signed-in first screen carries ${path}`);
      const shared = beyondEntry("src/components/SharedPageView.tsx");
      for (const path of SHARED_LEAVES) if (carries(shared, path)) failures.push(`a shared page carries ${path}`);
      for (const path of OWN_CHUNKS) if (facade(path) && !facade(path).isDynamicEntry) failures.push(`${path} is not its own chunk`);
      if (failures.length) {
        this.error(`The first screens carry code they do not draw (import it lazily where it is first shown):\n  ${[...new Set(failures)].join("\n  ")}`);
      }
    },
  };
}
