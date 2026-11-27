import i18next from "i18next";
import { type RefObject, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { GlobeIcon, LockIcon, PhoneShareIcon, SharePeopleIcon, SharePublishedIcon, ShareWorkspaceIcon } from "@/icons/leafen";
import { blockMenuKeepsShareKey, shareKeyOpensShare } from "@/components/editor/blockMenuShareKey";
import { matchesKeyboardShortcut, shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import { pressByShortcut } from "@/lib/lastPress";
import type { OverlayKind } from "@/lib/overlayKinds";
import { topLayerKind } from "@/lib/overlayLayer";
import type { PageReach } from "@/lib/usePageOnlyReader";

/**
 * The "공유" button's face, as Notion's in the top bar and a peek alike
 * (seen 2026-10-28, 2026-11-05, 2026-11-08): a lock on a page only its reader opens, people on one
 * others share, a building on one everyone in its workspace opens, a globe on
 * one with a web link (with the workspace's too), a browser window on one
 * published as a site, before the word; a phone's is the share mark alone. Not yet
 * known (a silent server), it says neither. Its button opens with ⌘⇧O (useShareShortcut).
 */
export function TopBarShareFace({ reach, phone = false }: { reach: PageReach | undefined; phone?: boolean }) {
  const { t } = useTranslation("topBar");
  const word = useRef<HTMLSpanElement>(null);
  useShareShortcut(word, !phone);
  return (
    <>
      {phone ? <PhoneShareIcon size="regular" aria-hidden="true" />
        // Its mark comes with the reach and widens the button, so the first
        // screen waits for it (FirstScreenCover).
        : reach === undefined ? <span hidden data-first-screen-pending="" />
        : reach === "private" ? <LockIcon size="regular" aria-hidden="true" />
        : reach === "site" ? <SharePublishedIcon size="regular" aria-hidden="true" />
        : reach === "web" ? <GlobeIcon size="regular" aria-hidden="true" />
        : reach === "workspace" ? <ShareWorkspaceIcon size="regular" aria-hidden="true" />
        : <SharePeopleIcon size="regular" aria-hidden="true" />}
      <span ref={word}>{t("share")}</span>
    </>
  );
}

/**
 * The "공유" button's tip, as Notion's on a page's bar and a peek's alike
 * (seen 2026-11-13): who the page reaches — "본인만 접근할 수 있음",
 * "특정 사용자와 공유됨", the workspace's members, those with its link, the
 * web it is published to — over ⌘⇧O; before the server says, what the button
 * does. The button has words of its own, so the tip is named apart from a
 * title (iconTips.ts `data-icon-tip`).
 */
export function shareTip(reach: PageReach | undefined) {
  return {
    "data-icon-tip": i18next.t(`topBar:shareTip.${reach ?? "unknown"}`),
    "data-tip-keys": shortcutCompactLabel("sharePage"),
  };
}

type ShareButton = { button: () => HTMLButtonElement | null; peek: boolean };
const shareButtons: ShareButton[] = [];
let listening = false;
// The kind of the window or menu on top as ⌘⇧O presses "공유" (topLayerKind), while it does.
let pressingOver: OverlayKind | undefined | null = null;

/** Whether "공유" is being pressed by ⌘⇧O put down over a menu (kind "menu"), which stays open under the share (TopBar). */
export function shareKeyOverMenu() {
  return pressingOver === "menu";
}

const sidebarRowMenuDrawn = () => [...document.querySelectorAll('[data-row-menu-variant="sidebar"]')].some((menu) => menu.getClientRects().length > 0);

function onShareKey(event: KeyboardEvent) {
  if (!matchesKeyboardShortcut("sharePage", event)) return;
  // A block's menu ⌘/ opened keeps it, opening nothing (blockMenuShareKey.ts).
  if (blockMenuKeepsShareKey()) { event.preventDefault(); return; }
  const over = topLayerKind();
  // So does a sidebar row's "⋯" menu (RowMenuPlaced's and the favorites' and views' rows' alike), the menu staying.
  if (over === "menu" && sidebarRowMenuDrawn()) { event.preventDefault(); return; }
  const shown = shareButtons.filter((entry) => entry.button()?.isConnected);
  const front = [...shown].reverse().find((entry) => entry.peek) ?? shown.at(-1);
  const button = front?.button();
  if (!button) return;
  event.preventDefault();
  // The bar it hangs from stays, though the key came from the page's text
  // (typingFade.ts): Notion's keeps its bar (seen 2026-11-13).
  delete document.documentElement.dataset.typing;
  pressingOver = over;
  try {
    if (button.getAttribute("aria-expanded") !== "true") pressByShortcut(button);
  } finally { pressingOver = null; }
  shareKeyOpensShare(over);
}

/**
 * ⌘⇧O presses the "공유" of the page in front, as Notion's (seen
 * 2026-11-13: with a side peek open, the peek's share opened under its
 * button): a peek's before the page under it, the one shown last first;
 * an open share stays open. It does so from a window or menu open over the
 * page too, heard before that window's own keys: Notion's share opened from
 * a table cell's files list and choice picker, the filter's property menu
 * and a filter's window, a column's menu, the move picker, the "/" menu, a
 * block's menu and a "delete this property?" question, its field taking the
 * letter typed next (2026-11-26, Chrome 1440, judged by the screen and one
 * letter after the keys with nothing run in the page before them, in two
 * tabs at two times). The "/" menu and a block's menu its handle opened
 * close as it opens, as Notion's; a block's menu ⌘/ opened keeps the key.
 * So do the page's "⋯" (its menu staying under the share, the first Escape
 * closing the share alone, the second the menu, "⋯" ringed), a comment's
 * "⋯" and the page's verification menu (2026-11-26, the same way, the page's
 * "⋯" in three tabs, the others in one); a sidebar row's "⋯" menu keeps the
 * key, nothing opening, as Notion's (2026-11-26, two tabs at two times,
 * after a press and after Tab into it). Over the text selection's toolbar or
 * its turn list the words are let go as the share opens
 * (NativeTiptapSelectionToolbar).
 */
function useShareShortcut(inside: RefObject<HTMLElement | null>, enabled: boolean) {
  useEffect(() => {
    const at = inside.current;
    if (!enabled || !at) return undefined;
    const entry: ShareButton = {
      button: () => at.closest("button"),
      peek: Boolean(at.closest('aside[data-mode="side"], aside[data-mode="center"]')),
    };
    shareButtons.push(entry);
    if (!listening) {
      window.addEventListener("keydown", onShareKey, true);
      listening = true;
    }
    return () => {
      const index = shareButtons.indexOf(entry);
      if (index >= 0) shareButtons.splice(index, 1);
    };
  }, [enabled, inside]);
}
