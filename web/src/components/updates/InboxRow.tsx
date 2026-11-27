import { personDistinction } from "@/lib/peopleDirectory";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { CheckIcon } from "@/icons/leafen";
import { useRef, useState, type ReactNode } from "react";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { Trans, useTranslation } from "react-i18next";
import type { CommentActorProfile } from "@/commentWire.generated";
import {
  AlarmIcon, ArchiveIcon, Bell, CommentIcon, FileText, FlagIcon, LightningIcon, MarkReadIcon, MarkUnreadIcon, ServerAppsIcon, UnarchiveIcon,
  VerifiedIcon,
} from "@/icons/leafen";
import { deadlineNoticeIdOf } from "@/lib/deadlines";
import { listedPageNameKind } from "@/lib/pageTitle";
import { usePageNames } from "@/lib/pageNames";
import { activeDateLocale } from "@/lib/i18n";
import { sanitizeTextSpans } from "@/lib/textOperations";
import type { TextSpan } from "@/lib/types";
import { closedMentionLabel, isClosedMentionText, lineWithClosedMentions } from "@/lib/closedMentions";
import { dateMentionDisplayText } from "../editor/dateMentions";
import { RichTextCellText } from "../database/RichTextCellText";
import { noticeConnectionAuthor } from "../comments/commentModel";
import { PropertyTypeIcon } from "../database/PropertyTypeIcon";
import { PageIconGlyph, WorkspaceIconGlyph, workspaceIconKind } from "../page/PageIcon";
import type { useAccessRequestDecisions } from "./useAccessRequestDecisions";
import { InboxAccessRequest } from "./InboxAccessRequest";
import { InboxPermissionChange } from "./InboxPermissionChange";
import type { useDeadlineAcknowledgements } from "./useDeadlineAcknowledgements";
import {
  accessRequestIdOf, inboxAge, inboxSentenceKey, isAppNotice, isPagelessNotice, isPersonPropertyNotice, isWorkspaceNotice, permissionChangeOf,
  type NotificationActivity,
} from "./updatesPanelModel";
import styles from "../UpdatesPanelFeed.module.css";
import noticeStyles from "./InboxNotice.module.css";
import { useInboxRowGestures } from "./useInboxRowGestures";
import { InboxRowCard } from "./InboxRowCard";
import touchStyles from "./InboxRowTouch.module.css";
import { keysFocusHolder } from "@/lib/lastPress";

// A row the keys' focus is in shows its buttons (data-keys-within).
keysFocusHolder(styles.row);

type AccessRequests = ReturnType<typeof useAccessRequestDecisions>;
type DeadlineAcknowledgements = ReturnType<typeof useDeadlineAcknowledgements>;

/** A page or database mention the reader can't open, which the server sends in marks (closedMentions.ts). */
function closedMention(span: TextSpan) {
  return (span.mention === "page" || span.mention === "database") && isClosedMentionText(span.text);
}

/**
 * A span's words in a line: a date as it says now rather than when it was written, a closed mention "사용 권한 없음",
 * one nothing answers to "찾을 수 없음".
 */
function spanWords(span: TextSpan) {
  if (closedMention(span)) return closedMentionLabel(span.text);
  return span.mention === "date" && span.date
    ? dateMentionDisplayText(span.date, span.text, undefined, span.dateEnd, span.dateFormat, span.dateTimeFormat) : span.text;
}

/**
 * A span in a line: a mention the reader can't open by the crossed-out eye and "사용 권한 없음", as Notion's inbox
 * draws it in a comment's or a page's line (seen 2026-11-09, a guest); any other span by its words.
 */
function spanNode(span: TextSpan, index: number) {
  return closedMention(span) ? <RichTextCellText key={index} spans={[span]} linksAsText /> : spanWords(span);
}

/** A mention's or a comment's plain line, with any date in it as it says now rather than when it was written. */
function lineAsItStands(notification: NotificationActivity["notification"]) {
  return (sanitizeTextSpans(notification.previewSpans) ?? []).map(spanWords).join("").trim();
}

/** A line naming a page closed to the reader, drawn span by span; null for any other. */
function closedMentionLineNodes(notification: NotificationActivity["notification"]) {
  const spans = sanitizeTextSpans(notification.previewSpans) ?? [];
  return spans.some(closedMention) ? spans.map(spanNode) : null;
}

