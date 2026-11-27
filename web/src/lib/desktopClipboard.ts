import { hasDesktopMenuBar } from "./desktopMenuCommands";

// In the Mac app WebKit puts the HTML a copy writes (clipboardData.setData)
// on the clipboard only after re-serializing it in a page of its own, with
// the computed text look inline on the outer box — white text under the dark
// theme (2026-11-16) — so a copy pasted in a light mail or document read
// white on white. Notion's app (Chromium) keeps the page's HTML as written,
// with no theme colour. So after every copy or cut this page writes itself,
// the app puts the HTML it wrote back on the clipboard
// (desktop/pasteboard_darwin.m), WebKit's other entries kept. A copy WebKit
// makes of the page's own selection (one no handler took) is left as WebKit
// makes it.

type Copied = { text: string; html: string };

/** Hears what this copy writes into its clipboard data, as it writes it. */
function hearWrites(data: DataTransfer, copied: Copied) {
  const set = data.setData.bind(data);
  const clear = data.clearData.bind(data);
  data.setData = (format: string, value: string) => {
    set(format, value);
    const type = format.toLowerCase();
    if (type === "text/html") copied.html = value;
    else if (type === "text/plain" || type === "text") copied.text = value;
  };
  data.clearData = (format?: string) => {
    clear(format);
    const type = format?.toLowerCase();
    if (!type || type === "text/html") copied.html = "";
    if (!type || type === "text/plain" || type === "text") copied.text = "";
  };
}

function keepCopiedHtml(event: ClipboardEvent) {
  const data = event.clipboardData;
  if (!data) return;
  const copied: Copied = { text: "", html: "" };
  hearWrites(data, copied);
  // WebKit writes the clipboard as the copy's handlers finish, before the next task.
  window.setTimeout(() => {
    if (!event.defaultPrevented || !copied.html) return;
    void import("@wailsio/runtime")
      .then(({ Call }) => Call.ByName("main.Pasteboard.KeepCopiedHTML", copied.text, copied.html))
      .catch(() => undefined);
  }, 0);
}

/** In the Mac app, keeps the HTML the page copies as the page wrote it. */
export function installDesktopClipboard(): void {
  if (!hasDesktopMenuBar()) return;
  // First, before any handler writes.
  window.addEventListener("copy", keepCopiedHtml, true);
  window.addEventListener("cut", keepCopiedHtml, true);
}
