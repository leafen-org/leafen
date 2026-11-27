"use client";

import { pageIconSource } from "@/lib/pageNames";
import { useEffect, useState, type ReactNode } from "react";
import type { FeedPostService, FeedPostViewers } from "@/lib/feedPostService";
import { editorName } from "@/lib/peopleDirectory";
import type { DbProperty, DbView, Page } from "@/lib/types";
import { hasExplicitPageIcon, PageIconGlyph } from "../page/PageIcon";
import { PropValue, type PropertyValueReadContext } from "./PropValue";
import { databaseViewLabels } from "./databaseViewLabels";
import { FeedPost } from "./FeedPost";
import { type QueryBounds, visibleViewProperties } from "./query";
import styles from "./databaseSheet";
import { viewShowsPageIcons } from "./CardTitleIcon";
import { CardsLoadMore, type CardsLoadMoreControl } from "./CardsLoadMore";
import { conditionalColorBackground, conditionalColorResolver } from "./conditionalColorModel";
import { viewDisplayName } from "./databaseViewShared";
import { Plus } from "../icons";
import type { Session } from "@/types";
import { useRowDisplayTitle, useUntitledRowName } from "./untitledRowName";

export type CurrentDatabaseViewsReadSnapshot = {
  properties: readonly DbProperty[];
  propertyValueContext: PropertyValueReadContext;
  queryBounds: QueryBounds;
};


export function FeedView({
  view,
  rows,
  onOpenRow,
  snapshot,
  posts,
  loadMore,
  onAddRow,
  rowMenu,
}: {
  view: DbView;
  rows: Page[];
  onOpenRow: (pageId: string) => void;
  snapshot: CurrentDatabaseViewsReadSnapshot;
  /** A post's "⋯" opens its row's menu, the one the database's other views open. */
  rowMenu?: { open(rowId: string, trigger: HTMLElement): void; menu: ReactNode };
  /** "더 불러오기" under the first posts, as many as the view first shows. */
  loadMore?: CardsLoadMoreControl;
  /** Its "+ 새 페이지" after the posts, when it can make one. */
  onAddRow?: () => void;
  posts?: {
    service: FeedPostService | null;
    readOnly: boolean;
    /** Where post views are recorded and read: the page holding the rows. */
    analytics?: { workspaceId: string; pageId: string };
    /** Who reads the feed, for a post's discussion. */
    session?: Session;
  };
}) {
  const labels = databaseViewLabels();
  const newPageLabel = useUntitledRowName();
  const rowTitle = useRowDisplayTitle();
  // "페이지 아이콘 표시", "작성자 표시" and "속성 줄바꿈", as Notion's feed (seen 2026-10-05).
  const showIcons = viewShowsPageIcons(view.config ?? {});
  const showAuthor = view.config?.feedShowAuthor !== false;
  const wrapProperties = view.config?.wrap === true;
  const [views, setViews] = useState<Record<string, FeedPostViewers>>({});
  const service = posts?.service ?? null;
  const analytics = posts?.analytics;
  const rowKey = rows.map(({ id }) => id).join("\n");
  useEffect(() => {
    if (!service || !analytics || !rowKey) {
      setViews({});
      return;
    }
    const controller = new AbortController();
    service.readViews(analytics.workspaceId, rowKey.split("\n"), controller.signal)
      .then((next) => { if (!controller.signal.aborted) setViews(next); })
      .catch(() => { if (!controller.signal.aborted) setViews({}); });
    return () => controller.abort();
  }, [service, analytics?.workspaceId, rowKey]);
  const properties = snapshot.properties as DbProperty[];
  const visibleProperties = visibleViewProperties(properties, view, snapshot.queryBounds)
    .filter((property) => property.type !== "title");
  const postColor = conditionalColorResolver(view.config as Record<string, unknown>, properties, {
    pagesById: snapshot.propertyValueContext.pagesById as Record<string, Page>, currentUserId: snapshot.propertyValueContext.userId,
  }, snapshot.queryBounds);
  return (
    <div
      className={styles.feedView}
      role="feed"
      aria-label={labels.feedViewAria(viewDisplayName(view))}
      data-feed-view
    >
      {rows.map((row) => {
        const title = rowTitle(row);
        const tint = postColor(row);
        return (
          <FeedPost
            key={row.id}
            background={tint ? { color: tint.color, value: conditionalColorBackground(tint.color) } : undefined}
            rowId={row.id}
            title={title}
            untitled={!row.title}
            // A post shows its page's own icon only, and its author by name, as Notion's (seen 2026-10-20).
            icon={showIcons && hasExplicitPageIcon(pageIconSource(row)) ? <PageIconGlyph page={pageIconSource(row)} size={24} /> : undefined}
            author={showAuthor && row.createdBy ? editorName(row.createdBy, snapshot.propertyValueContext.userId ?? "") : undefined}
            authorId={row.createdBy || undefined}
            createdAt={row.createdAt}
            updatedAt={row.updatedAt}
            views={views[row.id]}
            analytics={analytics}
            onOpen={() => onOpenRow(row.id)}
            onActions={rowMenu ? (trigger) => rowMenu.open(row.id, trigger) : undefined}
            service={posts?.service ?? null}
            session={posts?.session}
            readOnly={posts?.readOnly ?? true}
            properties={visibleProperties.length > 0 ? (
              <div className={styles.feedItemProperties} data-wrap={wrapProperties ? "true" : undefined} data-feed-post-properties>
                {visibleProperties.map((property) => (
                  <span key={property.id}>
                    <PropValue
                      context={snapshot.propertyValueContext}
                      row={row}
                      prop={property}
                      interactive={false}
                    />
                  </span>
                ))}
              </div>
            ) : null}
          />
        );
      })}
      {loadMore ? <CardsLoadMore placement="view" {...loadMore} /> : null}
      {/* After the posts, or alone with none, as Notion's feed (seen 2026-10-11). */}
      {onAddRow ? <button type="button" className={styles.feedNew} onClick={onAddRow}>
        <Plus size="small" aria-hidden="true" />{newPageLabel}</button> : null}
      {rowMenu?.menu}
    </div>
  );
}