/**
 * A comment's line with the reader's own mention in yellow, its "@" fainter,
 * as Notion's inbox marks it (seen 2026-11-06: --c-yelTexSec, the "@" at 60%);
 * other mentions read as the line does. Null when the line names no reader.
 */
function commentLineWithReader(notification: NotificationActivity["notification"], readerId: string | undefined) {
  const spans = sanitizeTextSpans(notification.previewSpans) ?? [];
  if (!readerId || !spans.some((span) => span.mention === "person" && span.userId === readerId)) return null;
  return spans.map((span, index) => {
    if (span.mention === "person" && span.userId === readerId) {
      const name = span.text.replace(/^@/, "");
      return <span key={index} className={styles.readerMention} data-inbox-reader-mention><span>@</span>{name}</span>;
    }
    return spanNode(span, index);
  });
}

/**
 * A person property's line, as Notion's inbox draws it (seen 2026-11-09): the
 * property kind's icon 6px before its name, then the people it names as they
 * stand now, each "@" fainter. Null for a line without its spans.
 */
function personPropertyLineNodes(notification: NotificationActivity["notification"]) {
  const [name, ...people] = sanitizeTextSpans(notification.previewSpans) ?? [];
  if (!name || name.mention || !people.length) return null;
  return (
    <>
      <span className={styles.propertyName}><PropertyTypeIcon type="person" />{name.text.trimEnd()}</span>{" "}
      {people.map((span, index) => span.mention === "person"
        ? <span key={index} className={styles.lineMention}><span>@</span>{span.text.replace(/^@/, "")}</span>
        : spanNode(span, index))}
    </>
  );
}

/**
 * The page a sentence names: its icon, then its title. An app's name goes without one; the row leads with the app's
 * mark. A workspace goes with its own mark, its letter on a tint without an icon, as a page with its icon.
 */
function PageMark({ activity, workspaceIcon, children }: { activity: NotificationActivity; workspaceIcon?: string; children?: ReactNode }) {
  const letter = workspaceIconKind(workspaceIcon) === "letter";
  return (
    <span className={styles.pageMark}>
      {/* An emoji icon is bare text; held in a box it keeps its gap before the title as an icon does (Notion's, seen 2026-11-06). */}
      {activity.page ? <span className={styles.pageMarkIcon}><PageIconGlyph page={activity.page} size={14} /></span>
        : isWorkspaceNotice(activity.notification) ? <span className={styles.pageMarkIcon} data-workspace-letter={letter || undefined}>
          {letter ? activity.notification.title.trim().slice(0, 1) : <WorkspaceIconGlyph icon={workspaceIcon} size={14} fallback="" />}</span>
        : isPagelessNotice(activity.notification) ? null
          : <FileText size="tiny" aria-hidden="true" />}
      <span className={styles.pageMarkTitle}>{children}</span>
    </span>
  );
}

function ageLabel(at: number, now: number, t: (key: string, options?: Record<string, unknown>) => string) {
  const age = inboxAge(at, now);
  if (age.unit === "now") return t("inbox.time.now");
  if (age.unit !== "date") return t(`inbox.time.${age.unit}`, { count: age.count });
  const date = new Date(at);
  return date.toLocaleDateString(activeDateLocale(), {
    month: "short", day: "numeric",
    ...(date.getFullYear() === new Date(now).getFullYear() ? {} : { year: "numeric" }),
  });
}

/**
 * One notification, as Notion's inbox draws it (seen 2026-10-07): the
 * person's picture, a sentence naming them and the page, how long ago and a
 * blue dot while unread, the comment or message below in gray. On hover a
 * small group of buttons sits at its top right, on a touch screen wider than
 * a phone's under its words; a swipe left archives and a long press opens
 * its card (useInboxRowGestures, InboxRowCard).
 */
