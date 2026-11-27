import { useRef, useState, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import { shortcutKeys } from "@/lib/desktopBackground";
import { KEYBOARD_SHORTCUTS, matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";
import styles from "./CommandSearchShortcutField.module.css";

// The command search's key field in 설정 › 이 컴퓨터, as Notion's app's
// "명령어 검색 단축키" (its settings source, read 2026-11-13): a field that
// takes the keys pressed in it — focused it empties, the keys show as they
// are held, and letting go of them all keeps them; Esc leaves it as it was.
// A key needs Option, Control or Command on a Mac (Control or Alt on
// Windows), and one the app already uses is refused, with why under it.

const KEY_NAMES: Record<string, string> = {
  ArrowDown: "down", ArrowLeft: "left", ArrowRight: "right", ArrowUp: "up", Backquote: "`", Backslash: "\\",
  Backspace: "backspace", BracketLeft: "[", BracketRight: "]", Comma: ",", Delete: "delete", Enter: "return",
  Equal: "=", Minus: "-", Period: ".", Quote: "'", Semicolon: ";", Slash: "/", Space: "space",
};

// The app's own keys for its tabs, which a key for the whole computer would take (Notion's list).
const TAB_KEYS_MAC = ["control+shift+tab", "control+tab", "option+command+left", "option+command+right",
  "shift+command+[", "shift+command+]", ...Array.from({ length: 9 }, (_, n) => `command+${n + 1}`)];
const TAB_KEYS_WINDOWS = ["shift+ctrl+tab", "ctrl+tab", ...Array.from({ length: 9 }, (_, n) => `ctrl+${n + 1}`)];

type Held = { shift?: boolean; control?: boolean; option?: boolean; command?: boolean; key?: string };

function heldFrom(codes: ReadonlySet<string>): Held {
  const held: Held = {};
  for (const code of codes) {
    if (code.startsWith("Meta")) held.command = true;
    else if (code.startsWith("Alt")) held.option = true;
    else if (code.startsWith("Shift")) held.shift = true;
    else if (code.startsWith("Control")) held.control = true;
    else if (code.startsWith("Key")) held.key = code.slice(3).toLowerCase();
    else if (code.startsWith("Digit")) held.key = code.slice(5);
    else if (/^F\d{1,2}$/u.test(code)) held.key = code.toLowerCase();
    else if (KEY_NAMES[code]) held.key = KEY_NAMES[code];
  }
  return held;
}

function written(held: Held, mac: boolean): string {
  const parts: string[] = [];
  if (held.shift) parts.push("shift");
  if (held.control) parts.push(mac ? "control" : "ctrl");
  if (held.option) parts.push(mac ? "option" : "alt");
  if (held.command) parts.push("command");
  if (held.key) parts.push(held.key);
  return parts.join("+");
}

function sameKey(a: string, b: string): boolean {
  const norm = (text: string) => text.toLowerCase().replace(/\bctrl\b/gu, "control").replace(/\balt\b/gu, "option")
    .split("+").sort().join("+");
  return norm(a) === norm(b);
}

/** Whether the app already uses the key: one of its keys for every screen, or for its tabs. */
function usedByApp(held: Held, mac: boolean): boolean {
  const text = written(held, mac);
  if ((mac ? TAB_KEYS_MAC : TAB_KEYS_WINDOWS).some((used) => sameKey(used, text))) return true;
  const key = held.key ?? "";
  const event = { key: key.length === 1 ? key : "", code: "", metaKey: Boolean(held.command), ctrlKey: Boolean(held.control),
    altKey: Boolean(held.option), shiftKey: Boolean(held.shift), defaultPrevented: false, repeat: false, isComposing: false };
  return KEYBOARD_SHORTCUTS.some((shortcut) => shortcut.scope === "global" && matchesKeyboardShortcut(shortcut.id, event));
}

export function CommandSearchShortcutField({ value, mac, refused, onChange }: {
  value: string;
  mac: boolean;
  /** The key the system refused, given to another app (shown as Notion's "이미 사용 중"). */
  refused: string;
  onChange: (text: string) => void;
}) {
  const { t } = useTranslation("desktopBackground");
  const [shown, setShown] = useState<string | null>(null);
  const [error, setError] = useState("");
  const held = useRef(new Set<string>());
  // The keys as last pressed together, kept once they are all let go.
  const combo = useRef<Held | null>(null);
  const display = (text: string) => shortcutKeys(text, mac).join(mac ? "" : "+");

  const commit = () => {
    const keys = combo.current;
    combo.current = null;
    held.current = new Set();
    setShown(null);
    if (!keys?.key) return;
    if (!(keys.control || keys.option || (mac && keys.command))) {
      setError(t(mac ? "shortcutModifierMac" : "shortcutModifierWindows"));
      return;
    }
    const text = written(keys, mac);
    if (usedByApp(keys, mac)) {
      setError(t("shortcutTaken", { shortcut: display(text) }));
      return;
    }
    setError("");
    if (!sameKey(text, value)) onChange(text);
  };

  const keyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Escape") {
      combo.current = null;
      event.currentTarget.blur();
      return;
    }
    if (event.key === "Tab") return;
    event.preventDefault();
    event.stopPropagation();
    if (!mac && event.code.startsWith("Meta")) return;
    held.current.add(event.code);
    combo.current = heldFrom(held.current);
    setShown(written(combo.current, mac));
    setError("");
  };

  const keyUp = (event: KeyboardEvent<HTMLInputElement>) => {
    held.current.delete(event.code);
    // ⌘ held swallows the other keys' letting go on a Mac.
    if (event.code.startsWith("Meta")) {
      for (const code of [...held.current]) if (!/^(Shift|Alt|Control)/u.test(code)) held.current.delete(code);
    }
    if (held.current.size === 0 && combo.current) commit();
  };

  const errorText = error || (refused ? t("shortcutTaken", { shortcut: display(refused) }) : "");
  return (
    <div className={styles.field}>
      <input className={styles.input} readOnly value={shown === null ? display(value) : shown ? display(shown) : ""}
        aria-label={t("commandSearchShortcut")} data-command-search-shortcut
        onFocus={() => { setShown(""); setError(""); }}
        onBlur={() => { if (combo.current) commit(); else { setShown(null); held.current = new Set(); } }}
        onKeyDown={keyDown} onKeyUp={keyUp} />
      {errorText ? <div className={styles.error} role="alert">{errorText}</div> : null}
    </div>
  );
}
