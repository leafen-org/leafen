// The one way the screens copy text (2026-10-26: buttons that called the
// browser's clipboard themselves copied nothing over plain http on the local
// network, where it is missing, or in a window that refuses it). The
// browser's clipboard first, then a hidden text box, then in the desktop app
// its own clipboard; where none copies, a small card shows the text selected
// to copy by hand, and the copy counts as done, as Notion's link copy then
// still says it copied (seen 2026-11-01).
export async function copyText(text: string): Promise<boolean> {
  if (!text) return false;
  // Where the card's closing gives the focus back: the button pressed, before
  // a wait leaves it disabled.
  const opener = typeof document !== "undefined" && document.activeElement instanceof HTMLElement
    && document.activeElement !== document.body ? document.activeElement : null;
  if (await writeText(text)) return true;
  if (await writeAppClipboard(text)) return true;
  if (typeof document === "undefined") return false;
  try {
    const { showTextToCopy } = await import("@/components/CopyByHand");
    showTextToCopy(text, opener);
    return true;
  } catch {
    return false;
  }
}

// The desktop app copies through its own clipboard where WebKit refuses the
// page's: a copy its menu bar or a tab's menu asks for ("현재 페이지 링크 복사"
// clicked there, a tab's "링크 복사") comes with no press in the page, and
// Notion's app then copies as from a key (2026-11-13: the card showed under
// "복사됨"). Loaded only then, as the card is.
async function writeAppClipboard(text: string): Promise<boolean> {
  try {
    const { packagedComputer } = await import("./desktopMenuCommands");
    if (!packagedComputer()) return false;
    const { Clipboard } = await import("@wailsio/runtime");
    await Clipboard.SetText(text);
    return true;
  } catch {
    return false;
  }
}

async function writeText(text: string): Promise<boolean> {
  if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Fall through to the textarea path below.
    }
  }

  if (typeof document === "undefined") return false;
  // A page without the keys goes on to the desktop app's clipboard: the box's
  // focus would take them — WebKit makes a page's web view the window's
  // first responder as its box is chosen — and its copy is refused anyway,
  // with no press of its own (2026-11-24, the Mac app: a tab's "링크 복사"
  // left the keys in the shown page, not on the tab, as Notion's).
  if (typeof document.hasFocus === "function" && !document.hasFocus()) return false;
  const previousFocus = document.activeElement instanceof HTMLElement
    ? document.activeElement
    : null;
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "true");
  textarea.style.position = "fixed";
  textarea.style.top = "0";
  textarea.style.left = "-9999px";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();

  try {
    return document.execCommand("copy");
  } catch {
    return false;
  } finally {
    document.body.removeChild(textarea);
    previousFocus?.focus({ preventScroll: true });
  }
}
