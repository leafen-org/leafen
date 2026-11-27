import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { DoubleChevronRight, X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { focusOpenedField } from "../editor/openedFieldCaret";
import { closeDiscussionSide, useDiscussionSideThread } from "./discussionSide";
import styles from "./DiscussionSideView.module.css";

// The peeks a thread can be shown in: their frame is where its view opens.
const PEEK_FRAME = "[data-row-peek-panel]:not([data-mode=\"full\"]), [data-page-peek]";

/**
 * A page title thread opened by its "▯ 열기", as Notion's (seen 2026-11-25
 * in Chrome at 1440, a page's and a database page's discussion, two tabs,
 * and a database page in a side peek): a view of its own headed "토론" — its
 * first comment over a line, every reply under it in its column and the
 * thread's reply row, the caret in it. Opened from a page shown whole it
 * stands beside the page from the window's top, 564px wide, the page giving
 * up that room, closed by "»"; opened in a peek it takes the peek's place,
 * closed by "✕". Escape closes it too, from its empty reply box at once
 * (Notion's, seen 2026-11-25): it is a layer the box's Escape goes on to.
 */
export function DiscussionSideView({ anchor, onClose, children }: {
  /** The page's title discussion it was opened from. */
  anchor: HTMLElement | null;
  onClose(): void;
  children: ReactNode;
}) {
  const { t } = useTranslation(["commentsPanel", "common"]);
  const panelRef = useRef<HTMLElement>(null);
  const [frame] = useState(() => anchor?.closest<HTMLElement>(PEEK_FRAME) ?? null);
  const [place, setPlace] = useState<CSSProperties | undefined>(undefined);

  // In a peek it covers the peek's frame wherever that stands, following it
  // while it still slides in — a reload opens both together (sidePeekMotion.ts).
  useLayoutEffect(() => {
    if (!frame) return undefined;
    const follow = () => {
      const box = frame.getBoundingClientRect();
      setPlace((now) => now && now.top === box.top && now.left === box.left && now.width === box.width && now.height === box.height
        ? now : { top: box.top, left: box.left, width: box.width, height: box.height });
    };
    follow();
    let tick = 0;
    const sliding = () => {
      follow();
      tick = frame.getAnimations().some((motion) => motion.playState === "running") ? window.requestAnimationFrame(sliding) : 0;
    };
    const slide = () => { if (!tick) sliding(); };
    sliding();
    const observer = new ResizeObserver(follow);
    observer.observe(frame);
    window.addEventListener("resize", follow);
    frame.addEventListener("transitionrun", slide);
    frame.addEventListener("transitionend", follow);
    return () => {
      window.cancelAnimationFrame(tick);
      observer.disconnect();
      window.removeEventListener("resize", follow);
      frame.removeEventListener("transitionrun", slide);
      frame.removeEventListener("transitionend", follow);
    };
  }, [frame]);

  // Beside a page shown whole, the page gives up the view's room.
  useEffect(() => {
    const scroll = frame ? null : anchor?.closest<HTMLElement>("[data-page-scroll]");
    if (!scroll) return undefined;
    scroll.dataset.discussionSide = "true";
    return () => { delete scroll.dataset.discussionSide; };
  }, [anchor, frame]);

  // The caret goes into the thread's reply row as it opens; without one, the view takes the focus.
  useEffect(() => focusOpenedField(() => panelRef.current?.querySelector<HTMLElement>("[data-comment-reply-input]")
    ?? panelRef.current), []);
  // A layer, so Escape in it closes it once nothing over it takes the key; a press beside it leaves it open.
  const { zIndex } = useOverlayLayer(true, { kind: "panel", ref: panelRef, restoreFocus: false, initialFocus: "opener", onEscape: onClose });

  const view = (
    <aside ref={panelRef} className={styles.view} data-in-peek={frame ? "true" : undefined} style={{ ...place, zIndex }}
      role="complementary" aria-label={t("commentsPanel:discussions.title")} tabIndex={-1}
      data-discussion-side-view>
      <div className={styles.head}>
        <button type="button" className={styles.close} aria-label={t("common:actions.close")} title={t("common:actions.close")}
          onClick={onClose} data-discussion-side-close>
          {frame ? <X size="regular" aria-hidden="true" /> : <DoubleChevronRight size="regular" aria-hidden="true" />}
        </button>
        <span className={styles.title}>{t("commentsPanel:discussions.title")}</span>
      </div>
      <div className={`${styles.body} nscroll`} role="list" aria-label={t("commentsPanel:discussions.title")}>{children}</div>
    </aside>
  );
  return typeof document === "undefined" ? view : createPortal(view, document.body);
}

/**
 * The thread the address opens in its own view (discussionSide.ts), drawn by
 * the page's title discussion whose threads — resolved ones too — it is among:
 * it stays while the address names it, whether the thread is still under the
 * title or was resolved, and goes with the thread.
 */
export function DiscussionSide<T extends { id: string }>({ threads, owner, placeRef, children }: {
  threads: readonly T[];
  /** The title discussion drawing it, which holds the view (discussionSide.ts). */
  owner: string;
  /** Where that title discussion is drawn: in a peek, or in a page shown whole. */
  placeRef: RefObject<HTMLElement | null>;
  children(thread: T): ReactNode;
}) {
  const thread = useDiscussionSideThread(threads, owner);
  // The place is known once it is drawn, after a reload too.
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  useLayoutEffect(() => {
    const place = thread ? placeRef.current : null;
    if (place !== anchor) setAnchor(place);
  });
  return thread && anchor ? <DiscussionSideView anchor={anchor} onClose={closeDiscussionSide}>{children(thread)}</DiscussionSideView> : null;
}
