import { useMemo, useRef, type MouseEvent } from "react";
import type { GraphBlock } from "@/contentGraph";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { databasePageHref } from "@/lib/databasePageLink";
import { PageTitleText } from "@/components/page/TitleWithCustomEmoji";
import { pushRoute } from "@/lib/router";
import { blockItemText } from "./blockItemLabels";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { PageOutlineLink, usePageOutlineNavigation } from "./usePageOutlineNavigation";
import styles from "./editorStyles.generated.module.css";
import {
  pageBreadcrumbChain,
} from "./pageOutlineModel";

// The breadcrumb follows the page's current place in the workspace; content
// saved without it, as on a public page, keeps the path it was saved with.
export function BreadcrumbBlock({ adapter, block }: { adapter?: NativeEditorGraphAdapter; block: GraphBlock }) {
  const pagesById = adapter?.mentionSnapshot?.pagesById;
  const pages = useMemo(
    () => (adapter && pagesById ? pageBreadcrumbChain(adapter.pageId, pagesById) : []),
    [adapter, pagesById],
  );
  if (pages.length === 0) {
    const segments = Array.isArray(block.content.segments) ? block.content.segments.map(String) : [];
    return <nav className={styles.breadcrumbBlock} aria-label={blockItemText("breadcrumb.label")}>{segments.length ? segments.map((segment, index) => (
      <span className={styles.breadcrumbSegment} key={`${segment}-${index}`}><span className={styles.breadcrumbItem}>{segment}</span>
        {index < segments.length - 1 ? <span className={styles.breadcrumbSeparator}>/</span> : null}</span>
    )) : <span className={styles.breadcrumbEmpty}>{blockItemText("breadcrumb.empty")}</span>}</nav>;
  }
  // A full-page database's piece names the view it opens at, as Notion's
  // breadcrumb block does (seen 2026-11-25: /p/<database>?v=<the view last
  // chosen there>, as its path's piece, seen twice; databasePageHref).
  const userId = adapter?.session?.user.id;
  const open = (href: string, event: MouseEvent<HTMLAnchorElement>) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    pushRoute(href);
  };
  return <nav className={styles.breadcrumbBlock} contentEditable={false} aria-label={blockItemText("breadcrumb.label")}>
    {pages.map((page, index) => {
      const last = index === pages.length - 1;
      // A page with its own icon shows it before its name; one without shows none (Notion, seen 2026-10-31).
      const icon = page.icon && page.iconType ? <span className={styles.breadcrumbIcon} aria-hidden="true">
        <PageIconGlyph page={{ icon: page.icon, iconType: page.iconType, workspaceId: page.workspaceId }} size={16} fallback="none" /></span> : null;
      return <span key={page.id} className={styles.breadcrumbSegment}>
        <a className={styles.breadcrumbItem} data-current={last ? "true" : undefined} data-has-icon={icon ? "true" : undefined}
          href={databasePageHref(page, userId)} onClick={(event) => open(databasePageHref(page, userId), event)}>{icon}<span><PageTitleText page={page} /></span></a>
        {last ? null : <span className={styles.breadcrumbSeparator}>/</span>}
      </span>;
    })}
  </nav>;
}

/**
 * A table of contents in its block's color, as Notion draws it (seen
 * 2026-11-10: "default" the page's text color, red red); one with no color
 * of its own gray, the color Notion gives a new one.
 */
export function TableOfContentsBlock({ adapter, color }: { adapter: NativeEditorGraphAdapter; color?: string }) {
  const host = useRef<HTMLElement>(null);
  const {headings,activeId,jump} = usePageOutlineNavigation(adapter,host);
  return <nav ref={host} className={styles.toc} contentEditable={false} aria-label={blockItemText("toc.label")} data-table-of-contents
    data-toc-color={color || undefined}>
    {headings.length===0 ? <div className={styles.tocEmpty}>{blockItemText("toc.empty")}</div>
      : headings.map(heading => <PageOutlineLink key={heading.block.id} className={styles.tocItem} adapter={adapter}
          heading={heading} active={activeId===heading.block.id} onJump={jump} />)}
  </nav>;
}
