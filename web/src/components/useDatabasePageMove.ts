import { useMemo, useRef, type Dispatch, type SetStateAction } from "react";
import type { GraphRow } from "@/contentGraph";
import { api } from "@/api";
import { changeWikiPage, movePageIntoDatabase } from "@/databaseRowPageApi";
import { commitNativePageMetadata } from "@/lib/nativePageMetadata";
import { createDatabaseRowPageService, type DatabaseRowMoveDestination } from "@/lib/databaseRowPageService";
import { newId } from "@/lib/ids";
import { holdPageThroughMove } from "@/lib/pageBecomingRow";
import { useOpenPageGraphStore, type PageGraphStore } from "@/lib/pageGraphStore";
import { holdRowsGone, rowsDrawn } from "@/lib/rowsShownAtPress";
import type { Bootstrap, Page, Session } from "@/types";
import type { MoveToDialogRow } from "./MoveToDialog";
import { appendMovePosition } from "./pageMoveDialogModel";
import { useProductPages } from "./product/productPagesContext";

/**
 * A database's page moved from a place that doesn't say it is one — a wiki's
 * page in the sidebar, the page's own top bar or menu — goes a database page's
 * way: the values its place can't keep are asked about, and it comes out as a
 * page or goes into another database, as the favorite row's menu moves it. A
 * page's move doesn't find a database page (2026-11-09: a wiki's first page
 * moved out of it answered "page not found"). Under its own wiki's page or
 * another page of its wiki, it stays in the wiki, last under that page, as
 * Notion's does (seen 2026-11-11), and "실행 취소" puts it back where it was.
 * The sidebar shows it there at the press — last under that page, or gone
 * from the wiki until the list is read again — and back where it was when it
 * stays (user decision 2026-11-12: a press answers on the next frame); gone
 * from the wiki, it leaves the open page's view of the wiki at the press too.
 * Its notice's "실행 취소" shows it back where it was at the press, in the
 * sidebar and the wiki's views, until the server put it back.
 */
export function useDatabasePageMove(
  session: Session, page: Page | undefined, given: MoveToDialogRow | undefined, pagesById: Readonly<Record<string, Page>> = {},
) {
  const service = useMemo(() => createDatabaseRowPageService(session, () => {}), [session]);
  const databasePage = !given && page && isWikiPage(page) ? page : undefined;
  const pagesRef = useRef(pagesById);
  pagesRef.current = pagesById;
  // The sidebar and the page's path read the wiki again for where it went.
  const productPages = useProductPages();
  const refreshPages = productPages?.refreshPages, setData = productPages?.setData;
  const store = useOpenPageGraphStore();
  return useMemo<MoveToDialogRow | undefined>(() => {
    const page = databasePage;
    if (given || !page) return given;
    const withinWiki = (target: DatabaseRowMoveDestination) => target.kind === "page" && target.id !== page.id
      && (target.id === page.wikiRootId || pagesRef.current[target.id]?.wikiRootId === page.wikiRootId);
    // The database it came from, read when it moves, and where it was listed
    // and drawn at the press, for "실행 취소".
    let source = "", listedBefore: Page | undefined, drawn: readonly GraphRow[] = [];
    // Out of the database or the wiki's place it went to, back where it was.
    const putBack = async (target: DatabaseRowMoveDestination) => {
      if (withinWiki(target)) {
        await changeWikiPage(session, page.id, {
          requestId: newId(), parentId: page.parentId ?? page.wikiRootId ?? "", position: page.position,
        });
        await refreshPages?.();
        return;
      }
      if (!source) return;
      if (target.kind === "page") {
        await movePageIntoDatabase(session, page.id, { requestId: newId(), dataSourceId: source }, new AbortController().signal);
        return;
      }
      await service.refresh(page.id);
      await service.move(page.id, { kind: "database", id: source }, []);
    };
    return {
      page,
      leave: (target: DatabaseRowMoveDestination) => {
        const listed = pagesRef.current[page.id];
        listedBefore = listed;
        drawn = withinWiki(target) ? [] : rowsDrawn(store, [page.id]);
        const shown = listed && withinWiki(target) ? { ...listed, parentId: target.id, position: appendMovePosition({ ...pagesRef.current }, target.id, page.id) } : null;
        const gone = withinWiki(target) ? () => undefined : holdRowsGone(store, drawn);
        // Into another database, the open one is drawn till it is its row there (pageBecomingRow.ts).
        const turned = target.kind === "database" ? holdPageThroughMove(session, [page.id]) : () => undefined;
        if (setData && listed) {
          setData((current) => ({ ...current, pages: shown
            ? current.pages.map((item) => item.id === page.id ? shown : item) : current.pages.filter(({ id }) => id !== page.id) }));
        }
        return (refused = false) => {
          gone(refused);
          if (refused && setData && listed) setData((current) => ({ ...current, pages: [...current.pages.filter(({ id }) => id !== page.id), listed] }));
          turned(refused);
        };
      },
      preview: (target: DatabaseRowMoveDestination) => withinWiki(target) ? Promise.resolve([]) : service.previewMove(page.id, target),
      move: async (target: DatabaseRowMoveDestination, accepted: string[]) => {
        if (withinWiki(target)) {
          const moved = await changeWikiPage(session, page.id, { requestId: newId(), parentId: target.id });
          await refreshPages?.();
          return moved;
        }
        source = (await service.refresh(page.id))?.row.dataSourceId ?? "";
        const moved = await service.move(page.id, target, accepted);
        await Promise.resolve(refreshPages?.()).catch(() => undefined);
        return moved;
      },
      undo: async (target: DatabaseRowMoveDestination) => {
        const listed = listedBefore, back = holdRowsGone(store, drawn, true);
        if (setData && listed) setData((current) => ({ ...current, pages: [...current.pages.filter(({ id }) => id !== page.id), listed] }));
        try {
          await putBack(target);
          back();
        } catch (error) {
          back(true);
          await Promise.resolve(refreshPages?.()).catch(() => undefined);
          throw error;
        }
      },
    };
  }, [databasePage, given, refreshPages, service, session, setData, store]);
}

