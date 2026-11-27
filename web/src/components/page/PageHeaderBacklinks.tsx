import { useMemo } from "react";
import { useSidebarLayout } from "@/components/shell/AppShell";
import type { BacklinksDisplay } from "@/lib/modelTypes/page";
import {
  createPageBacklinksService,
  retainedPageForBacklinks,
  type NativeBacklinkPage,
} from "@/lib/pageBacklinksService";
import type { Page, Session } from "@/types";
import { pageMapOf } from "@/lib/pageIndex";
import { PageBacklinks } from "./PageBacklinks";

type BacklinkPage = Page & { backlinksDisplay?: BacklinksDisplay };
type RetainedPages = Readonly<Record<string, NativeBacklinkPage>>;

const retainedOf = new WeakMap<Page, NativeBacklinkPage>();
const recordsOf = new WeakMap<readonly Page[], RetainedPages>();

/**
 * The list's pages by id as backlinks read them, each made as it is looked
 * up: the backlinks read a few pages, the open page's sources and the pages
 * above them, and a record of every page made again for each new list held a
 * 44,000-page workspace some 55 ms at CPU ×2 as a title was saved (2026-11-15).
 */
function retainedPagesById(pages: readonly Page[]): RetainedPages {
  let record = recordsOf.get(pages);
  if (record) return record;
  const byId = pageMapOf(pages);
  record = new Proxy({} as RetainedPages, {
    get(_target, id) {
      const page = typeof id === "string" ? byId.get(id) : undefined;
      if (!page) return undefined;
      let retained = retainedOf.get(page);
      if (!retained) {
        retained = retainedPageForBacklinks(page);
        retainedOf.set(page, retained);
      }
      return retained;
    },
    has: (_target, id) => typeof id === "string" && byId.has(id),
  });
  recordsOf.set(pages, record);
  return record;
}

export function PageHeaderBacklinks({
  page,
  pages,
  session,
  onPageChange,
}: {
  page: BacklinkPage;
  pages: Page[];
  session: Session;
  onPageChange?: (page: Page) => void;
}) {
  const { mobile, sidebarOpen, toggleSidebarChrome } = useSidebarLayout();
  const listBacklinks = useMemo(() => createPageBacklinksService(session), [session]);
  const localPagesById = useMemo(() => retainedPagesById(pages), [pages]);

  return (
    <PageBacklinks
      pageId={page.id}
      workspaceId={page.workspaceId}
      display={page.backlinksDisplay ?? "default"}
      localBlocksByPage={{}}
      localPagesById={localPagesById}
      loadedSourcePageIds={[]}
      listBacklinks={listBacklinks}
      onApplyRemotePage={(remotePage) => onPageChange?.(remotePage as NativeBacklinkPage)}
      onSidebarClose={() => {
        if (mobile && sidebarOpen) toggleSidebarChrome();
      }}
    />
  );
}
