import type { ReactNode } from "react";
import type { DatabaseRowPageBodyBlock } from "@/databaseRowPageWire.generated";
import { calloutGlyphSize, storedBlockIcon } from "../editor/blockIconModel";
import { blockColorStyle } from "../editor/blockHandleModel";
import { PageIconGlyph } from "../page/PageIcon";
import { useProductPages } from "../product/productPagesContext";
import styles from "./FeedPostCallout.module.css";

/**
 * A callout in a feed post or a card's "페이지 콘텐츠", as its page draws it
 * (Notion's both, seen 2026-11-17): its icon — the default bulb when it has
 * none set, nothing when taken away — and its words, then what is inside it
 * (a callout made here keeps its words in its first line), in its outlined
 * box, or on its background color.
 */
export function FeedPostCallout({ block, text, className, textClassName, children }: {
  block: DatabaseRowPageBodyBlock;
  text: string;
  /** The card's own place for the box. */
  className?: string;
  /** The card's own paragraph for its words. */
  textClassName?: string;
  children?: ReactNode;
}) {
  const workspaceId = useProductPages()?.workspaceId;
  const icon = storedBlockIcon(block);
  const color = blockColorStyle(block.content.color);
  const tint = block.content.color === "gray_background" ? "var(--c-callout-bg-gray)" : color?.background;
  return (
    <div className={className ? `${styles.callout} ${className}` : styles.callout} data-tinted={tint ? "true" : undefined}
      style={{ background: tint, color: color?.color }}>
      {block.content.iconType === "none" ? null : (
        <span className={styles.icon} aria-hidden="true" data-emoji={!icon || icon.iconType === "emoji" ? "true" : undefined}>
          {icon ? <PageIconGlyph page={{ ...icon, workspaceId }} {...calloutGlyphSize(icon.iconType)} fallback="💡" />
            : <span data-emoji-glyph="">💡</span>}
        </span>
      )}
      <div className={styles.words}>
        {text || !children ? <p className={textClassName}>{text || "\u200b"}</p> : null}
        {children}
      </div>
    </div>
  );
}
