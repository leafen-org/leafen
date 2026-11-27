import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import type { FeedPostViewers } from "@/lib/feedPostService";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { menuTimestampLabel } from "@/lib/relativeTime";
import styles from "./FeedPost.module.css";

/**
 * A post's viewers at the right end of its reaction row, as Notion's feed
 * (seen 2026-10-11): the last viewers' faces, overlapping, and how many
 * people viewed it, on posts with and without reactions. Pressed, it lists
 * them (what Notion's opens there wasn't seen). Only those who may read the
 * post's analytics have it.
 */
export function FeedPostViewersButton({ viewers }: { viewers: FeedPostViewers }) {
  const { t } = useTranslation("feedPost");
  const trigger = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const close = () => setOpen(false);
  const placement = usePopoverPlacement(list, open ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { align: "end", gap: 4 },
  ) : null, [open], undefined, { kind: "menu", onEscape: close, onOutsidePress: close, inside: () => [trigger.current] });
  return (
    <>
      <button ref={trigger} type="button" className={styles.viewers} aria-haspopup="dialog" aria-expanded={open}
        aria-label={t("viewers", { count: viewers.count })} data-feed-post-viewers={viewers.count}
        onClick={() => setOpen((shown) => !shown)}>
        {viewers.recent.length ? (
          <span className={styles.faces} aria-hidden="true">
            {viewers.recent.map((viewer) => (
              <PersonAvatar key={viewer.userId} className={styles.face} userId={viewer.userId} name={viewer.displayName} size={20} />
            ))}
          </span>
        ) : null}
        <span aria-hidden="true">{viewers.count}</span>
      </button>
      {open ? createPortal(
        <div ref={list} role="dialog" aria-label={t("recentViewers")} className={styles.viewersList} data-feed-post-viewers-list
          style={{ ...popoverPlacementStyle(placement), position: "fixed", right: "auto", bottom: "auto", visibility: placement ? "visible" : "hidden" }}>
          <strong>{t("recentViewers")}</strong>
          {viewers.recent.map((viewer) => (
            <span key={viewer.userId} className={styles.viewer}>
              <PersonAvatar userId={viewer.userId} name={viewer.displayName} size={20} />
              <span>{viewer.displayName || t("unknownAuthor")}</span>
              <time dateTime={viewer.lastViewedAt}>{menuTimestampLabel(viewer.lastViewedAt)}</time>
            </span>
          ))}
        </div>,
        document.body,
      ) : null}
    </>
  );
}
