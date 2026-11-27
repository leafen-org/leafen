import { type ReactNode, type RefObject, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowDown } from "@/icons/leafen";
import styles from "./FeedPostBodyBox.module.css";

/**
 * A feed post's body box, as Notion's feed draws it (seen 2026-11-17 in two
 * tabs, at 1440 and at 1024: the box at most 500px high, its overflow
 * hidden): a body taller than the box ends in a 30px fade into the post's
 * ground, 28px up into the box, with "↓ 더 보기" in its middle; pressing it
 * shows the whole body in place, the fade gone. A body that fits has none.
 * `className` draws the box (FeedPost's .body, where its height is set).
 */
export function FeedPostBodyBox({ className, children }: { className: string; children: ReactNode }) {
  const { t } = useTranslation("feedPost");
  const box = useRef<HTMLDivElement>(null);
  const [whole, setWhole] = useState(false);
  const over = useTallerThanBox(box, !whole);
  return (
    <div className={styles.frame}>
      <div ref={box} className={className} data-feed-post-body data-whole={whole ? "true" : undefined}>{children}</div>
      {over && !whole ? (
        <div className={styles.fade} data-feed-post-more>
          <button type="button" className={styles.more} onClick={() => setWhole(true)}>
            <ArrowDown size="micro" aria-hidden="true" />{t("showMore")}
          </button>
        </div>
      ) : null}
    </div>
  );
}

/**
 * Whether what `box` holds is taller than the box, measured as the box or a
 * block in it is laid out anew — a block come or gone, a toggle opened — by a
 * ResizeObserver, whose answers come after layout, before drawing.
 */
function useTallerThanBox(boxRef: RefObject<HTMLElement | null>, enabled: boolean) {
  const [over, setOver] = useState(false);
  useLayoutEffect(() => {
    const box = boxRef.current;
    if (!box || !enabled) return undefined;
    const measure = () => setOver(box.scrollHeight - box.clientHeight > 1);
    const sized = new ResizeObserver(measure);
    sized.observe(box);
    for (const block of box.children) sized.observe(block);
    const blocks = new MutationObserver((records) => {
      for (const record of records) {
        for (const node of record.addedNodes) if (node instanceof Element) sized.observe(node);
        for (const node of record.removedNodes) if (node instanceof Element) sized.unobserve(node);
      }
      measure();
    });
    blocks.observe(box, { childList: true });
    return () => {
      blocks.disconnect();
      sized.disconnect();
    };
  }, [boxRef, enabled]);
  return over;
}
