"use client";

import type { Session } from "@/types";
import { copyText } from "@/lib/copyText";
import { pageLinkToCopy } from "@/lib/databasePageLink";
import { usePageLinkCopyNotice } from "@/lib/usePageLinkCopyNotice";
import { useProductToast } from "@/lib/productToast";
import { useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { CenterPeekIcon, CheckIcon, ChevronDown, ChevronUp, FullPagePeekIcon, LinkIcon, NewTabIcon, PageDetailsIcon, SidePeekIcon, SlidersIcon, Star, StarFilled } from "@/icons/leafen";
import type { RowPageDetails } from "./RowPageLayoutBody";
import type { DatabaseRowPageLockControl } from "./DatabaseRowMenu";
import type { DatabaseViewOpenMode } from "./DatabaseView";
import styles from "./databaseSheet";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { PopoverLayer } from "@/lib/popoverLayer";
import { type PopoverRequest, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { inlineEndX } from "@/lib/readingDirection";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { caretAtPeekTitle, PEEK_TITLE } from "./peekFocusReturn";
import { shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import { type RowPeekSiblings, useRowPeekSiblingKeys } from "./rowPeekSiblingKeys";

// A database page's top bar, as Notion's: its view options, the time it was
// last edited, its link and its favorite, beside its share and … menu.

type PeekMode = "side" | "center" | "full";

/** What a database page's top bar offers beside its share and … menu. */
export type RowPageTopBarActions = {
  /** Switches how the page shows, from its view options. */
  onChangeMode?: (mode: DatabaseViewOpenMode) => void;
  onChangeDefaultMode?: () => void;
  onCopyLink?: () => void;
  onFavorite?: (favorite: boolean) => void;
  /** Opens the page's updates, from the time it was last edited. */
  onOpenUpdates?: () => void;
  /** Closes the panel docked beside the page, its updates or "모든 토론": the bar then reads as a page's beside its panel. */
  onClosePanel?: () => void;
  /** The panel docked is "모든 토론", which the bar's comments close (barCommentsPress). */
  discussionsDocked?: boolean;
  /** The page's lock, as its … menu changes it, for the bar's "Locked". */
  lock?: DatabaseRowPageLockControl;
  /** The rows before and after this one in its view, for a peek's ∧ and ∨. */
  siblings?: RowPeekSiblings;
};

const VIEW_OPTIONS = [
  ["side", SidePeekIcon, "peekModes.side"],
  ["center", CenterPeekIcon, "peekModes.center"],
  ["full", FullPagePeekIcon, "peekModes.full"],
  ["new_tab", NewTabIcon, "peekModes.newTab"],
] as const;
const CHANGE_DEFAULT = ["default", SlidersIcon, "peekModes.changeDefault"] as const;

/**
 * Under the bar's line and view options button together, its middle under
 * theirs and its top on the button's foot, as Notion's (seen 2026-11-13, 1440:
 * its 220 menu at 693.5,34 under a 39px line and button at 784–823, and at
 * 180,106 under 270.5–309.5 in a center peek) — reaching past the peek's edge.
 */
function underViewOptions(button: HTMLElement | null): PopoverRequest {
  const box = button?.getBoundingClientRect() ?? { top: 0, bottom: 0, left: 0, right: 0 };
  // Notion's line and button are 39px wide: their middle is 19.5 in from the button's end.
  const middle = inlineEndX(box, -19.5, button);
  return { anchor: { top: box.top, bottom: box.bottom, left: middle, right: middle, contextElement: button ?? undefined },
    placement: "bottom", gap: 0, options: {} };
}

/**
 * How the database page shows: beside its view, over the view's center, as
 * the full page, or in a new tab. It switches the open page at once.
 */
export function RowPeekViewOptions({ mode, onOpenMode, onChangeDefault }: {
  mode: PeekMode;
  onOpenMode: (mode: DatabaseViewOpenMode) => void;
  /** "기본 보기 변경", under a line: the view's "페이지 보기 선택", as Notion's (seen 2026-11-04). */
  onChangeDefault?: () => void;
}) {
  const { t } = useTranslation("databaseView");
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLSpanElement>(null);
  const menuId = useId();
  const menuRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const { zIndex, onKeyDown } = useOverlayLayer(open, { kind: "menu", ref: menuRef, onEscape: () => setOpen(false),
    onOutsidePress: () => setOpen(false), inside: () => [rootRef.current] });
  const menuPlace = usePopoverPlacement(menuRef, open ? () => underViewOptions(buttonRef.current) : null, [open], undefined, false);
  const menu = useNativeMenu({ items: onChangeDefault ? [...VIEW_OPTIONS, CHANGE_DEFAULT] : [...VIEW_OPTIONS], open, menuRef, id: menuId, label: t("peekViewOptions"),
    keyOf: item => item[0], textOf: item => t(item[2]), firstKey: mode,
    onAction: item => {
      setOpen(false);
      if (item[0] === "default") onChangeDefault?.();
      else if (item[0] !== mode) {
        onOpenMode(item[0]);
        // Turned between side and center, the peek's title takes the caret
        // once the menu has gone, what is typed meanwhile waiting for it (caretAtPeekTitle).
        if (item[0] === "side" || item[0] === "center") caretAtPeekTitle(
          () => buttonRef.current?.closest<HTMLElement>("[data-page-peek], [data-row-peek-panel]") ?? null, PEEK_TITLE, true, true);
      }
    } });
  const Current = mode === "side" ? SidePeekIcon : mode === "center" ? CenterPeekIcon : FullPagePeekIcon;
  return (
    <span ref={rootRef} className={styles.rowPeekViewOptions}>
      <button ref={buttonRef} type="button" className={styles.rowPeekIconAction} data-row-peek-view-options
        aria-label={t("peekViewOptions")} title={t("peekViewOptions")} aria-haspopup="menu" aria-expanded={open}
        aria-controls={open ? menuId : undefined} onClick={() => setOpen((current) => !current)}>
        <Current size="regular" />
      </button>
      {open ? <PopoverLayer>
        <div {...menu.menuProps} ref={menuRef} className={styles.rowPeekViewMenu} style={{ ...popoverPlacementStyle(menuPlace), zIndex }}
          onKeyDown={event => { menu.menuProps.onKeyDown?.(event); onKeyDown(event); }}>
          {VIEW_OPTIONS.map(([value, Icon, label]) => (
            <NativeMenuItem key={value} itemKey={value} state={menu.state} role="menuitemradio" aria-checked={value === mode}
              data-peek-mode={value}>{({ labelProps }) => <>
              <Icon size="regular" />
              <span {...labelProps}>{t(label)}</span>
              {value === mode ? <CheckIcon size="small" /> : null}
            </>}</NativeMenuItem>
          ))}
          {onChangeDefault ? <>
            <hr className={styles.rowPeekViewMenuLine} />
            <NativeMenuItem itemKey={CHANGE_DEFAULT[0]} state={menu.state} data-peek-change-default>{({ labelProps }) => <>
              <SlidersIcon size="regular" />
              <span {...labelProps}>{t(CHANGE_DEFAULT[2])}</span>
            </>}</NativeMenuItem>
          </> : null}
        </div>
      </PopoverLayer> : null}
    </span>
  );
}

const SIBLINGS = [
  ["previous", ChevronUp, "previousRowPage", "previousPeekPage"],
  ["next", ChevronDown, "nextRowPage", "nextPeekPage"],
] as const;

/**
 * ∧ and ∨, which open the row before or after this one in its view's order,
 * as Notion's peek does (seen 2026-11-01): after a line like the view
 * options', shown with them, faded and inert at either end; ⌃⇧K and ⌃⇧J
 * do the same from anywhere while the peek is open.
 */
export function RowPeekSiblingButtons({ siblings }: { siblings: RowPeekSiblings }) {
  const { t } = useTranslation("databaseView");
  useRowPeekSiblingKeys(siblings);
  return (
    <span className={styles.rowPeekSiblings}>
      {SIBLINGS.map(([side, Icon, label, shortcut]) => {
        const rowId = siblings[side];
        return (
          <button key={side} type="button" className={styles.rowPeekIconAction} data-row-peek-sibling={side}
            aria-label={t(label)} title={t(label)} data-tip-keys={shortcutCompactLabel(shortcut)}
            aria-disabled={rowId ? undefined : true} onClick={() => { if (rowId) siblings.onOpen(rowId); }}>
            <Icon size="regular" />
          </button>
        );
      })}
    </span>
  );
}

/** The bar's link button, named in the page bar's tip with its keys, as Notion's (seen 2026-11-13). */
export function RowPageCopyLink({ onCopyLink, session, pageId, onOpenShare }: {
  onCopyLink: () => void; session?: Session; pageId: string; onOpenShare: () => void;
}) {
  const { t } = useTranslation("topBar");
  const { notify } = useProductToast();
  const notifyLinkCopied = usePageLinkCopyNotice(session, pageId, onOpenShare);
  async function copyLink() {
    if (!session) { onCopyLink(); return; }
    if (await copyText(pageLinkToCopy({ id: pageId }, session.user.id))) notifyLinkCopied();
    else notify(t("toast.couldntCopyLink"), "error");
  }
  return (
    <button type="button" className={styles.rowPeekIconAction} data-row-peek-copy-link
      aria-label={t("copyLinkTip")} title={t("copyLinkTip")} data-tip-keys={shortcutCompactLabel("copyPageLink")}
      onClick={() => void copyLink()}>
      <LinkIcon size="regular" />
    </button>
  );
}

export function RowPageFavorite({ favorite, pageTitle, onFavorite }: {
  favorite: boolean;
  pageTitle: string;
  onFavorite: (favorite: boolean) => void;
}) {
  const { t } = useTranslation("topBar");
  const label = t(favorite ? "removeFavorite" : "addFavorite", { pageTitle });
  return (
    <button type="button" className={styles.rowPeekIconAction} data-row-peek-favorite
      data-favorite={favorite ? "true" : undefined} aria-label={label} title={t(favorite ? "removeFavoriteTip" : "addFavoriteTip")} aria-pressed={favorite}
      onClick={() => onFavorite(!favorite)}>
      {favorite ? <StarFilled size="regular" /> : <Star size="regular" />}
    </button>
  );
}

/**
 * A page with a details panel shows it or hides it from its top bar, before
 * its favorite, as Notion's "페이지 정보" (seen 2026-11-11 in a page, a side
 * and a center peek and a template): "세부 정보 보기" closed, "상세정보
 * 숨기기" lit while the panel shows, the same panel "세부 정보 보기" under its
 * title opens.
 */
export function RowPageDetailsButton({ details }: { details: RowPageDetails }) {
  const { t } = useTranslation("rowProperties");
  return (
    <button type="button" className={styles.rowPeekIconAction} data-row-peek-details aria-label={t("pageInfo")}
      title={t(details.open ? "topBarDetailsHide" : "layoutDetailsShow")} aria-expanded={details.open}
      onClick={() => details.setOpen(!details.open)}>
      <PageDetailsIcon size="regular" />
    </button>
  );
}
