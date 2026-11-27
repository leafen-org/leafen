import { isPackagedClient } from "./packagedClient";

// The desktop app keeping on behind its last window once it is closed, as
// Notion's app (desktop/background_desktop.go), and while this computer
// serves: in the menu bar or tray, opening at login, keeping the computer
// awake when allowed; and the app's command search,
// ⇧⌘K (Ctrl+Shift+K) from anywhere or the key chosen, written as Notion
// writes it ("shift+command+k", none for the default), and on a Mac its
// menu bar icon (desktop/command_search_desktop.go).

export type DesktopBackgroundSettings = { keepRunning: boolean; openAtLogin: boolean; keepAwake: boolean; awakeOnBattery: boolean;
  commandSearch: boolean; commandSearchShortcut: string; menuBarIcon: boolean };

/** The refusal of a key the system gave another app (command_search_desktop.go). */
export const COMMAND_SEARCH_KEY_TAKEN = "command search key taken";

/** The command search's key, the default when none is chosen. */
export function commandSearchShortcutText(settings: Pick<DesktopBackgroundSettings, "commandSearchShortcut">, mac: boolean): string {
  return settings.commandSearchShortcut || (mac ? "shift+command+k" : "shift+ctrl+k");
}

const SYMBOLS: Record<string, string> = { control: "⌃", ctrl: "⌃", option: "⌥", alt: "⌥", shift: "⇧", command: "⌘", meta: "⌘" };
const WORDS: Record<string, string> = { control: "Ctrl", ctrl: "Ctrl", option: "Alt", alt: "Alt", shift: "Shift", command: "Win", meta: "Win" };
const ORDER = ["control", "ctrl", "option", "alt", "shift", "command", "meta"];
const KEY_NAMES: Record<string, string> = { space: "Space", return: "↵", backspace: "⌫", delete: "⌦", left: "←", right: "→", up: "↑", down: "↓" };

/**
 * A written key as it shows: on a Mac its symbols in the Mac's order (⌃⌥⇧⌘K),
 * elsewhere its words (Ctrl+Shift+K), as Notion's setting and search foot show it.
 */
export function shortcutKeys(text: string, mac: boolean): string[] {
  const parts = text.toLowerCase().split("+").filter(Boolean);
  const modifiers = parts.filter((part) => ORDER.includes(part)).sort((a, b) => ORDER.indexOf(a) - ORDER.indexOf(b));
  const key = parts.find((part) => !ORDER.includes(part)) ?? "";
  const keyName = KEY_NAMES[key] ?? (key.length === 1 ? key.toUpperCase() : key.toUpperCase());
  return [...modifiers.map((part) => (mac ? SYMBOLS : WORDS)[part]), keyName];
}

export type DesktopBackgroundState = DesktopBackgroundSettings & {
  serving: boolean;
  awake: boolean;
  onBattery: boolean;
  loginItem: "on" | "off" | "needs-approval" | "unavailable";
};

/** Whether this window is the desktop app's, which can stay behind it. */
export function desktopBackgroundAvailable(): boolean {
  return isPackagedClient() && !/Android|iPhone|iPad/u.test(navigator.userAgent);
}

async function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const { Call } = await import("@wailsio/runtime");
  return await Call.ByName(`main.Background.${method}`, ...args) as T;
}

export const desktopBackground = {
  state: () => call<DesktopBackgroundState>("State"),
  set: (settings: DesktopBackgroundSettings) => call<DesktopBackgroundState>("Set", settings),
};

/** Tells the desktop app the window's language, now and as it changes, for the words in its menu bar or tray. */
export function followLanguageOnDevice(): void {
  if (!desktopBackgroundAvailable()) return;
  let told = "";
  const tell = () => {
    const language = document.documentElement.lang;
    if (!language || language === told) return;
    told = language;
    void call("SetLanguage", language).catch(() => undefined);
  };
  tell();
  new MutationObserver(tell).observe(document.documentElement, { attributes: true, attributeFilter: ["lang"] });
}
