/**
 * The shortcuts Leafen answers to: each one's chords, category and scope.
 * keyboardShortcuts.ts matches keys against them and writes them out.
 */
export type KeyboardShortcutPlatform = "mac" | "windows" | "linux";
export type KeyboardShortcutCategory = "popular" | "createStyle" | "editMove";
export type KeyboardShortcutScope = "global" | "page" | "editor";
export type KeyboardShortcutId =
  | "openKeyboardShortcuts"
  | "newPage"
  | "quickFind"
  | "findInPage"
  | "copyPageLink"
  | "sharePage"
  | "navigateBack"
  | "navigateForward"
  | "toggleTheme"
  | "openParentPage"
  | "movePage"
  | "renamePage"
  | "openInNewTab"
  | "toggleSidebar"
  | "openInbox"
  | "closeSidePanel"
  | "previousPeekPage"
  | "nextPeekPage"
  | "bold"
  | "italic"
  | "underline"
  | "strikethrough"
  | "inlineCode"
  | "inlineEquation"
  | "clearFormatting"
  | "addLink"
  | "addComment"
  | "suggestEdits"
  | "moveBlock"
  | "copyBlockLink"
  | "duplicateBlock"
  | "duplicateBlockWithoutContent"
  | "deleteBlock"
  | "moveBlockUp"
  | "moveBlockDown"
  | "blockColor"
  | "openBlockMenu"
  | "toggleAllToggles"
  | "presentPage"
  | "toggleCaption"
  | "imageLink"
  | "fullScreen"
  | "unsyncAllSynced"
  | "turnInto"
  | "toggleHighContrast"
  | "switchWorkspace";

export type ModifierRequirement = boolean | "any";

export type KeyboardShortcutChord = Readonly<{
  key: string;
  code?: string;
  displayKey?: string;
  primary: boolean;
  alt?: ModifierRequirement;
  shift?: ModifierRequirement;
  /** The Control key beside ⌘, which only a Mac has apart from its primary key. */
  control?: true;
  /** The primary key is Control itself, ⌃ on a Mac rather than ⌘. */
  ctrl?: true;
}>;

export type KeyboardShortcutDefinition = Readonly<{
  id: KeyboardShortcutId;
  category: KeyboardShortcutCategory;
  labelKey: `keyboardShortcuts:actions.${string}`;
  searchTermsKey?: `keyboardShortcuts:searchTerms.${string}`;
  scope: KeyboardShortcutScope;
  helpVisible: boolean;
  chords: readonly KeyboardShortcutChord[];
  /** The chords in the Mac app, whose menu bar takes the others (desktopMenuCommands.ts), and in a browser, which keeps them. */
  macAppChords?: readonly KeyboardShortcutChord[];
  browserChords?: readonly KeyboardShortcutChord[];
  /** The chords away from a Mac (Windows, Linux), where they differ beyond ⌘ being Ctrl. */
  pcChords?: readonly KeyboardShortcutChord[];
}>;

const chord = (
  key: string,
  options: Omit<KeyboardShortcutChord, "key" | "primary"> & { primary?: boolean } = {},
): KeyboardShortcutChord => ({
  key,
  primary: options.primary ?? true,
  ...options,
});

