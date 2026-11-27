import type { MouseEvent as ReactMouseEvent } from "react";
import { hasDesktopTabs, onDesktopWindowEvent, packagedWindows } from "./desktopMenuCommands";

// A right press in the Mac or Windows app's page opens the page's own short
// menu, as Notion's app's page draws its ElectronContextMenu (its desktop
// client, read 2026-11-13; its Windows app measured 2026-11-24) where its
// own screens take no right press: the menu a press in a block, a row or the
// sidebar opens stays theirs, and the web view's whole menu (WebKit's
// 찾아보기·번역·서체·말하기·공유·서비스 …, WebView2's 이모지·쓰기 방향·기타
// 도구·검사) never shows. What it offers comes from the press (here) and
// from the app (desktop/text_menu_darwin.go, text_menu_windows.go: spelling,
// cut, copy, paste, and on a Mac look-up); the menu is
// components/desktop/DesktopTextMenu.tsx. A page's frame or a site in a
// pop-up keeps the app's own short menu on a Mac (text_menu_darwin.m).

/** What a right press was on, read as it came: Electron's context-menu params, for this page. */
export type TextMenuPress = {
  x: number;
  y: number;
  editable: boolean;
  /** The words chosen at the press (the press chooses the word under it, as the Mac does). */
  selection: string;
  /** The text the chosen words are in, and where they start in it: spelling is checked there, as WebKit's underline is. */
  context: { text: string; at: number } | null;
  /** Where those words are, in a page's text (not a field's). */
  selectionRect: { left: number; right: number; top: number; bottom: number } | null;
  fontSize: number;
  link: { href: string; text: string } | null;
  image: string | null;
  /** The choice at the press, put back before the menu's line acts. */
  restore: () => void;
  /**
   * On Windows, where a right press only moves the caret, the word the caret
   * stands in when nothing is chosen: chosen once its spelling turns out
   * wrong, as Chromium chooses a misspelt word for its menu (Notion's Windows
   * app, measured 2026-11-24: "윈도" chosen, its corrections over 잘라내기·복사).
   */
  caretWord?: { text: string; at: number; context: string; choose: () => TextMenuPress } | null;
};

// While the menu is up, or coming for a right press, the words it chose
// draw no text toolbar (NativeTiptapSelectionToolbar), as Notion's app's
// (seen 2026-11-16: a right press on a word, its menu and no toolbar).
let menuUp = false;
const menuWatchers = new Set<() => void>();

/** Whether the Mac app's text menu is up, or coming for a right press. */
export function desktopTextMenuUp(): boolean {
  return menuUp;
}

/** Says whether the text menu is up; it is told by DesktopTextMenu.tsx as it closes or opens nothing. */
export function setDesktopTextMenuUp(up: boolean): void {
  if (menuUp === up) return;
  menuUp = up;
  for (const watcher of menuWatchers) watcher();
}

/** Calls `watch` whenever the text menu comes up or goes; returns the way to stop. */
export function watchDesktopTextMenu(watch: () => void): () => void {
  menuWatchers.add(watch);
  return () => { menuWatchers.delete(watch); };
}

const TEXT_FIELD_TYPES = new Set(["", "text", "search", "url", "tel", "email", "password", "number"]);

function textField(target: Element): HTMLInputElement | HTMLTextAreaElement | null {
  const field = target.closest("input, textarea");
  if (field instanceof HTMLTextAreaElement) return field;
  if (field instanceof HTMLInputElement && TEXT_FIELD_TYPES.has(field.type.toLowerCase())) return field;
  return null;
}

