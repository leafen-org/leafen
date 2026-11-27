import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Trans, useTranslation } from "react-i18next";
import { commandSearchShortcutText, shortcutKeys } from "@/lib/desktopBackground";
import { hasDesktopMenuBar, packagedWindows } from "@/lib/desktopMenuCommands";
import controls from "./SearchControls.module.css";
import styles from "./CommandSearchHint.module.css";
import { useTipPress } from "@/lib/tipTiming";

// The desktop app's command search, once asked (desktop/command_search_desktop.go):
// the next search shows its hint at once.
let known: { on: boolean; shortcut: string } | undefined;

/** Whether the desktop app's command search is on, and its key. */
export function useCommandSearch(): { on: boolean; shortcut: string } | null {
  const [state, setState] = useState(known ?? null);
  useEffect(() => {
    if (!hasDesktopMenuBar() && !packagedWindows()) return;
    let current = true;
    void import("@/lib/desktopBackground").then(({ desktopBackground }) => desktopBackground.state()).then((answer) => {
      known = { on: answer.commandSearch, shortcut: commandSearchShortcutText(answer, hasDesktopMenuBar()) };
      if (current) setState(known);
    }, () => undefined);
    return () => { current = false; };
  }, []);
  return state;
}

const TIP_DELAY_MS = 400;
const TIP_GAP = 8;
const TIP_WIDTH = 260;

/**
 * The search foot's "⇧⌘K 명령어 검색", as Notion's app's (its search foot,
 * read 2026-11-13): pointed at, the keys get a hover ground and the words
 * turn to "클릭하여 단축키 편집" (back 200 ms after the pointer leaves); a
 * press opens the key's setting. On a Mac a tip 260 wide comes 400 ms after,
 * 8 below (above where the window has no room): "어디서나 검색과 Q&A 사용"
 * and how the key and the menu bar's icon reach the search, over a picture
 * of the icon and the search's window.
 */
export function CommandSearchHint({ shortcut, onEdit }: { shortcut: string; onEdit: () => void }) {
  const { t } = useTranslation(["searchDialog"]);
  const mac = hasDesktopMenuBar();
  const keys = shortcutKeys(shortcut, mac);
  const [hovered, setHovered] = useState(false);
  const [tipAt, setTipAt] = useState<DOMRect | null>(null);
  const rootRef = useRef<HTMLSpanElement>(null);
  const timers = useRef<{ tip?: number; leave?: number }>({});
  useEffect(() => () => { window.clearTimeout(timers.current.tip); window.clearTimeout(timers.current.leave); }, []);
  const enter = () => {
    window.clearTimeout(timers.current.leave);
    setHovered(true);
    if (!mac) return;
    window.clearTimeout(timers.current.tip);
    timers.current.tip = window.setTimeout(() => setTipAt(rootRef.current?.getBoundingClientRect() ?? null), TIP_DELAY_MS);
  };
  const leave = () => {
    window.clearTimeout(timers.current.tip);
    setTipAt(null);
    timers.current.leave = window.setTimeout(() => setHovered(false), 200);
  };
  // A press takes the tip and its wait, as Notion's every tip goes as the mouse goes down (lib/tipTiming.ts).
  useTipPress(hovered, () => { window.clearTimeout(timers.current.tip); setTipAt(null); });
  const shownKeys = mac ? keys : [keys.join("+")];
  return (
    <span ref={rootRef} className={`${controls.footerHint} ${styles.hint}`} data-search-footer-command-search
      onPointerEnter={enter} onPointerLeave={leave} onClick={() => { leave(); onEdit(); }}>
      <span className={styles.keys} data-hovered={hovered || undefined}>{shownKeys.map(key => <kbd key={key}>{key}</kbd>)}</span>
      <span>{hovered ? t("searchDialog:footer.commandSearchEdit") : t("searchDialog:footer.commandSearch")}</span>
      {tipAt ? <CommandSearchTip anchor={tipAt} shortcut={keys.join("")} /> : null}
    </span>
  );
}

function CommandSearchTip({ anchor, shortcut }: { anchor: DOMRect; shortcut: string }) {
  const { t } = useTranslation(["searchDialog"]);
  const ref = useRef<HTMLDivElement>(null);
  const [top, setTop] = useState(anchor.bottom + TIP_GAP);
  useLayoutEffect(() => {
    const height = ref.current?.offsetHeight ?? 0;
    setTop(anchor.bottom + TIP_GAP + height > window.innerHeight - 4 ? Math.max(4, anchor.top - TIP_GAP - height) : anchor.bottom + TIP_GAP);
  }, [anchor]);
  const left = Math.min(Math.max(4, anchor.left + anchor.width / 2 - TIP_WIDTH / 2), window.innerWidth - TIP_WIDTH - 4);
  return createPortal(
    <div ref={ref} className={styles.tip} style={{ top, left }} role="tooltip" data-command-search-tip>
      <div className={styles.tipText}>
        <div>{t("searchDialog:footer.commandSearchTipHeader")}</div>
        <div className={styles.tipDescription}>
          <Trans t={t} i18nKey="searchDialog:footer.commandSearchTip" values={{ shortcut }}
            components={{ icon: <span className={styles.menuBarIcon} aria-hidden="true" /> }} />
        </div>
      </div>
      <CommandSearchPicture />
    </div>,
    document.body,
  );
}

/** The menu bar with the app's icon lit and the search's window under it. */
function CommandSearchPicture() {
  return (
    <svg className={styles.picture} viewBox="0 0 260 112" aria-hidden="true">
      <rect className={styles.pictureGround} width="260" height="112" />
      <rect className={styles.pictureBar} width="260" height="20" />
      <circle className={styles.pictureGlyph} cx="176" cy="10" r="3.5" />
      <circle className={styles.pictureGlyph} cx="192" cy="10" r="3.5" />
      <rect className={styles.pictureLit} x="203" y="3" width="18" height="14" rx="4" />
      <foreignObject x="205" y="4" width="14" height="12"><span className={styles.pictureMark} /></foreignObject>
      <rect className={styles.pictureGlyph} x="229" y="7" width="24" height="6" rx="3" />
      <rect className={styles.pictureWindow} x="38" y="34" width="184" height="66" rx="10" />
      <rect className={styles.pictureField} x="50" y="45" width="160" height="14" rx="4" />
      <rect className={styles.pictureRow} x="50" y="68" width="110" height="6" rx="3" />
      <rect className={styles.pictureRow} x="50" y="82" width="80" height="6" rx="3" />
    </svg>
  );
}
