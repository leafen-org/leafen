import type { DatabaseRowPageBodyBlock } from "@/databaseRowPageWire.generated";
import { CaretRightFill, DatabaseCylinderIcon } from "@/icons/leafen";
import { feedPostBlockText } from "@/lib/feedPostService";
import { filePreviewWidth, useWorkspaceFile } from "@/lib/fileUrls";
import { useImageLoadFailure } from "@/lib/imageLoadFailure";
import { pageHref } from "@/lib/navigation";
import { pushRoute } from "@/lib/router";
import type { PageHistoryDatabase } from "@/pageHistoryWire.generated";
import { ImageLoadFailure } from "../editor/ImageLoadFailure";
import { nativeWorkspaceFileLocator } from "@/lib/workspaceFileTransport";
import { useProductPages } from "../product/productPagesContext";
import { PageIconGlyph } from "../page/PageIcon";
import { CreditedPicture } from "../page/CoverCredit";
import { PageHistoryDatabaseTable } from "../PageHistoryDatabaseTable";
import cardStyles from "./CardPreviewBlocks.module.css";
import feedStyles from "./FeedPost.module.css";
import { FeedPostCallout } from "./FeedPostCallout";
import { FeedPostToggle } from "./FeedPostToggle";
import { TOGGLE_BLOCK_TYPES } from "@/lib/blockSemantics";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { plainTitleOf } from "@/lib/closedMentions";

type Node = { block: DatabaseRowPageBodyBlock; number?: number; children: Node[] };
/** A feed post's body, or a card's "페이지 콘텐츠": one block at a 12px size, a toggle shut. */
export type PostBlocksLook = "feed" | "card";

/** The blocks as their page holds them: each under its parent, in order, numbered lists counted. */
function blockTree(blocks: readonly DatabaseRowPageBodyBlock[]): Node[] {
  const ids = new Set(blocks.map((block) => block.id));
  const childrenOf = new Map<string | null, DatabaseRowPageBodyBlock[]>();
  for (const block of blocks) {
    const parent = block.parentId && ids.has(block.parentId) ? block.parentId : null;
    childrenOf.set(parent, [...(childrenOf.get(parent) ?? []), block]);
  }
  const build = (parent: string | null, seen: Set<string>): Node[] => {
    let number = 0;
    return (childrenOf.get(parent) ?? []).filter((block) => !seen.has(block.id)).map((block) => {
      number = block.type === "numbered_list_item" ? number + 1 : 0;
      const inside = new Set(seen).add(block.id);
      return { block, number: number || undefined, children: build(block.id, inside) };
    });
  };
  return build(null, new Set());
}

/**
 * A feed post's body, as Notion's feed draws it (seen 2026-10-04): the page's
 * first blocks in their order with what is inside them under them — a toggle
 * open or shut as its page holds it (FeedPostToggle) — its sub-pages as links
 * and its databases as their tables, read-only.
 */
export function FeedPostBlocks({ blocks, databases, look = "feed", saveToggle }: {
  blocks: readonly DatabaseRowPageBodyBlock[];
  /** The page's databases, by their block, as far as they were read. */
  databases: ReadonlyMap<string, PageHistoryDatabase>;
  /** "card" draws a card's "페이지 콘텐츠": its first block without what is inside it, a toggle shut (Notion's ▶, seen 2026-11-17). */
  look?: PostBlocksLook;
  /** Writes a toggle opened or shut in the post on its page (FeedPostToggle); none for a reader who can't. */
  saveToggle?: SaveToggle;
}) {
  return <>{blockTree(blocks).map((node) => <PostNode key={node.block.id} node={node} databases={databases} look={look} saveToggle={saveToggle} />)}</>;
}

/** Writes a block's toggle shut (true) or open on its page, from its content as last known; answers the content written. */
export type SaveToggle = (block: DatabaseRowPageBodyBlock, collapsed: boolean, known: Record<string, unknown>) => Promise<Record<string, unknown>>;

function PostNode({ node, databases, look, saveToggle }: {
  node: Node; databases: ReadonlyMap<string, PageHistoryDatabase>; look: PostBlocksLook; saveToggle?: SaveToggle;
}) {
  // What is inside a callout stands in its box, under its words.
  if (node.block.type === "callout") {
    const card = look === "card";
    return (
      <FeedPostCallout block={node.block} text={feedPostBlockText(node.block)}
        className={card ? cardStyles.callout : undefined} textClassName={card ? cardStyles.paragraph : undefined}>
        {node.children.length ? node.children.map((child) => <PostNode key={child.block.id} node={child} databases={databases} look={look} saveToggle={saveToggle} />) : null}
      </FeedPostCallout>
    );
  }
  const line = <PostBlock block={node.block} number={node.number} database={databases.get(node.block.id)} look={look} />;
  const children = node.children.length ? (
    <div className={feedStyles.children} data-feed-block-children={node.block.id}>
      {node.children.map((child) => <PostNode key={child.block.id} node={child} databases={databases} look={look} saveToggle={saveToggle} />)}
    </div>
  ) : null;
  if (look === "feed" && TOGGLE_BLOCK_TYPES.has(node.block.type as never)) {
    const shut = node.block.content.collapsed === true;
    // Drawn again as its page holds it once the page changed it.
    const block = node.block;
    return <FeedPostToggle key={String(shut)} blockId={block.id} shut={shut} content={block.content} line={line}
      save={saveToggle ? (collapsed, known) => saveToggle(block, collapsed, known) : undefined}
      level={block.type.startsWith("toggle_heading_") ? Number(block.type.at(-1)) : undefined}>{children}</FeedPostToggle>;
  }
  return <>{line}{children}</>;
}

