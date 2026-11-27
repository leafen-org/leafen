import { Fragment, useEffect, useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { DatabaseCylinderIcon } from "@/icons/leafen";
import { hoverCardAnchor, listenHoverCardKeys } from "@/lib/hoverCard";
import { placeAround, type PopoverRect, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PagePreviewImage, SearchPagePreview, type PreviewLineBlock } from "@/components/search/SearchPagePreview";
import { TitleWithCustomEmoji, type NamedTitle } from "@/components/page/TitleWithCustomEmoji";
import type { Place } from "@/lib/pagePath";
import type { Page } from "@/types";
import styles from "./PageMentionPreview.module.css";

// Notion's card, and its tip for one in the trash, come some 420 ms after
// the pointer comes onto the mention (seen 2026-11-24 in Chrome at 1440, two
// tabs: 419–422 ms over eleven, cards and tips alike): a 400 ms wait and the
// drawing.
const SHOW_AFTER_MS = 400;
const CARD_GAP = 6;
// A data source's cylinder a size up and centred in the icon's 26px, as a database's table (PageIcon.tsx).
const CYLINDER_PIXELS = Math.round((26 * 20) / 18);
// A page block's or a link to a page's row shows its page's card too (Notion,
// seen 2026-10-31), and a data source's mention its own (seen 2026-11-24).
const MENTION_SELECTOR = 'a[data-mention="page"][data-page-id], [data-mention="database"][data-database-id], '
  + '[data-mention="database"][data-mention-data-source]:not([data-mention-data-source=""]), a[data-page-card][data-page-id]';

export type MentionKind = "page" | "database" | "dataSource";

/** The mention a card shows, and the place the card goes by: below it, or above it near the window's foot. */
export type ShownMention = {
  kind: MentionKind;
  id: string;
  element: HTMLElement;
  anchor: PopoverRect;
};

export type MentionCard = {
  icon: Pick<Page, "icon" | "iconType" | "workspaceId"> & Partial<Pick<Page, "kind" | "hasContent">>;
  /** What stands for an icon it has none of: a page's, a database's table, a data source's cylinder. */
  fallback: "file" | "database" | "dataSource";
  location: Place;
  title: string;
  /** What names the title, for its custom emoji to be drawn (TitleWithCustomEmoji). */
  named?: NamedTitle | null;
  /** The titles holding a custom emoji, by their plain title, for the place's to be drawn. */
  emojiTitles?: ReadonlyMap<string, NamedTitle>;
  lines: readonly string[];
  /** The page's first blocks, drawn small; without them, its first lines. */
  blocks?: readonly PreviewLineBlock[];
  /** Room for a body whose blocks are still being read: the page holds something. */
  reserve?: boolean;
  table: boolean;
};

/**
 * The page or database mention the pointer rests on, as Notion's cards
 * follow it (seen 2026-09-30): after 400 ms, while the pointer stays on the
 * mention. It goes at once as the pointer leaves the mention, as Notion's
 * card and tip do (seen 2026-11-24 in Chrome at 1440, dpr 1, two tabs: 5–11
 * ms) — for anywhere else, the gap below it, the card itself, which takes no
 * pointer, or another mention, whose own comes after its own wait; a press
 * or a scroll ends it, a key does not (listenHoverCardKeys).
 */
export function useMentionHover(): { shown: ShownMention | null; cardRef: RefObject<HTMLDivElement | null> } {
  const [shown, setShown] = useState<ShownMention | null>(null);
  const shownRef = useRef<ShownMention | null>(null);
  const cardRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let showTimer = 0;
    // The mention waited on, which the pointer's coming onto its own pieces doesn't wait on again.
    let waited: HTMLElement | null = null;
    const show = (next: ShownMention | null) => {
      shownRef.current = next;
      setShown(next);
    };
    function onOver(event: MouseEvent) {
      const target = event.target instanceof Element ? event.target : null;
      const mention = target?.closest<HTMLElement>(MENTION_SELECTOR);
      const kind: MentionKind = mention?.dataset.mention !== "database" ? "page"
        : mention.dataset.mentionDataSource ? "dataSource" : "database";
      const id = kind === "database" ? mention?.dataset.databaseId
        : kind === "dataSource" ? mention?.dataset.mentionDataSource : mention?.dataset.pageId;
      if (mention && id && (mention === waited || shownRef.current?.kind === kind && shownRef.current.id === id)) return;
      window.clearTimeout(showTimer);
      waited = null;
      if (shownRef.current) show(null);
      if (!mention || !id) return;
      waited = mention;
      showTimer = window.setTimeout(() => {
        waited = null;
        if (!mention.isConnected) return;
        show({ kind, id, element: mention, anchor: hoverCardAnchor(mention)! });
      }, SHOW_AFTER_MS);
    }
    function hideNow() {
      window.clearTimeout(showTimer);
      waited = null;
      if (shownRef.current) show(null);
    }
    document.addEventListener("mouseover", onOver);
    document.addEventListener("mousedown", hideNow, true);
    document.addEventListener("click", hideNow, true);
    const keys = listenHoverCardKeys(() => shownRef.current?.element ?? null, hideNow);
    window.addEventListener("scroll", hideNow, true);
    return () => {
      window.clearTimeout(showTimer);
      document.removeEventListener("mouseover", onOver);
      document.removeEventListener("mousedown", hideNow, true);
      document.removeEventListener("click", hideNow, true);
      keys();
      window.removeEventListener("scroll", hideNow, true);
    };
  }, []);

  return { shown, cardRef };
}

