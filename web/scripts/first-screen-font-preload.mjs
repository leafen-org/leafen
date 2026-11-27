import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// The page asks for the one bundled face every first screen draws in,
// whatever its language — Inter's Latin (spaces, digits, names, the English
// screens) — as it is read, beside the app's code, not once the
// app has drawn: asked for only then, it came 0.2–0.3 s after the loading
// screen on a first visit to a NAS-class server, and the text drawn
// meanwhile in a stand-in changed shape as it came (2026-11-18). Only the
// page's own face is asked for: the Korean, Japanese and Chinese faces are
// cut in many pieces by the characters they hold, and which a screen needs
// depends on its words and its language, which the page does not know yet;
// asked for on every visit, they would be fetched for every language beside
// the app's code on a slow link. The face is found in the generated sheet,
// so a new set of faces (scripts/vendor-typography-fonts.mjs) carries on.

/** The Latin face of Inter (upright) in typography.generated.css, as its URL. */
export function interLatinFace(css) {
  for (const block of css.match(/@font-face\s*\{[^}]*\}/gu) ?? []) {
    if (!/font-family:\s*"Leafen Inter"/u.test(block) || !/font-style:\s*normal/u.test(block)) continue;
    if (!/unicode-range:[^;]*U\+0000-00FF/iu.test(block)) continue;
    const url = /src:\s*url\("([^"]+\.woff2)"\)/u.exec(block)?.[1];
    if (url) return url;
  }
  return null;
}

export function firstScreenFontPreload(webRoot) {
  return {
    name: "leafen-first-screen-font-preload",
    apply: "build",
    transformIndexHtml: {
      order: "post",
      handler(html) {
        if (!html.includes('id="boot-screen"')) return html;
        const href = interLatinFace(readFileSync(resolve(webRoot, "src/app/typography.generated.css"), "utf8"));
        if (!href) throw new Error("first-screen-font-preload: no upright Latin face of Leafen Inter in typography.generated.css");
        // Fonts are fetched without credentials across the CORS rules even
        // from their own origin; the preload asks the same way, or it is not used.
        return { html, tags: [{ tag: "link", attrs: { rel: "preload", as: "font", type: "font/woff2", href, crossorigin: "" }, injectTo: "head" }] };
      },
    },
  };
}
