import { PersonAvatar } from "@/components/people/PersonAvatar";
import type { ProductStatusSink } from "@/lib/productStatus";
import { Fragment, useRef, useState, type CSSProperties, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { CommentActorProfile } from "@/commentWire.generated";
import type { NativeComment } from "@/lib/commentService";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { Session } from "@/types";
import { authorLabel, commentAuthorFace } from "./commentModel";
import { openComments } from "./commentPanelEvents";
import { DiscussionFilterSheet } from "./DiscussionFilterMenu";
import {
  DISCUSSION_SECTION_LABELS, discussionFilterPeople, discussionLine, discussionPeople, discussionShown, filterChanged, latestReplyWords, listedDiscussions, OPEN_DISCUSSIONS,
  popoverDiscussions, sectionedDiscussions, type DiscussionFilter,
} from "./discussionListModel";
import { suggestionFacts } from "./suggestionModel";
import { SuggestionSummary } from "./SuggestionSummary";
import { FilterIcon, MarkReadIcon } from "@/icons/leafen";
import { PageNotifyBell } from "./PageNotifyBell";
import { usePopoverListEdges } from "./popoverListEdges";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import styles from "./PageDiscussions.module.css";
import sheetStyles from "./PhoneDiscussionsSheet.module.css";

// Notion's top bar windows open 8px under their 28px button (seen 2026-11-06).
const GAP = 8;

type PageDiscussionsProps = {
  anchor: RefObject<HTMLElement | null>;
  /** The page, or a database row's page, whose discussions open. */
  pageId: string;
  /** None for a web link's visitor, who only reads. */
  session: Session | null;
  discussions: readonly NativeComment[];
  /** Each discussion's latest reply, by its first comment. */
  latestReplies?: ReadonlyMap<string, NativeComment>;
  /** Each discussion's replies, by its first comment: whom a phone's filter can pick. */
  replies?: ReadonlyMap<string, readonly NativeComment[]>;
  actorProfiles: Readonly<Record<string, CommentActorProfile>>;
  onOpenAll: () => void;
  onClose: () => void;
  onStatus?: ProductStatusSink;
  /**
   * A web link's visitor's, as Notion's public page (seen 2026-11-06): the
   * same list to read, no bell or way into a thread, and "모두 보기" shows the
   * resolved ones here too instead of the discussions panel.
   */
  visitor?: boolean;
  showResolved?: boolean;
};

/**
 * The top bar's "댓글", as Notion's "토론" popover (seen 2026-09-30): the
 * page's open discussions — on the page, its blocks or its text, not those
 * only reacted to — newest first, the unread ones and the five newest read
 * ones (popoverDiscussions), one line each, its latest reply's where it
 * has replies (2026-10-08, listedDiscussions); picking one opens it where it is. "모두 보기" shows
 * every discussion, resolved ones too, in the discussions panel; a phone's
 * sheet filters its own list instead. A touch screen wider than a phone's
 * opens the phone's "모든 토론" sheet too, risen from the bottom in its
 * middle, as Notion's iPad (TouchSheetLayout).
 */
export function PageDiscussionsPopover(props: PageDiscussionsProps) {
  return <TouchSheetLayout><PageDiscussions {...props} /></TouchSheetLayout>;
}

function PageDiscussions({
  anchor, pageId, session, discussions, latestReplies, replies, actorProfiles, onOpenAll, onClose, onStatus, visitor = false, showResolved = false,
}: PageDiscussionsProps) {
  const userId = session?.user.id ?? "";
  const { t } = useTranslation("commentsPanel");
  const { phone } = useSidebarLayout(true);
  const popover = useRef<HTMLDivElement>(null), list = useRef<HTMLDivElement>(null);
  usePopoverListEdges(list);
  const [filter, setFilter] = useState<DiscussionFilter>(OPEN_DISCUSSIONS);
  const [filtering, setFiltering] = useState(false);
  const listed = listedDiscussions(discussions, latestReplies);
  // The unread ones and the five newest read ones (popoverDiscussions); a
  // visitor's "모두 보기" lists every one, the resolved ones too.
  const open = showResolved ? listed : popoverDiscussions(listed.filter((comment) => !comment.resolved), !visitor);
  // Unread ones come first under "신규", the rest under "이전", once both are
  // there (sectionedDiscussions); each unread one with a blue dot at its end
  // (Notion's, seen 2026-11-11).
  const rows = sectionedDiscussions(open, !visitor);
  // Under "댓글", lined up with its right edge, or over it where only that
  // side has room.
  const place = usePopoverPlacement(popover, (size, viewport) => placeAround(
    popoverRectOf(anchor.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport,
    { gap: GAP, align: "end" },
  ), [anchor], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose, inside: () => [anchor.current] });
  const style: CSSProperties = place ? popoverPlacementStyle(place) : { visibility: "hidden" };


  function openThread(root: NativeComment) {
    if (visitor) return;
    onClose();
    openComments({ pageId, blockId: root.blockId ?? null, activeCommentId: root.id });
  }

  // A phone's is Notion's "모든 토론" half sheet (seen 2026-10-27): white,
  // its blue filter at the head's left and a round ✕ at its right, then a
  // 45px row over a line in a 52px slot each, 20px in — the 24px face, the name
  // (16px/500) and the latest line grey after it, a blue dot while unread —
  // the open ones until the filter says otherwise, every one of them, under
  // "신규" and "이전" as the bar's (sectionedDiscussions, seen 2026-11-25 on a
  // Galaxy A90). It has no "모두 보기": ticking "해결" shows the rest.
  if (phone) {
    const shown = sectionedDiscussions(listed.filter((root) => discussionShown(root, replies?.get(root.id) ?? [], filter)), !visitor);
    const people = discussionFilterPeople(discussionPeople([...discussions, ...[...(replies?.values() ?? [])].flat()]), userId, actorProfiles);
    const changed = filterChanged(filter, OPEN_DISCUSSIONS);
    return (
      <PhoneSheet size="half" plain title={t("discussions.all")} closeButton="small" onClose={onClose}
        start={(
          <button type="button" className={styles.sheetFilter} aria-label={t("discussions.filter")} aria-haspopup="dialog" aria-expanded={filtering}
            data-filtered={changed || undefined} data-discussion-filter-button onClick={() => setFiltering(true)}>
            <FilterIcon size="regular" aria-hidden="true" />
          </button>
        )}
        data={{ "data-page-discussions-popover": "", "data-discussions-phone-sheet": "" }}>
        {shown.length > 0 ? (
          <div className={sheetStyles.list}>
            {shown.map(({ root, section }) => {
              const latest = latestReplies?.get(root.id);
              const last = latest ?? root;
              const author = authorLabel(last, userId, actorProfiles);
              return (
                <Fragment key={root.id}>
                  {section ? (
                    <div className={sheetStyles.sectionHead} role="presentation" data-discussions-section={section}>{t(DISCUSSION_SECTION_LABELS[section])}</div>
                  ) : null}
                  <button type="button" className={sheetStyles.slot} data-discussion-id={root.id} data-resolved={root.resolved ? "true" : undefined}
                    onClick={() => openThread(root)}>
                    <span className={`${styles.sheetRow} ${sheetStyles.slotRow}`}>
                      <PersonAvatar className={styles.sheetAvatar} name={author} {...commentAuthorFace(last, actorProfiles)} size={24} />
                      <span className={styles.sheetText}><span className={styles.sheetAuthor}>{author}</span> <LatestLine root={root} latest={latest} /></span>
                      {root.unread ? <span className={styles.sheetUnread} aria-label={t("discussions.unread")} data-discussion-unread /> : null}
                    </span>
                  </button>
                </Fragment>
              );
            })}
          </div>
        ) : (
          // Nothing the filter lets through, as Notion's (seen 2026-10-27): its
          // two lines in the middle of the list's whole height, under the half
          // sheet's fold until the sheet is pulled up.
          <div className={styles.sheetEmpty} role="status" data-discussions-empty>
            <strong>{t("discussions.applyFilter")}</strong>
            {listed.some((root) => root.resolved) ? <span>{t("discussions.resolvedHere")}</span> : null}
          </div>
        )}
        {filtering ? <DiscussionFilterSheet filter={filter} people={people} onChange={setFilter} onClose={() => setFiltering(false)} /> : null}
      </PhoneSheet>
    );
  }

  const content = (
    <div ref={popover} className={styles.popover} style={style} role="dialog" aria-label={t("discussions.title")}
      data-page-discussions-popover>
      <div className={styles.popoverHeader}>
        <span>{t("discussions.title")}</span>
        {session && !visitor ? <PageNotifyBell pageId={pageId} session={session} onFailure={(message) => onStatus?.(message, "error")} /> : null}
      </div>
      {open.length > 0 ? (
        <div ref={list} className={styles.popoverList} role="list">
          {rows.map(({ root, section }) => {
            const latest = latestReplies?.get(root.id);
            const shown = latest ?? root;
            const author = authorLabel(shown, userId, actorProfiles);
            return (
              visitor ? (
                <div key={root.id} className={styles.discussionRow} role="listitem" data-discussion-id={root.id}
                  data-resolved={root.resolved ? "true" : undefined}>
                  <PersonAvatar className={styles.avatar} name={author} {...commentAuthorFace(shown, actorProfiles)} size={22} />
                  <span className={styles.author}>{author}</span>
                  <span className={styles.line}><LatestLine root={root} latest={latest} /></span>
                </div>
              ) : (
                <Fragment key={root.id}>
                  {section ? (
                    <div className={styles.sectionLabel} role="presentation" data-discussions-section={section}>{t(DISCUSSION_SECTION_LABELS[section])}</div>
                  ) : null}
                  <button type="button" className={styles.discussionRow} role="listitem"
                    data-discussion-id={root.id} onClick={() => openThread(root)}>
                    <PersonAvatar className={styles.avatar} name={author} {...commentAuthorFace(shown, actorProfiles)} size={22} />
                    <span className={styles.author}>{author}</span>
                    <span className={styles.line}><LatestLine root={root} latest={latest} /></span>
                    {root.unread ? <span className={styles.rowUnread} aria-label={t("discussions.unread")} data-discussion-unread /> : null}
                  </button>
                </Fragment>
              )
            );
          })}
        </div>
      ) : (
        // Nothing open, as Notion's (seen 2026-11-06): the read mark, its two
        // lines and a small "모두 보기" in the middle of the popover's body.
        <div className={styles.empty} role="status">
          <MarkReadIcon className={styles.emptyIcon} size="card" aria-hidden="true" />
          <strong>{t("discussions.caughtUp")}</strong>
          <span>{t("discussions.noOpen")}</span>
          <button type="button" className={styles.emptyViewAll} data-discussions-view-all onClick={() => { if (!visitor) onClose(); onOpenAll(); }}>
            {t("discussions.viewAll")}
          </button>
        </div>
      )}
      {open.length > 0 && !(visitor && showResolved) ? (
        <button type="button" className={styles.viewAll} data-discussions-view-all onClick={() => { if (!visitor) onClose(); onOpenAll(); }}>
          {t("discussions.viewAll")}
        </button>
      ) : null}
    </div>
  );
  return typeof document === "undefined" ? content : createPortal(content, document.body);
}

/**
 * A discussion's line in the lists: its latest reply's words — after "답글:"
 * in the line's own grey and weight on the page's own discussion, the one
 * under its title, and bare on a block's or its text's — or its first
 * comment's, or its suggestion's summary in one line (SuggestionSummary).
 * As Notion's (seen 2026-11-25 in Chrome at 1440 in two tabs at different
 * times and in its Android app's sheet): a page's discussion's reply line had
 * the prefix read or unread, the reader's reply or another's, on their thread
 * or another's; a block's none in the same states.
 */
function LatestLine({ root, latest }: { root: NativeComment; latest?: NativeComment }) {
  const { t } = useTranslation("commentsPanel");
  const words = latestReplyWords(latest);
  const suggestion = words ? null : suggestionFacts(root);
  if (suggestion) return <SuggestionSummary facts={suggestion} inline />;
  if (!words) return discussionLine(root);
  return root.blockId ? words : <><span data-reply-prefix>{t("discussions.replyPrefix")}</span> {words}</>;
}
