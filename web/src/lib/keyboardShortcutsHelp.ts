import { shortcutDisplayKeys, type KeyboardShortcutId, type KeyboardShortcutPlatform } from "./keyboardShortcuts";

/**
 * What the keyboard shortcuts window lists, in Notion's order (read
 * 2026-11-19 in Chrome 1440, ko and en): its sections — the tabs along its
 * top —, their groups and rows. A row that is one of Leafen's shortcuts shows
 * that shortcut's own keys (keyboardShortcuts.ts); the rest show what Notion
 * writes: keys the editor reads as Notion's do ("Enter", "Shift Tab"), the
 * Markdown typed before a space, and the "@", "+", "[[" and "/" commands.
 */
export type ShortcutsHelpSectionId = "popular" | "createStyle" | "editMove" | "markdown" | "commands" | "slash";

/** "mod" is ⌘ on a Mac and Ctrl elsewhere, "alt" Option or Alt; any other word is written as it is. */
type Keys = readonly string[];
export type ShortcutsHelpRow = Readonly<{ label: string; shortcut?: KeyboardShortcutId; chord?: number; keys?: Keys; caps?: Keys }>;
export type ShortcutsHelpGroup = Readonly<{ id?: string; rows: readonly ShortcutsHelpRow[] }>;
export type ShortcutsHelpSection = Readonly<{ id: ShortcutsHelpSectionId; groups: readonly ShortcutsHelpGroup[] }>;

const row = (label: string, shortcut: KeyboardShortcutId, chord = 0): ShortcutsHelpRow => ({ label, shortcut, chord });
const keys = (label: string, ...pressed: string[]): ShortcutsHelpRow => ({ label, keys: pressed });
const caps = (label: string, ...written: string[]): ShortcutsHelpRow => ({ label, caps: written });

export const SHORTCUTS_HELP: readonly ShortcutsHelpSection[] = [
  { id: "popular", groups: [{ rows: [
    row("findInPage", "findInPage"), row("quickFind", "quickFind", 1), row("addLink", "addLink"),
    row("copyPageLink", "copyPageLink"), row("navigateBack", "navigateBack"), row("navigateForward", "navigateForward"),
    row("peekAbove", "previousPeekPage"), row("peekBelow", "nextPeekPage"), row("toggleTheme", "toggleTheme"),
    row("toggleHighContrast", "toggleHighContrast"),
  ] }] },
  { id: "createStyle", groups: [{ rows: [
    keys("insertLine", "Enter"), keys("lineBreak", "Shift", "Enter"), row("addComment", "addComment"),
    row("bold", "bold"), row("italic", "italic"), row("underline", "underline"), row("strikethrough", "strikethrough"),
    row("addLink", "addLink"), row("inlineCode", "inlineCode"), row("clearFormatting", "clearFormatting"),
    keys("indent", "Tab"), keys("unindent", "Shift", "Tab"), row("turnInto", "turnInto"), keys("zoomIn", "mod", "="), keys("zoomOut", "mod", "-"),
    row("openParentPage", "openParentPage"), row("duplicateBlock", "duplicateBlock"),
  ] }] },
  { id: "editMove", groups: [{ rows: [
    keys("selectBlock", "Esc"), keys("selectWithCursor", "mod", "A"), keys("insertSpace", "Space"),
    keys("cursorLeft", "Left"), keys("cursorRight", "Right"), keys("cursorUp", "Up"), keys("cursorDown", "Down"),
    keys("deleteBlocks", "Backspace"), keys("deleteContent", "Delete"), row("duplicateBlock", "duplicateBlock"),
    keys("insertLine", "Enter"), row("editBlock", "openBlockMenu"), row("moveBlockUp", "moveBlockUp"),
    row("moveBlockDown", "moveBlockDown"), row("toggleAllToggles", "toggleAllToggles"), row("highlight", "blockColor"),
  ] }] },
  { id: "markdown", groups: [{ rows: [
    caps("bold", "**text**"), caps("italic", "*text*"), caps("code", "`code`"), caps("strikethrough", "~text~"),
    caps("heading1", "#", "Space"), caps("heading2", "##", "Space"), caps("heading3", "###", "Space"),
    caps("bulleted", "- / * /", "/ •", "Space"), caps("numbered", "1.", "Space"), caps("todo", "[]", "Space"),
    caps("toggle", ">", "Space"), caps("quote", "\"", "Space"),
  ] }] },
  { id: "commands", groups: [
    { id: "at", rows: [caps("person", "@person"), caps("page", "@page"), caps("date", "@date"), caps("remind", "@remind")] },
    { id: "plus", rows: [caps("subPage", "+sub-page name"), caps("newPage", "+new page name"), caps("pageLink", "+page name")] },
    { id: "bracket", rows: [caps("pageLink", "[[page name"), caps("subPage", "[[sub-page name"), caps("newPage", "[[new page name")] },
  ] },
  { id: "slash", groups: [
    { id: "basic", rows: [caps("text", "/text"), caps("page", "/page"), caps("bulleted", "/bullet"), caps("numbered", "/num"),
      caps("todo", "/todo"), caps("toggle", "/toggle"), caps("divider", "/div"), caps("quote", "/quote"), caps("heading1", "/h1"),
      caps("heading2", "/h2"), caps("heading3", "/h3"), caps("pageLink", "/link")] },
    { id: "inline", rows: [caps("mention", "/mention"), caps("date", "/date"), caps("equation", "/equation"), caps("emoji", "/emoji")] },
    { id: "media", rows: [caps("image", "/image"), caps("pdf", "/pdf"), caps("bookmark", "/book"), caps("video", "/video"),
      caps("audio", "/audio"), caps("code", "/code"), caps("file", "/file"), caps("embed", "/embed")] },
    { id: "advanced", rows: [caps("duplicate", "/duplicate"), caps("moveTo", "/moveto"), caps("delete", "/delete"), caps("toc", "/toc"),
      caps("button", "/button"), caps("breadcrumb", "/bread"), caps("math", "/math")] },
  ] },
];

/**
 * A key as the window writes it, a word on a Mac too: "Cmd", "Option",
 * "Shift", "Ctrl", "Enter", "Delete" (Notion's, seen 2026-11-19).
 */
function word(key: string, platform: KeyboardShortcutPlatform) {
  const mac = platform === "mac";
  if (key === "mod") return mac ? "Cmd" : "Ctrl";
  if (key === "alt") return mac ? "Option" : "Alt";
  if (key === "Control") return "Ctrl";
  if (key === "↵") return "Enter";
  if (key === "Del") return "Delete";
  return key;
}

/** A row's key caps: each key of its shortcut, or each piece Notion writes. */
export function shortcutsHelpCaps(item: ShortcutsHelpRow, platform: KeyboardShortcutPlatform): readonly string[] {
  if (item.caps) return item.caps;
  const pressed = item.shortcut ? shortcutDisplayKeys(item.shortcut, platform)[item.chord ?? 0] ?? [] : item.keys ?? [];
  return pressed.map((key) => word(key, platform));
}
