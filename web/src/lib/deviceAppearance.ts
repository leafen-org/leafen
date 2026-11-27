// The packaged app's window shows its own color around the page — on iOS
// behind the status bar and the home indicator — so it takes the page's
// background, and the status bar's text stays readable on it, now and
// whenever the theme changes. The shell's appearance is reached through
// Wails' bindings; a page a server serves never asks for it. A page drawn
// light for printing leaves the window as it is (lib/theme.ts).

import { printingInLight } from "./theme";

export function followThemeOnDevice(): void {
  let told = "";
  const tell = () => {
    if (printingInLight()) return;
    const dark = document.documentElement.dataset.theme === "dark";
    const background = getComputedStyle(document.body).backgroundColor;
    const key = `${background}|${dark}`;
    if (!background || key === told) return;
    told = key;
    void import("@wailsio/runtime")
      .then(({ Call }) => Call.ByName("main.Appearance.Set", background, dark))
      .catch(() => undefined);
  };
  tell();
  new MutationObserver(tell).observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
}
