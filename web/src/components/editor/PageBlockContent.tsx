import { useDatabasePageTurning } from "@/lib/turningDatabasePages";
import { useMemo, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import type { GraphBlock } from "@/contentGraph";
import { openComments } from "@/components/comments/commentPanelEvents";
import { AnchoredIconPicker, PICKER_WIDTH } from "@/components/page/AnchoredEmojiPicker";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { TitleWithCustomEmoji } from "@/components/page/TitleWithCustomEmoji";
import i18next from "i18next";
import { DotsHorizontal, EyeSlashIcon } from "@/icons/leafen";
import { TRASH_MARK, TRASH_MARK_BACKGROUND, TRASH_MARK_COLOR } from "./trashMark";
import { useInlineDatabaseRequests } from "@/lib/inlineDatabaseRequests";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import { blockAnchor, blockIsPageItself } from "@/lib/blockAddress";
import { inlineDatabaseFullPageHref } from "@/lib/inlineDatabaseFullPage";
import { databasePageHref, databasePageViewUnknown, readDatabasePageViews, useDatabasePageViewsRead } from "@/lib/databasePageLink";
import { readDatabaseViewChoice, useDatabaseViewChoices } from "@/lib/databaseViewChoice";
import { pageHref } from "@/lib/navigation";
import { pageLookup } from "@/lib/pageArchive";
import { pageTrashRoot } from "@/lib/pageTrash";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { useNamedPage, usePageNames } from "@/lib/pageNames";
import { usePageLinkTarget, type PageLinkTargetKind } from "@/lib/pageLinkTargets";
import { usePublicPageHref } from "@/lib/publicPageLinks";
import { openInSidePeek, openPagePeek } from "@/lib/pagePeek";
import { pushRoute } from "@/lib/router";
import type { Page } from "@/lib/types";
import type { Page as WorkspacePage } from "@/types";
import { takeIconCompanions } from "./blockIconCompanions";
import { blockItemText } from "./blockItemLabels";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { PageLinkPicker } from "./PageLinkPicker";
import { isDatabaseSourceTitle, type DatabaseSourceTitle } from "./databaseTitles";
import type { PageLinkChoice } from "./pageLinkChoices";
import { staticProjectionSpans } from "./nativeTiptapGraph";
import { BreadcrumbBlock, TableOfContentsBlock } from "./PageOutlineBlocks";
import { selectBlockWithoutCaret } from "./slashBlockPlacement";
import { requestEditorBlockCommand } from "./editorCommands";
import { childPageBlockPageId } from "./useBlockPageMenu";
import styles from "./editorStyles.generated.module.css";
import { inlineStartX } from "@/lib/readingDirection";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

type LinkedPages = Readonly<Record<string, Pick<Page, "title" | "icon" | "iconType">>>;

type LinkedTarget = { kind: PageLinkTargetKind; id: string; publicPageId?: string; publicBlockId?: string };

// Link blocks name a page with childPageId (older content used pageId), a
// database with databaseContainerId, or a comment with commentId. A public
// page's projection names instead the page that holds a database or comment
// it can open, and the database's place on it.
function linkedTarget(content: GraphBlock["content"]): LinkedTarget {
  const projected = content.publicLinkKind;
  if ((projected === "database" || projected === "comment") && typeof content.publicLinkPageId === "string") {
    return {
      kind: projected, id: "", publicPageId: content.publicLinkPageId,
      publicBlockId: typeof content.publicLinkBlockId === "string" ? content.publicLinkBlockId : undefined,
    };
  }
  for (const [kind, value] of [
    ["page", content.childPageId], ["page", content.pageId],
    ["database", content.databaseContainerId], ["comment", content.commentId],
  ] as const) {
    if (typeof value === "string" && value.trim()) return { kind, id: value.trim() };
  }
  return { kind: "page", id: "" };
}

// The page each link block last opened where it isn't the one it names: a
// database's or comment's page, or where a page turned database went.
const shownPages = new Map<string, string>();

/** The page a link block opens beside the one it names, as it last drew (its widget reads that page's entries too). */
export function pageLinkShownPage(blockId: string) {
  return shownPages.get(blockId);
}

function useLinkedPage(block: GraphBlock, adapter?: NativeEditorGraphAdapter, pagesById?: LinkedPages) {
  const publicHref = usePublicPageHref();
  const target = linkedTarget(block.content);
  // In the app the server says what the target is for this reader; a public
  // page shows what the server projected for it.
  const resolved = usePageLinkTarget(publicHref ? null : adapter?.session, adapter?.pageId, target.kind, target.id);
  // A link to the page a full-page database left as it turned inline leads
  // to the database, where the server says it is now.
  const moved = target.kind === "page" && Boolean(resolved?.pageId) && resolved?.pageId !== target.id;
  const pageId = target.kind === "page" && !moved ? target.id : target.publicPageId ?? resolved?.pageId ?? "";
  if (pageId && pageId !== target.id) shownPages.set(block.id, pageId); else shownPages.delete(block.id);
  // Each named as it is named everywhere now, followed (lib/pageNames.ts).
  const page = useNamedPage(publicHref || target.kind !== "page" || moved ? undefined : pageId ? pagesById?.[pageId] : undefined);
  // The workspace's own record of the page, when the reader has it; a
  // full-page database turning inline still a database (turningDatabasePages).
  const turning = useDatabasePageTurning(pageId);
  // A database's link names that page only when it is the database's own, a
  // full-page one's: the page holding a database in a page is not it (its
  // name, icon and block menu are the database's, as Notion's link shows).
  const listedPage = pageId && !publicHref && !moved
    ? adapter?.moveContext?.pages.find((candidate) => candidate.id === pageId
      && (target.kind !== "database" || candidate.kind === "database" || turning)) : undefined;
  const workspacePage = useNamedPage(listedPage && turning ? { ...listedPage, kind: "database" as const } : listedPage);
  const stored = [block.content.childPageTitle, block.content.title]
    .find((value): value is string => typeof value === "string" && value.trim() !== "");
  // An untitled link reads as Notion's: "새 페이지", or "새 데이터베이스" for a
  // database, "제목 없음" for a view block's page, "A 및 B" for a database of
  // several sources by their titles, which a public page's link carries
  // (public_content_page_link_projection.go) and the server says of a
  // database to its reader (page_link_targets.go) (untitledPageDisplayTitle).
  // A link to a full-page database's own reads as its page does.
  const projectedSources = publicHref ? block.content.childPageSourceTitles : resolved?.sourceTitles;
  const untitledKind = Array.isArray(projectedSources) && projectedSources.every(isDatabaseSourceTitle)
    ? { kind: "database", sourceTitles: projectedSources as DatabaseSourceTitle[] }
    : target.kind === "database" ? (workspacePage?.kind === "database" ? workspacePage : "database")
    : workspacePage ?? (block.content.childPageKind === "database" ? "database" : "page");
  const untitled = untitledPageDisplayTitle(untitledKind);
  // A title's mentions say themselves as a plain title says them, as Notion's
  // sub-page line does (seen 2026-11-10: "W202 제목 @W202 대상 와 …"), by the
  // spans of the page it names (closedMentions.ts).
  // A public page's link carries its target's title's spans as the visitor
  // reads them (public_content_page_links.go), as a member's list has them.
  const projectedSpans = publicHref ? staticProjectionSpans(block.content.childPageTitleSpans) : undefined;
  // A title of spaces alone is a title, its line blank, as Notion's (pageTitle.ts).
  const named = page?.title ? page : workspacePage?.title ? workspacePage
    : projectedSpans?.length && stored ? { title: stored, titleSpans: projectedSpans } : undefined;
  const title = target.kind === "comment"
    ? blockItemText("pageLink.comment")
    : titleWithClosedMentions(named?.title || resolved?.title || stored || "", named && "titleSpans" in named ? named.titleSpans : undefined) || untitled;
  const iconType = page ? page.iconType : resolved ? resolved.iconType : block.content.childPageIconType;
  const icon = page ? page.icon : resolved ? resolved.icon : block.content.childPageIcon;
  const emoji = target.kind === "comment" ? "💬" : iconType === "emoji" && typeof icon === "string" && icon ? icon : "";
  // That record shows the page's icon of any kind; a public page's link
  // says whether its target holds anything, its empty or lined document, and
  // so draws the target's icon as a member's link does.
  const filled = publicHref ? block.content.childPageHasContent : undefined;
  const projected = typeof filled === "boolean";
  const iconSource = workspacePage ?? {
    icon: typeof icon === "string" ? icon : "",
    iconType: (typeof iconType === "string" ? iconType : "none") as Page["iconType"],
    // A database's, with no page of the reader's list for it, by the database's mark.
    kind: target.kind === "database" || moved ? "database"
      : typeof block.content.childPageKind === "string" ? block.content.childPageKind : undefined,
    ...(typeof filled === "boolean" ? { hasContent: filled } : {}),
  };
  const drawsIcon = projected || !workspacePage && iconSource.kind === "database";
  const blockId = target.kind === "database" ? target.publicBlockId ?? resolved?.blockId : resolved?.blockId;
  // A database's link, and one to the page a full-page database left as it
  // turned inline, opens the database by its own address — alone where it is
  // in a page — as Notion's link block and mention do (seen 2026-11-22: a
  // full-page one's and one in a page's both /p/<database>?v=, nothing
  // chosen); a public page's at its place on the page holding it. A
  // comment's opens its page at its block; a public page shows no comments,
  // so there its page. None names the page itself (blockAddress.ts).
  const database = (target.kind === "database" || moved) && !publicHref && pageId && blockId ? blockId : "";
  const fullPage = Boolean(database) && (blockIsPageItself(pageId, database) || workspacePage?.kind === "database");
  const anchor = target.kind === "page" && !moved || database || publicHref && target.kind === "comment" ? "" : blockAnchor(pageId, blockId);
  // A database's address names the view it opens at, as Notion's links to a
  // database do (databasePageHref; seen twice 2026-11-25, ⌘-click opening a
  // new tab at it): a full-page one's, the person's choice there, else its
  // saved or first view once read — read as the pointer or the focus comes to
  // a link to one with none chosen yet, so its press names it; one in a page
  // opened alone, the person's choice there, as its "전체 페이지로 열기" names
  // the view it shows. Drawn again as those change.
  const userId = adapter?.session?.user.id;
  const databasePage = !publicHref && pageId && (database ? fullPage : workspacePage?.kind === "database")
    ? { id: pageId, kind: "database", isWiki: workspacePage?.isWiki } : null;
  useDatabaseViewChoices(Boolean(database || databasePage));
  useDatabasePageViewsRead(Boolean(databasePage));
  const inlineView = database && !fullPage ? readDatabaseViewChoice(userId, database) : null;
  const href = !pageId ? "" : databasePage ? databasePageHref(databasePage, userId)
    : database ? inlineDatabaseFullPageHref(pageId, database, inlineView ?? undefined)
    : (publicHref ? publicHref(pageId) : pageHref(pageId)) + anchor;
  const readView = databasePage && databasePageViewUnknown(databasePage, userId)
    ? () => { void readDatabasePageViews(adapter?.session, databasePage); } : undefined;
  // A page of the reader's own list opens in a side peek on ⌥Click, as Notion's.
  const peekable = target.kind === "page" && Boolean(pageId) && !publicHref && Boolean(page ?? workspacePage);
  const onClick = (event: MouseEvent<HTMLAnchorElement>) => {
    if (event.button === 0 && event.altKey && !event.metaKey && !event.ctrlKey && !event.shiftKey && peekable && pageId) {
      event.preventDefault();
      openPagePeek(pageId);
      return;
    }
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    // Pressed in a side peek, a page of the reader's own list opens in it (openInSidePeek).
    if (peekable && pageId && !anchor && openInSidePeek(pageId, event.currentTarget)) return;
    pushRoute(href);
    if (target.kind === "comment" && pageId && !publicHref) {
      window.requestAnimationFrame(() => openComments({ pageId, activeCommentId: target.id, blockId: resolved?.blockId ?? null }));
    }
  };
  // A database or comment has a page to open only once the server says so;
  // on a public page, only when its projection names that page. A page in
  // the reader's own page list is theirs to open, though the server was
  // asked before it was made, as for a block just turned into a page.
  const listed = Boolean(page ?? workspacePage);
  const unavailable = block.content.pageLinkUnavailable === true || (resolved?.available === false && !listed)
    || (publicHref !== null && publicHref !== undefined && target.kind !== "page" && !target.publicPageId);
  const copying = resolved?.copying === true && !listed;
  // In the trash, the page still opens, under the trash's banner; its link
  // says so. The reader's own list says it as it changes (a restore clears it
  // at once); the server's answer, kept a while, where the list hasn't it.
  // A sub-page goes to the trash with the page holding it, so it is in the
  // trash when that page is (Notion's link shows it, seen 2026-11-07).
  const listedPages = adapter?.moveContext?.pages;
  const byId = useMemo(() => pageLookup(listedPages ?? []), [listedPages]);
  const inTrash = workspacePage ? pageTrashRoot(workspacePage, byId) !== null : resolved?.inTrash === true;
  // It names something, though a database's or comment's page comes later.
  const namesTarget = Boolean(target.id || target.publicPageId);
  // Its title's custom emoji are drawn by the page's own spans, as Notion's (TitleWithCustomEmoji).
  const titled = target.kind === "comment" ? undefined : named;
  return { pageId, namesTarget, title, titled, emoji, iconSource, projected, drawsIcon, workspacePage, href, onClick, readView, unavailable, copying, inTrash };
}

function NoAccess() {
  return <div className={styles.childPageMissing} data-page-link-no-access="true">
    <span className={styles.childPageIcon}>↗</span>{blockItemText("pageLink.noAccess")}</div>;
}

/**
 * A sub-page or full-page database in the page: its icon and title, the
 * page's card when the pointer rests on it (mentionPreviewCard.tsx), and its
 * block menu on right-click or the menu key. As Notion's (seen 2026-10-31),
 * it has no buttons of its own: opening in a new tab and copying its link
 * are its block menu's.
 */
function ChildPageBlock({ adapter, block, pagesById }: { adapter?: NativeEditorGraphAdapter; block: GraphBlock; pagesById?: LinkedPages }) {
  const link = useLinkedPage(block, adapter, pagesById);
  const [iconAnchor, setIconAnchor] = useState<{ top: number; bottom: number; left: number; contextElement?: Element } | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const linkRef = useRef<HTMLAnchorElement>(null), companions = useRef<readonly string[]>([]);
  const context = adapter?.moveContext;
  const menuPage = context?.pageMenu ? link.workspacePage : undefined;
  // Its block menu's "아이콘 편집" opens the page's icon picker by the block,
  // and "이름 바꾸기" names the page in place, as Notion's (seen 2026-10-12).
  // The picker is a window of its own, the menu gone, its middle under the
  // block's handle 25px before the link and its top 2px under it (Notion's,
  // measured 2026-11-10: a picker in the 284px menu was cut).
  useInlineDatabaseRequests(block.id, (request) => {
    if (!menuPage || adapter?.readOnly) return;
    if (request === "rename") {
      setRenaming(menuPage.title);
      return;
    }
    // Several chosen pages' "아이콘 편집" opens this one's picker for them all (blockIconCompanions).
    companions.current = takeIconCompanions(block.id);
    const link = linkRef.current;
    if (!link) return;
    setIconAnchor({
      get top() { return link.getBoundingClientRect().top; },
      get bottom() { return link.getBoundingClientRect().bottom - 2; },
      get left() { return link.getBoundingClientRect().left - 25 - PICKER_WIDTH / 2; },
      contextElement: link,
    });
  });
  function changeIcon(icon: string, iconType: WorkspacePage["iconType"]) {
    setIconAnchor(null);
    if (!menuPage || !context?.pageMenu) return;
    const pageMenu = context.pageMenu;
    const others = companions.current.flatMap((id) => {
      const one = adapter?.graph.blocks.find((candidate) => candidate.id === id);
      return (one && context.pages.find((page) => page.id === childPageBlockPageId(one))) ?? [];
    });
    for (const page of [menuPage, ...others]) {
      void pageMenu.onPageMetadataChange(page, { icon, iconType }, new AbortController().signal).then((result) => {
        if (result === false) throw new Error("Icon change failed.");
        if (typeof result === "object") context.onPageChange(result);
      }).catch(() => context.onStatus?.(blockItemText("childPage.iconFailed"), "error"));
    }
  }

  function rename(title: string | null) {
    setRenaming(null);
    if (title === null || !menuPage || !context?.pageMenu || title === menuPage.title) return;
    void context.pageMenu.onPageMetadataChange(menuPage, { title }, new AbortController().signal).then((result) => {
      if (result === false) throw new Error("Rename failed.");
      if (typeof result === "object") context.onPageChange(result);
    }).catch(() => context.onStatus?.(blockItemText("childPage.renameFailed"), "error"));
  }
  // A page of a copy still being made holds its line, as Notion's copy does,
  // and shows once made (usePageLinkTarget asks again meanwhile).
  if (link.copying) {
    return <span className={styles.childPageWrap} contentEditable={false} data-page-copying="">
      <span className={styles.childPageCopying} aria-hidden="true"><span className={styles.childPageIcon} /></span>
    </span>;
  }
  // A sub-page its reader may not open isn't drawn (NativeTiptapBlockView).
  if (link.unavailable) return block.type === "child_page" ? null : <NoAccess />;
  if (!link.pageId) return <div className={styles.childPageMissing}>{blockItemText("pageLink.unavailable")}</div>;
  const database = block.type === "child_database" || link.iconSource.kind === "database";

  function openMenuFromKeyboard(event: KeyboardEvent<HTMLAnchorElement>) {
    if (!adapter || adapter.readOnly || !(event.key === "ContextMenu" || (event.shiftKey && event.key === "F10"))) return;
    event.preventDefault();
    event.stopPropagation();
    const rect = event.currentTarget.getBoundingClientRect();
    requestEditorBlockCommand(block.id, "menu", { x: inlineStartX(rect, 0, event.currentTarget), y: rect.bottom }, event.currentTarget);
  }

  return <span className={styles.childPageWrap} contentEditable={false}>
    <a ref={linkRef} className={styles.childPageLink} href={link.href} data-page-card="" data-page-id={link.pageId} onClick={(event) => { if (renaming === null) link.onClick(event); else event.preventDefault(); }}
      onKeyDown={openMenuFromKeyboard} onPointerEnter={link.readView} onFocus={link.readView}>
      <span className={styles.childPageIcon}>
        <PageIconGlyph page={link.iconSource} size={20} fallback={database ? "database" : "file"} /></span>
      {renaming !== null ? (
        <input className={styles.childPageRename} value={renaming} autoFocus aria-label={blockItemText("childPage.rename")}
          onFocus={(event) => event.currentTarget.select()} onChange={(event) => setRenaming(event.target.value)}
          onClick={(event) => event.stopPropagation()} onBlur={(event) => rename(event.currentTarget.value)}
          onKeyDown={(event) => {
            event.stopPropagation();
            if (fieldEnter(event)) event.currentTarget.blur();
            if (event.key === "Escape") rename(null);
          }} />
      ) : <span className={styles.childPageTitle}><TitleWithCustomEmoji text={link.title} named={link.titled} workspaceId={adapter?.workspaceId} /></span>}
      {/* A sub-page in the trash says so after its name, as a link to a page
          does (Notion's, seen 2026-11-07: "🗑 휴지통에 있음"). */}
      {link.inTrash && renaming === null ? <TrashPill /> : null}
    </a>
    {iconAnchor && menuPage && context?.pageMenu ? (
      // The block menu's picker has Notion's two tabs, no "업로드", 356px
      // high, and "제거" only while the page has an icon (seen 2026-11-10);
      // the page's own head keeps all three.
      <AnchoredIconPicker anchor={iconAnchor} marker="child-page-icon-picker" onClose={() => setIconAnchor(null)} picker={{
        uploadTarget: { workspaceId: menuPage.workspaceId, pageId: menuPage.id },
        short: true, current: menuPage,
        onPick: (emoji) => changeIcon(emoji, "emoji"),
        onPickCustomEmoji: (emoji) => changeIcon(emoji.id, "custom_emoji"),
        onPickBuiltInIcon: (value) => changeIcon(value, "icon"),
        onRemove: menuPage.icon ? () => changeIcon("", "none") : undefined,
      }} />
    ) : null}
  </span>;
}

/**
 * A link to a page: the page's icon and title, and the page's card when the
 * pointer rests on it, with no buttons, as Notion's (seen 2026-10-31). An
 * empty link opens a picker of the workspace's pages, which can also make a
 * new page here that takes the link's place; one the reader can't open can
 * be pointed elsewhere.
 */
export function LinkToPageBlock({ adapter, block, pagesById }: { adapter?: NativeEditorGraphAdapter; block: GraphBlock; pagesById?: LinkedPages }) {
  const pageNames = usePageNames();
  const link = useLinkedPage(block, adapter, pagesById);
  const pages = adapter?.moveContext?.pages;
  const session = adapter?.session;
  const editable = Boolean(adapter && !adapter.readOnly && pages && session);
  // Only a link that names nothing opens on its picker: one to a database or
  // a comment learns its page from the server after it is drawn.
  const [pickerOpen, setPickerOpen] = useState(() => editable && !link.namesTarget && block.content.pageLinkPickerDismissed !== true);
  // The page it names as the reader's list has it, none for a database's holder (useLinkedPage).
  const linked = link.workspacePage;
  // A wiki's link to one of its pages stands for the page's block, as a
  // Notion wiki's home shows its pages (child_page_block_moves.go): no arrow,
  // and it names its page for good.
  const wikiPage = block.content.wikiPage === true;

  function choose({ page, target }: PageLinkChoice) {
    if (!adapter) return;
    const { pageLinkPickerDismissed: _dismissed, sourcePageHref: _source, childPageId: _page, pageId: _old, databaseContainerId: _database,
      childPageTitle: _title, childPageIcon: _icon, childPageIconType: _iconType, childPageKind: _kind, ...content } = block.content;
    // A database in a page or a row's is named by its block, as a link to it always is (linkedTarget).
    adapter.replaceBlocks([{ ...block, content: "databaseId" in target ? { ...content, databaseContainerId: target.databaseId } : {
      ...content, childPageId: target.pageId, childPageTitle: pageNames.title(page),
      childPageIcon: page.icon, childPageIconType: page.iconType, childPageKind: page.kind ?? "page",
    } }]);
    // The picker's field had the caret; the link is left selected, as Notion's
    // is even with an empty line after it (seen 2026-11-09). Its keys act on
    // it, where Notion's picker leaves them acting on nothing.
    selectBlockWithoutCaret(block.id);
    setPickerOpen(false);
  }

  // A link that names no page goes as its picker closes unchosen, by Escape or a press outside, as Notion's
  // (Escape seen 2026-11-05; a press outside 2026-11-26 in Chrome 1440, three times in two tabs: the block gone
  // from the page, after a reload too). A block whose empty state is a place of its own stays, as Notion's do
  // (the same day, two tabs each): a picture's "이미지 추가", an equation's field, a linked view's "데이터 소스 연결".
  // One left shut before (pageLinkPickerDismissed, no longer written) keeps its picker shut.
  function leave() {
    if (adapter && !link.namesTarget) adapter.remove([block.id]);
    setPickerOpen(false);
  }

  const picker = pickerOpen && pages && adapter ? (
    <PageLinkPicker pages={pages} workspaceId={adapter.workspaceId} snapshot={adapter.mentionSnapshot}
      initialQuery="" onChoose={choose} onEscape={leave} onClose={leave} />
  ) : null;
  const change = editable ? (
    <button type="button" aria-haspopup="dialog" aria-expanded={pickerOpen} aria-label={blockItemText("pageLink.change")}
      title={blockItemText("common.change")} onClick={() => setPickerOpen(true)}><DotsHorizontal size="tiny" aria-hidden="true" /></button>
  ) : null;
  if (picker) return <div className={styles.linkToPageWrap} contentEditable={false}>{picker}</div>;
  // One its reader may not open reads as Notion's (seen 2026-11-09, a guest):
  // the crossed-out eye with the link's arrow, and "사용 권한 없음".
  if (link.unavailable) {
    return <div className={styles.linkToPageWrap} contentEditable={false}><div className={styles.linkToPageRow}>
      <a className={styles.linkToPage} href={link.href || undefined} data-page-link-no-access="true" onClick={link.href ? link.onClick : undefined}>
        <span className={styles.childPageIcon} data-link-arrow=""><EyeSlashIcon pixels={20} aria-hidden="true" /></span>
        <span className={styles.linkToPageText}><span>{blockItemText("pageLink.mentionNoAccess")}</span></span>
      </a>
      {change ? <span className={styles.linkToPageActions}>{change}</span> : null}</div></div>;
  }
  return <div className={styles.linkToPageWrap} contentEditable={false}>
    {link.pageId ? <div className={styles.linkToPageRow}>
      <a className={styles.linkToPage} href={link.href} data-page-card="" data-page-id={link.pageId} onClick={link.onClick}
        onPointerEnter={link.readView} onFocus={link.readView}>
        <span className={styles.childPageIcon} data-link-arrow={wikiPage ? undefined : ""}>
          {linked ? <PageIconGlyph page={linked} size={20} /> : link.emoji ? <span data-emoji-glyph="">{link.emoji}</span>
            : link.drawsIcon ? <PageIconGlyph page={link.iconSource} size={20} /> : "↗"}</span>
        <span className={styles.linkToPageText}><span><TitleWithCustomEmoji text={link.title} named={link.titled} workspaceId={adapter?.workspaceId} /></span></span>
        {link.inTrash ? <TrashPill /> : null}
      </a>
    </div> : null}
  </div>;
}

export function PageBlockContent({ adapter, block, pagesById }: {
  adapter?: NativeEditorGraphAdapter;
  block: GraphBlock;
  pagesById?: LinkedPages;
}) {
  if (block.type === "child_page" || block.type === "child_database") return <ChildPageBlock adapter={adapter} block={block} pagesById={pagesById} />;
  if (block.type === "link_to_page") return <LinkToPageBlock adapter={adapter} block={block} pagesById={pagesById} />;
  if (block.type === "breadcrumb") return <BreadcrumbBlock adapter={adapter} block={block} />;
  return adapter ? <TableOfContentsBlock adapter={adapter} color={typeof block.content.color === "string" ? block.content.color : undefined} /> : null;
}

/** "휴지통에 있음" after the name of a page in the trash, gray with its bin. */
function TrashPill() {
  return <span className={styles.linkToPageTrash} style={{ color: TRASH_MARK_COLOR, background: TRASH_MARK_BACKGROUND }}
    data-page-link-in-trash="">
    <span className={styles.linkToPageTrashMark} style={{ backgroundImage: TRASH_MARK }} aria-hidden="true" />
    {i18next.t("pageView:inTrashTopBarTitle")}
  </span>;
}
