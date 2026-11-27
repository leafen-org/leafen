import type { ReactNode } from "react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon, ChevronDown } from "@/icons/leafen";
import { activeOwnDigitsLocale } from "@/lib/i18n";
import { pageHistoryGroupings, pageHistoryGroupingSpan, type PageHistoryGroup, type PageHistoryGrouping } from "@/lib/pageHistoryGroups";
import { pagePresenceColor } from "@/lib/pagePresenceModel";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import type { PageHistoryBookmark } from "@/pageHistoryWire.generated";
import { pageHistoryBookmarkItemClass, PageHistoryBookmarkedMark, PageHistoryBookmarkRow, PageHistoryBookmarkVersionButton } from "./PageHistoryBookmarks";
import { PageHistoryAuthorName, PageHistoryShowChangesButton } from "./PageHistoryChangeList";
import styles from "./PageHistoryVersionList.module.css";

const groupingLabelKey = {
  minute: "updatesPanel:versions.groupMinute",
  quarter: "updatesPanel:versions.groupQuarter",
  hour: "updatesPanel:versions.groupHour",
  bookmark: "updatesPanel:versions.groupBookmark",
} as const satisfies Record<PageHistoryGrouping, string>;

/** The bookmarks and what the list does with them. */
export type PageHistoryListBookmarks = {
  items: readonly PageHistoryBookmark[];
  selectedId: string;
  canEdit: boolean;
  name: (bookmark: PageHistoryBookmark) => string;
  time: (bookmark: PageHistoryBookmark) => string;
  onSelect: (bookmark: PageHistoryBookmark) => void;
  onBookmarkVersion: (group: PageHistoryGroup) => void;
  onRename: (bookmark: PageHistoryBookmark) => void;
  onDelete: (bookmark: PageHistoryBookmark) => void;
};

/**
 * The version history's list, as Notion's (seen 2026-09-30): "버전 기록"
 * over the page's name and the "Group by" choice, then each entry's time
 * with a dot and name for each person whose changes it holds. A bookmark
 * sits above the entry holding its version, and "Group by 북마크" lists the
 * bookmarks alone (seen 2026-10-01). An entry's "›" opens its changes.
 */
