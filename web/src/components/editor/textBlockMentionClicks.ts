import type { MouseEvent } from "react";
import { inlineDatabaseFullPageHref } from "@/lib/inlineDatabaseFullPage";
import { pageHref } from "@/lib/navigation";
import { openInSidePeek, openPagePeek } from "@/lib/pagePeek";
import { personDeactivated } from "@/lib/peopleDirectory";
import { openPersonProfile } from "@/lib/peopleProfiles";
import { pushRoute } from "@/lib/router";
import { appToastStack } from "@/lib/productToast";
import { i18next } from "@/i18n";
import type { DateMentionTarget } from "./DateMentionEditor";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import type { MentionCardTarget } from "./MentionCard";
import { dataSourcePreviewOnce, databasePreviewOnce } from "./pagePreviews";
import { openPageMention } from "./publicRichtext";
import { inlineStartX } from "@/lib/readingDirection";

/**
 * What a click on a text block's mention opens, as Notion's does even while
 * editing: a person mention their profile (or, without profiles, its card; a deactivated person's nothing), a date mention its editor, a database
 * mention the database alone as a page (a data source's at a view of it), and a page mention its page — in a
 * new tab with Cmd or Ctrl, in a side peek with Option.
 */
export function openTextBlockMention(event: MouseEvent<HTMLElement>, context: {
  adapter: NativeEditorGraphAdapter;
  showsRich: boolean;
  closeMenu: () => void;
  openCard: (card: MentionCardTarget) => void;
  openDate: (target: DateMentionTarget) => void;
}) {
  const { adapter, closeMenu } = context;
  const mention = (event.target as HTMLElement).closest<HTMLElement>("[data-mention]");
  if (!mention || !event.currentTarget.contains(mention)) return;
  const rect = mention.getBoundingClientRect();
  const anchor = { left: rect.left, top: rect.top, bottom: rect.bottom };
  const text = mention.textContent ?? "";
  if (mention.dataset.mention === "person" && mention.dataset.userId) {
    event.preventDefault();
    closeMenu();
    // A deactivated person's mention opens nothing, as Notion's (seen
    // 2026-11-15, a removed guest's, before and after a reload).
    if (personDeactivated(mention.dataset.userId)) return;
    // A member's mention opens their profile where the workspace has
    // profiles, as Notion's does; elsewhere, its card.
    if (openPersonProfile(mention.dataset.userId, event.metaKey || event.ctrlKey, mention)) return;
    context.openCard({ kind: "person", userId: mention.dataset.userId, text, anchor });
    return;
  }
  // A data source's opens its database alone as a page at a view of the
  // source, as Notion's does (seen 2026-11-22), one in the trash with its
  // database too; one sent to the trash alone opens the database as it opens
  // and says so (seen twice, two tabs).
  const dataSourceId = mention.dataset.mention === "database" ? mention.dataset.mentionDataSource : undefined;
  if (dataSourceId && adapter.session) {
    event.preventDefault();
    closeMenu();
    const newTab = event.metaKey || event.ctrlKey;
    void dataSourcePreviewOnce(adapter.session, dataSourceId).then((source) => {
      if (!source) return;
      const href = inlineDatabaseFullPageHref(source.pageId, source.databaseId, source.viewId || undefined);
      if (newTab) window.open(href, "_blank", "noopener,noreferrer");
      else pushRoute(href);
      if (source.trashedAlone) appToastStack()?.notify(String(i18next.t("blockItem:pageLink.mentionDataSourceInTrash")));
    });
    return;
  }
  const databaseId = mention.dataset.mention === "database" ? mention.dataset.databaseId : undefined;
  if (databaseId && adapter.session) {
    // A database mention opens the database alone as a page, as Notion's
    // does; a full-page database's page opens as it is; one in the trash
    // under the trash's banner, one sent there alone from a page out of it
    // too (seen 2026-11-24, two tabs; lib/trashedDatabaseAlone.ts).
    event.preventDefault();
    closeMenu();
    const newTab = event.metaKey || event.ctrlKey;
    void databasePreviewOnce(adapter.session, databaseId).then((database) => {
      if (!database) return;
      const href = inlineDatabaseFullPageHref(database.pageId, databaseId);
      if (newTab) window.open(href, "_blank", "noopener,noreferrer");
      else pushRoute(href);
    });
    return;
  }
  // A page of the reader's own list pressed in a side peek opens in it (openInSidePeek).
  const pageId = mention.dataset.mention === "page" ? mention.dataset.pageId : undefined;
  const listed = Boolean(pageId) && adapter.moveContext?.pages.some((page) => page.id === pageId && !page.inTrash);
  const plainPress = event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey;
  if ((mention.dataset.mention === "page" || mention.dataset.databasePageId) && adapter.readOnly) {
    if (pageId && listed && plainPress && !event.altKey && openInSidePeek(pageId, mention)) event.preventDefault();
    else if (context.showsRich) openPageMention(event);
    return;
  }
  if (pageId) {
    event.preventDefault();
    closeMenu();
    // ⌥Click opens a page of the reader's own list in a side peek, as
    // Notion's does (seen 2026-10-02); another opens as a click does.
    if (plainPress && listed && event.altKey) {
      openPagePeek(pageId);
      return;
    }
    if (plainPress && listed && openInSidePeek(pageId, mention)) return;
    const href = mention.getAttribute("href") || pageHref(pageId);
    if (event.metaKey || event.ctrlKey) window.open(href, "_blank", "noopener,noreferrer");
    else pushRoute(href);
    return;
  }
  if (adapter.readOnly || !adapter.collaborationDocument) return;
  const date = dateMentionTarget(mention, event.currentTarget);
  if (!date) return;
  event.preventDefault();
  closeMenu();
  context.openDate(date);
}

/**
 * A pressed date mention's date window: which of the text's dates it is and
 * where the window hangs — the same in a page's text, a database cell and a
 * comment box, as Notion's.
 */
export function dateMentionTarget(mention: HTMLElement, text: Element): DateMentionTarget | null {
  // One whose start was never given opens it too, as Notion's (seen 2026-11-11).
  if (mention.dataset.mention !== "date" || mention.dataset.date === undefined) return null;
  const rect = mention.getBoundingClientRect();
  // The window hangs from the date's text, its top at the text's foot with no
  // gap: the line's middle plus 9px at 16px (Notion's, seen 2026-11-06: the
  // mention's 18px text box ends where the window starts). The mention here is
  // a line high, so its foot is taken from its middle.
  const size = Number.parseFloat(getComputedStyle(mention).fontSize) || 16;
  const foot = (rect.top + rect.bottom) / 2 + size * 0.5625;
  return {
    ordinal: Array.from(text.querySelectorAll('[data-mention="date"]')).indexOf(mention),
    date: mention.dataset.date,
    dateEnd: mention.dataset.dateEnd,
    dateTimeZone: mention.dataset.dateTimeZone,
    dateReminder: mention.dataset.dateReminder,
    dateFormat: mention.dataset.dateFormat,
    dateTimeFormat: mention.dataset.dateTimeFormat,
    anchor: { left: inlineStartX(rect, 0, mention), top: rect.top, bottom: foot },
  };
}