export function InboxRow({
  activity, profiles, now, archived, requests, deadlines, readerId, readerName, workspaceIcon,
  onOpen, onPageSettings, onMarkRead, onMarkUnread, onArchive, onUnarchive,
}: {
  activity: NotificationActivity;
  profiles: Record<string, CommentActorProfile>;
  now: number;
  /** Who reads the inbox: a comment's mention of them is marked, as Notion's. */
  readerId?: string;
  /** Their name, which a notice of a share of theirs lowered or taken away shows. */
  readerName?: string;
  /** The inbox's workspace's icon, which a notice about the workspace shows. */
  workspaceIcon?: string;
  archived: boolean;
  requests: AccessRequests;
  deadlines: DeadlineAcknowledgements;
  onOpen: () => void;
  onPageSettings: (anchor: HTMLElement) => void;
  onMarkRead: () => void;
  onMarkUnread: () => void;
  onArchive: () => void;
  onUnarchive: () => void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("updatesPanel");
  const { notification, page } = activity;
  const unread = !notification.readAt;
  const row = useRef<HTMLLIElement>(null);
  const { phone } = useSidebarLayout(true);
  // The row a long press opened its card under.
  const [cardAt, setCardAt] = useState<HTMLElement | null>(null);
  const toggleRead = unread ? onMarkRead : onMarkUnread;
  const gestures = useInboxRowGestures({ actions: true, onLongSwipe: archived ? onUnarchive : onArchive, onLongPress: () => setCardAt(row.current) });
  const swiped = (action: () => void) => () => { gestures.close(); action(); };
  // A connection's comment's notice names the connection as the comment does, with no person card.
  const connection = noticeConnectionAuthor(notification.actorConnection);
  const profile = notification.actorId && !connection ? profiles[notification.actorId] : undefined;
  // In a sentence another of the same name is told apart in brackets (personDistinction).
  const distinction = profile && notification.actorId ? personDistinction(notification.actorId) : null;
  const name = connection ? connection.name
    : profile?.displayName ? (distinction ? `${profile.displayName} (${distinction})` : profile.displayName) : t("inbox.someone");
  const sentence = inboxSentenceKey(notification);
  // A notice about a database item names the item (notification_item_now.go), a
  // deadline its item though the item is gone: never the page holding its database.
  const named = notification.itemId || sentence !== "deadline" ? page : undefined;
  // A page it knows is named as it is now, an untitled one by its kind as
  // everywhere else; one it doesn't by the title the server read for it.
  // Its mentions as a plain title says them, by the title's spans the server
  // read for its reader: "@〈name〉", one closed to them "@찾을 수 없음" (closedMentions.ts).
  const title = pageNames.listed(named, notification.title, notification.titleSpans,
    listedPageNameKind(notification));
  // Leafen's own notice about a page or the workspace — a Notion move's stages and end — reads its own title, and a page
  // it opens goes under it (2026-11-16: only the page's title showed, and nobody could tell what had ended; one about
  // the workspace took its mark's letter from the notice's words). One about an app or the agents names it as before.
  const noticeTitle = sentence === "system" && (!isPagelessNotice(notification) || isWorkspaceNotice(notification))
    ? notification.title.trim() : "";
  const label = noticeTitle || title;
  const requestId = accessRequestIdOf(notification);
  const permission = permissionChangeOf(notification);
  const noticeId = deadlineNoticeIdOf(notification.activityKey);
  // A request names no role unless it asks to edit, as Notion's "편집 권한을 요청했습니다." (seen 2026-11-09).
  const preview = sentence === "accessRequest"
    ? notification.preview === "edit" ? t("rows.requestedEditAccess") : ""
    // The server's new address: what of the person's keeps the old one.
    : sentence === "addressChanged"
      ? t("rows.addressFixes", { list: notification.preview.split(",").filter(Boolean).map((fix) => t(`addressFixes.${fix}`, { defaultValue: fix })).join(" · ") })
    : sentence === "wikiExpired" || sentence === "pageEdit" ? "" : lineAsItStands(notification) || lineWithClosedMentions(notification.preview);
  // An automation's message names people, pages and dates as mentions, as Notion's inbox shows it (seen 2026-10-23),
  // and a reminder's line its date as the page does now (seen 2026-11-05).
  const previewSpans = (sentence === "automation" || sentence === "reminder") && lineAsItStands(notification)
    ? sanitizeTextSpans(notification.previewSpans) : undefined;
  // A comment's line marks the reader's own mention; a mention in a page's text reads plain (Notion's, seen 2026-11-06).
  const readerLine = sentence === "comment" ? commentLineWithReader(notification, readerId) : null;
  const propertyLine = isPersonPropertyNotice(notification) ? personPropertyLineNodes(notification) : null;
  // A line naming a page closed to the reader draws its mention as Notion's inbox does.
  const closedLine = preview && preview === lineAsItStands(notification) ? closedMentionLineNodes(notification) : null;
  // It says who sent it, the automation or the button by name, and while its
  // message names the page it ran for, "1개 보기" opens that page (Notion's, seen 2026-10-24).
  // One left unnamed reads as its maker's, whom the server names as its actor (Notion's "〈사람〉님의 자동화 기능").
  const sender = notification.title.trim() || (profile ? t("inbox.automationOf", { name }) : t("inbox.automationSender"));
  const viewsRunPage = sentence === "automation" && Boolean(notification.pageId) && Boolean(previewSpans?.some((span) => span.mention === "page" && span.pageId === notification.pageId));
  // A reminder leads with its alarm clock, though it names who set it (Notion's, seen 2026-11-06:
  // it showed the setter's face).
  const lead = sentence === "automation" ? <LightningIcon size="regular" aria-hidden="true" />
    : sentence === "reminder" ? <AlarmIcon size="regular" aria-hidden="true" />
    : connection ? <PersonAvatar userId={null} avatar={null} name={connection.face} size={24} bot />
    : profile
    ? <PersonAvatar userId={notification.actorId} avatar={profile.avatar} name={name} size={24} />
    : sentence === "wikiExpired" ? <VerifiedIcon size="regular" aria-hidden="true" />
          : sentence === "deadline" ? <FlagIcon size="regular" className={styles.deadlineMark} aria-hidden="true" />
          : isAppNotice(notification) ? <ServerAppsIcon size="regular" aria-hidden="true" />
            : <Bell size="regular" aria-hidden="true" />;

  return (
    <li ref={row} className={`${styles.row} ${touchStyles.row}`} data-inbox-row={notification.id} data-unread={unread ? "true" : undefined}
      data-sentence={sentence === "automation" ? sentence : undefined} {...gestures.handlers}>
      <div className={touchStyles.slide} data-dragging={gestures.dragging ? "true" : undefined} {...gestures.slideHandlers}
        style={gestures.offset ? { transform: `translateX(${gestures.offset}px)` } : undefined}>
      <button type="button" className={`${styles.rowMain} ${touchStyles.main}`} onClick={onOpen}>
        {/* The sender's picture shows their card on hover, as Notion's (seen 2026-10-12); the name doesn't. */}
        <span className={styles.lead} data-person={profile ? "true" : undefined} data-person-card={profile ? notification.actorId : undefined}
          data-inbox-lead={sentence === "reminder" ? sentence : undefined} aria-hidden="true">{lead}</span>
        <span className={styles.body} data-inbox-body>
          <span className={styles.line}>
            <span className={styles.sentence} data-inbox-words>
              {noticeTitle || (
                <Trans t={t} i18nKey={`inbox.sentences.${sentence}`} values={{ name, title: sentence === "automation" ? sender : title }}
                  components={{ name: <span className={styles.name} />, page: <PageMark activity={{ ...activity, page: named }} workspaceIcon={workspaceIcon} /> }} />
              )}
            </span>
            <span className={styles.trail} data-inbox-trail>
              <span className={styles.time} data-inbox-time>{ageLabel(activity.at, now, t)}</span>
              {unread ? <span className={styles.unreadDot} role="img" aria-label={t("rows.unreadDot")} data-inbox-dot /> : null}
            </span>
          </span>
          {noticeTitle && named ? (
            <span className={noticeStyles.page} data-inbox-notice-page>
              <PageMark activity={{ ...activity, page: named }} workspaceIcon={workspaceIcon}>{title}</PageMark>
            </span>
          ) : null}
          {permission ? <InboxPermissionChange reader={readerName ?? ""} from={permission.from} to={permission.to} />
            : previewSpans?.length ? <span className={styles.preview} data-inbox-preview="" data-inbox-preview-mentions><RichTextCellText spans={previewSpans} linksAsText /></span>
            : readerLine ? <span className={styles.preview} data-inbox-preview="">{readerLine}</span>
            : propertyLine ? <span className={styles.preview} data-inbox-preview="" data-inbox-property-line>{propertyLine}</span>
            : closedLine ? <span className={styles.preview} data-inbox-preview="">{closedLine}</span>
            // The inviter's message reads as their words, in quotes, as Notion's inbox shows it (seen once, 2026-11-21).
            : preview && sentence === "invite"
              ? <q className={`${styles.preview} ${noticeStyles.inviteMessage}`} data-inbox-preview="" data-inbox-invite-message>{preview}</q>
            : preview ? <span className={styles.preview} data-inbox-preview="">{preview}</span>
            // A reminder's or a mention's line emptied since — the server sends
            // the line as it stands — reads "비어 있음", as Notion's (seen 2026-11-06).
            : (sentence === "reminder" || sentence === "mention") && Array.isArray(notification.previewSpans) && !lineAsItStands(notification)
              ? <span className={styles.preview} data-inbox-preview="" data-inbox-empty-line>{t("rows.emptyLine")}</span> : null}
          {/* A line standing for comments on two discussions or more says how many (notification_inbox_groups.go). */}
          {(notification.discussions ?? 0) >= 2 ? <span className={styles.discussionCount} data-inbox-discussions>
            <CommentIcon size="small" aria-hidden="true" />{t("rows.discussions", { count: notification.discussions })}</span> : null}
          {/* A notice of a discussion resolved since says so under it, as Notion's (seen 2026-11-06). */}
          {notification.resolved ? <span className={styles.resolved} data-inbox-resolved>
            <CheckIcon size="small" aria-hidden="true" />{t("rows.resolved")}</span> : null}
        </span>
      </button>
      {viewsRunPage ? (
        <div className={styles.viewPagesActions} data-inbox-view-pages>
          <button type="button" className={`${styles.requestAction} ${styles.viewPages}`} onClick={onOpen}>{t("rows.viewPages", { count: 1 })}</button>
        </div>
      ) : null}
      {requestId ? (
        <InboxAccessRequest requestId={requestId} requests={requests} readerId={readerId} unread={unread}
          onMarkRead={onMarkRead} onArchive={archived ? undefined : onArchive} />
      ) : noticeId && deadlines.acknowledged[noticeId] ? (
        <div className={styles.requestState} role="status" data-deadline-acknowledged>{t("rows.deadlineAcknowledged")}</div>
      ) : noticeId && deadlines.acknowledged[noticeId] === false ? (
        <div className={styles.requestActions} data-deadline-actions>
          <button type="button" className={styles.requestAction} data-primary="true" disabled={Boolean(deadlines.busy)}
            onClick={() => void deadlines.acknowledge(noticeId).then((done) => { if (done && unread) onMarkRead(); })}>
            {t("rows.acknowledge")}
          </button>
          {deadlines.failed === noticeId ? <span className={styles.requestActionNote} role="alert" data-error>{t("rows.acknowledgeFailed")}</span> : null}
        </div>
      ) : null}
      {phone ? null : <div className={styles.tools} role="group" aria-label={label} data-inbox-row-tools>
        {archived ? (
          <button type="button" className={styles.tool} aria-label={t("inbox.unarchive")} title={t("inbox.unarchive")}
            data-inbox-unarchive onClick={onUnarchive}>
            <UnarchiveIcon size="small" aria-hidden="true" />
          </button>
        ) : (
          <>
            {notification.pageId && !isPagelessNotice(notification) ? (
              <button type="button" className={styles.tool} aria-label={t("inbox.pageSettings")} title={t("inbox.pageSettings")}
                data-inbox-page-settings onClick={(event) => onPageSettings(event.currentTarget)}>
                <Bell size="small" aria-hidden="true" />
              </button>
            ) : null}
            <button type="button" className={styles.tool} aria-label={t(unread ? "inbox.markRead" : "inbox.markUnread")}
              title={t(unread ? "inbox.markRead" : "inbox.markUnread")} data-inbox-toggle-read
              onClick={unread ? onMarkRead : onMarkUnread}>
              {unread ? <MarkReadIcon size="small" aria-hidden="true" /> : <MarkUnreadIcon size="small" aria-hidden="true" />}
            </button>
            <button type="button" className={styles.tool} aria-label={t("inbox.archive")} title={t("inbox.archive")}
              data-inbox-archive onClick={onArchive}>
              <ArchiveIcon size="small" aria-hidden="true" />
            </button>
          </>
        )}
      </div>}
      </div>
      {gestures.drawn ? (
        <div className={touchStyles.swipeGround}>
          <button type="button" className={touchStyles.swipeAction} data-inbox-swipe-action onClick={swiped(archived ? onUnarchive : onArchive)}>
            {archived ? <UnarchiveIcon size="regular" aria-hidden="true" /> : <ArchiveIcon size="regular" aria-hidden="true" />}
            <span>{t(archived ? "inbox.unarchive" : "inbox.touchArchive")}</span>
          </button>
        </div>
      ) : null}
      {cardAt ? (
        <InboxRowCard row={cardAt} label={label} unread={unread} archived={archived} onToggleRead={toggleRead}
          onArchive={onArchive} onUnarchive={onUnarchive} onClose={() => setCardAt(null)} />
      ) : null}
    </li>
  );
}
