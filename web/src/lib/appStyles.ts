// The app's styles, which the built page fetches without waiting on them
// (vite.config.ts, stylesAfterBootScreen) so its own loading screen draws
// while they come: applied here, and the app draws once they have come — or
// failed, so a missing one never keeps it from starting.
export function appStylesReady(): Promise<void> {
  if (typeof document === "undefined") return Promise.resolve();
  const links = [...document.querySelectorAll<HTMLLinkElement>("link[data-app-style]")];
  return Promise.all(links.map((link) => new Promise<void>((done) => {
    if (link.rel === "stylesheet" && link.sheet) {
      done();
      return;
    }
    link.addEventListener("load", () => done(), { once: true });
    link.addEventListener("error", () => done(), { once: true });
    link.rel = "stylesheet";
  }))).then(() => undefined);
}
