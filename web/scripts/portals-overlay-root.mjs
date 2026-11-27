import { fileURLToPath } from 'node:url';

// The app's code imports "react-dom" as src/lib/overlayRoot.ts, which draws a
// layer meant for the page's body into one box at its end (see there). Only
// the app's own code: a library keeps React's own, and the module itself
// reads React's. It knows createPortal and flushSync, what the app imports
// from "react-dom"; another name fails the build.
const src = fileURLToPath(new URL('../src/', import.meta.url)).replaceAll('\\', '/');
const own = fileURLToPath(new URL('../src/lib/overlayRoot.ts', import.meta.url)).replaceAll('\\', '/');

export function portalsIntoOverlayRoot() {
  return {
    name: 'leafen-portals-into-overlay-root', enforce: 'pre',
    resolveId(source, importer) {
      if (source !== 'react-dom' || !importer) return null;
      const from = importer.split('?')[0].replaceAll('\\', '/');
      if (!from.startsWith(src) || from === own) return null;
      return own;
    },
  };
}
