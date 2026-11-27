import { useCallback, useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { imeComposing } from "@/lib/fieldKeyBoundary";
import { topLayerZIndex, useOverlayLayer } from "@/lib/overlayLayer";
import cardStyles from "./BlockDiscussionCard.module.css";
import styles from "./GoneDiscussionCard.module.css";
import { ANCHORED_WIDTH, useAnchoredPlace } from "./anchoredCardPlace";

// A block's discussion deleted whole: an old address of it (a copied link, a
// notice) chooses the block it was left on and opens a card under the block
// that says none are left, as Notion's (seen 2026-11-21 twice, two tabs, 1440
// dpr 2: a text discussion and a block's own discussion each deleted whole —
// the block chosen, a 480px card centred under it with "해결된 댓글이 없습니다."
// alone, no field to write in). The address names a comment no longer there,
// which commentLinks.ts learns from the server and notes here before it asks
// for the block's card; the card that comes for it is this one.

const goneLinks = new Set<string>();

/** Notes that `commentId`'s discussion is deleted whole: a card opened for it is this one. */
export function noteGoneDiscussionLink(commentId: string) {
  goneLinks.add(commentId);
}

/** Whether a card asked for at `commentId` is one for a discussion deleted whole. */
export function goneDiscussionLink(commentId: string | undefined) {
  return Boolean(commentId && goneLinks.has(commentId));
}

/** The card under `blockId` saying none of its discussion is left; a press beside it or Escape closes it. */
export function GoneDiscussionCard({ blockId, onClose }: { blockId: string; onClose: () => void }) {
  const { t } = useTranslation("commentsPanel");
  const ref = useRef<HTMLElement>(null);
  const place = useAnchoredPlace(blockId, false, null, null, undefined, ref, ANCHORED_WIDTH);
  const close = useCallback(() => onClose(), [onClose]);
  // The focus stays where it was, with the chosen block, as Notion's. A press beside it goes on to the page whole, as
  // beside the card reading a block's threads (CommentsPanel; Notion's own card for a deleted discussion not reproduced).
  const { zIndex } = useOverlayLayer(true, { kind: "card", ref, restoreFocus: false, initialFocus: "opener", onEscape: close, onOutsidePress: close });
  // Escape closes the card first, the block staying chosen, as Notion's (seen
  // 2026-11-21); the chosen block's own Escape (useBlockSelectionKeys) would
  // let go of it and leave the card open, so the card takes the key before it.
  const layer = useRef(zIndex); layer.current = zIndex;
  useEffect(() => {
    function escape(event: KeyboardEvent) {
      if (event.key !== "Escape" || event.defaultPrevented || imeComposing(event) || layer.current === undefined
        || topLayerZIndex() !== layer.current) return;
      event.preventDefault();
      event.stopPropagation();
      close();
    }
    window.addEventListener("keydown", escape, true);
    return () => window.removeEventListener("keydown", escape, true);
  }, [close]);
  if (typeof document === "undefined") return null;
  return createPortal(
    <aside ref={ref} className={cardStyles.card} role="dialog" aria-label={t("title")} tabIndex={-1}
      style={{ ...place, zIndex, visibility: place ? undefined : "hidden" }}
      data-comments-panel data-comments-mode="block-card" data-discussion-gone="">
      <div className={styles.none} role="status">{t("list.noResolved")}</div>
    </aside>,
    document.body,
  );
}
