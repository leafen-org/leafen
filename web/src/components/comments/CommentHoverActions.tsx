import { Fragment, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CheckIcon, DotsHorizontal, ReopenIcon, SidePeekIcon, UndoIcon, XMarkSmallIcon } from "@/icons/leafen";
import { popoverAnchorOf, placeAround, popoverBackdropStyle, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheetMenu, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import type { NativeComment } from "@/lib/commentService";
import { AddReactionButton } from "./CommentReactions";
import styles from "./CommentHoverActions.module.css";
import { suggestionFacts } from "./suggestionModel";
import { keysLedFocus } from "@/lib/lastPress";
import { hoverRestsAfterLayer, restHoverUnderPointer } from "@/lib/hoverRest";

// After the reaction picker opened from a comment closes, its buttons stay
// away until the pointer moves on it again, as Notion's (lib/hoverRest.ts).
hoverRestsAfterLayer(styles.host, "move");

export type CommentMenuItem = {
  key: string;
  label: string;
  icon: ReactNode;
  danger?: boolean;
  /** A line goes above the item, as above Notion's delete. */
  separatorBefore?: boolean;
  /** Chosen, with the "⋯" its menu opened from (none from a phone's sheet). */
  onSelect(opener?: HTMLElement | null): void;
};

const MENU_WIDTH = 200;

/** An open suggestion's accept and reject, for its comment's buttons (CommentHoverActions `settle`). */
export type SuggestionSettleButtons = { onAccept?: () => void; onReject?: () => void };

/**
 * The accept and reject an open suggestion's comment leads its buttons with,
 * wherever its card is drawn — beside the page open or folded, in "모든
 * 토론", in a block's window — as Notion's (seen 2026-11-25 in Chrome at
 * 1440 and 1000, two tabs: [✓ ✕ 😊 ⋯] on a folded margin card too): accept
 * for whoever may edit the page, reject for them and the suggestion's
 * author; none once it is settled.
 */
export function suggestionSettleButtons(comment: NativeComment, person: { userId: string; canComment: boolean; canEditPage: boolean },
  settle: (decision: "accept" | "reject") => void): SuggestionSettleButtons | undefined {
  const suggestion = suggestionFacts(comment);
  if (!suggestion || suggestion.state !== null || !person.canComment) return undefined;
  const accept = person.canEditPage, reject = person.canEditPage || comment.authorId === person.userId;
  if (!accept && !reject) return undefined;
  return { onAccept: accept ? () => settle("accept") : undefined, onReject: reject ? () => settle("reject") : undefined };
}

/** The class a comment's card takes for its buttons to sit at its top right. */
export const commentHoverHost = styles.host;
/** The dot at the end of a thread's first comment's header while the person has it marked unread. */
export const commentUnreadDot = styles.unread;

/**
 * A comment's buttons as Notion's (seen 2026-09-30): hovering the comment
 * shows, at its top right, add reaction, resolve for a thread's first
 * comment, and "…", whose menu holds the comment's other actions — on a
 * phone a short list at the screen's bottom, as Notion's sheets. An open
 * suggestion's comment leads with accept and reject, [✓ ✕ 😊 ⋯], as
 * Notion's beside the page, open or folded, and in a block's window, and
 * after "↩" in "모든 토론", [↩ ✓ ✕ 😊 ⋯] (seen 2026-11-22 twice at 1440;
 * 2026-11-25 in Chrome at 1440 and 1000, two tabs — suggestionSettleButtons):
 * the keyboard reaches them by Tab, which shows the buttons, and a touch
 * screen shows them all the time. Each names itself in a tip above it, 6px
 * over it and centred (Notion's, seen 2026-11-25 in two tabs on ✓, ✕, ↩,
 * 😊, ⋯ and a thread's ✓, above though there was room below). A thread under a page's title, answered
 * from a box of its own, leads with "↩ 회신", [↩ 😊 ✓ ⋯], as Notion's
 * there (seen 2026-11-25 on a Galaxy A90's Chrome at 411 and in Chrome at
 * 1440, a page's and a database page's, two tabs each; a block's thread
 * keeps [😊 ✓ ⋯], its box always under it). On a computer it leads them
 * with "▯ 열기" and a line, [▯ 열기 | ↩ 😊 ✓ ⋯], which opens the thread in a
 * view of its own (DiscussionSideView; Notion's, seen 2026-11-25 at 1440,
 * two tabs; its phone has none).
 */
export function CommentHoverActions({ canReact, onOpenReactionPicker, open, reply, resolve, settle, menu }: {
  canReact: boolean;
  onOpenReactionPicker: (button: HTMLElement) => void;
  /** Opens the thread in a view of its own, from the button pressed. */
  open?: (button: HTMLElement) => void;
  /** Opens the thread's own reply box, the caret in it. */
  reply?: () => void;
  resolve?: { resolved: boolean; onToggle(): void };
  /** An open suggestion's accept and reject, each where the person may (suggestionSettleButtons). */
  settle?: SuggestionSettleButtons;
  menu: readonly CommentMenuItem[];
}) {
  const { t } = useTranslation("commentsPanel");
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  // The buttons stay up while a focus the keys brought is in them — by Tab,
  // or given back to "⋯" as Escape closes its menu or a delete question —
  // with the pointer gone, as Notion's; a focus a press gave back does not
  // keep them once the pointer goes, though a key typed after it rings it
  // (seen 2026-11-26 in Chrome at 1440 in two tabs at different times, judged
  // after the action: "⋯"'s menu closed by a press on the comment, a letter
  // typed, the pointer moved off — the buttons gone, "⋯" ringed under them).
  const [keysIn, setKeysIn] = useState(false);
  const moreRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const { phone } = useSidebarLayout(true);
  // Under the "…", lined up with its right edge, or over it where only that
  // side has room. Escape leaves the comment under the pointer without its
  // buttons until the pointer moves (lib/hoverRest.ts).
  const place = usePopoverPlacement(menuRef, anchor ? (size, viewport) => placeAround(
    anchor, size, viewport, { gap: 4, align: "end" },
  ) : null, [anchor], undefined, { kind: "menu", onEscape: () => { restHoverUnderPointer(); close(); }, onOutsidePress: close,
    inside: () => [moreRef.current] });
  // Opened with the pointer it lights no row, as Notion's comment "…" (seen 2026-11-06).
  const picker = useNativeMenu({ items: [...menu], open: Boolean(anchor) && !phone, menuRef, quietOnPointer: true,
    label: t("actions.more"), keyOf: item => item.key, textOf: item => item.label,
    onAction: item => { setAnchor(null); item.onSelect(moreRef.current); } });

  function close() {
    setAnchor(null);
  }

  const openShown = Boolean(open) && !phone;
  if (!canReact && !openShown && !reply && !resolve && !settle?.onAccept && !settle?.onReject && menu.length === 0) return null;
  return (
    <div className={styles.pill} data-comment-hover-actions data-open={anchor ? "true" : undefined} data-keys-in={keysIn ? "" : undefined}
      onFocus={(event) => setKeysIn(keysLedFocus(event.target))}
      onBlur={(event) => { if (!(event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget))) setKeysIn(false); }}>
      {open && openShown && (
        <>
          <button type="button" className={`${styles.button} ${styles.openButton}`} data-comment-action="open"
            onClick={(event) => open(event.currentTarget)}>
            <SidePeekIcon size="regular" aria-hidden="true" /><span className={styles.openLabel}>{t("actions.open")}</span>
          </button>
          <span className={styles.divider} aria-hidden="true" />
        </>
      )}
      {reply && (
        // Notion's arrowUTurnUpLeft, the arrow turning back up and left.
        <button type="button" className={styles.button} data-comment-action="reply"
          aria-label={t("actions.reply")} title={t("actions.reply")} data-tip-side="top" onClick={reply}>
          <UndoIcon size="small" aria-hidden="true" />
        </button>
      )}
      {settle?.onAccept && (
        <button type="button" className={styles.button} data-suggestion-action="accept"
          aria-label={t("actions.accept")} title={t("actions.accept")} data-tip-side="top" onClick={settle.onAccept}>
          <CheckIcon size="small" aria-hidden="true" />
        </button>
      )}
      {settle?.onReject && (
        <button type="button" className={styles.button} data-suggestion-action="reject"
          aria-label={t("actions.reject")} title={t("actions.reject")} data-tip-side="top" onClick={settle.onReject}>
          <XMarkSmallIcon size="small" aria-hidden="true" />
        </button>
      )}
      {canReact && <AddReactionButton className={styles.button} iconSize="small" tipSide="top" onOpen={onOpenReactionPicker} />}
      {resolve && (
        <button type="button" className={styles.button} data-comment-action="resolve" aria-pressed={resolve.resolved}
          aria-label={t(resolve.resolved ? "actions.reopen" : "actions.resolve")}
          title={t(resolve.resolved ? "actions.reopen" : "actions.resolve")} data-tip-side="top" onClick={resolve.onToggle}>
          {resolve.resolved ? <ReopenIcon size="small" aria-hidden="true" /> : <CheckIcon size="small" aria-hidden="true" />}
        </button>
      )}
      {menu.length > 0 && (
        <button ref={moreRef} type="button" className={styles.button} data-comment-action="more"
          aria-label={t("actions.more")} title={t("actions.more")} data-tip-side="top" aria-haspopup="menu" aria-expanded={Boolean(anchor)}
          onClick={(event) => setAnchor(anchor ? null : popoverAnchorOf(event.currentTarget))}>
          <DotsHorizontal size="small" aria-hidden="true" />
        </button>
      )}
      {anchor && phone ? (
        <PhoneSheetMenu label={t("actions.more")} onClose={close}>
          {menu.map((item) => (
            <PhoneSheetRow key={item.key} more={false} icon={item.icon} label={item.label} danger={item.danger}
              data={{ "data-comment-action": item.key }} onPress={() => { setAnchor(null); item.onSelect(); }} />
          ))}
        </PhoneSheetMenu>
      ) : anchor && typeof document !== "undefined" ? createPortal(
        <>
          <button type="button" className={styles.backdrop} style={popoverBackdropStyle(place)} tabIndex={-1} aria-label={t("actions.closeMenu")} onClick={close} />
          <div {...picker.menuProps} ref={menuRef} className={styles.menu}
            style={{ width: MENU_WIDTH, ...popoverPlacementStyle(place) }}>
            {menu.map((item, index) => (
              <Fragment key={item.key}>
                {item.separatorBefore && index > 0 && <div role="separator" className={styles.separator} />}
                {/* "삭제하기" in the menu's own color, red under the pointer, as Notion's (seen 2026-11-06, 2026-11-24). */}
                <NativeMenuItem state={picker.state} itemKey={item.key} className={styles.item}
                  data-comment-action={item.key} data-danger={item.danger ? "true" : undefined}>
                  {({ labelProps }) => <>{item.icon}<span {...labelProps}>{item.label}</span></>}
                </NativeMenuItem>
              </Fragment>
            ))}
          </div>
        </>,
        document.body,
      ) : null}
    </div>
  );
}