/**
 * Whether a page is one of a wiki's: every page in a wiki's tree is its
 * database's page, and names the wiki's page it is under (bootstrap's wiki
 * metadata); the wiki's own page is a page.
 */
function isWikiPage(page: Page) {
  return Boolean(page.wikiRootId && page.wikiRootId !== page.id);
}

/**
 * Puts a page moved into a database back where it was, for its notice's
 * "실행 취소" (Notion's, seen 2026-11-11: "〈page〉의 변경된 위치는 〈database〉입니다."
 * with "이동" and "실행 취소"): out of the database as a page under the page
 * it was under, or at the top of its place, then in its old place there.
 */
export async function putPageBackFromDatabase(session: Session, before: Page) {
  await createDatabaseRowPageService(session, () => {}).move(before.id,
    before.parentId ? { kind: "page", id: before.parentId } : { kind: "page", id: "", teamspaceId: before.teamspaceId ?? "" });
  const current = await api.readPage(session, before.id);
  return (await commitNativePageMetadata(session, current, {
    parentId: before.parentId, teamspaceId: before.teamspaceId ?? "", position: before.position,
  })).page;
}

/**
 * putPageBackFromDatabase shown at the press (user decision 2026-11-12: a
 * press answers on the next frame — the sidebar and the database's views
 * changed once the server had put it back): the page, with the pages
 * `listed` under it, is back in the sidebar where it was, edited now, as the
 * server writes it and as Notion's moved page is (seen 2026-11-13: its last
 * edit was the "실행 취소") — a section listing by last edit shows it first,
 * as it does once read again — and its row leaves the databases the open
 * page and the sidebar draw, until the server put it back; refused, it is a
 * row again and gone from the sidebar.
 */
export async function putPageBackShown(input: {
  session: Session; before: Page; listed: readonly Page[]; store: PageGraphStore | null;
  setData?: Dispatch<SetStateAction<Bootstrap>>;
}) {
  const { session, before, store, setData } = input;
  const edited = new Date().toISOString();
  const listed = input.listed.map((page) => page.id === before.id ? { ...page, updatedAt: edited } : page);
  const ids = new Set(listed.map(({ id }) => id));
  const gone = holdRowsGone(store, rowsDrawn(store, [before.id]));
  setData?.((current) => ({ ...current, pages: [...current.pages.filter(({ id }) => !ids.has(id)), ...listed] }));
  try {
    const page = await putPageBackFromDatabase(session, before);
    gone();
    return page;
  } catch (error) {
    gone(true);
    setData?.((current) => ({ ...current, pages: current.pages.filter(({ id }) => !ids.has(id)) }));
    throw error;
  }
}