export const KEYBOARD_SHORTCUTS = [
  {
    id: "openKeyboardShortcuts",
    category: "popular",
    labelKey: "keyboardShortcuts:actions.openKeyboardShortcuts",
    scope: "global",
    helpVisible: false,
    chords: [chord("/", { code: "Slash", alt: true })],
  },
  {
    id: "newPage",
    category: "popular",
    labelKey: "keyboardShortcuts:actions.newPage",
    scope: "global",
    helpVisible: true,
    chords: [chord("n", { code: "KeyN" })],
  },
  {
    id: "quickFind",
    category: "popular",
    labelKey: "keyboardShortcuts:actions.quickFind",
    scope: "global",
    helpVisible: true,
    chords: [chord("k", { code: "KeyK" }), chord("p", { code: "KeyP" })],
  },
  {
    id: "findInPage",
    category: "popular",
    labelKey: "keyboardShortcuts:actions.findInPage",
    scope: "page",
    helpVisible: true,
    chords: [chord("f", { code: "KeyF" })],
  },
  {
    id: "copyPageLink",
    category: "popular",
    labelKey: "keyboardShortcuts:actions.copyPageLink",
    scope: "global",
    helpVisible: true,
    // Notion's page menu names ⌘⌥L (seen 2026-10-07); a browser keeps ⌘L, which Notion's web page leaves
    // to it (seen 2026-11-15 in Chrome, two tabs at two times: the clipboard stayed, ⌘⌥L copied the link).
    // In Notion's Mac app the page menu names ⌘L, and ⌥⌘L is the menu bar's "현재 페이지의 이름 복사"
    // (seen 2026-11-09); the Windows app keeps both.
    chords: [chord("l", { code: "KeyL", alt: true }), chord("l", { code: "KeyL" })],
    macAppChords: [chord("l", { code: "KeyL" })],
    browserChords: [chord("l", { code: "KeyL", alt: true })],
  },
  {
    // The share menu of the page in front, as Notion's top bar tip names it
    // (seen 2026-11-13: "공유" tip "⌘⇧O"; with a side peek open, the peek's).
    id: "sharePage",
    category: "popular",
    labelKey: "keyboardShortcuts:actions.sharePage",
    scope: "global",
    helpVisible: false,
    chords: [chord("o", { code: "KeyO", shift: true })],
  },
  {
    id: "navigateBack",
    category: "popular",
    labelKey: "keyboardShortcuts:actions.navigateBack",
    scope: "global",
    helpVisible: true,
    chords: [chord("[", { code: "BracketLeft" })],
  },
  {
    id: "navigateForward",
    category: "popular",
    labelKey: "keyboardShortcuts:actions.navigateForward",
    scope: "global",
    helpVisible: true,
    chords: [chord("]", { code: "BracketRight" })],
  },
  {
    id: "toggleTheme",
    category: "popular",
    labelKey: "keyboardShortcuts:actions.toggleTheme",
    searchTermsKey: "keyboardShortcuts:searchTerms.toggleTheme",
    scope: "global",
    helpVisible: true,
    // ⌃⇧L on a Mac too, as Notion's (toggleDarkMode, read and seen 2026-11-20); its window names ⌘⇧L.
    chords: [chord("l", { code: "KeyL", shift: true }), chord("l", { code: "KeyL", ctrl: true, shift: true })],
  },
  // ⌘⇧⌥L turns this device's high contrast on, or off where it shows
  // (contrast.ts toggleHighContrast): Notion's "고대비 모드 전환" (seen 2026-11-20),
  // and ⌃⇧⌥L on a Mac too, as Notion's (read and seen 2026-11-20).
  {
    id: "toggleHighContrast",
    category: "popular",
    labelKey: "keyboardShortcuts:actions.toggleHighContrast",
    scope: "global",
    helpVisible: false,
    chords: [chord("l", { code: "KeyL", shift: true, alt: true }), chord("l", { code: "KeyL", ctrl: true, shift: true, alt: true })],
  },
  // The desktop app's ⌃⇧1–9 open the workspace at that place in the workspace
  // menu (useWorkspaceNumberKeys.ts); away from a Mac Alt+Shift+1–9, clear of
  // turn-into's Ctrl+Shift, as Notion's (switchSpacesPostTabs, read 2026-11-20).
  {
    id: "switchWorkspace",
    category: "popular",
    labelKey: "keyboardShortcuts:actions.switchWorkspace",
    scope: "global",
    helpVisible: false,
    chords: [..."123456789"].map((digit) => chord(digit, { code: `Digit${digit}`, ctrl: true, shift: true })),
    pcChords: [..."123456789"].map((digit) => chord(digit, { code: `Digit${digit}`, primary: false, alt: true, shift: true })),
  },
  {
    id: "bold",
    category: "createStyle",
    labelKey: "keyboardShortcuts:actions.bold",
    scope: "editor",
    helpVisible: true,
    chords: [chord("b", { code: "KeyB" })],
  },
  {
    id: "italic",
    category: "createStyle",
    labelKey: "keyboardShortcuts:actions.italic",
    scope: "editor",
    helpVisible: true,
    chords: [chord("i", { code: "KeyI" })],
  },
  {
    id: "underline",
    category: "createStyle",
    labelKey: "keyboardShortcuts:actions.underline",
    scope: "editor",
    helpVisible: true,
    chords: [chord("u", { code: "KeyU" })],
  },
  {
    id: "strikethrough",
    category: "createStyle",
    labelKey: "keyboardShortcuts:actions.strikethrough",
    scope: "editor",
    helpVisible: true,
    chords: [chord("x", { code: "KeyX", shift: true }), chord("s", { code: "KeyS", shift: true })],
  },
  {
    id: "inlineCode",
    category: "createStyle",
    labelKey: "keyboardShortcuts:actions.inlineCode",
    scope: "editor",
    helpVisible: true,
    chords: [chord("e", { code: "KeyE" })],
  },
  {
    // ⌘⇧E turns the selected text into an inline equation in its editor, as Notion's (seen 2026-10-08).
    id: "inlineEquation",
    category: "createStyle",
    labelKey: "keyboardShortcuts:actions.inlineEquation",
    scope: "editor",
    helpVisible: false,
    chords: [chord("e", { code: "KeyE", shift: true })],
  },  // Notion's text menu names ⌘⌥\ for "형식 초기화" (seen 2026-10-28).
  {
    id: "clearFormatting",
    category: "createStyle",
    labelKey: "keyboardShortcuts:actions.clearFormatting",
    scope: "editor",
    helpVisible: false,
    chords: [chord("\\", { code: "Backslash", alt: true })],
  },
  // ⌘⌥0–9 turn the caret's line, the lines words are chosen across, or the
  // chosen blocks into text, heading 1–3, a to-do, a bullet, a number, a
  // toggle, code or a page, in that order (editorInputModel.ts
  // shortcutBlockType); Notion's shortcuts window names the first, "Cmd
  // Option 0" (seen 2026-11-20). Away from a Mac Ctrl+Shift+0–9, as Notion's
  // help ("cmd/ctrl + option/shift") and its code (turnIntoType: Apple
  // command+alt, else command+shift — read 2026-11-20) give them.
  {
    id: "turnInto",
    category: "createStyle",
    labelKey: "keyboardShortcuts:actions.turnInto",
    scope: "editor",
    helpVisible: false,
    chords: [..."0123456789"].map((digit) => chord(digit, { code: `Digit${digit}`, alt: true, shift: "any" })),
    pcChords: [..."0123456789"].map((digit) => chord(digit, { code: `Digit${digit}`, shift: true })),
  },

  {
    id: "addLink",
    category: "createStyle",
    labelKey: "keyboardShortcuts:actions.addLink",
    scope: "editor",
    helpVisible: true,
    chords: [chord("k", { code: "KeyK" })],
  },
  {
    id: "addComment",
    category: "createStyle",
    labelKey: "keyboardShortcuts:actions.addComment",
    scope: "editor",
    helpVisible: true,
    chords: [chord("m", { code: "KeyM", shift: true })],
  },
  // "편집 내용 제안" ⌘⇧⌥X, as Notion's block menu shows it (seen 2026-10-04).
  {
    id: "suggestEdits",
    category: "createStyle",
    labelKey: "keyboardShortcuts:actions.suggestEdits",
    scope: "editor",
    // Notion's shortcut list wasn't seen with it; the block menu shows it.
    helpVisible: false,
    chords: [chord("x", { code: "KeyX", shift: true, alt: true })],
  },
  {
    id: "openParentPage",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.openParentPage",
    scope: "global",
    helpVisible: true,
    chords: [chord("u", { code: "KeyU", shift: true })],
  },
  // Notion's menus give ⌘⌥P for presentation mode (seen 2026-10-12): from
  // the page's title, or the slide holding the caret.
  {
    id: "presentPage",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.presentPage",
    scope: "page",
    helpVisible: false,
    chords: [chord("p", { code: "KeyP", alt: true })],
  },
  {
    id: "movePage",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.movePage",
    scope: "global",
    helpVisible: true,
    chords: [chord("p", { code: "KeyP", shift: true })],
  },
  // Notion's page menus show ⌘⇧R for rename (seen 2026-10-02); it opens the
  // rename popover from a page's title.
  {
    id: "renamePage",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.renamePage",
    scope: "page",
    helpVisible: false,
    chords: [chord("r", { code: "KeyR", shift: true })],
  },
  // Notion's menus show ⌘⇧↵ for "새 탭에서 열기" (seen 2026-10-10); with a
  // menu open it opens what the menu is for in a new tab.
  {
    id: "openInNewTab",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.openInNewTab",
    scope: "page",
    helpVisible: false,
    chords: [chord("Enter", { code: "Enter", shift: true, displayKey: "↵" })],
  },
  {
    id: "toggleSidebar",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.toggleSidebar",
    scope: "global",
    helpVisible: true,
    chords: [chord("\\", { code: "Backslash" })],
  },
  // Notion's tips name these (seen 2026-10-28): "수신함 ⌘⌥U" and, over the
  // panel beside a page, "패널 닫기 ⌘⇧\".
  {
    id: "openInbox",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.openInbox",
    scope: "global",
    helpVisible: false,
    chords: [chord("u", { code: "KeyU", alt: true })],
  },
  {
    id: "closeSidePanel",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.closeSidePanel",
    scope: "page",
    helpVisible: false,
    chords: [chord("\\", { code: "Backslash", shift: true })],
  },
  // A database page's peek opens the row before or after it in its view
  // (Notion's ⌃⇧K and ⌃⇧J, seen 2026-11-01).
  {
    id: "previousPeekPage",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.previousPeekPage",
    scope: "page",
    helpVisible: false,
    chords: [chord("k", { code: "KeyK", shift: true, ctrl: true })],
  },
  {
    id: "nextPeekPage",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.nextPeekPage",
    scope: "page",
    helpVisible: false,
    chords: [chord("j", { code: "KeyJ", shift: true, ctrl: true })],
  },
  {
    id: "moveBlock",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.moveBlock",
    scope: "editor",
    helpVisible: true,
    chords: [chord("p", { code: "KeyP", shift: true })],
  },
  // Notion's block menus show these keys (seen 2026-10-01); its help gives
  // ⌘D and Delete to selected blocks. ⌘⌃L is a Mac's key; a Windows key
  // for a block's link isn't known, so there it has none.
  {
    id: "copyBlockLink",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.copyBlockLink",
    scope: "editor",
    helpVisible: false,
    chords: [chord("l", { code: "KeyL", control: true })],
  },
  {
    id: "duplicateBlock",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.duplicateBlock",
    scope: "editor",
    helpVisible: false,
    chords: [chord("d", { code: "KeyD" })],
  },
  {
    id: "duplicateBlockWithoutContent",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.duplicateBlockWithoutContent",
    scope: "editor",
    helpVisible: false,
    chords: [chord("d", { code: "KeyD", shift: true })],
  },
  {
    id: "deleteBlock",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.deleteBlock",
    scope: "editor",
    helpVisible: false,
    chords: [
      chord("Delete", { primary: false, shift: "any", displayKey: "Del" }),
      chord("Backspace", { primary: false, shift: "any" }),
    ],
  },
  {
    id: "moveBlockUp",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.moveBlockUp",
    scope: "editor",
    helpVisible: true,
    chords: [chord("ArrowUp", { shift: true })],
  },
  {
    id: "moveBlockDown",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.moveBlockDown",
    scope: "editor",
    helpVisible: true,
    chords: [chord("ArrowDown", { shift: true })],
  },
  {
    id: "blockColor",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.blockColor",
    scope: "editor",
    helpVisible: true,
    chords: [chord("h", { code: "KeyH", shift: true })],
  },
  {
    id: "openBlockMenu",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.openBlockMenu",
    scope: "editor",
    helpVisible: true,
    chords: [chord("/", { code: "Slash" })],
  },
  {
    id: "toggleAllToggles",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.toggleAllToggles",
    scope: "page",
    helpVisible: true,
    chords: [chord("t", { code: "KeyT", alt: true })],
  },
  // Notion's block menus show these beside their rows (seen 2026-10-11): the
  // caption ⌘⌥M, an image's link ⌘K, full screen Space, a synced block's
  // "동기화 모두 해제" ⌘⇧G. With the menu open, or the block selected, they do
  // the row's work (Space only with the block selected, where it doesn't
  // choose the menu's row).
  {
    id: "toggleCaption",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.toggleCaption",
    scope: "editor",
    helpVisible: false,
    chords: [chord("m", { code: "KeyM", alt: true })],
  },
  {
    id: "imageLink",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.imageLink",
    scope: "editor",
    helpVisible: false,
    chords: [chord("k", { code: "KeyK" })],
  },
  {
    id: "fullScreen",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.fullScreen",
    scope: "editor",
    helpVisible: false,
    chords: [chord(" ", { code: "Space", primary: false, displayKey: "Space" })],
  },
  {
    id: "unsyncAllSynced",
    category: "editMove",
    labelKey: "keyboardShortcuts:actions.unsyncAllSynced",
    scope: "editor",
    helpVisible: false,
    chords: [chord("g", { code: "KeyG", shift: true })],
  },
] as const satisfies readonly KeyboardShortcutDefinition[];
