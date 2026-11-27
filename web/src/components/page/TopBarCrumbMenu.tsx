import { useCallback, useEffect, useRef, useState, type CSSProperties, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ChevronRight } from "@/icons/leafen";
import { childTreePages, rootTreePages, sharedSectionPages } from "@/components/sidebar/pageTreeModel";
import { sortSidebarPages } from "@/components/sidebar/sidebarLayoutModel";
import { type DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import { pageHref } from "@/lib/navigation";
import { placeAround, placeBesideRow, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PageTitleText } from "@/components/page/TitleWithCustomEmoji";
import { useRouter } from "@/lib/router";
import { pageMapOf } from "@/lib/pageIndex";
import type { Page } from "@/types";
import type { SidebarSectionSort } from "@/workspaceMembershipWire.generated";
import { PageIconGlyph } from "./PageIcon";
import menuStyles from "./TopBarCrumbMenu.module.css";

// A path piece's menu opens as the pointer rests on it, as Notion's (seen
// 2026-10-28): about 0.2s on the piece opens it, 0.12s on a row opens the
// row's own pages beside it, and 0.2s away from both closes it.
const OPEN_DELAY = 200;
const SUB_DELAY = 120;
const CLOSE_DELAY = 200;
// Above a database page open over the page, as the path's own menu.
const MENU_LAYER = 130;
// A menu lists five pages and a "외 N개" row that lists the rest in place,
// as Notion's (seen 2026-11-08, beside menus too): the first five, or, where
// the path's page comes later, it first and the first four.
const SHOWN = 5;

/** A row: a page, or a teamspace whose rows are its top pages — none for an archived one. */
export type CrumbMenuRow = { id: string; page?: Page; teamspace?: Pick<DialogTeamspace, "icon" | "name">; archived?: boolean };
export type CrumbMenuList = { head?: string; rows: CrumbMenuRow[] };

/**
 * What a menu row opens beside it: the page's own pages, or the teamspace's
 * top pages; a teamspace's row opens beside it even with none, saying so,
 * as Notion's (seen 2026-11-14 for an archived teamspace's "하위 페이지 없음").
 */
function rowsUnder(row: CrumbMenuRow, pages: readonly Page[]): CrumbMenuList | null {
  const rows = row.teamspace
    ? row.archived ? [] : rootTreePages(pages).filter((page) => page.teamspaceId === row.id)
    : childTreePages(pages, row.id);
  return rows.length || row.teamspace ? { rows: rows.map((page) => ({ id: page.id, page })) } : null;
}

/**
 * The pages beside a path's page, as its piece's menu lists them: those
 * under the same page, or at the top those of its place — its teamspace's
 * (no head; the teamspace's piece names it), the shared pages or the
 * private pages, headed with the sidebar's name for them and in their
 * section's order, as Notion's (seen 2026-11-09: "최종 편집 일시" lists
 * the last edited first, "수동" as placed).
 */
export function pagesBeside(page: Page, allPages: readonly Page[], sharing: { shared: readonly string[]; workspaceWide: readonly string[] },
  heads: { shared: string; private: string }, sorts: { shared: SidebarSectionSort; private: SidebarSectionSort }): CrumbMenuList {
  // The reader's pages of other workspaces aren't beside it.
  const pages = allPages.filter(({ workspaceId }) => workspaceId === page.workspaceId);
  const pageRows = (list: readonly Page[]) => list.map((item) => ({ id: item.id, page: item }));
  if (page.parentId && pageMapOf(allPages).get(page.parentId)?.workspaceId === page.workspaceId) return { rows: pageRows(childTreePages(pages, page.parentId)) };
  const shared = new Set(sharing.shared);
  if (page.teamspaceId) return { rows: pageRows(rootTreePages(pages).filter((item) => item.teamspaceId === page.teamspaceId)) };
  if (page.parentId || shared.has(page.id)) {
    return { head: heads.shared, rows: pageRows(sortSidebarPages(sharedSectionPages(pages, shared, sharing.workspaceWide), sorts.shared)) };
  }
  const privatePages = rootTreePages(pages).filter((item) => !item.teamspaceId && !shared.has(item.id));
  return { head: heads.private, rows: pageRows(sortSidebarPages(privatePages, sorts.private)) };
}

function firstRows(rows: CrumbMenuRow[], onPath: (row: CrumbMenuRow) => boolean) {
  if (rows.length <= SHOWN) return rows;
  const first = rows.slice(0, SHOWN);
  const path = first.some(onPath) ? undefined : rows.find(onPath);
  return path ? [path, ...rows.slice(0, SHOWN - 1)] : first;
}

type Open = { key: string; anchor: HTMLElement };

// Where the mouse last pressed, till the pointer leaves a piece: a piece
// pressed opens its page, a press on the cover of a window over the page
// only puts that away, and the piece drawn or bared there under the pointer
// opens no menu, nor as the pointer moves on it, until it leaves the piece
// and comes back, as Notion's (seen 2026-11-16 in Chrome at 1440: one press
// on a parent's piece only went there, the pointer moved on the piece then
// opened nothing, and out and back on it opened the menu). Read for every
// path, as a new page draws its own.
let pressedAt: { x: number; y: number } | null = null;
if (typeof window !== "undefined") {
  window.addEventListener("pointerdown", (event) => {
    pressedAt = event.pointerType === "mouse" ? { x: event.clientX, y: event.clientY } : null;
  }, { capture: true, passive: true });
}
function underPress(piece: HTMLElement) {
  if (!pressedAt) return false;
  const box = piece.getBoundingClientRect();
  return pressedAt.x >= box.left && pressedAt.x <= box.right && pressedAt.y >= box.top && pressedAt.y <= box.bottom;
}

/**
 * The pointer's rest on a path's pieces: `bind(key)` goes on a piece, and
 * `open` names the piece whose menu shows. A touch, or a press on the piece,
 * opens none — nor the piece under a press until the pointer leaves it.
 */
export function useCrumbHover() {
  const [open, setOpen] = useState<Open | null>(null);
  const opening = useRef(0), closing = useRef(0);
  const close = useCallback(() => {
    window.clearTimeout(opening.current);
    window.clearTimeout(closing.current);
    setOpen(null);
  }, []);
  const stay = useCallback(() => window.clearTimeout(closing.current), []);
  const leave = useCallback(() => {
    window.clearTimeout(opening.current);
    window.clearTimeout(closing.current);
    closing.current = window.setTimeout(() => setOpen(null), CLOSE_DELAY);
  }, []);
  useEffect(() => () => {
    window.clearTimeout(opening.current);
    window.clearTimeout(closing.current);
  }, []);
  const bind = (key: string) => ({
    onPointerEnter: (event: ReactPointerEvent<HTMLElement>) => {
      if (event.pointerType !== "mouse" || underPress(event.currentTarget)) return;
      pressedAt = null;
      const anchor = event.currentTarget;
      stay();
      window.clearTimeout(opening.current);
      opening.current = window.setTimeout(() => setOpen({ key, anchor }), OPEN_DELAY);
    },
    onPointerLeave: () => { pressedAt = null; leave(); },
    onPointerDown: close,
  });
  return { open, bind, stay, leave, close };
}

/** The menu under a resting piece, and the menus its rows open beside it. */
export function CrumbMenu({ anchor, list, pages, currentId, onPath, onStay, onLeave, onClose }: {
  anchor: HTMLElement;
  list: CrumbMenuList;
  pages: readonly Page[];
  /** The page the path is of, its row marked as chosen. */
  currentId: string;
  /** The ids on the path above it, their rows marked as passed through in this menu only — the menus its rows open
   *  beside it list them first all the same but leave them unmarked, as Notion's (seen 2026-11-08). */
  onPath: ReadonlySet<string>;
  onStay(): void;
  onLeave(): void;
  onClose(): void;
}) {
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => { if (event.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose]);
  if (typeof document === "undefined") return null;
  return createPortal(
    <CrumbMenuPanel at={{ anchor }} list={list} pages={pages} currentId={currentId} onPath={onPath}
      onStay={onStay} onLeave={onLeave} onClose={onClose} />,
    document.body,
  );
}

function CrumbMenuPanel({ at, list, pages, currentId, onPath, onStay, onLeave, onClose }: {
  at: { anchor: HTMLElement } | { row: HTMLElement };
  list: CrumbMenuList;
  pages: readonly Page[];
  currentId: string;
  onPath: ReadonlySet<string>;
  onStay(): void;
  onLeave(): void;
  onClose(): void;
}) {
  const { t } = useTranslation(["topBar", "sidebar"]);
  const router = useRouter();
  const ref = useRef<HTMLDivElement>(null);
  const [beside, setBeside] = useState<{ row: HTMLElement; list: CrumbMenuList } | null>(null);
  const [whole, setWhole] = useState(false);
  const rows = whole ? list.rows : firstRows(list.rows, (row) => row.id === currentId || onPath.has(row.id));
  const besideTimer = useRef(0);
  useEffect(() => () => window.clearTimeout(besideTimer.current), []);
  const place = usePopoverPlacement(ref, (size, viewport) => "anchor" in at
    ? placeAround(popoverRectOf(at.anchor) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { gap: 4 })
    : placeBesideRow(at.row, size, viewport), ["anchor" in at ? at.anchor : at.row], undefined, false);

  function rest(row: CrumbMenuRow | null, element: HTMLElement) {
    window.clearTimeout(besideTimer.current);
    besideTimer.current = window.setTimeout(() => {
      const under = row && rowsUnder(row, pages);
      setBeside(under ? { row: element, list: under } : null);
    }, SUB_DELAY);
  }

  function go(event: ReactMouseEvent<HTMLAnchorElement>, href: string) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onClose();
    router.push(href);
  }

  return (
    <>
      <div ref={ref} role="menu" className={menuStyles.menu} data-topbar-crumb-menu
        style={{ position: "fixed", zIndex: MENU_LAYER, ...popoverPlacementStyle(place) } as CSSProperties}
        onPointerEnter={onStay} onPointerLeave={onLeave}>
        {list.head ? <div className={menuStyles.head}>{list.head}</div> : null}
        {list.rows.length === 0 ? <div className={menuStyles.empty} data-crumb-menu-empty>{t("sidebar:noPagesInside")}</div> : null}
        {rows.map((row) => {
          // A teamspace's row opens only its pages: Leafen has no teamspace home to go to.
          const href = row.page ? pageHref(row.page.id) : undefined;
          const more = rowsUnder(row, pages) !== null;
          return (
            <a key={row.id} role="menuitem" tabIndex={-1} className={menuStyles.row} href={href}
              data-current={row.id === currentId ? "true" : undefined}
              data-on-path={"anchor" in at && row.id !== currentId && onPath.has(row.id) ? "true" : undefined}
              data-open={beside?.row && beside.row.dataset.rowId === row.id ? "true" : undefined}
              data-row-id={row.id} data-teamspace={row.teamspace ? "true" : undefined}
              onPointerEnter={(event) => rest(row, event.currentTarget)}
              onClick={(event) => {
                if (href) go(event, href);
                else event.preventDefault();
              }}>
              <span className={menuStyles.icon} aria-hidden="true">
                {row.teamspace
                  ? <span className={menuStyles.glyph}>{teamspaceGlyph(row.teamspace)}</span>
                  : <PageIconGlyph page={row.page!} size={18} />}
              </span>
              <span className={menuStyles.label}>{row.teamspace ? row.teamspace.name : <PageTitleText page={row.page!} />}</span>
              {more ? <ChevronRight className={menuStyles.more} size="small" aria-hidden="true" /> : null}
            </a>
          );
        })}
        {rows.length < list.rows.length ? (
          <div role="menuitem" tabIndex={-1} className={`${menuStyles.row} ${menuStyles.rest}`} data-crumb-menu-rest
            onPointerEnter={(event) => rest(null, event.currentTarget)}
            onClick={() => setWhole(true)}>
            {t("topBar:crumbMenuRest", { count: list.rows.length - rows.length })}
          </div>
        ) : null}
      </div>
      {beside ? (
        <CrumbMenuPanel key={beside.row.dataset.rowId} at={{ row: beside.row }} list={beside.list} pages={pages}
          currentId={currentId} onPath={onPath} onStay={onStay} onLeave={onLeave} onClose={onClose} />
      ) : null}
    </>
  );
}