function readPress(event: MouseEvent): TextMenuPress | null {
  const target = event.target instanceof Element ? event.target : event.target instanceof Node ? event.target.parentElement : null;
  if (!target) return null;
  const field = textField(target);
  const anchor = target.closest("a[href]");
  const image = target.closest("img");
  const link = anchor instanceof HTMLAnchorElement && anchor.href
    ? { href: anchor.href, text: (anchor.textContent ?? "").trim() } : null;
  const fontSize = Number.parseFloat(getComputedStyle(field ?? target).fontSize) || 16;
  const base = { x: event.clientX, y: event.clientY, fontSize, link, image: image instanceof HTMLImageElement ? image.currentSrc || image.src || null : null };
  if (field) {
    const start = field.selectionStart ?? 0, end = field.selectionEnd ?? start;
    const editable = !field.readOnly && !field.disabled;
    const fieldPress = (from: number, to: number): TextMenuPress => ({ ...base, editable, selection: field.value.slice(from, to),
      context: { text: field.value, at: from }, selectionRect: null,
      restore: () => { if (document.activeElement !== field) field.focus({ preventScroll: true }); field.setSelectionRange(from, to); } });
    const press = fieldPress(start, end);
    const around = editable && start === end && packagedWindows() ? wordAround(field.value, start) : null;
    return around ? { ...press, caretWord: { ...around, context: field.value, choose: () => fieldPress(around.at, around.at + around.text.length) } } : press;
  }
  const chosen = window.getSelection();
  const range = chosen && chosen.rangeCount ? chosen.getRangeAt(0).cloneRange() : null;
  const selection = chosen ? String(chosen) : "";
  const editable = target instanceof HTMLElement ? target.isContentEditable : target.closest("[contenteditable=true]") !== null;
  const context = range && range.startContainer === range.endContainer && range.startContainer instanceof Text
    ? { text: range.startContainer.data, at: range.startOffset } : null;
  const pagePress = (chosen: Range | null, words: string, at: typeof context): TextMenuPress => {
    const rect = chosen && words ? chosen.getBoundingClientRect() : null;
    return { ...base, editable, selection: words, context: at, selectionRect: rect && rect.width + rect.height > 0
      ? { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom } : null,
    restore: () => {
      const now = window.getSelection();
      if (!chosen || !now) return;
      const same = now.rangeCount && now.getRangeAt(0).compareBoundaryPoints(Range.START_TO_START, chosen) === 0
        && now.getRangeAt(0).compareBoundaryPoints(Range.END_TO_END, chosen) === 0;
      if (!same) { now.removeAllRanges(); now.addRange(chosen); }
    } };
  };
  const press = pagePress(range, selection, context);
  const node = range?.startContainer;
  const around = editable && !selection && context && node instanceof Text && packagedWindows() ? wordAround(context.text, context.at) : null;
  if (!around || !(node instanceof Text)) return press;
  return { ...press, caretWord: { ...around, context: context!.text, choose: () => {
    const word = document.createRange();
    word.setStart(node, around.at);
    word.setEnd(node, around.at + around.text.length);
    const chosen = pagePress(word, around.text, { text: context!.text, at: around.at });
    chosen.restore();
    return chosen;
  } } };
}

/** The word a caret at `at` stands in or at the end of, by the language's own word breaks. */
function wordAround(text: string, at: number): { text: string; at: number } | null {
  if (typeof Intl === "undefined" || !("Segmenter" in Intl)) return null;
  for (const segment of new Intl.Segmenter(undefined, { granularity: "word" }).segment(text)) {
    if (!segment.isWordLike) continue;
    const end = segment.index + segment.segment.length;
    if (at >= segment.index && at <= end) return { text: segment.segment, at: segment.index };
  }
  return null;
}

function giveTextMenu(event: MouseEvent): void {
  if (event.defaultPrevented) return;
  event.preventDefault();
  setDesktopTextMenuUp(true);
  // What the press chose is read in the next task, once the press's own
  // work is done, not as the page hears it: a real right press on a word
  // once drew cut, copy and paste with no "검색" lines for the word it chose
  // (2026-11-16). Notion's app's menu has the word the press chose.
  window.setTimeout(() => openTextMenu(event), 0);
}

function openTextMenu(event: MouseEvent): void {
  const press = readPress(event);
  if (!press) { setDesktopTextMenuUp(false); return; }
  void import("@/components/desktop/DesktopTextMenu").then(({ showDesktopTextMenu }) => showDesktopTextMenu(press), () => setDesktopTextMenuUp(false));
}

/** In the Mac or Windows app, gives a right press no screen took the page's own short menu. */
export function installDesktopTextMenu(): void {
  if (!hasDesktopTabs()) return;
  if (packagedWindows()) followSpellcheck();
  // Last, after every screen's own right press: one that opened its own menu
  // has prevented the press's default.
  window.addEventListener("contextmenu", giveTextMenu);
}

/**
 * A right press a screen keeps from the screens it is drawn in (an image's
 * preview from its block's menu, a card's title from its card's), which still
 * gives the Mac app's short menu: that listens beyond the page, and a press
 * kept from it showed WebKit's whole menu instead (2026-11-15: no image copy
 * over an image's preview, which Notion's app offers there).
 */
export function keepRightPress(event: ReactMouseEvent<HTMLElement>): void {
  event.stopPropagation();
  if (hasDesktopTabs()) giveTextMenu(event.nativeEvent);
}

/**
 * Spelling checked as one types in the Windows app's pages, as the menu's
 * "맞춤법 검사 비활성화" leaves it in every tab (desktop/text_menu_windows.go):
 * the page's own red underlines come and go with it.
 */
function followSpellcheck(): void {
  const apply = (on: boolean) => { document.documentElement.spellcheck = on; };
  onDesktopWindowEvent("leafen:spellcheck", (data) => {
    if (data && typeof data === "object") apply((data as { on?: unknown }).on !== false);
  });
  void import("@wailsio/runtime")
    .then(({ Call }) => Call.ByName("main.TextMenu.Spelling", "", 0, 0))
    .then((answer) => { if (answer && typeof answer === "object") apply((answer as { checking?: unknown }).checking !== false); })
    .catch(() => undefined);
}
