"use client";

import { useEffect, useRef, useState, useSyncExternalStore } from "react";
export type PageContentPreviewTransport = (
  pageId: string,
  expectedRevision?: number,
) => Promise<{ pageId: string; previewUrl?: string; revision: number }>;
export type WorkspaceFileHook = (
  value: string | undefined,
  dataPrefixes?: string[],
  options?: { width?: FilePreviewWidth },
) => { url: string; failedStatus: number };
import type { DatabaseRowPageBodyBlock } from "@/databaseRowPageWire.generated";
import { filePreviewWidth, type FilePreviewWidth } from "@/lib/fileUrls";
import { cardPreviewLine, useCardPageContent, type CardPageContent } from "@/lib/cardPageContent";
import { useBackgroundImageFailure } from "@/lib/imageLoadFailure";
import type { DbProperty, Page } from "@/lib/types";
import { CoverCredit } from "../page/CoverCredit";
import { PageIconGlyph } from "../page/PageIcon";
import { FeedPostBlocks } from "./FeedPostBlocks";
import styles from "./CardPreviewMedia.module.css";
import { CARD_PREVIEW_PAGE_CONTENT } from "./cardPreviewModePolicy";
import { cardCoverValue, coverBackground } from "./cardPreviewValue";

const NO_DATABASES = new Map();
const noSubscription: CardPageContent["subscribe"] = () => () => {};

