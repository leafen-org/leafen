import { useEffect, useState } from "react";
import type { GraphBlock } from "@/contentGraph";
import { blockItemText } from "./blockItemLabels";
import { BlockEmptyLinkPane, BlockEmptyPopover, BlockEmptyRow } from "./BlockEmptyPopover";
import { emptyRowMark } from "./emptyRowMarks";
import { readBookmarkMetadata, UNREAD_BOOKMARK } from "./bookmarkMetadata";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { BookmarkBlockTools } from "./ImageBlockTools";
import { blockWebAddress, givenWebLink, safeMediaUrl } from "./mediaBlockUrl";
import styles from "./editorStyles.generated.module.css";

// A card without its page's name is named by its host, "www." and all, as
// Notion's (seen 2026-11-11: "www.wikipedia.org", "www.example.invalid").
function bookmarkHost(url: string) {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}

/**
 * Keeps what a bookmark's page shows on the block, once (bookmarkMetadata.ts),
 * read at the address it opens and kept for the address it holds. One whose
 * address the block API wrote holds an empty read for it, and stays Notion's
 * plain card (plainNotionAPIBookmark).
 */
function useBookmarkMetadata(adapter: NativeEditorGraphAdapter | undefined, block: GraphBlock, href: string, address: string) {
  const readFor = typeof block.content.metadataUrl === "string" ? block.content.metadataUrl : "";
  useEffect(() => {
    // Read again once for a card kept before its picture was read (2026-11-05).
    if (!adapter || adapter.readOnly || !href || ((readFor === address || readFor === href) && typeof block.content.imageUrl === "string")) return;
    const controller = new AbortController();
    void readBookmarkMetadata(adapter, href, controller.signal).then((found) => {
      const live = adapter.graph.blocks.find((candidate) => candidate.id === block.id);
      if (!found || !live || live.content.url !== block.content.url || !adapter.isCurrent() || adapter.signal.aborted) return;
      const filled = { ...found, metadataUrl: address };
      if (adapter.fillBlock) adapter.fillBlock(live, filled);
      else adapter.updateBlock(live, filled);
    });
    return () => controller.abort();
  }, [adapter, block.id, readFor, href, address]);
}

/**
 * A bookmark: an empty one takes a web address, and a filled one is
 * Notion's card (seen 2026-10-17): its page's name, up to two lines of its
 * description and its address, the whole card a link. Its address is copied
 * and the bookmark made a mention from the block's menu, as Notion's. Given
 * its address it stays where its line was, nothing after it and no caret
 * (Notion's, 2026-11-01).
 */
