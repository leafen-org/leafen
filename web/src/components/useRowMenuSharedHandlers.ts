"use client";

import { type RefObject, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { copyPageLink } from "@/components/editor/pageLinkClipboard";
import { copyText } from "@/lib/copyText";
import { openPageInNewTab, readPageLinkToCopy } from "@/lib/databasePageLink";
import { newWikiDatabaseNames } from "@/lib/fullPageDatabase";
import { withLoadDeadline } from "@/lib/lazyScreen";
import { usePageNames } from "@/lib/pageNames";
import type { useProductToast } from "@/lib/productToast";
import { announceWikiChange, forgetNewWiki, markNewWiki } from "@/lib/wikiChangeEvents";
import type { Page, Session } from "@/types";
import { createWikiService, type WikiPageSummary } from "@/wikiApi";
import { type MovePopoverAnchor, movePopoverAnchorOf } from "./movePopoverPlacement";
import { confirmWikiUndo } from "./wikiUndoConfirm";

/** RowMenu's run of a change that keeps the menu busy until it is done. */
export type RowMenuRunOperation = <T>(operation: (signal: AbortSignal) => Promise<T>, options?: {
  closeAfter?: boolean;
  onFailure?: (error: unknown) => void;
  onSuccess?: (result: T) => void | Promise<void>;
}) => void;
/** RowMenu's run of a change that goes on past the menu, which closes. */
export type RowMenuRunPastMenu = <T>(operation: (signal: AbortSignal) => Promise<T>, options: {
  onFailure(error: unknown): void;
  onSuccess(result: T): void;
}) => void;

function wikiSummaryFromPage(page: Page): WikiPageSummary {
  return {
    id: page.id,
    workspaceId: page.workspaceId,
    title: page.title,
    parentId: page.parentId,
    icon: page.icon,
    iconType: page.iconType,
    wikiRevision: page.wikiRevision ?? 0,
    verifiedAt: page.verifiedAt ?? "",
    verifiedBy: page.verifiedBy ?? "",
    verificationExpiresAt: page.verificationExpiresAt ?? "",
  };
}

type WikiChange = Awaited<ReturnType<ReturnType<typeof createWikiService>["undo"]>>;
const wikiPageAfter = (current: Page, result: WikiChange): Page => ({
  ...current,
  isWiki: result.isWiki,
  wikiRootId: result.wikiRootId,
  wikiRevision: result.page.wikiRevision,
  verifiedAt: result.page.verifiedAt,
  verifiedBy: result.page.verifiedBy,
  verificationExpiresAt: result.page.verificationExpiresAt,
});

/**
 * The handlers every variant of a page's menu shares (its full menu, a
 * sidebar row's short one, a phone's sheets): copying its link and its
 * contents, moving it, opening it in a new tab, renaming it and turning it
 * into a wiki or back. RowMenu keeps the busy and close handling they run in.
 */
export function useRowMenuSharedHandlers({ page, session, anchor, menuRef, hangsFrom, close, notify, runOperation, runPastMenu, onPageChange, onRename }: {
  page: Page;
  session: Session;
  /** Where the menu was placed by a point, not hung from a bar's button. */
  anchor: unknown;
  menuRef: RefObject<HTMLDivElement | null>;
  hangsFrom(): HTMLElement | null;
  close(): void;
  notify: ReturnType<typeof useProductToast>["notify"];
  runOperation: RowMenuRunOperation;
  runPastMenu: RowMenuRunPastMenu;
  onPageChange?: (page: Page) => void | Promise<void>;
  onRename?: () => void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["rowMenu", "topBar"]);
  const [moveOpen, setMoveOpen] = useState(false);
  const [moveAnchor, setMoveAnchor] = useState<MovePopoverAnchor | null>(null);
  // The page as the list holds it now: a title typed just before a wiki change
  // is saved first (wikiApi.ts) and stays with the page the change gives back.
  const latest = useRef(page);
  latest.current = page;

  // A new wiki's notice names the page and undoes it, as Notion's (seen 2026-10-11); undoing one whose pages are its
  // database's asks over the closed menu first, answered back on what the menu hung from (confirmWikiUndo), and ends
  // without a notice (2026-11-13).
  async function toggleWiki() {
    if (!onPageChange) return;
    const undoing = page.isWiki && page.wikiRootId === page.id;
    if (undoing && page.wikiDataSourceId) {
      const from = hangsFrom(); close(); from?.focus({ preventScroll: true });
      if (!await confirmWikiUndo(page)) return;
    }
    const service = createWikiService(api, session, page.wikiRootId || page.id);
    // The new wiki greets its maker whichever draws it first, a read of its
    // page while the job runs or the job's answer (2026-11-21: the title typed
    // just before went first and its answer read the page again, which drew
    // the wiki before the job was answered and greeted no one, 10 times in 10).
    if (!undoing) markNewWiki(page.id);
    runOperation(
      (signal) => undoing
        ? service.undo(wikiSummaryFromPage(page), signal)
        : service.convert(wikiSummaryFromPage(page), signal, newWikiDatabaseNames()),
      {
        onFailure: () => {
          if (!undoing) forgetNewWiki(page.id);
          notify(t("topBar:wikiUpdateFailed"), "error");
        },
        onSuccess: async (result) => {
          const changed = wikiPageAfter(latest.current.id === page.id ? latest.current : page, result);
          await onPageChange(changed);
          announceWikiChange(page.id);
          if (undoing) return;
          // Its undo takes the wiki back with no further word, as Notion's
          // (seen 2026-11-14: the page is a page again, no notice or question).
          // It stays ten seconds, Notion's for this one (its web client, read
          // 2026-11-16: the wiki conversion toast's durationMs 1e4).
          notify(t("topBar:wikiCreated", { title: pageNames.title(page) }), "success", {
            label: t("rowMenu:actions.undo"),
            autoDismissMs: 10_000,
            onClick: async () => {
              try {
                const undone = await createWikiService(api, session, page.id)
                  .undo(wikiSummaryFromPage(changed), new AbortController().signal);
                await onPageChange(wikiPageAfter(changed, undone));
                announceWikiChange(page.id);
              } catch {
                notify(t("topBar:wikiUpdateFailed"), "error");
              }
            },
          });
        },
      },
    );
  }

  const copyLink = () => runPastMenu(async () => {
    if (!(await copyPageLink(await readPageLinkToCopy(session, page), page.id, pageNames.title(page)))) throw new Error("Copy failed.");
  }, {
    onFailure: () => notify(t("rowMenu:toast.couldntCopyLink"), "error"),
    onSuccess: () => notify(t("rowMenu:toast.copiedLink"), "success"),
  });
  // "페이지 내용 복사하기": the page's contents as Markdown, without its title.
  const copyContents = () => runOperation(async (signal) => {
    const adapter = await withLoadDeadline(import("./nativeExport"));
    const markdown = (await adapter.readPageMarkdown(page, { session, signal })).replace(/^# .*\n\n?/u, "");
    // An empty page has nothing to copy, which isn't a failure.
    if (!markdown.trim()) return false;
    if (!(await copyText(markdown))) throw new Error("Copy failed.");
    return true;
  }, {
    onFailure: () => notify(t("rowMenu:toast.couldntCopyContents"), "error"),
    onSuccess: (copied) => notify(t(copied ? "rowMenu:toast.copiedContents" : "rowMenu:toast.nothingToCopy"), copied ? "success" : "default"),
  });
  const openMove = () => {
    // "옮기기" opens right under what opened the menu, 8px above where the menu was (Notion's, seen 2026-11-05);
    // from a bar's "…" (a peek's) lined up with the button, as the page's own bar's (seen 2026-11-13: 1098.5, 108).
    const bar = anchor ? null : movePopoverAnchorOf(hangsFrom()), menu = movePopoverAnchorOf(menuRef.current);
    setMoveAnchor(bar ? { ...bar, top: bar.top - 4, bottom: bar.bottom - 4 } : menu ? { ...menu, top: menu.top - 12, bottom: menu.top - 12 } : null);
    setMoveOpen(true);
  };
  // Its own address, a full-page database's view with it, nothing of the address open now (openPageInNewTab).
  const openInNewTab = () => { openPageInNewTab(page, session.user.id); close(); };
  const rename = onRename ? () => { close(); onRename(); } : undefined;
  const wiki = onPageChange && (!page.wikiRootId || (page.isWiki && page.wikiRootId === page.id)) ? () => void toggleWiki() : undefined;
  return { copyLink, copyContents, openMove, moveOpen, setMoveOpen, moveAnchor, openInNewTab, rename, wiki };
}
