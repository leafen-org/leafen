import { useLayoutEffect, useMemo, useState } from "react";
import { useCommentMentionSnapshot } from "@/components/comments/commentMentionContext";
import { useProductPages } from "@/components/product/productPagesContext";
import type { Page } from "@/types";
import { useFieldMenus, type MenuField } from "../editor/useFieldMenus";
import { titleAnchor } from "../editor/anchoredSubPage";
import type { PageTitleEngine } from "./pageTitleEngine";

/**
 * A page title's "@" menu: the body's (useFieldMenus), as Notion's title
 * opens it (seen 2026-11-10: dates, people with an invite, pages, and a new
 * page under this one or elsewhere). What it offers is the product's
 * (CommentMentionContext), the title being outside the body's editing; the
 * pages it makes go in the product's list, a sub-page hanging on the title
 * (anchoredSubPage.ts). A database's title, a wiki page's (its title
 * cell's) and a title only read open none.
 */
export function usePageTitleMentions(page: Page, readOnly: boolean, engine: () => PageTitleEngine | null) {
  const snapshot = useCommentMentionSnapshot();
  const productPages = useProductPages();
  const ownTitle = page.kind !== "database" && !page.wikiDataSourceId && !readOnly;
  const session = productPages?.session, setData = productPages?.setData, pages = productPages?.pages;
  const onStatus = productPages?.onStatus, refreshPages = productPages?.refreshPages;
  const fieldPages = useMemo(() => session && setData && pages ? {
    pageId: page.id, workspaceId: page.workspaceId,
    moveContext: {
      pages, session, onStatus, onPagesChanged: refreshPages,
      onPageChange: (made: Page) => setData((current) => ({ ...current,
        pages: current.pages.some((item) => item.id === made.id)
          ? current.pages.map((item) => item.id === made.id ? made : item) : [...current.pages, made] })),
    },
  } : null, [page.id, page.workspaceId, pages, onStatus, refreshPages, session, setData]);
  const [core, setCore] = useState<MenuField | null>(null);
  // The field is made in its owner's layout effect; the menu's pill follows it.
  useLayoutEffect(() => {
    const field = engine()?.field ?? null;
    if (field !== core) setCore(field);
  });
  return useFieldMenus({ field: () => engine()?.field ?? null, core, snapshot: ownTitle ? snapshot : null,
    pageId: page.id, anchor: titleAnchor, invitePage: ownTitle && session ? page.id : undefined, place: "page", fieldPages });
}
