import { POPOVER_MARGIN, popoverAnchorOf, popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { tipRequest } from "@/lib/tipPlacement";
import { useRef, useState, type FocusEvent, type PointerEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Trans, useTranslation } from "react-i18next";
import { personName } from "@/lib/peopleDirectory";
import tipStyles from "@/components/IconTipButton.module.css";
import type { CommentReaction } from "@/commentWire.generated";
import { AnchoredEmojiPicker, PICKER_WIDTH } from "@/components/page/AnchoredEmojiPicker";
import { AddReactionIcon, CheckIcon } from "@/icons/leafen";
import { useWorkspaceCustomEmojiFileId } from "@/lib/customEmojiIcon";
import { filePreviewWidth, useWorkspaceFileUrl } from "@/lib/fileUrls";
import { nativeWorkspaceFileLocator } from "@/lib/workspaceFileTransport";
import styles from "./CommentReactions.module.css";
import { useTipPress } from "@/lib/tipTiming";
import { keysLedFocus } from "@/lib/lastPress";
import { giveFocusBack } from "@/lib/focusGiveBack";
import { restHoverAround } from "@/lib/hoverRest";

// A reaction with one of the workspace's own emoji names it by its ID, as a
// page icon does; the server keeps the same name.
const CUSTOM_EMOJI_PREFIX = "custom_emoji:";

function customEmojiId(emoji: string) {
  return emoji.startsWith(CUSTOM_EMOJI_PREFIX) ? emoji.slice(CUSTOM_EMOJI_PREFIX.length) : undefined;
}

/** A reaction's emoji: the character, or the workspace emoji's picture. */
function ReactionGlyph({ emoji, workspaceId }: { emoji: string; workspaceId?: string }) {
  const emojiId = customEmojiId(emoji);
  const fileId = useWorkspaceCustomEmojiFileId(workspaceId, emojiId);
  const url = useWorkspaceFileUrl(fileId ? nativeWorkspaceFileLocator(fileId) : "", ["data:image/"], { width: filePreviewWidth(32) });
  if (!emojiId) return <>{emoji}</>;
  return url
    ? <img className={styles.customEmoji} src={url} alt="" data-reaction-custom-emoji={emojiId} />
    : <span className={styles.customEmojiMissing} data-reaction-custom-emoji={emojiId} />;
}

// A comment's reactions as Notion shows them (seen 2026-09-30): under the
// comment, each emoji with how many used it, the person's own in blue; a
// chip adds or takes back the person's emoji, and the smiley after the
// chips, or in the comment's actions, picks a new one.

// The picker centred on the button that asked for it (kept inside the
// window), as Notion's (seen 2026-11-06: under a page comment and in a
// block's card alike).
function centeredPickerAnchor(button: HTMLElement): DOMRect {
  const anchor = popoverAnchorOf(button);
  const width = () => Math.min(PICKER_WIDTH, window.innerWidth - 2 * POPOVER_MARGIN);
  return Object.defineProperties({} as DOMRect, {
    x: { get: () => anchor.left + (anchor.width - width()) / 2 },
    y: { get: () => anchor.y },
    top: { get: () => anchor.top },
    bottom: { get: () => anchor.bottom },
    left: { get: () => anchor.left + (anchor.width - width()) / 2 },
    right: { get: () => anchor.left + (anchor.width + width()) / 2 },
    width: { get: () => width() },
    height: { get: () => anchor.height },
    contextElement: { value: button },
  });
}

/**
 * Opens the emoji picker by the button that asked for it. Closed by Escape
 * or a press beside it, it leaves the focus on nothing, the button pressed or
 * reached by the keys and opened by Enter alike, as Notion's (measured
 * 2026-11-26 in Chrome in two tabs at different times each, judged after the
 * action with nothing run in the page before it: a page comment's
 * add-reaction pressed, or reached by ⇧Tab and opened by Enter, then Escape
 * or a press beside the picker — the focus on nothing, no ring, a letter
 * typed next landing nowhere). A press leaves the focus where it was
 * (AddReactionButton) and the picker's layer gives it back there; a button
 * the keys opened it from lets the focus go as the picker opens, so its
 * layer has none to give back. An emoji picked from one the keys opened
 * gives that button its focus back (Notion's not measured). Given the
 * comment's workspace and page, the picker offers the workspace's own
 * emoji, as Notion's reaction picker does (seen 2026-10-01: "사용자 지정"
 * and "추가" at the end of its bar). How it opens may name where it stands
 * (`at`, read before its button went), leave the focus to a field opened
 * with it (`leavesFocus`, ReactionPickerOpening) and the comment whose
 * buttons rest once it closes (`restAround`).
 */
export function useReactionPicker(onPick: (emoji: string) => void, place?: { workspaceId: string; pageId: string }) {
  const [shown, setShown] = useState<{ anchor: DOMRect; leavesFocus: boolean } | null>(null);
  const buttonRef = useRef<HTMLElement | null>(null);
  const openerRef = useRef<HTMLElement | null>(null);
  const restRef = useRef<ReactionPickerOpening["restAround"]>(undefined);
  const close = (picked: boolean) => {
    // An emoji pressed in one that left the focus to a field opened with it
    // takes it from there, the focus on nothing, as Notion's (seen 2026-11-26
    // in two tabs: a resting margin card's, its reply box left).
    if (picked && shown?.leavesFocus && document.activeElement instanceof HTMLElement) document.activeElement.blur();
    setShown(null);
    const opener = openerRef.current;
    openerRef.current = null;
    // The comment or feed card it was opened from keeps its buttons away
    // while the pointer stays where it was (lib/hoverRest.ts).
    restHoverAround(restRef.current?.() ?? buttonRef.current);
    buttonRef.current = null;
    restRef.current = undefined;
    if (picked && opener?.isConnected) giveFocusBack({ to: opener, keepScroll: true, unlessOpened: true });
  };
  return {
    open: (button: HTMLElement, how: ReactionPickerOpening = {}) => {
      buttonRef.current = button;
      restRef.current = how.restAround;
      const keys = document.activeElement === button;
      openerRef.current = keys ? button : null;
      if (keys) button.blur();
      setShown({ anchor: how.at ?? centeredPickerAnchor(button), leavesFocus: how.leavesFocus === true });
    },
    picker: shown ? (
      <ReactionPickerLayer
        anchor={shown.anchor}
        leavesFocus={shown.leavesFocus}
        place={place}
        onClose={() => close(false)}
        onPick={(emoji) => {
          close(true);
          onPick(emoji);
        }}
      />
    ) : null,
  };
}

/** How a reaction picker opens (useReactionPicker's `open`). */
export type ReactionPickerOpening = {
  /** Where it stands, read from its button before a redraw took the button away (reactionPickerAnchor). */
  at?: DOMRect;
  /** It takes no focus, leaving it to a field opened with it (AnchoredEmojiPicker `leavesFocus`). */
  leavesFocus?: boolean;
  /** The comment whose buttons rest once it closes, where its button is gone (lib/hoverRest.ts). */
  restAround?: () => Element | null | undefined;
};

/** Where a reaction picker opened from `button` stands, kept once the button is gone. */
export const reactionPickerAnchor = (button: HTMLElement): DOMRect => centeredPickerAnchor(button);

/**
 * The one picker every reaction opens — a comment's, a page's own and chosen
 * words' — given the workspace and page, its "최근 사용한 항목" and own emoji
 * are the workspace's, a picked own emoji named by its ID.
 */
export function ReactionPickerLayer({ anchor, leavesFocus, place, marker = "comment-reaction-picker", onClose, onPick }: {
  anchor: DOMRect;
  /** It takes no focus, a field opened with it keeping the caret (AnchoredEmojiPicker). */
  leavesFocus?: boolean;
  place?: { workspaceId: string; pageId: string };
  /** Names the layer for the place that opened it, as data-{marker}. */
  marker?: string;
  onClose: () => void;
  onPick: (emoji: string) => void;
}) {
  const { t } = useTranslation("commentsPanel");
  return (
    <AnchoredEmojiPicker anchor={anchor} label={t("reactions.add")} marker={marker} leavesFocus={leavesFocus}
      onPick={onPick} onClose={onClose}
      custom={place ? { target: place, onPick: (emoji) => onPick(`${CUSTOM_EMOJI_PREFIX}${emoji.id}`), pickAdded: false } : undefined} />
  );
}

/** The comment actions' smiley with a plus, as Notion's (seen 2026-11-06), which picks an emoji to react with. */
export function AddReactionButton({ className, iconSize = "regular", tipSide, onOpen }: {
  className?: string;
  /** 16px on a comment's hover tools, as Notion's (seen 2026-10-07). */
  iconSize?: "small" | "regular";
  /** Its tip above it, as among a comment's hover tools (CommentHoverActions). */
  tipSide?: "top";
  onOpen: (button: HTMLElement) => void;
}) {
  const { t } = useTranslation("commentsPanel");
  return (
    <button type="button" className={className} aria-label={t("reactions.add")} title={t("reactions.add")} data-tip-side={tipSide}
      aria-haspopup="dialog" data-comment-action="react" data-press-keeps-focus onClick={(event) => onOpen(event.currentTarget)}>
      <AddReactionIcon size={iconSize} aria-hidden="true" />
    </button>
  );
}

/**
 * A reaction's chip with a dark tip under it while the pointer is on it (or
 * the keyboard's focus): the emoji on a small light tile, then who reacted,
 * as Notion's "〈이름〉 님이 👍 이모티콘으로 반응함" (seen 2026-11-06). A finger
 * has no hover: its press on the chip only toggles it, with no tip.
 */
function ReactionChip({ reaction, workspaceId, children }: {
  reaction: CommentReaction; workspaceId?: string;
  children: (tipProps: { onPointerEnter(event: PointerEvent<HTMLElement>): void; onPointerLeave(): void;
    onFocus(event: FocusEvent<HTMLElement>): void; onBlur(): void }) => ReactNode;
}) {
  const { t } = useTranslation("commentsPanel");
  const [anchor, setAnchor] = useState<PopoverRect | null>(null);
  const tipRef = useRef<HTMLSpanElement>(null);
  const place = usePopoverPlacement(tipRef, anchor ? () => tipRequest(anchor, "bottom", 6) : null,
    [anchor], undefined, false);
  // A press takes the tip, as Notion's every tip goes as the mouse goes down (lib/tipTiming.ts).
  useTipPress(anchor !== null, () => setAnchor(null));
  const glyph = <ReactionGlyph emoji={reaction.emoji} workspaceId={workspaceId} />;
  const names = reaction.userIds.map((id) => personName(id)).filter(Boolean).join(", ");
  return <>
    {children({
      onPointerEnter: (event) => { if (event.pointerType !== "touch") setAnchor(event.currentTarget.getBoundingClientRect()); },
      onPointerLeave: () => setAnchor(null),
      onFocus: (event) => { if (keysLedFocus(event.currentTarget)) setAnchor(event.currentTarget.getBoundingClientRect()); },
      onBlur: () => setAnchor(null),
    })}
    {anchor && names && typeof document !== "undefined" ? createPortal(
      <span ref={tipRef} className={`${tipStyles.tip} ${styles.tip}`} style={popoverPlacementStyle(place)} role="tooltip" data-reaction-tip>
        <span className={styles.tipTile}>{glyph}</span>
        <span className={styles.tipLine}>
          <Trans t={t} i18nKey="reactions.tip" values={{ names }} components={{ dim: <span className={styles.tipDim} />, emoji: glyph }} />
        </span>
      </span>,
      document.body,
    ) : null}
  </>;
}

/**
 * The chips of a comment's reactions, with the smiley to add another; a feed
 * post gives its own button for that, which shows with no chips as well.
 */
export function CommentReactionChips({ reactions, userId, workspaceId, canReact, onToggle, onOpenPicker, onResolve, addButton, trailing, className, variant }: {
  reactions: readonly CommentReaction[];
  userId: string;
  /** The comment's workspace, whose own emoji its reactions may use. */
  workspaceId?: string;
  canReact: boolean;
  onToggle: (emoji: string, reacted: boolean) => void;
  onOpenPicker: (button: HTMLElement) => void;
  /** Resolves a page's reactions together, as Notion's "해결" beside them. */
  onResolve?: () => void;
  addButton?: ReactNode;
  /** What the row ends with on its right, as a feed post's viewers. */
  trailing?: ReactNode;
  className?: string;
  /** A page's own reactions, heading its comments. */
  variant?: "page";
}) {
  const { t } = useTranslation("commentsPanel");
  if (reactions.length === 0 && !addButton && !trailing) return null;
  return (
    <div className={[styles.row, variant === "page" ? styles.pageRow : "", className ?? ""].filter(Boolean).join(" ")}
      data-comment-reactions={variant ?? "true"}>
      {reactions.map((reaction) => {
        const mine = reaction.userIds.includes(userId);
        const count = reaction.userIds.length;
        return (
          <ReactionChip key={reaction.emoji} reaction={reaction} workspaceId={workspaceId}>
            {(tipProps) => <button type="button" className={styles.chip} data-mine={mine ? "true" : undefined}
              aria-pressed={mine} disabled={!canReact} data-reaction-emoji={reaction.emoji}
              aria-label={t("reactions.chip", { emoji: customEmojiId(reaction.emoji) ? t("reactions.customEmoji") : reaction.emoji, count })}
              onClick={() => onToggle(reaction.emoji, !mine)} {...tipProps}>
              <span className={styles.emoji} aria-hidden="true"><ReactionGlyph emoji={reaction.emoji} workspaceId={workspaceId} /></span>
              <span aria-hidden="true">{count}</span>
            </button>}
          </ReactionChip>
        );
      })}
      {addButton !== undefined ? addButton : canReact
        ? <AddReactionButton className={styles.add} iconSize={variant === "page" ? "regular" : "small"} onOpen={onOpenPicker} />
        : null}
      {onResolve && reactions.length ? (
        <button type="button" className={styles.add} aria-label={t("reactions.resolve")} title={t("reactions.resolve")}
          data-page-reactions-resolve onClick={onResolve}>
          <CheckIcon size="small" aria-hidden="true" />
        </button>
      ) : null}
      {trailing}
    </div>
  );
}
