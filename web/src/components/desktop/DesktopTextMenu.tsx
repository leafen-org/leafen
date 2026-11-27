import { Fragment, useRef, useState, type MouseEvent as ReactMouseEvent } from "react";
import { createRoot, type Root } from "react-dom/client";
import { i18next } from "@/i18n";
import rowMenu from "@/components/RowMenu.module.css";
import { copyImage, imageCopyNotice, imagePng } from "@/lib/copyImage";
import { copyText } from "@/lib/copyText";
import { setDesktopTextMenuUp, type TextMenuPress } from "@/lib/desktopTextMenu";
import { packagedWindows } from "@/lib/desktopMenuCommands";
import { menuKeyHint } from "@/lib/menuKeyHint";
import { ownLinkInApp } from "@/lib/pageLinks";
import { placeAround, placeAtPoint, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { appToastStack } from "@/lib/productToast";
import { serverUrl } from "@/lib/serverOrigin";
import styles from "./DesktopTabMenus.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/** A word's spelling, and on Windows whether there is anything to paste (desktop/text_menu_*.go). */
type Spelling = { checking: boolean; misspelt: boolean; guesses: string[]; paste?: boolean };
type Line = { key: string; label: string; hint?: string; run: () => void };

let host: HTMLDivElement | null = null;
let root: Root | null = null;
let opened = 0;

async function call<T>(name: string, ...args: unknown[]): Promise<T | undefined> {
  try {
    const { Call } = await import("@wailsio/runtime");
    return await Call.ByName(name, ...args) as T;
  } catch {
    return undefined;
  }
}

/**
 * Opens the menu for a right press (lib/desktopTextMenu.ts), once the app has
 * said how the word pressed is spelt; with nothing to offer none opens.
 */
export function showDesktopTextMenu(press: TextMenuPress): void {
  if (!host || !host.isConnected) {
    host = document.createElement("div");
    host.dataset.desktopTextMenuHost = "";
    host.dataset.reactAriaTopLayer = "";
    document.body.append(host);
    root = createRoot(host);
  }
  const shown = ++opened;
  root?.render(null);
  const caret = press.caretWord;
  const word = press.editable && oneWord(press.selection) ? press.selection.trim() : caret?.text ?? "";
  // The word where it stands in its text, whose language the Mac tells from all of it.
  const lead = press.selection.length - press.selection.trimStart().length;
  const text = caret && !press.selection ? caret.context : word && press.context ? press.context.text : "";
  const at = caret && !press.selection ? caret.at : word && press.context ? press.context.at + lead : 0;
  const asked = press.editable ? call<Spelling>("main.TextMenu.Spelling", text, at, word.length) : Promise.resolve(undefined);
  void asked.then((spelling) => {
    if (shown !== opened) return;
    // A misspelt word the caret stood in is chosen for the menu (Windows).
    const menuPress = caret && !press.selection && spelling?.misspelt ? caret.choose() : press;
    const sections = menuSections(menuPress, spelling ?? { checking: true, misspelt: false, guesses: [] }, word);
    if (!sections.length) { setDesktopTextMenuUp(false); return; }
    const close = () => {
      if (shown !== opened) return;
      root?.render(null);
      setDesktopTextMenuUp(false);
    };
    root?.render(<DesktopTextMenu key={shown} press={menuPress} sections={sections} onClose={close} />);
  });
}

function oneWord(text: string): boolean {
  const word = text.trim();
  return word.length > 0 && !/\s/u.test(word);
}

const say = (key: string, values?: Record<string, string>) => i18next.t(`desktopTextMenu:${key}`, values);

const openInApp = (address: string, active = true) => void call("main.WindowOpener.Open", address, active, false);

/** The page's choice back where the press left it, then the app's edit there. */
function edit(press: TextMenuPress, action: "cut" | "copy" | "paste" | "spelling") {
  press.restore();
  void call("main.TextMenu.Edit", action);
}

/** Puts words over the chosen ones as typing does, so the editor and WebKit's spelling both see it. */
function typeOver(press: TextMenuPress, text: string) {
  press.restore();
  document.execCommand("insertText", false, text);
}

/**
 * The picture pressed on the clipboard, started at the press (lib/copyImage.ts).
 * Notion's app says nothing either way here (its ElectronContextMenu, read
 * 2026-11-15); a copy that failed is said as its image block's is, or the
 * clipboard would keep what it had with no word of it.
 */
function copyPressedImage(src: string) {
  void copyImage(imagePng(src)).then((copied) => {
    if (!copied) appToastStack()?.notify(imageCopyNotice(false));
  });
}

/**
 * Notion's sections, each after a line (its ElectronContextMenu): the
 * corrections of a misspelt word; "사전에 추가" and "맞춤법 검사 비활성화";
 * a link's lines — a link to the active server, at the page's address or the
 * server's, opens in a new tab or window too, as Notion's does one to its own
 * domain, and its address is the server's — and mail's "이메일 주소 복사"; an image's;
 * "잘라내기 ⌘X", "복사 ⌘C", "붙여넣기 ⌘V" as the press allows them; the chosen
 * words' "'〈낱말〉' 검색" (one word) and "Google로 검색"; "맞춤법 검사 활성화"
 * in text one types while spelling isn't checked.
 */
function menuSections(press: TextMenuPress, spelling: Spelling, word: string): Line[][] {
  const misspelt = press.editable && spelling.checking && spelling.misspelt && word !== "";
  const chosen = press.selection.trim();
  const sections: Line[][] = [
    misspelt ? spelling.guesses.map((guess) => ({ key: `guess:${guess}`, label: guess, run: () => typeOver(press, guess) })) : [],
    misspelt ? [
      { key: "learn", label: say("addToDictionary"), run: () => { void call("main.TextMenu.Learn", word).then(() => typeOver(press, word)); } },
      { key: "disable", label: say("disableSpellcheck"), run: () => edit(press, "spelling") },
    ] : [],
  ];
  if (press.link) {
    const { href, text } = press.link;
    const own = ownLinkInApp(href);
    const onServer = own ? serverUrl(`${own.pathname}${own.search}${own.hash}`) : href;
    sections.push([
      ...(own ? [
        { key: "tab", label: say("openLinkInNewTab"), run: () => openInApp(own.href) },
        { key: "window", label: say("openLinkInNewWindow"), run: () => void call("main.WindowOpener.OpenWindow", own.href) },
      ] : []),
      { key: "browser", label: say("openLinkInBrowser"), run: () => openInApp(onServer) },
      href.startsWith("mailto:")
        ? { key: "email", label: say("copyEmailAddress"), run: () => void copyText(text || decodeURIComponent(href.slice("mailto:".length))) }
        : { key: "link", label: say("copyLink"), run: () => void copyText(onServer) },
    ]);
  }
  if (press.image) {
    const src = press.image;
    sections.push([
      { key: "image", label: say("copyImage"), run: () => copyPressedImage(src) },
      { key: "imageAddress", label: say("copyImageAddress"), run: () => void copyText(src) },
    ]);
  }
  // Windows names the keys Ctrl+ (menuKeyHint), and offers "붙여넣기" only with
  // something to paste and no look-up, as Notion's Windows app (measured 2026-11-24).
  const windows = packagedWindows();
  const key = (letter: string) => menuKeyHint(`⌘${letter}`);
  const pasting = press.editable && spelling.paste !== false;
  if (press.selection.length || pasting) {
    sections.push([
      ...(press.editable && press.selection.length ? [{ key: "cut", label: say("cut"), hint: key("X"), run: () => edit(press, "cut") }] : []),
      ...(press.selection.length ? [{ key: "copy", label: say("copy"), hint: key("C"), run: () => edit(press, "copy") }] : []),
      ...(pasting ? [{ key: "paste", label: say("paste"), hint: key("V"), run: () => requestAnimationFrame(() => edit(press, "paste")) }] : []),
    ]);
  }
  if (chosen) {
    const box = press.selectionRect;
    sections.push([
      ...(oneWord(chosen) && !windows ? [{ key: "lookUp", label: say("lookUp", { selection: chosen }), run: () => {
        press.restore();
        void call("main.TextMenu.LookUp", chosen, box?.left ?? press.x, (box?.bottom ?? press.y) - press.fontSize * 0.25, press.fontSize);
      } }] : []),
      { key: "google", label: say("searchWithGoogle"), run: () => openInApp(`https://www.google.com/search?q=${encodeURIComponent(chosen)}`) },
    ]);
  }
  if (press.editable && !spelling.checking) {
    sections.push([{ key: "enable", label: say("enableSpellcheck"), run: () => edit(press, "spelling") }]);
  }
  return sections.filter((lines) => lines.length > 0);
}

/**
 * The menu, as Notion's page draws it: a popup menu 4px under the chosen
 * words, from their start, or at the press with none chosen — on Windows
 * always at the press, as Notion's Windows app's; its sections
 * apart by a line, no line lit until pointed at or stepped to. Its lines
 * take no focus, so the choice and the caret stay in the page; ↑ ↓ step,
 * Enter acts, Escape or a press beside it closes it.
 */
function DesktopTextMenu({ press, sections, onClose }: { press: TextMenuPress; sections: Line[][]; onClose(): void }) {
  const menuRef = useRef<HTMLDivElement>(null);
  const lines = sections.flat();
  const [lit, setLit] = useState(-1);
  const choose = (line: Line) => {
    onClose();
    line.run();
  };
  // On Windows at the press, as Notion's Windows app's menu (measured 2026-11-24).
  const place = usePopoverPlacement(menuRef, (size, viewport) => press.selectionRect && !packagedWindows()
    ? placeAround(press.selectionRect, size, viewport, { gap: 4, prefer: "below", align: "start" })
    : placeAtPoint({ x: press.x, y: press.y }, size, viewport), [press], undefined, {
    kind: "menu", restoreFocus: false, onEscape: () => { onClose(); press.restore(); },
    onOutsidePress: onClose,
    onCaretKeyDown: (event) => {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        event.stopPropagation();
        const step = event.key === "ArrowDown" ? 1 : -1;
        setLit((now) => (now < 0 ? (step > 0 ? 0 : lines.length - 1) : (now + step + lines.length) % lines.length));
      } else if (fieldEnter(event) && lit >= 0 && lines[lit]) {
        event.preventDefault();
        event.stopPropagation();
        choose(lines[lit]);
      }
    },
  });
  const keep = (event: ReactMouseEvent) => event.preventDefault();
  let index = 0;
  return (
    <div ref={menuRef} role="menu" aria-label={say("label")} className={`${rowMenu.menu} ${rowMenu.contextMenu} ${styles.textMenu}`}
      style={popoverPlacementStyle(place)} data-desktop-text-menu onMouseDown={keep} onContextMenu={keep}
      onMouseLeave={() => setLit(-1)}>
      {sections.map((section, at) => (
        <Fragment key={section[0].key}>
          {at > 0 ? <div className={rowMenu.divider} role="separator" /> : null}
          {section.map((line) => {
            const mine = index++;
            return (
              <button key={line.key} type="button" role="menuitem" tabIndex={-1} className={`${rowMenu.item} ${styles.menuRow}`}
                data-text-menu-line={line.key} data-lit={mine === lit ? "true" : undefined}
                onMouseMove={() => setLit(mine)} onClick={() => choose(line)}>
                <span className={styles.label}>{line.label}</span>
                {line.hint ? <span className={rowMenu.itemHint}>{line.hint}</span> : null}
              </button>
            );
          })}
        </Fragment>
      ))}
    </div>
  );
}