export function CardPreviewMedia({
  row,
  props,
  coverProperty,
  fitImage,
  coverClassName,
  iconClassName,
  iconSize,
  collapseEmptyPageContent = false,
  collapseEmptyCover = false,
  getPageContentPreview,
  useWorkspaceFile,
}: {
  row: Page;
  props: DbProperty[];
  coverProperty?: string;
  fitImage: boolean;
  coverClassName: string;
  iconClassName: string;
  iconSize: number;
  collapseEmptyPageContent?: boolean;
  /** A card whose cover or file is missing draws no preview, as a Notion board's (seen 2026-10-08). */
  collapseEmptyCover?: boolean;
  getPageContentPreview: PageContentPreviewTransport;
  useWorkspaceFile: WorkspaceFileHook;
}) {
  const pageContentMode = coverProperty === CARD_PREVIEW_PAGE_CONTENT;
  // "페이지 콘텐츠" draws the page's first block small (cardPreviewLine), as
  // Notion's (seen 2026-10-08, 2026-11-17), read once the card comes near the
  // screen; a viewer without the reader (a published view) keeps the page's
  // first image.
  const pageContent = useCardPageContent();
  const contentMode = pageContentMode && pageContent !== null;
  const changes = useSyncExternalStore(pageContent?.subscribe ?? noSubscription, () => pageContent?.changes(row.id) ?? 0);
  const element = useRef<HTMLDivElement>(null);
  const [near, setNear] = useState(false);
  const [blocks, setBlocks] = useState<readonly DatabaseRowPageBodyBlock[] | null>(null);
  useEffect(() => {
    if (!contentMode || near) return;
    const target = element.current;
    if (!target || typeof IntersectionObserver === "undefined") {
      setNear(true);
      return;
    }
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) setNear(true);
    }, { rootMargin: "300px" });
    observer.observe(target);
    return () => observer.disconnect();
  }, [contentMode, near]);
  useEffect(() => {
    if (!contentMode || !pageContent) {
      setBlocks(null);
      return;
    }
    if (row.hasContent === false && changes === 0) {
      setBlocks([]);
      return;
    }
    if (!near) return;
    let cancelled = false;
    pageContent.read(row.id, row.blockRevision)
      .then((read) => { if (!cancelled) setBlocks(read); })
      .catch(() => { if (!cancelled) setBlocks([]); });
    return () => { cancelled = true; };
  }, [changes, contentMode, near, pageContent, row.blockRevision, row.hasContent, row.id]);
  const [pageContentCover, setPageContentCover] = useState("");
  const [pageContentResolved, setPageContentResolved] = useState(false);
  useEffect(() => {
    if (!pageContentMode || contentMode) {
      setPageContentCover("");
      setPageContentResolved(false);
      return;
    }
    let cancelled = false;
    setPageContentCover("");
    setPageContentResolved(false);
    void getPageContentPreview(row.id, row.blockRevision)
      .then((result) => {
        if (
          !cancelled
          && result.pageId === row.id
          && (row.blockRevision === undefined || result.revision === row.blockRevision)
        ) {
          setPageContentCover(result.previewUrl ?? "");
          setPageContentResolved(true);
        }
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [contentMode, pageContentMode, row.blockRevision, row.id]);

  const line = contentMode && blocks ? cardPreviewLine(blocks) : null;
  // A page whose first block is a picture shows it across the preview as a cover.
  const lineImage = line && "image" in line ? line.image : "";
  const rawCover = contentMode ? lineImage : pageContentMode
    ? pageContentCover
    : cardCoverValue(row, props, coverProperty);
  const gradientCover = rawCover.startsWith("linear-gradient") ? rawCover : "";
  // A card's cover comes at a card's size.
  const coverFile = useWorkspaceFile(
    gradientCover ? undefined : rawCover,
    ["data:image/"],
    { width: filePreviewWidth(320) },
  );
  const resolvedCover = coverFile.url;
  // A cover that couldn't be loaded, or whose file the server no longer
  // gives, leaves the card's own colour, as Notion's (seen 2026-10-21) — no
  // grey, no icon.
  const failed = useBackgroundImageFailure(gradientCover ? undefined : resolvedCover, coverFile.failedStatus);
  const cover = gradientCover || resolvedCover || (failed ? rawCover : "");

  if (contentMode) {
    // A board's card with nothing to draw has no preview, as Notion's (seen
    // 2026-11-17: a divider, code, an equation, a table, an embed or another
    // block that is not a line of words first, an empty line or a callout
    // with no words — cardPreviewLine); a gallery's keeps its empty one.
    // While its page is read it stands without one too, and takes it once
    // there is a line to draw, so no card shrinks under the others (Notion's,
    // seen 2026-11-17 at 1440 in two ways: a board opened in a new tab, every
    // card 40 high until the pages came, then those with a line 188; cards
    // arriving on an open board 40, then 188 or staying 40). The empty mark
    // takes no room and tells when the card comes near the screen.
    if (collapseEmptyPageContent && !blocks) return <div ref={element} data-page-content-preview="reading" />;
    if (collapseEmptyPageContent && !line) return null;
    if (lineImage) {
      return (
        <div ref={element} className={`${coverClassName} ${styles.cover}`} data-page-content-preview="ready"
          data-has-image={cover ? "true" : undefined} data-fit-image={fitImage ? "true" : undefined}
          data-image-failed={failed ? "true" : undefined}
          style={{ backgroundImage: failed ? undefined : coverBackground(cover) }}>
          {failed ? null : <CoverCredit cover={lineImage} />}
        </div>
      );
    }
    return (
      <div ref={element} className={`${coverClassName} ${styles.pageContent}`} data-page-content-preview={blocks ? "ready" : "reading"}>
        {line && "block" in line ? (
          <div className={styles.blocks} aria-hidden="true" inert>
            <FeedPostBlocks blocks={[line.block]} databases={NO_DATABASES} look="card" />
          </div>
        ) : null}
      </div>
    );
  }
  // A published board's card the same: no preview until its picture is known.
  if (collapseEmptyPageContent && pageContentMode && (!pageContentResolved || !pageContentCover)) return null;
  if (collapseEmptyCover && !pageContentMode && !rawCover) return null;

  return (
    <div
      className={`${coverClassName} ${styles.cover}`}
      data-has-image={cover ? "true" : undefined}
      data-fit-image={fitImage ? "true" : undefined}
      data-image-failed={failed ? "true" : undefined}
      style={{ backgroundImage: failed ? undefined : coverBackground(cover) }}
    >
      {failed ? null : <CoverCredit cover={rawCover} />}
      {!cover && (
        <span className={iconClassName}>
          <PageIconGlyph
            page={{
              icon: row.icon ?? "",
              iconType: row.iconType,
              workspaceId: row.workspaceId,
              kind: row.kind,
              notionIcon: row.notionIcon,
              hasContent: row.hasContent,
            }}
            size={iconSize}
          />
        </span>
      )}
    </div>
  );
}
