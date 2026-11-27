import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// What the page's own loading screen needs comes in the page itself, not as
// files asked of the server beside the app's code: asked for, a request that
// failed or stalled left the screen with a broken picture and no word
// (2026-11-25, a NAS page in an app's browser), and the script that says so
// failed with the rest when the network went right after the page.
//
// - The mark: the same file (public/icon-96.png) as the picture's data, so
//   the screens the app draws while it starts and signs in take it from
//   there too (lib/pageMark.ts) and nothing on the first screens asks for it.
// - The first script (public/theme-init.js): its text in the page, which the
//   server's policy lets run by its hash (internal/app/document_page_scripts.go).
//   The file stays in the build for a page an older build left in a tab.

const MARK = 'data-page-mark src="/icon-96.png"';
const FIRST_SCRIPT = '<script src="/theme-init.js"></script>';

export function pageOwnParts(webRoot) {
  return {
    name: "leafen-page-own-parts",
    transformIndexHtml: {
      order: "pre",
      handler(html) {
        if (!html.includes('id="boot-screen"')) return html;
        if (!html.includes(MARK)) throw new Error(`page-own-parts: the loading screen's picture no longer reads ${MARK}`);
        if (!html.includes(FIRST_SCRIPT)) throw new Error(`page-own-parts: the page no longer reads ${FIRST_SCRIPT}`);
        const mark = readFileSync(resolve(webRoot, "public/icon-96.png")).toString("base64");
        const script = readFileSync(resolve(webRoot, "public/theme-init.js"), "utf8");
        if (script.includes("</script")) throw new Error("page-own-parts: theme-init.js can't hold </script");
        return html
          .replace(MARK, `data-page-mark src="data:image/png;base64,${mark}"`)
          .replace(FIRST_SCRIPT, () => `<script>${script}</script>`);
      },
    },
  };
}
