import { tiptapNodeViewPortals } from "./scripts/tiptap-node-view-portals.mjs";
import { portalsIntoOverlayRoot } from "./scripts/portals-overlay-root.mjs";
import { tiptapSelectionCache } from "./scripts/tiptap-selection-cache.mjs";
import { tiptapDeleteSelectionMarks } from "./scripts/tiptap-delete-selection-marks.mjs";
import { prosemirrorUnfocusedSelection } from "./scripts/prosemirror-unfocused-selection.mjs";
import { reactEditorSelection } from "./scripts/react-editor-selection.mjs";
import react from "@vitejs/plugin-react";
import { resolve } from "node:path";
import { defineConfig, type Plugin } from "vite";

import { writeServiceWorkerPrecache } from "./scripts/generate-sw-precache.mjs";
import { startChunks } from "./scripts/start-chunks.mjs";
import { composedSheetImports } from "./scripts/composed-sheets-once.mjs";
import { firstScreenSplit, shippedStyleRules } from "./scripts/shipped-bundle-rules.mjs";
import { firstScreenFontPreload } from "./scripts/first-screen-font-preload.mjs";
import { pageOwnParts } from "./scripts/page-own-parts.mjs";

// The worker's precache list and build generation go into the folder the
// build wrote, wherever --outDir put it.
function serviceWorkerPrecache(): Plugin {
  return {
    name: "leafen-sw-precache",
    apply: "build",
    writeBundle(options) {
      if (options.dir) writeServiceWorkerPrecache(options.dir);
    },
  };
}

// The release puts the organization module's entry ahead of the app's, so
// the module attaches before the app starts; LEAFEN_CORE=1 builds the core
// alone, which never imports the module.
function organizationModuleEntry(): Plugin {
  return {
    name: "leafen-organization-module",
    // Before the build reads the page's scripts as its entries.
    transformIndexHtml: {
      order: "pre",
      handler(html) {
        if (process.env.LEAFEN_CORE === "1") return html;
        return html.replace(
          '<script type="module" src="/src/main.tsx"></script>',
          '<script type="module" src="/src/organization/entry.ts"></script>\n    <script type="module" src="/src/main.tsx"></script>',
        );
      },
    },
  };
}

// The page's own loading screen (index.html's boot-screen) draws before the
// app's styles have come: the page fetches them without waiting on them, and
// the app applies them and waits for them before it draws anything
// (appStyles.ts). Read in the head, or even in the body, 984 KB of styles
// held a slow network's first visit blank seven seconds, the boot screen
// with it (2026-11-06).
function stylesAfterBootScreen(): Plugin {
  return {
    name: "leafen-styles-after-boot-screen",
    apply: "build",
    transformIndexHtml: {
      order: "post",
      handler(html) {
        if (!html.includes('id="boot-screen"')) return html;
        return html.replace(/<link rel="stylesheet"([^>]*)>/g, '<link rel="preload" as="style" data-app-style$1>');
      },
    },
  };
}

export default defineConfig({
  plugins: [tiptapSelectionCache(), tiptapDeleteSelectionMarks(), prosemirrorUnfocusedSelection(), reactEditorSelection(), tiptapNodeViewPortals(), portalsIntoOverlayRoot(), react(), serviceWorkerPrecache(), organizationModuleEntry(), stylesAfterBootScreen(),
    // The face every first screen draws in, asked for as the page is read (first-screen-font-preload.mjs).
    firstScreenFontPreload(__dirname),
    // What the loading screen needs, in the page itself (page-own-parts.mjs).
    pageOwnParts(__dirname),
    // What ships keeps the hover, height and first-screen rules (shipped-bundle-rules.mjs).
    shippedStyleRules(), firstScreenSplit(__dirname),
    // A sheet composed from ships once, ahead of the sheets that compose from it (composed-sheets-once.mjs).
    composedSheetImports(resolve(__dirname, "src"))],
  resolve: {
    alias: { "@": resolve(__dirname, "src") },
    dedupe: ["yjs"],
  },
  build: {
    manifest: true,
    // Vite gzips every output file only to print a size table; nothing reads it, so builds skip it.
    reportCompressedSize: false,
    // The code a start needs in a few files, not hundreds (start-chunks.mjs).
    rolldownOptions: { output: { codeSplitting: startChunks(resolve(__dirname, "src")) } },
  },
  server: {
    port: 3000,
    strictPort: true,
    // The server serves the third-party notices of its own edition (embed.go).
    proxy: { "/api": "http://127.0.0.1:8787", "/THIRD-PARTY-NOTICES.txt": "http://127.0.0.1:8787" },
  },
});
