// The code a start needs comes in a few files, not hundreds (2026-11-13: a
// first visit over a NAS's plain http — HTTP/1.1, six connections, 100 ms a
// round trip — fetched about 430 of the build's 720 files, 385 of them under
// 5 KB, six at a time: 8.4 of its 10.5 seconds). Each group below is the
// code one step of a start runs, gathered from the module graph:
//
// - boot: what the page runs before anything (everything index.html loads);
// - view: what the public pages (a shared or published page, a view link)
//   run, which the signed-in product runs too — a page's reading view and
//   editor — and what the product and the database view share;
// - product: the rest of the signed-in product's start;
// - database: a database view's code, which a page loads once it has one.
//
// What a start doesn't run keeps the bundler's own chunks, each loaded when
// it is first asked for, and so does a module imported lazily that none of a
// group's modules imports directly. The boot is one file: split by size, its
// pieces imported each other and a module ran before one it reads.
import { resolve } from "node:path";

/** The build's `output.codeSplitting`, for the app's sources under `src`. */
export function startChunks(src) {
  const at = (path) => resolve(src, path);
  const roots = {
    product: [at("components/product/AuthenticatedProduct.tsx")],
    view: [at("components/SharedPageView.tsx"), at("components/PublicSiteView.tsx"), at("components/ViewLinkView.tsx")],
    database: [at("components/database/InlineDatabase.tsx")],
  };
  // Each root's static imports, all the way down, read once per build.
  let reach = null;
  const reached = (ctx, ids) => {
    const missing = ids.find((id) => !ctx.getModuleInfo(id));
    if (missing) throw new Error(`start-chunks.mjs: ${missing} is not in the build; name the screen that starts there now.`);
    const seen = new Set();
    const walk = (id) => {
      if (seen.has(id)) return;
      seen.add(id);
      for (const next of ctx.getModuleInfo(id)?.importedIds ?? []) walk(next);
    };
    ids.forEach(walk);
    return seen;
  };
  const groupOf = (id, ctx) => {
    reach ??= Object.fromEntries(Object.entries(roots).map(([name, ids]) => [name, reached(ctx, ids)]));
    const info = ctx.getModuleInfo(id);
    if (!info || info.isEntry || info.dynamicImporters.length > 0) return null;
    // Each group takes what its modules import, all the way down, so each
    // is closed under imports: no group reaches into a later one.
    if (reach.view.has(id) || (reach.product.has(id) && reach.database.has(id))) return "view";
    if (reach.product.has(id)) return "product";
    return reach.database.has(id) ? "database" : null;
  };
  const group = (name, priority) => ({ name: (id, ctx) => (groupOf(id, ctx) === name ? name : null), priority });
  return {
    groups: [
      { name: "boot", tags: ["$initial"], priority: 4 },
      group("view", 3),
      group("product", 2),
      group("database", 1),
    ],
  };
}