function PostBlock({ block, number, database, look }: {
  block: DatabaseRowPageBodyBlock; number?: number; database?: PageHistoryDatabase; look: PostBlocksLook;
}) {
  const pages = useProductPages()?.pages;
  const text = feedPostBlockText(block);
  const card = look === "card";
  const styles: Partial<Record<string, string>> = card ? cardStyles : feedStyles;
  // A card's toggle heading draws its caret shut, as its toggle does (Notion's, seen 2026-11-17).
  const caret = card && block.type.startsWith("toggle_heading_")
    ? <span className={cardStyles.headingCaret} aria-hidden="true"><CaretRightFill /></span> : null;
  switch (block.type) {
    case "heading_1":
    case "toggle_heading_1":
      return <h3 className={styles.heading1}>{caret}{text}</h3>;
    case "heading_2":
    case "toggle_heading_2":
      return <h4 className={styles.heading2}>{caret}{text}</h4>;
    case "heading_3":
    case "heading_4":
    case "toggle_heading_3":
    case "toggle_heading_4":
      return <h5 className={styles.heading3}>{caret}{text}</h5>;
    case "bulleted_list_item":
      return <p className={styles.bullet}>{text}</p>;
    case "toggle":
      // A feed post's caret is its toggle's own button (FeedPostToggle).
      return card ? <p className={styles.toggle}><span className={styles.marker} aria-hidden="true"><CaretRightFill /></span>{text}</p>
        : <p>{text}</p>;
    case "numbered_list_item":
      return <p className={styles.numbered}><span className={styles.marker} aria-hidden="true">{number ?? 1}.</span>{text}</p>;
    case "to_do":
      return (
        <p className={styles.todo} data-checked={block.content.checked === true || undefined}>
          <input type="checkbox" checked={block.content.checked === true} readOnly tabIndex={-1} aria-hidden="true" />
          <span>{text}</span>
        </p>
      );
    case "quote":
      return <blockquote className={styles.quote}>{text}</blockquote>;
    case "code":
      return <pre className={styles.code}>{text}</pre>;
    case "divider":
      return <hr className={styles.divider} />;
    case "image":
      return <PostImage fileId={typeof block.content.fileId === "string" ? block.content.fileId : ""}
        url={typeof block.content.url === "string" ? block.content.url : ""} />;
    case "child_page":
    case "link_to_page": {
      const pageId = String(block.content.childPageId ?? block.content.pageId ?? block.id);
      const page = pages?.find((candidate) => candidate.id === pageId);
      const title = plainTitleOf(page) || text || untitledPageDisplayTitle(page);
      return (
        <button type="button" className={styles.pageLink} data-feed-page-link={pageId} onClick={() => pushRoute(pageHref(pageId))}>
          <PageIconGlyph page={page ?? { icon: "", iconType: "none" }} size={18} />
          <span>{title}</span>
        </button>
      );
    }
    case "inline_database":
      return database ? (
        <div className={styles.database} data-feed-database={block.id}>
          <p className={styles.databaseTitle}><DatabaseCylinderIcon size="small" aria-hidden="true" />{plainTitleOf(database) || untitledPageDisplayTitle("database")}</p>
          <PageHistoryDatabaseTable database={database} />
        </div>
      ) : null;
    default:
      return text ? <p className={styles.paragraph}>{text}</p> : null;
  }
}

/**
 * An image block, its file read as the page reads it (useMediaBlockUrl): an
 * uploaded picture by its file's ID, a linked one by its address. One that
 * couldn't be loaded shows the page's failure state, as Notion's feed does
 * (seen 2026-10-21), which a card's preview leaves blank.
 */
function PostImage({ fileId, url }: { fileId: string; url: string }) {
  const address = fileId ? nativeWorkspaceFileLocator(fileId) : url;
  const file = useWorkspaceFile(address || undefined, ["data:image/"], { width: filePreviewWidth(600) });
  const src = file.url;
  const failure = useImageLoadFailure(src, file.failedStatus);
  if (failure.failed) return <ImageLoadFailure url={src} refused={file.failedStatus} />;
  return src ? <CreditedPicture cover={url}><img className={feedStyles.image} src={src} alt="" loading="lazy" onError={failure.fail} /></CreditedPicture> : null;
}