/**
 * A mention's card, as Notion's (seen 2026-10-31, the same for a mention and
 * a page block): 260px, the icon 26px over the place and the title, then
 * the page's first blocks drawn small — or its first picture instead, as
 * Notion's (seen 2026-10-31) — for a database a picture of a table; no
 * buttons. A page with nothing in it ends 28px under its title (Notion's,
 * measured 2026-11-13: 119.4px tall, 179.4 with a body of any length).
 */
export function MentionPreviewCard({ shown, card, cardRef, marginY }: {
  shown: ShownMention;
  card: MentionCard;
  cardRef: RefObject<HTMLDivElement | null>;
  /** How near the window's top it may go, where not the usual margin (a tab's card, under the strip). */
  marginY?: number;
}) {
  // Whole or not at all: near the window's foot the card goes above, as
  // Notion's, rather than shrinking below — even once its blocks come in.
  const place = usePopoverPlacement(cardRef, (size, viewport) => placeAround(
    shown.anchor, size, viewport, { gap: CARD_GAP, minHeight: size.height, ...(marginY === undefined ? {} : { marginY }) },
  ), [shown, marginY], undefined, { kind: "hovercard" });
  if (typeof document === "undefined") return null;
  const blocks: readonly PreviewLineBlock[] = card.blocks ?? card.lines.map((text) => ({ type: "paragraph", text, depth: 0 }));
  const picture = blocks.find((block) => block.type === "image" && (block.fileId || block.url));
  const body = !card.table && !picture && (blocks.length > 0 || card.reserve === true);
  return createPortal(
    <div ref={cardRef} className={styles.card} role="tooltip" data-page-mention-preview={shown.id}
      data-mention-kind={shown.kind} data-bare={!card.table && !picture && !body ? "true" : undefined} style={popoverPlacementStyle(place)}>
      <span className={styles.icon} aria-hidden="true"><PageIconGlyph page={card.icon} size={26}
        fallback={card.fallback === "dataSource" ? <DatabaseCylinderIcon pixels={CYLINDER_PIXELS} style={{ margin: (26 - CYLINDER_PIXELS) / 2 }}
          aria-hidden="true" /> : card.fallback} /></span>
      {card.location.length ? <span className={styles.location}>{card.location.map((part, index) => (
        <Fragment key={index}>{index > 0 ? <span className={styles.separator}>/</span> : null}<span className={styles.crumb}>
          {part === null ? "..." : <TitleWithCustomEmoji text={part} named={card.emojiTitles?.get(part)} />}</span></Fragment>
      ))}</span> : null}
      <span className={styles.title}><TitleWithCustomEmoji text={card.title} named={card.named} workspaceId={card.icon.workspaceId} /></span>
      {!card.table && picture ? <PagePreviewImage block={picture} className={styles.picture} frameClassName={styles.pictureFrame} /> : null}
      {body ? (
        <span className={styles.body}>
          {blocks.length > 0 ? <SearchPagePreview blocks={blocks} workspaceId={card.icon.workspaceId} variant="card" /> : null}
        </span>
      ) : null}
      {card.table ? (
        <span className={styles.table} aria-hidden="true">
          <span className={styles.tableNew} />
          {[0, 1, 2, 3].map((row) => <span key={row} className={styles.tableRow} />)}
        </span>
      ) : null}
    </div>,
    document.body,
  );
}