export function PageHistoryVersionList({
  timelineToggle,
  titleId, pageTitle, groups, grouping, onGrouping, selectedId, onSelect, onShowChanges,
  actorLabel, timeLabel, loading, hasMore, onLoadMore, bookmarks,
}: {
  /** The button that folds the list away. */
  timelineToggle?: ReactNode;
  titleId: string;
  pageTitle: string;
  groups: readonly PageHistoryGroup[];
  grouping: PageHistoryGrouping;
  onGrouping: (grouping: PageHistoryGrouping) => void;
  selectedId: string;
  onSelect: (versionId: string) => void;
  /** Opens an entry's changes from its "›"; a database's own history has none. */
  onShowChanges?: (group: PageHistoryGroup) => void;
  actorLabel: (actorId: string) => string;
  timeLabel: (createdAt: number) => string;
  loading: boolean;
  hasMore: boolean;
  onLoadMore: () => void;
  bookmarks: PageHistoryListBookmarks;
}) {
  const { t } = useTranslation(["updatesPanel", "wikiView"]);
  const [menuOpen, setMenuOpen] = useState(false);
  const groupRow = (group: PageHistoryGroup, index: number, bookmarked: boolean) => {
    const selected = !bookmarks.selectedId && selectedId === group.version.id;
    return (
      <div key={group.version.id} className={pageHistoryBookmarkItemClass} role="presentation" data-version-entry="">
        <button
          type="button"
          role="option"
          aria-selected={selected}
          className={styles.versionRow}
          data-current={index === 0 ? "true" : undefined}
          data-selected={selected ? "true" : undefined}
          onClick={() => onSelect(group.version.id)}
        >
          <span className={styles.versionTime}>{timeLabel(group.version.createdAt)}</span>
          {group.authors.length > 0 ? (
            <span className={styles.versionActors}>
              {group.authors.map((author, actorIndex) => (
                <span key={`${author.actorId}:${author.connectionName}`} className={styles.versionActor}>
                  <span className={styles.versionActorDot} style={{ background: pagePresenceColor(author.actorId) }} aria-hidden="true" />
                  <PageHistoryAuthorName author={author} actorLabel={actorLabel} />{actorIndex < group.authors.length - 1 ? "," : ""}
                </span>
              ))}
            </span>
          ) : null}
        </button>
        {bookmarked ? <PageHistoryBookmarkedMark /> : bookmarks.canEdit ? (
          <PageHistoryBookmarkVersionButton beside onBookmark={() => bookmarks.onBookmarkVersion(group)} />
        ) : null}
        {onShowChanges ? <PageHistoryShowChangesButton onShow={() => onShowChanges(group)} /> : null}
      </div>
    );
  };
  const bookmarkRow = (bookmark: PageHistoryBookmark) => (
    <PageHistoryBookmarkRow key={`bookmark-${bookmark.id}`} bookmark={bookmark} name={bookmarks.name(bookmark)}
      time={bookmarks.time(bookmark)} selected={bookmarks.selectedId === bookmark.id} canEdit={bookmarks.canEdit}
      onSelect={() => bookmarks.onSelect(bookmark)} onRename={() => bookmarks.onRename(bookmark)}
      onDelete={() => bookmarks.onDelete(bookmark)} />
  );
  const menuRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);

  usePopoverAppear(menuOpen, menuRef, () => buttonRef.current);
  const layer = useOverlayLayer(menuOpen, { kind: "menu", ref: menuRef, inside: () => [buttonRef.current],
    onEscape: () => setMenuOpen(false), onOutsidePress: () => setMenuOpen(false) });
  const picker = useNativeMenu({ items: [...pageHistoryGroupings], open: menuOpen, menuRef,
    label: t("updatesPanel:versions.groupChangesBy"), firstKey: grouping,
    keyOf: option => option, textOf: option => t(groupingLabelKey[option]),
    onAction: option => { onGrouping(option); setMenuOpen(false); } });

  return (
    <section className={styles.historyVersionPane} aria-label={t("updatesPanel:versions.versionHistory")}>
      <div className={styles.historyVersionHeader}>
        {timelineToggle}
        <div id={titleId} className={styles.historyVersionLabel}>{t("updatesPanel:versions.versionHistory")}</div>
        <div className={styles.historyVersionPageTitle}>{pageTitle}</div>
        <div className={styles.historyGroupBy}>
          <button
            ref={buttonRef}
            type="button"
            className={styles.historyGroupByButton}
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            data-page-history-group-by={grouping}
            onClick={() => setMenuOpen((open) => !open)}
          >
            <span className={styles.historyGroupByLabel}>{t("updatesPanel:versions.groupBy")}</span>{" "}
            <span>{grouping === "bookmark" ? t(groupingLabelKey.bookmark) : pageHistoryGroupingSpan(grouping, activeOwnDigitsLocale())}</span>
            <ChevronDown size="tiny" aria-hidden="true" />
          </button>
          {menuOpen ? (
            <div
              {...picker.menuProps}
              ref={menuRef}
              className={styles.historyGroupByMenu}
              style={{ zIndex: layer.zIndex }}
              onKeyDown={event => { picker.menuProps.onKeyDown?.(event); layer.onKeyDown(event); }}
            >
              <div className={styles.historyGroupByHeading} aria-hidden="true">
                {t("updatesPanel:versions.groupChangesBy")}
              </div>
              {pageHistoryGroupings.map((option) => (
                <NativeMenuItem state={picker.state} itemKey={option}
                  key={option}
                  role="menuitemradio"
                  aria-checked={option === grouping}
                  className={styles.historyGroupByItem}
                >
                  {({ labelProps }) => <><span {...labelProps}>{t(groupingLabelKey[option])}</span>
                  {option === grouping ? <CheckIcon size="small" aria-hidden="true" /> : null}
                  </>}
                </NativeMenuItem>
              ))}
            </div>
          ) : null}
        </div>
      </div>

      <div className={`${styles.versionList} nscroll`} role="listbox" aria-label={t("updatesPanel:versions.versionsAria")}>
        {!loading && grouping !== "bookmark" && groups.length === 0 ? (
          <div className={styles.listEmpty}>{t("updatesPanel:empty.noPageHistoryYet")}</div>
        ) : null}
        {grouping === "bookmark" && bookmarks.items.length === 0 ? (
          <div className={styles.listEmpty}>{t("updatesPanel:versions.noBookmarks")}</div>
        ) : null}
        {grouping === "bookmark" ? bookmarks.items.map(bookmarkRow) : null}
        {grouping === "bookmark" ? null : groups.flatMap((group, index) => {
          const marks = bookmarks.items.filter((bookmark) => group.versionIds.includes(bookmark.versionId));
          return [...marks.map(bookmarkRow), groupRow(group, index, marks.length > 0)];
        })}
        {/* A bookmark whose version is older than the entries loaded yet. */}
        {grouping === "bookmark" ? null : bookmarks.items
          .filter((bookmark) => !groups.some((group) => group.versionIds.includes(bookmark.versionId)))
          .map(bookmarkRow)}
        {hasMore && grouping !== "bookmark" ? (
          <button type="button" className={styles.listLoadMore} disabled={loading} onClick={onLoadMore}>
            {t("wikiView:loadMore")}
          </button>
        ) : null}
      </div>
    </section>
  );
}
