import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  ArchiveIcon, ArrowBendUpRightIcon, Bell, ClipboardIcon, UpdatesClockIcon, CommentIcon, ConnectionsIcon, Copy, DotsHorizontal, Download, ExportIcon,
  EyeLineIcon, ImportIcon, MergeCsvIcon, LinkIcon, LockIcon, PageLayoutIcon, PresentIcon, RepeatIcon, SuggestEditIcon, TableOfContentsIcon, Trash, UndoIcon,
  UnlockIcon, VersionHistoryIcon,
} from "@/icons/leafen";
import { shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import type { PageNotificationLevel } from "@/notificationSettingsApi";
import type { Page } from "@/types";
import type { PagePresentationChange } from "./page/pagePresentation";
import { everyRow, menuCheck, menuSwitch, pageMenuItem, suggestingStop, type PageMenuEntry, type PageMenuShow, type PageMenuSubmenu } from "./PageMenu";
import { usePageThemeSubmenu } from "./PageStyleControls";
import { notifyValueKey, RowMenuChoiceSubmenu } from "./RowMenuSubmenus";
import styles from "./RowMenu.module.css";
import { pageMenuSearchWords } from "@/lib/pageMenuSearchWords";
import { menuKeyHint } from "@/lib/menuKeyHint";
import { useGuestEditsPageAbove } from "@/lib/pageActionAccessContext";
import { pageMenuRows } from "./lockedPageMenu";

/** Choices a page menu row opens beside the menu, drawn by the menu that reads them. */
export type RowMenuPanels = Partial<Record<"notify" | "connections", (row: HTMLElement, onClose: () => void, onCloseAll: () => void) => ReactNode>>;

const NOTIFY_LEVELS = ["all_comments", "replies_and_mentions"] as const;
const INLINE_COMMENTS = ["default", "minimal"] as const;

/**
 * A page's "…" menu below its search field, as Notion lays it out (seen
 * 2026-10-17): link, contents, duplicate, move, history, updates | 테마와
 * 레이아웃 › | suggest edits, notifications › | lock, archive, trash | 더 보기 ›
 * (presentation mode, undo, import, export, Leafen's read log, wiki |
 * connections › | offline).
 * Notion's "AI로 사용 ›" isn't here: Leafen keeps no skills and runs no
 * agent of its own (the agents-outside decision of 2026-10-29); nor "번역"
 * (no AI in the editor). A wiki's own menu, as Notion's, leaves out copying contents,
 * presenting, full width and undo, lists "CSV와 병합" by duplicate and move,
 * and offers the page settings it keeps (small text, inline comments) in its
 * theme; a database's page has "복제 ›" with or without its rows and
 * "레이아웃 사용자 지정" in place of the theme and suggestions.
 */
export function useRowMenuGroups({
  archiveState, busy, offlinePinned, page, lockedForMe, guest = false, canEdit = true, canMove = canEdit, databasePage = false, trashed = false,
  onArchive, onCopyLink, onCopyContents, onDuplicate, onDuplicateWithoutContent, onDuplicateToPrivate, duplicateToWorkspace, onExport, onHistory, onAnalytics,
  onReadLog,
  onImport, onMergeCSV, onCustomizeLayout, onPresent, onLock, lockKind = "page", locked = page.isLocked, onMove, notifyLevel, onNotifyLevel, onNotifyRetry,
  connectionsValue, onConnections, onOffline, onPresentationChange, onTrash, suggesting = false, onSuggesting, onUndo, onWiki, panels = {},
}: {
  /** "inherited" when the page is archived with an archived parent page. */
  archiveState: "active" | "archived" | "inherited";
  busy: boolean;
  offlinePinned: boolean;
  page: Page;
  /** The page's lock stops this person's changes. */
  lockedForMe: boolean;
  /** This person may change the page; a view-only page offers no changes. */
  /** The reader is a guest here: no contents to copy or export, whatever they can edit. */
  guest?: boolean;
  canEdit?: boolean;
  /** This person has the page's full access, which moving it asks; without it "옮기기" shows dimmed, as Notion's. */
  canMove?: boolean;
  /** The page is in the trash, or under a page that is. */
  trashed?: boolean;
  /** A full-page or linked database's own page. */
  databasePage?: boolean;
  onArchive?: () => void;
  onCopyLink(): void;
  /** "페이지 내용 복사하기": the page's contents as Markdown on the clipboard. */
  onCopyContents(): void;
  onDuplicate(): void;
  onDuplicateWithoutContent?: () => void;
  /** "개인 페이지로 복제": a copy at the top of one's private pages. */
  onDuplicateToPrivate?: () => void;
  /** A guest's "페이지 복제 ›", to a workspace of their own (useGuestDuplicateToWorkspace). */
  duplicateToWorkspace?: PageMenuSubmenu;
  onExport(): void;
  onHistory(): void;
  /** Opens the page's updates and analytics. */
  onAnalytics?: () => void;
  /** Opens the page's read log (Leafen's own); the server holds it to full access. */
  onReadLog?: () => void;
  /** Opens the import screen, which imports into the page. */
  onImport?: () => void;
  /** A full-page database's or a wiki's "CSV와 병합", by duplicate and move (seen 2026-10-17). */
  onMergeCSV?: () => void;
  /** A database page's "레이아웃 사용자 지정", opening its page layout builder. */
  onCustomizeLayout?: () => void;
  /** "프레젠테이션 모드": presents the page from its title. */
  onPresent?: () => void;
  onLock(): void;
  /**
   * What the lock is called: a full-page database's "데이터베이스 잠금" and a
   * linked database's page's "보기 잠금", as Notion's (seen 2026-10-12).
   */
  lockKind?: "page" | "database" | "views";
  /** Whether what the lock row locks is locked: the page, or an inline database opened alone. */
  locked?: boolean;
  onMove(): void;
  /** The page's "Notify me" choice once loaded; null while unknown. */
  notifyLevel?: PageNotificationLevel | null;
  onNotifyLevel?: (level: PageNotificationLevel) => void;
  /** The "Notify me" choice couldn't be read; choosing the row reads it again. */
  onNotifyRetry?: () => void;
  /** "연결"'s value: "없음" or how many apps are connected; empty while read. */
  connectionsValue?: string;
  /** The page has connections to show, for those who may change it. */
  onConnections?: boolean;
  onOffline(): void;
  onPresentationChange(change: PagePresentationChange): void;
  onTrash(): void;
  /** Suggesting mode is on for this page. */
  suggesting?: boolean;
  /** Turns suggesting mode on or off, where the reader may suggest edits. */
  onSuggesting?: () => void;
  /** "실행 취소": undoes the page's last change in its editor, as ⌘Z there. */
  onUndo?: () => void;
  onWiki?: () => void;
  panels?: RowMenuPanels;
}): PageMenuEntry[][] {
  const { t } = useTranslation(["rowMenu", "topBar", "common"]);
  // Every row is made; the menu's search finds and ranks them (usePageMenu).
  const visible: PageMenuShow = everyRow;
  const item = pageMenuItem(busy);
  const wikiRoot = page.isWiki && page.wikiRootId === page.id;
  const plainPage = !wikiRoot && !databasePage;
  const styleLocked = busy || lockedForMe;
  // Locked, or suggested in, the page keeps fewer rows (lockedPageMenu.ts).
  const lockRows = pageMenuRows(lockedForMe, suggesting);
  // A locked database's menu keeps no lock, move, merge or history row, its
  // lock mark unlocking it, as Notion's (seen 2026-11-11).
  const databaseLocked = databasePage && lockKind !== "page" && locked && lockedForMe;

  // "인라인 댓글 ›" (240px): every comment shown, or a mark in the margin.
  const inlineLabel = t("rowMenu:customize.inlineComments");
  const inlineValue = page.inlineCommentsDisplay ?? "default";
  const inlineChoiceLabel = (value: typeof INLINE_COMMENTS[number]) => t(value === "minimal" ? "rowMenu:customize.inlineMinimal" : "rowMenu:customize.inlineDefault");
  const chooseInline = (value: typeof INLINE_COMMENTS[number]) => { if (!styleLocked) onPresentationChange({ inlineCommentsDisplay: value }); };
  const inlineComments: PageMenuSubmenu = {
    submenu: "inline", label: inlineLabel, icon: <CommentIcon size="regular" aria-hidden="true" />, width: 240, disabled: styleLocked,
    data: { "data-page-inline-comments": inlineValue },
    panel: (row, onClose) => (
      <RowMenuChoiceSubmenu row={row} label={inlineLabel} width={240} marker="page-inline-comments-choice" value={inlineValue}
        choices={INLINE_COMMENTS.map((value) => ({ value, label: inlineChoiceLabel(value) }))} onChoose={chooseInline} onClose={onClose} />
    ),
    // A search lists the choices under its name.
    sections: (show, searching) => searching ? [{
      rows: INLINE_COMMENTS.map((value) => item(show, `inline-${value}`, inlineChoiceLabel(value), null, () => chooseInline(value),
        { disabled: lockedForMe, role: "menuitemradio", checked: inlineValue === value, trailing: menuCheck(inlineValue === value),
          data: { "data-page-inline-comments-choice": value } })),
    }] : [],
  };
  const theme = usePageThemeSubmenu({
    busy, disabled: styleLocked, fullWidth: !wikiRoot, styled: lockRows.pageStyle, themeHead: true,
    style: { font: page.font ?? "default", smallText: Boolean(page.smallText), fullWidth: Boolean(page.fullWidth) },
    onChange: onPresentationChange,
    rows: (show) => [
      wikiRoot || !lockRows.pageStyle ? null : item(show, "outline", t("rowMenu:customize.tableOfContents"), <TableOfContentsIcon size="regular" aria-hidden="true" />,
        () => onPresentationChange({ tableOfContentsDisplay: page.tableOfContentsDisplay === "off" ? "default" : "off" }),
        { disabled: lockedForMe, role: "menuitemcheckbox", checked: page.tableOfContentsDisplay !== "off", trailing: menuSwitch(page.tableOfContentsDisplay !== "off"),
          data: { "data-page-outline-toggle": "" } }),
      wikiRoot ? null : item(show, "page-comments", t("rowMenu:customize.pageComments"), <CommentIcon size="regular" aria-hidden="true" />,
        () => onPresentationChange({ pageCommentsDisplay: page.pageCommentsDisplay === "off" ? "default" : "off" }),
        { disabled: lockedForMe, role: "menuitemcheckbox", checked: page.pageCommentsDisplay !== "off", trailing: menuSwitch(page.pageCommentsDisplay !== "off"),
          data: { "data-page-comments-toggle": "" } }),
      inlineComments,
    ],
  });

  const notifyLabel = t("rowMenu:notify.label");
  const notify: PageMenuSubmenu | null = onNotifyLevel ? {
    submenu: "notify", label: notifyLabel, icon: <Bell size="regular" aria-hidden="true" />, width: 300,
    value: notifyLevel ? t(`rowMenu:${notifyValueKey(notifyLevel)}`) : onNotifyRetry ? t("rowMenu:notify.loadFailed") : "",
    disabled: busy || (!notifyLevel && !onNotifyRetry), onChoose: !notifyLevel && onNotifyRetry ? onNotifyRetry : undefined,
    data: { "data-page-notify-level": notifyLevel ?? "", ...(onNotifyRetry && !notifyLevel ? { "data-read-failed": "" } : {}) },
    panel: panels.notify,
    // A search lists the choices under its name, once read, each found by its own word ("댓글", "멘션": pageMenuSearchWords).
    sections: notifyLevel ? (show, searching) => searching ? [{
      rows: NOTIFY_LEVELS.map((level) => item(show, `notify-${level}`, t(level === "all_comments" ? "rowMenu:notify.allComments" : "rowMenu:notify.repliesAndMentions"),
        null, () => onNotifyLevel(level), { role: "menuitemradio", checked: notifyLevel === level, trailing: menuCheck(notifyLevel === level),
          data: { "data-page-notify-choice": level },
          search: pageMenuSearchWords(level === "all_comments" ? "allComments" : "mentions") })),
    }] : [] : undefined,
  } : null;

  const wikiLabel = t(wikiRoot ? "topBar:undoWiki" : "topBar:turnIntoWiki");
  // Undoing a wiki asks over the closed menu (confirmWikiUndo).
  const wikiRow = (show: PageMenuShow) => canEdit && onWiki && !guest && lockRows.wiki
    ? item(show, "wiki", wikiLabel, <RepeatIcon size="regular" aria-hidden="true" />, onWiki) : null;

  const connections: PageMenuSubmenu | null = onConnections && canEdit && panels.connections && lockRows.connections ? {
    submenu: "connections", label: t("rowMenu:actions.connections"), search: pageMenuSearchWords("connections"),
    icon: <ConnectionsIcon size="regular" aria-hidden="true" />, width: 350,
    value: connectionsValue ?? "", disabled: busy, data: { "data-page-connections-action": "" }, panel: panels.connections,
  } : null;
  const more: PageMenuSubmenu = {
    submenu: "more", label: t("rowMenu:more"), icon: <DotsHorizontal size="regular" aria-hidden="true" />, width: 280, data: { "data-page-more-menu": "" },
    sections: (show) => [
      {
        rows: [
          // "프레젠테이션 모드 [베타]" leads, as Notion's (seen 2026-10-17).
          plainPage ? item(show, "present", t("rowMenu:actions.presentationMode"), <PresentIcon size="regular" aria-hidden="true" />, onPresent,
            { hint: shortcutCompactLabel("presentPage"), badge: t("rowMenu:actions.beta"), data: { "data-page-present-action": "" } }) : null,
          // A guest's "더 보기" has no undo nor wiki, as Notion's (seen 2026-11-16); the server makes no wiki of theirs.
          canEdit && !wikiRoot && !guest && lockRows.undo ? item(show, "undo", t("rowMenu:actions.undo"), <UndoIcon size="regular" aria-hidden="true" />, onUndo,
            { hint: menuKeyHint("⌘Z"), data: { "data-page-undo-action": "" } }) : null,
          canEdit && lockRows.import ? item(show, "import", t("rowMenu:actions.import"), <ImportIcon size="regular" aria-hidden="true" />, onImport,
            { data: { "data-page-import-action": "" } }) : null,
          item(show, "export", t("rowMenu:actions.export"), <ExportIcon size="regular" aria-hidden="true" />, onExport,
            { disabled: !canEdit || guest, data: { "data-page-export-action": "" } }),
          // Leafen's read log follows the export, so a page's menu is no taller than Notion's and needs no scrolling in a
          // 723px window. It takes full access (read_log_routes.go), which a guest has only where given it; a member may
          // also be a compliance administrator.
          canEdit && (!guest || canMove) ? item(show, "read-log", t("rowMenu:actions.readLog"), <EyeLineIcon size="regular" aria-hidden="true" />, onReadLog,
            { data: { "data-page-read-log-action": "" } }) : null,
          databasePage ? null : wikiRow(show),
        ],
      },
      { rows: [connections] },
      {
        rows: [lockRows.offline ? item(show, "offline", t("topBar:availableOffline"), <Download size="regular" aria-hidden="true" />, onOffline,
          { role: "menuitemcheckbox", checked: offlinePinned, data: { "data-testid": "offline-pin-toggle" }, trailing: menuSwitch(offlinePinned) }) : null],
      },
    ],
  };

  // A guest copies beside the page and throws it away only under a page above they edit (useGuestEditsPageAbove).
  const editsAbove = useGuestEditsPageAbove(page.parentId, guest);
  const duplicate = canEdit && editsAbove && lockRows.duplicate ? (onDuplicateWithoutContent ? {
    // A database's "복제 ›": with its rows or without them (seen 2026-10-17).
    submenu: "duplicate", label: t("rowMenu:actions.duplicate"), icon: <Copy size="regular" aria-hidden="true" />, width: 275,
    data: { "data-page-duplicate-menu": "" },
    sections: (show: PageMenuShow) => [{
      rows: [
        // Its two rows have no icon, each with its key, as Notion's (seen 2026-11-07).
        item(show, "duplicate", t("rowMenu:actions.duplicateWithContent"), null, onDuplicate, { hint: menuKeyHint("⌘D") }),
        item(show, "duplicate-empty", t("rowMenu:actions.duplicateWithoutContent"), null,
          onDuplicateWithoutContent, { hint: menuKeyHint("⌘⇧D"), data: { "data-duplicate-without-content": "" } }),
      ],
    }],
  } satisfies PageMenuSubmenu : item(visible, "duplicate", t("rowMenu:actions.duplicate"), <Copy size="regular" aria-hidden="true" />, onDuplicate, { hint: menuKeyHint("⌘D") })) : null;

  // A page in the trash keeps only its updates and its export, as Notion's
  // (seen 2026-11-15, a trashed page's and a trashed database's "…" under its
  // search: "업데이트와 애널리틱스", "내보내기", then the edited foot; a
  // database's "데이터베이스 사용법" opens Notion's help center and is left out).
  if (trashed) return [[
    item(visible, "analytics", t("topBar:updatesAndAnalytics"), <UpdatesClockIcon size="regular" aria-hidden="true" />, onAnalytics,
      { data: { "data-page-analytics-action": "" }, search: pageMenuSearchWords("updatesAndAnalytics") }),
    item(visible, "export", t("rowMenu:actions.export"), <ExportIcon size="regular" aria-hidden="true" />, onExport,
      { disabled: guest, data: { "data-page-export-action": "" } }),
  ]];
  return [
    [
      item(visible, "link", t("rowMenu:actions.copyLink"), <LinkIcon size="regular" aria-hidden="true" />, onCopyLink, { hint: shortcutCompactLabel("copyPageLink") }),
      // Someone who can't edit the page can't copy its contents or export it, nor can a guest who can
      // edit, without a word of why, as in Notion (seen 2026-10-08, 2026-11-05).
      plainPage ? item(visible, "contents", t("rowMenu:actions.copyContents"), <ClipboardIcon size="regular" aria-hidden="true" />, onCopyContents,
        { disabled: !canEdit || guest, data: { "data-page-copy-contents": "" } }) : null,
      guest && canEdit && plainPage && lockRows.duplicate ? duplicateToWorkspace ?? null : null,
      duplicate,
      canEdit && lockRows.duplicate ? item(visible, "duplicate-private", t("rowMenu:actions.duplicateToPrivate"), <Copy size="regular" aria-hidden="true" />,
        onDuplicateToPrivate, { data: { "data-duplicate-to-private": "" } }) : null,
      // A locked page can't move, nor go back to an earlier version, as Notion's (lockedPageMenu.ts).
      canEdit && lockRows.move ? item(visible, "move", t("rowMenu:actions.moveTo"), <ArrowBendUpRightIcon size="regular" aria-hidden="true" />, onMove,
        { hint: menuKeyHint("⌘⇧P"), disabled: !canMove }) : null,
      canEdit && !databaseLocked ? item(visible, "merge-csv", t("rowMenu:actions.mergeCSV"), <MergeCsvIcon size="regular" aria-hidden="true" />, onMergeCSV,
        { disabled: lockedForMe, data: { "data-page-merge-csv-action": "" } }) : null,
      // The history and the updates are found by their own words too (pageMenuSearchWords: "2.0", "수정").
      canEdit && lockRows.history ? item(visible, "history", t("rowMenu:actions.pageHistory"), <VersionHistoryIcon size="regular" aria-hidden="true" />, onHistory,
        { search: pageMenuSearchWords("pageHistory") }) : null,
      canEdit ? item(visible, "analytics", t("topBar:updatesAndAnalytics"), <UpdatesClockIcon size="regular" aria-hidden="true" />, onAnalytics,
        { data: { "data-page-analytics-action": "" }, search: pageMenuSearchWords("updatesAndAnalytics") }) : null,
    ],
    databasePage ? [
      canEdit && lockRows.pageStyle ? item(visible, "layout", t("rowMenu:actions.customizeLayout"), <PageLayoutIcon size="regular" aria-hidden="true" />, onCustomizeLayout,
        { disabled: lockedForMe, data: { "data-page-customize-layout": "" } }) : null,
      notify,
    ] : [canEdit ? (lockedForMe ? { ...theme, disabled: true } : theme) : null],
    databasePage ? [] : [
      // Offered only where it may be used: to one who can comment, and not on a locked page.
      item(visible, "suggest", t("rowMenu:actions.suggestEdits"), <SuggestEditIcon size="regular" aria-hidden="true" />, onSuggesting,
        { data: { "data-page-suggest-action": "" }, trailing: suggesting ? suggestingStop(t("rowMenu:actions.stopSuggesting")) : null }),
      notify,
    ],
    canEdit ? [
      databaseLocked || !lockRows.lock ? null : item(visible, "lock", t(wikiRoot ? "rowMenu:actions.wikiLock" : lockKind === "views" ? "rowMenu:actions.viewLock"
        : lockKind === "database" ? "rowMenu:actions.databaseLock" : "rowMenu:actions.pageLock"),
        locked ? <LockIcon size="regular" aria-hidden="true" /> : <UnlockIcon size="regular" aria-hidden="true" />,
        onLock, { role: "menuitemcheckbox", checked: locked, trailing: menuSwitch(locked) }),
      // Locked, the page can't be archived or thrown away from here (Notion: 휴지통으로 이동 dimmed).
      // A locked page offers no 보관, as Notion's (seen 2026-11-05); an archived one can still come back.
      // A guest archives nothing (page_metadata_guest.go), as Notion's guest has no 보관.
      !guest && archiveState !== "inherited" && !(locked && lockedForMe && archiveState === "active") ? item(visible, "archive", t(archiveState === "archived" ? "rowMenu:actions.unarchive" : "rowMenu:actions.archive"),
        <ArchiveIcon size="regular" aria-hidden="true" />, onArchive, { disabled: lockedForMe, data: { "data-page-archive-action": archiveState } }) : null,
      item(visible, "trash", t("rowMenu:actions.moveToTrash"), <Trash size="regular" aria-hidden="true" />,
        onTrash, { disabled: lockedForMe || !editsAbove, className: styles.dangerOnHover, data: { "data-page-trash-action": "" } }),
    ] : [],
    [more],
  ];
}