export function BookmarkBlock({ adapter, block }: { adapter?: NativeEditorGraphAdapter; block: GraphBlock }) {
  const storedUrl = typeof block.content.url === "string" ? block.content.url : "";
  // The link as it was given (givenWebLink), which Notion's card shows and reads (seen 2026-11-06:
  // "https://example.com/" and "https://example.org" each as typed). It opens as a text link does,
  // one without its scheme over https and never a page of this app (blockWebAddress).
  const url = storedUrl.trim();
  // A mail address's card is named by its domain and opens the address, as
  // Notion's (seen 2026-11-11: "b.co" over "mailto:a@b.co"); nothing is read for it.
  const mailDomain = /^mailto:[^@\s]+@([^?#/\s]+)/iu.exec(url)?.[1] ?? "";
  const href = mailDomain ? url : blockWebAddress(url) && /^https?:/iu.test(url) ? url : blockWebAddress(url);
  const [iconFailed, setIconFailed] = useState("");
  const [pictureFailed, setPictureFailed] = useState("");
  useBookmarkMetadata(adapter, block, mailDomain ? "" : href, url);
  const editable = Boolean(adapter && !adapter.readOnly);

  // Words that are no address, which Notion's API keeps, are its card's one
  // line, opening nothing, as Notion's (seen 2026-11-10: "not a url", "javascript:…").
  if (url && !href) {
    return <div className={styles.bookmarkWrap} contentEditable={false}>
      <div className={styles.bookmarkCard} data-bookmark-card data-bookmark-unopened>
        <span className={styles.bookmarkUrl}><span>{url}</span></span>
      </div>
      {editable ? <BookmarkBlockTools block={block} /> : null}
    </div>;
  }
  if (!url) {
    if (!editable || !adapter) {
      return <BlockEmptyRow name="bookmark" icon={emptyRowMark("bookmark")} label={blockItemText("blockEmpty.bookmark.row")} />;
    }
    // Notion's "북마크 추가" row and its link window (seen 2026-10-18), on a phone its "북마크" screen (2026-11-15).
    return (
      <BlockEmptyPopover blockId={block.id} name="bookmark" icon={emptyRowMark("bookmark")}
        label={blockItemText("blockEmpty.bookmark.row")} phoneTitle={blockItemText("bookmark.label")}>
        {() => (
          <BlockEmptyLinkPane placeholder={blockItemText("blockEmpty.bookmark.placeholder")}
            submit={blockItemText("blockEmpty.bookmark.submit")} hint={blockItemText("blockEmpty.bookmark.hint")}
            initial={storedUrl} onLink={(value) => {
              const normalized = givenWebLink(value);
              if (!normalized) return blockItemText("bookmark.invalidLink");
              adapter.updateBlock(block, { ...UNREAD_BOOKMARK, url: normalized });
              return "";
            }} />
        )}
      </BlockEmptyPopover>
    );
  }
  const title = typeof block.content.title === "string" && block.content.title.trim() ? block.content.title : mailDomain || bookmarkHost(href);
  const description = typeof block.content.description === "string" ? block.content.description.trim() : "";
  const iconUrl = typeof block.content.iconUrl === "string" && /^https?:/iu.test(block.content.iconUrl) ? block.content.iconUrl : "";
  // The address as it was given: a bare site without the "/" its parse adds.
  const imageUrl = typeof block.content.imageUrl === "string" && /^https?:/iu.test(block.content.imageUrl) ? block.content.imageUrl : "";
  const picture = imageUrl && pictureFailed !== imageUrl ? imageUrl : "";
  const icon = iconUrl && iconFailed !== iconUrl ? iconUrl : "";
  const words = <>
    <span className={styles.bookmarkTitle} data-bookmark-title>{title}</span>
    {description ? <span className={styles.bookmarkDescription} data-bookmark-description>{description}</span> : null}
  </>;
  return (
    <div className={styles.bookmarkWrap} contentEditable={false}>
      <a className={styles.bookmarkCard} href={href} target="_blank" rel="noreferrer" data-block-media-link="true" data-bookmark-card
        data-picture={picture ? "true" : undefined} aria-label={blockItemText("bookmark.openNamed", { title })}>
        {/* A page with a picture shows it at the card's left and its icon over its name, as Notion's (seen 2026-11-05). */}
        {picture ? <>
          <img className={styles.bookmarkPicture} src={picture} alt="" referrerPolicy="no-referrer" loading="lazy"
            onError={() => setPictureFailed(picture)} />
          <span className={styles.bookmarkText}>
            {icon ? <img className={styles.bookmarkSiteIcon} src={icon} alt="" width={21} height={21} referrerPolicy="no-referrer"
              loading="lazy" onError={() => setIconFailed(icon)} /> : null}
            {words}
            <span className={styles.bookmarkUrl}><span>{url}</span></span>
          </span>
        </> : <>
          {/* Without a picture too, the page's icon sits over its name and its
              address goes alone, as Notion's (seen 2026-11-07: 21px icon, name
              26px lower, card 163 high where it was 137). */}
          {icon ? <img className={styles.bookmarkSiteIcon} src={icon} alt="" width={21} height={21} referrerPolicy="no-referrer"
            loading="lazy" onError={() => setIconFailed(icon)} /> : null}
          {words}
          <span className={styles.bookmarkUrl}><span>{url}</span></span>
        </>}
      </a>
      {editable ? <BookmarkBlockTools block={block} /> : null}
    </div>
  );
}

// A link preview keeps the URL a Notion link preview was made from. Leafen
// shows it as a link card; it does not fetch the provider's live preview.
export function LinkPreviewBlock({ block }: { block: GraphBlock }) {
  const url = safeMediaUrl(block.content.url);
  return <div className={styles.bookmarkWrap} data-link-preview="true">{url ? <a className={styles.bookmarkCard} href={url} target="_blank" rel="noreferrer">
    <span className={styles.bookmarkContent}><strong className={styles.bookmarkTitle}>{linkPreviewTitle(url)}</strong>
      <span className={styles.bookmarkDescription}>{blockItemText("linkPreview.label")}</span><span className={styles.bookmarkUrl}>{url}</span></span>
  </a> : <div className={styles.bookmarkEmpty}>{blockItemText("linkPreview.label")}</div>}</div>;
}

function linkPreviewTitle(url: string) {
  try {
    const parsed = new URL(url);
    const path = parsed.pathname.replace(/\/+$/u, "");
    return `${parsed.hostname.replace(/^www\./iu, "")}${path}`;
  } catch {
    return url;
  }
}
